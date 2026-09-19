// Package agent implements a minimal, guarded ReAct-style tool-calling loop
// on top of llm.Client (Phase 3):
//
//	LLM (with tools) -> tool_calls? -> execute registered tool -> feed result back -> repeat
//
// Guardrails: only registered tools can run (implicit allow-list), a hard step
// limit stops runaway loops, and tool arguments are passed to handlers as raw
// JSON for the handler to validate. This is deliberately small and readable —
// a reference for how agent orchestration actually works, in the standard library.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/adiet95/open-llm/internal/llm"
)

// ToolHandler executes a tool call. args is the raw JSON arguments from the
// model; the handler validates and returns a string result fed back to the LLM.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// registered pairs a tool schema with its handler.
type registered struct {
	spec    llm.Tool
	handler ToolHandler
}

// Agent runs a tool-calling loop against a set of registered tools.
type Agent struct {
	client   *llm.Client
	tools    map[string]registered
	maxSteps int
	system   string
}

// Chatter is the subset of llm.Client the agent needs (aids testing).
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// New builds an Agent. maxSteps caps how many tool round-trips are allowed
// before the loop stops (runaway guardrail); <=0 defaults to 5.
func New(client *llm.Client, systemPrompt string, maxSteps int) *Agent {
	if maxSteps <= 0 {
		maxSteps = 5
	}
	return &Agent{
		client:   client,
		tools:    make(map[string]registered),
		maxSteps: maxSteps,
		system:   systemPrompt,
	}
}

// Register adds a tool the model may call. name must match the schema's name.
func (a *Agent) Register(name, description string, parameters map[string]any, handler ToolHandler) {
	a.tools[name] = registered{
		spec: llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		},
		handler: handler,
	}
}

// Step records one action taken during a run (for transparency/debugging).
type Step struct {
	Tool   string `json:"tool"`
	Args   string `json:"args"`
	Result string `json:"result"`
	Err    string `json:"error,omitempty"`
}

// Result is the outcome of a run.
type Result struct {
	Answer string `json:"answer"`
	Steps  []Step `json:"steps"`
}

// Run executes the ReAct loop for a task until the model returns a final answer
// (no tool calls) or the step limit is hit.
func (a *Agent) Run(ctx context.Context, task string) (*Result, error) {
	specs := make([]llm.Tool, 0, len(a.tools))
	for _, t := range a.tools {
		specs = append(specs, t.spec)
	}

	messages := []llm.Message{{Role: llm.RoleUser, Content: task}}
	if a.system != "" {
		messages = append([]llm.Message{{Role: llm.RoleSystem, Content: a.system}}, messages...)
	}

	result := &Result{}
	for step := 0; step < a.maxSteps; step++ {
		resp, err := a.client.Chat(ctx, llm.ChatRequest{Messages: messages, Tools: specs})
		if err != nil {
			return nil, fmt.Errorf("agent step %d: %w", step, err)
		}

		// No tool calls => final answer.
		if len(resp.ToolCalls) == 0 {
			result.Answer = resp.Content
			return result, nil
		}

		// Record the assistant turn (with its tool calls) then answer each call.
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, call := range resp.ToolCalls {
			s := Step{Tool: call.Function.Name, Args: call.Function.Arguments}
			out, herr := a.dispatch(ctx, call)
			if herr != nil {
				s.Err = herr.Error()
				out = "error: " + herr.Error()
			}
			s.Result = out
			result.Steps = append(result.Steps, s)

			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    out,
			})
		}
	}

	// Step limit reached: ask once more without tools for a best-effort answer.
	resp, err := a.client.Chat(ctx, llm.ChatRequest{Messages: append(messages,
		llm.Message{Role: llm.RoleUser, Content: "Provide your best final answer now, without calling more tools."})})
	if err != nil {
		return nil, fmt.Errorf("agent final answer: %w", err)
	}
	result.Answer = resp.Content
	return result, nil
}

// dispatch runs the handler for a tool call, enforcing the allow-list (only
// registered tools) — a core guardrail.
func (a *Agent) dispatch(ctx context.Context, call llm.ToolCall) (string, error) {
	t, ok := a.tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("tool %q is not registered", call.Function.Name)
	}
	args := json.RawMessage(call.Function.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return t.handler(ctx, args)
}
