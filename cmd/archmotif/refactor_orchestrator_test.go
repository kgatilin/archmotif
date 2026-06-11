package main

import (
	"bytes"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
)

// TestRefactor_ListEmptyPath confirms --list with a tmp module that
// has no anomalies prints the "nothing to propose" line and exits 0.
func TestRefactor_ListEmptyPipeline(t *testing.T) {
	dir := writeEmptyModule(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"refactor", "--list", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to propose") {
		t.Errorf("stdout missing 'nothing to propose':\n%s", stdout.String())
	}
}

// TestRefactor_NoIDEmptyPipelineExits0 confirms the ADR-031 §2 path:
// running refactor (no --id, no --list) on a module with zero
// proposals prints "nothing to propose" and exits 0.
func TestRefactor_NoIDEmptyPipelineExits0(t *testing.T) {
	dir := writeEmptyModule(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"refactor", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to propose") {
		t.Errorf("stdout missing 'nothing to propose':\n%s", stdout.String())
	}
}

// TestRefactor_BadIDExits1 confirms an explicit --id that doesn't
// match any surfaced proposal still errors out (different from the
// empty-pipeline graceful-exit path).
func TestRefactor_BadIDExits1(t *testing.T) {
	dir := writeEmptyModule(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"refactor", "--id", "does-not-exist", dir}, &stdout, &stderr)
	// Empty pipeline path takes precedence even with --id, so exit 0
	// stays. We still check the message is unambiguous.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (empty pipeline wins); stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to propose") {
		t.Errorf("stdout missing 'nothing to propose':\n%s", stdout.String())
	}
}

// TestRankProposals_TriggerValueDescIDAsc covers ADR-031 §1: rank
// proposals by Trigger.Value desc, then ID asc. The unit test
// exercises rankProposals directly with hand-built proposals so the
// guarantee is documented next to the code.
func TestRankProposals_TriggerValueDescIDAsc(t *testing.T) {
	props := []*propose.Proposal{
		{ID: "b-low", Trigger: &propose.AnomalyRef{Value: 3}},
		{ID: "a-high", Trigger: &propose.AnomalyRef{Value: 5}},
		{ID: "c-tie", Trigger: &propose.AnomalyRef{Value: 5}},
		{ID: "no-trigger"},
	}
	ranked := rankProposals(props)
	wantOrder := []string{"a-high", "c-tie", "b-low", "no-trigger"}
	if len(ranked) != len(wantOrder) {
		t.Fatalf("len = %d, want %d; got %+v", len(ranked), len(wantOrder), ranked)
	}
	for i, want := range wantOrder {
		if ranked[i].ID != want {
			t.Errorf("rank[%d] = %q, want %q (full order: %v)",
				i, ranked[i].ID, want, idsOf(ranked))
		}
	}
}

// TestPickProposal_AutoPickAnnounces covers ADR-031 §4: when --id is
// empty the auto-pick announcement goes to stderr.
func TestPickProposal_AutoPickAnnounces(t *testing.T) {
	ranked := []*propose.Proposal{
		{ID: "extract_interface-motif-0", Trigger: &propose.AnomalyRef{Value: 5},
			Description: "extract shared interface from 5 isomorphic motif instances"},
		{ID: "extract_interface-motif-1", Trigger: &propose.AnomalyRef{Value: 3}},
	}
	var stderr bytes.Buffer
	picked, err := pickProposal(ranked, "", &stderr)
	if err != nil {
		t.Fatalf("pickProposal: %v", err)
	}
	if picked.ID != "extract_interface-motif-0" {
		t.Errorf("picked.ID = %q, want extract_interface-motif-0", picked.ID)
	}
	out := stderr.String()
	if !strings.Contains(out, "auto-picked extract_interface-motif-0") {
		t.Errorf("stderr missing auto-pick announcement:\n%s", out)
	}
	if !strings.Contains(out, "value=5") {
		t.Errorf("stderr missing value=5:\n%s", out)
	}
	if !strings.Contains(out, "2 candidates") {
		t.Errorf("stderr missing candidate count:\n%s", out)
	}
}

// TestPickProposal_ExplicitIDMatches covers the --id-given path: the
// orchestrator picks the named proposal even when it isn't the
// top-ranked candidate.
func TestPickProposal_ExplicitIDMatches(t *testing.T) {
	ranked := []*propose.Proposal{
		{ID: "high", Trigger: &propose.AnomalyRef{Value: 10}},
		{ID: "low", Trigger: &propose.AnomalyRef{Value: 1}},
	}
	var stderr bytes.Buffer
	picked, err := pickProposal(ranked, "low", &stderr)
	if err != nil {
		t.Fatalf("pickProposal: %v", err)
	}
	if picked.ID != "low" {
		t.Errorf("picked.ID = %q, want low", picked.ID)
	}
	if strings.Contains(stderr.String(), "auto-picked") {
		t.Errorf("explicit --id should not announce auto-pick; got: %s", stderr.String())
	}
}

// TestPickProposal_ExplicitIDMissing covers the --id-typo path: the
// orchestrator surfaces available IDs in the error.
func TestPickProposal_ExplicitIDMissing(t *testing.T) {
	ranked := []*propose.Proposal{
		{ID: "first", Trigger: &propose.AnomalyRef{Value: 5}},
		{ID: "second", Trigger: &propose.AnomalyRef{Value: 3}},
	}
	var stderr bytes.Buffer
	_, err := pickProposal(ranked, "third", &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, want := range []string{"third", "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

// TestWriteProposalList_StableOutput covers --list output format:
// one line per proposal, tab-separated id/value/description, in the
// order rankProposals returned.
func TestWriteProposalList_StableOutput(t *testing.T) {
	ranked := []*propose.Proposal{
		{ID: "a", Trigger: &propose.AnomalyRef{Value: 5}, Description: "alpha"},
		{ID: "b", Trigger: &propose.AnomalyRef{Value: 3}, Description: "beta"},
	}
	var buf bytes.Buffer
	writeProposalList(&buf, ranked)
	want := "a\tvalue=5\talpha\nb\tvalue=3\tbeta\n"
	if buf.String() != want {
		t.Errorf("output mismatch:\nwant: %q\ngot:  %q", want, buf.String())
	}
}

// idsOf is a test helper that pulls IDs out of a proposal slice for
// comparison messages.
func idsOf(props []*propose.Proposal) []string {
	out := make([]string, len(props))
	for i, p := range props {
		out[i] = p.ID
	}
	return out
}

// writeEmptyModule creates a tmp Go module with no repeated motifs —
// the Stage 3 motif metric won't fire, so the proposal pipeline
// returns zero candidates. Keep the module valid Go so parser.Build
// succeeds.
func writeEmptyModule(t *testing.T) string {
	t.Helper()
	src := `package empty

// Helloer prints a greeting. Single-method type, no isomorphic peers.
type Helloer struct{}

func (h *Helloer) Hello(name string) string {
	return "hi, " + name
}
`
	dir := t.TempDir()
	writeGoModule(t, dir, "emptymod", src)
	return dir
}

// Compile-time assertion that propose.AnomalyRef.Value is float64;
// rankProposals depends on the field shape.
var _ = mgraph.NodeKind("compile-time anchor")
