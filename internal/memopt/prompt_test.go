package memopt_test

import (
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/memopt"
)

// TestRenderPrompt_OrphanBatchFixture is the prompt fixture test for
// the orphan-bucket batch shape called out in issue #39 §Verification.
// It checks the load-bearing structural anchors a downstream reviewer
// (or smoke test against Claude) keys off.
func TestRenderPrompt_OrphanBatchFixture(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	out, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	for _, want := range []string{
		"You are restructuring a small batch",
		"id: orphan-2026-05-06",
		"kind: orphan_bucket",
		"forbidRemovals: true",
		"contextLimit: 8",
		"allowedOps: regroup, annotate",
		"- mem:9d1a :: Service token rotation",
		"- mem:5d04 :: Reminder API renames 2026-04",
		"MEMORY CONTEXT GATHERING",
		"bounded batches of at most 8",
		"contextSourcesUsed",
		"ASSIGNMENT VALIDATION",
		"GROUPING RATIONALE",
		"```json",
		"\"contractId\": \"orphan-2026-05-06\"",
		"FORBIDS removals",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("orphan-batch prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestRenderPrompt_FlatStarHubFixture is the second prompt fixture
// from issue #39 §Verification: a hub-and-spokes batch that allows the
// full operation set. Confirms operation enumeration adapts to
// contract.AllowedOps.
func TestRenderPrompt_FlatStarHubFixture(t *testing.T) {
	c := loadContract(t, "flat_star_hub.json")
	out, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	for _, want := range []string{
		"id: hub-notes-2026-05",
		"kind: flat_star_hub",
		"allowedOps: regroup, merge, retitle, annotate",
		"- mem:hub-001 :: Shared notes hub",
		"- mem:c-106 :: Note: worker capacity limits",
		"bounded batches of at most 12",
		"FORBIDS removals",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("flat-star prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestRenderPrompt_DefaultsContextLimit pins the default-batch-size
// behaviour for a contract that doesn't set ContextLimit. The
// orchestrator omitting the field must not silently produce an
// instruction that says "fetch in batches of 0".
func TestRenderPrompt_DefaultsContextLimit(t *testing.T) {
	c := &memopt.Contract{
		ID:   "min-1",
		Kind: "orphan_bucket",
		Selected: []memopt.Selection{
			{ID: "n1", Title: "n1"},
		},
		AllowedOps: []memopt.Operation{memopt.OpAnnotate},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	wantLimit := "bounded batches of at most 20"
	if !strings.Contains(out, wantLimit) {
		t.Errorf("expected %q in prompt; got\n%s", wantLimit, out)
	}
}

// TestRenderPrompt_Deterministic guards against accidental random
// ordering: rendering the same contract twice must produce identical
// bytes.
func TestRenderPrompt_Deterministic(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	a, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	b, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	if a != b {
		t.Fatalf("RenderPrompt is non-deterministic")
	}
}
