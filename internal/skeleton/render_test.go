package skeleton_test

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/skeleton"
)

// motif001Proposal returns the canonical extract-interface Proposal
// matching the motif-001 worked-example fixture (ADR-016). The
// Stage-5 proposer for extract-interface produces this shape; the
// renderer is a pure function of the Proposal.
func motif001Proposal() *propose.Proposal {
	return &propose.Proposal{
		ID:          "motif-001",
		Description: "extract interface from repeated motif (size 3)",
		AffectedFiles: []string{
			"pkg/store/user.go",
			"pkg/store/order.go",
			"pkg/store/product.go",
		},
		TargetSubgraph: propose.TargetSubgraph{
			Roles: []propose.Role{
				{
					Name:        "Iface",
					Kind:        mgraph.NodeType,
					Cardinality: 1,
					Attrs:       map[string]any{mgraph.AttrContractKind: "interface"},
				},
				{Name: "Impl", Kind: mgraph.NodeType, Cardinality: 3},
				{Name: "Method", Kind: mgraph.NodeMethod, Cardinality: 3},
			},
			Edges: []propose.EdgeConstraint{
				{From: "Impl", To: "Iface", Kind: mgraph.EdgeImplements},
				{From: "Method", To: "Impl", Kind: mgraph.EdgeContains},
			},
		},
		Samples: []map[string]string{
			{"IfaceName": "UserStore", "ImplName": "SQLUserStore", "MethodName": "Find"},
			{"IfaceName": "OrderStore", "ImplName": "SQLOrderStore", "MethodName": "Find"},
			{"IfaceName": "ProductStore", "ImplName": "PgProductStore", "MethodName": "Lookup"},
		},
	}
}

func TestRenderGoMatchesFixture(t *testing.T) {
	p := motif001Proposal()
	got, err := skeleton.RenderGo(p)
	if err != nil {
		t.Fatalf("RenderGo: %v", err)
	}
	want := mustReadFixture(t, "motif-001.skeleton.go")
	if normalize(string(got)) != normalize(string(want)) {
		t.Fatalf("RenderGo mismatch with fixture\n--- got\n%s\n--- want\n%s\n", got, want)
	}
}

func TestRenderYAMLMatchesFixture(t *testing.T) {
	p := motif001Proposal()
	got, err := skeleton.RenderYAML(p)
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}
	want := mustReadFixture(t, "motif-001.skeleton.yaml")
	if normalize(string(got)) != normalize(string(want)) {
		t.Fatalf("RenderYAML mismatch with fixture\n--- got\n%s\n--- want\n%s\n", got, want)
	}
}

func TestRenderGoParsesWithGoParser(t *testing.T) {
	p := motif001Proposal()
	got, err := skeleton.RenderGo(p)
	if err != nil {
		t.Fatalf("RenderGo: %v", err)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "rendered.go", got, parser.ParseComments); err != nil {
		t.Fatalf("rendered Go does not parse: %v\n%s", err, got)
	}
}

// TestRenderYAMLRoundTrip verifies the rendered YAML decodes back to
// the same target_subgraph shape — the verifier (Stage 8) reads the
// YAML, so the round-trip property is the format's load-bearing
// promise (per ADR-016 §"YAML companion grammar").
func TestRenderYAMLRoundTrip(t *testing.T) {
	p := motif001Proposal()
	out, err := skeleton.RenderYAML(p)
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}
	var doc struct {
		ProposalID     string   `yaml:"proposal_id"`
		Description    string   `yaml:"description"`
		Affected       []string `yaml:"affected"`
		TargetSubgraph struct {
			Roles []struct {
				ID           string `yaml:"id"`
				Kind         string `yaml:"kind"`
				ReceiverRole string `yaml:"receiver_role"`
				Realises     *struct {
					Role   string `yaml:"role"`
					Method string `yaml:"method"`
				} `yaml:"realises"`
				Methods []struct {
					NameRole   string `yaml:"name_role"`
					ReturnRole string `yaml:"return_role"`
					Params     []struct {
						Role     string `yaml:"role"`
						TypeRole string `yaml:"type_role"`
					} `yaml:"params"`
				} `yaml:"methods"`
			} `yaml:"roles"`
			Edges []struct {
				From string `yaml:"from"`
				To   string `yaml:"to"`
				Kind string `yaml:"kind"`
			} `yaml:"edges"`
		} `yaml:"target_subgraph"`
		Samples []map[string]string `yaml:"samples"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("Unmarshal rendered YAML: %v\n%s", err, out)
	}
	if doc.ProposalID != p.ID {
		t.Errorf("proposal_id = %q, want %q", doc.ProposalID, p.ID)
	}
	if doc.Description != p.Description {
		t.Errorf("description mismatch: got %q want %q", doc.Description, p.Description)
	}
	if len(doc.Affected) != len(p.AffectedFiles) {
		t.Errorf("affected count = %d, want %d", len(doc.Affected), len(p.AffectedFiles))
	}
	if got, want := len(doc.TargetSubgraph.Roles), len(p.TargetSubgraph.Roles); got != want {
		t.Errorf("roles count = %d, want %d", got, want)
	}
	if got, want := len(doc.TargetSubgraph.Edges), len(p.TargetSubgraph.Edges); got != want {
		t.Errorf("edges count = %d, want %d", got, want)
	}
	if got, want := len(doc.Samples), len(p.Samples); got != want {
		t.Errorf("samples count = %d, want %d", got, want)
	}
	// Spot-check edge kinds.
	for i, e := range doc.TargetSubgraph.Edges {
		want := string(p.TargetSubgraph.Edges[i].Kind)
		if e.Kind != want {
			t.Errorf("edges[%d].kind = %q, want %q", i, e.Kind, want)
		}
	}
}

func TestRenderRejectsNilAndInvalid(t *testing.T) {
	if _, err := skeleton.RenderGo(nil); err == nil {
		t.Error("RenderGo(nil) returned nil error")
	}
	if _, err := skeleton.RenderYAML(nil); err == nil {
		t.Error("RenderYAML(nil) returned nil error")
	}
	// Empty ID.
	if _, err := skeleton.RenderGo(&propose.Proposal{}); err == nil {
		t.Error("RenderGo with empty proposal returned nil error")
	}
	// No samples.
	noSamples := motif001Proposal()
	noSamples.Samples = nil
	if _, err := skeleton.RenderGo(noSamples); err == nil {
		t.Error("RenderGo with no samples returned nil error")
	}
}

// TestRenderGoSamplePadding checks the padding fallback when the
// proposer emits fewer than MinSamples — the renderer pads by
// repeating the last sample so the SAMPLES block always has 3..5
// rows (per ADR-023 §"sample padding"). Truncates above MaxSamples.
func TestRenderGoSamplePadding(t *testing.T) {
	p := motif001Proposal()
	p.Samples = p.Samples[:1] // single sample
	got, err := skeleton.RenderGo(p)
	if err != nil {
		t.Fatalf("RenderGo: %v", err)
	}
	if got, want := strings.Count(string(got), "Iface=UserStore"), 3; got < want {
		t.Errorf("padded sample count = %d, want >= %d (renderer must pad)", got, want)
	}
}

func TestRenderGoSampleTruncation(t *testing.T) {
	p := motif001Proposal()
	for i := 0; i < 5; i++ {
		p.Samples = append(p.Samples, map[string]string{
			"IfaceName": "ExtraStore", "ImplName": "ExtraImpl", "MethodName": "Extra",
		})
	}
	got, err := skeleton.RenderGo(p)
	if err != nil {
		t.Fatalf("RenderGo: %v", err)
	}
	// Count non-comment SAMPLES rows.
	sampleLines := 0
	for _, line := range strings.Split(string(got), "\n") {
		if strings.HasPrefix(line, "//   Iface=") {
			sampleLines++
		}
	}
	if sampleLines != skeleton.MaxSamples {
		t.Errorf("sample line count = %d, want %d (truncation cap)", sampleLines, skeleton.MaxSamples)
	}
}

// normalize trims trailing whitespace per line and a trailing newline
// so the byte-for-byte comparison ignores incidental differences
// (per the deliverable: "byte-for-byte modulo trailing whitespace").
func normalize(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	return strings.TrimRight(out, "\n") + "\n"
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := fixturePath(t, name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}

// fixturePath is defined in format_test.go (same _test package).
