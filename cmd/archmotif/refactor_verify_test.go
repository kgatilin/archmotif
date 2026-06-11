package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
)

// TestVerifyAfterRefactor_Matches exercises ADR-028 §issue-#9 "matching
// code → passes": render the motif-001 proposal's skeleton, build a
// graph from a hand-rolled module that realises the target shape, run
// the verifier — expect Match=true and a PASS verdict on stdout.
func TestVerifyAfterRefactor_Matches(t *testing.T) {
	dir := t.TempDir()
	writeGoModule(t, dir, "matchmod", `package matchmod

type UserStore interface {
	Find(id string) string
}

type SQLUserStore struct {
	dsn string
}

func (s *SQLUserStore) Find(id string) string {
	return s.dsn + ":" + id
}
`)

	prop := motif001Proposal()
	var stdout, stderr bytes.Buffer
	matched, err := verifyAfterRefactor(dir, prop, "./...", false, "text", &stdout, &stderr)
	if err != nil {
		t.Fatalf("verifyAfterRefactor: %v\nstderr=%s", err, stderr.String())
	}
	if !matched {
		t.Fatalf("matched=false; stdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Match on proposal motif-001") {
		t.Errorf("stdout missing Match marker:\n%s", out)
	}
	for _, role := range []string{"<Iface> = UserStore", "<Impl> = SQLUserStore", "<Method> = Find"} {
		if !strings.Contains(out, role) {
			t.Errorf("stdout missing role binding %q:\n%s", role, out)
		}
	}
}

// TestVerifyAfterRefactor_MismatchedShape exercises issue #9 "mismatched
// shape → fails with diagnostic": the module declares the interface
// and the struct but no method realising the interface contract — so
// the implements edge is missing.
func TestVerifyAfterRefactor_MismatchedShape(t *testing.T) {
	dir := t.TempDir()
	writeGoModule(t, dir, "shapemod", `package shapemod

type UserStore interface {
	Find(id string) string
}

// SQLUserStore declared but does NOT realise UserStore — no Find
// method on the receiver. Verifier should report missing role
// (no method candidate) or a failing implements edge.
type SQLUserStore struct {
	dsn string
}
`)

	prop := motif001Proposal()
	var stdout, stderr bytes.Buffer
	matched, err := verifyAfterRefactor(dir, prop, "./...", false, "text", &stdout, &stderr)
	if err != nil {
		t.Fatalf("verifyAfterRefactor: %v\nstderr=%s", err, stderr.String())
	}
	if matched {
		t.Fatalf("expected mismatch; got Match=true\nstdout=%s", stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Mismatch") {
		t.Errorf("stdout missing Mismatch marker:\n%s", out)
	}
}

// TestVerifyAfterRefactor_ContractRenamed exercises issue #9 "matching
// shape but contract method renamed → fails": the module has the
// interface, the struct, AND a method on the struct, but the method
// is named Lookup while the interface declares Find. The struct does
// not satisfy the interface, so go/types does not emit an Implements
// edge — verifier should fail with a missing implements edge.
func TestVerifyAfterRefactor_ContractRenamed(t *testing.T) {
	dir := t.TempDir()
	writeGoModule(t, dir, "renamedmod", `package renamedmod

type UserStore interface {
	Find(id string) string
}

type SQLUserStore struct {
	dsn string
}

// Lookup, not Find — contract method renamed. Method node exists on
// SQLUserStore (NodeMethod), so the role candidate set is non-empty,
// but no Implements edge is emitted because go/types reports the
// interface unsatisfied.
func (s *SQLUserStore) Lookup(id string) string {
	return s.dsn + ":" + id
}
`)

	prop := motif001Proposal()
	var stdout, stderr bytes.Buffer
	matched, err := verifyAfterRefactor(dir, prop, "./...", false, "text", &stdout, &stderr)
	if err != nil {
		t.Fatalf("verifyAfterRefactor: %v\nstderr=%s", err, stderr.String())
	}
	if matched {
		t.Fatalf("expected mismatch; got Match=true\nstdout=%s", stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Mismatch") {
		t.Errorf("stdout missing Mismatch marker:\n%s", out)
	}
	// The diagnostic must point at the implements-edge gap so the
	// operator (or Stage 9) can feed the reason back to the LLM.
	if !strings.Contains(out, "implements") {
		t.Errorf("stdout missing 'implements' edge diagnostic:\n%s", out)
	}
}

// TestVerifyAfterRefactor_JSONFormat covers ADR-028 §4 --verify-format=json:
// the verdict is emitted as the same JSON envelope `archmotif verify`
// produces, so downstream tools have one shape to consume.
func TestVerifyAfterRefactor_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeGoModule(t, dir, "jsonmod", `package jsonmod

type UserStore interface {
	Find(id string) string
}

type SQLUserStore struct{ dsn string }

func (s *SQLUserStore) Find(id string) string { return s.dsn + ":" + id }
`)

	prop := motif001Proposal()
	var stdout, stderr bytes.Buffer
	matched, err := verifyAfterRefactor(dir, prop, "./...", false, "json", &stdout, &stderr)
	if err != nil {
		t.Fatalf("verifyAfterRefactor: %v\nstderr=%s", err, stderr.String())
	}
	if !matched {
		t.Fatalf("matched=false; stdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") || !strings.HasSuffix(out, "}") {
		t.Errorf("stdout is not a JSON object:\n%s", out)
	}
	if !strings.Contains(out, `"match"`) {
		t.Errorf("stdout missing match field:\n%s", out)
	}
}

// motif001Proposal hand-builds a propose.Proposal matching the on-disk
// motif-001 fixture (testdata/verify/motif-001/skeleton.yaml). Kept in
// the test rather than imported from proposetest so the test exercises
// the propose → skeleton → verify pipeline end-to-end without coupling
// to motif-detection internals.
func motif001Proposal() *propose.Proposal {
	return &propose.Proposal{
		ID:          "motif-001",
		Description: "extract interface from repeated motif (test fixture)",
		AffectedFiles: []string{
			"main.go",
		},
		TargetSubgraph: propose.TargetSubgraph{
			Roles: []propose.Role{
				{
					Name:        "Iface",
					Kind:        mgraph.NodeType,
					Cardinality: 1,
					Attrs: map[string]any{
						mgraph.AttrContractKind: "interface",
					},
				},
				{
					Name:        "Impl",
					Kind:        mgraph.NodeType,
					Cardinality: 1,
				},
				{
					Name:        "Method",
					Kind:        mgraph.NodeMethod,
					Cardinality: 1,
				},
			},
			Edges: []propose.EdgeConstraint{
				{From: "Impl", To: "Iface", Kind: mgraph.EdgeImplements},
				{From: "Impl", To: "Method", Kind: mgraph.EdgeContains},
			},
		},
		Samples: []map[string]string{
			{"Iface": "UserStore", "Impl": "SQLUserStore", "Method": "Find"},
			{"Iface": "OrderStore", "Impl": "SQLOrderStore", "Method": "Find"},
			{"Iface": "ProductStore", "Impl": "PgProductStore", "Method": "Lookup"},
		},
	}
}

// writeGoModule writes a minimal go.mod + main.go pair into dir so
// parser.Build can load it as a module. modName is the go.mod module
// path; src is the contents of main.go (must declare a package).
func writeGoModule(t *testing.T, dir, modName, src string) {
	t.Helper()
	mod := "module " + modName + "\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
}
