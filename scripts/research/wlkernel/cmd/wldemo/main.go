// Command wldemo prints WL-kernel similarity numbers for two tiny
// hand-built role-typed graphs. It is a research-only driver for
// ADR-036; the numbers it prints are quoted in the ADR's "sample
// output" section so a reader can reproduce them.
//
// Usage:
//
//	go run ./scripts/research/wlkernel/cmd/wldemo
package main

import (
	"fmt"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/scripts/research/wlkernel"
)

func main() {
	a := buildHex("A")
	b := buildHex("B")
	c := buildBroken("C")

	for iter := 0; iter <= 2; iter++ {
		ea := wlkernel.Compute(a, iter)
		eb := wlkernel.Compute(b, iter)
		ec := wlkernel.Compute(c, iter)
		fmt.Printf("iter=%d  sim(A,B)=%.3f  sim(A,C)=%.3f  |labels(A)|=%d\n",
			iter,
			wlkernel.Cosine(ea, eb),
			wlkernel.Cosine(ea, ec),
			len(ea.Counts),
		)
	}
}

func buildHex(prefix string) *mgraph.Graph {
	g := mgraph.New()
	addNode(g, prefix+".Entity", "domain_entity")
	addNode(g, prefix+".Port", "port")
	addNode(g, prefix+".Adapter", "outbound_adapter")
	addEdge(g, prefix+".Adapter", prefix+".Port", mgraph.EdgeImplements)
	addEdge(g, prefix+".Adapter", prefix+".Entity", mgraph.EdgeCalls)
	return g
}

func buildBroken(prefix string) *mgraph.Graph {
	g := mgraph.New()
	addNode(g, prefix+".Entity", "domain_entity")
	addNode(g, prefix+".Port", "port")
	addNode(g, prefix+".Adapter", "outbound_adapter")
	addEdge(g, prefix+".Adapter", prefix+".Port", mgraph.EdgeCalls)
	addEdge(g, prefix+".Port", prefix+".Entity", mgraph.EdgeCalls)
	return g
}

func addNode(g *mgraph.Graph, id, role string) {
	_, _ = g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodeType,
		Name:  id,
		Attrs: map[string]any{"role": role},
	})
}

func addEdge(g *mgraph.Graph, from, to string, kind mgraph.EdgeKind) {
	if _, err := g.AddEdge(mgraph.Edge{From: from, To: to, Kind: kind}); err != nil {
		panic(err)
	}
}
