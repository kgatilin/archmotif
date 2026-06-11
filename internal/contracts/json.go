package contracts

import (
	"encoding/json"
	"fmt"
	"io"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// JSON is the on-disk / on-stdout serialisation form of `archmotif
// contracts` output. Versioned so we can evolve later without silent
// breakage.
type JSON struct {
	Version          int                `json:"version"`
	ModuleRoot       string             `json:"moduleRoot,omitempty"`
	ConfigPath       string             `json:"configPath,omitempty"`
	Contracts        []ContractRecord   `json:"contracts"`
	Unresolved       []UnresolvedRecord `json:"unresolved,omitempty"`
	KindMismatches   []string           `json:"kindMismatches,omitempty"`
	LoadErrors       []string           `json:"loadErrors,omitempty"`
	GraphNodeCount   int                `json:"graphNodeCount"`
	GraphEdgeCount   int                `json:"graphEdgeCount"`
	GraphMarkedCount int                `json:"graphMarkedCount"`
}

// ContractRecord is one contract plus its discovered materialisations.
type ContractRecord struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	QName      string           `json:"qname,omitempty"`
	Pos        mgraph.Position  `json:"pos,omitempty"`
	Kind       string           `json:"kind"`   // "interface" or "type"
	Source     string           `json:"source"` // "config" or "embedded"
	EmbedsFrom string           `json:"embedsFrom,omitempty"`
	Producers  []ProducerRecord `json:"producers"`
}

// ProducerRecord is one producer entry attached to a contract.
type ProducerRecord struct {
	ID    string          `json:"id"`
	Kind  ProducerKind    `json:"kind"` // "returns" or "implements"
	Name  string          `json:"name"`
	QName string          `json:"qname,omitempty"`
	Pos   mgraph.Position `json:"pos,omitempty"`
	// NodeKind is the underlying graph node kind (function, method,
	// type) — included so consumers can filter without re-resolving.
	NodeKind mgraph.NodeKind `json:"nodeKind"`
}

// UnresolvedRecord is one Config entry that could not be matched to a
// loaded type.
type UnresolvedRecord struct {
	Kind       EntryKind `json:"kind"`
	Identifier string    `json:"identifier"`
	Reason     string    `json:"reason"`
}

// CurrentJSONVersion is the version emitted by ToJSON. Bump on
// breaking changes.
const CurrentJSONVersion = 1

// ToJSON converts the Result into its serialisable form.
func (r *Result) ToJSON() JSON {
	out := JSON{
		Version:        CurrentJSONVersion,
		ModuleRoot:     r.ModuleRoot,
		ConfigPath:     r.ConfigPath,
		LoadErrors:     append([]string(nil), r.LoadErrors...),
		KindMismatches: append([]string(nil), r.KindMismatches...),
		Contracts:      make([]ContractRecord, 0, len(r.Materialisations)),
		Unresolved:     make([]UnresolvedRecord, 0, len(r.Unresolved)),
		GraphNodeCount: r.Graph.NodeCount(),
		GraphEdgeCount: r.Graph.EdgeCount(),
	}
	for _, m := range r.Materialisations {
		rec := ContractRecord{
			ID:        m.Contract.ID,
			Name:      m.Contract.Name,
			QName:     m.Contract.QName,
			Pos:       m.Contract.Pos,
			Kind:      m.Contract.ContractKind(),
			Source:    m.Source,
			Producers: make([]ProducerRecord, 0, len(m.Producers)),
		}
		if m.Contract.Attrs != nil {
			if v, ok := m.Contract.Attrs[mgraph.AttrContractEmbeds]; ok {
				if s, ok := v.(string); ok {
					rec.EmbedsFrom = s
				}
			}
		}
		for _, p := range m.Producers {
			rec.Producers = append(rec.Producers, ProducerRecord{
				ID:       p.Node.ID,
				Kind:     p.Kind,
				Name:     p.Node.Name,
				QName:    p.Node.QName,
				Pos:      p.Node.Pos,
				NodeKind: p.Node.Kind,
			})
		}
		out.Contracts = append(out.Contracts, rec)
	}
	for _, u := range r.Unresolved {
		out.Unresolved = append(out.Unresolved, UnresolvedRecord{
			Kind:       u.Entry.Kind(),
			Identifier: u.Entry.Identifier(),
			Reason:     u.Reason,
		})
	}
	out.GraphMarkedCount = len(out.Contracts)
	return out
}

// WriteJSON encodes the result as JSON with two-space indentation.
func (r *Result) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.ToJSON())
}

// PrettyPrint renders a human-readable summary. The output is intended
// to be readable on a terminal; not a stable format.
func (r *Result) PrettyPrint(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "contracts: %d declared, %d resolved, %d unresolved\n",
		len(r.Config.Contracts), len(r.Resolved), len(r.Unresolved)); err != nil {
		return err
	}
	if r.ConfigPath != "" {
		if _, err := fmt.Fprintf(w, "config: %s\n", r.ConfigPath); err != nil {
			return err
		}
	}
	if r.Graph != nil {
		if _, err := fmt.Fprintf(w, "graph:  %d nodes, %d edges\n", r.Graph.NodeCount(), r.Graph.EdgeCount()); err != nil {
			return err
		}
	}
	for _, m := range r.Materialisations {
		marker := "[contract]"
		if m.Source == "embedded" {
			marker = "[contract:embedded]"
		}
		label := m.Contract.QName
		if label == "" {
			label = m.Contract.Name
		}
		if _, err := fmt.Fprintf(w, "\n%s %s (%s)\n", marker, label, m.Contract.ContractKind()); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  declared at: %s\n", m.Contract.ID); err != nil {
			return err
		}
		if m.Source == "embedded" && m.Contract.Attrs != nil {
			if origin, ok := m.Contract.Attrs[mgraph.AttrContractEmbeds].(string); ok {
				if _, err := fmt.Fprintf(w, "  embeds:      %s\n", origin); err != nil {
					return err
				}
			}
		}
		if len(m.Producers) == 0 {
			if _, err := fmt.Fprintf(w, "  producers:   (none — no Returns or Implements producers in the loaded set)\n"); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "  producers:\n"); err != nil {
			return err
		}
		for _, p := range m.Producers {
			label := p.Node.QName
			if label == "" {
				label = p.Node.Name
			}
			if _, err := fmt.Fprintf(w, "    [%s] %s — %s\n", p.Kind, label, p.Node.ID); err != nil {
				return err
			}
		}
	}
	if len(r.KindMismatches) > 0 {
		if _, err := fmt.Fprintf(w, "\nkind mismatches:\n"); err != nil {
			return err
		}
		for _, m := range r.KindMismatches {
			if _, err := fmt.Fprintf(w, "  - %s\n", m); err != nil {
				return err
			}
		}
	}
	if len(r.Unresolved) > 0 {
		if _, err := fmt.Fprintf(w, "\nunresolved:\n"); err != nil {
			return err
		}
		for _, u := range r.Unresolved {
			if _, err := fmt.Fprintf(w, "  - %s (%s): %s\n", u.Entry.Identifier(), u.Entry.Kind(), u.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}
