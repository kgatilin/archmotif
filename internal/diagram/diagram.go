// Package diagram projects archmotif's typed graph (see internal/graph
// and ADR-005, ADR-027, ADR-030) into deterministic diagram-shaped
// slices. Each projection answers a single architectural question
// ("which packages depend on which?", "which contracts/ports does the
// system expose?", "what does the call flow rooted at this entrypoint
// look like?") rather than asking the operator to filter the full
// graph by hand.
//
// A projection produces a small in-memory graph (Diagram) that mirrors
// the typed graph but is intentionally smaller and aligned to a
// specific shape. Diagram nodes/edges keep stable evidence IDs back to
// the underlying graph nodes/edges so consumers can pivot from a
// rendered view to the source-level evidence at any time.
//
// Renderers (D2, JSON, GraphML) live alongside the projection logic in
// this package; the CLI wires `archmotif diagram <kind> [--format ...]`
// through Run. See ADR-035 for the projection model + filter rules.
package diagram

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/graph"
)

// Kind identifies a registered projection. Stable string form so the
// CLI surface and JSON envelopes don't churn when the registry grows.
type Kind string

// Registered projection kinds. Add new kinds at the bottom — order is
// stable so `archmotif diagram --list` output is deterministic.
const (
	// KindPackageDeps projects EdgeDependsOn between Package nodes.
	KindPackageDeps Kind = "package-deps"
	// KindContractPort projects the contract / port boundary: every
	// node marked as a contract (ADR-009) plus its declared package
	// and the implementing types (EdgeImplements). Includes nodes
	// roled as port (ADR-027) even when not flagged contract.
	KindContractPort Kind = "contract-port"
	// KindCallFlow seeds a call-flow diagram from one or more
	// entrypoints and walks EdgeCalls / EdgeCallsFrom forward to a
	// configurable depth.
	KindCallFlow Kind = "call-flow"
)

// AllKinds returns every registered projection kind in stable order.
func AllKinds() []Kind {
	return []Kind{KindPackageDeps, KindContractPort, KindCallFlow}
}

// ParseKind normalises a CLI string into a known Kind. Returns an
// error listing valid kinds when the input is unknown.
func ParseKind(s string) (Kind, error) {
	for _, k := range AllKinds() {
		if string(k) == strings.TrimSpace(s) {
			return k, nil
		}
	}
	names := make([]string, 0, len(AllKinds()))
	for _, k := range AllKinds() {
		names = append(names, string(k))
	}
	return "", fmt.Errorf("unknown diagram kind %q (want: %s)", s, strings.Join(names, "|"))
}

// Description returns a one-line human-readable description for a
// projection kind. Used by the CLI list output.
func Description(k Kind) string {
	switch k {
	case KindPackageDeps:
		return "package dependency diagram (DependsOn between Package nodes)"
	case KindContractPort:
		return "contract/port boundary diagram (contracts + roled ports + implementers)"
	case KindCallFlow:
		return "call-flow seed diagram (BFS from entrypoints along Calls/CallsFrom)"
	default:
		return ""
	}
}

// Options configures a projection run. Not all options apply to every
// kind; unused fields are ignored.
type Options struct {
	// Seeds is the comma-separated list of node IDs (or QName glob
	// matches, see ResolveSeeds) used by KindCallFlow. Empty means
	// "auto-pick package-level main + every NodeFunction whose Name
	// starts with 'main' or 'Run' / 'Serve'" — see callflow.go.
	Seeds []string
	// Depth caps the BFS depth for KindCallFlow. <=0 means default
	// (3 hops) — picked to surface a useful slice without blowing up
	// fan-out on hub functions.
	Depth int
	// IncludeForeign keeps nodes flagged with Attrs.foreign=true in
	// the projection. Default (false) drops them since most
	// architecture views care about owned code.
	IncludeForeign bool
}

// Diagram is the projection output: a small directed graph with stable
// node and edge identifiers plus evidence pointers back to the source
// graph. The shape is intentionally close to the renderer API so D2 /
// JSON / GraphML formatting is mechanical.
type Diagram struct {
	// Kind is the projection that produced this diagram.
	Kind Kind `json:"kind"`
	// Title is a human-readable label rendered at the top of D2 /
	// Markdown output.
	Title string `json:"title"`
	// Nodes is the projection's node set, in stable insertion order.
	Nodes []DiagNode `json:"nodes"`
	// Edges is the projection's edge set, in stable insertion order.
	Edges []DiagEdge `json:"edges"`
	// Notes carries operator-facing diagnostics: dropped seeds, missing
	// roles, depth truncation. Stable order.
	Notes []string `json:"notes,omitempty"`
}

// DiagNode is a projection node. EvidenceIDs lists the underlying
// graph node IDs that this projection node summarises (typically just
// one, but contract-port collapses pkg+contract for layout).
type DiagNode struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Kind        graph.NodeKind `json:"kind,omitempty"`
	Role        graph.Role     `json:"role,omitempty"`
	Cluster     string         `json:"cluster,omitempty"`
	EvidenceIDs []string       `json:"evidenceIds"`
}

// DiagEdge is a projection edge. EvidenceIDs lists the underlying
// graph edges, encoded as "from>to>kind" so callers can reconstruct
// the full evidence list without holding the source graph.
type DiagEdge struct {
	From        string         `json:"from"`
	To          string         `json:"to"`
	Kind        graph.EdgeKind `json:"kind"`
	Label       string         `json:"label,omitempty"`
	Count       int            `json:"count"`
	EvidenceIDs []string       `json:"evidenceIds"`
}

// Build dispatches to the registered projection for kind. Returns an
// error when kind is unknown.
func Build(g *graph.Graph, kind Kind, opts Options) (*Diagram, error) {
	if g == nil {
		return nil, fmt.Errorf("diagram: nil graph")
	}
	switch kind {
	case KindPackageDeps:
		return buildPackageDeps(g, opts), nil
	case KindContractPort:
		return buildContractPort(g, opts), nil
	case KindCallFlow:
		return buildCallFlow(g, opts), nil
	}
	return nil, fmt.Errorf("diagram: unknown kind %q", kind)
}

// edgeEvidenceID encodes a graph edge for the EvidenceIDs slice. Same
// shape as the coupling package's evidence references so downstream
// tools can join across reports.
func edgeEvidenceID(from, to string, kind graph.EdgeKind) string {
	return from + ">" + to + ">" + string(kind)
}

// stableNodes returns g.Nodes() filtered to the requested kind in
// insertion order (deterministic).
func stableNodes(g *graph.Graph, kind graph.NodeKind) []graph.Node {
	all := g.NodesByKind(kind)
	out := make([]graph.Node, len(all))
	copy(out, all)
	return out
}

// sortNodesByLabel sorts a DiagNode slice in-place by label then ID
// for stable rendering. Insertion order tends to track build order
// already, but explicit sort keeps snapshots robust to upstream
// changes.
func sortNodesByLabel(ns []DiagNode) {
	sort.SliceStable(ns, func(i, j int) bool {
		if ns[i].Label != ns[j].Label {
			return ns[i].Label < ns[j].Label
		}
		return ns[i].ID < ns[j].ID
	})
}

// sortEdges sorts a DiagEdge slice in-place by (from, to, kind) for
// stable rendering.
func sortEdges(es []DiagEdge) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].From != es[j].From {
			return es[i].From < es[j].From
		}
		if es[i].To != es[j].To {
			return es[i].To < es[j].To
		}
		return es[i].Kind < es[j].Kind
	})
}

// nodeForeign returns true when the node carries Attrs.foreign=true.
func nodeForeign(n graph.Node) bool {
	if n.Attrs == nil {
		return false
	}
	v, _ := n.Attrs["foreign"].(bool)
	return v
}
