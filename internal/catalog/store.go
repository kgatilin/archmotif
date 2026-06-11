package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// DefaultPath is the catalog file location relative to the user's
// project root. `.archmotif/` is gitignored by archmotif's own
// repository; downstream users may choose to commit it.
const DefaultPath = ".archmotif/catalog.yaml"

// Load reads a catalog from disk. A missing file is treated as an
// empty catalog (so the first `archmotif catalog` invocation succeeds
// without a manual mkdir / touch). All other I/O errors are returned.
//
// An unknown CatalogVersion is rejected explicitly rather than
// silently re-interpreted: catalog schemas evolve, and a bad parse on
// a forward-incompatible file would corrupt the user's snapshots.
func Load(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Catalog{Version: CatalogVersion}, nil
		}
		return Catalog{}, fmt.Errorf("catalog.Load: %w", err)
	}
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("catalog.Load: parse %s: %w", path, err)
	}
	if c.Version == 0 {
		// Empty file — treat as a fresh catalog.
		c.Version = CatalogVersion
	}
	if c.Version != CatalogVersion {
		return Catalog{}, fmt.Errorf("catalog.Load: unsupported catalog version %d in %s (this build expects %d)", c.Version, path, CatalogVersion)
	}
	return c, nil
}

// Save writes a catalog to disk, creating the parent directory as
// needed. Snapshots are sorted by label for stable on-disk ordering
// regardless of insertion order — this keeps committed catalog files
// diff-friendly when a user does choose to track them in git.
func Save(path string, c Catalog) error {
	if c.Version == 0 {
		c.Version = CatalogVersion
	}
	if c.Version != CatalogVersion {
		return fmt.Errorf("catalog.Save: refusing to write catalog with version %d (build expects %d)", c.Version, CatalogVersion)
	}
	cp := Catalog{Version: c.Version, Snapshots: append([]Snapshot(nil), c.Snapshots...)}
	sort.SliceStable(cp.Snapshots, func(i, j int) bool {
		return cp.Snapshots[i].Label < cp.Snapshots[j].Label
	})
	data, err := yaml.Marshal(cp)
	if err != nil {
		return fmt.Errorf("catalog.Save: marshal: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("catalog.Save: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("catalog.Save: write %s: %w", path, err)
	}
	return nil
}

// Upsert replaces the snapshot whose label matches s.Label, or
// appends s when no such snapshot exists.
func (c *Catalog) Upsert(s Snapshot) {
	for i, existing := range c.Snapshots {
		if existing.Label == s.Label {
			c.Snapshots[i] = s
			return
		}
	}
	c.Snapshots = append(c.Snapshots, s)
}

// Find returns the snapshot with the given label and a boolean
// indicating presence.
func (c Catalog) Find(label string) (Snapshot, bool) {
	for _, s := range c.Snapshots {
		if s.Label == label {
			return s, true
		}
	}
	return Snapshot{}, false
}

// Labels returns the labels of all snapshots in insertion order.
func (c Catalog) Labels() []string {
	out := make([]string, 0, len(c.Snapshots))
	for _, s := range c.Snapshots {
		out = append(out, s.Label)
	}
	return out
}
