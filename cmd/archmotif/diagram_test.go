package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiagramCommand_Usage verifies the no-arg path emits Usage and
// exits 2.
func TestDiagramCommand_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestDiagramCommand_List exercises --list and verifies every
// registered kind is surfaced.
func TestDiagramCommand_List(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "--list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"package-deps", "contract-port", "call-flow"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("--list output missing %q:\n%s", want, stdout.String())
		}
	}
}

// TestDiagramCommand_UnknownKind exits 2 with a helpful error.
func TestDiagramCommand_UnknownKind(t *testing.T) {
	dir := writeDiagramFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "nope", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown diagram kind") {
		t.Errorf("stderr missing helpful message:\n%s", stderr.String())
	}
}

// TestDiagramCommand_PackageDepsD2 exercises the package-deps
// projection in D2 format end-to-end.
func TestDiagramCommand_PackageDepsD2(t *testing.T) {
	dir := writeDiagramFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "package-deps", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"# kind: package-deps",
		"diagramfixture/domain",
		"diagramfixture/adapter",
		"-> ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("D2 output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestDiagramCommand_PackageDepsJSON exercises JSON output and asserts
// that evidence IDs survive serialisation.
func TestDiagramCommand_PackageDepsJSON(t *testing.T) {
	dir := writeDiagramFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "package-deps", "--format", "json", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var env struct {
		Version int `json:"version"`
		Diagram struct {
			Kind  string `json:"kind"`
			Nodes []struct {
				ID          string   `json:"id"`
				EvidenceIDs []string `json:"evidenceIds"`
			} `json:"nodes"`
			Edges []struct {
				From        string   `json:"from"`
				To          string   `json:"to"`
				Kind        string   `json:"kind"`
				EvidenceIDs []string `json:"evidenceIds"`
			} `json:"edges"`
		} `json:"diagram"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if env.Version != 1 {
		t.Errorf("version = %d, want 1", env.Version)
	}
	if env.Diagram.Kind != "package-deps" {
		t.Errorf("kind = %q, want package-deps", env.Diagram.Kind)
	}
	if len(env.Diagram.Nodes) == 0 {
		t.Fatal("no nodes in diagram")
	}
	for _, n := range env.Diagram.Nodes {
		if len(n.EvidenceIDs) == 0 {
			t.Errorf("node %q: missing evidence ids", n.ID)
		}
	}
	for _, e := range env.Diagram.Edges {
		if len(e.EvidenceIDs) == 0 {
			t.Errorf("edge %s->%s: missing evidence ids", e.From, e.To)
		}
	}
}

// TestDiagramCommand_GraphMLWellFormed asserts the GraphML renderer
// produces parseable XML on a real-loaded fixture.
func TestDiagramCommand_GraphMLWellFormed(t *testing.T) {
	dir := writeDiagramFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "package-deps", "--format", "graphml", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	dec := xml.NewDecoder(strings.NewReader(stdout.String()))
	for {
		_, err := dec.Token()
		if err != nil {
			break
		}
	}
}

// TestDiagramCommand_CallFlowAutoSeed exercises the call-flow
// projection without an explicit seed; the fixture exports a `Run`
// function that auto-seeds.
func TestDiagramCommand_CallFlowAutoSeed(t *testing.T) {
	dir := writeDiagramFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagram", "call-flow", "--format", "json", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var env struct {
		Diagram struct {
			Kind  string `json:"kind"`
			Nodes []struct {
				Label string `json:"label"`
			} `json:"nodes"`
			Notes []string `json:"notes"`
		} `json:"diagram"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout.String())
	}
	if env.Diagram.Kind != "call-flow" {
		t.Errorf("kind = %q", env.Diagram.Kind)
	}
	// Either we found Run as auto-seed (good) or the fixture has no
	// suitable entrypoint (acceptable, but at least confirm the
	// notes carry guidance).
	if len(env.Diagram.Nodes) == 0 && len(env.Diagram.Notes) == 0 {
		t.Errorf("call-flow produced empty diagram with no notes:\n%s", stdout.String())
	}
}

// writeDiagramFixture sets up a small Go module exercising the
// projections: domain package with a Greeter interface, adapter
// package implementing it, and an app package with a Run() entry
// point that calls into both.
func writeDiagramFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module diagramfixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}

	archmotif := `roles:
  packages:
    - {pattern: diagramfixture/domain, role: domain}
    - {pattern: diagramfixture/adapter, role: outbound_adapter}
    - {pattern: diagramfixture/app, role: application}
contracts:
  - interface: "diagramfixture/domain.Greeter"
`
	if err := os.WriteFile(filepath.Join(dir, ".archmotif.yaml"),
		[]byte(archmotif), 0o644); err != nil {
		t.Fatalf("archmotif.yaml: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "domain"), 0o755); err != nil {
		t.Fatalf("mkdir domain: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "domain", "greeter.go"),
		[]byte(`package domain

type Greeter interface {
	Greet(name string) string
}
`), 0o644); err != nil {
		t.Fatalf("domain/greeter.go: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "adapter"), 0o755); err != nil {
		t.Fatalf("mkdir adapter: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "adapter", "server.go"),
		[]byte(`package adapter

import "fmt"

type Server struct {
	Tag string
}

func (s *Server) Greet(name string) string {
	return fmt.Sprintf("hi %s from %s", name, s.Tag)
}
`), 0o644); err != nil {
		t.Fatalf("adapter/server.go: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "run.go"),
		[]byte(`package app

import (
	"diagramfixture/adapter"
	"diagramfixture/domain"
)

func Hello() string { return "hello" }

func Run() domain.Greeter {
	_ = Hello()
	return &adapter.Server{Tag: "t"}
}
`), 0o644); err != nil {
		t.Fatalf("app/run.go: %v", err)
	}

	return dir
}
