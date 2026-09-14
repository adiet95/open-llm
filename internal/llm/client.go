package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrEmptyResponse is returned when the provider replies with no choices.
var ErrEmptyResponse = errors.New("llm: provider returned no choices")

// Client talks to a single OpenAI-compatible chat-completions endpoint.
// Reuse one Client for the whole application (it wraps a pooled *http.Client).
type Client struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
}

// NewClient builds a Client. baseURL must include the API version segment
// (e.g. https://api.groq.com/openai/v1). apiKey may be empty for local Ollama.
func NewClient(baseURL, apiKey, defaultModel string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		defaultModel: defaultModel,
		httpClient:   httpClient,
	}
}

// Chat performs a non-streaming chat completion.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read chat response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.apiError(resp.StatusCode, body)
	}

	var wire wireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	if wire.Error != nil {
		return nil, fmt.Errorf("llm provider error: %s", wire.Error.Message)
	}
	if len(wire.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	return &ChatResponse{
		Content: wire.Choices[0].Message.Content,
		Model:   wire.Model,
		Usage:   wire.Usage,
	}, nil
}

// ChatStream performs a streaming chat completion, invoking onChunk for each
// content delta as it arrives. It stops early and returns ctx.Err() if the
// context is cancelled.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onChunk func(string) error) error {
	req.Stream = true
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return c.apiError(resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	// SSE lines can exceed the default 64KB scanner buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk wireResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip malformed keep-alive or comment lines rather than failing.
			continue
		}
		if chunk.Error != nil {
			return fmt.Errorf("llm provider error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if delta := chunk.Choices[0].Delta.Content; delta != "" {
			if err := onChunk(delta); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, req ChatRequest) (*http.Response, error) {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	payload := wireRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send chat request: %w", err)
	}
	return resp, nil
}

func (c *Client) apiError(status int, body []byte) error {
	var wire wireResponse
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error != nil {
		return fmt.Errorf("llm provider returned %d: %s", status, wire.Error.Message)
	}
	return fmt.Errorf("llm provider returned %d: %s", status, strings.TrimSpace(string(body)))
}
