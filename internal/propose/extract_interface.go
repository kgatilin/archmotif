package propose

import (
	"fmt"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// ExtractInterfaceRule is the v1 transformation rule (ADR-019). It
// reads `motif_redundancy` Records (Stage 3) and proposes
// extract-interface refactors when a motif group:
//
//   - has at least MinRedundancy instances (default 3),
//   - has motif size in [MinMotifSize, MaxMotifSize] (default 3..5),
//   - contains no contract-marked nodes (per ADR-009).
//
// The rule emits one Proposal per qualifying Record. The TargetSubgraph
// is the canonical extract-interface shape: one Iface, N Impls, N
// Methods, with Implements (Impl→Iface) and Contains (Impl→Method)
// edges.
//
// Apply is intentionally lightweight: it returns the right-shaped
// Proposal on hand-built fixtures. Stage 5's implementation issue
// extends Apply to mine real method signatures and infer common types
// once Stage 4's anomaly stream is wired in.
type ExtractInterfaceRule struct {
	// MinMotifSize is the smallest motif size accepted. Defaults to
	// DefaultMinMotifSize when zero.
	MinMotifSize int
	// MaxMotifSize is the largest motif size accepted. Defaults to
	// DefaultMaxMotifSize when zero.
	MaxMotifSize int
	// MinRedundancy is the minimum motif group size (number of
	// isomorphic instances) required to fire. Defaults to
	// DefaultMinRedundancy when zero.
	MinRedundancy int
}

// Threshold defaults pinned by ADR-019.
const (
	// DefaultMinMotifSize matches the Stage 3 motif enumeration lower
	// bound (ADR-013).
	DefaultMinMotifSize = 3
	// DefaultMaxMotifSize matches the Stage 3 motif enumeration upper
	// bound (ADR-013). Stage 3's runtime default cap is 4; the rule
	// accepts the full enumeration range.
	DefaultMaxMotifSize = 5
	// DefaultMinRedundancy is the empirical floor for "this looks like
	// a real abstraction opportunity". Two instances is too noisy.
	DefaultMinRedundancy = 3
)

// motifMetricName is the metric name the rule consumes.
const motifMetricName = "motif_redundancy"

// Name returns the rule identifier.
func (ExtractInterfaceRule) Name() string { return "extract_interface" }

// Description returns the rule documentation string.
func (ExtractInterfaceRule) Description() string {
	return "extract a shared interface from N isomorphic motif instances (consumes motif_redundancy)"
}

func (r ExtractInterfaceRule) thresholds() (minSize, maxSize, minRedundancy int) {
	minSize = r.MinMotifSize
	if minSize <= 0 {
		minSize = DefaultMinMotifSize
	}
	maxSize = r.MaxMotifSize
	if maxSize <= 0 {
		maxSize = DefaultMaxMotifSize
	}
	minRedundancy = r.MinRedundancy
	if minRedundancy <= 0 {
		minRedundancy = DefaultMinRedundancy
	}
	return
}

// Trigger reports whether rec is a motif-redundancy region Record that
// passes size, redundancy, and contract-exclusion checks.
func (r ExtractInterfaceRule) Trigger(rec metrics.Record, g *mgraph.Graph) bool {
	if rec.Metric != motifMetricName {
		return false
	}
	if rec.Scope != metrics.ScopeRegion {
		return false
	}
	minSize, maxSize, minRedundancy := r.thresholds()
	if int(rec.Value) < minRedundancy {
		return false
	}
	size, ok := intFromDetails(rec.Details, "size")
	if !ok {
		return false
	}
	if size < minSize || size > maxSize {
		return false
	}
	instances, ok := instancesFromDetails(rec.Details)
	if !ok || len(instances) < minRedundancy {
		return false
	}
	if g != nil && anyMemberIsContract(g, instances) {
		return false
	}
	return true
}

// Apply builds the extract-interface Proposal for rec. Trigger must
// have returned true; Apply rechecks the structural invariants
// (instance shape, member existence) and returns an error if the
// Record is malformed.
//
// Per ADR-022 the real Apply:
//
//  1. Defends in depth: re-runs the contract-exclusion check from
//     Trigger so a malformed call site never produces an illegal
//     Proposal.
//
//  2. For each instance, identifies the Impl Type, the Method on it,
//     and (when present) the external Iface Type the Impl Implements.
//     Role assignment uses graph edges, not name heuristics:
//
//     - Method = the lone Method node in the instance.
//     - Impl   = the Type node that Contains the Method.
//     - Iface  = a Type node that the Impl Implements (preferring
//     one that's NOT in the instance member list when
//     that disambiguates).
//
//  3. Records a structural method-signature fingerprint per instance
//     ("in:<incoming-edge-count>,out:<outgoing-edge-count>") so Stage
//     6 / Stage 7 can sanity-check signature alignment. When the
//     fingerprint set has more than one distinct value the rule
//     declines (returns nil, nil) — a real extract-interface needs
//     methods to align. ADR-022 §4.
//
//  4. Builds the canonical TargetSubgraph: 1 Iface role, N Impl, N
//     Method, with Implements + Contains edges.
func (r ExtractInterfaceRule) Apply(g *mgraph.Graph, rec metrics.Record) (*Proposal, error) {
	instances, ok := instancesFromDetails(rec.Details)
	if !ok {
		return nil, fmt.Errorf("rec %q: missing or malformed details.instances", rec.Target)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("rec %q: zero instances after filter", rec.Target)
	}
	// Defence in depth (ADR-022 §4): Trigger already enforced this.
	if g != nil && anyMemberIsContract(g, instances) {
		return nil, nil
	}
	_, _, minRedundancy := r.thresholds()

	methodInstances := assignableExtractInterfaceInstances(g, instances)
	if len(methodInstances) >= minRedundancy {
		return r.applyMethodShape(g, rec, methodInstances)
	}
	return r.applyTypeShape(g, rec, instances, minRedundancy)
}

func (r ExtractInterfaceRule) applyMethodShape(g *mgraph.Graph, rec metrics.Record, instances [][]string) (*Proposal, error) {
	n := len(instances)
	target := TargetSubgraph{
		Roles: []Role{
			{
				Name:        "Iface",
				Kind:        mgraph.NodeType,
				Cardinality: 1,
				Attrs: map[string]any{
					mgraph.AttrContractKind: "interface",
				},
			},
			{
				Name:        "Impl",
				Kind:        mgraph.NodeType,
				Cardinality: n,
			},
			{
				Name:        "Method",
				Kind:        mgraph.NodeMethod,
				Cardinality: n,
			},
		},
		Edges: []EdgeConstraint{
			{From: "Impl", To: "Iface", Kind: mgraph.EdgeImplements},
			{From: "Impl", To: "Method", Kind: mgraph.EdgeContains},
		},
	}

	samples := make([]map[string]string, 0, n)
	affected := map[string]struct{}{}
	signatures := map[string]struct{}{}
	for i, members := range instances {
		sample := map[string]string{}
		impl, method, iface := assignRoles(g, members)

		if impl != "" {
			sample["Impl"] = impl
			if pos := nodePos(g, impl); pos != "" {
				affected[pos] = struct{}{}
			}
			if n, ok := g.Node(impl); ok && n.Name != "" {
				sample["ImplName"] = n.Name
			}
		}
		if method != "" {
			sample["Method"] = method
			if pos := nodePos(g, method); pos != "" {
				affected[pos] = struct{}{}
			}
			if mn, ok := g.Node(method); ok && mn.Name != "" {
				sample["MethodName"] = mn.Name
			}
			sig := methodSignature(g, method)
			sample["MethodSignature"] = sig
			signatures[sig] = struct{}{}
		}
		if iface != "" {
			sample["Iface"] = iface
			if pos := nodePos(g, iface); pos != "" {
				affected[pos] = struct{}{}
			}
			if n, ok := g.Node(iface); ok && n.Name != "" {
				sample["IfaceName"] = n.Name
			}
		}
		// Tag the sample with its instance index so Stage 6 can
		// identify which Impl/Method tuple goes together.
		sample["_index"] = fmt.Sprintf("%d", i)
		samples = append(samples, sample)
	}

	// Method signatures must align (ADR-022 §4). One signature →
	// extract is meaningful; >1 signature → decline.
	if len(signatures) > 1 {
		return nil, nil
	}

	files := make([]string, 0, len(affected))
	for f := range affected {
		files = append(files, f)
	}
	sort.Strings(files)

	desc := fmt.Sprintf("extract shared interface from %d isomorphic motif instances", n)
	if len(samples) > 0 {
		if mn, ok := samples[0]["MethodName"]; ok && mn != "" {
			desc = fmt.Sprintf("extract interface with method %q across %d implementations", mn, n)
		}
	}

	prop := &Proposal{
		ID:             fmt.Sprintf("%s-%s", r.Name(), rec.Target),
		Description:    desc,
		Trigger:        AnomalyRefFrom(rec),
		AffectedFiles:  files,
		TargetSubgraph: target,
		Samples:        samples,
	}
	return prop, nil
}

func (r ExtractInterfaceRule) applyTypeShape(g *mgraph.Graph, rec metrics.Record, instances [][]string, minRedundancy int) (*Proposal, error) {
	iface, impls := typeShapeParticipants(g, instances)
	if iface == "" || len(impls) < minRedundancy {
		return nil, nil
	}
	target := TargetSubgraph{
		Roles: []Role{
			{
				Name:        "Iface",
				Kind:        mgraph.NodeType,
				Cardinality: 1,
				Attrs: map[string]any{
					mgraph.AttrContractKind: "interface",
				},
			},
			{
				Name:        "Impl",
				Kind:        mgraph.NodeType,
				Cardinality: len(impls),
			},
		},
		Edges: []EdgeConstraint{
			{From: "Impl", To: "Iface", Kind: mgraph.EdgeImplements},
		},
	}
	samples := make([]map[string]string, 0, len(impls))
	affected := map[string]struct{}{}
	if pos := nodePos(g, iface); pos != "" {
		affected[pos] = struct{}{}
	}
	ifaceName := iface
	if n, ok := g.Node(iface); ok && n.Name != "" {
		ifaceName = n.Name
	}
	for i, impl := range impls {
		sample := map[string]string{
			"Iface":  iface,
			"Impl":   impl,
			"_index": fmt.Sprintf("%d", i),
		}
		if n, ok := g.Node(iface); ok && n.Name != "" {
			sample["IfaceName"] = n.Name
		}
		if n, ok := g.Node(impl); ok && n.Name != "" {
			sample["ImplName"] = n.Name
		}
		if pos := nodePos(g, impl); pos != "" {
			affected[pos] = struct{}{}
		}
		samples = append(samples, sample)
	}
	files := make([]string, 0, len(affected))
	for f := range affected {
		files = append(files, f)
	}
	sort.Strings(files)

	return &Proposal{
		ID:             fmt.Sprintf("%s-%s", r.Name(), rec.Target),
		Description:    fmt.Sprintf("factor shared interface %q across %d implementations", ifaceName, len(impls)),
		Trigger:        AnomalyRefFrom(rec),
		AffectedFiles:  files,
		TargetSubgraph: target,
		Samples:        samples,
	}, nil
}

func assignableExtractInterfaceInstances(g *mgraph.Graph, instances [][]string) [][]string {
	if g == nil {
		return nil
	}
	out := make([][]string, 0, len(instances))
	for _, members := range instances {
		impl, method, _ := assignRoles(g, members)
		if impl == "" || method == "" {
			continue
		}
		cp := append([]string(nil), members...)
		out = append(out, cp)
	}
	return out
}

func typeShapeParticipants(g *mgraph.Graph, instances [][]string) (string, []string) {
	if g == nil {
		return "", nil
	}
	ifaceCounts := map[string]int{}
	implByIface := map[string]map[string]struct{}{}
	for _, members := range instances {
		memberSet := map[string]struct{}{}
		typeMembers := []string{}
		for _, id := range members {
			memberSet[id] = struct{}{}
			n, ok := g.Node(id)
			if ok && n.Kind == mgraph.NodeType {
				typeMembers = append(typeMembers, id)
			}
		}
		sort.Strings(typeMembers)
		for _, candidateIface := range typeMembers {
			impls := []string{}
			for _, candidateImpl := range typeMembers {
				if candidateImpl == candidateIface {
					continue
				}
				if hasEdge(g, candidateImpl, candidateIface, mgraph.EdgeImplements) {
					impls = append(impls, candidateImpl)
				}
			}
			if len(impls) == 0 {
				continue
			}
			ifaceCounts[candidateIface]++
			if implByIface[candidateIface] == nil {
				implByIface[candidateIface] = map[string]struct{}{}
			}
			for _, impl := range impls {
				if _, inInstance := memberSet[impl]; inInstance {
					implByIface[candidateIface][impl] = struct{}{}
				}
			}
		}
	}
	type row struct {
		iface string
		count int
		impls []string
	}
	rows := make([]row, 0, len(ifaceCounts))
	for iface, count := range ifaceCounts {
		impls := make([]string, 0, len(implByIface[iface]))
		for impl := range implByIface[iface] {
			impls = append(impls, impl)
		}
		sort.Strings(impls)
		rows = append(rows, row{iface: iface, count: count, impls: impls})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		if len(rows[i].impls) != len(rows[j].impls) {
			return len(rows[i].impls) > len(rows[j].impls)
		}
		return rows[i].iface < rows[j].iface
	})
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].iface, rows[0].impls
}

func hasEdge(g *mgraph.Graph, from, to string, kind mgraph.EdgeKind) bool {
	for _, e := range g.IncidentEdges(from, mgraph.DirectionOut, kind) {
		if e.To == to {
			return true
		}
	}
	return false
}

// assignRoles inspects the actual graph edges among the motif members
// and pins which member fills the Impl, Method, and (optionally) Iface
// slots. Returns empty strings for unfilled slots. Per ADR-022 §4.
//
// Strategy:
//
//   - Method = the Method node in members (assumes exactly one;
//     extract-interface motifs by definition have one Method per
//     instance).
//   - Impl   = the Type node that Contains the Method (an outgoing
//     EdgeContains edge from the Type to the Method). If multiple Type
//     nodes contain it, the smallest ID wins for determinism.
//   - Iface  = a Type node that the Impl Implements (outgoing
//     EdgeImplements). If the Iface candidate is itself one of the
//     instance members, that's fine — extract-interface fixtures
//     include the Iface in the motif. If multiple candidates, prefer
//     one OUTSIDE the instance member list (a shared external Iface
//     is the typical case); otherwise fall back to the smallest ID.
func assignRoles(g *mgraph.Graph, members []string) (impl, method, iface string) {
	if g == nil {
		return "", "", ""
	}
	memberSet := map[string]struct{}{}
	for _, id := range members {
		memberSet[id] = struct{}{}
	}
	// Find the Method node.
	methodCandidates := []string{}
	for _, id := range members {
		n, ok := g.Node(id)
		if !ok {
			continue
		}
		if n.Kind == mgraph.NodeMethod {
			methodCandidates = append(methodCandidates, id)
		}
	}
	sort.Strings(methodCandidates)
	if len(methodCandidates) > 0 {
		method = methodCandidates[0]
	}
	// Find the Impl: a Type that Contains the Method.
	if method != "" {
		implCandidates := []string{}
		for _, e := range g.IncidentEdges(method, mgraph.DirectionIn, mgraph.EdgeContains) {
			n, ok := g.Node(e.From)
			if !ok || n.Kind != mgraph.NodeType {
				continue
			}
			if _, inMembers := memberSet[e.From]; !inMembers {
				continue
			}
			implCandidates = append(implCandidates, e.From)
		}
		sort.Strings(implCandidates)
		if len(implCandidates) > 0 {
			impl = implCandidates[0]
		}
	}
	// Find the Iface: a Type the Impl Implements.
	if impl != "" {
		var inside, outside []string
		for _, e := range g.IncidentEdges(impl, mgraph.DirectionOut, mgraph.EdgeImplements) {
			n, ok := g.Node(e.To)
			if !ok || n.Kind != mgraph.NodeType {
				continue
			}
			if _, inMembers := memberSet[e.To]; inMembers {
				inside = append(inside, e.To)
			} else {
				outside = append(outside, e.To)
			}
		}
		sort.Strings(inside)
		sort.Strings(outside)
		switch {
		case len(outside) > 0:
			iface = outside[0]
		case len(inside) > 0:
			iface = inside[0]
		}
	}
	return impl, method, iface
}

// methodSignature returns a structural fingerprint of the method
// node: in:<incoming non-Contains edges>,out:<outgoing non-Contains
// edges>. Contains edges are excluded since they describe the
// method's structural place (its containing type), not its signature.
//
// archmotif's graph carries node kinds and edge kinds, not Go type
// strings, so the fingerprint is structural rather than nominal. Per
// ADR-022 §4 this is advisory: Stage 6 / Stage 7 can refine using
// source-level inspection.
func methodSignature(g *mgraph.Graph, methodID string) string {
	if g == nil || methodID == "" {
		return ""
	}
	in := 0
	for _, e := range g.IncidentEdges(methodID, mgraph.DirectionIn, "") {
		if e.Kind == mgraph.EdgeContains {
			continue
		}
		in++
	}
	out := 0
	for _, e := range g.IncidentEdges(methodID, mgraph.DirectionOut, "") {
		if e.Kind == mgraph.EdgeContains {
			continue
		}
		out++
	}
	return fmt.Sprintf("in:%d,out:%d", in, out)
}

// nodePos returns the source-position file the node belongs to, or
// the node ID's "file" prefix for synthetic nodes. Used to populate
// AffectedFiles. Returns empty when no positional info is available.
func nodePos(g *mgraph.Graph, id string) string {
	n, ok := g.Node(id)
	if !ok {
		return ""
	}
	if n.Pos.File != "" {
		return n.Pos.File
	}
	// Fall back to the file-prefix component of the synthetic ID
	// format (per ADR-005: "<file>:<line>:<col>:<kind>:<name>").
	if i := strings.Index(id, ":"); i > 0 {
		// IDs starting with "pkg:" carry no file; skip.
		if strings.HasPrefix(id, "pkg:") {
			return ""
		}
		return id[:i]
	}
	return ""
}

// anyMemberIsContract reports whether any member node of any instance
// in the motif group carries IsContract=true. Per ADR-009 / ADR-019
// the rule must skip such groups entirely.
func anyMemberIsContract(g *mgraph.Graph, instances [][]string) bool {
	for _, members := range instances {
		for _, id := range members {
			n, ok := g.Node(id)
			if !ok {
				continue
			}
			if n.IsContract() {
				return true
			}
		}
	}
	return false
}

// intFromDetails coerces details[key] into an int, accepting both
// int and float64 (JSON numbers decode as float64 in map[string]any).
func intFromDetails(details map[string]any, key string) (int, bool) {
	if details == nil {
		return 0, false
	}
	v, ok := details[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	}
	return 0, false
}

// instancesFromDetails extracts the per-instance member-ID lists from
// a motif_redundancy region Record. Accepts both [][]string (in-Go
// construction) and []any of []string / []any (post-JSON-roundtrip).
func instancesFromDetails(details map[string]any) ([][]string, bool) {
	if details == nil {
		return nil, false
	}
	raw, ok := details["instances"]
	if !ok {
		return nil, false
	}
	switch typed := raw.(type) {
	case [][]string:
		out := make([][]string, len(typed))
		for i, ins := range typed {
			cp := make([]string, len(ins))
			copy(cp, ins)
			out[i] = cp
		}
		return out, true
	case []any:
		out := make([][]string, 0, len(typed))
		for _, ins := range typed {
			members, ok := stringSliceFromAny(ins)
			if !ok {
				return nil, false
			}
			out = append(out, members)
		}
		return out, true
	}
	return nil, false
}

// stringSliceFromAny coerces v into []string, accepting both []string
// and []any of strings.
func stringSliceFromAny(v any) ([]string, bool) {
	switch typed := v.(type) {
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, e := range typed {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func init() { Register(ExtractInterfaceRule{}) }
