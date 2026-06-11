package metrics

import (
	"context"
	"fmt"

	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Matrix-based architecture validators: encode each rule once and evaluate it
// as a matrix product, then dogfood the validators on archmotif itself.
//
// The validators are intentionally split into three composable layers so
// rules can be encoded once and evaluated by different matrix products:
//
//  1. Encoder — turns a rule spec into a rule matrix R (typically N×N or N×K).
//  2. Operation — combines the adjacency A with R via a real *mat.Dense
//     operation (Hadamard product, matrix power, layer collapse, …).
//  3. Interpreter — walks the resulting violation matrix and emits Records.
//
// All three layers operate exclusively on `*mat.Dense` from
// gonum.org/v1/gonum/mat. Graph traversal is restricted to the initial
// adjacency materialisation step; everything downstream is matrix algebra.
//
// New validators are wired in by composing a MatrixValidator value in their
// own metric file and registering it via init(), exactly like every other
// metric (per ADR-011).

// Encoder converts a rule spec into a rule matrix R.
//
// Implementations decide the matrix shape:
//   - For Hadamard masks, R has the same dimensions as A (N×N).
//   - For layer collapse, R is the N×K layer-incidence matrix L.
//   - For power-based encoders, R may be nil — the operation only needs A.
//
// Encoders must be deterministic for a given (graph, spec) pair so metric
// output is reproducible.
type Encoder interface {
	Name() string
	Encode(g *mgraph.Graph, ids []string, spec any) (*mat.Dense, error)
}

// Operation combines the adjacency A and rule matrix R into a violation
// matrix V via a single matrix operation. Different ops implement different
// semantics:
//
//   - hadamard:         V = A ⊙ R               (forbidden-edge mask)
//   - power_diag:       V_kk = diag(A^k) ⊆ V    (cycle detection)
//   - layer_collapse:   V = Lᵀ · A · L          (layer-graph adjacency)
//   - transpose_diff:   V = A − Aᵀ              (asymmetric edges)
//   - row_col_sum:      V[i] = (Σⱼ A_ij, Σⱼ A_ji) (fan-out, fan-in)
//
// Apply must not mutate A or R.
type Operation interface {
	Name() string
	Apply(A, R *mat.Dense) (*mat.Dense, error)
}

// Interpreter walks a violation matrix V and emits metric Records keyed by
// node IDs. The graph is passed so interpreters can enrich Details with
// attributes (role labels, kinds, …) without re-deriving them from V.
type Interpreter interface {
	Interpret(V *mat.Dense, ids []string, g *mgraph.Graph) []Record
}

// MatrixValidator composes an Encoder + Operation + Interpreter into a
// registered Metric. The SpecLoader hook is reserved for future
// `--config`-driven rule sets; built-in defaults remain in the validator
// file.
type MatrixValidator struct {
	MetricName  string
	Desc        string
	EncoderImpl Encoder
	OpImpl      Operation
	InterpImpl  Interpreter
	// EdgeKinds restricts which edge kinds contribute to A. Nil/empty
	// means "all kinds" — but most architecture validators want only
	// dependency-bearing kinds (Calls, DependsOn, UsesType, Returns,
	// References).
	EdgeKinds map[mgraph.EdgeKind]bool
	// Spec is the rule input handed to the encoder (e.g. {K:8} for
	// cycle_matrix, the layer-rule table for layer_mask). Loaded
	// statically per validator today; SpecLoader is the extension hook.
	Spec       any
	SpecLoader func(cfg map[string]any) (any, error)
	// ConfigDefaults is returned from Configurable() so the runner can
	// surface tunables uniformly across metrics.
	ConfigDefaults map[string]any
}

// Name returns the metric identifier.
func (m MatrixValidator) Name() string { return m.MetricName }

// Description returns the metric documentation string.
func (m MatrixValidator) Description() string { return m.Desc }

// Configurable returns the user-tunable knobs for this validator.
func (m MatrixValidator) Configurable() map[string]any {
	if m.ConfigDefaults == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m.ConfigDefaults))
	for k, v := range m.ConfigDefaults {
		out[k] = v
	}
	return out
}

// Compute is the four-step pipeline: adjacency → encode → operation →
// interpret. Empty graphs short-circuit to an empty record set.
func (m MatrixValidator) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	A, ids, err := adjacencyDense(g, m.EdgeKinds)
	if err != nil {
		return nil, fmt.Errorf("%s: adjacency: %w", m.MetricName, err)
	}
	if len(ids) == 0 {
		return []Record{{
			Metric: m.MetricName,
			Scope:  ScopeGraph,
			Value:  0,
			Details: map[string]any{
				"note": "empty graph (no nodes in selected edge view)",
			},
		}}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var R *mat.Dense
	if m.EncoderImpl != nil {
		R, err = m.EncoderImpl.Encode(g, ids, m.Spec)
		if err != nil {
			return nil, fmt.Errorf("%s: encode: %w", m.MetricName, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	V, err := m.OpImpl.Apply(A, R)
	if err != nil {
		return nil, fmt.Errorf("%s: op %s: %w", m.MetricName, m.OpImpl.Name(), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.InterpImpl == nil {
		return nil, fmt.Errorf("%s: nil interpreter", m.MetricName)
	}
	return m.InterpImpl.Interpret(V, ids, g), nil
}
