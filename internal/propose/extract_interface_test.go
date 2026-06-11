package propose_test

import (
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/propose/proposetest"
)

// TestExtractInterfaceRule_TriggerAndApply runs the three core cases
// pinned by issue #19's acceptance criteria:
//
//  1. motif × 3 + zero contracts → Proposal with 1 Iface + 3 Impls.
//  2. motif × 3 + one contract participant → no Proposal.
//  3. motif × 2 → no Proposal (below redundancy threshold).
//
// The fixtures are built in proposetest/. The hand-built Record drives
// the rule directly so the test does not depend on Stage 3's motif
// metric output (Stage 5's implementation issue wires that path; this
// issue only commits the spec + stub).
func TestExtractInterfaceRule_TriggerAndApply(t *testing.T) {
	rule := propose.ExtractInterfaceRule{}

	t.Run("motif_x3_no_contracts_emits_proposal", func(t *testing.T) {
		g, rec := proposetest.Triple(3, -1)
		if !rule.Trigger(rec, g) {
			t.Fatalf("Trigger returned false on motif×3 with no contracts; expected true")
		}
		prop, err := rule.Apply(g, rec)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if prop == nil {
			t.Fatal("Apply returned nil Proposal")
		}
		assertExtractInterfaceShape(t, prop, 3)
	})

	t.Run("motif_x3_one_contract_skips", func(t *testing.T) {
		g, rec := proposetest.Triple(3, 1)
		if rule.Trigger(rec, g) {
			t.Fatalf("Trigger returned true on motif×3 with a contract participant; expected false (per ADR-009)")
		}
	})

	t.Run("motif_x2_below_redundancy_threshold", func(t *testing.T) {
		g, rec := proposetest.Triple(2, -1)
		if rule.Trigger(rec, g) {
			t.Fatalf("Trigger returned true on motif×2 (below MinRedundancy=3); expected false")
		}
	})
}

// TestExtractInterfaceRule_RejectsNonMotifMetrics asserts the rule
// only fires for motif_redundancy region records — not graph-scope
// records, not other metrics.
func TestExtractInterfaceRule_RejectsNonMotifMetrics(t *testing.T) {
	rule := propose.ExtractInterfaceRule{}
	g, motifRec := proposetest.Triple(3, -1)

	cases := []struct {
		name string
		rec  metrics.Record
	}{
		{
			name: "wrong_metric_name",
			rec: metrics.Record{
				Metric: "cycle_rank",
				Scope:  metrics.ScopeRegion,
				Value:  3,
				Details: map[string]any{
					"size":      3,
					"instances": motifRec.Details["instances"],
				},
			},
		},
		{
			name: "wrong_scope",
			rec: metrics.Record{
				Metric:  "motif_redundancy",
				Scope:   metrics.ScopeGraph,
				Value:   3,
				Details: motifRec.Details,
			},
		},
		{
			name: "missing_size",
			rec: metrics.Record{
				Metric:  "motif_redundancy",
				Scope:   metrics.ScopeRegion,
				Value:   3,
				Details: map[string]any{"instances": motifRec.Details["instances"]},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if rule.Trigger(tc.rec, g) {
				t.Fatalf("Trigger returned true for %s; expected false", tc.name)
			}
		})
	}
}

// TestExtractInterfaceRule_CustomThresholds asserts the threshold
// fields take effect: a motif×2 rule fires when MinRedundancy is
// lowered to 2.
func TestExtractInterfaceRule_CustomThresholds(t *testing.T) {
	g, rec := proposetest.Triple(2, -1)
	rule := propose.ExtractInterfaceRule{MinRedundancy: 2}
	if !rule.Trigger(rec, g) {
		t.Fatalf("Trigger returned false with MinRedundancy=2 on motif×2; expected true")
	}
	prop, err := rule.Apply(g, rec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertExtractInterfaceShape(t, prop, 2)
}

// TestProposer_RoutesMotifRecordsToExtractInterface end-to-end-tests
// that the registry-driven Proposer picks up motif_redundancy records
// and produces extract-interface proposals.
func TestProposer_RoutesMotifRecordsToExtractInterface(t *testing.T) {
	g, rec := proposetest.Triple(3, -1)
	p := propose.NewProposerWith(propose.ExtractInterfaceRule{})
	res := p.ProposeFromRecords(g, []metrics.Record{rec})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(res.Proposals))
	}
	if got := res.Proposals[0].Trigger.Metric; got != "motif_redundancy" {
		t.Fatalf("trigger metric = %q, want motif_redundancy", got)
	}
	if !strings.HasPrefix(res.Proposals[0].ID, "extract_interface-") {
		t.Fatalf("proposal ID = %q, want extract_interface- prefix", res.Proposals[0].ID)
	}
}

// TestProposer_DedupesAnomaliesByGroup asserts that two anomalies
// pointing at the same motif group (Stage 4 emits one per instance,
// per ADR-021) collapse into a single proposal.
func TestProposer_DedupesAnomaliesByGroup(t *testing.T) {
	g, rec := proposetest.Triple(3, -1)
	// Two records sharing the same Target → the proposer's group dedup
	// keeps only one. (ProposeFromRecords wraps each as a zero-score
	// Anomaly; dedup keys are (Metric, SourceRecord.Target).)
	p := propose.NewProposerWith(propose.ExtractInterfaceRule{})
	res := p.ProposeFromRecords(g, []metrics.Record{rec, rec})
	if len(res.Proposals) != 1 {
		t.Fatalf("expected 1 proposal after dedup, got %d", len(res.Proposals))
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("expected 0 skipped (dedup, not conflict), got %d", len(res.Skipped))
	}
}

// TestRegistry_ExtractInterfaceRegistered asserts the v1 rule is in
// the package-level registry — the discipline check from ADR-019
// (mirrors metrics ADR-011's discipline check for new metrics).
func TestRegistry_ExtractInterfaceRegistered(t *testing.T) {
	r, ok := propose.Lookup("extract_interface")
	if !ok {
		t.Fatal("extract_interface not in registry")
	}
	if r.Name() != "extract_interface" {
		t.Fatalf("Name() = %q, want extract_interface", r.Name())
	}
	names := propose.Names()
	found := false
	for _, n := range names {
		if n == "extract_interface" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Names() = %v; missing extract_interface", names)
	}
}

// assertExtractInterfaceShape checks the canonical extract-interface
// TargetSubgraph shape: 3 roles (Iface×1, Impl×n, Method×n), 2 edge
// constraints (Implements, Contains), and n samples.
func assertExtractInterfaceShape(t *testing.T, prop *propose.Proposal, n int) {
	t.Helper()
	if len(prop.TargetSubgraph.Roles) != 3 {
		t.Fatalf("Roles count = %d, want 3 (Iface, Impl, Method); got %+v", len(prop.TargetSubgraph.Roles), prop.TargetSubgraph.Roles)
	}
	roles := map[string]propose.Role{}
	for _, r := range prop.TargetSubgraph.Roles {
		roles[r.Name] = r
	}
	if r, ok := roles["Iface"]; !ok || r.Cardinality != 1 || r.Kind != mgraph.NodeType {
		t.Fatalf("Iface role missing or wrong shape: %+v", r)
	}
	if r, ok := roles["Impl"]; !ok || r.Cardinality != n || r.Kind != mgraph.NodeType {
		t.Fatalf("Impl role missing or wrong shape (want cardinality=%d, kind=type): %+v", n, r)
	}
	if r, ok := roles["Method"]; !ok || r.Cardinality != n || r.Kind != mgraph.NodeMethod {
		t.Fatalf("Method role missing or wrong shape (want cardinality=%d, kind=method): %+v", n, r)
	}

	if len(prop.TargetSubgraph.Edges) != 2 {
		t.Fatalf("Edges count = %d, want 2 (Implements, Contains); got %+v", len(prop.TargetSubgraph.Edges), prop.TargetSubgraph.Edges)
	}
	hasImplements, hasContains := false, false
	for _, e := range prop.TargetSubgraph.Edges {
		if e.From == "Impl" && e.To == "Iface" && e.Kind == mgraph.EdgeImplements {
			hasImplements = true
		}
		if e.From == "Impl" && e.To == "Method" && e.Kind == mgraph.EdgeContains {
			hasContains = true
		}
	}
	if !hasImplements {
		t.Fatalf("missing Impl→Iface Implements edge in %+v", prop.TargetSubgraph.Edges)
	}
	if !hasContains {
		t.Fatalf("missing Impl→Method Contains edge in %+v", prop.TargetSubgraph.Edges)
	}

	if len(prop.Samples) != n {
		t.Fatalf("Samples count = %d, want %d (one per instance)", len(prop.Samples), n)
	}
}
