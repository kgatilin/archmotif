// Package roles resolves the `roles:` block of `.archmotif.yaml`
// against a typed graph and produces a node-id -> role map.
//
// Selector precedence (lowest to highest):
//
//  1. inferred (currently unused; reserved for future heuristics)
//  2. explicit package selector match
//  3. explicit type/symbol selector match
//
// The Apply helper writes the resolved roles back onto graph nodes via
// graph.SetRole, recording provenance in graph.AttrRoleSource.
//
// See docs/decisions/027-role-metadata.md for design rationale.
package roles

import (
	"path"
	"strings"

	"github.com/kgatilin/archmotif/internal/contracts"
	"github.com/kgatilin/archmotif/internal/graph"
)

// SourceType records how a role was assigned. Stored in
// graph.AttrRoleSource. Higher precedence wins overrides.
const (
	SourceInferred = "inferred"
	SourcePackage  = "package"
	SourceType     = "type"
)

// Resolution maps stable node IDs to a resolved role + source.
type Resolution struct {
	Role   graph.Role
	Source string
}

// Resolve walks every node in g, applies the package- and type-scoped
// selectors from cfg, and returns a map of node ID -> resolution. It
// does not mutate the graph; pass the returned map to Apply to write
// the role back onto Node.Attrs.
//
// Precedence: a type/symbol selector that matches a node always wins
// over a package selector that also matches the same node.
func Resolve(g *graph.Graph, cfg contracts.Roles) map[string]Resolution {
	out := make(map[string]Resolution)
	if g == nil {
		return out
	}
	for _, n := range g.Nodes() {
		// Package selectors first.
		if role, ok := matchPackage(n, cfg.Packages); ok {
			out[n.ID] = Resolution{Role: role, Source: SourcePackage}
		}
		// Type selectors override (or fill in if no package match).
		if role, ok := matchType(n, cfg.Types); ok {
			out[n.ID] = Resolution{Role: role, Source: SourceType}
		}
	}
	return out
}

// Apply writes the resolved roles back onto g via graph.SetRole.
// Returns the number of nodes annotated.
func Apply(g *graph.Graph, res map[string]Resolution) int {
	if g == nil {
		return 0
	}
	count := 0
	for id, r := range res {
		if g.SetRole(id, r.Role, r.Source) {
			count++
		}
	}
	return count
}

// matchPackage walks the package selectors and returns the role of the
// first match. Selectors with `qualified:` set match when qualified
// equals the node's QName *or* the node's package import path; selectors
// with `pattern:` glob-match the package import path, the file path, or
// the QName.
func matchPackage(n graph.Node, sels []contracts.RoleSelector) (graph.Role, bool) {
	for _, sel := range sels {
		if sel.Qualified != "" && qualifiedMatches(n, sel.Qualified) {
			return graph.Role(sel.Role), true
		}
		if sel.Pattern != "" && patternMatches(n, sel.Pattern) {
			return graph.Role(sel.Role), true
		}
	}
	return "", false
}

// matchType uses the same matching logic as matchPackage but on the
// type/symbol selector list. Kept as a distinct function so future
// refinements (e.g. tightening the predicate to "type-kind nodes only")
// don't bleed across.
func matchType(n graph.Node, sels []contracts.RoleSelector) (graph.Role, bool) {
	for _, sel := range sels {
		if sel.Qualified != "" && qualifiedMatches(n, sel.Qualified) {
			return graph.Role(sel.Role), true
		}
		if sel.Pattern != "" && patternMatches(n, sel.Pattern) {
			return graph.Role(sel.Role), true
		}
	}
	return "", false
}

// qualifiedMatches checks whether the qualified selector matches the
// node. Matches when QName == qualified, or for package nodes when the
// import path encoded in the ID equals qualified.
func qualifiedMatches(n graph.Node, qualified string) bool {
	if n.QName != "" && n.QName == qualified {
		return true
	}
	if n.Kind == graph.NodePackage && nodePackagePath(n) == qualified {
		return true
	}
	return false
}

// patternMatches glob-matches the selector pattern against any of the
// node's identifying strings: package import path, file path, qualified
// name. The first match wins.
func patternMatches(n graph.Node, pattern string) bool {
	candidates := candidateStrings(n)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if globMatch(pattern, c) {
			return true
		}
	}
	return false
}

func candidateStrings(n graph.Node) []string {
	out := make([]string, 0, 3)
	if pkg := nodePackagePath(n); pkg != "" {
		out = append(out, pkg)
	}
	if n.Pos.File != "" {
		out = append(out, n.Pos.File)
	}
	if n.QName != "" {
		out = append(out, n.QName)
	}
	return out
}

// nodePackagePath extracts the package import path from a node when
// possible. Package nodes encode the import path in QName or as the
// `pkg:<import-path>` ID prefix; non-package nodes carry the package
// path in QName up to the last dot.
func nodePackagePath(n graph.Node) string {
	if n.Kind == graph.NodePackage {
		if n.QName != "" {
			return n.QName
		}
		if strings.HasPrefix(n.ID, "pkg:") {
			rest := strings.TrimPrefix(n.ID, "pkg:")
			// `pkg:<import-path>` for Package nodes; `pkg:<import-path>:<kind>`
			// for foreign placeholders. Return everything up to the first
			// trailing `:<kind>` segment.
			if i := strings.LastIndex(rest, ":"); i >= 0 {
				return rest[:i]
			}
			return rest
		}
		return ""
	}
	if n.QName == "" {
		return ""
	}
	// Method qnames have a receiver prefix like `(*pkg/path.Type).Method`.
	q := n.QName
	if strings.HasPrefix(q, "(") {
		if end := strings.Index(q, ")"); end > 0 {
			recv := q[1:end]
			recv = strings.TrimPrefix(recv, "*")
			if i := strings.LastIndex(recv, "."); i > 0 {
				return recv[:i]
			}
		}
		return ""
	}
	if i := strings.LastIndex(q, "."); i > 0 {
		return q[:i]
	}
	return ""
}

// globMatch implements `*` (any chars within a path segment) and `**`
// (any chars across segments) plus literal `?` (single non-separator
// char). Falls back to path.Match for patterns without `**`.
func globMatch(pattern, s string) bool {
	if !strings.Contains(pattern, "**") {
		// path.Match treats `/` as a separator and `*` as
		// non-crossing; that's the segment-scoped semantics we want.
		ok, err := path.Match(pattern, s)
		if err != nil {
			return false
		}
		return ok
	}
	// Translate `**` to a multi-segment wildcard via a hand-rolled
	// recursive matcher. Simpler than building a regex and avoids a
	// runtime dependency.
	return doubleStarMatch(pattern, s)
}

// doubleStarMatch supports `**` as "match any characters including
// `/`". Otherwise honours the same `*`, `?`, literal semantics as
// path.Match.
func doubleStarMatch(pattern, s string) bool {
	// Empty pattern matches only empty string.
	if pattern == "" {
		return s == ""
	}
	if pattern == "**" {
		return true
	}
	// Process token by token.
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			// Look for `**` (multi-segment) vs `*` (single segment).
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				rest := pattern[i+2:]
				// Strip a leading `/` after `**` so `a/**/b` matches `a/b`.
				rest = strings.TrimPrefix(rest, "/")
				// Try to match `rest` at every suffix of s.
				for j := 0; j <= len(s); j++ {
					if doubleStarMatch(rest, s[j:]) {
						return true
					}
				}
				return false
			}
			// Single `*`: no separator crossing.
			rest := pattern[i+1:]
			for j := 0; j <= len(s); j++ {
				// Don't let `*` swallow `/`.
				if j > 0 && s[j-1] == '/' {
					break
				}
				if doubleStarMatch(rest, s[j:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 || s[0] == '/' {
				return false
			}
			s = s[1:]
		default:
			if len(s) == 0 || s[0] != pattern[i] {
				return false
			}
			s = s[1:]
		}
	}
	return s == ""
}
