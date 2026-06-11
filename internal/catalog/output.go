package catalog

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON encodes the drift report with two-space indentation. The
// envelope is versioned (`version: 1`) so downstream tooling can
// detect schema bumps.
func (d Drift) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// WriteText renders a human-readable drift summary. Output is for
// terminal reading; the format is not stable and is not parsed by
// any other archmotif command.
func (d Drift) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "drift: %s%s → %s%s\n",
		d.From.Label, refSuffix(d.From),
		d.To.Label, refSuffix(d.To),
	); err != nil {
		return err
	}
	if !d.HasChanges() {
		_, err := fmt.Fprintln(w, "  no changes")
		return err
	}

	if len(d.Metrics) > 0 {
		if _, err := fmt.Fprintf(w, "\nmetrics:\n"); err != nil {
			return err
		}
		for _, m := range d.Metrics {
			if _, err := fmt.Fprintf(w, "  %-22s %s → %s  (Δ %s)\n",
				m.Name, fmtNullable(m.From), fmtNullable(m.To), fmtNullable(m.Delta),
			); err != nil {
				return err
			}
		}
	}

	mo := d.Motifs
	if mo.TotalGroupsFrom != mo.TotalGroupsTo || mo.TotalInstancesFrom != mo.TotalInstancesTo ||
		len(mo.Added) > 0 || len(mo.Removed) > 0 || len(mo.Changed) > 0 {
		if _, err := fmt.Fprintf(w, "\nmotifs:\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  total_groups     %d → %d  (Δ %+d)\n",
			mo.TotalGroupsFrom, mo.TotalGroupsTo, mo.TotalGroupsTo-mo.TotalGroupsFrom,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  total_instances  %d → %d  (Δ %+d)\n",
			mo.TotalInstancesFrom, mo.TotalInstancesTo, mo.TotalInstancesTo-mo.TotalInstancesFrom,
		); err != nil {
			return err
		}
		for _, g := range mo.Added {
			if _, err := fmt.Fprintf(w, "  + size=%d count=%d  %s\n", g.Size, g.CountTo, g.Canonical); err != nil {
				return err
			}
		}
		for _, g := range mo.Removed {
			if _, err := fmt.Fprintf(w, "  - size=%d count=%d  %s\n", g.Size, g.CountFrom, g.Canonical); err != nil {
				return err
			}
		}
		for _, g := range mo.Changed {
			if _, err := fmt.Fprintf(w, "  ~ size=%d count %d → %d  %s\n", g.Size, g.CountFrom, g.CountTo, g.Canonical); err != nil {
				return err
			}
		}
	}

	if len(d.Patterns) > 0 {
		if _, err := fmt.Fprintf(w, "\npatterns:\n"); err != nil {
			return err
		}
		for _, p := range d.Patterns {
			from := p.StatusFrom
			if from == "" {
				from = "(absent)"
			}
			to := p.StatusTo
			if to == "" {
				to = "(absent)"
			}
			if _, err := fmt.Fprintf(w, "  %-28s %s → %s  (score %s → %s)\n",
				p.ID, from, to,
				fmtNullable(p.ScoreFrom), fmtNullable(p.ScoreTo),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func refSuffix(r SnapshotRef) string {
	if r.Ref == "" {
		return ""
	}
	return "@" + r.Ref
}

func fmtNullable(p *float64) string {
	if p == nil {
		return "—"
	}
	// %g gives a compact rendering: integers print without trailing
	// zeros, fractions print with up to ~6 sig figs.
	return fmt.Sprintf("%g", *p)
}
