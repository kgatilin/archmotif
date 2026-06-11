package llm_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/kgatilin/archmotif/internal/llm"
)

// TestV1Template_RendersOnFixture proves the v1 prompt template parses
// and executes against a hand-built Proposal without panicking. It is
// the minimum bar ADR-017 commits us to: the template is part of the
// shipped contract, so a syntax error in v1.tmpl breaks the build.
func TestV1Template_RendersOnFixture(t *testing.T) {
	path := filepath.Join("prompts", "v1.tmpl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	tmpl, err := template.New("v1").Parse(string(raw))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	p := llm.Proposal{
		ID:          "motif-001",
		Description: "extract interface from repeated method shape",
		SkeletonGo: []byte(`type <Iface> interface {
    <Method>(ctx context.Context, id string) (<Result>, error)
}
`),
		SkeletonYAML: []byte("kind: interface\nname: <Iface>\n"),
		Samples: []map[string]string{
			{"Iface": "UserStore", "Method": "GetUser", "Result": "*User"},
			{"Iface": "OrderStore", "Method": "GetOrder", "Result": "*Order"},
		},
		AffectedFiles: map[string][]byte{
			"internal/store/user.go":  []byte("package store\n\ntype UserStore struct{}\n"),
			"internal/store/order.go": []byte("package store\n\ntype OrderStore struct{}\n"),
		},
		Model: llm.ModelSonnet,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("rendered template is empty")
	}

	// Sanity-check the structural anchors the prompt commits to. These
	// are the lines downstream tooling (Stage 7 parser, prompt-diff
	// review) keys off; if a future prompt edit silently drops one of
	// them, this test fails before the change ships.
	wantSubstrings := []string{
		"You are refactoring Go code",
		"PROPOSAL: extract interface from repeated method shape",
		"TARGET SHAPE (annotated Go):",
		"EXISTING SAMPLE INSTANCES:",
		"ORIGINAL CODE REGIONS:",
		"--- internal/store/user.go ---",
		"--- internal/store/order.go ---",
		"OUTPUT FORMAT",
		"```diff",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("rendered prompt missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestV1Template_EmptyProposal_RendersWithoutPanic guards the
// degenerate path: zero-value Proposal must not panic. The template is
// never given a nil Proposal in production, but a regression here
// almost always means the template introduced a non-nil-safe field
// access (e.g. `{{.Foo.Bar}}` instead of `{{with .Foo}}{{.Bar}}{{end}}`).
func TestV1Template_EmptyProposal_RendersWithoutPanic(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("prompts", "v1.tmpl"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	tmpl, err := template.New("v1").Parse(string(raw))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, llm.Proposal{}); err != nil {
		t.Fatalf("execute on empty proposal: %v", err)
	}
}
