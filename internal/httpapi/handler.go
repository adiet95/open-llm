// Package httpapi wires HTTP endpoints to the llm client.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/adiet95/open-llm/internal/llm"
)

// Handler holds dependencies shared across HTTP endpoints.
type Handler struct {
	client   *llm.Client
	provider string
	model    string
	log      *slog.Logger
}

// NewHandler builds a Handler.
func NewHandler(client *llm.Client, provider, model string, log *slog.Logger) *Handler {
	return &Handler{client: client, provider: provider, model: model, log: log}
}

// Routes returns the configured mux for the service.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /", h.root)
	mux.HandleFunc("POST /v1/chat", h.chat)
	mux.HandleFunc("POST /v1/chat/stream", h.chatStream)
	return mux
}

type chatRequestBody struct {
	Messages    []llm.Message `json:"messages"`
	Model       string        `json:"model,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

func (h *Handler) root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":  "open-llm",
		"provider": h.provider,
		"model":    h.model,
		"endpoints": []string{
			"GET  /health",
			"POST /v1/chat",
			"POST /v1/chat/stream",
		},
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
