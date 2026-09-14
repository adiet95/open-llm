// Package config loads service configuration from environment variables.
//
// No secrets are hard-coded: the API key is always read from the environment,
// which satisfies the "credentials should not be hard-coded" rule.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Provider identifies which LLM backend the service talks to. This service
// targets Groq's OpenAI-compatible cloud API (free tier).
type Provider string

// ProviderGroq targets Groq's OpenAI-compatible cloud API (free tier).
const ProviderGroq Provider = "groq"

const (
	defaultPort           = "8080"
	defaultGroqBaseURL    = "https://api.groq.com/openai/v1"
	defaultGroqModel      = "openai/gpt-oss-20b"
	defaultRequestTimeout = 60 * time.Second
)

// Config holds all runtime settings, resolved from the environment.
type Config struct {
	Port           string
	Provider       Provider
	BaseURL        string
	APIKey         string
	Model          string
	RequestTimeout time.Duration
}

// Load reads configuration from the environment and applies sensible defaults.
// It returns an error when the required Groq API key is missing.
//
// For local development convenience it first loads a ".env" file (if present)
// into the environment, without overriding variables already set in the real
// environment. In production (Render, etc.) there is no .env file, so values
// come straight from the platform's environment.
func Load() (*Config, error) {
	if err := LoadDotEnv(".env"); err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:           getEnv("PORT", defaultPort),
		Provider:       ProviderGroq,
		BaseURL:        getEnv("LLM_BASE_URL", defaultGroqBaseURL),
		Model:          getEnv("LLM_MODEL", defaultGroqModel),
		APIKey:         os.Getenv("LLM_API_KEY"),
		RequestTimeout: getDurationEnv("LLM_REQUEST_TIMEOUT", defaultRequestTimeout),
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("provider %q requires LLM_API_KEY", cfg.Provider)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	// Accept either a plain seconds integer or a Go duration string.
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return fallback
}
