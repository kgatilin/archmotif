package contracts

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitIdentifier(t *testing.T) {
	cases := []struct {
		in       string
		wantPath string
		wantName string
	}{
		{"pkg/store.UserStore", "pkg/store", "UserStore"},
		{"example.com/foo/bar.Baz", "example.com/foo/bar", "Baz"},
		{"single", "", ""}, // no dot — invalid
		{".Trailing", "", "Trailing"},
		{"path/", "", ""},     // trailing slash, no dot
		{"a.b.c", "a.b", "c"}, // last dot wins
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gotP, gotN := SplitIdentifier(c.in)
			// `.Trailing` → SplitIdentifier returns ("", "Trailing") which
			// our spec rejects as invalid via the empty-pkgPath guard;
			// match that.
			if c.wantPath == "" && c.wantName == "" {
				if gotP != "" || gotN != "" {
					t.Fatalf("expected invalid; got (%q, %q)", gotP, gotN)
				}
				return
			}
			if c.wantPath == "" && c.wantName != "" {
				if gotP != "" {
					t.Fatalf("expected empty path; got %q", gotP)
				}
				return
			}
			if gotP != c.wantPath || gotN != c.wantName {
				t.Fatalf("got (%q, %q), want (%q, %q)", gotP, gotN, c.wantPath, c.wantName)
			}
		})
	}
}

func TestReadConfig_OK(t *testing.T) {
	src := `contracts:
  - interface: pkg/store.UserStore
  - type: pkg/api.Request
graph:
  exclude:
    qnames:
      - fmt.Errorf
    qname_prefixes:
      - strings.
    packages:
      - testing
    kinds:
      - branch
`
	cfg, err := readConfig(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contracts) != 2 {
		t.Fatalf("want 2 entries, got %d", len(cfg.Contracts))
	}
	if cfg.Contracts[0].Kind() != EntryInterface {
		t.Fatalf("entry 0 kind = %q, want interface", cfg.Contracts[0].Kind())
	}
	if cfg.Contracts[1].Identifier() != "pkg/api.Request" {
		t.Fatalf("entry 1 ident = %q", cfg.Contracts[1].Identifier())
	}
	if got := cfg.Graph.Exclude.QNames; len(got) != 1 || got[0] != "fmt.Errorf" {
		t.Fatalf("exclude qnames = %v", got)
	}
	if got := cfg.Graph.Exclude.QNamePrefixes; len(got) != 1 || got[0] != "strings." {
		t.Fatalf("exclude qname_prefixes = %v", got)
	}
	if got := cfg.Graph.Exclude.Packages; len(got) != 1 || got[0] != "testing" {
		t.Fatalf("exclude packages = %v", got)
	}
	if got := cfg.Graph.Exclude.Kinds; len(got) != 1 || got[0] != "branch" {
		t.Fatalf("exclude kinds = %v", got)
	}
}

func TestReadConfig_RejectsEmptyExclude(t *testing.T) {
	src := `graph:
  exclude:
    qnames:
      - ""
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error for empty exclude qname")
	}
}

func TestReadConfig_RejectsBothFields(t *testing.T) {
	src := `contracts:
  - interface: a.B
    type: c.D
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error when entry has both interface: and type:")
	}
}

func TestReadConfig_RejectsBareName(t *testing.T) {
	src := `contracts:
  - interface: NoDot
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error when identifier has no dot")
	}
}

func TestReadConfig_RejectsUnknownField(t *testing.T) {
	src := `contracts:
  - interface: a.B
unrelated: true
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error for unknown top-level field (KnownFields strict)")
	}
}

func TestLoadConfig_MissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(cfg.Contracts) != 0 {
		t.Fatalf("missing file should yield empty config; got %d entries", len(cfg.Contracts))
	}
}

func TestLoadConfig_FixtureRoundTrip(t *testing.T) {
	dir, err := filepath.Abs("testdata/userstore")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contracts) != 3 {
		t.Fatalf("fixture declares 3 contracts; got %d", len(cfg.Contracts))
	}
}
