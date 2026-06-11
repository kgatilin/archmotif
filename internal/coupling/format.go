package coupling

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/graph"
)

// RenderJSON writes the report as a stable JSON envelope. Indented
// output (two spaces) so diffs in regression-test snapshots stay
// readable.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	envelope := jsonEnvelope{
		Version:             1,
		PairCounts:          r.PairCounts,
		ForbiddenViolations: r.ForbiddenViolations,
		Scores:              r.Scores,
		EdgesConsidered:     r.EdgesConsidered,
		UnroledEndpoints:    r.UnroledEndpoints,
	}
	return enc.Encode(envelope)
}

type jsonEnvelope struct {
	Version             int                  `json:"version"`
	PairCounts          []PairCount          `json:"pairCounts"`
	ForbiddenViolations []ForbiddenViolation `json:"forbiddenViolations,omitempty"`
	Scores              []Score              `json:"scores"`
	EdgesConsidered     int                  `json:"edgesConsidered"`
	UnroledEndpoints    int                  `json:"unroledEndpoints"`
}

// RenderMarkdown writes a human-friendly Markdown summary: a scores
// section, a role-pair matrix table, and a forbidden-edge violations
// list. Format is stable so snapshot tests can assert on it.
func RenderMarkdown(w io.Writer, r Report) error {
	var b strings.Builder
	b.WriteString("# Coupling report\n\n")
	fmt.Fprintf(&b, "Edges considered: %d\n\n", r.EdgesConsidered)
	if r.UnroledEndpoints > 0 {
		fmt.Fprintf(&b, "Unroled endpoints (config gaps): %d\n\n", r.UnroledEndpoints)
	}

	b.WriteString("## Scores\n\n")
	if len(r.Scores) == 0 {
		b.WriteString("_no scores computed_\n\n")
	} else {
		b.WriteString("| score | value | numerator | denominator |\n")
		b.WriteString("|-------|------:|----------:|------------:|\n")
		for _, s := range r.Scores {
			fmt.Fprintf(&b, "| `%s` | %s | %d | %d |\n",
				s.Name, formatValue(s.Value), s.Numerator, s.Denominator)
		}
		b.WriteString("\n")
		for _, s := range r.Scores {
			fmt.Fprintf(&b, "- `%s` — %s\n", s.Name, s.Description)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Role-pair matrix\n\n")
	if len(r.PairCounts) == 0 {
		b.WriteString("_no edges in scope_\n\n")
	} else {
		b.WriteString("| from | to | count |\n")
		b.WriteString("|------|----|------:|\n")
		for _, p := range r.PairCounts {
			fmt.Fprintf(&b, "| `%s` | `%s` | %d |\n",
				roleOrEmpty(p.Pair.From), roleOrEmpty(p.Pair.To), p.Count)
		}
		b.WriteString("\n")
	}

	if len(r.ForbiddenViolations) > 0 {
		b.WriteString("## Forbidden-edge violations\n\n")
		// Group violations by rule for readability.
		grouped := groupViolations(r.ForbiddenViolations)
		for _, key := range sortedRuleKeys(grouped) {
			rule := grouped[key].rule
			ev := grouped[key].evidence
			reason := rule.Reason
			if reason == "" {
				reason = fmt.Sprintf("edge from %s to %s is forbidden", rule.From, rule.To)
			}
			fmt.Fprintf(&b, "### `%s` -> `%s` (%d)\n\n",
				roleOrEmpty(rule.From), roleOrEmpty(rule.To), len(ev))
			fmt.Fprintf(&b, "_%s_\n\n", reason)
			for _, e := range ev {
				fmt.Fprintf(&b, "- `%s` -[%s]-> `%s`\n",
					nameOr(e.FromName, e.From), e.EdgeKind, nameOr(e.ToName, e.To))
			}
			b.WriteString("\n")
		}
	}
	_, err := w.Write([]byte(b.String()))
	return err
}

type ruleGroup struct {
	rule     ForbiddenEdge
	evidence []EdgeEvidence
}

func groupViolations(vs []ForbiddenViolation) map[string]ruleGroup {
	out := make(map[string]ruleGroup)
	for _, v := range vs {
		key := string(v.Rule.From) + "->" + string(v.Rule.To)
		g := out[key]
		g.rule = v.Rule
		g.evidence = append(g.evidence, v.Evidence)
		out[key] = g
	}
	return out
}

func sortedRuleKeys(m map[string]ruleGroup) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatValue(v float64) string {
	// Render NaN as "—" so Markdown stays valid; otherwise three
	// decimals like the metric runner.
	if v != v { // NaN
		return "—"
	}
	return fmt.Sprintf("%.3f", v)
}

func roleOrEmpty(r graph.Role) graph.Role {
	if r == "" {
		return UnknownRole
	}
	return r
}

func nameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}
