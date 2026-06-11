// Package wlkernel is a research-only prototype for issue #32 — graph
// embeddings for architecture similarity. It implements a tiny
// Weisfeiler-Lehman (WL) subtree kernel over the archmotif typed graph
// and exposes a similarity score between two graphs.
//
// This package is intentionally NOT wired into the CLI or any Stage
// pipeline. It exists so ADR-036 can show concrete numbers on real
// fixtures rather than hand-waving. See docs/decisions/036-graph-embeddings-research.md.
//
// The prototype is intentionally small:
//   - One iteration of node-label refinement (WL-1).
//   - Initial labels are derived from (NodeKind, Role) — the two
//     deterministic axes the rest of archmotif uses.
//   - Edge kind is folded into the neighbour signature so that
//     `domain --calls--> port` and `domain --embeds--> port` refine
//     to different labels at iteration 1.
//   - Graph "embedding" is the multiset of refined labels, hashed to
//     a sparse uint64 → count map.
//   - Similarity is normalised label-multiset cosine.
//
// Why WL and not node2vec / metapath2vec? See ADR-036 §"Methods
// evaluated". Briefly: WL is deterministic, label-aware, has no
// training step, and is a strict generalisation of the kind of
// motif-iso work archmotif already does in internal/metrics/motif.go.
// node2vec and friends need a corpus and a training run; archmotif
// has neither at this scale.
package wlkernel

import (
	"hash/fnv"
	"math"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Embedding is the WL multiset signature of a graph.
//
// Counts maps a refined-label hash to the number of nodes carrying
// that label after Iterations rounds of refinement. Iterations is
// recorded so two embeddings at different depths cannot be silently
// compared.
type Embedding struct {
	Iterations int
	Counts     map[uint64]int
}

// Compute returns the WL embedding of g after `iterations` rounds of
// label refinement. iterations=0 captures only the initial
// (NodeKind, Role) histogram; iterations=1 is the WL-1 subtree
// kernel, which is the prototype's default.
//
// The function is pure and deterministic: the same graph + iterations
// always produces the same Counts map.
func Compute(g *mgraph.Graph, iterations int) Embedding {
	if iterations < 0 {
		iterations = 0
	}

	// Initial labels: (kind, role). Role is read via the Attrs API
	// rather than typed accessor to keep the prototype free of
	// internal dependencies on the graph package's role helpers.
	labels := make(map[string]uint64, g.NodeCount())
	for _, n := range g.Nodes() {
		labels[n.ID] = hashStrings(string(n.Kind), roleOf(n))
	}

	for iter := 0; iter < iterations; iter++ {
		next := make(map[string]uint64, len(labels))
		for _, n := range g.Nodes() {
			next[n.ID] = refine(g, n.ID, labels)
		}
		labels = next
	}

	counts := make(map[uint64]int, len(labels))
	for _, h := range labels {
		counts[h]++
	}
	return Embedding{Iterations: iterations, Counts: counts}
}

// refine returns the WL refinement of node id given the current label
// map. Edge kind and direction are part of the neighbour signature,
// so a node with two `calls` neighbours refines differently from a
// node with one `calls` and one `implements` neighbour.
func refine(g *mgraph.Graph, id string, labels map[string]uint64) uint64 {
	type sigPart struct {
		key   string // "<kind>:<dir>:<labelHash>"
		label uint64
	}
	var parts []sigPart
	for _, e := range g.IncidentEdges(id, mgraph.DirectionOut, "") {
		parts = append(parts, sigPart{key: string(e.Kind) + ":out", label: labels[e.To]})
	}
	for _, e := range g.IncidentEdges(id, mgraph.DirectionIn, "") {
		parts = append(parts, sigPart{key: string(e.Kind) + ":in", label: labels[e.From]})
	}
	// Sort for determinism — WL refinement is multiset-based.
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].key != parts[j].key {
			return parts[i].key < parts[j].key
		}
		return parts[i].label < parts[j].label
	})

	h := fnv.New64a()
	// Seed with the node's own current label so isolated nodes still
	// refine to a stable signature.
	writeUint64(h, labels[id])
	for _, p := range parts {
		_, _ = h.Write([]byte(p.key))
		writeUint64(h, p.label)
	}
	return h.Sum64()
}

// Cosine returns the normalised cosine similarity between the two
// label multisets. Result is in [0, 1]: 1 means identical multisets,
// 0 means no shared labels at all.
//
// Embeddings produced at different iteration depths return 0 — the
// label spaces are not commensurable.
func Cosine(a, b Embedding) float64 {
	if a.Iterations != b.Iterations {
		return 0
	}
	if len(a.Counts) == 0 || len(b.Counts) == 0 {
		return 0
	}
	var dot, na, nb float64
	for h, ca := range a.Counts {
		na += float64(ca) * float64(ca)
		if cb, ok := b.Counts[h]; ok {
			dot += float64(ca) * float64(cb)
		}
	}
	for _, cb := range b.Counts {
		nb += float64(cb) * float64(cb)
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// roleOf returns the node's role attribute as a string, or "" if
// unset. Mirrors what ADR-027 declares for the Attrs key.
func roleOf(n mgraph.Node) string {
	if n.Attrs == nil {
		return ""
	}
	if v, ok := n.Attrs["role"]; ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

// hashStrings combines an arbitrary number of strings into one uint64
// using FNV-1a. Order matters.
func hashStrings(parts ...string) uint64 {
	h := fnv.New64a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// writeUint64 streams an 8-byte little-endian representation of v
// into h. We avoid encoding/binary to keep the prototype's import
// surface trivial.
func writeUint64(h interface{ Write([]byte) (int, error) }, v uint64) {
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(v >> (8 * i))
	}
	_, _ = h.Write(buf[:])
}
