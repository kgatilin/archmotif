package memopt_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archmotif/internal/memopt"
)

// loadContract reads a fixture contract from testdata into memory. The
// fixtures are checked in so the prompt-rendering and validator tests
// share the exact wire shape the loop CLI (#38) writes to disk.
func loadContract(t *testing.T, name string) *memopt.Contract {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var c memopt.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("fixture %s failed Validate: %v", path, err)
	}
	return &c
}

// TestContract_Validate_RejectsDegenerate enumerates the caller-bug
// shapes Contract.Validate is the safety net for. Each case mutates a
// known-good fixture and checks the validator rejects it.
func TestContract_Validate_RejectsDegenerate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*memopt.Contract)
		wantErr string
	}{
		{
			name:    "empty id",
			mutate:  func(c *memopt.Contract) { c.ID = "" },
			wantErr: "empty ID",
		},
		{
			name:    "empty kind",
			mutate:  func(c *memopt.Contract) { c.Kind = "" },
			wantErr: "empty Kind",
		},
		{
			name:    "no selected",
			mutate:  func(c *memopt.Contract) { c.Selected = nil },
			wantErr: "no Selected nodes",
		},
		{
			name: "duplicate selected id",
			mutate: func(c *memopt.Contract) {
				c.Selected = append(c.Selected, c.Selected[0])
			},
			wantErr: "duplicate ID",
		},
		{
			name:    "no allowed ops",
			mutate:  func(c *memopt.Contract) { c.AllowedOps = nil },
			wantErr: "no AllowedOps",
		},
		{
			name: "unknown op",
			mutate: func(c *memopt.Contract) {
				c.AllowedOps = append(c.AllowedOps, memopt.Operation("delete"))
			},
			wantErr: "unknown operation",
		},
		{
			name: "empty selection id",
			mutate: func(c *memopt.Contract) {
				c.Selected[0].ID = ""
			},
			wantErr: "empty ID",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := loadContract(t, "orphan_batch.json")
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate succeeded; want error containing %q", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestContract_SelectedSet_Lookups confirms the helpers used by
// Validate to answer "is this id allowed?" produce the right closed
// sets.
func TestContract_SelectedSet_Lookups(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	set := c.SelectedSet()
	if len(set) != len(c.Selected) {
		t.Errorf("SelectedSet size = %d, want %d", len(set), len(c.Selected))
	}
	for _, s := range c.Selected {
		if _, ok := set[s.ID]; !ok {
			t.Errorf("SelectedSet missing %q", s.ID)
		}
	}
	if _, ok := set["mem:not-on-list"]; ok {
		t.Errorf("SelectedSet should not contain absent id")
	}

	ops := c.AllowedOpSet()
	if _, ok := ops[memopt.OpRegroup]; !ok {
		t.Errorf("AllowedOpSet missing OpRegroup")
	}
	if _, ok := ops[memopt.OpMerge]; ok {
		t.Errorf("AllowedOpSet should not contain OpMerge for orphan batch")
	}
}

// contains is a small substring helper to keep the test file free of
// strings.Contains imports for one assertion shape.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
