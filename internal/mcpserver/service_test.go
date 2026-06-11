package mcpserver

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func mustService(t *testing.T, slug string) (*Service, string) {
	t.Helper()
	root := installFixture(t, slug)
	return NewService(root), root
}

func TestQueryFilters(t *testing.T) {
	svc, _ := mustService(t, "demo")
	cases := []struct {
		name   string
		filter QueryFilter
		wantID []string
	}{
		{
			name:   "all",
			filter: QueryFilter{},
			wantID: []string{"pkg:foo", "pkg:foo:bar", "pkg:foo:baz", "pkg:other"},
		},
		{
			name:   "by_kind",
			filter: QueryFilter{Kind: "function"},
			wantID: []string{"pkg:foo:bar", "pkg:foo:baz"},
		},
		{
			name:   "by_tag",
			filter: QueryFilter{Tag: "api"},
			wantID: []string{"pkg:foo:bar"},
		},
		{
			name:   "by_name_substr_case_insensitive",
			filter: QueryFilter{Name: "BA"},
			wantID: []string{"pkg:foo:bar", "pkg:foo:baz"},
		},
		{
			name:   "by_package",
			filter: QueryFilter{Package: "example/other"},
			wantID: []string{"pkg:other"},
		},
		{
			name:   "kind_and_name",
			filter: QueryFilter{Kind: "function", Name: "baz"},
			wantID: []string{"pkg:foo:baz"},
		},
		{
			name:   "no_match",
			filter: QueryFilter{Kind: "missing"},
			wantID: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := svc.Query("demo", tc.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			got := nodeIDs(nodes)
			if !reflect.DeepEqual(got, tc.wantID) {
				t.Fatalf("ids = %v, want %v", got, tc.wantID)
			}
		})
	}
}

func TestNeighborsEdgeKindsAndDepth(t *testing.T) {
	svc, _ := mustService(t, "demo")
	cases := []struct {
		name      string
		node      string
		edgeKinds []string
		depth     int
		want      []string
	}{
		{name: "depth0", node: "pkg:foo", depth: 0, want: []string{}},
		{name: "depth1_any", node: "pkg:foo", depth: 1, want: []string{"pkg:foo:bar", "pkg:foo:baz"}},
		{name: "depth1_calls", node: "pkg:foo:bar", edgeKinds: []string{"calls"}, depth: 1, want: []string{"pkg:foo:baz"}},
		{name: "depth1_contains_only", node: "pkg:foo", edgeKinds: []string{"contains"}, depth: 1, want: []string{"pkg:foo:bar", "pkg:foo:baz"}},
		{name: "depth2_any", node: "pkg:foo", depth: 2, want: []string{"pkg:foo:bar", "pkg:foo:baz", "pkg:other"}},
		{name: "depth1_no_match", node: "pkg:foo", edgeKinds: []string{"references"}, depth: 1, want: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := svc.Neighbors("demo", tc.node, tc.edgeKinds, tc.depth)
			if err != nil {
				t.Fatalf("Neighbors: %v", err)
			}
			got := nodeIDs(nodes)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNeighborsUnknownNode(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.Neighbors("demo", "missing", nil, 1); err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestPathHappyAndUnreachable(t *testing.T) {
	svc, _ := mustService(t, "demo")
	cases := []struct {
		name      string
		from, to  string
		edgeKinds []string
		want      []string
	}{
		{name: "self", from: "pkg:foo", to: "pkg:foo", want: []string{"pkg:foo"}},
		{name: "one_hop", from: "pkg:foo", to: "pkg:foo:bar", want: []string{"pkg:foo", "pkg:foo:bar"}},
		{name: "two_hops_via_calls", from: "pkg:foo:bar", to: "pkg:other", want: []string{"pkg:foo:bar", "pkg:foo:baz", "pkg:other"}},
		{name: "unreachable_reverse", from: "pkg:other", to: "pkg:foo", want: []string{}},
		{name: "filtered_unreachable", from: "pkg:foo:bar", to: "pkg:other", edgeKinds: []string{"contains"}, want: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, err := svc.Path("demo", tc.from, tc.to, tc.edgeKinds)
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			if !reflect.DeepEqual(path, tc.want) {
				t.Fatalf("path = %v, want %v", path, tc.want)
			}
		})
	}
}

func TestAddNodeLogsAndPersists(t *testing.T) {
	svc, root := mustService(t, "demo")
	res, err := svc.AddNode("demo", "memory", map[string]string{"name": "session-1", "weight": "0.5"})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	id, _ := res["id"].(string)
	if id != "memory:session-1" {
		t.Fatalf("derived id = %q, want memory:session-1", id)
	}

	// Query should now find it.
	nodes, err := svc.Query("demo", QueryFilter{Kind: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != id {
		t.Fatalf("Query did not return the new node: %+v", nodes)
	}

	// Mutation log entry exists with right shape.
	recs := readLog(t, root)
	if len(recs) != 1 {
		t.Fatalf("expected 1 mutation record, got %d", len(recs))
	}
	if recs[0].Tool != "graph_add_node" {
		t.Errorf("tool = %q", recs[0].Tool)
	}
	if recs[0].GraphID != "demo" {
		t.Errorf("graph_id = %q", recs[0].GraphID)
	}
	if recs[0].Args == nil || recs[0].Args["kind"] != "memory" {
		t.Errorf("args missing kind: %+v", recs[0].Args)
	}
	if recs[0].Result == nil || recs[0].Result["id"] != id {
		t.Errorf("result missing id: %+v", recs[0].Result)
	}
	if recs[0].Timestamp == "" {
		t.Error("timestamp empty")
	}
}

func TestAddEdgeLogsAndPersists(t *testing.T) {
	svc, root := mustService(t, "demo")
	if _, err := svc.AddEdge("demo", "pkg:foo", "pkg:other", "references", map[string]string{"reason": "import"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	// New path uses the new edge.
	path, err := svc.Path("demo", "pkg:foo", "pkg:other", []string{"references"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(path, []string{"pkg:foo", "pkg:other"}) {
		t.Fatalf("path via references = %v", path)
	}
	recs := readLog(t, root)
	if len(recs) != 1 || recs[0].Tool != "graph_add_edge" {
		t.Fatalf("log entry wrong: %+v", recs)
	}
}

func TestAddEdgeUnknownEndpoint(t *testing.T) {
	svc, root := mustService(t, "demo")
	if _, err := svc.AddEdge("demo", "pkg:foo", "missing", "references", nil); err == nil {
		t.Fatal("expected error")
	}
	// No log entry on failure.
	recs := readLog(t, root)
	if len(recs) != 0 {
		t.Fatalf("expected no log entries on failure, got %d", len(recs))
	}
}

func TestActivateAccumulates(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.Activate("demo", []string{"pkg:foo:bar"}, 0.5); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate("demo", []string{"pkg:foo:bar"}, 0.25); err != nil {
		t.Fatal(err)
	}
	nodes, err := svc.Query("demo", QueryFilter{Kind: "function", Name: "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if got := nodes[0].Attrs["activation"]; got != "0.75" {
		t.Fatalf("activation = %q, want 0.75", got)
	}
}

func TestUpdateWeightDelta(t *testing.T) {
	svc, root := mustService(t, "demo")
	if _, err := svc.UpdateWeight("demo", "pkg:foo:bar", 1.5); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateWeight("demo", "pkg:foo:bar", -0.5); err != nil {
		t.Fatal(err)
	}
	nodes, err := svc.Query("demo", QueryFilter{Kind: "function", Name: "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].Attrs["weight"]; got != "1" {
		t.Fatalf("weight = %q, want 1", got)
	}
	recs := readLog(t, root)
	if len(recs) != 2 {
		t.Fatalf("log entries = %d, want 2", len(recs))
	}
}

func TestUnknownGraphID(t *testing.T) {
	root := t.TempDir()
	svc := NewService(root)
	if _, err := svc.Query("nope", QueryFilter{}); err == nil {
		t.Fatal("expected error for unknown graph")
	}
}

// --- helpers --------------------------------------------------------------

func nodeIDs(ns []Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.ID)
	}
	sort.Strings(out)
	return out
}

func readLog(t *testing.T, root string) []MutationRecord {
	t.Helper()
	logger := NewMutationLogger(root)
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read log: %v", err)
	}
	var out []MutationRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec MutationRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}
