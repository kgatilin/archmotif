package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCouplingCommand_NoArg confirms the command surfaces usage on
// stderr and exits 2 when called without a path.
func TestCouplingCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"coupling"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestCouplingCommand_BadFormat confirms unknown --format values are
// rejected with exit 2.
func TestCouplingCommand_BadFormat(t *testing.T) {
	dir := writeCouplingFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"coupling", "--format", "xml", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestCouplingCommand_JSONOutput exercises the end-to-end pipeline:
// parse a tmp Go module, apply role config from a tmp .archmotif.yaml,
// compute the coupling report, and surface it as JSON on stdout.
func TestCouplingCommand_JSONOutput(t *testing.T) {
	dir := writeCouplingFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"coupling", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var envelope struct {
		Version    int `json:"version"`
		PairCounts []struct {
			Pair struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"pair"`
			Count int `json:"count"`
		} `json:"pairCounts"`
		Scores []struct {
			Name string `json:"name"`
		} `json:"scores"`
		EdgesConsidered int `json:"edgesConsidered"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if envelope.Version != 1 {
		t.Errorf("version = %d, want 1", envelope.Version)
	}
	if envelope.EdgesConsidered == 0 {
		t.Errorf("EdgesConsidered = 0; expected at least the package import edge")
	}
	// Both default scores must be present.
	gotNames := map[string]bool{}
	for _, s := range envelope.Scores {
		gotNames[s.Name] = true
	}
	for _, want := range []string{"domain_purity", "adapter_isolation"} {
		if !gotNames[want] {
			t.Errorf("missing score %q in output:\n%s", want, stdout.String())
		}
	}
}

// TestCouplingCommand_MarkdownOutput exercises the Markdown renderer
// path through the CLI surface.
func TestCouplingCommand_MarkdownOutput(t *testing.T) {
	dir := writeCouplingFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"coupling", "--format", "markdown", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"# Coupling report",
		"## Scores",
		"## Role-pair matrix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Markdown missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// writeCouplingFixture sets up a tmp Go module with two packages —
// domain and adapter — plus an .archmotif.yaml that assigns roles via
// pattern selectors and forbids one edge. Returns the module root.
func writeCouplingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module couplingfixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}

	// .archmotif.yaml: pattern-roles + a forbidden rule.
	archmotif := `roles:
  packages:
    - {pattern: couplingfixture/domain, role: domain}
    - {pattern: couplingfixture/adapter, role: outbound_adapter}
coupling:
  forbidden:
    - {from: outbound_adapter, to: domain, reason: "test rule (not realistic, just exercising the path)"}
`
	if err := os.WriteFile(filepath.Join(dir, ".archmotif.yaml"),
		[]byte(archmotif), 0o644); err != nil {
		t.Fatalf("archmotif.yaml: %v", err)
	}

	// domain package — no imports.
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

	// adapter package — imports domain (creates a dependsOn edge).
	if err := os.MkdirAll(filepath.Join(dir, "adapter"), 0o755); err != nil {
		t.Fatalf("mkdir adapter: %v", err)
	}
	adapterSrc := `package adapter

import "couplingfixture/domain"

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
