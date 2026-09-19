package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/adiet95/open-llm/internal/llm"
)

func TestChunkOverlap(t *testing.T) {
	text := strings.Repeat("word ", 500) // ~2500 runes
	chunks := Chunk(text, 800, 150)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c)) > 800+10 {
			t.Errorf("chunk %d longer than size: %d runes", i, len([]rune(c)))
		}
	}
}

func TestChunkShortText(t *testing.T) {
	chunks := Chunk("hello world", 800, 150)
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Fatalf("short text should be one chunk, got %v", chunks)
	}
}

// fakeEmbedder maps text to a tiny deterministic vector so cosine search is
// predictable without calling a real embeddings API.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		// 3 dims: counts of 'a', 'b', 'c' — enough to separate documents.
		var a, b, c float32
		for _, r := range t {
			switch r {
			case 'a':
				a++
			case 'b':
				b++
			case 'c':
				c++
			}
		}
		out[i] = []float32{a, b, c}
	}
	return out, nil
}

// fakeGenerator echoes the prompt so we can assert the context was injected.
type fakeGenerator struct{ lastPrompt string }

func (g *fakeGenerator) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	g.lastPrompt = req.Messages[len(req.Messages)-1].Content
	return &llm.ChatResponse{Content: "answer grounded in context", Model: "fake"}, nil
}

func TestServiceIngestAndQuery(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore("") // in-memory, no persistence
	if err != nil {
		t.Fatal(err)
	}
	gen := &fakeGenerator{}
	svc := NewService(fakeEmbedder{}, store, gen, 800, 150, 2)

	if _, err := svc.Ingest(ctx, "doc-a", "aaaa aaaa aaaa"); err != nil {
		t.Fatalf("ingest doc-a: %v", err)
	}
	if _, err := svc.Ingest(ctx, "doc-c", "cccc cccc cccc"); err != nil {
		t.Fatalf("ingest doc-c: %v", err)
	}

	res, err := svc.Query(ctx, "aaaa", nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Sources) == 0 {
		t.Fatal("expected sources")
	}
	// The 'a'-heavy query should retrieve doc-a as the top source.
	if res.Sources[0].Source != "doc-a" {
		t.Errorf("top source = %q, want doc-a", res.Sources[0].Source)
	}
	// The prompt handed to the LLM must contain the retrieved context.
	if !strings.Contains(gen.lastPrompt, "aaaa") {
		t.Error("expected retrieved context injected into the prompt")
	}
}

func TestQueryEmptyStore(t *testing.T) {
	store, _ := NewMemoryStore("")
	svc := NewService(fakeEmbedder{}, store, &fakeGenerator{}, 800, 150, 2)
	if _, err := svc.Query(context.Background(), "anything", nil); err == nil {
		t.Fatal("expected error querying an empty knowledge base")
	}
}
