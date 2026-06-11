package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// MetricError captures the failure of one metric so the runner can keep
// going and the CLI can report a non-zero exit when any metric failed.
type MetricError struct {
	Metric string
	Err    error
}

// Error returns the error string for log output.
func (e MetricError) Error() string {
	return fmt.Sprintf("metric %s: %v", e.Metric, e.Err)
}

// Unwrap returns the wrapped error.
func (e MetricError) Unwrap() error { return e.Err }

// Result aggregates the output of a Run call.
type Result struct {
	Records []Record
	Errors  []MetricError
	// Ran lists metric names that produced records (errored metrics
	// are not in this list, even if they emitted partial output).
	Ran []string
}

// Run executes every selected metric against g and returns a combined
// Result. names selects metrics by Name(); pass nil/empty to run all
// registered metrics. Records are sorted by (metric, scope, target) so
// the output is stable across runs.
//
// Run uses a background context; long-running callers that need
// cancellation should use RunContext.
func Run(g *mgraph.Graph, names []string) Result {
	return RunContext(context.Background(), g, names)
}

// RunContext is Run with an explicit cancellation context. Each metric
// receives ctx via Compute; if ctx is cancelled mid-run the remaining
// metrics are skipped and the cancellation is recorded as an error
// against the current metric.
func RunContext(ctx context.Context, g *mgraph.Graph, names []string) Result {
	var metrics []Metric
	if len(names) == 0 {
		metrics = All()
	} else {
		for _, n := range names {
			m, ok := Lookup(n)
			if !ok {
				metrics = append(metrics, missingMetric{name: n})
				continue
			}
			metrics = append(metrics, m)
		}
	}

	res := Result{}
	for _, m := range metrics {
		if err := ctx.Err(); err != nil {
			res.Errors = append(res.Errors, MetricError{Metric: m.Name(), Err: err})
			continue
		}
		recs, err := m.Compute(ctx, g)
		if err != nil {
			res.Errors = append(res.Errors, MetricError{Metric: m.Name(), Err: err})
			continue
		}
		res.Records = append(res.Records, recs...)
		res.Ran = append(res.Ran, m.Name())
	}
	sort.SliceStable(res.Records, func(i, j int) bool {
		if res.Records[i].Metric != res.Records[j].Metric {
			return res.Records[i].Metric < res.Records[j].Metric
		}
		if res.Records[i].Scope != res.Records[j].Scope {
			return res.Records[i].Scope < res.Records[j].Scope
		}
		return res.Records[i].Target < res.Records[j].Target
	})
	return res
}

// missingMetric is a stub used when a CLI request names a metric that
// isn't registered. It produces a single error in the runner output
// so the CLI exits non-zero with a clear message.
type missingMetric struct{ name string }

func (m missingMetric) Name() string                 { return m.name }
func (m missingMetric) Description() string          { return "" }
func (m missingMetric) Configurable() map[string]any { return nil }
func (m missingMetric) Compute(_ context.Context, _ *mgraph.Graph) ([]Record, error) {
	return nil, fmt.Errorf("metric %q not registered", m.name)
}

// JSON is the on-disk / stdout serialisation envelope. Versioned so
// downstream consumers can detect schema bumps.
type JSON struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
	Ran     []string `json:"ran,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// CurrentJSONVersion is the version emitted by ToJSON. Bump on
// breaking schema changes.
const CurrentJSONVersion = 1

// ToJSON converts a Result into the on-disk envelope.
func (r Result) ToJSON() JSON {
	out := JSON{Version: CurrentJSONVersion, Records: r.Records, Ran: r.Ran}
	for _, e := range r.Errors {
		out.Errors = append(out.Errors, e.Error())
	}
	return out
}

// WriteJSON encodes the result with two-space indentation.
func (r Result) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.ToJSON())
}

// PrettyPrint renders a human-readable summary grouped by metric.
// Output is intended for terminal reading; not a stable format.
func (r Result) PrettyPrint(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "metrics: %d records across %d metrics\n", len(r.Records), len(r.Ran)); err != nil {
		return err
	}
	byMetric := make(map[string][]Record)
	for _, rec := range r.Records {
		byMetric[rec.Metric] = append(byMetric[rec.Metric], rec)
	}
	names := make([]string, 0, len(byMetric))
	for n := range byMetric {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		recs := byMetric[n]
		if _, err := fmt.Fprintf(w, "\n[%s] %d records\n", n, len(recs)); err != nil {
			return err
		}
		for _, rec := range recs {
			target := rec.Target
			if target == "" {
				target = "(graph)"
			}
			if _, err := fmt.Fprintf(w, "  %-8s %-60s %g\n", rec.Scope, truncate(target, 60), rec.Value); err != nil {
				return err
			}
		}
	}
	if len(r.Errors) > 0 {
		if _, err := fmt.Fprintf(w, "\nerrors:\n"); err != nil {
			return err
		}
		for _, e := range r.Errors {
			if _, err := fmt.Fprintf(w, "  - %s\n", e.Error()); err != nil {
				return err
			}
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
