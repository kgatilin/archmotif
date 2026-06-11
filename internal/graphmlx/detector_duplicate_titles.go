package graphmlx

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// DuplicateTitlesDetector flags clusters of nodes with the same or
// near-identical title/label/summary. The "near" comparison is
// deterministic: we strip whitespace, lowercase, and collapse
// punctuation, then bucket by the canonical key. This deduplicates stubs
// without pulling in fuzzy-match libraries (issue #37 spec calls out
// "repeated or near-identical titles/session summaries/stubs").
//
// One Finding per bucket with size >= 2. Severity grows with bucket size.
type DuplicateTitlesDetector struct{}

// Name returns the detector identifier.
func (DuplicateTitlesDetector) Name() string { return "duplicate_titles" }

// Description returns the detector documentation string.
func (DuplicateTitlesDetector) Description() string {
	return "groups nodes whose title/label/summary collapses to the same canonical form"
}

// Detect emits one finding per duplicate-title bucket with size >= 2.
func (d DuplicateTitlesDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	type bucket struct {
		canonical string
		raw       string // first non-empty raw title seen, for the message
		members   []string
	}
	buckets := map[string]*bucket{}
	for _, n := range g.Nodes {
		raw := pickFirst(n.Attrs, "title", "summary", "label", "name")
		if raw == "" {
			raw = n.Label
		}
		if raw == "" {
			continue
		}
		c := canonicalTitle(raw)
		if c == "" {
			continue
		}
		b, ok := buckets[c]
		if !ok {
			b = &bucket{canonical: c, raw: raw}
			buckets[c] = b
		}
		b.members = append(b.members, n.ID)
	}
	keys := make([]string, 0, len(buckets))
	for k, b := range buckets {
		if len(b.members) < 2 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Finding, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		members := append([]string(nil), b.members...)
		sort.Strings(members)
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     float64(len(members)),
			Severity:  duplicateTitleSeverity(len(members)),
			PrimaryID: members[0],
			Members:   members,
			Reason: Reason{
				Code:    "duplicate_titles",
				Message: fmt.Sprintf("%d nodes share canonical title %q", len(members), k),
				Details: map[string]any{
					"canonical": k,
					"sample":    b.raw,
					"count":     len(members),
				},
			},
			Evidence: map[string]any{
				"canonical": k,
				"raw":       b.raw,
				"count":     len(members),
				"members":   members,
			},
		})
	}
	return out, nil
}

// canonicalTitle lowercases s, drops punctuation, and collapses
// runs of whitespace. Pure-Go, no external libs (deterministic).
func canonicalTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevSpace = false
		case unicode.IsSpace(r), unicode.IsPunct(r), unicode.IsSymbol(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func duplicateTitleSeverity(count int) Severity {
	switch {
	case count >= 10:
		return SeverityHigh
	case count >= 5:
		return SeverityMedium
	case count >= 3:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func init() { Register(DuplicateTitlesDetector{}) }
