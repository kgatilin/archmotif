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

func TestPPRDistributionSumsToOne(t *testing.T) {
	g := pprFixture(t)
	for _, seeds := range [][]string{nil, {"a"}, {"d"}, {"a", "c"}} {
		got := g.PersonalizedPageRankByNames(seeds, 0.15)
		if len(got) != 5 {
			t.Fatalf("seeds %v: got %d scores, want 5", seeds, len(got))
		}
		if s := sumScores(got); math.Abs(s-1.0) > 1e-6 {
			t.Errorf("seeds %v: scores sum to %v, want 1.0", seeds, s)
		}
	}
}

func TestPPRSeededRankingFavoursSeedAndItsReach(t *testing.T) {
	g := pprFixture(t)
	got := g.PersonalizedPageRankByNames([]string{"a"}, 0.15)

	// Ranking is sorted desc; the seed must lead.
	if got[0].Name != "a" {
		t.Errorf("top node = %q, want \"a\"", got[0].Name)
	}
	// d is only an in-neighbour of the seed (a does not reach d) and is not a
	// seed, so directed diffusion must leave it at ~0.
	byName := map[string]float64{}
	for _, s := range got {
		byName[s.Name] = s.Score
	}
	if byName["d"] > 1e-9 {
		t.Errorf("d score = %v, want ~0 (unreachable from seed a)", byName["d"])
	}
	if byName["a"] <= byName["b"] {
		t.Errorf("seed a (%v) should outrank b (%v)", byName["a"], byName["b"])
	}
}

func TestPPRDanglingSeedAbsorbsAllMass(t *testing.T) {
	g := pprFixture(t)
	// e is a pure sink (no outgoing edges): all mass teleports back to it.
	got := g.PersonalizedPageRankByNames([]string{"e"}, 0.15)
	if got[0].Name != "e" || math.Abs(got[0].Score-1.0) > 1e-6 {
		t.Errorf("dangling seed e: top = %+v, want e≈1.0", got[0])
	}
}

func TestPPRPersonalizedDiffersFromGlobal(t *testing.T) {
	g := pprFixture(t)
	global := g.PersonalizedPageRankByNames(nil, 0.15)
	seeded := g.PersonalizedPageRankByNames([]string{"a"}, 0.15)
	// Seeding at a must concentrate more mass on a than the global vector does.
	globalA, seededA := 0.0, 0.0
	for _, s := range global {
		if s.Name == "a" {
			globalA = s.Score
		}
	}
	for _, s := range seeded {
		if s.Name == "a" {
			seededA = s.Score
		}
	}
	if !(seededA > globalA) {
		t.Errorf("seeded a (%v) should exceed global a (%v)", seededA, globalA)
	}
}

func TestPPRDeterministic(t *testing.T) {
	g := pprFixture(t)
	a := g.PersonalizedPageRankByNames([]string{"a", "d"}, 0.2)
	b := g.PersonalizedPageRankByNames([]string{"a", "d"}, 0.2)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestPPRNonPositiveRestartUsesDefault(t *testing.T) {
	g := pprFixture(t)
	got := g.PersonalizedPageRankByNames([]string{"a"}, 0)
	if s := sumScores(got); math.Abs(s-1.0) > 1e-6 {
		t.Errorf("restart=0 (→ default): sum %v, want 1.0", s)
	}
}

func TestPPRUnknownSeedsFallBackToGlobal(t *testing.T) {
	g := pprFixture(t)
	unknown := g.PersonalizedPageRankByNames([]string{"nope"}, 0.15)
	global := g.PersonalizedPageRankByNames(nil, 0.15)
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
	if got := g.PersonalizedPageRank(nil, 0.15); got != nil {
		t.Errorf("empty graph PPR = %v, want nil", got)
	}
}
