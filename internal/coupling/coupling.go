// Package coupling computes deterministic, role-aware coupling
// reports over a typed graph: a role-pair dependency matrix, a list
// of edges that violate a configured forbidden-edge policy, and a
// small set of named scores (domain purity, adapter isolation).
//
// The package consumes the role metadata produced by ADR-027
// (internal/roles): every edge is attributed to a directed pair of
// architectural roles, with package-role inheritance for endpoints
// that don't carry an explicit type-level role. See ADR-030 for the
// full rationale.
//
// Public entry point: Compute(g, cfg) Report. Renderers live in
// format.go.
package coupling

import (
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/graph"
)

// UnknownRole is the placeholder role for endpoints whose node and
// containing package are both unroled. Surfaced in the matrix so the
// operator can tell whether the .archmotif.yaml roles config covers
// their code.
const UnknownRole graph.Role = "unknown"

// Pair identifies a directed pair of architectural roles. From is the
// edge tail's role, To is the head's role.
type Pair struct {
	From graph.Role `json:"from"`
	To   graph.Role `json:"to"`
}

// EdgeEvidence references one concrete edge that contributed to a
// matrix cell or a forbidden-edge violation list. IDs are the stable
// graph node IDs (per ADR-005); Names are the human-friendly QName /
// Name from the corresponding nodes (best-effort, may be empty for
// foreign placeholders).
type EdgeEvidence struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	FromName string         `json:"fromName,omitempty"`
	ToName   string         `json:"toName,omitempty"`
	EdgeKind graph.EdgeKind `json:"edgeKind"`
}

// PairCount aggregates one cell of the role-pair matrix: the directed
// (From, To) role pair, the total edge count, and a capped list of
// representative edges.
type PairCount struct {
	Pair     Pair           `json:"pair"`
	Count    int            `json:"count"`
	Evidence []EdgeEvidence `json:"evidence,omitempty"`
}

// ForbiddenEdge is a single declarative entry from coupling.forbidden
// in .archmotif.yaml: edges from From-roled nodes to To-roled nodes
// constitute architectural violations.
type ForbiddenEdge struct {
	From   graph.Role `json:"from"`
	To     graph.Role `json:"to"`
	Reason string     `json:"reason,omitempty"`
}

// ForbiddenViolation is a concrete edge that matched a ForbiddenEdge
// rule. The rule index identifies which forbidden entry triggered the
// violation; useful for CI tools that group violations by rule.
type ForbiddenViolation struct {
	Rule     ForbiddenEdge `json:"rule"`
	Evidence EdgeEvidence  `json:"evidence"`
}

// Score is a named scalar in [0, 1] (or NaN when undefined for the
// graph) plus its raw numerator / denominator so consumers can
// recompute under different definitions.
type Score struct {
	Name        string  `json:"name"`
	Value       float64 `json:"value"`
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
	Description string  `json:"description"`
}

// Config drives Compute. EvidenceCap caps the per-pair Evidence slice
// length (also applied to ForbiddenViolations as a rule-wide cap, in
// the order edges are walked). EdgeKinds, when set, restricts which
// edge kinds contribute; an empty slice means DefaultEdgeKinds().
type Config struct {
	Forbidden   []ForbiddenEdge
	EvidenceCap int
	EdgeKinds   []graph.EdgeKind
}

// DefaultEvidenceCap is the per-pair evidence limit when Config does
// not specify one. ADR-030 §5 commits to 5 as the default.
const DefaultEvidenceCap = 5

// DefaultEdgeKinds returns the edge kinds the coupling matrix counts
// by default. Excludes EdgeContains (structural nesting, not coupling)
// and EdgeCallsFrom (double-counts EdgeCalls via the enclosing CFG
// primitive). See ADR-030 §2.
func DefaultEdgeKinds() []graph.EdgeKind {
	return []graph.EdgeKind{
		graph.EdgeDependsOn,
		graph.EdgeImplements,
		graph.EdgeEmbeds,
		graph.EdgeCalls,
		graph.EdgeReferences,
		graph.EdgeReturns,
		graph.EdgeUsesType,
	}
}

// Report is the full output of Compute.
type Report struct {
	// PairCounts is the role-pair matrix as a flat list, sorted by
	// (Count desc, From asc, To asc) so JSON snapshots are stable.
	PairCounts []PairCount `json:"pairCounts"`
	// ForbiddenViolations enumerates concrete edges that match any
	// rule in Config.Forbidden, deduplicated by edge.
	ForbiddenViolations []ForbiddenViolation `json:"forbiddenViolations,omitempty"`
	// Scores carries the named architectural-health scalars
	// (domain_purity, adapter_isolation, ...) in stable order.
	Scores []Score `json:"scores"`
	// EdgesConsidered is the total number of edges that contributed
	// to the matrix (after filtering by EdgeKinds and excluding
	// external_noise endpoints).
	EdgesConsidered int `json:"edgesConsidered"`
	// UnroledEndpoints is the count of edges whose From or To
	// endpoint resolved to UnknownRole. Surfaced so the operator can
	// gauge the coverage of their .archmotif.yaml roles block.
	UnroledEndpoints int `json:"unroledEndpoints"`
}

// Compute walks g, projects each in-scope edge onto a (fromRole,
// toRole) pair using the role-attribution rules in ADR-030 §4, and
// produces a Report. cfg is consulted for forbidden-edge rules,
// evidence-cap, and the edge-kind filter.
//
// Endpoints whose role resolves to graph.RoleTypeExternalNoise are
// excluded entirely (the edge is dropped — neither counted nor
// considered for forbidden rules). This matches ADR-030 §4's
// canonical noise marker.
func Compute(g *graph.Graph, cfg Config) Report {
	if g == nil {
		return Report{}
	}
	cap := cfg.EvidenceCap
	if cap <= 0 {
		cap = DefaultEvidenceCap
	}
	kinds := cfg.EdgeKinds
	if len(kinds) == 0 {
		kinds = DefaultEdgeKinds()
	}
	allowed := make(map[graph.EdgeKind]struct{}, len(kinds))
	for _, k := range kinds {
		allowed[k] = struct{}{}
	}

	roleOf := buildRoleResolver(g)

	// Stable per-pair accumulator.
	type pairKey struct {
		from graph.Role
		to   graph.Role
	}
	counts := make(map[pairKey]int)
	evidence := make(map[pairKey][]EdgeEvidence)

	report := Report{}

	// Pre-index forbidden rules by (from, to) for O(1) lookup. Multiple
	// rules per pair are unusual but allowed; the first match wins for
	// the violation record and others are ignored.
	rules := make(map[pairKey]ForbiddenEdge, len(cfg.Forbidden))
	for _, r := range cfg.Forbidden {
		key := pairKey{from: r.From, to: r.To}
		if _, dup := rules[key]; !dup {
			rules[key] = r
		}
	}

	for _, e := range g.Edges() {
		if _, ok := allowed[e.Kind]; !ok {
			continue
		}
		fromNode, fromOK := g.Node(e.From)
		toNode, toOK := g.Node(e.To)
		if !fromOK || !toOK {
			continue
		}
		fromRole := roleOf(fromNode)
		toRole := roleOf(toNode)

		// External-noise filter: drop edges that touch noise on
		// either side, per ADR-030 §4.
		if fromRole == graph.RoleTypeExternalNoise || toRole == graph.RoleTypeExternalNoise {
			continue
		}

		report.EdgesConsidered++
		if fromRole == UnknownRole || toRole == UnknownRole {
			report.UnroledEndpoints++
		}

		key := pairKey{from: fromRole, to: toRole}
		counts[key]++
		ev := EdgeEvidence{
			From:     e.From,
			To:       e.To,
			FromName: displayName(fromNode),
			ToName:   displayName(toNode),
			EdgeKind: e.Kind,
		}
		if len(evidence[key]) < cap {
			evidence[key] = append(evidence[key], ev)
		}

		if rule, hit := rules[key]; hit && len(report.ForbiddenViolations) < forbiddenLimit(cap, len(cfg.Forbidden)) {
			report.ForbiddenViolations = append(report.ForbiddenViolations, ForbiddenViolation{
				Rule:     rule,
				Evidence: ev,
			})
		}
	}

	// Materialise pair counts in stable sort order.
	pairs := make([]PairCount, 0, len(counts))
	for k, c := range counts {
		pairs = append(pairs, PairCount{
			Pair:     Pair{From: k.from, To: k.to},
			Count:    c,
			Evidence: evidence[k],
		})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		if pairs[i].Pair.From != pairs[j].Pair.From {
			return pairs[i].Pair.From < pairs[j].Pair.From
		}
		return pairs[i].Pair.To < pairs[j].Pair.To
	})
	report.PairCounts = pairs

	report.Scores = computeScores(pairs)
	return report
}

// forbiddenLimit caps the global forbidden-violation list. We pick a
// generous multiple of the per-pair cap so a single rule that matches
// many edges still produces a useful evidence list, but a config with
// many rules doesn't blow the report up unboundedly.
func forbiddenLimit(perPairCap, numRules int) int {
	if numRules <= 0 {
		return perPairCap * 4
	}
	return perPairCap * (numRules + 1) * 2
}

// buildRoleResolver returns a function that resolves a node's role per
// ADR-030 §4: the node's own role, falling back to its containing
// package's role, falling back to UnknownRole.
//
// Pre-computes per-package roles in one pass so the inheritance lookup
// is O(1) per node.
func buildRoleResolver(g *graph.Graph) func(graph.Node) graph.Role {
	pkgRole := make(map[string]graph.Role)
	for _, n := range g.NodesByKind(graph.NodePackage) {
		if r := n.Role(); r != "" {
			pkgRole[n.ID] = r
		}
	}
	// Also build a node-id → containing-package-id index once via the
	// reverse-Contains walk. For very large graphs this is O(E_contains).
	containerOf := make(map[string]string)
	for _, e := range g.Edges() {
		if e.Kind == graph.EdgeContains {
			// e.From contains e.To; record the parent of e.To.
			if _, dup := containerOf[e.To]; !dup {
				containerOf[e.To] = e.From
			}
		}
	}

	// Walk up containment chain to find the package node.
	pkgOf := func(n graph.Node) string {
		if n.Kind == graph.NodePackage {
			return n.ID
		}
		cur := n.ID
		for i := 0; i < 16; i++ {
			parent, ok := containerOf[cur]
			if !ok {
				return ""
			}
			pn, ok := g.Node(parent)
			if !ok {
				return ""
			}
			if pn.Kind == graph.NodePackage {
				return pn.ID
			}
			cur = parent
		}
		return ""
	}

	return func(n graph.Node) graph.Role {
		if r := n.Role(); r != "" {
			return r
		}
		if pid := pkgOf(n); pid != "" {
			if r, ok := pkgRole[pid]; ok {
				return r
			}
		}
		return UnknownRole
	}
}

// computeScores produces the named architectural-health scalars from
// the role-pair matrix. v1 ships two scores per ADR-030 §3:
//
//   - domain_purity: edges out of role=domain whose target stays
//     within domain or domain-typed roles, divided by total edges
//     out of role=domain. Higher = purer.
//
//   - adapter_isolation: edges out of role=adapter_dto (or any of
//     the adapter package roles) whose target stays within adapter /
//     infrastructure layers, divided by total edges out of those
//     roles. Higher = more isolated.
func computeScores(pairs []PairCount) []Score {
	domainPurity := purityScore(pairs, []graph.Role{graph.RolePackageDomain},
		[]graph.Role{
			graph.RolePackageDomain,
			graph.RoleTypeDomainEntity,
			graph.RoleTypeValueObject,
			graph.RoleTypePort,
			graph.RolePackageShared,
		},
		"domain_purity",
		"fraction of edges out of domain-roled nodes that stay within domain / value object / port / shared",
	)
	adapterIsolation := purityScore(pairs,
		[]graph.Role{
			graph.RolePackageInboundAdapter,
			graph.RolePackageOutboundAdapter,
			graph.RoleTypeAdapterDTO,
		},
		[]graph.Role{
			graph.RolePackageInboundAdapter,
			graph.RolePackageOutboundAdapter,
			graph.RolePackageInfrastructure,
			graph.RoleTypeAdapterDTO,
			graph.RolePackageShared,
		},
		"adapter_isolation",
		"fraction of edges out of adapter-roled nodes that stay within adapter / infrastructure / shared layers",
	)
	return []Score{domainPurity, adapterIsolation}
}

// purityScore is a generic "fraction of edges out of source-set that
// land in target-set" helper. Both sets are matched as set membership
// (graph.Role string equality).
func purityScore(pairs []PairCount, source, target []graph.Role, name, desc string) Score {
	srcSet := roleSet(source)
	tgtSet := roleSet(target)
	num, denom := 0, 0
	for _, p := range pairs {
		if _, ok := srcSet[p.Pair.From]; !ok {
			continue
		}
		denom += p.Count
		if _, ok := tgtSet[p.Pair.To]; ok {
			num += p.Count
		}
	}
	value := 0.0
	if denom > 0 {
		value = float64(num) / float64(denom)
	}
	return Score{
		Name:        name,
		Value:       value,
		Numerator:   num,
		Denominator: denom,
		Description: desc,
	}
}

func roleSet(roles []graph.Role) map[graph.Role]struct{} {
	out := make(map[graph.Role]struct{}, len(roles))
	for _, r := range roles {
		out[r] = struct{}{}
	}
	return out
}

// displayName picks the most human-friendly label for a node:
// QName when present, falling back to Name. Returns "" when both are
// empty (foreign placeholders).
func displayName(n graph.Node) string {
	if strings.TrimSpace(n.QName) != "" {
		return n.QName
	}
	return n.Name
}
