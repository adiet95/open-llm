// Package orchestrator implements a minimal multi-agent research pipeline
// (Phase 5): a single high-level goal is decomposed into independent subtasks
// by a PLANNER, each subtask is answered concurrently by a WORKER, and the
// worker findings are merged into one grounded answer by a SYNTHESIZER.
//
//	goal -> planner (structured output: []subtask)
//	     -> workers  (run in parallel, one Chat each, bounded)
//	     -> synthesizer (single Chat over all findings) -> answer
//
// It is deliberately small and stdlib-only: a readable reference for how
// planner/worker/synthesizer orchestration actually works. Guardrails: the
// number of subtasks is capped (maxTasks) and worker concurrency is bounded so
// a single request cannot fan out without limit.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/adiet95/open-llm/internal/llm"
)

// Chatter is the subset of llm.Client the orchestrator needs. Defining it as an
// interface keeps the package decoupled from llm and makes it testable without
// a real provider.
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// Orchestrator runs the planner -> workers -> synthesizer pipeline.
type Orchestrator struct {
	client      Chatter
	maxTasks    int
	concurrency int
}

// New builds an Orchestrator. maxTasks caps how many subtasks the planner may
// produce (<=0 defaults to 5); concurrency bounds parallel workers (<=0
// defaults to 3). Both are guardrails against unbounded fan-out.
func New(client Chatter, maxTasks, concurrency int) *Orchestrator {
	if maxTasks <= 0 {
		maxTasks = 5
	}
	if concurrency <= 0 {
		concurrency = 3
	}
	return &Orchestrator{client: client, maxTasks: maxTasks, concurrency: concurrency}
}

// Finding is one worker's answer to one subtask.
type Finding struct {
	Task   string `json:"task"`
	Answer string `json:"answer"`
	Err    string `json:"error,omitempty"`
}

// Result is the outcome of a research run.
type Result struct {
	Goal     string    `json:"goal"`
	Subtasks []string  `json:"subtasks"`
	Findings []Finding `json:"findings"`
	Answer   string    `json:"answer"`
}

// planSchema is the JSON Schema the planner's output must conform to, so we get
// a clean list of subtasks (structured output) instead of parsing free text.
var planSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"subtasks": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	},
	"required": []string{"subtasks"},
}

// Run executes the full pipeline for a goal.
func (o *Orchestrator) Run(ctx context.Context, goal string) (*Result, error) {
	subtasks, err := o.plan(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	findings := o.work(ctx, goal, subtasks)

	answer, err := o.synthesize(ctx, goal, findings)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}

	return &Result{Goal: goal, Subtasks: subtasks, Findings: findings, Answer: answer}, nil
}

// plan asks the model to decompose the goal into independent subtasks, using a
// strict JSON schema so the result is machine-readable. The list is capped at
// maxTasks (guardrail).
func (o *Orchestrator) plan(ctx context.Context, goal string) ([]string, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are a research planner. Break the user's goal into a small set of " +
			"independent, specific subtasks (questions) that can each be researched on their own. " +
			"Return between 2 and " + fmt.Sprint(o.maxTasks) + " subtasks. Do not answer them."},
		{Role: llm.RoleUser, Content: goal},
	}
	resp, err := o.client.Chat(ctx, llm.ChatRequest{
		Messages:       messages,
		ResponseFormat: llm.NewJSONSchemaFormat("research_plan", planSchema),
	})
	if err != nil {
		return nil, err
	}

	var plan struct {
		Subtasks []string `json:"subtasks"`
	}
	if uerr := json.Unmarshal([]byte(resp.Content), &plan); uerr != nil {
		return nil, fmt.Errorf("decode plan: %w", uerr)
	}

	tasks := make([]string, 0, len(plan.Subtasks))
	for _, t := range plan.Subtasks {
		if s := strings.TrimSpace(t); s != "" {
			tasks = append(tasks, s)
		}
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("planner returned no subtasks")
	}
	if len(tasks) > o.maxTasks {
		tasks = tasks[:o.maxTasks] // enforce cap
	}
	return tasks, nil
}

// work runs one worker per subtask concurrently, bounded by o.concurrency.
// Findings are returned in subtask order (a per-index slice avoids a data race
// without locking around the results).
func (o *Orchestrator) work(ctx context.Context, goal string, subtasks []string) []Finding {
	findings := make([]Finding, len(subtasks))
	sem := make(chan struct{}, o.concurrency)

	var wg sync.WaitGroup
	wg.Add(len(subtasks))
	for i, task := range subtasks {
		go func(i int, task string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire slot (bounds concurrency)
			defer func() { <-sem }() // release slot

			findings[i] = Finding{Task: task}
			answer, err := o.answerOne(ctx, goal, task)
			if err != nil {
				findings[i].Err = err.Error()
				return
			}
			findings[i].Answer = answer
		}(i, task)
	}
	wg.Wait()
	return findings
}

// answerOne runs a single worker: answer one subtask in the context of the goal.
func (o *Orchestrator) answerOne(ctx context.Context, goal, task string) (string, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are a focused research worker. Answer the given subtask concisely and " +
			"factually. It is part of a larger goal shown for context; answer only the subtask."},
		{Role: llm.RoleUser, Content: "Overall goal: " + goal + "\n\nSubtask: " + task},
	}
	resp, err := o.client.Chat(ctx, llm.ChatRequest{Messages: messages})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// synthesize merges all worker findings into one coherent answer to the goal.
// Failed subtasks are included as notes so the synthesizer can account for gaps.
func (o *Orchestrator) synthesize(ctx context.Context, goal string, findings []Finding) (string, error) {
	var b strings.Builder
	for i, f := range findings {
		fmt.Fprintf(&b, "## Subtask %d: %s\n", i+1, f.Task)
		if f.Err != "" {
			fmt.Fprintf(&b, "(failed: %s)\n\n", f.Err)
			continue
		}
		b.WriteString(f.Answer)
		b.WriteString("\n\n")
	}

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are a research synthesizer. Using only the subtask findings provided, " +
			"write a single, well-structured answer to the overall goal. Note any gaps where a subtask failed."},
		{Role: llm.RoleUser, Content: "Overall goal: " + goal + "\n\nFindings:\n" + b.String()},
	}
	resp, err := o.client.Chat(ctx, llm.ChatRequest{Messages: messages})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
