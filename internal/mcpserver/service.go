package mcpserver

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Service is the storage-aware layer that backs the MCP tool handlers. It
// resolves graph_id -> on-disk path, performs reads/writes through the in-
// memory Graph, and appends to the mutation log.
//
// All exposed methods are safe for concurrent use. Writes serialise on a
// per-graph mutex.
//
// TODO(#56-followup): introduce a SQLite index over graphs/* for fast lookup
// and cross-graph queries. For v1 we walk the in-memory copy on every call —
// fine for the experiment's small graphs (~hundreds of nodes).
type Service struct {
	Root   string
	Logger *MutationLogger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-graph write locks
}

// NewService constructs a Service rooted at the given workspace dir (typically
// ~/.archmotif). The directory is created lazily on first write.
func NewService(root string) *Service {
	return &Service{
		Root:   root,
		Logger: NewMutationLogger(root),
		locks:  make(map[string]*sync.Mutex),
	}
}

// resolvePath maps a graph_id to its on-disk GraphML path. The v1 layout is
// graphs/<slug>/actual.graphml; callers can also pass a graph_id with an
// explicit suffix ("foo:target") to address a non-default variant.
func (s *Service) resolvePath(graphID string) (string, error) {
	if err := validateGraphID(graphID); err != nil {
		return "", err
	}
	variant := "actual"
	slug := graphID
	if idx := strings.LastIndex(graphID, ":"); idx >= 0 {
		slug = graphID[:idx]
		variant = graphID[idx+1:]
	}
	slug = Slug(slug)
	variant = Slug(variant)
	return filepath.Join(s.Root, "graphs", slug, variant+".graphml"), nil
}

// LoadGraph reads the graph referenced by graphID from disk.
func (s *Service) LoadGraph(graphID string) (*Graph, error) {
	path, err := s.resolvePath(graphID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("graph %q not found at %s", graphID, path)
	}
	return Load(path)
}

// SaveGraph writes the graph back to disk under graphID.
func (s *Service) SaveGraph(graphID string, g *Graph) error {
	path, err := s.resolvePath(graphID)
	if err != nil {
		return err
	}
	return g.Save(path)
}

// graphLock returns the per-graph mutex for serialising writes.
func (s *Service) graphLock(graphID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[graphID]; ok {
		return l
	}
	l := &sync.Mutex{}
	s.locks[graphID] = l
	return l
}

// mutate is the canonical write entry point. It (a) acquires the per-graph
// lock, (b) loads the graph, (c) applies fn, (d) appends to the mutation log,
// and (e) writes the graph back atomically. If fn returns an error, the log
// is NOT appended and the on-disk state is untouched.
func (s *Service) mutate(graphID, tool string, args map[string]any, fn func(g *Graph) (map[string]any, error)) (map[string]any, error) {
	lk := s.graphLock(graphID)
	lk.Lock()
	defer lk.Unlock()

	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	result, err := fn(g)
	if err != nil {
		return nil, err
	}
	if err := s.SaveGraph(graphID, g); err != nil {
		return nil, err
	}
	rec := MutationRecord{
		Tool:    tool,
		GraphID: graphID,
		Args:    args,
		Result:  result,
	}
	if err := s.Logger.Append(rec); err != nil {
		return nil, fmt.Errorf("write mutation log: %w", err)
	}
	return result, nil
}

// ----- READ TOOLS ---------------------------------------------------------

// QueryFilter is the filter shape for graph_query. All fields are optional;
// when multiple are set, results must match ALL constraints (AND).
type QueryFilter struct {
	Kind    string `json:"kind,omitempty"`
	Tag     string `json:"tag,omitempty"`     // matched against attrs["tags"] (comma-separated)
	Name    string `json:"name,omitempty"`    // substring match on name (case-insensitive)
	Package string `json:"package,omitempty"` // substring match on attrs["package"] or qname prefix
}

// Query returns nodes that satisfy the filter. Results are sorted by ID for
// determinism.
func (s *Service) Query(graphID string, filter QueryFilter) ([]Node, error) {
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0)
	nameNeedle := strings.ToLower(filter.Name)
	pkgNeedle := strings.ToLower(filter.Package)
	for _, n := range g.Nodes {
		if filter.Kind != "" && n.Kind != filter.Kind {
			continue
		}
		if filter.Tag != "" {
			tags := n.Attrs["tags"]
			matched := false
			for _, t := range strings.Split(tags, ",") {
				if strings.TrimSpace(t) == filter.Tag {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if nameNeedle != "" && !strings.Contains(strings.ToLower(n.Name), nameNeedle) {
			continue
		}
		if pkgNeedle != "" {
			pkg := strings.ToLower(n.Attrs["package"])
			qname := strings.ToLower(n.Attrs["qname"])
			if !strings.Contains(pkg, pkgNeedle) && !strings.HasPrefix(qname, pkgNeedle) {
				continue
			}
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Neighbors returns nodes reachable from nodeID within the given depth along
// edges whose kind is in edgeKinds (empty = any kind). Depth=0 returns the
// node itself; depth=1 immediate neighbours; depth>1 expands BFS-style.
// Direction is undirected (treats edges as both-ways) — matches the typical
// "what is around this node" intuition.
func (s *Service) Neighbors(graphID, nodeID string, edgeKinds []string, depth int) ([]Node, error) {
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	if !g.HasNode(nodeID) {
		return nil, fmt.Errorf("node %q not found in graph %q", nodeID, graphID)
	}
	if depth < 0 {
		return nil, fmt.Errorf("depth must be >= 0")
	}

	kinds := make(map[string]struct{}, len(edgeKinds))
	for _, k := range edgeKinds {
		kinds[k] = struct{}{}
	}
	edgeMatch := func(kind string) bool {
		if len(kinds) == 0 {
			return true
		}
		_, ok := kinds[kind]
		return ok
	}

	seen := map[string]int{nodeID: 0}
	queue := list.New()
	queue.PushBack(nodeID)
	for queue.Len() > 0 {
		front := queue.Front()
		queue.Remove(front)
		id := front.Value.(string)
		d := seen[id]
		if d >= depth {
			continue
		}
		for _, e := range g.Edges {
			if !edgeMatch(e.Kind) {
				continue
			}
			var other string
			switch {
			case e.From == id:
				other = e.To
			case e.To == id:
				other = e.From
			default:
				continue
			}
			if _, visited := seen[other]; visited {
				continue
			}
			seen[other] = d + 1
			queue.PushBack(other)
		}
	}

	out := make([]Node, 0, len(seen))
	for id := range seen {
		if id == nodeID {
			continue
		}
		if n, ok := g.Node(id); ok {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Path returns one shortest path (sequence of node IDs) from `from` to `to`,
// optionally constrained to edges of the given kinds. If the destination is
// unreachable the returned slice is empty (no error).
//
// The walk treats edges as directed (from -> to). The MCP contract for
// "reachability" in v1 is directional; undirected variants can be added later.
func (s *Service) Path(graphID, from, to string, edgeKinds []string) ([]string, error) {
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	if !g.HasNode(from) {
		return nil, fmt.Errorf("node %q not found", from)
	}
	if !g.HasNode(to) {
		return nil, fmt.Errorf("node %q not found", to)
	}
	if from == to {
		return []string{from}, nil
	}

	kinds := make(map[string]struct{}, len(edgeKinds))
	for _, k := range edgeKinds {
		kinds[k] = struct{}{}
	}
	edgeMatch := func(kind string) bool {
		if len(kinds) == 0 {
			return true
		}
		_, ok := kinds[kind]
		return ok
	}

	// Build per-node adjacency (out-edges only — directed walk).
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		if !edgeMatch(e.Kind) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	// Determinism: sort each neighbour list so the chosen shortest path is stable.
	for k := range adj {
		sort.Strings(adj[k])
	}

	prev := map[string]string{from: ""}
	queue := list.New()
	queue.PushBack(from)
	found := false
	for queue.Len() > 0 {
		front := queue.Front()
		queue.Remove(front)
		id := front.Value.(string)
		if id == to {
			found = true
			break
		}
		for _, next := range adj[id] {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = id
			queue.PushBack(next)
		}
	}
	if !found {
		return []string{}, nil
	}
	// Reconstruct.
	path := []string{to}
	for cur := to; cur != from; {
		cur = prev[cur]
		path = append(path, cur)
	}
	// Reverse.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

// ----- WRITE TOOLS --------------------------------------------------------

// Activate bumps the activation weight on a set of nodes by `weight` (added
// to the existing weight). The weight is stored as a float64 string in the
// node's `activation` attribute.
func (s *Service) Activate(graphID string, nodeIDs []string, weight float64) (map[string]any, error) {
	args := map[string]any{"node_ids": nodeIDs, "weight": weight}
	return s.mutate(graphID, "graph_activate", args, func(g *Graph) (map[string]any, error) {
		updated := make([]map[string]any, 0, len(nodeIDs))
		for _, id := range nodeIDs {
			if !g.HasNode(id) {
				return nil, fmt.Errorf("node %q not found", id)
			}
			cur := 0.0
			if v := g.Nodes[g.byID[id]].Attrs["activation"]; v != "" {
				cur, _ = strconv.ParseFloat(v, 64)
			}
			next := cur + weight
			if err := g.UpdateNodeAttr(id, "activation", strconv.FormatFloat(next, 'f', -1, 64)); err != nil {
				return nil, err
			}
			updated = append(updated, map[string]any{"id": id, "activation": next})
		}
		return map[string]any{"updated": updated}, nil
	})
}

// AddNode adds a new node. The caller supplies an explicit id via attrs["id"];
// if omitted, the id is derived as "<kind>:<name>" or, lacking a name,
// "<kind>:<hash-of-attrs>". The new node is then loaded into the in-memory
// graph and written back to disk; the mutation is logged.
func (s *Service) AddNode(graphID, kind string, attrs map[string]string) (map[string]any, error) {
	args := map[string]any{"kind": kind, "attrs": attrs}
	return s.mutate(graphID, "graph_add_node", args, func(g *Graph) (map[string]any, error) {
		node := Node{Kind: kind, Attrs: copyAttrs(attrs)}
		if node.Attrs == nil {
			node.Attrs = make(map[string]string)
		}
		if id, ok := node.Attrs["id"]; ok && id != "" {
			node.ID = id
		} else if name := node.Attrs["name"]; name != "" {
			node.ID = kind + ":" + name
		} else {
			node.ID = kind + ":" + strconv.Itoa(len(g.Nodes))
		}
		// Mirror name onto the struct field for ergonomic queries.
		if name, ok := node.Attrs["name"]; ok {
			node.Name = name
		}
		if err := g.AddNode(node); err != nil {
			return nil, err
		}
		return map[string]any{"id": node.ID}, nil
	})
}

// AddEdge adds a new directed edge between existing nodes.
func (s *Service) AddEdge(graphID, from, to, kind string, attrs map[string]string) (map[string]any, error) {
	args := map[string]any{"from": from, "to": to, "kind": kind, "attrs": attrs}
	return s.mutate(graphID, "graph_add_edge", args, func(g *Graph) (map[string]any, error) {
		if err := g.AddEdge(Edge{From: from, To: to, Kind: kind, Attrs: copyAttrs(attrs)}); err != nil {
			return nil, err
		}
		return map[string]any{"from": from, "to": to, "kind": kind}, nil
	})
}

// UpdateWeight adjusts a node's `weight` attribute by `delta` (added). The
// attribute is stored as a float64-formatted string.
func (s *Service) UpdateWeight(graphID, nodeID string, delta float64) (map[string]any, error) {
	args := map[string]any{"node_id": nodeID, "delta": delta}
	return s.mutate(graphID, "graph_update_weight", args, func(g *Graph) (map[string]any, error) {
		if !g.HasNode(nodeID) {
			return nil, fmt.Errorf("node %q not found", nodeID)
		}
		cur := 0.0
		if v := g.Nodes[g.byID[nodeID]].Attrs["weight"]; v != "" {
			cur, _ = strconv.ParseFloat(v, 64)
		}
		next := cur + delta
		if err := g.UpdateNodeAttr(nodeID, "weight", strconv.FormatFloat(next, 'f', -1, 64)); err != nil {
			return nil, err
		}
		return map[string]any{"id": nodeID, "weight": next}, nil
	})
}
