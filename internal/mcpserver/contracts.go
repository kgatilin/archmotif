package mcpserver

// The contract lens (#57) sits on top of the multi-graph substrate. It does not
// introduce a new on-disk schema: instead, it derives a `contract` tag on the
// existing nodes from structural patterns (kind + visibility + incident edges)
// and exposes six MCP tools that filter/diff/walk along those tagged nodes.
//
// The tagging is intentionally idempotent: running TagContracts twice on the
// same graph produces the same `tags` and `contract_kind` attributes. Callers
// can therefore safely re-run it after any mutation without bloating the tags
// list or flipping the inferred kind.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ContractKind describes the structural class of a contract. The set is open:
// we surface what we can derive from a typical Go code graph plus generic
// patterns that any extractor can emit (route_registers, config_schema, ...).
type ContractKind string

const (
	// ContractKindDTO marks an exported cross-module type (struct/interface/
	// alias) — anything callers in another package can name.
	ContractKindDTO ContractKind = "dto"
	// ContractKindHTTPHandler marks a function/method that is registered as
	// an HTTP/RPC route (incoming route_registers edge) or whose kind is
	// already `HTTPHandler`.
	ContractKindHTTPHandler ContractKind = "http_handler"
	// ContractKindConfigSchema marks a type explicitly tagged kind=ConfigSchema
	// (serde / yaml / JSON schema). The extractor sets the kind directly;
	// we just elevate it to a contract.
	ContractKindConfigSchema ContractKind = "config_schema"
	// ContractKindEvent marks an event publisher: a function that writes to
	// a bus / queue / SSE channel. Surfaced via kind=EventPublisher.
	ContractKindEvent ContractKind = "event"
	// ContractKindCLIFlag marks an operator-facing CLI flag (kind=CliFlag).
	ContractKindCLIFlag ContractKind = "cli_flag"
	// ContractKindEnvVar marks an operator-facing environment variable
	// (kind=EnvVar).
	ContractKindEnvVar ContractKind = "env_var"
)

// contractTagName is the canonical tag we append to attrs["tags"]. Other tags
// already present are preserved.
const contractTagName = "contract"

// attrContractKind stores the derived ContractKind on a node so downstream
// tools can filter without re-running the heuristic.
const attrContractKind = "contract_kind"

// attrVisibility is the canonical visibility marker on a node. If the
// extractor sets it explicitly we trust that value; otherwise the heuristic
// inspects the node name (Go convention: leading uppercase = exported).
const attrVisibility = "visibility"

// TagContracts inspects every node in g and adds the `contract` tag plus a
// `contract_kind` attribute when a structural pattern matches. Returns a
// histogram of {contract_kind: count} for the tags it has just (re)affirmed.
//
// The function is idempotent: it always recomputes the kind from the current
// node state and overwrites the prior value if it disagrees. Adding the tag
// uses splitTags to avoid duplicates in the comma-separated list.
func TagContracts(g *Graph) map[string]int {
	hist := make(map[string]int)
	if g == nil {
		return hist
	}
	// Pre-compute incoming-route_registers set so we can detect HTTP handlers
	// in a single pass.
	hasRouteIn := make(map[string]bool)
	for _, e := range g.Edges {
		if e.Kind == "route_registers" {
			hasRouteIn[e.To] = true
		}
	}

	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Attrs == nil {
			n.Attrs = make(map[string]string)
		}
		kind := classifyContract(*n, hasRouteIn[n.ID])
		if kind == "" {
			continue
		}
		// (Re)assert the tag without duplicates.
		n.Attrs["tags"] = joinTags(splitTags(n.Attrs["tags"]), contractTagName)
		n.Attrs[attrContractKind] = string(kind)
		// Make sure the visibility marker is non-empty so contracts_list
		// can filter by it; fall back to the heuristic.
		if n.Attrs[attrVisibility] == "" {
			n.Attrs[attrVisibility] = inferVisibility(*n)
		}
		hist[string(kind)]++
	}
	return hist
}

// classifyContract returns the ContractKind for a node, or "" if no rule
// matches. The rules are listed in the order they win:
//
//  1. Explicit kinds the extractor emits (ConfigSchema, EventPublisher,
//     CliFlag, EnvVar, HTTPHandler) are honoured directly.
//  2. A Function/Method with an incoming `route_registers` edge is an HTTP
//     handler.
//  3. A public Type (struct, interface, alias) is a DTO.
func classifyContract(n Node, hasRouteIn bool) ContractKind {
	switch strings.ToLower(n.Kind) {
	case "configschema", "config_schema":
		return ContractKindConfigSchema
	case "eventpublisher", "event_publisher":
		return ContractKindEvent
	case "cliflag", "cli_flag":
		return ContractKindCLIFlag
	case "envvar", "env_var":
		return ContractKindEnvVar
	case "httphandler", "http_handler":
		return ContractKindHTTPHandler
	}
	if hasRouteIn {
		k := strings.ToLower(n.Kind)
		if k == "function" || k == "method" {
			return ContractKindHTTPHandler
		}
	}
	if strings.ToLower(n.Kind) == "type" {
		if inferVisibility(n) == "public" {
			return ContractKindDTO
		}
	}
	return ""
}

// inferVisibility derives a node's visibility. Returns the existing attribute
// if set; otherwise applies the Go convention (leading uppercase rune = public).
// Anonymous / empty names default to "private" so we never tag them as
// cross-module DTOs.
func inferVisibility(n Node) string {
	if v := n.Attrs[attrVisibility]; v != "" {
		return v
	}
	name := n.Name
	if name == "" {
		name = n.Attrs["name"]
	}
	if name == "" {
		return "private"
	}
	// Go visibility is determined by the first rune of the identifier.
	first, _ := utf8.DecodeRuneInString(name)
	if unicode.IsUpper(first) {
		return "public"
	}
	return "private"
}

// splitTags returns the comma-separated tags as a trimmed slice.
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// joinTags returns the comma-joined tags ensuring `extra` appears exactly once.
// Existing order is preserved.
func joinTags(existing []string, extra string) string {
	for _, t := range existing {
		if t == extra {
			return strings.Join(existing, ",")
		}
	}
	existing = append(existing, extra)
	return strings.Join(existing, ",")
}

// hasTag reports whether the comma-separated tags string contains tag.
func hasTag(tags, tag string) bool {
	for _, t := range splitTags(tags) {
		if t == tag {
			return true
		}
	}
	return false
}

// ----- Service entry points -----------------------------------------------

// ContractRecord is the on-the-wire shape returned by contracts_list. It
// always carries enough context (id, name, qname, kind, contract_kind,
// visibility, package) for callers to render a list without round-tripping
// to graph_query.
type ContractRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	QName        string `json:"qname,omitempty"`
	Kind         string `json:"kind"`
	ContractKind string `json:"contract_kind"`
	Visibility   string `json:"visibility,omitempty"`
	Package      string `json:"package,omitempty"`
}

// TagAndPersist runs TagContracts against graphID, saves the result, and
// returns the histogram. Used by the contracts_tag write tool.
func (s *Service) TagAndPersist(graphID string) (map[string]int, error) {
	args := map[string]any{}
	var hist map[string]int
	_, err := s.mutate(graphID, "contracts_tag", args, func(g *Graph) (map[string]any, error) {
		hist = TagContracts(g)
		return map[string]any{"counts": hist}, nil
	})
	if err != nil {
		return nil, err
	}
	return hist, nil
}

// loadTaggedGraph loads graphID and runs TagContracts in-memory. The on-disk
// graph is not modified: this lets all read-only contract tools work even on
// graphs that have not had contracts_tag called on them yet.
func (s *Service) loadTaggedGraph(graphID string) (*Graph, error) {
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	TagContracts(g)
	return g, nil
}

// ContractsList returns every contract node in graphID, optionally filtered by
// contract_kind and visibility.
func (s *Service) ContractsList(graphID, kind, visibility string) ([]ContractRecord, error) {
	g, err := s.loadTaggedGraph(graphID)
	if err != nil {
		return nil, err
	}
	out := make([]ContractRecord, 0)
	for _, n := range g.Nodes {
		if !hasTag(n.Attrs["tags"], contractTagName) {
			continue
		}
		ck := n.Attrs[attrContractKind]
		if kind != "" && ck != kind {
			continue
		}
		vis := n.Attrs[attrVisibility]
		if visibility != "" && vis != visibility {
			continue
		}
		out = append(out, ContractRecord{
			ID:           n.ID,
			Name:         n.Name,
			QName:        n.Attrs["qname"],
			Kind:         n.Kind,
			ContractKind: ck,
			Visibility:   vis,
			Package:      n.Attrs["package"],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// FieldDiff captures one attribute-level change between two contract nodes.
type FieldDiff struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// ChangedContract is a contract present in both graphs whose attributes have
// shifted between them.
type ChangedContract struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Kind      string      `json:"kind,omitempty"`
	FieldDiff []FieldDiff `json:"field_diff"`
}

// ContractsDiffResult is the structured delta returned by contracts_diff.
type ContractsDiffResult struct {
	A       string            `json:"a"`
	B       string            `json:"b"`
	Scope   string            `json:"scope,omitempty"`
	Added   []ContractRecord  `json:"added"`
	Removed []ContractRecord  `json:"removed"`
	Changed []ChangedContract `json:"changed"`
	Summary struct {
		Added   int `json:"added"`
		Removed int `json:"removed"`
		Changed int `json:"changed"`
	} `json:"summary"`
}

// ContractsDiff computes the contract-level delta between two graphs. scope, if
// non-empty, restricts results to a single contract_kind (e.g. "http_handler").
func (s *Service) ContractsDiff(aID, bID, scope string) (ContractsDiffResult, error) {
	a, err := s.loadTaggedGraph(aID)
	if err != nil {
		return ContractsDiffResult{}, err
	}
	b, err := s.loadTaggedGraph(bID)
	if err != nil {
		return ContractsDiffResult{}, err
	}
	res := ContractsDiffResult{A: aID, B: bID, Scope: scope}
	res.Added = make([]ContractRecord, 0)
	res.Removed = make([]ContractRecord, 0)
	res.Changed = make([]ChangedContract, 0)

	want := func(n Node) bool {
		if !hasTag(n.Attrs["tags"], contractTagName) {
			return false
		}
		if scope != "" && n.Attrs[attrContractKind] != scope {
			return false
		}
		return true
	}
	aByID := make(map[string]Node)
	for _, n := range a.Nodes {
		if want(n) {
			aByID[n.ID] = n
		}
	}
	bByID := make(map[string]Node)
	for _, n := range b.Nodes {
		if want(n) {
			bByID[n.ID] = n
		}
	}
	for id, n := range bByID {
		if _, ok := aByID[id]; !ok {
			res.Added = append(res.Added, nodeToRecord(n))
		}
	}
	for id, n := range aByID {
		if _, ok := bByID[id]; !ok {
			res.Removed = append(res.Removed, nodeToRecord(n))
		}
	}
	for id, an := range aByID {
		bn, ok := bByID[id]
		if !ok {
			continue
		}
		fd := computeFieldDiff(an, bn)
		if len(fd) == 0 && an.Kind == bn.Kind && an.Name == bn.Name {
			continue
		}
		if an.Kind != bn.Kind {
			fd = append(fd, FieldDiff{Field: "__kind", Old: an.Kind, New: bn.Kind})
		}
		if an.Name != bn.Name {
			fd = append(fd, FieldDiff{Field: "__name", Old: an.Name, New: bn.Name})
		}
		res.Changed = append(res.Changed, ChangedContract{
			ID:        id,
			Name:      bn.Name,
			Kind:      bn.Attrs[attrContractKind],
			FieldDiff: fd,
		})
	}
	sort.SliceStable(res.Added, func(i, j int) bool { return res.Added[i].ID < res.Added[j].ID })
	sort.SliceStable(res.Removed, func(i, j int) bool { return res.Removed[i].ID < res.Removed[j].ID })
	sort.SliceStable(res.Changed, func(i, j int) bool { return res.Changed[i].ID < res.Changed[j].ID })
	res.Summary.Added = len(res.Added)
	res.Summary.Removed = len(res.Removed)
	res.Summary.Changed = len(res.Changed)
	return res, nil
}

// nodeToRecord materialises a ContractRecord from a node already tagged.
func nodeToRecord(n Node) ContractRecord {
	return ContractRecord{
		ID:           n.ID,
		Name:         n.Name,
		QName:        n.Attrs["qname"],
		Kind:         n.Kind,
		ContractKind: n.Attrs[attrContractKind],
		Visibility:   n.Attrs[attrVisibility],
		Package:      n.Attrs["package"],
	}
}

// computeFieldDiff returns sorted FieldDiff entries for attrs that differ
// between an and bn. archmotif_id is ignored (it always mirrors the node id).
func computeFieldDiff(an, bn Node) []FieldDiff {
	out := make([]FieldDiff, 0)
	keys := make(map[string]struct{})
	for k := range an.Attrs {
		keys[k] = struct{}{}
	}
	for k := range bn.Attrs {
		keys[k] = struct{}{}
	}
	delete(keys, "archmotif_id")
	for k := range keys {
		av, aok := an.Attrs[k]
		bv, bok := bn.Attrs[k]
		switch {
		case aok && !bok:
			out = append(out, FieldDiff{Field: k, Old: av, New: nil})
		case !aok && bok:
			out = append(out, FieldDiff{Field: k, Old: nil, New: bv})
		case av != bv:
			out = append(out, FieldDiff{Field: k, Old: av, New: bv})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// Consumer/Producer return shapes.

// ContractRelation describes one related node — used for both consumers and
// producers, with the role drawn from the edge kind.
type ContractRelation struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Role    string `json:"role"`
	Package string `json:"package,omitempty"`
}

// consumerEdgeKinds are the edge kinds that count a node as a consumer of the
// referenced contract (incoming on contract). The list is intentionally
// permissive: any inbound dependency expresses "I depend on this contract".
var consumerEdgeKinds = map[string]string{
	"uses":       "uses",
	"usesType":   "uses",
	"calls":      "calls",
	"callsFrom":  "calls",
	"references": "references",
	"dependsOn":  "depends_on",
	"contains":   "contains",
	"embeds":     "embeds",
}

// producerEdgeKinds are the edge kinds that mark a node as a producer of the
// contract.
var producerEdgeKinds = map[string]string{
	"returns":         "returns",
	"implements":      "implements",
	"publishes":       "publishes",
	"writes":          "writes",
	"route_registers": "route_registers",
}

// ContractsConsumers returns every node with an inbound edge to contractID
// whose edge kind is in consumerEdgeKinds. Self-edges and producer edges
// (returns/implements/...) are excluded.
func (s *Service) ContractsConsumers(graphID, contractID string) ([]ContractRelation, error) {
	g, err := s.loadTaggedGraph(graphID)
	if err != nil {
		return nil, err
	}
	if !g.HasNode(contractID) {
		return nil, fmt.Errorf("contract %q not found in %q", contractID, graphID)
	}
	out := make([]ContractRelation, 0)
	seen := make(map[string]bool)
	for _, e := range g.Edges {
		if e.To != contractID || e.From == contractID {
			continue
		}
		role, ok := consumerEdgeKinds[e.Kind]
		if !ok {
			continue
		}
		key := e.From + "|" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		if n, ok := g.Node(e.From); ok {
			out = append(out, ContractRelation{
				ID:      n.ID,
				Name:    n.Name,
				Kind:    n.Kind,
				Role:    role,
				Package: n.Attrs["package"],
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ContractsProducers returns every node with an inbound edge to contractID
// whose edge kind is in producerEdgeKinds (returns / implements / publishes /
// writes / route_registers).
func (s *Service) ContractsProducers(graphID, contractID string) ([]ContractRelation, error) {
	g, err := s.loadTaggedGraph(graphID)
	if err != nil {
		return nil, err
	}
	if !g.HasNode(contractID) {
		return nil, fmt.Errorf("contract %q not found in %q", contractID, graphID)
	}
	out := make([]ContractRelation, 0)
	seen := make(map[string]bool)
	for _, e := range g.Edges {
		if e.To != contractID {
			continue
		}
		role, ok := producerEdgeKinds[e.Kind]
		if !ok {
			continue
		}
		key := e.From + "|" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		if n, ok := g.Node(e.From); ok {
			out = append(out, ContractRelation{
				ID:      n.ID,
				Name:    n.Name,
				Kind:    n.Kind,
				Role:    role,
				Package: n.Attrs["package"],
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// FieldHistoryEntry is one (graph, value) entry returned by
// contracts_field_history.
type FieldHistoryEntry struct {
	GraphID string `json:"graph_id"`
	Value   string `json:"value"`
	Present bool   `json:"present"`
}

// ContractsFieldHistory walks every graph in the workspace and reports the
// value of `field` on contractID at each variant. The result is sorted by
// graph id for deterministic output. The seed graphID is included; if the
// contract is absent in a variant, the entry has Present=false.
func (s *Service) ContractsFieldHistory(graphID, contractID, field string) ([]FieldHistoryEntry, error) {
	if field == "" {
		return nil, fmt.Errorf("field is required")
	}
	if _, err := s.LoadGraph(graphID); err != nil {
		return nil, err
	}
	refs, err := s.ListGraphs()
	if err != nil {
		return nil, err
	}
	out := make([]FieldHistoryEntry, 0, len(refs))
	for _, r := range refs {
		g, err := s.LoadGraph(r.ID)
		if err != nil {
			continue
		}
		entry := FieldHistoryEntry{GraphID: r.ID}
		if n, ok := g.Node(contractID); ok {
			entry.Present = true
			entry.Value = n.Attrs[field]
		}
		out = append(out, entry)
	}
	return out, nil
}

// ContractsExport renders the contract subset as one of the supported export
// formats. Today we ship `openapi` for HTTP handlers; other formats return an
// error so callers fail fast.
func (s *Service) ContractsExport(graphID, format string) (map[string]any, error) {
	if format == "" {
		format = "openapi"
	}
	switch strings.ToLower(format) {
	case "openapi":
		return s.exportOpenAPI(graphID)
	case "json":
		return s.exportJSON(graphID)
	default:
		return nil, fmt.Errorf("unsupported format %q (have: openapi, json)", format)
	}
}

// ErrOpenAPICollision is returned by exportOpenAPI when two contracts would
// occupy the same (path, method) tuple or the same schema name. The error
// message lists every colliding node id so the caller can disambiguate by
// editing the source extractor or by adding explicit http_path / name
// overrides.
var ErrOpenAPICollision = errors.New("openapi export: schema/path collision")

// exportOpenAPI produces a minimal OpenAPI 3.0 document covering every node
// tagged as an HTTP handler. The path is taken from attrs["http_path"] (or
// attrs["path"]) and the method from attrs["http_method"] (or
// attrs["method"]). When neither is set the entry falls back to GET
// /<contract_name>.
//
// Collisions are explicit: if two handlers resolve to the same (path, method)
// or two DTOs resolve to the same schema name, the function returns
// ErrOpenAPICollision listing the offending node ids rather than silently
// overwriting the earlier entry.
func (s *Service) exportOpenAPI(graphID string) (map[string]any, error) {
	g, err := s.loadTaggedGraph(graphID)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]map[string]any)
	// pathOwners tracks which node id first claimed each (path, method) so we
	// can name both sides in a collision error.
	pathOwners := make(map[string]string)
	var pathCollisions [][3]string // (path, method, [first_id, second_id])-ish triples rendered as 3 strings
	for _, n := range g.Nodes {
		if !hasTag(n.Attrs["tags"], contractTagName) {
			continue
		}
		if n.Attrs[attrContractKind] != string(ContractKindHTTPHandler) {
			continue
		}
		path := n.Attrs["http_path"]
		if path == "" {
			path = n.Attrs["path"]
		}
		if path == "" {
			path = "/" + slugifyName(n.Name)
		}
		method := strings.ToLower(n.Attrs["http_method"])
		if method == "" {
			method = strings.ToLower(n.Attrs["method"])
		}
		if method == "" {
			method = "get"
		}
		op := map[string]any{
			"operationId":         n.ID,
			"summary":             n.Name,
			"x-archmotif-node-id": n.ID,
		}
		if pkg := n.Attrs["package"]; pkg != "" {
			op["x-archmotif-package"] = pkg
		}
		key := method + " " + path
		if prevID, dup := pathOwners[key]; dup {
			pathCollisions = append(pathCollisions, [3]string{key, prevID, n.ID})
			continue
		}
		pathOwners[key] = n.ID
		entry, ok := paths[path]
		if !ok {
			entry = make(map[string]any)
			paths[path] = entry
		}
		entry[method] = op
	}
	// Components / schemas: every public Type contract becomes a stub schema.
	schemas := make(map[string]any)
	schemaOwners := make(map[string]string)
	var schemaCollisions [][3]string // (name, first_id, second_id)
	for _, n := range g.Nodes {
		if !hasTag(n.Attrs["tags"], contractTagName) {
			continue
		}
		if n.Attrs[attrContractKind] != string(ContractKindDTO) {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		if prevID, dup := schemaOwners[name]; dup {
			schemaCollisions = append(schemaCollisions, [3]string{name, prevID, n.ID})
			continue
		}
		schemaOwners[name] = n.ID
		schemas[name] = map[string]any{
			"type":                "object",
			"x-archmotif-node-id": n.ID,
		}
	}
	if len(pathCollisions) > 0 || len(schemaCollisions) > 0 {
		var b strings.Builder
		if len(pathCollisions) > 0 {
			sort.Slice(pathCollisions, func(i, j int) bool { return pathCollisions[i][0] < pathCollisions[j][0] })
			b.WriteString("path collisions: ")
			for i, c := range pathCollisions {
				if i > 0 {
					b.WriteString("; ")
				}
				fmt.Fprintf(&b, "%s (%s vs %s)", c[0], c[1], c[2])
			}
		}
		if len(schemaCollisions) > 0 {
			if b.Len() > 0 {
				b.WriteString("; ")
			}
			sort.Slice(schemaCollisions, func(i, j int) bool { return schemaCollisions[i][0] < schemaCollisions[j][0] })
			b.WriteString("schema collisions: ")
			for i, c := range schemaCollisions {
				if i > 0 {
					b.WriteString("; ")
				}
				fmt.Fprintf(&b, "%s (%s vs %s)", c[0], c[1], c[2])
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrOpenAPICollision, b.String())
	}
	doc := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "archmotif extracted contracts: " + graphID,
			"version": "0.0.0",
		},
		"paths": paths,
	}
	if len(schemas) > 0 {
		doc["components"] = map[string]any{"schemas": schemas}
	}
	return doc, nil
}

// exportJSON produces a flat JSON dump of every tagged contract for callers
// (e.g. JDUI) that want the raw catalog rather than a domain-specific format.
func (s *Service) exportJSON(graphID string) (map[string]any, error) {
	recs, err := s.ContractsList(graphID, "", "")
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(recs)
	if err != nil {
		return nil, err
	}
	var contracts []any
	_ = json.Unmarshal(b, &contracts)
	return map[string]any{
		"graph_id":  graphID,
		"count":     len(recs),
		"contracts": contracts,
	}, nil
}

// slugifyName turns a name into a path-safe ASCII slug. Empty names become "_".
func slugifyName(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		return "_"
	}
	return s
}
