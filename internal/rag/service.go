package rag

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/adiet95/open-llm/internal/llm"
)

// Generator is the subset of llm.Client the RAG service needs. Defining it as
// an interface keeps rag decoupled from llm and makes the service testable.
type Generator interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// Service ties the RAG pipeline together: chunk + embed + store on ingest,
// embed + retrieve + augment + generate on query.
type Service struct {
	embedder     Embedder
	store        Store
	generator    Generator
	chunkSize    int
	chunkOverlap int
	topK         int
}

// NewService builds a RAG service.
func NewService(embedder Embedder, store Store, generator Generator, chunkSize, chunkOverlap, topK int) *Service {
	return &Service{
		embedder:     embedder,
		store:        store,
		generator:    generator,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		topK:         topK,
	}
}

// IngestResult reports what an ingest produced.
type IngestResult struct {
	Source string `json:"source"`
	Chunks int    `json:"chunks"`
}

// Ingest chunks text, embeds each chunk, and stores it under source.
func (s *Service) Ingest(ctx context.Context, source, text string) (*IngestResult, error) {
	chunks := Chunk(text, s.chunkSize, s.chunkOverlap)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("nothing to ingest: text is empty")
	}

	vectors, err := s.embedder.Embed(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("embed chunks: %w", err)
	}

	docs := make([]Document, len(chunks))
	for i, c := range chunks {
		docs[i] = Document{
			ID:        chunkID(source, i, c),
			Source:    source,
			Content:   c,
			Embedding: vectors[i],
		}
	}
	if err := s.store.Add(ctx, docs); err != nil {
		return nil, fmt.Errorf("store chunks: %w", err)
	}
	return &IngestResult{Source: source, Chunks: len(chunks)}, nil
}

// QueryResult is the answer plus the sources it was grounded in.
type QueryResult struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
	Model   string   `json:"model"`
	Usage   llm.Usage `json:"usage"`
}

// Source is a retrieved chunk surfaced for citation/verification.
type Source struct {
	Source  string  `json:"source"`
	Score   float32 `json:"score"`
	Excerpt string  `json:"excerpt"`
}

// Query embeds the question, retrieves the most relevant chunks, augments the
// prompt with them, and asks the LLM to answer grounded in that context only.
func (s *Service) Query(ctx context.Context, question string, temperature *float64) (*QueryResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	if s.store.Len() == 0 {
		return nil, fmt.Errorf("knowledge base is empty: ingest documents first")
	}

	qVec, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}

	hits, err := s.store.Search(ctx, qVec[0], s.topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no relevant documents found")
	}

	prompt := buildPrompt(question, hits)
	resp, err := s.generator.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: prompt},
		},
		Temperature: temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
	}

	sources := make([]Source, len(hits))
	for i, h := range hits {
		sources[i] = Source{
			Source:  h.Document.Source,
			Score:   h.Score,
			Excerpt: excerpt(h.Document.Content, 200),
		}
	}
	return &QueryResult{
		Answer:  resp.Content,
		Sources: sources,
		Model:   resp.Model,
		Usage:   resp.Usage,
	}, nil
}

const systemPrompt = "You are a helpful assistant. Answer the user's question using ONLY the " +
	"provided context. If the context does not contain the answer, say you don't know — " +
	"do not invent facts. Cite the source name in brackets like [source] when you use it."

func buildPrompt(question string, hits []Result) string {
	var b strings.Builder
	b.WriteString("Context:\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", h.Document.Source, h.Document.Content)
	}
	b.WriteString("\n---\nQuestion: ")
	b.WriteString(question)
	b.WriteString("\n\nAnswer using only the context above, citing sources in [brackets]:")
	return b.String()
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func chunkID(source string, i int, content string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%s", source, i, content)))
	return hex.EncodeToString(h[:8])
}
