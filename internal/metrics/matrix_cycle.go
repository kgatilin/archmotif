package metrics

import (
	"sort"

	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// cycle_matrix detects nodes that sit on a directed cycle of length ≤ K
// via repeated matrix multiplication. For each k ∈ [1..K] the validator
// computes A^k and inspects its diagonal: (A^k)[i][i] > 0 means there's a
// closed walk of length k starting and ending at i. The smallest such k
// is the shortest cycle length touching i.
//
// This is the canonical matrix-algebra cycle detector and gives us a
// complementary lens to cycle_rank (which uses Tarjan SCC).

// cycleMatrixSpec is the user-facing spec for the validator.
type cycleMatrixSpec struct {
	MaxCycleLength int
}

// cycleMatrixEncoder is a no-op encoder: cycle detection is purely a
// function of A. We return a 0×0 placeholder Dense so the framework's
// shape checks don't fire. (PowerDiagOp ignores R anyway.)
type cycleMatrixEncoder struct{}

// Name returns "cycle_matrix".
func (cycleMatrixEncoder) Name() string { return "cycle_matrix" }

// Encode returns nil — cycle_matrix is determined by A only and PowerDiagOp
// ignores R. Returning a placeholder Dense is wasteful (and gonum's
// `mat.NewDense` panics on zero dimensions); the framework tolerates a nil
// R from the encoder.
func (cycleMatrixEncoder) Encode(_ *mgraph.Graph, _ []string, _ any) (*mat.Dense, error) {
	return nil, nil
}

// cycleMatrixInterpreter scans the violation matrix V (where V[i][i]
// holds the shortest cycle length touching node i) and emits one
// ScopeNode record per cycling node plus a graph-scope summary.
type cycleMatrixInterpreter struct{}

// Interpret emits ScopeNode records for every diagonal cell > 0.
func (cycleMatrixInterpreter) Interpret(V *mat.Dense, ids []string, g *mgraph.Graph) []Record {
	n, _ := V.Dims()
	out := []Record{}
	cycling := 0
	for i := 0; i < n; i++ {
		length := V.At(i, i)
		if length <= 0 {
			continue
		}
		cycling++
		var qname string
		if node, ok := g.Node(ids[i]); ok {
			qname = node.QName
		}
		out = append(out, Record{
			Metric: "cycle_matrix",
			Scope:  ScopeNode,
			Target: ids[i],
			Value:  length,
			Details: map[string]any{
				"shortestCycle": length,
				"qname":         qname,
			},
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Target < out[b].Target })
	summary := Record{
		Metric: "cycle_matrix",
		Scope:  ScopeGraph,
		Value:  float64(cycling),
		Details: map[string]any{
			"cyclingNodes": cycling,
			"nodes":        n,
		},
	}
	return append([]Record{summary}, out...)
}

func init() {
	spec := cycleMatrixSpec{MaxCycleLength: 8}
	Register(MatrixValidator{
		MetricName:  "cycle_matrix",
		Desc:        "matrix-power cycle detector: smallest k ∈ [1..K] with (A^k)[i][i] > 0",
		EncoderImpl: cycleMatrixEncoder{},
		OpImpl:      PowerDiagOp{K: spec.MaxCycleLength},
		InterpImpl:  cycleMatrixInterpreter{},
		EdgeKinds: map[mgraph.EdgeKind]bool{
			mgraph.EdgeCalls:     true,
			mgraph.EdgeDependsOn: true,
		},
		Spec: spec,
		ConfigDefaults: map[string]any{
			"max_cycle_length": spec.MaxCycleLength,
		},
	})
}
