package memopt_test

import (
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/memopt"
)

// TestSmoke_OrphanBatch_RoundTrip exercises the full local protocol
// loop: render prompt → fake materializer reply → parse → validate.
// This stands in for issue #39's "smoke test with Claude Code using a
// small fixture contract" — without spending tokens. The loop CLI in
// issue #38 wires the same path through `claude -p`; this test pins
// the contract that wiring depends on.
func TestSmoke_OrphanBatch_RoundTrip(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")

	prompt, err := memopt.RenderPrompt(c)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	if !strings.Contains(prompt, c.ID) {
		t.Fatalf("prompt missing contract id %q", c.ID)
	}

	// Hand-built reply mimicking what a well-behaved materializer
	// would produce after fetching memory context for two of the four
	// nodes and assigning every selected node a disposition.
	reply := "I fetched context for the bot-rotation and reminder notes and now propose:\n\n" +
		"```json\n" + `{
  "contractId": "orphan-2026-05-06",
  "operations": [
    {"op": "regroup", "targetId": "mem:9d1a", "secondaryId": "mem:secrets-parent",
     "rationale": "fits secrets cluster (rotation, tokens)"},
    {"op": "regroup", "targetId": "mem:5d04", "secondaryId": "mem:reminders-parent",
     "rationale": "fits reminder API parent"},
    {"op": "annotate", "targetId": "mem:7b22"},
    {"op": "annotate", "targetId": "mem:3c81"}
  ],
  "assignmentValidation": [
    {"nodeId": "mem:9d1a", "outcome": "regrouped"},
    {"nodeId": "mem:7b22", "outcome": "annotated"},
    {"nodeId": "mem:3c81", "outcome": "annotated", "note": "kept as orphan, tagged for later"},
    {"nodeId": "mem:5d04", "outcome": "regrouped"}
  ],
  "groupingRationale": "Two nodes fit existing parent clusters (secrets, reminders). The other two are domain-distinct and best left annotated for a future pass.",
  "contextSourcesUsed": [
    {"id": "mem:secrets-parent",   "title": "Secrets parent",       "excerpt": "..."},
    {"id": "mem:reminders-parent", "title": "Reminders parent",     "excerpt": "..."},
    {"id": "mem:9d1a",             "title": "Service token rotation"},
    {"id": "mem:5d04",             "title": "Reminder API renames 2026-04"}
  ]
}` + "\n```\n"

	p, err := memopt.ParsePatch(reply)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if err := memopt.Validate(c, p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
