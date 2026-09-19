package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adiet95/open-llm/internal/llm"
)

// fakeChatter simulates the LLM: it returns a planning JSON when the request
// carries a json_schema response format, and otherwise routes worker/synthesizer
// calls to a supplied func. calls counts total Chat invocations (atomic so the
// -race detector stays clean under parallel workers).
type fakeChatter struct {
	planJSON string
	reply    func(req llm.ChatRequest) (string, error)
	calls    int64
}

func (f *fakeChatter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	atomic.AddInt64(&f.calls, 1)
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_schema" {
		return &llm.ChatResponse{Content: f.planJSON}, nil
	}
	out, err := f.reply(req)
	if err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Content: out}, nil
}

func plan(tasks ...string) string {
	b, _ := json.Marshal(struct {
		Subtasks []string `json:"subtasks"`
	}{Subtasks: tasks})
	return string(b)
}

func TestOrchestrator_Run(t *testing.T) {
	tests := []struct {
		name         string
		planJSON     string
		reply        func(req llm.ChatRequest) (string, error)
		maxTasks     int
		wantErr      bool
		wantSubtasks int
		wantAnswer   string
		checkFinding func(t *testing.T, fs []Finding)
	}{
		{
			name:     "happy_path_merges_findings",
			planJSON: plan("what is X", "what is Y"),
			reply: func(req llm.ChatRequest) (string, error) {
				last := req.Messages[len(req.Messages)-1].Content
				if strings.Contains(last, "Findings:") {
					return "final synthesized answer", nil
				}
				return "worker answer", nil
			},
			wantSubtasks: 2,
			wantAnswer:   "final synthesized answer",
		},
		{
			name:     "planner_cap_enforced",
			planJSON: plan("a", "b", "c", "d", "e", "f", "g"),
			reply: func(req llm.ChatRequest) (string, error) {
				return "ok", nil
			},
			maxTasks:     3,
			wantSubtasks: 3, // capped from 7
			wantAnswer:   "ok",
		},
		{
			name:     "empty_plan_returns_error",
			planJSON: plan(),
			reply:    func(req llm.ChatRequest) (string, error) { return "ok", nil },
			wantErr:  true,
		},
		{
			name:     "worker_failure_recorded_not_fatal",
			planJSON: plan("task ok", "task bad"),
			reply: func(req llm.ChatRequest) (string, error) {
				last := req.Messages[len(req.Messages)-1].Content
				switch {
				case strings.Contains(last, "Findings:"):
					return "synth despite gap", nil
				case strings.Contains(last, "task bad"):
					return "", errors.New("worker boom")
				default:
					return "good finding", nil
				}
			},
			wantSubtasks: 2,
			wantAnswer:   "synth despite gap",
			checkFinding: func(t *testing.T, fs []Finding) {
				if fs[0].Err != "" {
					t.Errorf("finding[0] should succeed, got err %q", fs[0].Err)
				}
				if fs[1].Err == "" {
					t.Errorf("finding[1] should record the worker error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeChatter{planJSON: tt.planJSON, reply: tt.reply}
			o := New(fc, tt.maxTasks, 3)

			res, err := o.Run(context.Background(), "some goal")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(res.Subtasks) != tt.wantSubtasks {
				t.Errorf("subtasks = %d, want %d", len(res.Subtasks), tt.wantSubtasks)
			}
			if res.Answer != tt.wantAnswer {
				t.Errorf("answer = %q, want %q", res.Answer, tt.wantAnswer)
			}
			if len(res.Findings) != tt.wantSubtasks {
				t.Errorf("findings = %d, want %d", len(res.Findings), tt.wantSubtasks)
			}
			if tt.checkFinding != nil {
				tt.checkFinding(t, res.Findings)
			}
		})
	}
}

func TestOrchestrator_DefaultsApplied(t *testing.T) {
	o := New(nil, 0, 0)
	if o.maxTasks != 5 {
		t.Errorf("maxTasks default = %d, want 5", o.maxTasks)
	}
	if o.concurrency != 3 {
		t.Errorf("concurrency default = %d, want 3", o.concurrency)
	}
}

// TestOrchestrator_FindingsOrdered verifies findings stay aligned to their
// subtask index even though workers run concurrently (per-index writes, no race).
func TestOrchestrator_FindingsOrdered(t *testing.T) {
	fc := &fakeChatter{
		planJSON: plan("q0", "q1", "q2", "q3"),
		reply: func(req llm.ChatRequest) (string, error) {
			last := req.Messages[len(req.Messages)-1].Content
			if strings.Contains(last, "Findings:") {
				return "synth", nil
			}
			// Echo which subtask this worker saw.
			for _, q := range []string{"q0", "q1", "q2", "q3"} {
				if strings.Contains(last, "Subtask: "+q) {
					return "ans-" + q, nil
				}
			}
			return "unknown", nil
		},
	}
	o := New(fc, 5, 2)
	res, err := o.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("Run() err = %v", err)
	}
	for i, f := range res.Findings {
		want := "ans-q" + string(rune('0'+i))
		if f.Answer != want {
			t.Errorf("finding[%d] = %q, want %q (order/race issue)", i, f.Answer, want)
		}
	}
}
