package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// buildExtractInterfaceGraph wires the canonical extract-interface
// shape: an interface, a struct that implements it, a method on the
// struct realising the interface method, plus the contains/implements
// edges Stage 1 emits.
//
// The shape mirrors testdata/verify/motif-001/code/store.go.
func buildExtractInterfaceGraph(t *testing.T) *mgraph.Graph {
	t.Helper()
	g := mgraph.New()
	addNode(t, g, mgraph.Node{
		ID:   "pkg/store/store.go:5:6:type:UserStore",
		Kind: mgraph.NodeType,
		Name: "UserStore",
		Attrs: map[string]any{
			"typeKind": "interface",
		},
	})
	addNode(t, g, mgraph.Node{
		ID:   "pkg/store/store.go:9:6:type:SQLUserStore",
		Kind: mgraph.NodeType,
		Name: "SQLUserStore",
		Attrs: map[string]any{
			"typeKind": "struct",
		},
	})
	addNode(t, g, mgraph.Node{
		ID:   "pkg/store/store.go:11:1:method:Find",
		Kind: mgraph.NodeMethod,
		Name: "Find",
	})

	addEdge(t, g, mgraph.Edge{
		From: "pkg/store/store.go:9:6:type:SQLUserStore",
		To:   "pkg/store/store.go:5:6:type:UserStore",
		Kind: mgraph.EdgeImplements,
	})
	addEdge(t, g, mgraph.Edge{
		From: "pkg/store/store.go:9:6:type:SQLUserStore",
		To:   "pkg/store/store.go:11:1:method:Find",
		Kind: mgraph.EdgeContains,
	})
	return g
}

// extractInterfaceSkeleton is the target subgraph for the
// extract-interface motif. Mirrors testdata/verify/motif-001/skeleton.yaml.
func extractInterfaceSkeleton() Skeleton {
	return Skeleton{
		ProposalID: "motif-001",
		Roles: []Role{
			{ID: "Iface", Kind: mgraph.NodeType},
			{ID: "Impl", Kind: mgraph.NodeType},
			{ID: "Method", Kind: mgraph.NodeMethod, ReceiverRole: "Impl",
				Realises: &Realisation{Role: "Iface", Method: "Method"}},
		},
		Edges: []EdgeConstraint{
			{From: "Impl", To: "Iface", Kind: mgraph.EdgeImplements},
			{From: "Impl", To: "Method", Kind: mgraph.EdgeContains},
		},
	}
}

func TestVerify_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		graph       func(t *testing.T) *mgraph.Graph
		skeleton    Skeleton
		wantMatch   bool
		wantReason  string // substring search; empty == skip
		wantBound   map[string]string
		wantMissing []string // role IDs expected in Diff.MissingRoles
	}{
		{
			name:      "matching code returns Match with role mapping",
			graph:     buildExtractInterfaceGraph,
			skeleton:  extractInterfaceSkeleton(),
			wantMatch: true,
			wantBound: map[string]string{
				"Iface":  "UserStore",
				"Impl":   "SQLUserStore",
				"Method": "Find",
			},
		},
		{
			name: "missing role candidate returns Mismatch with role-named reason",
			graph: func(t *testing.T) *mgraph.Graph {
				// Drop the method entirely — no NodeMethod in graph.
				g := mgraph.New()
				addNode(t, g, mgraph.Node{
					ID:   "pkg/store/store.go:5:6:type:UserStore",
					Kind: mgraph.NodeType,
					Name: "UserStore",
				})
				addNode(t, g, mgraph.Node{
					ID:   "pkg/store/store.go:9:6:type:SQLUserStore",
					Kind: mgraph.NodeType,
					Name: "SQLUserStore",
				})
				addEdge(t, g, mgraph.Edge{
					From: "pkg/store/store.go:9:6:type:SQLUserStore",
					To:   "pkg/store/store.go:5:6:type:UserStore",
					Kind: mgraph.EdgeImplements,
				})
				return g
			},
			skeleton:    extractInterfaceSkeleton(),
			wantMatch:   false,
			wantReason:  "no candidate matching kind=method",
			wantMissing: []string{"Method"},
		},
		{
			name: "wrong edge kind returns Mismatch citing failing edge",
			graph: func(t *testing.T) *mgraph.Graph {
				// All three nodes present, but the Impl→Iface edge is
				// a CALLS, not an Implements. Verifier must reject.
				g := mgraph.New()
				addNode(t, g, mgraph.Node{
					ID:   "pkg/store/store.go:5:6:type:UserStore",
					Kind: mgraph.NodeType,
					Name: "UserStore",
				})
				addNode(t, g, mgraph.Node{
					ID:   "pkg/store/store.go:9:6:type:SQLUserStore",
					Kind: mgraph.NodeType,
					Name: "SQLUserStore",
				})
				addNode(t, g, mgraph.Node{
					ID:   "pkg/store/store.go:11:1:method:Find",
					Kind: mgraph.NodeMethod,
					Name: "Find",
				})
				addEdge(t, g, mgraph.Edge{
					From: "pkg/store/store.go:9:6:type:SQLUserStore",
					To:   "pkg/store/store.go:5:6:type:UserStore",
					Kind: mgraph.EdgeCalls, // wrong kind
				})
				addEdge(t, g, mgraph.Edge{
					From: "pkg/store/store.go:9:6:type:SQLUserStore",
					To:   "pkg/store/store.go:11:1:method:Find",
					Kind: mgraph.EdgeContains,
				})
				return g
			},
			skeleton:   extractInterfaceSkeleton(),
			wantMatch:  false,
			wantReason: "no role assignment satisfies all edge constraints",
		},
	}

	v := NewBacktrackVerifier()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := v.Verify(context.Background(), tc.skeleton, tc.graph(t))
			if res.Match != tc.wantMatch {
				t.Fatalf("Match=%v, want %v; diff=%+v", res.Match, tc.wantMatch, res.Diff)
			}
			if tc.wantMatch {
				for role, want := range tc.wantBound {
					got := res.Bindings[role]
					if got != want {
						t.Errorf("binding[%q]=%q, want %q", role, got, want)
					}
				}
				return
			}
			if res.Diff == nil {
				t.Fatalf("expected diff on mismatch, got nil")
			}
			if tc.wantReason != "" {
				combined := res.Diff.Reason
				for _, mr := range res.Diff.MissingRoles {
					combined += " " + mr.Reason
				}
				for _, fe := range res.Diff.FailingEdges {
					combined += " " + fe.Reason
				}
				if !strings.Contains(combined, tc.wantReason) {
					t.Errorf("diff reasons %q lack substring %q", combined, tc.wantReason)
				}
			}
			if len(tc.wantMissing) > 0 {
				gotIDs := make(map[string]bool, len(res.Diff.MissingRoles))
				for _, mr := range res.Diff.MissingRoles {
					gotIDs[mr.Role] = true
				}
				for _, want := range tc.wantMissing {
					if !gotIDs[want] {
						t.Errorf("missing role %q not reported; got %+v", want, res.Diff.MissingRoles)
					}
				}
			}
			// On a wrong-edge case we expect at least one failing
			// edge in the diff (FailingEdges). On missing-role we
			// expect MissingRoles. Sanity-check whichever is set.
			if len(tc.wantMissing) == 0 && len(res.Diff.FailingEdges) == 0 && len(res.Diff.MissingRoles) == 0 {
				t.Errorf("diff carries no diagnostics: %+v", res.Diff)
			}
		})
	}
}

func TestFormatText_Match(t *testing.T) {
	g := buildExtractInterfaceGraph(t)
	res := NewBacktrackVerifier().Verify(context.Background(), extractInterfaceSkeleton(), g)
	if !res.Match {
		t.Fatalf("expected Match, got Mismatch: %+v", res.Diff)
	}
	var buf bytes.Buffer
	if err := FormatText(&buf, "motif-001", res); err != nil {
		t.Fatalf("FormatText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Match on proposal motif-001", "<Iface> = UserStore", "<Impl> = SQLUserStore", "<Method> = Find"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q; got:\n%s", want, out)
		}
	}
}

func TestFormatJSON_Mismatch(t *testing.T) {
	skel := extractInterfaceSkeleton()
	g := mgraph.New() // empty — every role missing
	res := NewBacktrackVerifier().Verify(context.Background(), skel, g)
	if res.Match {
		t.Fatalf("expected Mismatch on empty graph")
	}
	var buf bytes.Buffer
	if err := FormatJSON(&buf, skel.ProposalID, res); err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\noutput:\n%s", err, buf.String())
	}
	if decoded["match"] != false {
		t.Errorf("decoded match=%v, want false", decoded["match"])
	}
	if decoded["proposal_id"] != "motif-001" {
		t.Errorf("decoded proposal_id=%v, want motif-001", decoded["proposal_id"])
	}
	if decoded["version"].(float64) != 1 {
		t.Errorf("decoded version=%v, want 1", decoded["version"])
	}
	if _, ok := decoded["diff"]; !ok {
		t.Errorf("decoded JSON missing diff section: %s", buf.String())
	}
}

func addNode(t *testing.T, g *mgraph.Graph, n mgraph.Node) {
	t.Helper()
	if _, added := g.AddNode(n); !added {
		t.Fatalf("AddNode(%s) reported merge; expected fresh insert", n.ID)
	}
}

func addEdge(t *testing.T, g *mgraph.Graph, e mgraph.Edge) {
	t.Helper()
	if _, err := g.AddEdge(e); err != nil {
		t.Fatalf("AddEdge(%v): %v", e, err)
	}
}
