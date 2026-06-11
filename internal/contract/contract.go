// Package contract implements the graph-agnostic metrics + policy engine
// described in docs/prd/archmotif-graph-metrics-library.md. It operates purely
// on a graphmlx.Graph (nodes with attrs, directed edges) and never inspects
// source code, so the same engine works on any GraphML graph.
package contract

import (
	"fmt"
	"sort"

	"github.com/kgatilin/archmotif/internal/graphmlx"
	"gonum.org/v1/gonum/mat"
)

// Report is the structured output of Analyze — the primary machine-readable
// contract (R6 in the PRD).
type Report struct {
	Nodes       int                `json:"nodes"`
	Edges       int                `json:"edges"`
	Lambda2     float64            `json:"lambda2"`     // algebraic connectivity
	Components   int               `json:"components"`  // weakly-connected components
	Modularity  float64            `json:"modularity"`  // of the supplied partition (0 if none)
	Layering    float64            `json:"layering"`    // 1 - backedges/edges over the condensation DAG
	Cycles      [][]string         `json:"cycles"`      // non-trivial strongly-connected components
	GodNodes    []GodNode          `json:"god_nodes"`   // fan-in/out outliers
	Coupling    map[string]Coupling `json:"coupling"`   // per-node afferent/efferent/instability
}

// Coupling is Robert Martin's per-node coupling triple.
type Coupling struct {
	Afferent    int     `json:"ca"` // incoming deps (who depends on me)
	Efferent    int     `json:"ce"` // outgoing deps (who I depend on)
	Instability float64 `json:"i"`  // Ce / (Ca+Ce); 0 = stable sink, 1 = unstable source
}

// GodNode flags a node whose fan-in or fan-out is a statistical outlier.
type GodNode struct {
	ID     string `json:"id"`
	FanIn  int    `json:"fan_in"`
	FanOut int    `json:"fan_out"`
	Reason string `json:"reason"`
}

// Analyze runs the default structural metric suite over g.
func Analyze(g *graphmlx.Graph) Report {
	ids := nodeIDs(g)
	r := Report{
		Nodes:    len(ids),
		Edges:    len(g.Edges),
		Coupling: map[string]Coupling{},
	}
	r.Lambda2, r.Components = lambda2(g, ids)
	r.Cycles = cycles(g, ids)
	r.Layering = layering(g, ids)
	r.GodNodes = godNodes(g, ids)
	r.Modularity = modularityByAttr(g, ids, "group")
	for _, id := range ids {
		ca := len(g.IncomingTo(id))
		ce := len(g.OutgoingFrom(id))
		inst := 0.0
		if ca+ce > 0 {
			inst = float64(ce) / float64(ca+ce)
		}
		r.Coupling[id] = Coupling{Afferent: ca, Efferent: ce, Instability: inst}
	}
	return r
}

// Calculate runs a single named metric and returns its result.
func Calculate(name string, g *graphmlx.Graph) (any, error) {
	ids := nodeIDs(g)
	switch name {
	case "lambda2":
		v, _ := lambda2(g, ids)
		return v, nil
	case "components":
		_, c := lambda2(g, ids)
		return c, nil
	case "cycles":
		return cycles(g, ids), nil
	case "layering":
		return layering(g, ids), nil
	case "modularity":
		m := modularityByAttr(g, ids, "group")
		if m == 0 {
			m = modularityByAttr(g, ids, "domain")
		}
		return m, nil
	case "god-nodes":
		return godNodes(g, ids), nil
	case "coupling":
		out := map[string]Coupling{}
		for _, id := range ids {
			ca := len(g.IncomingTo(id))
			ce := len(g.OutgoingFrom(id))
			inst := 0.0
			if ca+ce > 0 {
				inst = float64(ce) / float64(ca+ce)
			}
			out[id] = Coupling{Afferent: ca, Efferent: ce, Instability: inst}
		}
		return out, nil
	case "semantic-clusters":
		return semanticClusters(g, ids)
	default:
		return nil, fmt.Errorf("unknown metric %q (have: lambda2, components, cycles, layering, god-nodes, coupling, semantic-clusters)", name)
	}
}

// ---- structural metrics -------------------------------------------------

func nodeIDs(g *graphmlx.Graph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	return ids
}

// lambda2 returns the algebraic connectivity (2nd-smallest Laplacian
// eigenvalue) of the undirected projection, and the number of zero
// eigenvalues (= weakly-connected components).
func lambda2(g *graphmlx.Graph, ids []string) (float64, int) {
	n := len(ids)
	if n < 2 {
		return 0, n
	}
	idx := map[string]int{}
	for i, id := range ids {
		idx[id] = i
	}
	adj := mat.NewSymDense(n, nil)
	for _, e := range g.Edges {
		fi, ok1 := idx[e.From]
		ti, ok2 := idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		adj.SetSym(fi, ti, 1)
	}
	lap := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		deg := 0.0
		for j := 0; j < n; j++ {
			if i != j {
				deg += adj.At(i, j)
			}
		}
		for j := 0; j < n; j++ {
			if i == j {
				lap.SetSym(i, j, deg)
			} else {
				lap.SetSym(i, j, -adj.At(i, j))
			}
		}
	}
	var es mat.EigenSym
	if ok := es.Factorize(lap, false); !ok {
		return 0, 0
	}
	vals := es.Values(nil)
	sort.Float64s(vals)
	comps := 0
	for _, v := range vals {
		if v < 1e-9 {
			comps++
		}
	}
	if len(vals) < 2 {
		return 0, comps
	}
	l2 := vals[1]
	if l2 < 0 {
		l2 = 0
	}
	return l2, comps
}

// cycles returns the non-trivial strongly-connected components (size > 1)
// via Tarjan's algorithm — these are dependency cycles.
func cycles(g *graphmlx.Graph, ids []string) [][]string {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if e.From != e.To {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	var sccs [][]string
	var strongconnect func(v string)
	strongconnect = func(v string) {
		index[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, seen := index[w]; !seen {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if index[w] < low[v] {
					low[v] = index[w]
				}
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				sccs = append(sccs, comp)
			}
		}
	}
	for _, id := range ids {
		if _, seen := index[id]; !seen {
			strongconnect(id)
		}
	}
	sort.Slice(sccs, func(i, j int) bool { return sccs[i][0] < sccs[j][0] })
	return sccs
}

// layering returns 1 - (backedges / edges): the fraction of edges that respect
// a topological layering. 1.0 = perfectly layered DAG; lower = more upward
// (cycle/back) edges. The right structural fitness for hub-shaped graphs.
func layering(g *graphmlx.Graph, ids []string) float64 {
	if len(g.Edges) == 0 {
		return 1
	}
	order, ok := topoOrder(g, ids)
	if !ok {
		// graph has cycles; rank within the condensation instead
		order = condensationOrder(g, ids)
	}
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i
	}
	back := 0
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		if rank[e.To] < rank[e.From] {
			back++
		}
	}
	return 1 - float64(back)/float64(len(g.Edges))
}

// topoOrder returns a topological order of the nodes (Kahn); ok=false if the
// graph has a cycle.
func topoOrder(g *graphmlx.Graph, ids []string) ([]string, bool) {
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, id := range ids {
		indeg[id] = 0
	}
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}
	var queue []string
	for _, id := range ids {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	var order []string
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		order = append(order, v)
		var next []string
		for _, w := range adj[v] {
			indeg[w]--
			if indeg[w] == 0 {
				next = append(next, w)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}
	return order, len(order) == len(ids)
}

// condensationOrder ranks nodes by the topological order of their SCC, so a
// layering score is still meaningful on cyclic graphs.
func condensationOrder(g *graphmlx.Graph, ids []string) []string {
	comp := sccOf(g, ids)
	// build condensation adjacency
	cadj := map[int]map[int]bool{}
	cindeg := map[int]int{}
	comps := map[int]bool{}
	for _, id := range ids {
		comps[comp[id]] = true
	}
	for c := range comps {
		cadj[c] = map[int]bool{}
		cindeg[c] = 0
	}
	for _, e := range g.Edges {
		a, b := comp[e.From], comp[e.To]
		if a != b && !cadj[a][b] {
			cadj[a][b] = true
			cindeg[b]++
		}
	}
	var queue []int
	for c := range comps {
		if cindeg[c] == 0 {
			queue = append(queue, c)
		}
	}
	sort.Ints(queue)
	var corder []int
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		corder = append(corder, v)
		var next []int
		for w := range cadj[v] {
			cindeg[w]--
			if cindeg[w] == 0 {
				next = append(next, w)
			}
		}
		sort.Ints(next)
		queue = append(queue, next...)
	}
	crank := map[int]int{}
	for i, c := range corder {
		crank[c] = i
	}
	order := append([]string{}, ids...)
	sort.Slice(order, func(i, j int) bool { return crank[comp[order[i]]] < crank[comp[order[j]]] })
	return order
}

// sccOf maps each node to a component id.
func sccOf(g *graphmlx.Graph, ids []string) map[string]int {
	comp := map[string]int{}
	cid := 0
	for _, c := range cycles(g, ids) {
		for _, id := range c {
			comp[id] = cid
		}
		cid++
	}
	for _, id := range ids {
		if _, ok := comp[id]; !ok {
			comp[id] = cid
			cid++
		}
	}
	return comp
}

// godNodes flags nodes whose fan-in or fan-out exceeds mean + 2*stddev.
func godNodes(g *graphmlx.Graph, ids []string) []GodNode {
	in := map[string]int{}
	out := map[string]int{}
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		out[e.From]++
		in[e.To]++
	}
	meanIn, sdIn := meanStd(in, ids)
	meanOut, sdOut := meanStd(out, ids)
	var res []GodNode
	for _, id := range ids {
		var reasons string
		if sdIn > 0 && float64(in[id]) > meanIn+2*sdIn {
			reasons = fmt.Sprintf("fan-in %d >> mean %.1f (shared sink / god dependency)", in[id], meanIn)
		}
		if sdOut > 0 && float64(out[id]) > meanOut+2*sdOut {
			if reasons != "" {
				reasons += "; "
			}
			reasons += fmt.Sprintf("fan-out %d >> mean %.1f (does too much)", out[id], meanOut)
		}
		if reasons != "" {
			res = append(res, GodNode{ID: id, FanIn: in[id], FanOut: out[id], Reason: reasons})
		}
	}
	return res
}

func meanStd(m map[string]int, ids []string) (float64, float64) {
	n := float64(len(ids))
	if n == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, id := range ids {
		sum += float64(m[id])
	}
	mean := sum / n
	varr := 0.0
	for _, id := range ids {
		d := float64(m[id]) - mean
		varr += d * d
	}
	return mean, sqrt(varr / n)
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 40; i++ {
		z = (z + x/z) / 2
	}
	return z
}
