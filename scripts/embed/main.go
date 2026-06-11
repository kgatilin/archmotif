// Command embed-exp builds Vertex AI text embeddings for archmotif graph nodes
// via the official google.golang.org/genai Go SDK (Vertex backend, ADC auth).
//
// Input : JSON map {nodeID: "semantic text"}.
// Output: JSON map {nodeID: [float32 embedding]}.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"google.golang.org/genai"
)

func main() {
	project := flag.String("project", env("ARCHMOTIF_GCP_PROJECT", ""), "GCP project (required; set ARCHMOTIF_GCP_PROJECT or -project)")
	location := flag.String("location", env("ARCHMOTIF_VERTEX_LOCATION", "europe-west4"), "Vertex region")
	// Model is config, not hardcoded: env ARCHMOTIF_EMBED_MODEL overrides the
	// default, and the -model flag overrides both. Default is the GA, recommended
	// gemini-embedding-001 (text-embedding-005 is legacy / being deprecated).
	model := flag.String("model", env("ARCHMOTIF_EMBED_MODEL", "gemini-embedding-001"), "embedding model")
	task := flag.String("task", env("ARCHMOTIF_EMBED_TASK", "CLUSTERING"), "embedding task type")
	in := flag.String("in", "pkg-text.json", "input {id:text} json")
	out := flag.String("out", "embeddings.json", "output {id:vec} json")
	flag.Parse()

	if *project == "" {
		fmt.Fprintln(os.Stderr, "error: GCP project required (set ARCHMOTIF_GCP_PROJECT or pass -project)")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*in)
	must(err)
	var texts map[string]string
	must(json.Unmarshal(raw, &texts))

	ids := make([]string, 0, len(texts))
	for id := range texts {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  *project,
		Location: *location,
	})
	must(err)

	emb := make(map[string][]float32, len(ids))
	for i, id := range ids {
		res, err := client.Models.EmbedContent(ctx, *model, genai.Text(texts[id]),
			&genai.EmbedContentConfig{TaskType: *task})
		must(err)
		emb[id] = res.Embeddings[0].Values
		if (i+1)%10 == 0 || i+1 == len(ids) {
			fmt.Fprintf(os.Stderr, "  embedded %d/%d\n", i+1, len(ids))
		}
	}

	b, err := json.Marshal(emb)
	must(err)
	must(os.WriteFile(*out, b, 0o644))
	dim := 0
	if len(ids) > 0 {
		dim = len(emb[ids[0]])
	}
	fmt.Printf("embedded %d nodes, dim=%d, model=%s region=%s -> %s\n",
		len(ids), dim, *model, *location, *out)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
