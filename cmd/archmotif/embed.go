package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kgatilin/archmotif/internal/contract"
	"github.com/kgatilin/archmotif/internal/graphmlx"
	"google.golang.org/genai"
)

type embeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type vertexEmbedConfig struct {
	Project  string
	Location string
	Model    string
	Task     string
}

type vertexEmbeddingProvider struct {
	client *genai.Client
	model  string
	task   string
}

var newEmbeddingProvider = func(ctx context.Context, cfg vertexEmbedConfig) (embeddingProvider, error) {
	if cfg.Project == "" {
		return nil, errors.New("set ARCHMOTIF_GCP_PROJECT or pass --project")
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  cfg.Project,
		Location: cfg.Location,
	})
	if err != nil {
		return nil, err
	}
	return &vertexEmbeddingProvider{client: client, model: cfg.Model, task: cfg.Task}, nil
}

func (p *vertexEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	res, err := p.client.Models.EmbedContent(ctx, p.model, genai.Text(text),
		&genai.EmbedContentConfig{TaskType: p.task})
	if err != nil {
		return nil, err
	}
	if len(res.Embeddings) == 0 || len(res.Embeddings[0].Values) == 0 {
		return nil, errors.New("Vertex returned no embedding values")
	}
	return res.Embeddings[0].Values, nil
}

type embedGraphOptions struct {
	TextKey        string
	VecKey         string
	CacheDir       string
	CacheNamespace string
}

type embedGraphResult struct {
	TextNodes int
	Embedded  int
	Cached    int
	Skipped   int
	Dim       int
}

// runEmbed implements `archmotif embed GRAPH --text-key KEY`: read a GraphML
// graph, embed every node text attribute through Vertex, and emit GraphML with
// a vector attribute (default: `vec`) suitable for semantic-clusters.
func runEmbed(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif embed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	textKey := fs.String("text-key", "doc", "node attribute containing text to embed")
	vecKey := fs.String("vec-key", "vec", "node attribute to write embedding vectors into")
	outPath := fs.String("out", "", "write GraphML to this path instead of stdout")
	project := fs.String("project", envDefault("ARCHMOTIF_GCP_PROJECT", ""), "Vertex AI GCP project")
	location := fs.String("location", envDefault("ARCHMOTIF_VERTEX_LOCATION", "europe-west4"), "Vertex AI region")
	model := fs.String("model", envDefault("ARCHMOTIF_EMBED_MODEL", "gemini-embedding-001"), "embedding model")
	task := fs.String("task", envDefault("ARCHMOTIF_EMBED_TASK", "CLUSTERING"), "embedding task type")
	cacheDir := fs.String("cache-dir", envDefault("ARCHMOTIF_EMBED_CACHE", ".archmotif/embed-cache"), "content-addressed embedding cache directory")
	noCache := fs.Bool("no-cache", false, "disable the embedding cache")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif embed [flags] <graphml-file>\n\nFlags:\n")
		fs.PrintDefaults()
	}

	pos, err := parsePermissiveErr(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fs.Usage()
		return 2
	}
	if strings.TrimSpace(*textKey) == "" {
		_, _ = fmt.Fprintln(stderr, "archmotif embed: --text-key must not be empty")
		return 2
	}
	if strings.TrimSpace(*vecKey) == "" {
		_, _ = fmt.Fprintln(stderr, "archmotif embed: --vec-key must not be empty")
		return 2
	}
	if *noCache {
		*cacheDir = ""
	}

	g, code := loadGraphML(pos[0], stderr)
	if g == nil {
		return code
	}

	ctx := context.Background()
	provider, err := newEmbeddingProvider(ctx, vertexEmbedConfig{
		Project:  *project,
		Location: *location,
		Model:    *model,
		Task:     *task,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif embed: %v\n", err)
		return 2
	}

	result, err := embedGraph(ctx, g, provider, embedGraphOptions{
		TextKey:        *textKey,
		VecKey:         *vecKey,
		CacheDir:       *cacheDir,
		CacheNamespace: *model + "\x00" + *task,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif embed: %v\n", err)
		return 1
	}

	if *outPath == "" {
		if err := contract.WriteGraphML(stdout, g); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif embed: write graphml: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "embedded %d nodes (api=%d cached=%d skipped=%d dim=%d model=%s region=%s)\n",
			result.TextNodes, result.Embedded, result.Cached, result.Skipped, result.Dim, *model, *location)
		return 0
	}

	f, err := os.Create(*outPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif embed: create %s: %v\n", *outPath, err)
		return 1
	}
	if err := contract.WriteGraphML(f, g); err != nil {
		_ = f.Close()
		_, _ = fmt.Fprintf(stderr, "archmotif embed: write %s: %v\n", *outPath, err)
		return 1
	}
	if err := f.Close(); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif embed: close %s: %v\n", *outPath, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "embedded %d nodes (api=%d cached=%d skipped=%d dim=%d model=%s region=%s) -> %s\n",
		result.TextNodes, result.Embedded, result.Cached, result.Skipped, result.Dim, *model, *location, *outPath)
	return 0
}

func embedGraph(ctx context.Context, g *graphmlx.Graph, provider embeddingProvider, opts embedGraphOptions) (embedGraphResult, error) {
	var result embedGraphResult
	textKey := strings.TrimSpace(opts.TextKey)
	vecKey := strings.TrimSpace(opts.VecKey)
	if textKey == "" {
		return result, errors.New("text key is empty")
	}
	if vecKey == "" {
		vecKey = "vec"
	}
	if provider == nil {
		return result, errors.New("embedding provider is nil")
	}

	for i := range g.Nodes {
		text := strings.TrimSpace(g.Nodes[i].Attrs[textKey])
		if text == "" {
			result.Skipped++
			continue
		}
		result.TextNodes++

		vec, cached, err := embeddingFromCache(opts.CacheDir, opts.CacheNamespace, text)
		if err != nil {
			return result, err
		}
		if cached {
			result.Cached++
		} else {
			vec, err = provider.Embed(ctx, text)
			if err != nil {
				return result, fmt.Errorf("embed node %q: %w", g.Nodes[i].ID, err)
			}
			if len(vec) == 0 {
				return result, fmt.Errorf("embed node %q: empty embedding", g.Nodes[i].ID)
			}
			if err := saveEmbeddingToCache(opts.CacheDir, opts.CacheNamespace, text, vec); err != nil {
				return result, err
			}
			result.Embedded++
		}

		if result.Dim == 0 {
			result.Dim = len(vec)
		} else if result.Dim != len(vec) {
			return result, fmt.Errorf("embed node %q: vector dimension %d differs from earlier dimension %d", g.Nodes[i].ID, len(vec), result.Dim)
		}
		if g.Nodes[i].Attrs == nil {
			g.Nodes[i].Attrs = map[string]string{}
		}
		g.Nodes[i].Attrs[vecKey] = formatVec(vec)
	}

	if result.TextNodes == 0 {
		return result, fmt.Errorf("no nodes carry non-empty text attribute %q", textKey)
	}
	return result, nil
}

func embeddingFromCache(cacheDir, namespace, text string) ([]float32, bool, error) {
	path := embeddingCachePath(cacheDir, namespace, text)
	if path == "" {
		return nil, false, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read embedding cache %s: %w", path, err)
	}
	var vec []float32
	if err := json.Unmarshal(raw, &vec); err != nil || len(vec) == 0 {
		return nil, false, nil
	}
	return vec, true, nil
}

func saveEmbeddingToCache(cacheDir, namespace, text string, vec []float32) error {
	path := embeddingCachePath(cacheDir, namespace, text)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create embedding cache: %w", err)
	}
	raw, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("marshal embedding cache: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write embedding cache %s: %w", path, err)
	}
	return nil
}

func embeddingCachePath(cacheDir, namespace, text string) string {
	if strings.TrimSpace(cacheDir) == "" {
		return ""
	}
	h := sha256.New()
	_, _ = io.WriteString(h, namespace)
	_, _ = h.Write([]byte{0})
	_, _ = io.WriteString(h, text)
	sum := hex.EncodeToString(h.Sum(nil))
	return filepath.Join(cacheDir, sum[:2], sum+".json")
}

func formatVec(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'g', -1, 32)
	}
	return strings.Join(parts, " ")
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parsePermissiveErr(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
