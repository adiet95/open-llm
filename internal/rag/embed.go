// Package rag implements a small, production-shaped Retrieval-Augmented
// Generation pipeline using only the standard library:
//
//	documents -> Chunk -> Embed -> Store (cosine search) -> Retrieve -> Augment -> Generate
//
// Embeddings come from any OpenAI-compatible /embeddings endpoint (local Ollama
// by default, free). Generation reuses the existing llm.Client. The vector
// store is an in-process cosine index persisted to a JSON file — dependency-free
// and fine for small/medium corpora; swap it for pgvector later behind the same
// Store interface without touching the rest of the pipeline.
package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Embedder turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OpenAIEmbedder calls an OpenAI-compatible /embeddings endpoint.
type OpenAIEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAIEmbedder builds an embedder. baseURL must include the version
// segment (e.g. http://localhost:11434/v1). apiKey may be empty for local Ollama.
func NewOpenAIEmbedder(baseURL, apiKey, model string, httpClient *http.Client) *OpenAIEmbedder {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIEmbedder{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		httpClient: httpClient,
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed returns one vector per input text, in the same order.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := e.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send embed request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var er embedResponse
	if err := json.Unmarshal(body, &er); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embeddings provider error: %s", er.Error.Message)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings count mismatch: got %d vectors for %d inputs", len(er.Data), len(texts))
	}

	// Preserve input order using the returned index.
	out := make([][]float32, len(texts))
	for _, d := range er.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("embeddings returned out-of-range index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
