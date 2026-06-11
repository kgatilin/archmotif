package contract

import (
	"fmt"
	"sort"

	"github.com/kgatilin/archmotif/internal/graphmlx"
	"gopkg.in/yaml.v3"
)

// Rules is a dependency policy: group nodes by Partition, then only the listed
// cross-group directions are allowed. Everything else is a violation. This is
// the generic "who may depend on whom" primitive (R4).
//
//	partition: group
//	allow:
//	  - from: pipeline
//	    to: core
//	sink: [core]      # optional: these groups must have no outgoing cross-group edge
type Rules struct {
	Partition string      `yaml:"partition"`
	Allow     []AllowRule `yaml:"allow"`
	Sink      []string    `yaml:"sink"`
}

type AllowRule struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Violation is one edge that breaks the policy.
type Violation struct {
	From      string `json:"from"`
	To        string `json:"to"`
	FromGroup string `json:"from_group"`
	ToGroup   string `json:"to_group"`
	Reason    string `json:"reason"`
}

// ParseRules decodes a policy YAML document.
func ParseRules(data []byte) (Rules, error) {
	var r Rules
	if err := yaml.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parse rules: %w", err)
	}
	if r.Partition == "" {
		return r, fmt.Errorf("policy: `partition` is required")
	}
	return r, nil
}

// Residual returns the edges of g that violate the policy. Empty = conformant.
func Residual(g *graphmlx.Graph, rules Rules) []Violation {
	p := PartitionBy(g, rules.Partition)
	allow := map[[2]string]bool{}
	for _, a := range rules.Allow {
		allow[[2]string{a.From, a.To}] = true
	}
	sink := map[string]bool{}
	for _, s := range rules.Sink {
		sink[s] = true
	}
	var vs []Violation
	for _, e := range g.Edges {
		a, b := p[e.From], p[e.To]
		if a == b {
			continue // intra-group always allowed
		}
		var reason string
		switch {
		case sink[a]:
			reason = fmt.Sprintf("%q is a sink group and must not depend on %q", a, b)
		case !allow[[2]string{a, b}]:
			reason = fmt.Sprintf("%q -> %q is not an allowed dependency direction", a, b)
		}
		if reason != "" {
			vs = append(vs, Violation{From: e.From, To: e.To, FromGroup: a, ToGroup: b, Reason: reason})
		}
	}
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].From != vs[j].From {
			return vs[i].From < vs[j].From
		}
		return vs[i].To < vs[j].To
	})
	return vs
}
