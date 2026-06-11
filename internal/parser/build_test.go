package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestSyntheticAllNodeAndEdgeKinds builds the graph for the
// `testdata/synthetic` fixture (which exercises every Stage 1 node and
// edge kind exactly once) and asserts each kind is present at least
// once. Table-driven so that adding a new kind to ROADMAP Stage 1
// becomes a one-line table edit plus a fixture line.
func TestSyntheticAllNodeAndEdgeKinds(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(Options{Dir: dir, Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, le := range res.LoadErrors {
		t.Logf("load error (non-fatal): %s", le)
	}
	g := res.Graph

	t.Run("nodes", func(t *testing.T) {
		cases := []struct {
			kind    mgraph.NodeKind
			wantMin int
			// findName, when non-empty, asserts at least one node of
			// this kind has Name == findName (helps catch
			// mis-categorisation).
			findName string
		}{
			{kind: mgraph.NodePackage, wantMin: 1, findName: "synthetic"},
			{kind: mgraph.NodeFile, wantMin: 1, findName: "synthetic.go"},
			{kind: mgraph.NodeType, wantMin: 2, findName: "Server"},
			{kind: mgraph.NodeFunction, wantMin: 2, findName: "Run"},
			{kind: mgraph.NodeMethod, wantMin: 1, findName: "Greet"},
			{kind: mgraph.NodeField, wantMin: 1, findName: "Counter"},
			{kind: mgraph.NodeLoop, wantMin: 1},
			{kind: mgraph.NodeBranch, wantMin: 1},
			{kind: mgraph.NodeGoroutine, wantMin: 1},
			{kind: mgraph.NodeDefer, wantMin: 1},
			{kind: mgraph.NodeChannelOp, wantMin: 3}, // send + recv + close
			{kind: mgraph.NodeSyncPrim, wantMin: 1},
		}
		for _, c := range cases {
			c := c
			t.Run(string(c.kind), func(t *testing.T) {
				ns := g.NodesByKind(c.kind)
				if len(ns) < c.wantMin {
					t.Fatalf("kind %s: got %d nodes, want >= %d", c.kind, len(ns), c.wantMin)
				}
				if c.findName != "" {
					found := false
					for _, n := range ns {
						if n.Name == c.findName {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("kind %s: no node named %q (have: %v)", c.kind, c.findName, namesOf(ns))
					}
				}
			})
		}
	})

	t.Run("edges", func(t *testing.T) {
		want := []mgraph.EdgeKind{
			mgraph.EdgeContains,
			mgraph.EdgeImplements,
			mgraph.EdgeEmbeds,
			mgraph.EdgeCalls,
			mgraph.EdgeCallsFrom,
			mgraph.EdgeReferences,
			mgraph.EdgeDependsOn,
			mgraph.EdgeReturns,
			mgraph.EdgeUsesType,
		}
		got := make(map[mgraph.EdgeKind]int)
		for _, e := range g.Edges() {
			got[e.Kind]++
		}
		for _, k := range want {
			if got[k] == 0 {
				t.Errorf("edge kind %s: not present (got kinds: %v)", k, got)
			}
		}
	})

	t.Run("implements_concrete_to_interface", func(t *testing.T) {
		// Server -[implements]-> Greeter
		var serverID, greeterID string
		for _, n := range g.NodesByKind(mgraph.NodeType) {
			switch n.Name {
			case "Server":
				serverID = n.ID
			case "Greeter":
				greeterID = n.ID
			}
		}
		if serverID == "" || greeterID == "" {
			t.Fatalf("missing Server (%q) or Greeter (%q)", serverID, greeterID)
		}
		found := false
		for _, e := range g.IncidentEdges(serverID, mgraph.DirectionOut, mgraph.EdgeImplements) {
			if e.To == greeterID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected Server -[implements]-> Greeter")
		}
	})

	t.Run("calls_from_branch_to_hello", func(t *testing.T) {
		// The Branch around `if i == 0 { Hello() }` should have a
		// CallsFrom edge to a Function node named Hello.
		branches := g.NodesByKind(mgraph.NodeBranch)
		if len(branches) == 0 {
			t.Fatal("no Branch nodes")
		}
		found := false
		for _, b := range branches {
			for _, n := range g.Neighbors(b.ID, mgraph.DirectionOut, mgraph.EdgeCallsFrom) {
				if n.Name == "Hello" {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("expected CallsFrom Branch->Hello; got branches=%v", namesOf(branches))
		}
	})

	t.Run("subgraph_extraction_and_pretty_print", func(t *testing.T) {
		// Pretty-print Stage 1 verify spec: pull a struct + its methods
		// + their callers.
		var serverID string
		for _, n := range g.NodesByKind(mgraph.NodeType) {
			if n.Name == "Server" {
				serverID = n.ID
				break
			}
		}
		if serverID == "" {
			t.Fatal("missing Server type")
		}
		sub := g.Subgraph([]string{serverID}, 2)
		if !sub.HasNode(serverID) {
			t.Fatal("subgraph missing seed")
		}
		// Should at least include Server's Greet method.
		methods := sub.NodesByKind(mgraph.NodeMethod)
		hasGreet := false
		for _, m := range methods {
			if m.Name == "Greet" {
				hasGreet = true
				break
			}
		}
		if !hasGreet {
			t.Fatalf("subgraph missing Server.Greet; methods=%v", namesOf(methods))
		}
		// Smoke-test the pretty-printer on the subgraph; this is what
		// the Stage 1 verify step calls "pretty-print sample of a small
		// subgraph (a struct + its methods + their callers)".
		var buf strings.Builder
		if err := mgraph.PrettyPrint(sub, &buf); err != nil {
			t.Fatalf("PrettyPrint: %v", err)
		}
		if !strings.Contains(buf.String(), "Server") || !strings.Contains(buf.String(), "Greet") {
			t.Fatalf("pretty-print output missing expected names:\n%s", buf.String())
		}
		t.Logf("Server subgraph pretty-print:\n%s", buf.String())
	})

	t.Run("ids_are_stable", func(t *testing.T) {
		// Building twice should produce identical node IDs.
		res2, err := Build(Options{Dir: dir, Patterns: []string{"./..."}})
		if err != nil {
			t.Fatal(err)
		}
		ids1 := nodeIDs(g.Nodes())
		ids2 := nodeIDs(res2.Graph.Nodes())
		if !equalSet(ids1, ids2) {
			t.Fatalf("node IDs differ between runs:\n  only in first: %v\n  only in second: %v",
				diff(ids1, ids2), diff(ids2, ids1))
		}
	})
}

func TestCrossPackageFieldDependsOnLoadedType(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "crosspkgfields"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(Options{Dir: dir, Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	g := res.Graph

	var itemID, fieldID string
	itemCount := 0
	for _, n := range g.Nodes() {
		switch n.QName {
		case "crosspkgfields/domain.Item":
			itemCount++
			itemID = n.ID
			if foreign, _ := n.Attrs["foreign"].(bool); foreign {
				t.Fatalf("domain.Item resolved as foreign placeholder: %+v", n)
			}
		case "crosspkgfields/api.Response.Items":
			fieldID = n.ID
		}
	}
	if itemCount != 1 {
		t.Fatalf("domain.Item node count = %d, want 1", itemCount)
	}
	if fieldID == "" {
		t.Fatal("missing Response.Items field node")
	}

	found := false
	for _, e := range g.IncidentEdges(fieldID, mgraph.DirectionOut, mgraph.EdgeDependsOn) {
		if e.To == itemID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Response.Items does not depend on local domain.Item")
	}
}

func TestBodyReferencesCallbacksAndTypes(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "bodyrefs"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(Options{Dir: dir, Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	g := res.Graph

	routes := findNodeByQName(t, g, "bodyrefs/httpapi.Routes")
	handle := findNodeByQName(t, g, "(bodyrefs/httpapi.handler).handle")
	request := findNodeByQName(t, g, "bodyrefs/api.Request")
	response := findNodeByQName(t, g, "bodyrefs/api.Response")

	if foreign, _ := handle.Attrs["foreign"].(bool); foreign {
		t.Fatalf("local callback resolved as foreign placeholder: %+v", handle)
	}
	if !hasEdge(g, routes.ID, handle.ID, mgraph.EdgeReferences) {
		t.Fatalf("Routes does not reference handler.handle as a callback")
	}
	if !hasEdge(g, handle.ID, request.ID, mgraph.EdgeUsesType) {
		t.Fatalf("handler.handle does not use api.Request")
	}
	if !hasEdge(g, handle.ID, response.ID, mgraph.EdgeUsesType) {
		t.Fatalf("handler.handle does not use api.Response")
	}
}

func namesOf(ns []mgraph.Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		name := n.Name
		if name == "" {
			name = n.ID
		}
		out = append(out, name)
	}
	return out
}

func findNodeByQName(t *testing.T, g *mgraph.Graph, qname string) mgraph.Node {
	t.Helper()
	for _, n := range g.Nodes() {
		if n.QName == qname {
			return n
		}
	}
	t.Fatalf("missing node qname %q", qname)
	return mgraph.Node{}
}

func hasEdge(g *mgraph.Graph, from, to string, kind mgraph.EdgeKind) bool {
	for _, e := range g.IncidentEdges(from, mgraph.DirectionOut, kind) {
		if e.To == to {
			return true
		}
	}
	return false
}

func nodeIDs(ns []mgraph.Node) map[string]struct{} {
	out := make(map[string]struct{}, len(ns))
	for _, n := range ns {
		out[n.ID] = struct{}{}
	}
	return out
}

func equalSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func diff(a, b map[string]struct{}) []string {
	out := make([]string, 0)
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// TestGraphSelf builds the typed graph for the archmotif repository
// itself and asserts a plausible size for the richer typed graph.
func TestGraphSelf(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	res, err := Build(Options{Dir: root})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	g := res.Graph
	t.Logf("archmotif self: nodes=%d edges=%d", g.NodeCount(), g.EdgeCount())
	if g.NodeCount() < 50 {
		t.Fatalf("too few nodes for archmotif self: %d", g.NodeCount())
	}
	if g.NodeCount() > 20000 {
		t.Fatalf("graph blew up: %d nodes", g.NodeCount())
	}
}

// TestGraphArchlint runs the parser against /tmp/archlint when present.
// Skipped otherwise so the test is portable to fresh CI environments.
func TestGraphArchlint(t *testing.T) {
	const dir = "/tmp/archlint"
	if !dirExists(dir) {
		t.Skipf("skipping: %s not present (clone with: gh repo clone kgatilin/archlint /tmp/archlint)", dir)
	}
	res, err := Build(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	g := res.Graph
	t.Logf("archlint: nodes=%d edges=%d", g.NodeCount(), g.EdgeCount())
	if g.NodeCount() < 100 {
		t.Fatalf("too few nodes for archlint: %d", g.NodeCount())
	}
}

func repoRoot() (string, error) {
	abs, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		if fileExists(filepath.Join(cur, "go.mod")) {
			return cur, nil
		}
		next := filepath.Dir(cur)
		if next == cur {
			return abs, nil
		}
		cur = next
	}
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func TestPackagePatternsExcludesNamedDirectorySegments(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "app", "app.go"))
	writeGoFile(t, filepath.Join(dir, "tests", "scenario", "scenario.go"))
	writeGoFile(t, filepath.Join(dir, "internal", "tests", "helper.go"))

	got, err := packagePatterns(dir, []string{"./..."}, []string{"tests"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./app"}
	if !equalStringSlices(got, want) {
		t.Fatalf("patterns = %#v, want %#v", got, want)
	}
}

func TestPackagePatternsExcludesRelativePrefix(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "internal", "app", "app.go"))
	writeGoFile(t, filepath.Join(dir, "internal", "generated", "gen.go"))
	writeGoFile(t, filepath.Join(dir, "tools", "generated", "tool.go"))

	got, err := packagePatterns(dir, []string{"./..."}, []string{"internal/generated"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./internal/app", "./tools/generated"}
	if !equalStringSlices(got, want) {
		t.Fatalf("patterns = %#v, want %#v", got, want)
	}
}

func writeGoFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
