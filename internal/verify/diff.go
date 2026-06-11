package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// FormatText renders a Result in the human-readable text form
// documented in ADR-018. Match results render a compact "Match" line
// plus the role bindings; Mismatch results render the missing-role,
// failing-edge, and partial-mapping sections.
func FormatText(w io.Writer, proposalID string, r Result) error {
	if r.Match {
		_, err := fmt.Fprintf(w, "Match on proposal %s:\n%s", proposalID, renderBindings(r.Bindings))
		return err
	}
	if r.Diff == nil {
		_, err := fmt.Fprintf(w, "Mismatch on proposal %s: (no diff produced)\n", proposalID)
		return err
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Mismatch on proposal %s:\n", proposalID)
	if r.Diff.Reason != "" {
		_, _ = fmt.Fprintf(&b, "  Reason: %s\n", r.Diff.Reason)
	}
	if len(r.Diff.MissingRoles) > 0 {
		for _, mr := range r.Diff.MissingRoles {
			_, _ = fmt.Fprintf(&b, "  Role <%s>:    %s\n", mr.Role, mr.Reason)
		}
	}
	if len(r.Diff.FailingEdges) > 0 {
		for _, fe := range r.Diff.FailingEdges {
			_, _ = fmt.Fprintf(&b, "  Edge (%s, %s, %s): %s\n",
				fe.Edge.From, fe.Edge.Kind, fe.Edge.To, fe.Reason)
		}
	}
	if len(r.Diff.PartialMapping) > 0 {
		_, _ = fmt.Fprintf(&b, "  Partial mapping:\n")
		for _, role := range sortedKeys(r.Diff.PartialMapping) {
			_, _ = fmt.Fprintf(&b, "    %s -> %s\n", role, r.Diff.PartialMapping[role])
		}
	}
	b.WriteString("  Removed roles: -\n")
	b.WriteString("  Extra nodes:    none considered (strict subgraph match).\n")
	_, err := w.Write([]byte(b.String()))
	return err
}

// FormatJSON renders a Result as a stable JSON envelope. The shape is
// intended for downstream tools (Stage 9 orchestrator, Stage 7
// prompt-feedback) that consume the diff programmatically.
func FormatJSON(w io.Writer, proposalID string, r Result) error {
	envelope := jsonEnvelope{
		Version:    1,
		ProposalID: proposalID,
		Match:      r.Match,
		Mapping:    r.Mapping,
		Bindings:   r.Bindings,
	}
	if r.Diff != nil {
		envelope.Diff = &jsonDiff{
			Reason:         r.Diff.Reason,
			MissingRoles:   r.Diff.MissingRoles,
			FailingEdges:   r.Diff.FailingEdges,
			PartialMapping: r.Diff.PartialMapping,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

type jsonEnvelope struct {
	Version    int               `json:"version"`
	ProposalID string            `json:"proposal_id"`
	Match      bool              `json:"match"`
	Mapping    map[string]NodeID `json:"mapping,omitempty"`
	Bindings   map[string]string `json:"bindings,omitempty"`
	Diff       *jsonDiff         `json:"diff,omitempty"`
}

type jsonDiff struct {
	Reason         string            `json:"reason,omitempty"`
	MissingRoles   []MissingRole     `json:"missing_roles,omitempty"`
	FailingEdges   []FailingEdge     `json:"failing_edges,omitempty"`
	PartialMapping map[string]NodeID `json:"partial_mapping,omitempty"`
}

func renderBindings(b map[string]string) string {
	if len(b) == 0 {
		return "  (no bindings)\n"
	}
	var sb strings.Builder
	for _, k := range sortedKeys(b) {
		_, _ = fmt.Fprintf(&sb, "  <%s> = %s\n", k, b[k])
	}
	return sb.String()
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
