// Command open-llm is a small, provider-agnostic HTTP service that talks to
// open-source LLMs through an OpenAI-compatible API.
//
// It runs against a local Ollama server for development (free, no key) and
// against Groq (or any OpenAI-compatible provider) in the cloud — controlled
// entirely by environment variables. See README.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adiet95/open-llm/internal/config"
	"github.com/adiet95/open-llm/internal/httpapi"
	"github.com/adiet95/open-llm/internal/llm"
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
	handler := httpapi.NewHandler(client, string(cfg.Provider), cfg.Model, log)

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
