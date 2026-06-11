package patterns

import (
	"encoding/json"
	"fmt"
	"io"
)

// CurrentJSONVersion is the version emitted by RunResult.WriteJSON.
// Bump on breaking schema changes.
const CurrentJSONVersion = 1

// JSONEnvelope is the on-disk / stdout serialisation envelope for a
// pattern run. Versioned so downstream consumers can detect schema
// bumps.
type JSONEnvelope struct {
	Version int      `json:"version"`
	Reports []Report `json:"reports"`
	Counts  struct {
		Match         int `json:"match"`
		NearMatch     int `json:"near_match"`
		Mismatch      int `json:"mismatch"`
		NotApplicable int `json:"not_applicable"`
	} `json:"counts"`
}

// ToJSON converts the run result to a JSON envelope.
func (r RunResult) ToJSON() JSONEnvelope {
	out := JSONEnvelope{Version: CurrentJSONVersion, Reports: r.Reports}
	c := r.StatusCounts()
	out.Counts.Match = c[StatusMatch]
	out.Counts.NearMatch = c[StatusNearMatch]
	out.Counts.Mismatch = c[StatusMismatch]
	out.Counts.NotApplicable = c[StatusNotApplicable]
	return out
}

// WriteJSON encodes the result with two-space indentation.
func (r RunResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.ToJSON())
}

// WriteText renders a human-readable summary. Output is intended for
// terminal reading; the format is not stable.
func (r RunResult) WriteText(w io.Writer) error {
	c := r.StatusCounts()
	if _, err := fmt.Fprintf(w,
		"patterns: %d reports — match=%d near_match=%d mismatch=%d not_applicable=%d\n",
		len(r.Reports),
		c[StatusMatch], c[StatusNearMatch], c[StatusMismatch], c[StatusNotApplicable],
	); err != nil {
		return err
	}
	for _, rep := range r.Reports {
		if _, err := fmt.Fprintf(w, "\n[%s] %s — %s (v%s)\n",
			rep.Status, rep.ID, rep.Reason, rep.Version,
		); err != nil {
			return err
		}
		if rep.Threshold != 0 || rep.Score != 0 {
			if _, err := fmt.Fprintf(w, "  score=%.3f threshold=%.3f\n", rep.Score, rep.Threshold); err != nil {
				return err
			}
		}
		if len(rep.EvidenceNodes) > 0 {
			if _, err := fmt.Fprintf(w, "  evidence (%d nodes):\n", len(rep.EvidenceNodes)); err != nil {
				return err
			}
			for _, en := range rep.EvidenceNodes {
				if _, err := fmt.Fprintf(w, "    - %s [%s] — %s\n", en.Name, en.Kind, en.Reason); err != nil {
					return err
				}
			}
		}
		if len(rep.Violations) > 0 {
			if _, err := fmt.Fprintf(w, "  violations:\n"); err != nil {
				return err
			}
			for _, v := range rep.Violations {
				if _, err := fmt.Fprintf(w, "    - [%s] %s\n", v.Code, v.Message); err != nil {
					return err
				}
			}
		}
		if len(rep.Recommendations) > 0 {
			if _, err := fmt.Fprintf(w, "  recommendations:\n"); err != nil {
				return err
			}
			for _, rec := range rep.Recommendations {
				if _, err := fmt.Fprintf(w, "    - %s\n", rec); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
