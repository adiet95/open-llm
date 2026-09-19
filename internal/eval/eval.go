// Package eval is a tiny evaluation harness for LLM outputs (Phase 4).
//
// LLM output is non-deterministic, so you cannot assert exact equality like a
// normal unit test. Instead you run a labelled dataset through the system and
// SCORE each result, then report an aggregate accuracy — so you can answer
// "did quality go up or down after I changed the prompt/model?" with a number.
package eval

import (
	"context"
	"fmt"
	"strings"
)

// Case is one labelled example.
type Case struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

// RunFunc produces an output for an input (wrap /v1/chat, rag.Query, etc.).
type RunFunc func(ctx context.Context, input string) (string, error)

// Scorer returns true if got satisfies want. Swap in different strategies:
// exact match, substring, or an LLM-as-judge.
type Scorer func(want, got string) bool

// ExactMatch scores true only on identical (trimmed, case-insensitive) strings.
func ExactMatch(want, got string) bool {
	return strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got))
}

// Contains scores true if got contains want (trimmed, case-insensitive) — a
// forgiving default for free-form answers.
func Contains(want, got string) bool {
	return strings.Contains(
		strings.ToLower(strings.TrimSpace(got)),
		strings.ToLower(strings.TrimSpace(want)),
	)
}

// ItemResult is the outcome for one case.
type ItemResult struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
	Pass     bool   `json:"pass"`
	Err      string `json:"error,omitempty"`
}

// Report aggregates a run.
type Report struct {
	Total   int          `json:"total"`
	Passed  int          `json:"passed"`
	Results []ItemResult `json:"results"`
}

// Accuracy returns the pass rate in [0,1].
func (r Report) Accuracy() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Total)
}

// Run executes every case through run, scores it, and aggregates a Report.
func Run(ctx context.Context, cases []Case, run RunFunc, score Scorer) Report {
	rep := Report{Total: len(cases)}
	for _, c := range cases {
		item := ItemResult{Input: c.Input, Expected: c.Expected}
		got, err := run(ctx, c.Input)
		if err != nil {
			item.Err = err.Error()
			rep.Results = append(rep.Results, item)
			continue
		}
		item.Got = got
		item.Pass = score(c.Expected, got)
		if item.Pass {
			rep.Passed++
		}
		rep.Results = append(rep.Results, item)
	}
	return rep
}

// Summary is a one-line human-readable result.
func (r Report) Summary() string {
	return fmt.Sprintf("Accuracy: %.0f%% (%d/%d)", r.Accuracy()*100, r.Passed, r.Total)
}
