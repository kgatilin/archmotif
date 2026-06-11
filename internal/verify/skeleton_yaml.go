package verify

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// LoadSkeletonFile reads a Stage 6 skeleton.yaml (per ADR-016) and
// returns the verifier-shaped Skeleton. The schema is the YAML
// companion grammar pinned in #16.
//
// Note: while issue #19 owns Proposal/TargetSubgraph in
// internal/propose, this loader exists here so the verifier can be
// exercised end-to-end against the on-disk skeleton format without
// importing the proposer package (see ADR-018 §5).
func LoadSkeletonFile(path string) (Skeleton, error) {
	f, err := os.Open(path)
	if err != nil {
		return Skeleton{}, fmt.Errorf("verify: open skeleton %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return ParseSkeleton(f)
}

// ParseSkeleton decodes the skeleton YAML stream from r.
func ParseSkeleton(r io.Reader) (Skeleton, error) {
	var raw skeletonYAML
	dec := yaml.NewDecoder(r)
	dec.KnownFields(false) // tolerate forward-compat fields
	if err := dec.Decode(&raw); err != nil {
		return Skeleton{}, fmt.Errorf("verify: decode skeleton yaml: %w", err)
	}
	return raw.toSkeleton()
}

// skeletonYAML mirrors the on-disk grammar from ADR-016. We accept
// extra unknown fields silently so #16's evolving format stays
// readable here.
type skeletonYAML struct {
	ProposalID     string              `yaml:"proposal_id"`
	Description    string              `yaml:"description"`
	Affected       []string            `yaml:"affected"`
	TargetSubgraph targetSubgraphYAML  `yaml:"target_subgraph"`
	Samples        []map[string]string `yaml:"samples"`
}

type targetSubgraphYAML struct {
	Roles []roleYAML `yaml:"roles"`
	Edges []edgeYAML `yaml:"edges"`
}

type roleYAML struct {
	ID           string        `yaml:"id"`
	Kind         string        `yaml:"kind"`
	Methods      []methodYAML  `yaml:"methods"`
	ReceiverRole string        `yaml:"receiver_role"`
	Realises     *realisesYAML `yaml:"realises"`
}

type methodYAML struct {
	NameRole   string      `yaml:"name_role"`
	Params     []paramYAML `yaml:"params"`
	ReturnRole string      `yaml:"return_role"`
}

type paramYAML struct {
	Role     string `yaml:"role"`
	TypeRole string `yaml:"type_role"`
}

type realisesYAML struct {
	Role   string `yaml:"role"`
	Method string `yaml:"method"`
}

type edgeYAML struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
	Kind string `yaml:"kind"`
}

func (s skeletonYAML) toSkeleton() (Skeleton, error) {
	out := Skeleton{
		ProposalID: s.ProposalID,
	}
	if out.ProposalID == "" {
		return Skeleton{}, fmt.Errorf("verify: skeleton missing proposal_id")
	}

	for _, r := range s.TargetSubgraph.Roles {
		if r.ID == "" {
			return Skeleton{}, fmt.Errorf("verify: role with empty id in skeleton %s", out.ProposalID)
		}
		kind, err := parseNodeKind(r.Kind)
		if err != nil {
			return Skeleton{}, fmt.Errorf("verify: role %q: %w", r.ID, err)
		}
		role := Role{
			ID:           r.ID,
			Kind:         kind,
			ReceiverRole: r.ReceiverRole,
		}
		if r.Realises != nil {
			role.Realises = &Realisation{
				Role:   r.Realises.Role,
				Method: r.Realises.Method,
			}
		}
		// v1: skeleton roles describe interface methods via the
		// roles[].methods slice. The verifier consumes the *first*
		// declared method as the role's MethodShape when the role
		// itself is a method; for an interface role the methods
		// list informs method-role candidate filtering downstream
		// (currently only the explicit ReceiverRole/Realises wires
		// that connection — see ADR-018 for the v1 trade-off).
		if len(r.Methods) > 0 && (kind == mgraph.NodeMethod || kind == mgraph.NodeFunction) {
			m := r.Methods[0]
			shape := &MethodShape{}
			for _, p := range m.Params {
				shape.ParamKinds = append(shape.ParamKinds, mgraph.NodeType)
				_ = p
			}
			if m.ReturnRole != "" {
				shape.ReturnKind = mgraph.NodeType
			}
			role.MethodShape = shape
		}
		out.Roles = append(out.Roles, role)
	}

	for _, e := range s.TargetSubgraph.Edges {
		ek, err := parseEdgeKind(e.Kind)
		if err != nil {
			return Skeleton{}, err
		}
		out.Edges = append(out.Edges, EdgeConstraint{
			From: e.From,
			To:   e.To,
			Kind: ek,
		})
	}
	return out, nil
}

// parseNodeKind maps the skeleton grammar kind strings to graph node
// kinds. Per ADR-016, valid values are {interface, struct, method,
// function, field, type}. Interface and struct fold to NodeType
// (graph distinguishes via Attrs.typeKind), the rest map directly.
func parseNodeKind(s string) (mgraph.NodeKind, error) {
	switch s {
	case "interface", "struct", "type":
		return mgraph.NodeType, nil
	case "method":
		return mgraph.NodeMethod, nil
	case "function":
		return mgraph.NodeFunction, nil
	case "field":
		return mgraph.NodeField, nil
	case "":
		return "", fmt.Errorf("empty node kind")
	default:
		return "", fmt.Errorf("unknown skeleton node kind %q", s)
	}
}

// parseEdgeKind maps a skeleton edge kind string to a graph edge
// kind. Accepts any of the canonical Stage 1 kinds; rejects unknown
// values loudly so a typo in the skeleton fails fast.
func parseEdgeKind(s string) (mgraph.EdgeKind, error) {
	for _, k := range mgraph.AllEdgeKinds() {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown edge kind %q", s)
}
