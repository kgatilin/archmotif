package localpartition

import (
	"math"
	"reflect"
	"sort"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// e is a terse WeightedEdge constructor for the fixtures below.
func e(from, to string, w float64) WeightedEdge {
	return WeightedEdge{From: from, To: to, Weight: w}
}

// region returns res.Region sorted, so tests compare against a literal.
func region(res Result) []string {
	got := append([]string(nil), res.Region...)
	sort.Strings(got)
	return got
}

// barbell is two triangles (A1A2A3, B1B2B3) joined by a single bridge A3–B1,
// every edge unweighted. Symmetric apart from which end a seed sits on, so it
// isolates the effect of seed weighting.
func barbell() []WeightedEdge {
	return []WeightedEdge{
		e("A1", "A2", 0), e("A1", "A3", 0), e("A2", "A3", 0),
		e("B1", "B2", 0), e("B1", "B3", 0), e("B2", "B3", 0),
		e("A3", "B1", 0),
	}
}

func TestLocalPartitionWeighted_SeedWeightsBiasTheRegion(t *testing.T) {
	tests := []struct {
		name  string
		seeds []Seed
		want  []string
	}{
		{
			name:  "heavy seed on the A side",
			seeds: []Seed{{Name: "A1", Weight: 9}, {Name: "B3", Weight: 1}},
			want:  []string{"A1", "A2", "A3"},
		},
		{
			name:  "heavy seed on the B side",
			seeds: []Seed{{Name: "A1", Weight: 1}, {Name: "B3", Weight: 9}},
			want:  []string{"B1", "B2", "B3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := LocalPartitionWeighted(barbell(), tt.seeds, DefaultOptions())
			if err != nil {
				t.Fatalf("LocalPartitionWeighted: %v", err)
			}
			if got := region(res); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("region = %v, want %v", got, tt.want)
			}
			if res.SeedCount != 2 {
				t.Errorf("SeedCount = %d, want 2", res.SeedCount)
			}
		})
	}
}

func TestLocalPartitionWeighted_UniformSeedsAreTheZeroValue(t *testing.T) {
	// Leaving Weight at zero must reproduce uniform seeding exactly, so a
	// caller with no relevance scores is unaffected by the personalization.
	explicit, err := LocalPartitionWeighted(barbell(),
		[]Seed{{Name: "A1", Weight: 4}, {Name: "B3", Weight: 4}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	zeroValue, err := LocalPartitionWeighted(barbell(),
		[]Seed{{Name: "A1"}, {Name: "B3"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if !reflect.DeepEqual(explicit, zeroValue) {
		t.Errorf("equal explicit weights = %+v, zero-value weights = %+v", explicit, zeroValue)
	}
}

func TestLocalPartitionWeighted_DuplicateSeedNamesSum(t *testing.T) {
	// Two sources voting for A1 must weigh the same as one source voting 2.
	summed, err := LocalPartitionWeighted(barbell(),
		[]Seed{{Name: "A1", Weight: 1}, {Name: "A1", Weight: 1}, {Name: "B3", Weight: 1}},
		DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	single, err := LocalPartitionWeighted(barbell(),
		[]Seed{{Name: "A1", Weight: 2}, {Name: "B3", Weight: 1}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if !reflect.DeepEqual(summed, single) {
		t.Errorf("duplicated seed = %+v, single seed of twice the weight = %+v", summed, single)
	}
	if summed.SeedCount != 2 {
		t.Errorf("SeedCount = %d, want 2 distinct names", summed.SeedCount)
	}
}

// forkedTriangles is a seed node S bridging two triangles, S–A1 and S–B1. The
// topology is fixed; only the two bridge weights change between cases.
func forkedTriangles(toA, toB float64) []WeightedEdge {
	return []WeightedEdge{
		e("A1", "A2", 1), e("A1", "A3", 1), e("A2", "A3", 1),
		e("B1", "B2", 1), e("B1", "B3", 1), e("B2", "B3", 1),
		e("S", "A1", toA), e("S", "B1", toB),
	}
}

func TestLocalPartitionWeighted_EdgeWeightsSteerDiffusion(t *testing.T) {
	tests := []struct {
		name       string
		toA, toB   float64
		want       []string
		wantWeight float64 // conductance
	}{
		{
			name: "heavy bridge into A",
			toA:  5, toB: 1,
			want: []string{"A1", "A2", "A3", "S"}, wantWeight: 1.0 / 7.0,
		},
		{
			name: "heavy bridge into B",
			toA:  1, toB: 5,
			want: []string{"B1", "B2", "B3", "S"}, wantWeight: 1.0 / 7.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := LocalPartitionWeighted(forkedTriangles(tt.toA, tt.toB),
				[]Seed{{Name: "S"}}, DefaultOptions())
			if err != nil {
				t.Fatalf("LocalPartitionWeighted: %v", err)
			}
			if got := region(res); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("region = %v, want %v", got, tt.want)
			}
			if math.Abs(res.Conductance-tt.wantWeight) > 1e-12 {
				t.Errorf("conductance = %v, want %v", res.Conductance, tt.wantWeight)
			}
		})
	}
}

// hubbedCluster builds the fixture the hub-damping case needs: a dense seed
// cluster (a 6-clique) whose members all touch a hub H, and a fringe of seven
// disjoint triangles, each attached to H by one vertex.
//
// The naive "hub between two equal triangles" graph cannot show anything: by
// symmetry the cut around the seed triangle and the cut around the hub weigh
// exactly the same, so the sweep ties at every tau and the tie-break decides.
// Pollution needs the asymmetry that makes hubs a problem in real dependency
// graphs — a dense cluster on one side and a scattered fringe on the other,
// with the hub's fringe degree small enough that swallowing it looks cheap.
//
// Undamped degrees: clique members 6, hub 13, fringe attachment points 3, the
// remaining fringe vertices 2.
func hubbedCluster() []WeightedEdge {
	clique := []string{"A1", "A2", "A3", "A4", "A5", "A6"}
	var edges []WeightedEdge
	for i := 0; i < len(clique); i++ {
		for j := i + 1; j < len(clique); j++ {
			edges = append(edges, e(clique[i], clique[j], 1))
		}
		edges = append(edges, e(clique[i], "H", 1))
	}
	for _, tri := range []string{"T1", "T2", "T3", "T4", "T5", "T6", "T7"} {
		a, b, c := tri+"a", tri+"b", tri+"c"
		edges = append(edges,
			e(a, b, 1), e(a, c, 1), e(b, c, 1),
			e("H", a, 1),
		)
	}
	return edges
}

func TestLocalPartitionWeighted_HubDampingSuppressesTheHub(t *testing.T) {
	seeds := []Seed{{Name: "A1"}}
	clique := []string{"A1", "A2", "A3", "A4", "A5", "A6"}
	cliqueAndHub := append(append([]string(nil), clique...), "H")
	sort.Strings(cliqueAndHub)

	tests := []struct {
		name string
		tau  float64
		want []string
	}{
		// vol(A)=36, cut(A)=6 -> 1/6; vol(A+H)=49, cut(A+H)=7 -> 1/7. The hub is
		// the cheaper cut, so the walk's region swallows it.
		{name: "undamped: the hub joins the region", tau: 0, want: cliqueAndHub},
		// tau=0.5 divides each weight by sqrt(d_u*d_v). The hub's own edges are
		// the most heavily discounted, which costs it far more volume than the
		// clique loses, and the cut around the clique becomes the cheaper one.
		{name: "damped: the hub is left out", tau: 0.5, want: clique},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.HubDamping = tt.tau
			res, err := LocalPartitionWeighted(hubbedCluster(), seeds, opts)
			if err != nil {
				t.Fatalf("LocalPartitionWeighted: %v", err)
			}
			if got := region(res); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("region = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalPartitionWeighted_HubDampingDrainsMassThroughTheHub(t *testing.T) {
	// Beyond the region, damping is meant to keep a hub from ferrying mass into
	// a neighbourhood that has nothing to do with the seeds. Measure the leak as
	// the fringe's ranking weight relative to the seed's.
	seeds := []Seed{{Name: "A1"}}
	leak := func(tau float64) float64 {
		opts := DefaultOptions()
		opts.HubDamping = tau
		res, err := LocalPartitionWeighted(hubbedCluster(), seeds, opts)
		if err != nil {
			t.Fatalf("LocalPartitionWeighted: %v", err)
		}
		if res.Weights["T1a"] == 0 {
			t.Fatalf("fringe node T1a was never reached at tau=%v", tau)
		}
		return res.Weights["T1a"] / res.Weights["A1"]
	}
	undamped, damped := leak(0), leak(0.5)
	if !(damped < undamped) {
		t.Errorf("leak past the hub: damped = %v, undamped = %v; damping must reduce it", damped, undamped)
	}
}

func TestLocalPartitionWeighted_ConductanceIsMeasuredInWeights(t *testing.T) {
	// A path A-B-C-D with weights 4, 2, 3. Weighted degrees are A=4, B=6, C=5,
	// D=3, so vol(V) = 18. The sweep walks A, B, C, D:
	//   {A}       cut 4, min(4, 14) = 4  -> 1
	//   {A,B}     cut 2, min(10, 8) = 8  -> 1/4   <- the minimum
	//   {A,B,C}   cut 3, min(15, 3) = 3  -> 1
	// Every weighted quantity is load-bearing in that 1/4: the cut is the
	// bridge's weight 2 (not one edge), the denominator is the complement's
	// weighted volume 8 (not the set's own 10, and not a count). Unweighted the
	// same path reports 1/3.
	edges := []WeightedEdge{
		e("A", "B", 4),
		e("B", "C", 2),
		e("C", "D", 3),
	}
	res, err := LocalPartitionWeighted(edges, []Seed{{Name: "A"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if got, want := region(res), []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("region = %v, want %v", got, want)
	}
	if math.Abs(res.Conductance-0.25) > 1e-12 {
		t.Errorf("conductance = %v, want %v", res.Conductance, 0.25)
	}
}

func TestLocalPartitionWeighted_ParallelAndOpposingEdgesSum(t *testing.T) {
	// Per ADR-012 the graph is undirected, and every directed weight between a
	// pair contributes: A->B 1, B->A 1 and a parallel A->B 1 make w(A,B)=3.
	summed, err := LocalPartitionWeighted([]WeightedEdge{
		e("A", "B", 1), e("B", "A", 1), e("A", "B", 1),
		e("B", "C", 1), e("C", "D", 3),
	}, []Seed{{Name: "A"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	single, err := LocalPartitionWeighted([]WeightedEdge{
		e("A", "B", 3),
		e("B", "C", 1), e("C", "D", 3),
	}, []Seed{{Name: "A"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if !reflect.DeepEqual(summed, single) {
		t.Errorf("three unit edges = %+v, one edge of weight 3 = %+v", summed, single)
	}
}

func TestLocalPartitionWeighted_SelfLoopsAreDropped(t *testing.T) {
	// A self-loop never crosses a cut, so it must not inflate a degree — but its
	// endpoint is still a node.
	withLoop, err := LocalPartitionWeighted([]WeightedEdge{
		e("A", "A", 5), e("A", "B", 1), e("B", "C", 1),
	}, []Seed{{Name: "A"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	without, err := LocalPartitionWeighted([]WeightedEdge{
		e("A", "B", 1), e("B", "C", 1),
	}, []Seed{{Name: "A"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if !reflect.DeepEqual(withLoop, without) {
		t.Errorf("with self-loop = %+v, without = %+v", withLoop, without)
	}

	// A lone self-loop still declares its node, which then behaves like any
	// isolated seed.
	lone, err := LocalPartitionWeighted([]WeightedEdge{e("Z", "Z", 1)},
		[]Seed{{Name: "Z"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if got, want := region(lone), []string{"Z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("region = %v, want %v", got, want)
	}
}

func TestLocalPartitionWeighted_DegenerateInputs(t *testing.T) {
	// A seed always creates its own node, so "the seed is missing from the
	// graph" cannot happen on this entrypoint. What can happen is a seed with no
	// incident edge: it holds its mass, cannot diffuse, and is its own region.
	tests := []struct {
		name      string
		edges     []WeightedEdge
		seeds     []Seed
		want      []string
		wantSeeds int
	}{
		{
			name:      "no edges at all",
			seeds:     []Seed{{Name: "solo"}},
			want:      []string{"solo"},
			wantSeeds: 1,
		},
		{
			name:      "seed disjoint from every edge",
			edges:     []WeightedEdge{e("A", "B", 1), e("B", "C", 1)},
			seeds:     []Seed{{Name: "Z"}},
			want:      []string{"Z"},
			wantSeeds: 1,
		},
		{
			name:      "no seeds",
			edges:     []WeightedEdge{e("A", "B", 1)},
			seeds:     nil,
			want:      nil,
			wantSeeds: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := LocalPartitionWeighted(tt.edges, tt.seeds, DefaultOptions())
			if err != nil {
				t.Fatalf("LocalPartitionWeighted: %v", err)
			}
			if got := region(res); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("region = %v, want %v", got, tt.want)
			}
			if res.Conductance != 0 {
				t.Errorf("conductance = %v, want 0", res.Conductance)
			}
			if res.SeedCount != tt.wantSeeds {
				t.Errorf("SeedCount = %d, want %d", res.SeedCount, tt.wantSeeds)
			}
			if res.Weights == nil {
				t.Error("Weights should be non-nil even when empty")
			}
		})
	}
}

func TestLocalPartitionWeighted_RejectsBadParams(t *testing.T) {
	edges := barbell()
	seeds := []Seed{{Name: "A1"}}
	tests := []struct {
		name string
		opts Options
	}{
		{name: "alpha zero", opts: Options{Alpha: 0, Epsilon: 1e-5}},
		{name: "alpha one", opts: Options{Alpha: 1, Epsilon: 1e-5}},
		{name: "epsilon zero", opts: Options{Alpha: 0.15, Epsilon: 0}},
		{name: "negative hub damping", opts: Options{Alpha: 0.15, Epsilon: 1e-5, HubDamping: -0.5}},
		{name: "NaN hub damping", opts: Options{Alpha: 0.15, Epsilon: 1e-5, HubDamping: math.NaN()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LocalPartitionWeighted(edges, seeds, tt.opts); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// buildTwoCliquesWithParallelEdges is buildTwoCliques with the A1–A2 pair
// stated three times: the original calls edge, a second edge of another kind in
// the same direction, and one in reverse.
func buildTwoCliquesWithParallelEdges() *Graph {
	g := buildTwoCliques()
	add := func(from, to string) {
		_, _ = g.AddEdge(mgraph.Edge{From: from, To: to, Kind: mgraph.EdgeReferences})
	}
	add("A1", "A2")
	add("A2", "A1")
	return g
}

func TestLocalPartition_ParallelEdgesStayCollapsed(t *testing.T) {
	// The typed entrypoint's degree has always been the simple-graph degree —
	// the number of distinct neighbours — so restating an edge must not change
	// anything. The weighted core sums parallel edges, so the adapter has to
	// collapse each pair before handing them over; without that, A1 and A2 would
	// each gain a degree and the sweep would move.
	plain, err := LocalPartition(buildTwoCliques(), []string{"A1"}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	parallel, err := LocalPartition(buildTwoCliquesWithParallelEdges(), []string{"A1"}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	if !reflect.DeepEqual(plain, parallel) {
		t.Errorf("with parallel edges = %+v, without = %+v", parallel, plain)
	}

	// Pin the absolute answer too, so a future change cannot move both sides
	// together: cut = 1 (the A3–B1 bridge), vol(A) = 7, vol(V\A) = 7.
	if got, want := region(plain), []string{"A1", "A2", "A3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("region = %v, want %v", got, want)
	}
	if math.Abs(plain.Conductance-1.0/7.0) > 1e-12 {
		t.Errorf("conductance = %v, want %v", plain.Conductance, 1.0/7.0)
	}
	if got := plain.Weights["A1"]; got == 0 {
		t.Error("seed A1 has no weight")
	}
}

func TestLocalPartition_MatchesTheWeightedEntrypoint(t *testing.T) {
	// The typed entrypoint is an adapter: same graph expressed as unit edges and
	// unit seeds must produce the identical Result, down to the last bit.
	typed, err := LocalPartition(buildTwoCliques(), []string{"A1"}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	weighted, err := LocalPartitionWeighted(barbell(), []Seed{{Name: "A1"}}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartitionWeighted: %v", err)
	}
	if !reflect.DeepEqual(typed, weighted) {
		t.Errorf("typed = %+v, weighted = %+v", typed, weighted)
	}
}
