package graphval

import (
	"math"
	"testing"
)

// pprFixture builds the canonical test graph used across the PPR tests:
// a 3-cycle a→b→c→a, with d→a feeding into the cycle and a→e a pure sink.
func pprFixture(t *testing.T) *Graph {
	t.Helper()
	g, err := New(
		[]Node{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}},
		[]Edge{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
			{From: "c", To: "a"},
			{From: "d", To: "a"},
			{From: "a", To: "e"},
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

func sumScores(s []Score) float64 {
	total := 0.0
	for _, x := range s {
		total += x.Score
	}
	return total
}

func scoreByName(s []Score) map[string]float64 {
	m := make(map[string]float64, len(s))
	for _, x := range s {
		m[x.Name] = x.Score
	}
	return m
}

func TestPPRDistributionSumsToOne(t *testing.T) {
	g := pprFixture(t)
	for _, seeds := range [][]string{nil, {"a"}, {"d"}, {"a", "c"}} {
		for _, undirected := range []bool{false, true} {
			got := g.PersonalizedPageRankByNames(seeds, 0.15, undirected)
			if len(got) != 5 {
				t.Fatalf("seeds %v und=%v: got %d scores, want 5", seeds, undirected, len(got))
			}
			if s := sumScores(got); math.Abs(s-1.0) > 1e-6 {
				t.Errorf("seeds %v und=%v: scores sum to %v, want 1.0", seeds, undirected, s)
			}
		}
	}
}

func TestPPRSeededRankingFavoursSeedAndItsReach(t *testing.T) {
	g := pprFixture(t)
	got := g.PersonalizedPageRankByNames([]string{"a"}, 0.15, false)

	if got[0].Name != "a" {
		t.Errorf("top node = %q, want \"a\"", got[0].Name)
	}
	// d is only an in-neighbour of the seed (a does not reach d) and is not a
	// seed, so directed diffusion must leave it at ~0.
	byName := scoreByName(got)
	if byName["d"] > 1e-9 {
		t.Errorf("d score = %v, want ~0 (unreachable from seed a, directed)", byName["d"])
	}
	if byName["a"] <= byName["b"] {
		t.Errorf("seed a (%v) should outrank b (%v)", byName["a"], byName["b"])
	}
}

func TestPPRUndirectedReachesInboundNeighbour(t *testing.T) {
	g := pprFixture(t)
	directed := scoreByName(g.PersonalizedPageRankByNames([]string{"a"}, 0.15, false))
	undirected := scoreByName(g.PersonalizedPageRankByNames([]string{"a"}, 0.15, true))

	// d reaches a only via d→a. Directed diffusion from a never touches d;
	// undirected diffusion walks a→d and gives it mass. This is the whole point
	// of the flag (inbound-port neighbours become reachable).
	if directed["d"] > 1e-9 {
		t.Errorf("directed d = %v, want ~0", directed["d"])
	}
	if undirected["d"] <= 1e-6 {
		t.Errorf("undirected d = %v, want > 0 (reachable via symmetrised edge)", undirected["d"])
	}
}

func TestPPRDanglingSeedAbsorbsAllMass(t *testing.T) {
	g := pprFixture(t)
	// e is a pure sink (no outgoing edges): all mass teleports back to it.
	got := g.PersonalizedPageRankByNames([]string{"e"}, 0.15, false)
	if got[0].Name != "e" || math.Abs(got[0].Score-1.0) > 1e-6 {
		t.Errorf("dangling seed e: top = %+v, want e≈1.0", got[0])
	}
}

func TestPPRPersonalizedDiffersFromGlobal(t *testing.T) {
	g := pprFixture(t)
	global := scoreByName(g.PersonalizedPageRankByNames(nil, 0.15, false))
	seeded := scoreByName(g.PersonalizedPageRankByNames([]string{"a"}, 0.15, false))
	if !(seeded["a"] > global["a"]) {
		t.Errorf("seeded a (%v) should exceed global a (%v)", seeded["a"], global["a"])
	}
}

func TestPPRDeterministic(t *testing.T) {
	g := pprFixture(t)
	a := g.PersonalizedPageRankByNames([]string{"a", "d"}, 0.2, true)
	b := g.PersonalizedPageRankByNames([]string{"a", "d"}, 0.2, true)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestPPRNonPositiveRestartUsesDefault(t *testing.T) {
	g := pprFixture(t)
	got := g.PersonalizedPageRankByNames([]string{"a"}, 0, false)
	if s := sumScores(got); math.Abs(s-1.0) > 1e-6 {
		t.Errorf("restart=0 (→ default): sum %v, want 1.0", s)
	}
}

func TestPPRUnknownSeedsFallBackToGlobal(t *testing.T) {
	g := pprFixture(t)
	unknown := g.PersonalizedPageRankByNames([]string{"nope"}, 0.15, false)
	global := g.PersonalizedPageRankByNames(nil, 0.15, false)
	for i := range global {
		if math.Abs(unknown[i].Score-global[i].Score) > 1e-12 {
			t.Fatalf("unknown-seed result diverged from global at %d", i)
		}
	}
}

func TestPPREmptyGraph(t *testing.T) {
	g, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New empty: %v", err)
	}
	if got := g.PersonalizedPageRank(nil, 0.15, false); got != nil {
		t.Errorf("empty graph PPR = %v, want nil", got)
	}
}
