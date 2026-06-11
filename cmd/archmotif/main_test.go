package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got == "" {
		t.Fatal("--version produced empty stdout")
	}
}

func TestHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "archmotif") {
		t.Fatalf("expected usage text, got %q", stderr.String())
	}
}

func TestNoArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage text in stderr, got %q", stderr.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"motifs", "."}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not implemented") {
		t.Fatalf("expected 'not implemented' in stderr, got %q", stderr.String())
	}
}

func TestGraphCommandWithoutPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"graph"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestGraphCommandSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// `internal/graph` is a small self-contained package — fast to load
	// and stable enough to assert a non-zero node count.
	code := run([]string{"graph", "--summary", "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nodes=") {
		t.Fatalf("expected nodes= line in stdout, got %q", stdout.String())
	}
}

func TestGraphCommandGraphML(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"graph", "--format", "graphml", "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`<graphml`, `edgedefault="directed"`, `attr.name="kind"`, `<node id="n0">`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in graphml output", want)
		}
	}
}

func TestGraphCommandGraphMLMarksContracts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"graph", "--format", "graphml", "../../internal/contracts/testdata/userstore"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`attr.name="is_contract"`,
		`<data key="n_is_contract">true</data>`,
		`<data key="n_contract_kind">interface</data>`,
		`<data key="n_contract_kind">type</data>`,
		`<data key="n_contract_source">config</data>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in graphml output", want)
		}
	}
}
