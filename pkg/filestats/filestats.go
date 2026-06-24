// Package filestats surfaces structurally overloaded source files — those
// carrying an outlier number of top-level declarations. It reads the file
// nodes and file→symbol contains edges of an archmotif graph, counts the
// declarations per file, and flags outliers against the median.
//
// Only declarations attributed to a file are counted (types and functions);
// methods and fields are not file-tagged in the model, so they do not
// contribute. This is a structural-mass lens — "which files do too much" —
// complementary to the dependency-flow lenses (components, trophic, spectral).
package filestats

import (
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// Graph is a type alias for archmotif's graph type.
type Graph = archmotifimport.Graph

const (
	// defaultOutlierRatio flags a file whose declaration count is at least
	// this multiple of the median file in scope.
	defaultOutlierRatio = 3.0
	// defaultOutlierFloor is the absolute minimum count to be an outlier, so a
	// uniformly tiny package does not flag a merely above-average file.
	defaultOutlierFloor = 20
)

// Options configures the analysis.
type Options struct {
	// NodeIDs restricts analysis to these file nodes. Empty means every file
	// node in the graph.
	NodeIDs []string
	// OutlierRatio overrides the median multiple for the outlier threshold.
	OutlierRatio float64
	// OutlierFloor overrides the absolute minimum count for an outlier.
	OutlierFloor int
}

// FileStat is one file's declaration count.
type FileStat struct {
	File        string `json:"file"`
	SymbolCount int    `json:"symbol_count"`
	Outlier     bool   `json:"outlier"`
}

// Result is the full analysis output, with Files sorted by count descending.
type Result struct {
	FileCount     int        `json:"file_count"`
	TotalSymbols  int        `json:"total_symbols"`
	MedianSymbols float64    `json:"median_symbols"`
	MaxSymbols    int        `json:"max_symbols"`
	OutlierCount  int        `json:"outlier_count"`
	Files         []FileStat `json:"files"`
}

// Analyze counts the file→symbol contains edges of each in-scope file node and
// flags files whose count is at least max(ratio×median, floor).
func Analyze(g *Graph, opts Options) Result {
	result := Result{Files: []FileStat{}}
	if g == nil {
		return result
	}
	ratio := opts.OutlierRatio
	if ratio <= 0 {
		ratio = defaultOutlierRatio
	}
	floor := opts.OutlierFloor
	if floor <= 0 {
		floor = defaultOutlierFloor
	}

	var scope map[string]bool
	if len(opts.NodeIDs) > 0 {
		scope = make(map[string]bool, len(opts.NodeIDs))
		for _, id := range opts.NodeIDs {
			scope[id] = true
		}
	}

	counts := map[string]int{}
	for _, n := range g.Nodes() {
		if n.Kind != mgraph.NodeFile {
			continue
		}
		if scope == nil || scope[n.ID] {
			counts[n.ID] = 0
		}
	}
	for _, e := range g.Edges() {
		if e.Kind == mgraph.EdgeContains {
			if _, ok := counts[e.From]; ok {
				counts[e.From]++
			}
		}
	}

	stats := make([]FileStat, 0, len(counts))
	vals := make([]int, 0, len(counts))
	total, maxc := 0, 0
	for id, c := range counts {
		stats = append(stats, FileStat{File: id, SymbolCount: c})
		vals = append(vals, c)
		total += c
		if c > maxc {
			maxc = c
		}
	}

	median := medianInt(vals)
	threshold := ratio * median
	if float64(floor) > threshold {
		threshold = float64(floor)
	}
	outliers := 0
	for i := range stats {
		if float64(stats[i].SymbolCount) >= threshold {
			stats[i].Outlier = true
			outliers++
		}
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].SymbolCount != stats[j].SymbolCount {
			return stats[i].SymbolCount > stats[j].SymbolCount
		}
		return stats[i].File < stats[j].File
	})

	result.FileCount = len(stats)
	result.TotalSymbols = total
	result.MedianSymbols = median
	result.MaxSymbols = maxc
	result.OutlierCount = outliers
	result.Files = stats
	return result
}

func medianInt(xs []int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return float64(s[n/2-1]+s[n/2]) / 2
}
