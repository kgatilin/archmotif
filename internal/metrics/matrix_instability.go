package metrics

import (
	"sort"

	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// instability_matrix computes Robert Martin's instability metric
//
//	I = fanOut / (fanIn + fanOut)
//
// per node using only matrix-derived row/col sums of the adjacency:
//
//	fanOut = A · 𝟙       (row sum)
//	fanIn  = Aᵀ · 𝟙       (col sum)
//
// Both products go through `mat.Mul`, so the operation stays in
// matrix-algebra land — no per-cell hand loops outside the operation
// itself. The validator also encodes a layer-incidence matrix L during
// the Encode step; L isn't fed to the operation (RowColSumOp only needs
// A) but it documents the rule-encoding layer, and the interpreter uses
// node roles (read from the graph directly) to produce per-layer
// aggregates.

// instabilityEncoder builds the N×K layer-incidence matrix L.
// L[i][p] = 1 iff node ids[i] has role == AllPackageRoles()[p].
// Nodes without a resolved role contribute an all-zero row.
type instabilityEncoder struct{}

// Name returns "instability_matrix".
func (instabilityEncoder) Name() string { return "instability_matrix" }

// Encode returns the N×K layer-incidence matrix L.
func (instabilityEncoder) Encode(g *mgraph.Graph, ids []string, _ any) (*mat.Dense, error) {
	roles := mgraph.AllPackageRoles()
	idxByRole := make(map[mgraph.Role]int, len(roles))
	for i, r := range roles {
		idxByRole[r] = i
	}
	n := len(ids)
	L := mat.NewDense(n, len(roles), nil)
	for i, id := range ids {
		node, ok := g.Node(id)
		if !ok {
			continue
		}
		role := node.Role()
		if role == "" {
			continue
		}
		col, ok := idxByRole[role]
		if !ok {
			continue
		}
		L.Set(i, col, 1)
	}
	return L, nil
}

// instabilityInterpreter scans the N×2 (fanOut, fanIn) matrix and emits
// per-node, per-layer, and graph-scope records.
type instabilityInterpreter struct{}

// Interpret produces:
//   - one ScopeNode record per node with non-zero fan-in+fan-out, value I.
//   - one ScopeRegion record per architectural layer with the layer's
//     fanOut-weighted instability (Σ fanOut / Σ (fanIn+fanOut) over the layer).
//   - one ScopeGraph record with the mean instability over scoring nodes.
func (instabilityInterpreter) Interpret(V *mat.Dense, ids []string, g *mgraph.Graph) []Record {
	n, _ := V.Dims()
	out := []Record{}
	type layerAgg struct {
		out, in float64
		count   int
	}
	byLayer := map[mgraph.Role]*layerAgg{}
	totalI := 0.0
	scored := 0
	for i := 0; i < n; i++ {
		fanOut := V.At(i, 0)
		fanIn := V.At(i, 1)
		total := fanOut + fanIn
		if total == 0 {
			continue
		}
		I := fanOut / total
		scored++
		totalI += I
		var role mgraph.Role
		if node, ok := g.Node(ids[i]); ok {
			role = node.Role()
		}
		agg := byLayer[role]
		if agg == nil {
			agg = &layerAgg{}
			byLayer[role] = agg
		}
		agg.out += fanOut
		agg.in += fanIn
		agg.count++
		out = append(out, Record{
			Metric: "instability_matrix",
			Scope:  ScopeNode,
			Target: ids[i],
			Value:  I,
			Details: map[string]any{
				"fanOut": fanOut,
				"fanIn":  fanIn,
				"role":   string(role),
			},
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Target < out[b].Target })

	// Per-layer aggregates, deterministic by role name.
	layerKeys := make([]string, 0, len(byLayer))
	for k := range byLayer {
		layerKeys = append(layerKeys, string(k))
	}
	sort.Strings(layerKeys)
	layerRecs := make([]Record, 0, len(layerKeys))
	for _, lk := range layerKeys {
		agg := byLayer[mgraph.Role(lk)]
		total := agg.out + agg.in
		layerI := 0.0
		if total > 0 {
			layerI = agg.out / total
		}
		label := lk
		if label == "" {
			label = "(unlabelled)"
		}
		layerRecs = append(layerRecs, Record{
			Metric: "instability_matrix",
			Scope:  ScopeRegion,
			Target: "layer:" + label,
			Value:  layerI,
			Details: map[string]any{
				"role":   label,
				"fanOut": agg.out,
				"fanIn":  agg.in,
				"nodes":  agg.count,
			},
		})
	}

	mean := 0.0
	if scored > 0 {
		mean = totalI / float64(scored)
	}
	summary := Record{
		Metric: "instability_matrix",
		Scope:  ScopeGraph,
		Value:  mean,
		Details: map[string]any{
			"scoredNodes": scored,
			"nodes":       n,
		},
	}
	all := []Record{summary}
	all = append(all, layerRecs...)
	all = append(all, out...)
	return all
}

func init() {
	Register(MatrixValidator{
		MetricName:  "instability_matrix",
		Desc:        "Robert Martin instability I = fanOut/(fanIn+fanOut) via row/col sums of A",
		EncoderImpl: instabilityEncoder{},
		OpImpl:      RowColSumOp{},
		InterpImpl:  instabilityInterpreter{},
		EdgeKinds:   defaultDependencyEdgeKinds(),
	})
}
