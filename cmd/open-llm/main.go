// Command open-llm is a small, provider-agnostic HTTP service that talks to
// open-source LLMs through an OpenAI-compatible API.
//
// It runs against a local Ollama server for development (free, no key) and
// against Groq (or any OpenAI-compatible provider) in the cloud — controlled
// entirely by environment variables. See README.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adiet95/open-llm/internal/agent"
	"github.com/adiet95/open-llm/internal/config"
	"github.com/adiet95/open-llm/internal/httpapi"
	"github.com/adiet95/open-llm/internal/llm"
	"github.com/adiet95/open-llm/internal/rag"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("service exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: cfg.RequestTimeout}
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model, httpClient)

	// Build the RAG pipeline: embeddings client + vector store + service.
	// Embeddings run against a separate (default: local Ollama) endpoint since
	// Groq has no embeddings API. If the store can't be opened, RAG is disabled
	// but the core chat service still runs.
	var ragSvc *rag.Service
	embedClient := &http.Client{Timeout: cfg.RequestTimeout}
	embedder := rag.NewOpenAIEmbedder(cfg.EmbedBaseURL, cfg.EmbedAPIKey, cfg.EmbedModel, embedClient)
	store, err := rag.NewMemoryStore(cfg.VectorStore)
	if err != nil {
		log.Warn("RAG disabled: could not open vector store", "err", err)
	} else {
		ragSvc = rag.NewService(embedder, store, client, cfg.ChunkSize, cfg.ChunkOverlap, cfg.RetrieveTopK)
		log.Info("RAG enabled",
			"embed_model", cfg.EmbedModel, "store", cfg.VectorStore, "docs", store.Len())
	}

	// Build a demo tool-calling agent (Phase 3). It ships with a safe
	// calculator tool and, when RAG is enabled, a knowledge_lookup tool that
	// reuses the RAG service — showing an agent orchestrating other capabilities.
	agentSvc := agent.New(client,
		"You are a helpful assistant. Use the provided tools when they help; "+
			"otherwise answer directly. Keep answers concise.", 5)
	agentSvc.Register("calculator",
		"Evaluate a simple arithmetic expression of two numbers.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a":  map[string]any{"type": "number"},
				"b":  map[string]any{"type": "number"},
				"op": map[string]any{"type": "string", "enum": []string{"+", "-", "*", "/"}},
			},
			"required": []string{"a", "b", "op"},
		},
		calculatorTool)
	if ragSvc != nil {
		agentSvc.Register("knowledge_lookup",
			"Look up an answer from the ingested knowledge base.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{"type": "string"},
				},
				"required": []string{"question"},
			},
			knowledgeLookupTool(ragSvc))
	}

	handler := httpapi.NewHandler(client, ragSvc, agentSvc, string(cfg.Provider), cfg.Model, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Run the server in a goroutine so main can wait for a shutdown signal.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("open-llm listening",
			"port", cfg.Port, "provider", cfg.Provider, "model", cfg.Model)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for either a fatal server error or an OS termination signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		log.Info("shutdown complete")
		return nil
	}
}


// calculatorTool evaluates a two-operand arithmetic expression. A tool handler
// receives raw JSON args and returns a string result fed back to the model.
func calculatorTool(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		A  float64 `json:"a"`
		B  float64 `json:"b"`
		Op string  `json:"op"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	var out float64
	switch in.Op {
	case "+":
		out = in.A + in.B
	case "-":
		out = in.A - in.B
	case "*":
		out = in.A * in.B
	case "/":
		if in.B == 0 {
			return "", fmt.Errorf("division by zero")
		}
		out = in.A / in.B
	default:
		return "", fmt.Errorf("unsupported op %q", in.Op)
	}
	return fmt.Sprintf("%g", out), nil
}

// knowledgeLookupTool adapts the RAG service into an agent tool, so the agent
// can answer factual questions from the ingested knowledge base.
func knowledgeLookupTool(svc *rag.Service) agent.ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		res, err := svc.Query(ctx, in.Question, nil)
		if err != nil {
			return "", err
		}
		return res.Answer, nil
	}
}
