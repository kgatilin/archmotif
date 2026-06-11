package catalog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpsertReplacesExisting(t *testing.T) {
	c := Catalog{Version: CatalogVersion}
	c.Upsert(Snapshot{Label: "main", Ref: "abc"})
	c.Upsert(Snapshot{Label: "main", Ref: "def"})
	c.Upsert(Snapshot{Label: "feat", Ref: "ghi"})
	if got := len(c.Snapshots); got != 2 {
		t.Fatalf("snapshots: got %d, want 2", got)
	}
	main, ok := c.Find("main")
	if !ok || main.Ref != "def" {
		t.Fatalf("main snapshot ref: got %#v, want ref=def", main)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")

	c := Catalog{Version: CatalogVersion}
	c.Upsert(Snapshot{
		Label:      "main",
		Ref:        "abc1234",
		CapturedAt: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		Path:       ".",
		Pattern:    "./...",
		Metrics: []MetricEntry{
			{Name: "cycle_rank", Value: 7},
			{Name: "modularity", Value: 0.7},
		},
		Motifs: MotifSummary{
			TotalGroups:    2,
			TotalInstances: 5,
			Groups: []MotifGroupEntry{
				{Canonical: "k=3|abc", Size: 3, Count: 3},
				{Canonical: "k=4|def", Size: 4, Count: 2},
			},
		},
		Patterns: []PatternEntry{
			{ID: "domain_core", Version: "1.0.0", Status: "match", Score: 0.9, Threshold: 0.7},
		},
	})

	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != CatalogVersion {
		t.Fatalf("version: got %d, want %d", got.Version, CatalogVersion)
	}
	if len(got.Snapshots) != 1 {
		t.Fatalf("snapshots: got %d, want 1", len(got.Snapshots))
	}
	main := got.Snapshots[0]
	if main.Label != "main" || main.Ref != "abc1234" {
		t.Fatalf("snapshot header: %#v", main)
	}
	if !main.CapturedAt.Equal(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("captured_at: %v", main.CapturedAt)
	}
	if got, want := main.Metrics[0].Name, "cycle_rank"; got != want {
		t.Fatalf("metric[0].Name: got %q, want %q", got, want)
	}
	if got, want := main.Motifs.TotalGroups, 2; got != want {
		t.Fatalf("motifs.TotalGroups: got %d, want %d", got, want)
	}
	if got, want := main.Patterns[0].ID, "domain_core"; got != want {
		t.Fatalf("patterns[0].ID: got %q, want %q", got, want)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got.Version != CatalogVersion {
		t.Fatalf("version: got %d, want %d", got.Version, CatalogVersion)
	}
	if len(got.Snapshots) != 0 {
		t.Fatalf("snapshots: got %d, want 0", len(got.Snapshots))
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.yaml")
	if err := os.WriteFile(path, []byte("version: 999\nsnapshots: []\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported catalog version") {
		t.Fatalf("Load: got %v, want unsupported version error", err)
	}
}

func TestSaveSortsSnapshotsByLabel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	c := Catalog{Version: CatalogVersion}
	c.Upsert(Snapshot{Label: "z"})
	c.Upsert(Snapshot{Label: "a"})
	c.Upsert(Snapshot{Label: "m"})
	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"a", "m", "z"}
	if got, _ := got.Snapshots, want; len(got) != len(want) {
		t.Fatalf("snapshots: %#v", got)
	}
	for i, s := range got.Snapshots {
		if s.Label != want[i] {
			t.Fatalf("snapshots[%d].Label: got %q, want %q", i, s.Label, want[i])
		}
	}
}

func TestDriftEmptyWhenIdentical(t *testing.T) {
	s := Snapshot{
		Label:   "x",
		Metrics: []MetricEntry{{Name: "a", Value: 1}},
	}
	got := Diff(s, s)
	if got.HasChanges() {
		t.Fatalf("identical diff reports changes: %+v", got)
	}
}

func TestDriftMetricChanges(t *testing.T) {
	from := Snapshot{Metrics: []MetricEntry{
		{Name: "cycle_rank", Value: 5},
		{Name: "modularity", Value: 0.8},
		{Name: "removed_metric", Value: 1},
	}}
	to := Snapshot{Metrics: []MetricEntry{
		{Name: "cycle_rank", Value: 7},
		{Name: "modularity", Value: 0.8},
		{Name: "new_metric", Value: 3},
	}}
	d := Diff(from, to)
	if len(d.Metrics) != 3 {
		t.Fatalf("metric deltas: got %d, want 3 (changed + removed + added). %+v", len(d.Metrics), d.Metrics)
	}
	// names sorted
	names := []string{}
	for _, m := range d.Metrics {
		names = append(names, m.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("deltas not sorted by name: %v", names)
		}
	}
	// cycle_rank changed: from 5 to 7, delta 2
	for _, m := range d.Metrics {
		if m.Name == "cycle_rank" {
			if m.From == nil || *m.From != 5 || m.To == nil || *m.To != 7 || m.Delta == nil || *m.Delta != 2 {
				t.Fatalf("cycle_rank delta: %+v", m)
			}
		}
		if m.Name == "removed_metric" {
			if m.From == nil || m.To != nil {
				t.Fatalf("removed_metric should have From only: %+v", m)
			}
		}
		if m.Name == "new_metric" {
			if m.To == nil || m.From != nil {
				t.Fatalf("new_metric should have To only: %+v", m)
			}
		}
	}
}

func TestDriftMotifBuckets(t *testing.T) {
	from := Snapshot{Motifs: MotifSummary{
		TotalGroups: 3, TotalInstances: 8,
		Groups: []MotifGroupEntry{
			{Canonical: "k=3|same", Size: 3, Count: 2},
			{Canonical: "k=3|grew", Size: 3, Count: 2},
			{Canonical: "k=4|gone", Size: 4, Count: 4},
		},
	}}
	to := Snapshot{Motifs: MotifSummary{
		TotalGroups: 3, TotalInstances: 11,
		Groups: []MotifGroupEntry{
			{Canonical: "k=3|same", Size: 3, Count: 2},
			{Canonical: "k=3|grew", Size: 3, Count: 5},
			{Canonical: "k=5|new", Size: 5, Count: 4},
		},
	}}
	d := Diff(from, to)
	if len(d.Motifs.Added) != 1 || d.Motifs.Added[0].Canonical != "k=5|new" {
		t.Fatalf("added: %+v", d.Motifs.Added)
	}
	if len(d.Motifs.Removed) != 1 || d.Motifs.Removed[0].Canonical != "k=4|gone" {
		t.Fatalf("removed: %+v", d.Motifs.Removed)
	}
	if len(d.Motifs.Changed) != 1 || d.Motifs.Changed[0].Canonical != "k=3|grew" ||
		d.Motifs.Changed[0].CountFrom != 2 || d.Motifs.Changed[0].CountTo != 5 {
		t.Fatalf("changed: %+v", d.Motifs.Changed)
	}
}

func TestDriftPatternStatusChange(t *testing.T) {
	from := Snapshot{Patterns: []PatternEntry{
		{ID: "p1", Status: "match", Score: 0.9},
		{ID: "p2", Status: "match", Score: 0.8},
	}}
	to := Snapshot{Patterns: []PatternEntry{
		{ID: "p1", Status: "near_match", Score: 0.7},
		{ID: "p2", Status: "match", Score: 0.8},
	}}
	d := Diff(from, to)
	if len(d.Patterns) != 1 || d.Patterns[0].ID != "p1" {
		t.Fatalf("pattern deltas: %+v", d.Patterns)
	}
	if d.Patterns[0].StatusFrom != "match" || d.Patterns[0].StatusTo != "near_match" {
		t.Fatalf("pattern status: %+v", d.Patterns[0])
	}
}

func TestDriftJSONIsDeterministic(t *testing.T) {
	from := Snapshot{
		Label: "a",
		Metrics: []MetricEntry{
			{Name: "z", Value: 1}, {Name: "a", Value: 2},
		},
		Motifs: MotifSummary{
			Groups: []MotifGroupEntry{{Canonical: "k=3|x", Size: 3, Count: 1}},
		},
	}
	to := Snapshot{
		Label: "b",
		Metrics: []MetricEntry{
			{Name: "z", Value: 3}, {Name: "a", Value: 5},
		},
		Motifs: MotifSummary{
			Groups: []MotifGroupEntry{{Canonical: "k=3|x", Size: 3, Count: 4}},
		},
	}
	var b1, b2 bytes.Buffer
	if err := Diff(from, to).WriteJSON(&b1); err != nil {
		t.Fatalf("WriteJSON #1: %v", err)
	}
	if err := Diff(from, to).WriteJSON(&b2); err != nil {
		t.Fatalf("WriteJSON #2: %v", err)
	}
	if b1.String() != b2.String() {
		t.Fatalf("WriteJSON not deterministic:\n%s\n!=\n%s", b1.String(), b2.String())
	}
	// Sanity: parse the JSON.
	var d Drift
	if err := json.Unmarshal(b1.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Version != CurrentDriftVersion {
		t.Fatalf("version: got %d, want %d", d.Version, CurrentDriftVersion)
	}
}

func TestDriftWriteTextNoChanges(t *testing.T) {
	s := Snapshot{Label: "x"}
	var buf bytes.Buffer
	if err := Diff(s, s).WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(buf.String(), "no changes") {
		t.Fatalf("expected 'no changes', got: %s", buf.String())
	}
}

func TestSnapshotRefFormatsCapturedAt(t *testing.T) {
	s := Snapshot{
		Label:      "main",
		Ref:        "abc",
		CapturedAt: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		Path:       "./...",
	}
	got := snapshotRef(s)
	if got.CapturedAt != "2026-05-06T12:00:00Z" {
		t.Fatalf("captured_at: got %q, want 2026-05-06T12:00:00Z", got.CapturedAt)
	}
	if got.Label != "main" || got.Ref != "abc" || got.Path != "./..." {
		t.Fatalf("snapshotRef: %+v", got)
	}
}
