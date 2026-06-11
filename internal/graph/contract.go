package graph

// Contract-related Attrs keys. Defined here so Stage 2 (and later)
// callers don't sprinkle string literals across the codebase. ADR-009
// records the rationale for using the existing Attrs map rather than
// adding typed fields to Node.
const (
	// AttrIsContract is the Attrs key holding a bool that is true when
	// the node has been marked as a contract by `archmotif contracts`
	// (Stage 2).
	AttrIsContract = "isContract"
	// AttrContractKind is the Attrs key holding the contract
	// declaration form ("interface" or "type") that mirrors the entry
	// in `.archmotif.yaml`.
	AttrContractKind = "contractKind"
	// AttrContractSource is the Attrs key holding the marker
	// provenance: "config" for an explicit YAML entry, "embedded" for
	// a marker propagated transitively through interface embedding,
	// and in future "comment" or "inferred".
	AttrContractSource = "contractSource"
	// AttrContractEmbeds is the Attrs key holding the node ID of the
	// origin contract when the marker was propagated via interface
	// embedding (source == "embedded"). Useful for traceability.
	AttrContractEmbeds = "contractEmbeds"
)

// IsContract reports whether the node is marked as a contract. Returns
// false for nodes without an AttrIsContract entry.
func (n Node) IsContract() bool {
	if n.Attrs == nil {
		return false
	}
	v, ok := n.Attrs[AttrIsContract]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// ContractKind returns the declaration form ("interface" or "type") if
// the node carries the AttrContractKind attribute; "" otherwise.
func (n Node) ContractKind() string {
	if n.Attrs == nil {
		return ""
	}
	v, ok := n.Attrs[AttrContractKind]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ContractSource returns the marker provenance ("config", "embedded",
// …) or "" when the node is not a contract.
func (n Node) ContractSource() string {
	if n.Attrs == nil {
		return ""
	}
	v, ok := n.Attrs[AttrContractSource]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// MarkContract sets the contract attributes on the node with the given
// stable ID. If the node already carries a contract marker, the
// existing attributes win — explicit configuration takes priority over
// later transitive propagation.
//
// Returns true when the node was newly marked, false when it already
// carried a marker (or didn't exist).
func (g *Graph) MarkContract(id, kind, source string, extra map[string]any) bool {
	entry, ok := g.nodes[id]
	if !ok {
		return false
	}
	if entry.node.Attrs == nil {
		entry.node.Attrs = make(map[string]any)
	}
	if existing, has := entry.node.Attrs[AttrIsContract]; has {
		if b, _ := existing.(bool); b {
			return false
		}
	}
	entry.node.Attrs[AttrIsContract] = true
	if kind != "" {
		entry.node.Attrs[AttrContractKind] = kind
	}
	if source != "" {
		entry.node.Attrs[AttrContractSource] = source
	}
	for k, v := range extra {
		entry.node.Attrs[k] = v
	}
	return true
}
