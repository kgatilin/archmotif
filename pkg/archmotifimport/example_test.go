package archmotifimport_test

import (
	"fmt"

	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// Example demonstrates the typical external caller: build a small typed
// graph (here three packages with a cyclic dependency between two of
// them), then inspect the result to feed into a downstream metric.
//
// The ticket asks for the doc example to "run one existing metric" on
// the constructed graph. Archmotif's metric implementations currently
// live under internal/metrics and are not yet exposed via a public
// shim (a separate ticket, per the issue's "Companion follow-up"
// section). Until that shim exists, this example shows the smallest
// possible self-contained cycle detection over the constructed
// dependency edges — exactly the input shape the future pkg/metrics
// helpers will consume.
func Example() {
	b := archmotifimport.NewBuilder()
	_ = b.AddPackage("pkg:a", "domain", "")
	_ = b.AddPackage("pkg:b", "application", "")
	_ = b.AddPackage("pkg:c", "outbound_adapter", "")

	// Introduce a small dependency cycle a -> b -> a, plus c -> a.
	_ = b.AddDependency("pkg:a", "pkg:b", archmotifimport.DependencyDependsOn)
	_ = b.AddDependency("pkg:b", "pkg:a", archmotifimport.DependencyDependsOn)
	_ = b.AddDependency("pkg:c", "pkg:a", archmotifimport.DependencyDependsOn)

	g, err := b.Build()
	if err != nil {
		fmt.Println("build error:", err)
		return
	}

	fmt.Println("nodes:", g.NodeCount())
	fmt.Println("edges:", g.EdgeCount())

	// Trivially detect the a<->b 2-cycle in the constructed edge set.
	type pair struct{ from, to string }
	seen := map[pair]bool{}
	cycles := 0
	for _, e := range g.Edges() {
		if seen[pair{e.To, e.From}] {
			cycles++
		}
		seen[pair{e.From, e.To}] = true
	}
	fmt.Println("2-cycles:", cycles)

	// Output:
	// nodes: 3
	// edges: 3
	// 2-cycles: 1
}
