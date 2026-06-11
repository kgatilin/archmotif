package llm_test

import (
	"math"
	"testing"

	"github.com/kgatilin/archmotif/internal/llm"
)

// TestCost_KnownModels checks that Cost agrees with the published
// per-1M-token rates on round numbers. Sonnet at 1M in / 1M out should
// be $3 + $15 = $18; Opus should be $15 + $75 = $90. These numbers are
// load-bearing for the usage.jsonl observability described in
// ADR-017.
func TestCost_KnownModels(t *testing.T) {
	cases := []struct {
		model string
		in    int64
		out   int64
		want  float64
	}{
		{llm.ModelSonnet, 1_000_000, 1_000_000, 18.0},
		{llm.ModelSonnet, 0, 0, 0.0},
		{llm.ModelOpus, 1_000_000, 1_000_000, 90.0},
		{llm.ModelOpus, 500_000, 0, 7.5},
		{llm.ModelSonnet, 0, 1_000_000, 15.0},
	}
	for _, tc := range cases {
		got := llm.Cost(tc.model, tc.in, tc.out)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Cost(%q, %d, %d) = %v, want %v", tc.model, tc.in, tc.out, got, tc.want)
		}
	}
}

// TestCost_UnknownModelReturnsZero documents the "unknown model = 0"
// branch from ADR-017: an unrecognised identifier produces a zero so
// the orchestrator can flag a warning rather than blow up. Stage 7
// adds the warning; this test pins the contract.
func TestCost_UnknownModelReturnsZero(t *testing.T) {
	if got := llm.Cost("gpt-4o", 1_000_000, 1_000_000); got != 0 {
		t.Errorf("Cost(unknown) = %v, want 0", got)
	}
}

// TestCost_NegativeTokensClamped documents the clamping behaviour:
// a buggy provider report should still produce a usage line, not a
// panic or a negative cost.
func TestCost_NegativeTokensClamped(t *testing.T) {
	if got := llm.Cost(llm.ModelSonnet, -5, -10); got != 0 {
		t.Errorf("Cost(neg, neg) = %v, want 0", got)
	}
	if got := llm.Cost(llm.ModelSonnet, -5, 1_000_000); got != 15.0 {
		t.Errorf("Cost(neg, 1M) = %v, want 15.0", got)
	}
}
