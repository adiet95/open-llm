package eval

import (
	"context"
	"strings"
	"testing"
)

func TestRunAccuracy(t *testing.T) {
	cases := []Case{
		{Input: "capital of France", Expected: "Paris"},
		{Input: "1+1", Expected: "2"},
		{Input: "color of sky", Expected: "blue"},
	}
	// Fake system: answers two correctly, one wrong.
	run := func(_ context.Context, in string) (string, error) {
		switch {
		case strings.Contains(in, "France"):
			return "The capital is Paris.", nil
		case in == "1+1":
			return "2", nil
		default:
			return "green", nil
		}
	}

	rep := Run(context.Background(), cases, run, Contains)
	if rep.Total != 3 {
		t.Fatalf("total = %d, want 3", rep.Total)
	}
	if rep.Passed != 2 {
		t.Errorf("passed = %d, want 2 (Paris + 2)", rep.Passed)
	}
	if got := rep.Accuracy(); got < 0.66 || got > 0.67 {
		t.Errorf("accuracy = %.2f, want ~0.67", got)
	}
}

func TestScorers(t *testing.T) {
	if !ExactMatch(" Paris ", "paris") {
		t.Error("ExactMatch should be trim+case-insensitive")
	}
	if ExactMatch("Paris", "Paris, France") {
		t.Error("ExactMatch should not match substrings")
	}
	if !Contains("Paris", "The capital is Paris.") {
		t.Error("Contains should match substring")
	}
}
