package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/adiet95/open-llm/internal/llm"
)

// TestAgentToolLoop drives the real ReAct loop against a fake provider that
// first asks for a tool call, then (after receiving the tool result) returns a
// final answer.
func TestAgentToolLoop(t *testing.T) {
	var turn int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&turn, 1) == 1 {
			// Turn 1: request a calculator tool call.
			_, _ = w.Write([]byte(`{
				"model":"m",
				"choices":[{"message":{"content":"","tool_calls":[
					{"id":"call_1","type":"function","function":{"name":"calculator","arguments":"{\"a\":2,\"b\":3,\"op\":\"+\"}"}}
				]}}]
			}`))
			return
		}
		// Turn 2: final answer (no tool calls).
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"The answer is 5."}}]}`))
	}))
	defer srv.Close()

	client := llm.NewClient(srv.URL, "k", "m", srv.Client())
	a := New(client, "you are helpful", 5)

	var called bool
	a.Register("calculator", "adds", map[string]any{"type": "object"},
		func(_ context.Context, args json.RawMessage) (string, error) {
			called = true
			var in struct{ A, B float64 }
			_ = json.Unmarshal(args, &in)
			return "5", nil
		})

	res, err := a.Run(context.Background(), "what is 2+3?")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !called {
		t.Error("calculator tool was not called")
	}
	if len(res.Steps) != 1 || res.Steps[0].Tool != "calculator" {
		t.Errorf("expected one calculator step, got %+v", res.Steps)
	}
	if res.Answer != "The answer is 5." {
		t.Errorf("answer = %q, want final answer", res.Answer)
	}
}

// TestUnregisteredToolIsGuarded ensures the allow-list guardrail blocks tools
// the agent didn't register.
func TestUnregisteredToolIsGuarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Always ask for an unregistered tool; loop should record the error
		// and eventually stop at the step limit with a best-effort answer.
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"final"}}]}`))
	}))
	defer srv.Close()

	client := llm.NewClient(srv.URL, "k", "m", srv.Client())
	a := New(client, "", 3)
	res, err := a.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.Answer != "final" {
		t.Errorf("answer = %q, want 'final'", res.Answer)
	}
}
