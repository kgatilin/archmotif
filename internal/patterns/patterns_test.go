package patterns

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// addType inserts a typed Type node with foreign-ness annotation.
// foreign=true marks the type as living outside the analysed module.
func addType(t *testing.T, g *mgraph.Graph, id, name string, foreign bool) {
	t.Helper()
	_, _ = g.AddNode(mgraph.Node{
		ID:   id,
		Kind: mgraph.NodeType,
		Name: name,
		Attrs: map[string]any{
			"foreign": foreign,
		},
	})
}

// addEdge inserts a typed edge of the given kind. Test failures here
// fail the surrounding case so the fixture can't drift silently.
func addEdge(t *testing.T, g *mgraph.Graph, from, to string, kind mgraph.EdgeKind) {
	t.Helper()
	if _, err := g.AddEdge(mgraph.Edge{From: from, To: to, Kind: kind}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
}

// TestExternalNoiseSink_PositiveCase builds a synthetic graph in which
// one in-module Type ("Sink") has 6 inbound Calls edges, 5 of them
// from foreign callers. Pattern must report Mismatch with Sink as
// evidence.
func TestExternalNoiseSink_PositiveCase(t *testing.T) {
	g := mgraph.New()

	addType(t, g, "local:Sink", "Sink", false)
	addType(t, g, "local:Caller", "Caller", false)
	for i := 0; i < 5; i++ {
		fid := "foreign:Caller" + string(rune('A'+i))
		addType(t, g, fid, "ForeignCaller", true)
		addEdge(t, g, fid, "local:Sink", mgraph.EdgeCalls)
	}
	addEdge(t, g, "local:Caller", "local:Sink", mgraph.EdgeCalls)

	rep := ExternalNoiseSink{}.Run(g)

	if rep.Status != StatusMismatch {
		t.Fatalf("status: got %q, want %q", rep.Status, StatusMismatch)
	}
	if got, want := len(rep.EvidenceNodes), 1; got != want {
		t.Fatalf("evidence_nodes: got %d, want %d", got, want)
	}
	if rep.EvidenceNodes[0].ID != "local:Sink" {
		t.Errorf("evidence node: got %q, want local:Sink", rep.EvidenceNodes[0].ID)
	}
	if rep.Score < 0.7 {
		t.Errorf("score: got %.3f, want ≥ 0.70", rep.Score)
	}
	if len(rep.Violations) == 0 {
		t.Error("violations: want at least one")
	}
	if rep.Version == "" {
		t.Error("version must be set")
	}
}

// TestExternalNoiseSink_NegativeCase has a high-degree Sink whose
// inbound edges all come from in-module callers. Pattern must report
// Match.
func TestExternalNoiseSink_NegativeCase(t *testing.T) {
	g := mgraph.New()

	addType(t, g, "local:Sink", "Sink", false)
	for i := 0; i < 6; i++ {
		cid := "local:Caller" + string(rune('A'+i))
		addType(t, g, cid, "Caller", false)
		addEdge(t, g, cid, "local:Sink", mgraph.EdgeCalls)
	}
	// One foreign edge — far below the 70 % threshold.
	addType(t, g, "foreign:OneOff", "OneOff", true)
	addEdge(t, g, "foreign:OneOff", "local:Sink", mgraph.EdgeCalls)

	rep := ExternalNoiseSink{}.Run(g)

	if rep.Status != StatusMatch {
		t.Fatalf("status: got %q, want %q", rep.Status, StatusMatch)
	}
	if rep.Score >= 0.7 {
		t.Errorf("score: got %.3f, expected below threshold 0.70", rep.Score)
	}
	if len(rep.Violations) != 0 {
		t.Errorf("violations: got %d, want 0", len(rep.Violations))
	}
}

// TestExternalNoiseSink_BelowDegreeFloor verifies that types with too
// few inbound edges are ignored even if every edge is foreign.
func TestExternalNoiseSink_BelowDegreeFloor(t *testing.T) {
	g := mgraph.New()
	addType(t, g, "local:Tiny", "Tiny", false)
	for i := 0; i < 2; i++ {
		fid := "foreign:F" + string(rune('A'+i))
		addType(t, g, fid, "Foreign", true)
		addEdge(t, g, fid, "local:Tiny", mgraph.EdgeCalls)
	}
	rep := ExternalNoiseSink{}.Run(g)
	if rep.Status != StatusMatch {
		t.Fatalf("status: got %q, want %q (no candidates)", rep.Status, StatusMatch)
	}
	if got := rep.Metrics["candidate_count"]; got != 0 {
		t.Errorf("candidate_count: got %v, want 0", got)
	}
}

// TestRegistry_HasV1Catalog enforces the V1 catalogue: every pattern
// the issue lists must be registered, even those returning
// NotApplicable today.
func TestRegistry_HasV1Catalog(t *testing.T) {
	want := []string{"domain_core", "external_noise_sink", "forbidden_role_edges"}
	for _, id := range want {
		if _, ok := Lookup(id); !ok {
			t.Errorf("pattern %q is not registered", id)
		}
	}
}

// TestDomainCore_NotApplicableUntilRoles verifies the stub returns
// NotApplicable so the CLI surface stays stable until #28 lands.
func TestDomainCore_NotApplicableUntilRoles(t *testing.T) {
	rep := DomainCore{}.Run(mgraph.New())
	if rep.Status != StatusNotApplicable {
		t.Fatalf("status: got %q, want %q", rep.Status, StatusNotApplicable)
	}
	if rep.Reason == "" {
		t.Error("reason: must explain the missing prerequisite")
	}
}

// TestForbiddenRoleEdges_NotApplicableUntilRoles verifies the stub.
func TestForbiddenRoleEdges_NotApplicableUntilRoles(t *testing.T) {
	rep := ForbiddenRoleEdges{}.Run(mgraph.New())
	if rep.Status != StatusNotApplicable {
		t.Fatalf("status: got %q, want %q", rep.Status, StatusNotApplicable)
	}
}

// TestRun_AggregatesAndCounts verifies the runner sorts reports by ID
// and returns accurate status counts.
func TestRun_AggregatesAndCounts(t *testing.T) {
	g := mgraph.New()
	res := Run(g, nil)
	ids := make([]string, 0, len(res.Reports))
	for _, r := range res.Reports {
		ids = append(ids, r.ID)
	}
	if len(ids) < 3 {
		t.Fatalf("Run returned %d reports, want ≥ 3", len(ids))
	}
	if !sortedAscending(ids) {
		t.Errorf("reports not sorted by ID: %v", ids)
	}
	counts := res.StatusCounts()
	// On an empty graph: external_noise_sink → match (no candidates),
	// domain_core / forbidden_role_edges → not_applicable.
	if counts[StatusNotApplicable] < 2 {
		t.Errorf("not_applicable count: got %d, want ≥ 2", counts[StatusNotApplicable])
	}
}

// TestRun_UnknownPatternIDProducesNotApplicable checks the
// missingPattern fallback path.
func TestRun_UnknownPatternIDProducesNotApplicable(t *testing.T) {
	res := Run(mgraph.New(), []string{"definitely_not_a_pattern"})
	if len(res.Reports) != 1 {
		t.Fatalf("reports: got %d, want 1", len(res.Reports))
	}
	if res.Reports[0].Status != StatusNotApplicable {
		t.Errorf("status: got %q, want %q", res.Reports[0].Status, StatusNotApplicable)
	}
}

// TestWriteJSON_RoundTrip verifies the JSON envelope shape.
func TestWriteJSON_RoundTrip(t *testing.T) {
	res := Run(mgraph.New(), []string{"external_noise_sink"})
	var buf bytes.Buffer
	if err := res.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var env JSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if env.Version != CurrentJSONVersion {
		t.Errorf("version: got %d, want %d", env.Version, CurrentJSONVersion)
	}
	if len(env.Reports) != 1 {
		t.Errorf("reports: got %d, want 1", len(env.Reports))
	}
	if env.Counts.Match+env.Counts.NearMatch+env.Counts.Mismatch+env.Counts.NotApplicable != len(env.Reports) {
		t.Errorf("counts don't sum to report total: %+v", env.Counts)
	}
}

// TestWriteText_ContainsKeyFields ensures the text format mentions
// status and pattern ID for at least one report.
func TestWriteText_ContainsKeyFields(t *testing.T) {
	g := mgraph.New()
	addType(t, g, "local:Sink", "Sink", false)
	for i := 0; i < 5; i++ {
		fid := "foreign:F" + string(rune('A'+i))
		addType(t, g, fid, "Foreign", true)
		addEdge(t, g, fid, "local:Sink", mgraph.EdgeCalls)
	}
	res := Run(g, []string{"external_noise_sink"})
	var buf bytes.Buffer
	if err := res.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"external_noise_sink", "mismatch", "Sink"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--\n%s", want, out)
		}
	}
}

func sortedAscending(xs []string) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			return false
		}
	}
	return true
}
