package metrics

import (
	"errors"
	"sort"

	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// layer_mask is a Hadamard-based validator that flags edges crossing
// forbidden layer boundaries. Encoded entirely as matrix algebra:
//
//   1. Encoder reads each node's `Attrs["role"]` (a package layer) and
//      builds an N×N forbidden-edge mask V where V[i][j]=1 iff the rule
//      table marks (role(i) → role(j)) as forbidden.
//   2. Operation is HadamardOp: A ⊙ V. Any nonzero cell is a violation.
//   3. Interpreter emits one ScopeEdge record per nonzero cell.
//
// Sign convention: V is the FORBIDDEN mask, not a permission mask. Any
// A ⊙ V > 0 is a hit. This keeps the math direct.

// layerMaskRule is a single forbidden (from → to) layer pair.
type layerMaskRule struct {
	From    mgraph.Role
	To      mgraph.Role
	Allowed bool
}

// layerMaskSpec is the rule set handed to the encoder.
type layerMaskSpec struct {
	Rules []layerMaskRule
}

// defaultLayerMaskSpec is a sensible Clean-Architecture baseline. Domain
// must not depend on anything outside the domain. Application must not
// depend on adapters or infrastructure. Adapters must not depend on each
// other directly. Anything → shared is allowed; shared → anywhere is
// allowed.
//
// "Allowed" rules are added for explicit clarity but are no-ops at the
// matrix level — the encoder only writes 1s for `Allowed: false` rules.
func defaultLayerMaskSpec() layerMaskSpec {
	deny := func(from, to mgraph.Role) layerMaskRule {
		return layerMaskRule{From: from, To: to, Allowed: false}
	}
	return layerMaskSpec{Rules: []layerMaskRule{
		deny(mgraph.RolePackageDomain, mgraph.RolePackageApplication),
		deny(mgraph.RolePackageDomain, mgraph.RolePackageInboundAdapter),
		deny(mgraph.RolePackageDomain, mgraph.RolePackageOutboundAdapter),
		deny(mgraph.RolePackageDomain, mgraph.RolePackageInfrastructure),
		deny(mgraph.RolePackageApplication, mgraph.RolePackageInboundAdapter),
		deny(mgraph.RolePackageApplication, mgraph.RolePackageOutboundAdapter),
		deny(mgraph.RolePackageApplication, mgraph.RolePackageInfrastructure),
		deny(mgraph.RolePackageOutboundAdapter, mgraph.RolePackageDomain),
		deny(mgraph.RolePackageInboundAdapter, mgraph.RolePackageOutboundAdapter),
		deny(mgraph.RolePackageOutboundAdapter, mgraph.RolePackageInboundAdapter),
	}}
}

// layerMaskEncoder builds the N×N forbidden-edge mask V.
type layerMaskEncoder struct{}

// Name returns "layer_mask".
func (layerMaskEncoder) Name() string { return "layer_mask" }

// Encode walks the node IDs (in their stable order) and emits V[i][j]=1
// when the rule table marks (role(i) → role(j)) as forbidden. Nodes
// without a resolved role contribute zero rows/cols, which is the
// intended behaviour: only role-annotated edges are subject to the
// architectural constraint.
func (layerMaskEncoder) Encode(g *mgraph.Graph, ids []string, spec any) (*mat.Dense, error) {
	s, ok := spec.(layerMaskSpec)
	if !ok {
		return nil, errors.New("layer_mask: spec is not layerMaskSpec")
	}
	forbidden := make(map[mgraph.Role]map[mgraph.Role]bool)
	for _, r := range s.Rules {
		if r.Allowed {
			continue
		}
		if forbidden[r.From] == nil {
			forbidden[r.From] = map[mgraph.Role]bool{}
		}
		forbidden[r.From][r.To] = true
	}
	roles := make([]mgraph.Role, len(ids))
	for i, id := range ids {
		if n, ok := g.Node(id); ok {
			roles[i] = n.Role()
		}
	}
	n := len(ids)
	V := mat.NewDense(n, n, nil)
	for i := 0; i < n; i++ {
		ri := roles[i]
		if ri == "" {
			continue
		}
		row, ok := forbidden[ri]
		if !ok {
			continue
		}
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			rj := roles[j]
			if rj == "" {
				continue
			}
			if row[rj] {
				V.Set(i, j, 1)
			}
		}
	}
	return V, nil
}

// layerMaskInterpreter emits one ScopeEdge record per nonzero cell of V.
type layerMaskInterpreter struct{}

// Interpret produces:
//   - one ScopeGraph record carrying the total violation count.
//   - one ScopeEdge record per violation with from/to IDs + roles.
func (layerMaskInterpreter) Interpret(V *mat.Dense, ids []string, g *mgraph.Graph) []Record {
	n, _ := V.Dims()
	out := []Record{}
	type hit struct{ i, j int }
	hits := []hit{}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if V.At(i, j) > 0 {
				hits = append(hits, hit{i, j})
			}
		}
	}
	for _, h := range hits {
		fromRole := ""
		toRole := ""
		if nFrom, ok := g.Node(ids[h.i]); ok {
			fromRole = string(nFrom.Role())
		}
		if nTo, ok := g.Node(ids[h.j]); ok {
			toRole = string(nTo.Role())
		}
		out = append(out, Record{
			Metric: "layer_mask",
			Scope:  ScopeEdge,
			Target: ids[h.i] + "→" + ids[h.j],
			Value:  1,
			Details: map[string]any{
				"from":     ids[h.i],
				"to":       ids[h.j],
				"fromRole": fromRole,
				"toRole":   toRole,
			},
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Target < out[b].Target })
	// Prepend the graph-scope summary.
	summary := Record{
		Metric: "layer_mask",
		Scope:  ScopeGraph,
		Value:  float64(len(hits)),
		Details: map[string]any{
			"violations": len(hits),
			"nodes":      n,
		},
	}
	return append([]Record{summary}, out...)
}

func init() {
	Register(MatrixValidator{
		MetricName:  "layer_mask",
		Desc:        "Hadamard mask of forbidden layer-to-layer edges (A ⊙ V)",
		EncoderImpl: layerMaskEncoder{},
		OpImpl:      HadamardOp{},
		InterpImpl:  layerMaskInterpreter{},
		EdgeKinds:   defaultDependencyEdgeKinds(),
		Spec:        defaultLayerMaskSpec(),
		ConfigDefaults: map[string]any{
			"rules": "clean-architecture defaults (domain isolated, application above adapters)",
		},
	})
}
