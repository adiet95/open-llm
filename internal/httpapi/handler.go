// Package httpapi wires HTTP endpoints to the llm client.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/adiet95/open-llm/internal/agent"
	"github.com/adiet95/open-llm/internal/llm"
	"github.com/adiet95/open-llm/internal/rag"
)

// Handler holds dependencies shared across HTTP endpoints.
type Handler struct {
	client   *llm.Client
	rag      *rag.Service
	agent    *agent.Agent
	provider string
	model    string
	log      *slog.Logger
}

// NewHandler builds a Handler. ragSvc and agentSvc may be nil if disabled.
func NewHandler(client *llm.Client, ragSvc *rag.Service, agentSvc *agent.Agent, provider, model string, log *slog.Logger) *Handler {
	return &Handler{client: client, rag: ragSvc, agent: agentSvc, provider: provider, model: model, log: log}
}

// Routes returns the configured mux for the service, wrapped in metrics.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /", h.root)
	mux.HandleFunc("POST /v1/chat", h.chat)
	mux.HandleFunc("POST /v1/chat/stream", h.chatStream)
	mux.HandleFunc("POST /v1/extract", h.extract)
	mux.HandleFunc("POST /v1/rag/ingest", h.ragIngest)
	mux.HandleFunc("POST /v1/rag/query", h.ragQuery)
	mux.HandleFunc("POST /v1/agent", h.runAgent)
	return withMetrics(h.log, mux)
}

type chatRequestBody struct {
	Messages    []llm.Message `json:"messages"`
	Model       string        `json:"model,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

func (h *Handler) root(w http.ResponseWriter, _ *http.Request) {
	endpoints := []string{
		"GET  /health",
		"POST /v1/chat",
		"POST /v1/chat/stream",
		"POST /v1/extract",
	}
	if h.rag != nil {
		endpoints = append(endpoints, "POST /v1/rag/ingest", "POST /v1/rag/query")
	}
	if h.agent != nil {
		endpoints = append(endpoints, "POST /v1/agent")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":       "open-llm",
		"provider":      h.provider,
		"model":         h.model,
		"rag_enabled":   h.rag != nil,
		"agent_enabled": h.agent != nil,
		"endpoints":     endpoints,
	})
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// chat handles a non-streaming chat completion.
func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}

	resp, err := h.client.Chat(r.Context(), llm.ChatRequest{
		Messages:    body.Messages,
		Model:       body.Model,
		Temperature: body.Temperature,
		MaxTokens:   body.MaxTokens,
	})
	if err != nil {
		h.log.Error("chat failed", "err", err)
		status := http.StatusBadGateway
		if errors.Is(err, llm.ErrEmptyResponse) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, "chat completion failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// chatStream streams the completion back to the caller as newline-delimited
// text chunks (chunked transfer). Simple to consume with curl --no-buffer.
func (h *Handler) chatStream(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	err := h.client.ChatStream(r.Context(), llm.ChatRequest{
		Messages:    body.Messages,
		Model:       body.Model,
		Temperature: body.Temperature,
		MaxTokens:   body.MaxTokens,
	}, func(chunk string) error {
		if _, werr := w.Write([]byte(chunk)); werr != nil {
			return werr
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		// Headers already sent; log and end the stream. A client sees a
		// truncated body, which is the standard streaming failure mode.
		h.log.Error("chat stream failed", "err", err)
	}
}

// ragIngest adds a document to the knowledge base: chunk -> embed -> store.
func (h *Handler) ragIngest(w http.ResponseWriter, r *http.Request) {
	if h.rag == nil {
		writeError(w, http.StatusNotFound, "RAG is not enabled")
		return
	}
	var body struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Source == "" || body.Text == "" {
		writeError(w, http.StatusBadRequest, "both 'source' and 'text' are required")
		return
	}

	res, err := h.rag.Ingest(r.Context(), body.Source, body.Text)
	if err != nil {
		h.log.Error("rag ingest failed", "err", err)
		writeError(w, http.StatusBadGateway, "ingest failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ragQuery answers a question grounded in the ingested knowledge base.
func (h *Handler) ragQuery(w http.ResponseWriter, r *http.Request) {
	if h.rag == nil {
		writeError(w, http.StatusNotFound, "RAG is not enabled")
		return
	}
	var body struct {
		Question    string   `json:"question"`
		Temperature *float64 `json:"temperature,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Question == "" {
		writeError(w, http.StatusBadRequest, "'question' is required")
		return
	}

	res, err := h.rag.Query(r.Context(), body.Question, body.Temperature)
	if err != nil {
		h.log.Error("rag query failed", "err", err)
		writeError(w, http.StatusBadGateway, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// extract turns free text into structured JSON constrained by a caller-supplied
// JSON Schema (Phase 1: structured outputs). The raw model JSON is returned
// under "data"; it is valid by construction, so no regex/repair is needed.
func (h *Handler) extract(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text        string         `json:"text"`
		SchemaName  string         `json:"schema_name,omitempty"`
		Schema      map[string]any `json:"schema"`
		Instruction string         `json:"instruction,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Text == "" || len(body.Schema) == 0 {
		writeError(w, http.StatusBadRequest, "both 'text' and 'schema' are required")
		return
	}
	name := body.SchemaName
	if name == "" {
		name = "extraction"
	}

	raw, resp, err := h.client.Extract(r.Context(), body.Text, name, body.Schema, body.Instruction)
	if err != nil {
		h.log.Error("extract failed", "err", err)
		writeError(w, http.StatusBadGateway, "extraction failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  raw,
		"model": resp.Model,
		"usage": resp.Usage,
	})
}

// runAgent executes the tool-calling agent loop for a task (Phase 3).
func (h *Handler) runAgent(w http.ResponseWriter, r *http.Request) {
	if h.agent == nil {
		writeError(w, http.StatusNotFound, "agent is not enabled")
		return
	}
	var body struct {
		Task string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Task == "" {
		writeError(w, http.StatusBadRequest, "'task' is required")
		return
	}

	res, err := h.agent.Run(r.Context(), body.Task)
	if err != nil {
		h.log.Error("agent run failed", "err", err)
		writeError(w, http.StatusBadGateway, "agent run failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request) (chatRequestBody, bool) {
	var body chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return body, false
	}
	if len(body.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty")
		return body, false
	}
	return body, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
