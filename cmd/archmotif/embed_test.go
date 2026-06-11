package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEmbeddingProvider struct {
	calls int
}

func (f *fakeEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	f.calls++
	switch text {
	case "reads and writes files on disk":
		return []float32{1, 0, 0}, nil
	case "parses bytes into a syntax tree":
		return []float32{0, 1, 0}, nil
	default:
		return []float32{0, 0, 1}, nil
	}
}

func TestEmbedCommandAddsVectors(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "text.graphml")
	if err := os.WriteFile(graphPath, []byte(textGraphMLFixture), 0o644); err != nil {
		t.Fatalf("write graph fixture: %v", err)
	}

	fake := &fakeEmbeddingProvider{}
	oldProvider := newEmbeddingProvider
	newEmbeddingProvider = func(context.Context, vertexEmbedConfig) (embeddingProvider, error) {
		return fake, nil
	}
	t.Cleanup(func() { newEmbeddingProvider = oldProvider })

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"embed", graphPath,
		"--text-key", "doc",
		"--project", "test-project",
		"--cache-dir", filepath.Join(dir, "cache"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`attr.name="vec"`,
		`<data key="vec">1 0 0</data>`,
		`<data key="vec">0 1 0</data>`,
		`<data key="weight">1.0</data>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("embedded graph missing %q:\n%s", want, out)
		}
	}
	if fake.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", fake.calls)
	}
	if !strings.Contains(stderr.String(), "embedded 2 nodes") {
		t.Fatalf("expected summary on stderr, got %q", stderr.String())
	}
}

func TestEmbedGraphUsesContentCache(t *testing.T) {
	graphPath := filepath.Join(t.TempDir(), "text.graphml")
	if err := os.WriteFile(graphPath, []byte(textGraphMLFixture), 0o644); err != nil {
		t.Fatalf("write graph fixture: %v", err)
	}
	g, code := loadGraphML(graphPath, ioDiscard{})
	if g == nil || code != 0 {
		t.Fatalf("loadGraphML code=%d graph=%v", code, g)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")
	fake := &fakeEmbeddingProvider{}
	opts := embedGraphOptions{TextKey: "doc", VecKey: "vec", CacheDir: cacheDir, CacheNamespace: "fake"}
	first, err := embedGraph(context.Background(), g, fake, opts)
	if err != nil {
		t.Fatalf("first embedGraph: %v", err)
	}
	if first.Embedded != 2 || first.Cached != 0 {
		t.Fatalf("first result = %+v, want embedded=2 cached=0", first)
	}

	g2, code := loadGraphML(graphPath, ioDiscard{})
	if g2 == nil || code != 0 {
		t.Fatalf("loadGraphML code=%d graph=%v", code, g2)
	}
	second, err := embedGraph(context.Background(), g2, fake, opts)
	if err != nil {
		t.Fatalf("second embedGraph: %v", err)
	}
	if second.Embedded != 0 || second.Cached != 2 {
		t.Fatalf("second result = %+v, want embedded=0 cached=2", second)
	}
	if fake.calls != 2 {
		t.Fatalf("provider calls after cache hit = %d, want 2", fake.calls)
	}
}

func TestEmbedCommandRequiresTextNodes(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "empty.graphml")
	if err := os.WriteFile(graphPath, []byte(strings.ReplaceAll(textGraphMLFixture, "doc", "other")), 0o644); err != nil {
		t.Fatalf("write graph fixture: %v", err)
	}

	oldProvider := newEmbeddingProvider
	newEmbeddingProvider = func(context.Context, vertexEmbedConfig) (embeddingProvider, error) {
		return &fakeEmbeddingProvider{}, nil
	}
	t.Cleanup(func() { newEmbeddingProvider = oldProvider })

	var stdout, stderr bytes.Buffer
	code := run([]string{"embed", graphPath, "--text-key", "doc", "--project", "test-project"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), `no nodes carry non-empty text attribute "doc"`) {
		t.Fatalf("expected missing text-key error, got %q", stderr.String())
	}
}

func TestEmbedCommandReportsProviderConfigError(t *testing.T) {
	oldProvider := newEmbeddingProvider
	newEmbeddingProvider = func(context.Context, vertexEmbedConfig) (embeddingProvider, error) {
		return nil, errors.New("missing project")
	}
	t.Cleanup(func() { newEmbeddingProvider = oldProvider })

	dir := t.TempDir()
	graphPath := filepath.Join(dir, "text.graphml")
	if err := os.WriteFile(graphPath, []byte(textGraphMLFixture), 0o644); err != nil {
		t.Fatalf("write graph fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"embed", graphPath, "--text-key", "doc"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "missing project") {
		t.Fatalf("expected provider error, got %q", stderr.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

const textGraphMLFixture = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="doc" for="node" attr.name="doc" attr.type="string"/>
  <key id="weight" for="edge" attr.name="weight" attr.type="double"/>
  <graph edgedefault="directed">
    <node id="a"><data key="doc">reads and writes files on disk</data></node>
    <node id="b"><data key="doc">parses bytes into a syntax tree</data></node>
    <edge source="a" target="b"><data key="weight">1.0</data></edge>
  </graph>
</graphml>`
