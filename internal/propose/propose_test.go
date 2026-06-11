package propose_test

import (
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/propose/proposetest"
)

// TestProposer_ScoreBasedConflictResolution asserts that when two
// proposals share at least one member node, the higher-scored
// proposal wins and the lower-scored one moves into Skipped (per
// ADR-022 §3).
//
// Setup: two motif Anomalies pointing at overlapping groups (the
// same Triple fixture, two different Targets so dedup doesn't
// collapse them). The first carries a low score, the second a high
// one. The high-score proposal must win.
func TestProposer_ScoreBasedConflictResolution(t *testing.T) {
	g, rec := proposetest.Triple(3, -1)
	rec1 := rec
	rec1.Target = "motif-low"
	rec2 := rec
	rec2.Target = "motif-high"

	a1 := wrapAsAnomaly(rec1, 4.0)
	a2 := wrapAsAnomaly(rec2, 9.0)

	p := propose.NewProposerWith(propose.ExtractInterfaceRule{})
	res := p.Propose(g, []anomalies.Anomaly{a1, a2})
	if len(res.Proposals) != 1 {
		t.Fatalf("expected 1 accepted proposal (highest score), got %d", len(res.Proposals))
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("expected 1 skipped proposal (lower score), got %d", len(res.Skipped))
	}
	if got := res.Proposals[0].Trigger.Target; got != "motif-high" {
		t.Fatalf("kept proposal target = %q, want motif-high (higher score)", got)
	}
	if got := res.Skipped[0].Trigger.Target; got != "motif-low" {
		t.Fatalf("skipped proposal target = %q, want motif-low (lower score)", got)
	}
}

// TestProposer_NonOverlappingProposalsBothKept asserts proposals on
// disjoint member sets both make it through, regardless of score.
// The two Triple fixtures use different pkg paths and impl IDs, so
// their member sets do not intersect.
func TestProposer_NonOverlappingProposalsBothKept(t *testing.T) {
	g, rec1, rec2 := proposetest.TwoDisjointTriples(3, 3)

	a1 := wrapAsAnomaly(rec1, 5.0)
	a2 := wrapAsAnomaly(rec2, 5.0)

	p := propose.NewProposerWith(propose.ExtractInterfaceRule{})
	res := p.Propose(g, []anomalies.Anomaly{a1, a2})
	if len(res.Proposals) != 2 {
		t.Fatalf("expected 2 accepted proposals (no overlap), got %d (skipped=%d)", len(res.Proposals), len(res.Skipped))
	}
}

// TestExtractInterfaceRule_AppliesRealRoleAssignment exercises the
// post-stub Apply: graph edges decide Impl/Method/Iface, not name
// heuristics. The Triple fixture has Impl→Iface (Implements) edges,
// Type→Method (Contains) edges, so all three roles must populate.
func TestExtractInterfaceRule_AppliesRealRoleAssignment(t *testing.T) {
	g, rec := proposetest.Triple(3, -1)
	rule := propose.ExtractInterfaceRule{}
	prop, err := rule.Apply(g, rec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if prop == nil {
		t.Fatal("Apply returned nil; expected a proposal on Triple(3, -1)")
	}
	for i, s := range prop.Samples {
		if s["Impl"] == "" {
			t.Fatalf("sample[%d] Impl unset; got %+v", i, s)
		}
		if s["Method"] == "" {
			t.Fatalf("sample[%d] Method unset; got %+v", i, s)
		}
		if s["Iface"] == "" {
			t.Fatalf("sample[%d] Iface unset; got %+v", i, s)
		}
		if s["MethodSignature"] == "" {
			t.Fatalf("sample[%d] MethodSignature unset; got %+v", i, s)
		}
	}
	// Iface should be the same across all samples (shared external
	// Reader type from the Triple fixture).
	iface := prop.Samples[0]["Iface"]
	for i, s := range prop.Samples {
		if s["Iface"] != iface {
			t.Fatalf("sample[%d] Iface = %q, want %q (shared)", i, s["Iface"], iface)
		}
	}
}

// wrapAsAnomaly is the test-side equivalent of the Stage 4 detector:
// it copies a metrics.Record into the Anomaly shape with the supplied
// Score, including the flat member list extracted from the record's
// instances details. The proposer's conflict-overlap check reads the
// member list, so it must populate accurately.
func wrapAsAnomaly(rec metrics.Record, score float64) anomalies.Anomaly {
	members := proposetest.MembersFromRecord(rec)
	return anomalies.Anomaly{
		Metric:   rec.Metric,
		Detector: rec.Metric,
		Score:    score,
		Region: anomalies.Region{
			Kind:    string(rec.Scope),
			Members: members,
		},
		SourceRecord: anomalies.SourceRecord{
			Scope:   string(rec.Scope),
			Target:  rec.Target,
			Value:   rec.Value,
			Details: rec.Details,
		},
	}
}
