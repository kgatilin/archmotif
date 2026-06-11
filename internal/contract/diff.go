package contract

import (
	"maps"
	"sort"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// DiffSummary is the machine-readable result of a focus diff.
type DiffSummary struct {
	Key        string   `json:"key"`         // node attribute used as stable identity
	Added      []string `json:"added"`       // keys present in after, absent in before
	Removed    []string `json:"removed"`     // keys present in before, absent in after
	AddedN     int      `json:"added_count"`
	RemovedN   int      `json:"removed_count"`
	ContextN   int      `json:"context_count"`
	FocusNodes int      `json:"focus_nodes"`
	FocusEdges int      `json:"focus_edges"`
}

// Diff computes the *added subgraph* of `after` relative to `before`, keyed by a
// stable node attribute (default "qname"). This is the "focus on the delta"
// primitive: instead of analysing the whole graph, look only at what a branch
// adds, plus a ring of context so the addition is legible.
//
// Keying matters: archmotif's GraphML node id is position-based
// (file:line:col:…), so an edit re-ids every symbol in a touched file. Diffing
// by `qname` (or any caller-chosen stable attribute) ignores that churn.
//
//   - key:       node attribute to use as identity ("id"/"label"/"kind" are
//     built-ins; anything else is read from Attrs, e.g. "qname").
//   - context:   how many hops of neighbours (in `after`) to pull in around the
//     added nodes as diff=context. 0 = added nodes only.
//   - namedOnly: drop nodes that lack the key attribute from the diff entirely
//     (so structural filler like branch/loop/field nodes doesn't show up as
//     spurious additions on a code graph).
//
// The returned graph carries a `diff` attribute on every node (added|context)
// and every edge (added = new to `after`, context = pre-existing).
func Diff(before, after *graphmlx.Graph, key string, context int, namedOnly bool) (*graphmlx.Graph, DiffSummary) {
	keyOf := func(n graphmlx.Node) string {
		var v string
		switch key {
		case "id":
			v = n.ID
		case "label":
			v = n.Label
		case "kind":
			v = n.Kind
		default:
			v = n.Attrs[key]
		}
		if v == "" && !namedOnly {
			v = n.ID
		}
		return v
	}

	beforeKeys := map[string]bool{}
	for _, n := range before.Nodes {
		if k := keyOf(n); k != "" {
			beforeKeys[k] = true
		}
	}
	afterKeys := map[string]bool{}
	idKey := map[string]string{} // after node ID -> key (only in-universe nodes)
	for _, n := range after.Nodes {
		k := keyOf(n)
		if k == "" {
			continue
		}
		afterKeys[k] = true
		idKey[n.ID] = k
	}

	addedIDs := map[string]bool{}
	for _, n := range after.Nodes {
		k := idKey[n.ID]
		if k != "" && !beforeKeys[k] {
			addedIDs[n.ID] = true
		}
	}

	// Expand `context` hops of neighbours in `after` (both directions), staying
	// inside the universe of keyed nodes.
	inFocus := map[string]bool{}
	for id := range addedIDs {
		inFocus[id] = true
	}
	frontier := make(map[string]bool, len(addedIDs))
	for id := range addedIDs {
		frontier[id] = true
	}
	for range context {
		next := map[string]bool{}
		for _, e := range after.Edges {
			if frontier[e.From] && idKey[e.To] != "" && !inFocus[e.To] {
				next[e.To] = true
			}
			if frontier[e.To] && idKey[e.From] != "" && !inFocus[e.From] {
				next[e.From] = true
			}
		}
		if len(next) == 0 {
			break
		}
		for id := range next {
			inFocus[id] = true
		}
		frontier = next
	}

	// Pre-existing edge identities, for marking which focus edges are new.
	beforeIDKey := map[string]string{}
	for _, n := range before.Nodes {
		if k := keyOf(n); k != "" {
			beforeIDKey[n.ID] = k
		}
	}
	type ek struct{ from, to, kind string }
	beforeEdges := map[ek]bool{}
	for _, e := range before.Edges {
		fk, tk := beforeIDKey[e.From], beforeIDKey[e.To]
		if fk != "" && tk != "" {
			beforeEdges[ek{fk, tk, e.Kind}] = true
		}
	}

	out := &graphmlx.Graph{Directed: after.Directed}
	contextCount := 0
	for _, n := range after.Nodes {
		if !inFocus[n.ID] {
			continue
		}
		attrs := make(map[string]string, len(n.Attrs)+1)
		maps.Copy(attrs, n.Attrs)
		if addedIDs[n.ID] {
			attrs["diff"] = "added"
		} else {
			attrs["diff"] = "context"
			contextCount++
		}
		out.Nodes = append(out.Nodes, graphmlx.Node{XMLID: n.XMLID, ID: n.ID, Label: n.Label, Kind: n.Kind, Attrs: attrs})
	}
	for _, e := range after.Edges {
		if !inFocus[e.From] || !inFocus[e.To] {
			continue
		}
		attrs := make(map[string]string, len(e.Attrs)+1)
		maps.Copy(attrs, e.Attrs)
		if beforeEdges[ek{idKey[e.From], idKey[e.To], e.Kind}] {
			attrs["diff"] = "context"
		} else {
			attrs["diff"] = "added"
		}
		out.Edges = append(out.Edges, graphmlx.Edge{XMLID: e.XMLID, From: e.From, To: e.To, Kind: e.Kind, Attrs: attrs})
	}

	added := keysOf(afterKeys, beforeKeys)
	removed := keysOf(beforeKeys, afterKeys)
	return out, DiffSummary{
		Key:        key,
		Added:      added,
		Removed:    removed,
		AddedN:     len(added),
		RemovedN:   len(removed),
		ContextN:   contextCount,
		FocusNodes: len(out.Nodes),
		FocusEdges: len(out.Edges),
	}
}

// keysOf returns the sorted keys in `a` that are absent from `b`.
func keysOf(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
