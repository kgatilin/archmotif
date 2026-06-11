package contract

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// Partition maps node ID -> group label.
type Partition map[string]string

// PartitionBy groups nodes by the value of a node attribute (the "declared"
// partition source in the PRD). Nodes lacking the attribute fall in "".
func PartitionBy(g *graphmlx.Graph, attr string) Partition {
	p := Partition{}
	for _, n := range g.Nodes {
		switch attr {
		case "id":
			p[n.ID] = n.ID
		case "label":
			p[n.ID] = n.Label
		case "kind":
			p[n.ID] = n.Kind
		default:
			p[n.ID] = n.Attrs[attr]
		}
	}
	return p
}

// QuotientGraph is the macro graph: one node per group, edges aggregated with a
// weight = number of underlying cross-group edges. SelfCycles flags groups that
// are strongly connected internally (a back-edge inside the group).
type QuotientGraph struct {
	Groups  []QuotientNode `json:"groups"`
	Edges   []QuotientEdge `json:"edges"`
	Acyclic bool           `json:"acyclic"` // true if the macro graph is a DAG
}

type QuotientNode struct {
	Group   string   `json:"group"`
	Members []string `json:"members"`
	FanIn   int      `json:"fan_in"`
	FanOut  int      `json:"fan_out"`
}

type QuotientEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Weight int    `json:"weight"`
}

// Quotient collapses g by the partition into a macro graph.
func Quotient(g *graphmlx.Graph, p Partition) QuotientGraph {
	members := map[string][]string{}
	for _, n := range g.Nodes {
		members[p[n.ID]] = append(members[p[n.ID]], n.ID)
	}
	type pair struct{ from, to string }
	w := map[pair]int{}
	for _, e := range g.Edges {
		a, b := p[e.From], p[e.To]
		if a == b {
			continue
		}
		w[pair{a, b}]++
	}
	var groups []string
	for gname := range members {
		groups = append(groups, gname)
	}
	sort.Strings(groups)
	q := QuotientGraph{}
	fanIn := map[string]int{}
	fanOut := map[string]int{}
	for pr, c := range w {
		fanOut[pr.from] += c
		fanIn[pr.to] += c
	}
	for _, gn := range groups {
		mem := members[gn]
		sort.Strings(mem)
		q.Groups = append(q.Groups, QuotientNode{Group: gn, Members: mem, FanIn: fanIn[gn], FanOut: fanOut[gn]})
	}
	var edges []QuotientEdge
	for pr, c := range w {
		edges = append(edges, QuotientEdge{From: pr.from, To: pr.to, Weight: c})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	q.Edges = edges
	q.Acyclic = quotientAcyclic(groups, edges)
	return q
}

func quotientAcyclic(groups []string, edges []QuotientEdge) bool {
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, g := range groups {
		indeg[g] = 0
	}
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}
	var queue []string
	for _, g := range groups {
		if indeg[g] == 0 {
			queue = append(queue, g)
		}
	}
	seen := 0
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		seen++
		for _, w := range adj[v] {
			indeg[w]--
			if indeg[w] == 0 {
				queue = append(queue, w)
			}
		}
	}
	return seen == len(groups)
}

// modularityByAttr computes Newman modularity of the partition induced by attr
// over the undirected projection. 0 if no attr present.
func modularityByAttr(g *graphmlx.Graph, ids []string, attr string) float64 {
	p := PartitionBy(g, attr)
	any := false
	for _, v := range p {
		if v != "" {
			any = true
			break
		}
	}
	if !any {
		return 0
	}
	deg := map[string]int{}
	m := 0.0
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		deg[e.From]++
		deg[e.To]++
		m++
	}
	if m == 0 {
		return 0
	}
	// adjacency presence (undirected)
	adj := map[[2]string]bool{}
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		adj[[2]string{e.From, e.To}] = true
		adj[[2]string{e.To, e.From}] = true
	}
	q := 0.0
	for _, i := range ids {
		for _, j := range ids {
			if p[i] != p[j] {
				continue
			}
			a := 0.0
			if adj[[2]string{i, j}] {
				a = 1
			}
			q += a - float64(deg[i]*deg[j])/(2*m)
		}
	}
	return q / (2 * m)
}

// ---- semantic clustering (feature-based metric) -------------------------

func parseVec(s string) []float64 {
	if s == "" {
		return nil
	}
	fields := strings.Fields(strings.ReplaceAll(s, ",", " "))
	v := make([]float64, 0, len(fields))
	for _, f := range fields {
		x, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil
		}
		v = append(v, x)
	}
	return v
}

// semanticClusters groups nodes by their `vec` attribute via k-means, picking k
// in [2..min(8,n-1)] by best average silhouette.
func semanticClusters(g *graphmlx.Graph, ids []string) (map[string][]string, error) {
	var have []string
	var vecs [][]float64
	for _, id := range ids {
		n, _ := g.Node(id)
		v := parseVec(n.Attrs["vec"])
		if v != nil {
			have = append(have, id)
			vecs = append(vecs, v)
		}
	}
	if len(have) < 2 {
		return nil, fmt.Errorf("semantic-clusters needs a `vec` attribute on at least 2 nodes (found %d); run `embed` first", len(have))
	}
	bestK, bestSil, bestLabels := 0, -2.0, []int(nil)
	maxK := len(have) - 1
	if maxK > 8 {
		maxK = 8
	}
	for k := 2; k <= maxK; k++ {
		labels := kmeans(vecs, k)
		sil := silhouette(vecs, labels)
		if sil > bestSil {
			bestSil, bestK, bestLabels = sil, k, labels
		}
	}
	_ = bestK
	out := map[string][]string{}
	for i, id := range have {
		key := fmt.Sprintf("cluster-%d", bestLabels[i])
		out[key] = append(out[key], id)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}

func dist2(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		d := a[i] - b[i]
		s += d * d
	}
	return s
}

// kmeans is deterministic Lloyd's with k-means++-ish seeding by spread index.
func kmeans(vecs [][]float64, k int) []int {
	n := len(vecs)
	cents := make([][]float64, k)
	for c := 0; c < k; c++ {
		idx := (c * n) / k // deterministic spread seeding
		cents[c] = append([]float64{}, vecs[idx]...)
	}
	labels := make([]int, n)
	for iter := 0; iter < 50; iter++ {
		changed := false
		for i, v := range vecs {
			best, bd := 0, dist2(v, cents[0])
			for c := 1; c < k; c++ {
				if d := dist2(v, cents[c]); d < bd {
					best, bd = c, d
				}
			}
			if labels[i] != best {
				labels[i] = best
				changed = true
			}
		}
		sums := make([][]float64, k)
		counts := make([]int, k)
		dim := len(vecs[0])
		for c := 0; c < k; c++ {
			sums[c] = make([]float64, dim)
		}
		for i, v := range vecs {
			c := labels[i]
			counts[c]++
			for d := 0; d < dim; d++ {
				sums[c][d] += v[d]
			}
		}
		for c := 0; c < k; c++ {
			if counts[c] == 0 {
				continue
			}
			for d := 0; d < dim; d++ {
				cents[c][d] = sums[c][d] / float64(counts[c])
			}
		}
		if !changed && iter > 0 {
			break
		}
	}
	return labels
}

func silhouette(vecs [][]float64, labels []int) float64 {
	n := len(vecs)
	if n < 2 {
		return 0
	}
	total := 0.0
	for i := 0; i < n; i++ {
		var a float64
		ac := 0
		bbest := -1.0
		byCluster := map[int]float64{}
		byCount := map[int]int{}
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			d := sqrt(dist2(vecs[i], vecs[j]))
			if labels[j] == labels[i] {
				a += d
				ac++
			} else {
				byCluster[labels[j]] += d
				byCount[labels[j]]++
			}
		}
		if ac > 0 {
			a /= float64(ac)
		}
		for c, sum := range byCluster {
			avg := sum / float64(byCount[c])
			if bbest < 0 || avg < bbest {
				bbest = avg
			}
		}
		if bbest < 0 {
			continue
		}
		mx := a
		if bbest > mx {
			mx = bbest
		}
		if mx > 0 {
			total += (bbest - a) / mx
		}
	}
	return total / float64(n)
}

// ---- GraphML writer (R1: round-trip) ------------------------------------

// WriteGraphML emits g as a minimal GraphML document. Attribute keys are
// declared per attr name; vec and group survive the round-trip.
func WriteGraphML(w io.Writer, g *graphmlx.Graph) error {
	nodeKeys := map[string]bool{}
	for _, n := range g.Nodes {
		for k := range n.Attrs {
			nodeKeys[k] = true
		}
	}
	edgeKeys := map[string]bool{}
	for _, e := range g.Edges {
		for k := range e.Attrs {
			edgeKeys[k] = true
		}
	}
	var nodeKeyList []string
	for k := range nodeKeys {
		nodeKeyList = append(nodeKeyList, k)
	}
	sort.Strings(nodeKeyList)
	var edgeKeyList []string
	for k := range edgeKeys {
		edgeKeyList = append(edgeKeyList, k)
	}
	sort.Strings(edgeKeyList)
	edgeKeyID := map[string]string{}
	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintln(w, `<graphml xmlns="http://graphml.graphdrawing.org/xmlns">`)
	for _, k := range nodeKeyList {
		fmt.Fprintf(w, "  <key id=%q for=\"node\" attr.name=%q attr.type=\"string\"/>\n", k, k)
	}
	for _, k := range edgeKeyList {
		id := k
		if nodeKeys[k] {
			id = "e_" + k
		}
		edgeKeyID[k] = id
		fmt.Fprintf(w, "  <key id=%q for=\"edge\" attr.name=%q attr.type=\"string\"/>\n", id, k)
	}
	dir := "directed"
	if !g.Directed {
		dir = "undirected"
	}
	fmt.Fprintf(w, "  <graph edgedefault=%q>\n", dir)
	for _, n := range g.Nodes {
		fmt.Fprintf(w, "    <node id=%q>", xmlEsc(n.ID))
		ks := make([]string, 0, len(n.Attrs))
		for k := range n.Attrs {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Fprintf(w, "<data key=%q>%s</data>", k, xmlEsc(n.Attrs[k]))
		}
		fmt.Fprintln(w, "</node>")
	}
	for i, e := range g.Edges {
		fmt.Fprintf(w, "    <edge id=\"e%d\" source=%q target=%q>", i, xmlEsc(e.From), xmlEsc(e.To))
		ks := make([]string, 0, len(e.Attrs))
		for k := range e.Attrs {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Fprintf(w, "<data key=%q>%s</data>", edgeKeyID[k], xmlEsc(e.Attrs[k]))
		}
		fmt.Fprintln(w, "</edge>")
	}
	fmt.Fprintln(w, "  </graph>")
	fmt.Fprintln(w, "</graphml>")
	return nil
}

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
