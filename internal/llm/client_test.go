package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/wrong auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "test-model",
			"choices": [{"message": {"content": "hello there"}}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model", srv.Client())
	resp, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if resp.Content != "hello there" {
		t.Errorf("content = %q, want %q", resp.Content, "hello there")
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("total tokens = %d, want 7", resp.Usage.TotalTokens)
	}
}

func TestChatProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"message": "rate limited", "type": "rate_limit"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m", srv.Client())
	_, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want it to mention 'rate limited'", err)
	}
}

func TestChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, chunk := range []string{"Hel", "lo", "!"} {
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + chunk + `"}}]}` + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "m", srv.Client())
	var got strings.Builder
	err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(chunk string) error {
		got.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if got.String() != "Hello!" {
		t.Errorf("streamed content = %q, want %q", got.String(), "Hello!")
	}
}


func TestExtract(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		// Model returns a valid JSON object as its content (structured output).
		_, _ = w.Write([]byte(`{
			"model": "test-model",
			"choices": [{"message": {"content": "{\"amount\":500000,\"recipient\":\"Budi\"}"}}]
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "test-model", srv.Client())
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"amount":    map[string]any{"type": "integer"},
			"recipient": map[string]any{"type": "string"},
		},
		"required": []string{"amount", "recipient"},
	}

	raw, resp, err := c.Extract(context.Background(), "Kirim 500000 ke Budi", "transfer", schema, "")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if resp.Model != "test-model" {
		t.Errorf("model = %q, want test-model", resp.Model)
	}

	// The request must carry a response_format (structured output).
	if _, ok := gotBody["response_format"]; !ok {
		t.Error("request did not include response_format")
	}

	// The returned bytes must be valid JSON we can unmarshal into our shape.
	var out struct {
		Amount    int    `json:"amount"`
		Recipient string `json:"recipient"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("returned data is not valid JSON: %v", err)
	}
	if out.Amount != 500000 || out.Recipient != "Budi" {
		t.Errorf("parsed = %+v, want {500000 Budi}", out)
	}
}

func TestExtractRejectsEmptySchema(t *testing.T) {
	c := NewClient("http://unused", "", "m", nil)
	if _, _, err := c.Extract(context.Background(), "text", "n", nil, ""); err == nil {
		t.Fatal("expected error for empty schema")
	}
}
