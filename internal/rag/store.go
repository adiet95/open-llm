package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Document is one stored chunk plus its embedding and provenance metadata.
type Document struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`  // where the chunk came from (filename, title)
	Content   string    `json:"content"` // the chunk text
	Embedding []float32 `json:"embedding"`
}

// Result is a retrieved document with its similarity score (1.0 = identical).
type Result struct {
	Document Document `json:"document"`
	Score    float32  `json:"score"`
}

// Store is the retrieval interface. The JSON-backed MemoryStore implements it;
// a pgvector-backed store could implement the same interface later.
type Store interface {
	Add(ctx context.Context, docs []Document) error
	Search(ctx context.Context, query []float32, topK int) ([]Result, error)
	Len() int
}

// MemoryStore is a thread-safe in-process cosine index persisted to a JSON file.
// Suitable for small/medium corpora; for large-scale use swap in pgvector.
type MemoryStore struct {
	mu   sync.RWMutex
	path string
	docs []Document
}

// NewMemoryStore loads an existing store from path if present, else starts empty.
func NewMemoryStore(path string) (*MemoryStore, error) {
	s := &MemoryStore{path: path}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // fresh store
		}
		return nil, fmt.Errorf("read vector store %q: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.docs); err != nil {
		return nil, fmt.Errorf("decode vector store %q: %w", path, err)
	}
	return s, nil
}

// Add appends documents and persists the store atomically.
func (s *MemoryStore) Add(_ context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	s.mu.Lock()
	s.docs = append(s.docs, docs...)
	snapshot := make([]Document, len(s.docs))
	copy(snapshot, s.docs)
	s.mu.Unlock()

	return s.persist(snapshot)
}

// Search returns the topK documents most similar to query by cosine similarity.
func (s *MemoryStore) Search(_ context.Context, query []float32, topK int) ([]Result, error) {
	if topK <= 0 {
		topK = 4
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.docs) == 0 {
		return nil, nil
	}

	results := make([]Result, 0, len(s.docs))
	for _, d := range s.docs {
		results = append(results, Result{Document: d, Score: cosine(query, d.Embedding)})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// Len reports how many documents are stored.
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// persist writes the store to disk atomically (temp file + rename).
func (s *MemoryStore) persist(docs []Document) error {
	if s.path == "" {
		return nil
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create vector store dir: %w", err)
		}
	}
	data, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("encode vector store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write vector store temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("commit vector store: %w", err)
	}
	return nil
}

// cosine returns the cosine similarity of a and b in [-1, 1]; 0 if either is
// zero-length or their dimensions differ.
func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
