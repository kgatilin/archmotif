package propose

import (
	"fmt"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// CommandPackageSplitRule turns the modularity "oversized command package"
// anomaly into a manual extraction contract. It deliberately does not guess the
// final package name; it gives downstream humans/agents an actionable boundary:
// keep CLI adapter code in cmd and move orchestration/reporting into one
// internal package.
type CommandPackageSplitRule struct{}

// Name returns the rule's stable identifier used by the proposer registry.
func (CommandPackageSplitRule) Name() string { return "command_package_split" }

// Description returns a one-line human-readable summary of the rule.
func (CommandPackageSplitRule) Description() string {
	return "suggests extracting orchestration from an oversized cmd/... package"
}

// Trigger reports whether rec describes an oversized command package that this
// rule should fire on.
func (CommandPackageSplitRule) Trigger(rec metrics.Record, g *mgraph.Graph) bool {
	if rec.Metric != "modularity" || rec.Scope != metrics.ScopeRegion {
		return false
	}
	pkg, ok := g.Node(rec.Target)
	if !ok || pkg.Kind != mgraph.NodePackage {
		return false
	}
	if !isCommandPackage(pkg) {
		return false
	}
	return len(commandSplitMembers(g, rec)) >= 20
}

// Apply builds a command-package split Proposal for the package referenced by
// rec, or returns (nil, nil) when there is no meaningful split to suggest.
func (CommandPackageSplitRule) Apply(g *mgraph.Graph, rec metrics.Record) (*Proposal, error) {
	pkg, ok := g.Node(rec.Target)
	if !ok {
		return nil, fmt.Errorf("command package split: unknown package %q", rec.Target)
	}
	members := commandSplitMembers(g, rec)
	if len(members) == 0 {
		return nil, nil
	}
	files := filesForMembers(g, members)
	samples := []map[string]string{{
		"CurrentCommandPackage": pkg.ID,
		"PackageName":           packageDisplayName(pkg),
		"MemberCount":           fmt.Sprintf("%d", len(members)),
	}}
	for i, id := range sampleMemberIDs(members, 5) {
		samples = append(samples, map[string]string{fmt.Sprintf("Member[%d]", i): id})
	}

	return &Proposal{
		ID:            fmt.Sprintf("%s-%s", CommandPackageSplitRule{}.Name(), rec.Target),
		Description:   fmt.Sprintf("extract orchestration/reporting from oversized command package %s", packageDisplayName(pkg)),
		Trigger:       AnomalyRefFrom(rec),
		AffectedFiles: files,
		TargetSubgraph: TargetSubgraph{
			Roles: []Role{
				{
					Name:        "CLIAdapter",
					Kind:        mgraph.NodePackage,
					Cardinality: 1,
					Attrs: map[string]any{
						"packageAction": "keep",
						"packageRole":   "cmd_adapter",
						"keeps":         "flag parsing, usage, stdout/stderr, exit codes",
					},
				},
				{
					Name:        "OptimizeOrchestration",
					Kind:        mgraph.NodePackage,
					Cardinality: 1,
					Attrs: map[string]any{
						"packageAction": "create",
						"packageName":   "optimize",
						"packagePath":   "internal/optimize",
						"packageRole":   "internal_orchestration",
						"extracts":      "optimization orchestration, report DTOs, render/write helpers",
					},
				},
				{
					Name:        "OptimizeOptions",
					Kind:        mgraph.NodeType,
					Cardinality: 1,
					Attrs: map[string]any{
						"file":        "internal/optimize/run.go",
						"packageRole": "internal_orchestration",
						"typeKind":    "struct",
						"typeName":    "Options",
					},
				},
				{
					Name:        "OptimizeResult",
					Kind:        mgraph.NodeType,
					Cardinality: 1,
					Attrs: map[string]any{
						"file":        "internal/optimize/run.go",
						"packageRole": "internal_orchestration",
						"typeKind":    "struct",
						"typeName":    "Result",
					},
				},
				{
					Name:        "OptimizeRun",
					Kind:        mgraph.NodeFunction,
					Cardinality: 1,
					Attrs: map[string]any{
						"file":         "internal/optimize/run.go",
						"functionName": "Run",
						"packageRole":  "internal_orchestration",
						"signature":    "func Run(ctx context.Context, opts Options) (Result, error)",
					},
				},
			},
			Edges: []EdgeConstraint{
				{From: "CLIAdapter", To: "OptimizeOrchestration", Kind: mgraph.EdgeDependsOn},
				{From: "OptimizeRun", To: "OptimizeOptions", Kind: mgraph.EdgeUsesType},
				{From: "OptimizeRun", To: "OptimizeResult", Kind: mgraph.EdgeReturns},
			},
		},
		Samples: samples,
	}, nil
}

func commandSplitMembers(g *mgraph.Graph, rec metrics.Record) []string {
	members := stringSliceFromDetails(rec.Details, "members")
	if len(members) == 0 && rec.Target != "" {
		members = collectContainsTree(g, rec.Target)
	}
	out := make([]string, 0, len(members))
	for _, id := range members {
		if id == "" {
			continue
		}
		if n, ok := g.Node(id); ok && (n.Kind == mgraph.NodeFile || n.Kind == mgraph.NodeFunction || n.Kind == mgraph.NodeMethod || n.Kind == mgraph.NodeType) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return compactStrings(out)
}

func collectContainsTree(g *mgraph.Graph, root string) []string {
	seen := map[string]struct{}{root: {}}
	out := []string{root}
	frontier := []string{root}
	for len(frontier) > 0 {
		next := []string{}
		for _, id := range frontier {
			for _, n := range g.Neighbors(id, mgraph.DirectionOut, mgraph.EdgeContains) {
				if _, ok := seen[n.ID]; ok {
					continue
				}
				seen[n.ID] = struct{}{}
				out = append(out, n.ID)
				next = append(next, n.ID)
			}
		}
		frontier = next
	}
	return out
}

func isCommandPackage(n mgraph.Node) bool {
	for _, s := range []string{n.ID, n.QName, n.Name} {
		if strings.Contains(s, "/cmd/") || strings.HasPrefix(s, "cmd/") || strings.HasPrefix(s, "cmd:") {
			return true
		}
	}
	return false
}

func packageDisplayName(n mgraph.Node) string {
	if n.QName != "" {
		return n.QName
	}
	if strings.HasPrefix(n.ID, "pkg:") {
		return strings.TrimPrefix(n.ID, "pkg:")
	}
	return n.Name
}

func filesForMembers(g *mgraph.Graph, members []string) []string {
	seen := map[string]struct{}{}
	for _, id := range members {
		n, ok := g.Node(id)
		if !ok || n.Pos.File == "" {
			continue
		}
		seen[n.Pos.File] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func sampleMemberIDs(members []string, limit int) []string {
	if limit <= 0 || len(members) <= limit {
		return append([]string(nil), members...)
	}
	return append([]string(nil), members[:limit]...)
}

func stringSliceFromDetails(details map[string]any, key string) []string {
	if details == nil {
		return nil
	}
	raw, ok := details[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, elem := range v {
			if s, ok := elem.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func compactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:0]
	var last string
	for i, s := range in {
		if i > 0 && s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

func init() { Register(CommandPackageSplitRule{}) }
