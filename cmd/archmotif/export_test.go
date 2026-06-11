package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportCommand_NoArg confirms the command surfaces usage on
// stderr and exits 2 without a target path.
func TestExportCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"export"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestExportCommand_BadFormat rejects unknown --format values.
func TestExportCommand_BadFormat(t *testing.T) {
	dir := writeExportFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--format", "xml", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestExportCommand_BadEncoding rejects unknown --encoding values.
func TestExportCommand_BadEncoding(t *testing.T) {
	dir := writeExportFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--encoding", "toml", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestExportCommand_JSONOutput exercises the full pipeline: parse a
// tmp Go module, apply role config from a tmp .archmotif.yaml, project
// to the archai model, and emit JSON. Validates that domain packages
// surface their layer, dependencies surface as depends_on, and the
// document is byte-deterministic across two runs.
func TestExportCommand_JSONOutput(t *testing.T) {
	dir := writeExportFixture(t)

	var stdoutA, stderrA bytes.Buffer
	codeA := run([]string{"export", dir}, &stdoutA, &stderrA)
	if codeA != 0 {
		t.Fatalf("exit code A = %d, want 0; stderr=%s", codeA, stderrA.String())
	}

	var stdoutB, stderrB bytes.Buffer
	codeB := run([]string{"export", dir}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("exit code B = %d, want 0", codeB)
	}
	if stdoutA.String() != stdoutB.String() {
		t.Errorf("non-deterministic export: two CLI runs produced different bytes")
	}

	var doc struct {
		Schema struct {
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"schema"`
		Source struct {
			Counts struct {
				Packages     int `json:"packages"`
				Symbols      int `json:"symbols"`
				Dependencies int `json:"dependencies"`
			} `json:"counts"`
		} `json:"source"`
		Packages []struct {
			ID         string   `json:"id"`
			ImportPath string   `json:"importPath"`
			Layer      string   `json:"layer"`
			Stereotype []string `json:"stereotypes"`
		} `json:"packages"`
		Symbols []struct {
			ID    string `json:"id"`
			Kind  string `json:"kind"`
			QName string `json:"qname"`
			Facet string `json:"facet"`
		} `json:"symbols"`
		Dependencies []struct {
			Relation string `json:"relation"`
			Kind     string `json:"kind"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(stdoutA.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdoutA.String())
	}
	if doc.Schema.Name != "archmotif.archai-model" || doc.Schema.Version < 1 {
		t.Errorf("unexpected schema %+v", doc.Schema)
	}
	if doc.Source.Counts.Packages == 0 {
		t.Errorf("counts.packages = 0, want >0")
	}

	// Domain package must have layer=domain (set via .archmotif.yaml).
	hasDomainLayer := false
	for _, p := range doc.Packages {
		if p.ImportPath == "exportfixture/domain" && p.Layer == "domain" {
			hasDomainLayer = true
		}
	}
	if !hasDomainLayer {
		t.Errorf("expected exportfixture/domain to carry layer=domain")
	}

	// At least one depends_on relation should be present
	// (adapter -> domain via the import).
	hasDependsOn := false
	for _, d := range doc.Dependencies {
		if d.Relation == "depends_on" && d.Kind == "dependsOn" {
			hasDependsOn = true
		}
	}
	if !hasDependsOn {
		t.Errorf("expected at least one depends_on dependency in output")
	}
}

// TestExportCommand_YAMLOutput sanity-checks the YAML encoder via the
// CLI: every top-level section is present and the schema header
// appears.
func TestExportCommand_YAMLOutput(t *testing.T) {
	dir := writeExportFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--encoding", "yaml", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, key := range []string{"schema:", "source:", "facets:", "packages:", "symbols:", "dependencies:"} {
		if !strings.Contains(out, key) {
			t.Errorf("yaml missing key %q\n%s", key, out)
		}
	}
}

// writeExportFixture sets up a tmp Go module with two packages —
// domain and adapter — plus an .archmotif.yaml that assigns
// architecture roles. Returns the module root.
func writeExportFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module exportfixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	archmotif := `roles:
  packages:
    - {pattern: exportfixture/domain, role: domain}
    - {pattern: exportfixture/adapter, role: outbound_adapter}
`
	if err := os.WriteFile(filepath.Join(dir, ".archmotif.yaml"),
		[]byte(archmotif), 0o644); err != nil {
		t.Fatalf("archmotif.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "domain"), 0o755); err != nil {
		t.Fatalf("mkdir domain: %v", err)
	}
	domainSrc := `package domain

type User struct {
	ID   string
	Name string
}

func New(id, name string) *User {
	return &User{ID: id, Name: name}
}
`
	if err := os.WriteFile(filepath.Join(dir, "domain", "user.go"),
		[]byte(domainSrc), 0o644); err != nil {
		t.Fatalf("domain/user.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "adapter"), 0o755); err != nil {
		t.Fatalf("mkdir adapter: %v", err)
	}
	adapterSrc := `package adapter

import "exportfixture/domain"

type UserDTO struct {
	ID   string
	Name string
}

func FromDomain(u *domain.User) *UserDTO {
	return &UserDTO{ID: u.ID, Name: u.Name}
}
`
	if err := os.WriteFile(filepath.Join(dir, "adapter", "dto.go"),
		[]byte(adapterSrc), 0o644); err != nil {
		t.Fatalf("adapter/dto.go: %v", err)
	}
	return dir
}
