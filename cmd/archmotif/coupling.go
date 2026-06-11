package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/contracts"
	"github.com/kgatilin/archmotif/internal/coupling"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/roles"
)

// runCoupling implements the `archmotif coupling <path>` subcommand.
// It builds the typed graph (via contracts.Build for full role +
// contract annotation), applies role metadata declared in
// .archmotif.yaml, computes the role-pair matrix, forbidden-edge
// violations, and named scores per ADR-030, and renders to stdout in
// JSON (default) or Markdown.
//
// Exit codes:
//
//	0 — report rendered (forbidden violations are surfaced, not fatal)
//	1 — pipeline / parse error
//	2 — argument or load error
func runCoupling(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif coupling", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "json", "output format: json|markdown")
	evidenceCap := fs.Int("evidence-cap", 0, "per-pair evidence list cap (0 = use config / default 5)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif coupling [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nReads coupling.forbidden + coupling.evidence_cap from .archmotif.yaml.\nSee ADR-030.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	if *format != "json" && *format != "markdown" {
		_, _ = fmt.Fprintf(stderr, "archmotif coupling: --format=%q (want: json|markdown)\n", *format)
		return 2
	}
	dir := fs.Arg(0)

	res, err := contracts.Build(contracts.BuildOptions{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif coupling: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	for _, m := range res.KindMismatches {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", m)
	}

	// Apply architecture-role metadata. ADR-027 introduced the role
	// infrastructure but didn't wire it into contracts.Build (would
	// have caused an import cycle). Coupling needs the role attrs, so
	// we resolve and apply here at the call site.
	if resolution := roles.Resolve(res.Graph, res.Config.Roles); len(resolution) > 0 {
		_ = roles.Apply(res.Graph, resolution)
	}

	cfg := couplingConfigFromContracts(res.Config.Coupling, *evidenceCap)
	report := coupling.Compute(res.Graph, cfg)

	switch *format {
	case "markdown":
		if err := coupling.RenderMarkdown(stdout, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif coupling: render markdown: %v\n", err)
			return 1
		}
	default:
		if err := coupling.RenderJSON(stdout, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif coupling: render json: %v\n", err)
			return 1
		}
	}
	return 0
}

// couplingConfigFromContracts translates the YAML-parsed
// contracts.Coupling block into the coupling package's runtime
// Config. The CLI flag --evidence-cap (when > 0) overrides the
// .archmotif.yaml setting.
func couplingConfigFromContracts(c contracts.Coupling, evidenceCapOverride int) coupling.Config {
	out := coupling.Config{
		EvidenceCap: c.EvidenceCap,
	}
	if evidenceCapOverride > 0 {
		out.EvidenceCap = evidenceCapOverride
	}
	for _, rule := range c.Forbidden {
		out.Forbidden = append(out.Forbidden, coupling.ForbiddenEdge{
			From:   mgraph.Role(rule.From),
			To:     mgraph.Role(rule.To),
			Reason: rule.Reason,
		})
	}
	return out
}
