package anomalies

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// DetectorError captures the failure of one detector so the runner
// can continue with the rest and the CLI can report a non-zero exit.
type DetectorError struct {
	Detector string
	Err      error
}

// Error returns the error string.
func (e DetectorError) Error() string {
	return fmt.Sprintf("detector %s: %v", e.Detector, e.Err)
}

// Unwrap returns the wrapped error.
func (e DetectorError) Unwrap() error { return e.Err }

// Result aggregates the output of a Run call.
type Result struct {
	Anomalies []Anomaly
	Errors    []DetectorError
	// Ran lists detector names that produced output (errored
	// detectors are not in this list).
	Ran []string
}

// Run executes every selected detector against the metric records
// and returns a combined Result. names selects detectors by Name();
// pass nil/empty to run all registered detectors.
//
// Anomalies are sorted by Score descending, then by (Metric, PrimaryID)
// for deterministic ordering within the same score.
func Run(g *mgraph.Graph, records []metrics.Record, names []string) Result {
	var dets []Detector
	if len(names) == 0 {
		dets = All()
	} else {
		for _, n := range names {
			d, ok := Lookup(n)
			if !ok {
				dets = append(dets, missingDetector{name: n})
				continue
			}
			dets = append(dets, d)
		}
	}

	res := Result{}
	for _, d := range dets {
		// Filter to this detector's metric so each Detect doesn't
		// re-scan unrelated records.
		filtered := records
		if metric := d.Metric(); metric != "" {
			filtered = filtered[:0:0]
			for _, r := range records {
				if r.Metric == metric {
					filtered = append(filtered, r)
				}
			}
		}
		anomalies, err := d.Detect(g, filtered)
		if err != nil {
			res.Errors = append(res.Errors, DetectorError{Detector: d.Name(), Err: err})
			continue
		}
		res.Anomalies = append(res.Anomalies, anomalies...)
		res.Ran = append(res.Ran, d.Name())
	}
	sort.SliceStable(res.Anomalies, func(i, j int) bool {
		if res.Anomalies[i].Score != res.Anomalies[j].Score {
			return res.Anomalies[i].Score > res.Anomalies[j].Score
		}
		if res.Anomalies[i].Metric != res.Anomalies[j].Metric {
			return res.Anomalies[i].Metric < res.Anomalies[j].Metric
		}
		return res.Anomalies[i].Region.PrimaryID < res.Anomalies[j].Region.PrimaryID
	})
	return res
}

// missingDetector is a stub used when a CLI request names a
// detector that isn't registered. It produces a single error so the
// CLI exits non-zero with a clear message.
type missingDetector struct{ name string }

func (m missingDetector) Name() string                 { return m.name }
func (m missingDetector) Metric() string               { return "" }
func (m missingDetector) Description() string          { return "" }
func (m missingDetector) Configurable() map[string]any { return nil }
func (m missingDetector) Detect(_ *mgraph.Graph, _ []metrics.Record) ([]Anomaly, error) {
	return nil, fmt.Errorf("detector %q not registered", m.name)
}

// JSON is the on-disk / stdout serialisation envelope. Versioned so
// downstream consumers (Stage 5 proposals) can detect schema bumps.
type JSON struct {
	Version   int       `json:"version"`
	Anomalies []Anomaly `json:"anomalies"`
	Ran       []string  `json:"ran,omitempty"`
	Errors    []string  `json:"errors,omitempty"`
}

// CurrentJSONVersion is the version emitted by ToJSON. Bump on
// breaking schema changes.
const CurrentJSONVersion = 1

// ToJSON converts a Result into the on-disk envelope.
func (r Result) ToJSON() JSON {
	out := JSON{Version: CurrentJSONVersion, Anomalies: r.Anomalies, Ran: r.Ran}
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

// WriteTable renders a human-readable ranked list. Format:
//
//	#   metric           score   target/primary               reason
//	1   motif_redundancy  10.0   pkg:foo                     instance count above floor
//
// Output is intended for terminal reading; not a stable format.
func (r Result) WriteTable(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "anomalies: %d found across %d detectors\n", len(r.Anomalies), len(r.Ran)); err != nil {
		return err
	}
	if len(r.Anomalies) == 0 {
		_, err := fmt.Fprintln(w, "(no anomalies — all metrics within their threshold bands)")
		return err
	}
	header := fmt.Sprintf("\n%-4s %-18s %-8s %-50s %s\n", "#", "metric", "score", "target/primary", "reason")
	if _, err := fmt.Fprint(w, header); err != nil {
		return err
	}
	for i, a := range r.Anomalies {
		target := a.Region.PrimaryID
		if target == "" {
			target = "(graph)"
		}
		if _, err := fmt.Fprintf(w, "%-4d %-18s %-8.2f %-50s %s\n",
			i+1,
			truncate(a.Metric, 18),
			a.Score,
			truncate(target, 50),
			a.Reason.Message,
		); err != nil {
			return err
		}
	}
	if len(r.Errors) > 0 {
		if _, err := fmt.Fprintln(w, "\nerrors:"); err != nil {
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

// LoadMetricsJSON parses metrics output (from `archmotif metrics`)
// into a Result-ready []metrics.Record. The version field is
// checked; an unknown version returns an error.
//
// Stage 4 in v1 always re-runs metrics in-process (so this loader is
// optional infrastructure). It exists so a user can persist metrics
// output and feed it back later (`archmotif metrics ... > x.json;
// archmotif anomalies --metrics-file x.json`).
func LoadMetricsJSON(r io.Reader) ([]metrics.Record, error) {
	var env metrics.JSON
	dec := json.NewDecoder(r)
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("decode metrics json: %w", err)
	}
	if env.Version != metrics.CurrentJSONVersion {
		return nil, fmt.Errorf("metrics json version %d (want %d)", env.Version, metrics.CurrentJSONVersion)
	}
	return env.Records, nil
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

// JoinNames returns a comma-separated string of detector names.
// Primarily used by the CLI's --help output.
func JoinNames() string {
	return strings.Join(Names(), ", ")
}
