package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GraphRef identifies one graph stored under the workspace root. A reference
// is `<slug>:<variant>` (e.g. `nassau:actual`, `nassau:target`,
// `feat-cost-cap:actual`). The default variant when only `<slug>` is supplied
// is "actual".
type GraphRef struct {
	ID      string `json:"id"`             // "<slug>:<variant>"
	Slug    string `json:"slug"`           // directory under graphs/
	Variant string `json:"variant"`        // file basename without .graphml
	Path    string `json:"path"`           // absolute path on disk
	Size    int64  `json:"size,omitempty"` // file size in bytes
	Nodes   int    `json:"nodes,omitempty"`
	Edges   int    `json:"edges,omitempty"`
}

// ListGraphs walks <root>/graphs and returns one GraphRef per `.graphml` file.
// The list is sorted by id (slug + variant) for deterministic output.
//
// Missing root or empty graphs/ returns an empty slice and no error — callers
// can use this to detect whether the workspace has any graphs yet.
func (s *Service) ListGraphs() ([]GraphRef, error) {
	graphsDir := filepath.Join(s.Root, "graphs")
	entries, err := os.ReadDir(graphsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GraphRef{}, nil
		}
		return nil, fmt.Errorf("ListGraphs: read %s: %w", graphsDir, err)
	}
	out := make([]GraphRef, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		variants, err := os.ReadDir(filepath.Join(graphsDir, slug))
		if err != nil {
			continue
		}
		for _, v := range variants {
			if v.IsDir() {
				continue
			}
			name := v.Name()
			if !strings.HasSuffix(name, ".graphml") {
				continue
			}
			variant := strings.TrimSuffix(name, ".graphml")
			full := filepath.Join(graphsDir, slug, name)
			ref := GraphRef{
				ID:      slug + ":" + variant,
				Slug:    slug,
				Variant: variant,
				Path:    full,
			}
			if info, err := v.Info(); err == nil {
				ref.Size = info.Size()
			}
			if g, err := Load(full); err == nil {
				ref.Nodes = len(g.Nodes)
				ref.Edges = len(g.Edges)
			}
			out = append(out, ref)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CheckoutGraph confirms graphID exists and returns the resolved reference.
// "Checking out" in v1 is a stateless lookup: there is no per-session current
// graph, so this is effectively `stat + load` and is exposed so agents can
// validate a graph_id before issuing follow-up tool calls.
func (s *Service) CheckoutGraph(graphID string) (GraphRef, error) {
	path, err := s.resolvePath(graphID)
	if err != nil {
		return GraphRef{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return GraphRef{}, fmt.Errorf("graph %q not found at %s", graphID, path)
	}
	g, err := Load(path)
	if err != nil {
		return GraphRef{}, err
	}
	slug, variant, err := splitGraphID(graphID)
	if err != nil {
		return GraphRef{}, err
	}
	return GraphRef{
		ID:      slug + ":" + variant,
		Slug:    slug,
		Variant: variant,
		Path:    path,
		Nodes:   len(g.Nodes),
		Edges:   len(g.Edges),
	}, nil
}

// ForkGraph copies the GraphML file at sourceID to newID. Both ids accept the
// `<slug>[:variant]` form; when the variant is omitted, the default "actual"
// is used. Returns the new graph's reference.
//
// If newID already exists, the call fails — callers can pass `force=true` to
// overwrite (matching git branch -f semantics).
func (s *Service) ForkGraph(sourceID, newID string, force bool) (GraphRef, error) {
	src, err := s.resolvePath(sourceID)
	if err != nil {
		return GraphRef{}, err
	}
	dst, err := s.resolvePath(newID)
	if err != nil {
		return GraphRef{}, err
	}
	if src == dst {
		return GraphRef{}, fmt.Errorf("ForkGraph: source and destination resolve to the same path %s", src)
	}
	if _, err := os.Stat(src); err != nil {
		return GraphRef{}, fmt.Errorf("source graph %q not found at %s", sourceID, src)
	}
	if !force {
		if _, err := os.Stat(dst); err == nil {
			return GraphRef{}, fmt.Errorf("target graph %q already exists at %s (pass force=true to overwrite)", newID, dst)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return GraphRef{}, fmt.Errorf("ForkGraph: mkdir: %w", err)
	}
	if err := copyFile(src, dst); err != nil {
		return GraphRef{}, fmt.Errorf("ForkGraph: copy: %w", err)
	}
	_ = s.Logger.Append(MutationRecord{
		Tool:    "graph_fork",
		GraphID: newID,
		Args:    map[string]any{"source_id": sourceID, "new_id": newID, "force": force},
		Result:  map[string]any{"path": dst},
	})
	slug, variant, err := splitGraphID(newID)
	if err != nil {
		return GraphRef{}, err
	}
	g, _ := Load(dst)
	out := GraphRef{ID: slug + ":" + variant, Slug: slug, Variant: variant, Path: dst}
	if g != nil {
		out.Nodes = len(g.Nodes)
		out.Edges = len(g.Edges)
	}
	return out, nil
}

// MergeResult describes the outcome of a graph_merge call.
//
// Strategy is currently fixed at "union" (a join on (node_id, edge_key) that
// keeps the destination's attributes on conflict). Conflicts is populated only
// when strategy="strict": any (id) collision aborts the merge.
type MergeResult struct {
	GraphRef
	NodesAdded     int      `json:"nodes_added"`
	EdgesAdded     int      `json:"edges_added"`
	NodeConflicts  []string `json:"node_conflicts,omitempty"`
	EdgeConflicts  []string `json:"edge_conflicts,omitempty"`
	Strategy       string   `json:"strategy"`
	ConflictPolicy string   `json:"conflict_policy"`
}

// MergeGraphs combines graphB into graphA and writes the result to outID
// (defaulting to graphA when outID is empty). The default strategy is "union":
//
//   - For each node in B not in A, the node is added.
//   - For each node in both A and B, A's attributes win; B's attributes are
//     merged only for keys A does not set.
//   - For each edge in B not in A (matched by (from,to,kind)), the edge is
//     added.
//
// Strategy "strict" aborts with conflict lists if any (node_id) overlaps.
func (s *Service) MergeGraphs(aID, bID, outID, strategy string) (MergeResult, error) {
	if strategy == "" {
		strategy = "union"
	}
	if outID == "" {
		outID = aID
	}
	if strategy != "union" && strategy != "strict" {
		return MergeResult{}, fmt.Errorf("MergeGraphs: unknown strategy %q (want: union|strict)", strategy)
	}

	lk := s.graphLock(outID)
	lk.Lock()
	defer lk.Unlock()

	a, err := s.LoadGraph(aID)
	if err != nil {
		return MergeResult{}, err
	}
	b, err := s.LoadGraph(bID)
	if err != nil {
		return MergeResult{}, err
	}

	res := MergeResult{Strategy: strategy, ConflictPolicy: "keep_destination"}
	if strategy == "strict" {
		var nodeConflicts []string
		for _, n := range b.Nodes {
			if a.HasNode(n.ID) {
				nodeConflicts = append(nodeConflicts, n.ID)
			}
		}
		if len(nodeConflicts) > 0 {
			res.NodeConflicts = nodeConflicts
			return res, fmt.Errorf("MergeGraphs: strict strategy aborted: %d node id conflicts", len(nodeConflicts))
		}
	}

	// Merge nodes.
	for _, n := range b.Nodes {
		if a.HasNode(n.ID) {
			// Union strategy: keep A's attrs but fill in missing keys from B.
			idx := a.byID[n.ID]
			if a.Nodes[idx].Attrs == nil {
				a.Nodes[idx].Attrs = make(map[string]string)
			}
			for k, v := range n.Attrs {
				if _, taken := a.Nodes[idx].Attrs[k]; !taken {
					a.Nodes[idx].Attrs[k] = v
				}
			}
			continue
		}
		if err := a.AddNode(Node{ID: n.ID, Kind: n.Kind, Name: n.Name, Attrs: copyAttrs(n.Attrs)}); err != nil {
			return res, fmt.Errorf("MergeGraphs: add node %q: %w", n.ID, err)
		}
		res.NodesAdded++
	}
	// Merge edges (dedup on (from, to, kind)).
	edgeKey := func(e Edge) string { return e.From + "→" + e.Kind + "→" + e.To }
	existing := make(map[string]struct{}, len(a.Edges))
	for _, e := range a.Edges {
		existing[edgeKey(e)] = struct{}{}
	}
	for _, e := range b.Edges {
		k := edgeKey(e)
		if _, dup := existing[k]; dup {
			continue
		}
		if !a.HasNode(e.From) || !a.HasNode(e.To) {
			// Should not happen because we merged nodes first, but guard.
			continue
		}
		if err := a.AddEdge(Edge{From: e.From, To: e.To, Kind: e.Kind, Attrs: copyAttrs(e.Attrs)}); err != nil {
			return res, fmt.Errorf("MergeGraphs: add edge %s: %w", k, err)
		}
		existing[k] = struct{}{}
		res.EdgesAdded++
	}

	// Persist to the requested output id (which may equal aID).
	if err := s.SaveGraph(outID, a); err != nil {
		return res, fmt.Errorf("MergeGraphs: save: %w", err)
	}
	_ = s.Logger.Append(MutationRecord{
		Tool:    "graph_merge",
		GraphID: outID,
		Args:    map[string]any{"a": aID, "b": bID, "strategy": strategy},
		Result:  map[string]any{"nodes_added": res.NodesAdded, "edges_added": res.EdgesAdded},
	})

	slug, variant, err := splitGraphID(outID)
	if err != nil {
		return res, err
	}
	path, _ := s.resolvePath(outID)
	res.GraphRef = GraphRef{
		ID:      slug + ":" + variant,
		Slug:    slug,
		Variant: variant,
		Path:    path,
		Nodes:   len(a.Nodes),
		Edges:   len(a.Edges),
	}
	return res, nil
}

// NodeDiff describes a single structural change between two graphs.
type NodeDiff struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind,omitempty"`
	Name      string            `json:"name,omitempty"`
	AttrsDiff map[string][2]any `json:"attrs_diff,omitempty"` // present only for changed
}

// EdgeDiff describes a single edge change.
type EdgeDiff struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

// GraphDiff is the structured delta produced by graph_diff.
type GraphDiff struct {
	A     string `json:"a"`
	B     string `json:"b"`
	Nodes struct {
		Added   []NodeDiff `json:"added"`
		Removed []NodeDiff `json:"removed"`
		Changed []NodeDiff `json:"changed"`
	} `json:"nodes"`
	Edges struct {
		Added   []EdgeDiff `json:"added"`
		Removed []EdgeDiff `json:"removed"`
	} `json:"edges"`
	Summary struct {
		NodesAdded   int `json:"nodes_added"`
		NodesRemoved int `json:"nodes_removed"`
		NodesChanged int `json:"nodes_changed"`
		EdgesAdded   int `json:"edges_added"`
		EdgesRemoved int `json:"edges_removed"`
	} `json:"summary"`
}

// DiffGraphs computes the structural delta from graphA to graphB. A node is
// "changed" when it exists in both graphs but at least one attribute (or
// kind/name) differs.
//
// Edges are matched on (from, to, kind). Attribute differences on edges are
// not surfaced — the typical pre-merge review question is "did the edge
// shape change?", which is captured by add/remove sets.
func (s *Service) DiffGraphs(aID, bID string) (GraphDiff, error) {
	a, err := s.LoadGraph(aID)
	if err != nil {
		return GraphDiff{}, err
	}
	b, err := s.LoadGraph(bID)
	if err != nil {
		return GraphDiff{}, err
	}
	d := GraphDiff{A: aID, B: bID}
	d.Nodes.Added = make([]NodeDiff, 0)
	d.Nodes.Removed = make([]NodeDiff, 0)
	d.Nodes.Changed = make([]NodeDiff, 0)
	d.Edges.Added = make([]EdgeDiff, 0)
	d.Edges.Removed = make([]EdgeDiff, 0)

	aByID := make(map[string]Node, len(a.Nodes))
	for _, n := range a.Nodes {
		aByID[n.ID] = n
	}
	bByID := make(map[string]Node, len(b.Nodes))
	for _, n := range b.Nodes {
		bByID[n.ID] = n
	}
	// Added (in B, not in A).
	for _, n := range b.Nodes {
		if _, ok := aByID[n.ID]; !ok {
			d.Nodes.Added = append(d.Nodes.Added, NodeDiff{ID: n.ID, Kind: n.Kind, Name: n.Name})
		}
	}
	// Removed (in A, not in B).
	for _, n := range a.Nodes {
		if _, ok := bByID[n.ID]; !ok {
			d.Nodes.Removed = append(d.Nodes.Removed, NodeDiff{ID: n.ID, Kind: n.Kind, Name: n.Name})
		}
	}
	// Changed.
	for _, an := range a.Nodes {
		bn, ok := bByID[an.ID]
		if !ok {
			continue
		}
		attrsDiff := diffAttrs(an.Attrs, bn.Attrs)
		// Skip purely-cosmetic deltas in the archmotif-internal attribute
		// `archmotif_id` (it always mirrors the node id, so it can't change).
		delete(attrsDiff, "archmotif_id")
		kindChanged := an.Kind != bn.Kind
		nameChanged := an.Name != bn.Name
		if !kindChanged && !nameChanged && len(attrsDiff) == 0 {
			continue
		}
		nd := NodeDiff{ID: an.ID, Kind: bn.Kind, Name: bn.Name, AttrsDiff: attrsDiff}
		if kindChanged {
			nd.AttrsDiff["__kind"] = [2]any{an.Kind, bn.Kind}
		}
		if nameChanged {
			nd.AttrsDiff["__name"] = [2]any{an.Name, bn.Name}
		}
		d.Nodes.Changed = append(d.Nodes.Changed, nd)
	}
	sort.SliceStable(d.Nodes.Added, func(i, j int) bool { return d.Nodes.Added[i].ID < d.Nodes.Added[j].ID })
	sort.SliceStable(d.Nodes.Removed, func(i, j int) bool { return d.Nodes.Removed[i].ID < d.Nodes.Removed[j].ID })
	sort.SliceStable(d.Nodes.Changed, func(i, j int) bool { return d.Nodes.Changed[i].ID < d.Nodes.Changed[j].ID })

	edgeKey := func(e Edge) string { return e.From + "→" + e.Kind + "→" + e.To }
	aEdges := make(map[string]Edge, len(a.Edges))
	for _, e := range a.Edges {
		aEdges[edgeKey(e)] = e
	}
	bEdges := make(map[string]Edge, len(b.Edges))
	for _, e := range b.Edges {
		bEdges[edgeKey(e)] = e
	}
	for k, e := range bEdges {
		if _, ok := aEdges[k]; !ok {
			d.Edges.Added = append(d.Edges.Added, EdgeDiff{From: e.From, To: e.To, Kind: e.Kind})
		}
	}
	for k, e := range aEdges {
		if _, ok := bEdges[k]; !ok {
			d.Edges.Removed = append(d.Edges.Removed, EdgeDiff{From: e.From, To: e.To, Kind: e.Kind})
		}
	}
	sort.SliceStable(d.Edges.Added, func(i, j int) bool {
		if d.Edges.Added[i].From != d.Edges.Added[j].From {
			return d.Edges.Added[i].From < d.Edges.Added[j].From
		}
		return d.Edges.Added[i].To < d.Edges.Added[j].To
	})
	sort.SliceStable(d.Edges.Removed, func(i, j int) bool {
		if d.Edges.Removed[i].From != d.Edges.Removed[j].From {
			return d.Edges.Removed[i].From < d.Edges.Removed[j].From
		}
		return d.Edges.Removed[i].To < d.Edges.Removed[j].To
	})

	d.Summary.NodesAdded = len(d.Nodes.Added)
	d.Summary.NodesRemoved = len(d.Nodes.Removed)
	d.Summary.NodesChanged = len(d.Nodes.Changed)
	d.Summary.EdgesAdded = len(d.Edges.Added)
	d.Summary.EdgesRemoved = len(d.Edges.Removed)
	return d, nil
}

func diffAttrs(a, b map[string]string) map[string][2]any {
	out := make(map[string][2]any)
	for k, av := range a {
		if bv, ok := b[k]; !ok {
			out[k] = [2]any{av, nil}
		} else if bv != av {
			out[k] = [2]any{av, bv}
		}
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			out[k] = [2]any{nil, bv}
		}
	}
	return out
}

// ErrInvalidGraphID is returned for graph_ids that fail validation. The
// concrete cause is wrapped in the error message; callers should treat all
// invalid-id errors as user-input failures (4xx, not 5xx).
var ErrInvalidGraphID = errors.New("invalid graph_id")

// validateGraphID rejects empty ids and ids containing path-traversal segments
// (".." anywhere, including hidden inside `foo/../bar`). Slug() alone is not
// sufficient: it allows `.` so `..` slips through to filepath.Join and can
// escape the workspace root.
func validateGraphID(graphID string) error {
	if graphID == "" {
		return fmt.Errorf("%w: graph_id is required", ErrInvalidGraphID)
	}
	// Reject the slug AND variant halves independently.
	parts := []string{graphID}
	if idx := strings.LastIndex(graphID, ":"); idx >= 0 {
		parts = []string{graphID[:idx], graphID[idx+1:]}
	}
	for _, p := range parts {
		// Split on both `/` and `\` so neither separator can sneak `..` past us.
		fields := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
		for _, seg := range fields {
			if seg == ".." {
				return fmt.Errorf("%w: %q contains path-traversal segment %q", ErrInvalidGraphID, graphID, seg)
			}
		}
	}
	return nil
}

func splitGraphID(graphID string) (slug, variant string, err error) {
	if err := validateGraphID(graphID); err != nil {
		return "", "", err
	}
	slug = graphID
	variant = "actual"
	if idx := strings.LastIndex(graphID, ":"); idx >= 0 {
		slug = graphID[:idx]
		variant = graphID[idx+1:]
	}
	return Slug(slug), Slug(variant), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	tmp = nil
	return os.Rename(tmpName, dst)
}

// graphHash computes a stable content hash for the graph at graphID. Used by
// the metric cache key.
func (s *Service) graphHash(graphID string) (string, error) {
	path, err := s.resolvePath(graphID)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
