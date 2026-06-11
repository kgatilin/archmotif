package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/contracts"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/propose"
)

const architectureOptimizeJSONVersion = 1

type architectureOptimizeOptions struct {
	Format            string
	Pattern           string
	Tests             bool
	Limit             int
	TargetGraphMLOut  string
	CurrentGraphMLOut string
	ContractOut       string
}

type architectureOptimizeJSON struct {
	Version   int                        `json:"version"`
	Mode      string                     `json:"mode"`
	Input     architectureOptimizeInput  `json:"input"`
	Pipeline  architectureOptimizeStages `json:"pipeline"`
	Contracts []optimizationContract     `json:"contracts"`
	Skipped   []*propose.Proposal        `json:"skipped,omitempty"`
	Errors    []string                   `json:"errors,omitempty"`
}

type architectureOptimizeInput struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Tests   bool   `json:"tests"`
	Nodes   int    `json:"nodes"`
	Edges   int    `json:"edges"`
}

type architectureOptimizeStages struct {
	MetricsRan    []string `json:"metricsRan,omitempty"`
	AnomaliesRan  []string `json:"anomaliesRan,omitempty"`
	Anomalies     int      `json:"anomalies"`
	Proposals     int      `json:"proposals"`
	Contracts     int      `json:"contracts"`
	SelectionRule string   `json:"selectionRule"`
}

type optimizationContract struct {
	ID                     string                       `json:"id"`
	Kind                   string                       `json:"kind"`
	Rule                   string                       `json:"rule"`
	Score                  float64                      `json:"score"`
	ProposalID             string                       `json:"proposalId"`
	Description            string                       `json:"description"`
	Objective              optimizationObjective        `json:"objective"`
	SourceAnomaly          optimizationSourceAnomaly    `json:"sourceAnomaly"`
	Current                optimizationCurrent          `json:"current"`
	Target                 optimizationTarget           `json:"target"`
	Feasibility            optimizationFeasibility      `json:"feasibility"`
	ExpectedMetricMovement []optimizationMetricMovement `json:"expectedMetricMovement"`
	Invariants             []string                     `json:"invariants"`
	Verification           []string                     `json:"verification"`
	Proposal               *propose.Proposal            `json:"proposal"`
}

type optimizationObjective struct {
	Metric    string  `json:"metric"`
	Target    string  `json:"target"`
	Before    float64 `json:"before"`
	Direction string  `json:"direction"`
	Rationale string  `json:"rationale"`
}

type optimizationSourceAnomaly struct {
	Metric   string                 `json:"metric"`
	Detector string                 `json:"detector"`
	Score    float64                `json:"score"`
	Region   anomalies.Region       `json:"region"`
	Reason   anomalies.Reason       `json:"reason"`
	Source   anomalies.SourceRecord `json:"sourceRecord"`
}

type optimizationCurrent struct {
	RegionMembers        []optimizationNodeRef `json:"regionMembers"`
	RegionEdges          []optimizationEdgeRef `json:"regionEdges,omitempty"`
	BoundaryNodes        []optimizationNodeRef `json:"boundaryNodes,omitempty"`
	BoundaryEdges        []optimizationEdgeRef `json:"boundaryEdges,omitempty"`
	BoundaryEdgesOmitted int                   `json:"boundaryEdgesOmitted,omitempty"`
	BoundaryRule         string                `json:"boundaryRule"`
}

type optimizationTarget struct {
	Subgraph propose.TargetSubgraph `json:"subgraph"`
	Graph    optimizationGraph      `json:"graph"`
}

type optimizationGraph struct {
	Nodes []optimizationNodeRef `json:"nodes"`
	Edges []optimizationEdgeRef `json:"edges"`
}

type optimizationMetricMovement struct {
	Metric    string  `json:"metric"`
	Target    string  `json:"target"`
	Before    float64 `json:"before"`
	Direction string  `json:"direction"`
	After     string  `json:"after"`
}

type optimizationFeasibility struct {
	Status                 string   `json:"status"`
	Warnings               []string `json:"warnings,omitempty"`
	ReusedSourceRoleValues []string `json:"reusedSourceRoleValues,omitempty"`
	UniqueCurrentMembers   int      `json:"uniqueCurrentMembers"`
	TargetNodes            int      `json:"targetNodes"`
}

type optimizationNodeRef struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name,omitempty"`
	QName        string `json:"qname,omitempty"`
	File         string `json:"file,omitempty"`
	Line         int    `json:"line,omitempty"`
	Foreign      bool   `json:"foreign,omitempty"`
	Contract     bool   `json:"contract,omitempty"`
	ContractKind string `json:"contractKind,omitempty"`
	Role         string `json:"role,omitempty"`
	TypeKind     string `json:"typeKind,omitempty"`
}

type optimizationEdgeRef struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type architectureContractBuild struct {
	Contract     optimizationContract
	CurrentGraph *mgraph.Graph
	TargetGraph  *mgraph.Graph
}

func runArchitectureOptimize(input string, opts architectureOptimizeOptions, stdout, stderr io.Writer) int {
	if opts.Pattern == "" {
		opts.Pattern = "./..."
	}
	build, err := contracts.Build(contracts.BuildOptions{
		Dir:      input,
		Patterns: []string{opts.Pattern},
		Tests:    opts.Tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: %v\n", err)
		return 1
	}

	errs := make([]string, 0)
	for _, e := range build.LoadErrors {
		msg := "load: " + e
		errs = append(errs, msg)
		_, _ = fmt.Fprintln(stderr, msg)
	}

	mres := metrics.Run(build.Graph, nil)
	for _, e := range mres.Errors {
		msg := "metric error: " + e.Error()
		errs = append(errs, msg)
		_, _ = fmt.Fprintln(stderr, msg)
	}

	ares := anomalies.Run(build.Graph, mres.Records, nil)
	for _, e := range ares.Errors {
		msg := "detector error: " + e.Error()
		errs = append(errs, msg)
		_, _ = fmt.Fprintln(stderr, msg)
	}

	pres := propose.NewProposer().Propose(build.Graph, ares.Anomalies)
	for _, e := range pres.Errors {
		msg := "rule error: " + e.Error()
		errs = append(errs, msg)
		_, _ = fmt.Fprintln(stderr, msg)
	}

	scoreByID := buildScoreMap(ares.Anomalies, pres.Proposals)
	ranked := rankOptimizationProposals(pres.Proposals, scoreByID)
	if opts.Limit > 0 && opts.Limit < len(ranked) {
		ranked = ranked[:opts.Limit]
	}

	contractBuilds, contractErrs := buildOptimizationContracts(build.Graph, ranked, ares.Anomalies, scoreByID)
	for _, e := range contractErrs {
		msg := "contract error: " + e.Error()
		errs = append(errs, msg)
		_, _ = fmt.Fprintln(stderr, msg)
	}
	contracts := make([]optimizationContract, 0, len(contractBuilds))
	for _, b := range contractBuilds {
		contracts = append(contracts, b.Contract)
	}

	env := architectureOptimizeJSON{
		Version: architectureOptimizeJSONVersion,
		Mode:    "architecture",
		Input: architectureOptimizeInput{
			Path:    input,
			Pattern: opts.Pattern,
			Tests:   opts.Tests,
			Nodes:   build.Graph.NodeCount(),
			Edges:   build.Graph.EdgeCount(),
		},
		Pipeline: architectureOptimizeStages{
			MetricsRan:    mres.Ran,
			AnomaliesRan:  ares.Ran,
			Anomalies:     len(ares.Anomalies),
			Proposals:     len(pres.Proposals),
			Contracts:     len(contracts),
			SelectionRule: "rank proposals by anomaly score, then proposal id; emit local target graph from Proposal.TargetSubgraph",
		},
		Contracts: contracts,
		Skipped:   pres.Skipped,
		Errors:    errs,
	}

	if opts.TargetGraphMLOut != "" && len(contractBuilds) > 0 {
		if err := writeTypedGraphMLFile(opts.TargetGraphMLOut, contractBuilds[0].TargetGraph); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write target graphml: %v\n", err)
			return 1
		}
	}
	if opts.CurrentGraphMLOut != "" && len(contractBuilds) > 0 {
		if err := writeTypedGraphMLFile(opts.CurrentGraphMLOut, contractBuilds[0].CurrentGraph); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write current graphml: %v\n", err)
			return 1
		}
	}
	if opts.ContractOut != "" {
		if err := writeArchitectureOptimizeJSONFile(opts.ContractOut, env); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write contract json: %v\n", err)
			return 1
		}
	}

	switch opts.Format {
	case "json", "":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(env); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write json: %v\n", err)
			return 1
		}
	case "table":
		writeArchitectureOptimizeTable(stdout, env)
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: unknown format %q (want: json|table)\n", opts.Format)
		return 2
	}

	if len(pres.Errors) > 0 || len(contractErrs) > 0 {
		return 1
	}
	return 0
}

func rankOptimizationProposals(in []*propose.Proposal, scores map[string]float64) []*propose.Proposal {
	out := make([]*propose.Proposal, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		si := scores[out[i].ID]
		sj := scores[out[j].ID]
		if si != sj {
			return si > sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func buildOptimizationContracts(g *mgraph.Graph, props []*propose.Proposal, anoms []anomalies.Anomaly, scores map[string]float64) ([]architectureContractBuild, []error) {
	anomalyByKey := bestAnomalyByMetricTarget(anoms)
	out := make([]architectureContractBuild, 0, len(props))
	errs := make([]error, 0)
	for _, prop := range props {
		targetGraph, err := buildTargetGraph(prop)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prop.ID, err))
			continue
		}
		anomaly := anomalyForProposal(prop, anomalyByKey)
		members := proposalMemberIDs(g, prop, anomaly)
		currentRegion := g.Subgraph(members, 0)
		currentGraph := g.Subgraph(members, 1)
		boundaryNodes, boundaryEdges, omitted := boundaryContext(g, members, 50)

		metric := ""
		target := ""
		before := 0.0
		if prop.Trigger != nil {
			metric = prop.Trigger.Metric
			target = prop.Trigger.Target
			before = prop.Trigger.Value
		}
		kind := optimizationKind(prop)
		contract := optimizationContract{
			ID:          "optimization-" + prop.ID,
			Kind:        kind,
			Rule:        proposalRuleName(prop),
			Score:       scores[prop.ID],
			ProposalID:  prop.ID,
			Description: prop.Description,
			Objective: optimizationObjective{
				Metric:    metric,
				Target:    target,
				Before:    before,
				Direction: "decrease",
				Rationale: objectiveRationale(kind),
			},
			SourceAnomaly: optimizationSourceAnomaly{
				Metric:   anomaly.Metric,
				Detector: anomaly.Detector,
				Score:    anomaly.Score,
				Region:   anomaly.Region,
				Reason:   anomaly.Reason,
				Source:   anomaly.SourceRecord,
			},
			Current: optimizationCurrent{
				RegionMembers:        nodeRefsFromGraph(currentRegion),
				RegionEdges:          edgeRefsFromGraph(currentRegion),
				BoundaryNodes:        boundaryNodes,
				BoundaryEdges:        boundaryEdges,
				BoundaryEdgesOmitted: omitted,
				BoundaryRule:         "Region members are editable; one-hop boundary nodes/edges are context and must remain valid unless a later contract owns them.",
			},
			Target: optimizationTarget{
				Subgraph: prop.TargetSubgraph,
				Graph: optimizationGraph{
					Nodes: nodeRefsFromGraph(targetGraph),
					Edges: edgeRefsFromGraph(targetGraph),
				},
			},
			Feasibility: proposalFeasibility(prop, currentRegion, targetGraph),
			ExpectedMetricMovement: []optimizationMetricMovement{
				{
					Metric:    metric,
					Target:    target,
					Before:    before,
					Direction: "decrease",
					After:     expectedMetricAfter(kind),
				},
			},
			Invariants: []string{
				"Do not mutate user-declared contract nodes unless they are explicit target roles.",
				"Do not treat foreign/external nodes as refactorable implementation owners.",
				"Preserve graph edges crossing the boundary unless the proposal lists them as target edges.",
				"Preserve source behaviour; target graph is a structural contract, not a semantic patch.",
			},
			Verification: []string{
				"Regenerate the ArchMotif graph after the code change.",
				"Verify the regenerated graph contains the target subgraph with matching role kinds and edge kinds.",
				"Re-run metrics/anomalies and confirm the triggering metric moved in the expected direction.",
			},
			Proposal: prop,
		}
		out = append(out, architectureContractBuild{
			Contract:     contract,
			CurrentGraph: currentGraph,
			TargetGraph:  targetGraph,
		})
	}
	return out, errs
}

func proposalFeasibility(prop *propose.Proposal, currentRegion, targetGraph *mgraph.Graph) optimizationFeasibility {
	reused := reusedOwnedSampleValues(prop)
	status := "candidate"
	warnings := []string{}
	if len(reused) > 0 {
		status = "needs_review"
		warnings = append(warnings, "some source Impl/Method sample nodes appear in more than one target role instance; materializer may need to pick a non-overlapping subset or split this into multiple iterations")
	}
	if targetGraph.NodeCount() > currentRegion.NodeCount()+len(prop.Samples) {
		status = "needs_review"
		warnings = append(warnings, "target graph is substantially larger than the unique current region; inspect overlap before materialization")
	}
	return optimizationFeasibility{
		Status:                 status,
		Warnings:               warnings,
		ReusedSourceRoleValues: reused,
		UniqueCurrentMembers:   currentRegion.NodeCount(),
		TargetNodes:            targetGraph.NodeCount(),
	}
}

func reusedOwnedSampleValues(prop *propose.Proposal) []string {
	counts := map[string]int{}
	for _, sample := range prop.Samples {
		for _, key := range []string{"Impl", "Method"} {
			if value := sample[key]; value != "" {
				counts[value]++
			}
		}
	}
	out := make([]string, 0)
	for value, count := range counts {
		if count > 1 {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func bestAnomalyByMetricTarget(anoms []anomalies.Anomaly) map[string]anomalies.Anomaly {
	out := map[string]anomalies.Anomaly{}
	for _, a := range anoms {
		key := metricTargetKey(a.Metric, a.SourceRecord.Target)
		if existing, ok := out[key]; !ok || a.Score > existing.Score {
			out[key] = a
		}
	}
	return out
}

func anomalyForProposal(prop *propose.Proposal, byKey map[string]anomalies.Anomaly) anomalies.Anomaly {
	if prop.Trigger == nil {
		return anomalies.Anomaly{}
	}
	return byKey[metricTargetKey(prop.Trigger.Metric, prop.Trigger.Target)]
}

func metricTargetKey(metric, target string) string {
	return metric + "\x00" + target
}

func proposalMemberIDs(g *mgraph.Graph, prop *propose.Proposal, anomaly anomalies.Anomaly) []string {
	seen := map[string]struct{}{}
	add := func(id string) {
		if id == "" || !g.HasNode(id) {
			return
		}
		seen[id] = struct{}{}
	}
	for _, sample := range prop.Samples {
		for key, value := range sample {
			if !sampleValueIsNodeID(key) {
				continue
			}
			add(value)
		}
	}
	if len(seen) == 0 {
		for _, id := range anomaly.Region.Members {
			add(id)
		}
		for _, id := range idsFromInstances(anomaly.SourceRecord.Details) {
			add(id)
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sampleValueIsNodeID(key string) bool {
	if key == "" || key == "_index" {
		return false
	}
	if strings.HasSuffix(key, "Name") || strings.HasSuffix(key, "Signature") {
		return false
	}
	return true
}

func idsFromInstances(details map[string]any) []string {
	if details == nil {
		return nil
	}
	raw, ok := details["instances"]
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case string:
			seen[x] = struct{}{}
		case []string:
			for _, s := range x {
				seen[s] = struct{}{}
			}
		case []any:
			for _, item := range x {
				visit(item)
			}
		}
	}
	visit(raw)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func buildTargetGraph(prop *propose.Proposal) (*mgraph.Graph, error) {
	g := mgraph.New()
	roles := map[string]propose.Role{}
	idsByRole := map[string][]string{}
	for _, role := range prop.TargetSubgraph.Roles {
		if role.Name == "" {
			return nil, fmt.Errorf("target role with empty name")
		}
		if _, exists := roles[role.Name]; exists {
			return nil, fmt.Errorf("duplicate target role %q", role.Name)
		}
		roles[role.Name] = role
		cardinality := role.Cardinality
		if cardinality <= 0 {
			cardinality = 1
		}
		for i := 0; i < cardinality; i++ {
			id := targetRoleNodeID(prop.ID, role.Name, i, cardinality)
			attrs := map[string]any{
				mgraph.AttrRole:       role.Name,
				mgraph.AttrRoleSource: "optimizer_target",
				"proposalID":          prop.ID,
				"roleCardinality":     cardinality,
			}
			for k, v := range role.Attrs {
				attrs[k] = v
			}
			if contractKind, _ := attrs[mgraph.AttrContractKind].(string); contractKind == "interface" {
				attrs["typeKind"] = "interface"
			}
			name := role.Name
			if cardinality > 1 {
				name = fmt.Sprintf("%s[%d]", role.Name, i)
			}
			g.AddNode(mgraph.Node{
				ID:    id,
				Kind:  role.Kind,
				Name:  name,
				QName: prop.ID + "." + name,
				Attrs: attrs,
			})
			idsByRole[role.Name] = append(idsByRole[role.Name], id)
		}
	}
	for _, edge := range prop.TargetSubgraph.Edges {
		fromIDs := idsByRole[edge.From]
		toIDs := idsByRole[edge.To]
		if len(fromIDs) == 0 {
			return nil, fmt.Errorf("target edge references unknown from-role %q", edge.From)
		}
		if len(toIDs) == 0 {
			return nil, fmt.Errorf("target edge references unknown to-role %q", edge.To)
		}
		pairs, err := expandRoleEdge(edge, fromIDs, toIDs)
		if err != nil {
			return nil, err
		}
		for _, pair := range pairs {
			if _, err := g.AddEdge(mgraph.Edge{
				From: pair[0],
				To:   pair[1],
				Kind: edge.Kind,
				Attrs: map[string]any{
					"proposalID": prop.ID,
					"roleEdge":   edge.From + "->" + edge.To,
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	return g, nil
}

func targetRoleNodeID(proposalID, role string, idx, cardinality int) string {
	base := "target:" + proposalID + ":" + role
	if cardinality <= 1 {
		return base
	}
	return fmt.Sprintf("%s[%d]", base, idx)
}

func expandRoleEdge(edge propose.EdgeConstraint, fromIDs, toIDs []string) ([][2]string, error) {
	switch {
	case len(fromIDs) == len(toIDs):
		out := make([][2]string, 0, len(fromIDs))
		for i := range fromIDs {
			out = append(out, [2]string{fromIDs[i], toIDs[i]})
		}
		return out, nil
	case len(fromIDs) == 1:
		out := make([][2]string, 0, len(toIDs))
		for _, to := range toIDs {
			out = append(out, [2]string{fromIDs[0], to})
		}
		return out, nil
	case len(toIDs) == 1:
		out := make([][2]string, 0, len(fromIDs))
		for _, from := range fromIDs {
			out = append(out, [2]string{from, toIDs[0]})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("cannot expand target edge %s->%s with cardinalities %d and %d", edge.From, edge.To, len(fromIDs), len(toIDs))
	}
}

func boundaryContext(g *mgraph.Graph, members []string, limit int) ([]optimizationNodeRef, []optimizationEdgeRef, int) {
	memberSet := map[string]struct{}{}
	for _, id := range members {
		memberSet[id] = struct{}{}
	}
	nodeIDs := map[string]struct{}{}
	edges := make([]optimizationEdgeRef, 0)
	for _, e := range g.Edges() {
		_, fromIn := memberSet[e.From]
		_, toIn := memberSet[e.To]
		if fromIn == toIn {
			continue
		}
		if fromIn {
			nodeIDs[e.To] = struct{}{}
		} else {
			nodeIDs[e.From] = struct{}{}
		}
		edges = append(edges, edgeRefFromGraph(e))
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	omitted := 0
	if limit > 0 && len(edges) > limit {
		omitted = len(edges) - limit
		edges = edges[:limit]
	}
	nodes := make([]optimizationNodeRef, 0, len(nodeIDs))
	for id := range nodeIDs {
		if n, ok := g.Node(id); ok {
			nodes = append(nodes, nodeRefFromGraph(n))
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, edges, omitted
}

func nodeRefsFromGraph(g *mgraph.Graph) []optimizationNodeRef {
	nodes := make([]optimizationNodeRef, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		nodes = append(nodes, nodeRefFromGraph(n))
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

func nodeRefFromGraph(n mgraph.Node) optimizationNodeRef {
	return optimizationNodeRef{
		ID:           n.ID,
		Kind:         string(n.Kind),
		Name:         n.Name,
		QName:        n.QName,
		File:         n.Pos.File,
		Line:         n.Pos.Line,
		Foreign:      boolGraphAttr(n.Attrs, "foreign"),
		Contract:     n.IsContract(),
		ContractKind: n.ContractKind(),
		Role:         string(n.Role()),
		TypeKind:     stringGraphAttr(n.Attrs, "typeKind"),
	}
}

func edgeRefsFromGraph(g *mgraph.Graph) []optimizationEdgeRef {
	edges := make([]optimizationEdgeRef, 0, g.EdgeCount())
	for _, e := range g.Edges() {
		edges = append(edges, edgeRefFromGraph(e))
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	return edges
}

func edgeRefFromGraph(e mgraph.Edge) optimizationEdgeRef {
	return optimizationEdgeRef{From: e.From, To: e.To, Kind: string(e.Kind)}
}

func boolGraphAttr(attrs map[string]any, key string) bool {
	if attrs == nil {
		return false
	}
	switch v := attrs[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

func stringGraphAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	switch v := attrs[key].(type) {
	case string:
		return v
	case mgraph.Role:
		return string(v)
	default:
		return ""
	}
}

func optimizationKind(prop *propose.Proposal) string {
	if prop.Trigger != nil && prop.Trigger.Metric == "motif_redundancy" && proposalRuleName(prop) == "extract_interface" {
		return "motif_quotient_extract_interface"
	}
	if prop.Trigger != nil && prop.Trigger.Metric == "modularity" && proposalRuleName(prop) == "command_package_split" {
		return "command_package_split"
	}
	return "target_subgraph_rewrite"
}

func objectiveRationale(kind string) string {
	switch kind {
	case "motif_quotient_extract_interface":
		return "Repeated isomorphic motif instances are factored into a quotient target shape with one abstraction role and repeated implementation/method roles."
	case "command_package_split":
		return "An oversized cmd package is split into a CLI adapter role and an internal orchestration role while preserving command behaviour."
	default:
		return "Target subgraph is produced by an anomaly-driven rewrite rule."
	}
}

func expectedMetricAfter(kind string) string {
	switch kind {
	case "command_package_split":
		return "command package region should shrink enough to drop below the modularity oversize threshold after graph regeneration"
	case "motif_quotient_extract_interface":
		return "source motif group should disappear or drop below the anomaly threshold after graph regeneration"
	default:
		return "triggering metric should move in the expected direction after graph regeneration"
	}
}

func writeArchitectureOptimizeTable(w io.Writer, env architectureOptimizeJSON) {
	_, _ = fmt.Fprintf(w, "optimize architecture: %d contract(s), graph=%d nodes/%d edges, anomalies=%d, proposals=%d\n",
		len(env.Contracts), env.Input.Nodes, env.Input.Edges, env.Pipeline.Anomalies, env.Pipeline.Proposals)
	if len(env.Contracts) == 0 {
		_, _ = fmt.Fprintln(w, "(no optimization contracts — no anomalies triggered a registered proposal rule)")
		return
	}
	_, _ = fmt.Fprintf(w, "\n%-4s %-30s %-9s %-18s %-10s %-16s\n", "#", "contract", "score", "kind", "members", "target-n/e")
	for i, c := range env.Contracts {
		_, _ = fmt.Fprintf(w, "%-4d %-30s %-9.2f %-18s %-10d %-16s\n",
			i+1,
			truncateOptimize(c.ID, 30),
			c.Score,
			truncateOptimize(c.Kind, 18),
			len(c.Current.RegionMembers),
			fmt.Sprintf("%d/%d", len(c.Target.Graph.Nodes), len(c.Target.Graph.Edges)),
		)
	}
}

func writeTypedGraphMLFile(path string, g *mgraph.Graph) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := g.WriteGraphML(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeArchitectureOptimizeJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
