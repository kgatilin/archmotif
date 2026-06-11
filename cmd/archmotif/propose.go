package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/propose"
)

// proposalsJSONVersion is the on-disk version of the propose JSON
// envelope. Bump on breaking schema changes.
const proposalsJSONVersion = 1

// proposeJSON is the top-level JSON envelope for `archmotif propose
// --format=json`. Mirrors the metrics/anomalies envelope shape so
// downstream tooling can detect schema bumps.
type proposeJSON struct {
	Version   int                 `json:"version"`
	Proposals []*propose.Proposal `json:"proposals"`
	Skipped   []*propose.Proposal `json:"skipped,omitempty"`
	Errors    []string            `json:"errors,omitempty"`
	Ranked    []proposalRankEntry `json:"ranked,omitempty"`
}

// proposalRankEntry records the score that drove conflict resolution
// for an accepted proposal. Score is the source anomaly's Score (per
// ADR-022). Surfaced so consumers can reproduce the ranking without
// re-running Stage 4.
type proposalRankEntry struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// runPropose implements the `archmotif propose` subcommand. It drives
// the full Stage 3 → Stage 4 → Stage 5 pipeline on the given path,
// returning ranked proposals for human or machine consumption.
//
// Per ADR-022 the CLI surface is:
//
//	archmotif propose [flags] <path>
//	  --format=text|json   default text
//	  --limit=N            default 10
//	  --list               list registered rules and exit
//	  --tests              include _test.go
//	  --pattern=...        go/packages pattern
//
// Exit codes:
//
//	0 — success
//	1 — pipeline error (parser, metrics, anomalies, or rule)
//	2 — argument error
func runPropose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif propose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listRules := fs.Bool("list", false, "list registered transformation rules and exit")
	format := fs.String("format", "text", "output format: text|json")
	limit := fs.Int("limit", 10, "limit output to top N proposals (0 = all)")
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif propose [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nRegistered rules: %s\n", strings.Join(propose.Names(), ", "))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *listRules {
		for _, r := range propose.All() {
			_, _ = fmt.Fprintf(stdout, "%-20s %s\n", r.Name(), r.Description())
		}
		return 0
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}

	dir := fs.Arg(0)
	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif propose: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	mres := metrics.Run(res.Graph, nil)
	for _, e := range mres.Errors {
		_, _ = fmt.Fprintf(stderr, "metric error: %s\n", e.Error())
	}

	ares := anomalies.Run(res.Graph, mres.Records, nil)
	for _, e := range ares.Errors {
		_, _ = fmt.Fprintf(stderr, "detector error: %s\n", e.Error())
	}

	pres := propose.NewProposer().Propose(res.Graph, ares.Anomalies)
	for _, e := range pres.Errors {
		_, _ = fmt.Fprintf(stderr, "rule error: %s\n", e.Error())
	}

	// Rank accepted proposals by score (desc) for output ordering.
	scoreByID := buildScoreMap(ares.Anomalies, pres.Proposals)
	ranked := append([]*propose.Proposal(nil), pres.Proposals...)
	sort.SliceStable(ranked, func(i, j int) bool {
		si, sj := scoreByID[ranked[i].ID], scoreByID[ranked[j].ID]
		if si != sj {
			return si > sj
		}
		return ranked[i].ID < ranked[j].ID
	})
	if *limit > 0 && *limit < len(ranked) {
		ranked = ranked[:*limit]
	}

	switch *format {
	case "text", "":
		writeProposalsText(stdout, ranked, scoreByID, len(pres.Proposals), len(pres.Skipped))
	case "json":
		envelope := proposeJSON{
			Version:   proposalsJSONVersion,
			Proposals: ranked,
			Skipped:   pres.Skipped,
		}
		for _, p := range ranked {
			envelope.Ranked = append(envelope.Ranked, proposalRankEntry{ID: p.ID, Score: scoreByID[p.ID]})
		}
		for _, e := range pres.Errors {
			envelope.Errors = append(envelope.Errors, e.Error())
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(envelope); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif propose: write json: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif propose: unknown format %q (want: text|json)\n", *format)
		return 2
	}

	if len(pres.Errors) > 0 {
		return 1
	}
	return 0
}

// buildScoreMap pairs each Proposal with its triggering anomaly's
// score. Used by both text and JSON output to rank proposals; the
// score is not stored on Proposal directly because Stage 6/7/8
// consumers don't need it.
func buildScoreMap(anoms []anomalies.Anomaly, props []*propose.Proposal) map[string]float64 {
	bestByTarget := map[string]float64{}
	for _, a := range anoms {
		t := a.SourceRecord.Target
		if existing, ok := bestByTarget[t]; !ok || a.Score > existing {
			bestByTarget[t] = a.Score
		}
	}
	out := map[string]float64{}
	for _, p := range props {
		if p.Trigger == nil {
			continue
		}
		out[p.ID] = bestByTarget[p.Trigger.Target]
	}
	return out
}

// writeProposalsText renders ranked proposals in human-readable form.
// Format mirrors `archmotif anomalies --format=table` for consistency:
// a header summary followed by per-proposal blocks.
func writeProposalsText(w io.Writer, ranked []*propose.Proposal, scores map[string]float64, total, skipped int) {
	_, _ = fmt.Fprintf(w, "proposals: %d accepted (%d skipped on conflict), showing %d:\n\n",
		total, skipped, len(ranked))
	if len(ranked) == 0 {
		_, _ = fmt.Fprintln(w, "(no proposals — no anomalies triggered any registered rule)")
		return
	}
	for i, p := range ranked {
		_, _ = fmt.Fprintf(w, "%d. %s (score %.2f)\n", i+1, p.Description, scores[p.ID])
		if p.Trigger != nil {
			_, _ = fmt.Fprintf(w, "   anomaly: %s / %s (value=%.0f)\n", p.Trigger.Metric, p.Trigger.Target, p.Trigger.Value)
		}
		_, _ = fmt.Fprintf(w, "   rule: %s\n", proposalRuleName(p))
		_, _ = fmt.Fprintf(w, "   target shape: %s\n", summariseTargetShape(p))
		if len(p.AffectedFiles) > 0 {
			_, _ = fmt.Fprintf(w, "   affects %d file(s):\n", len(p.AffectedFiles))
			max := 5
			for j, f := range p.AffectedFiles {
				if j >= max {
					_, _ = fmt.Fprintf(w, "     ... and %d more\n", len(p.AffectedFiles)-max)
					break
				}
				_, _ = fmt.Fprintf(w, "     - %s\n", f)
			}
		}
		if len(p.Samples) > 0 {
			_, _ = fmt.Fprintf(w, "   samples:\n")
			max := 3
			for j, s := range p.Samples {
				if j >= max {
					_, _ = fmt.Fprintf(w, "     ... and %d more\n", len(p.Samples)-max)
					break
				}
				_, _ = fmt.Fprintf(w, "     [%d] %s\n", j, summariseSample(s))
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}

// proposalRuleName extracts the rule name from a Proposal ID. By
// convention rule IDs start with "<rulename>-" (per
// extract_interface.go's Apply: "extract_interface-<target>"). When
// the convention doesn't hold we fall back to the full ID.
func proposalRuleName(p *propose.Proposal) string {
	if i := strings.Index(p.ID, "-"); i > 0 {
		return p.ID[:i]
	}
	return p.ID
}

// summariseTargetShape produces a one-line description of the
// TargetSubgraph (e.g. "1 Iface + 5 Impls + 5 Methods (Implements,
// Contains)").
func summariseTargetShape(p *propose.Proposal) string {
	parts := make([]string, 0, len(p.TargetSubgraph.Roles))
	for _, r := range p.TargetSubgraph.Roles {
		parts = append(parts, fmt.Sprintf("%d %s", r.Cardinality, r.Name))
	}
	edgeKinds := map[string]struct{}{}
	for _, e := range p.TargetSubgraph.Edges {
		edgeKinds[string(e.Kind)] = struct{}{}
	}
	kinds := make([]string, 0, len(edgeKinds))
	for k := range edgeKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	if len(kinds) == 0 {
		return strings.Join(parts, " + ")
	}
	return fmt.Sprintf("%s (%s)", strings.Join(parts, " + "), strings.Join(kinds, ", "))
}

// summariseSample renders one sample map into a compact key=value
// string, preferring human-friendly Name fields when present.
func summariseSample(s map[string]string) string {
	keys := []string{"ImplName", "MethodName", "IfaceName", "MethodSignature"}
	parts := []string{}
	for _, k := range keys {
		if v, ok := s[k]; ok && v != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", strings.TrimSuffix(k, "Name"), v))
		}
	}
	if len(parts) == 0 {
		// Fall back to all keys except _index.
		var keyList []string
		for k := range s {
			if k == "_index" {
				continue
			}
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		for _, k := range keyList {
			parts = append(parts, fmt.Sprintf("%s=%s", k, truncatePropose(s[k], 40)))
		}
	}
	return strings.Join(parts, ", ")
}

func truncatePropose(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
