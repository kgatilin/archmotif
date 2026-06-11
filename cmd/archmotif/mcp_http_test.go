package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archmotif/internal/mcpserver"
)

// fixtureGraphML is the minimal canonical GraphML fixture used by the HTTP
// transport tests. It mirrors the shape of internal/mcpserver/store_test.go's
// fixture so the same node ids (pkg:foo, pkg:foo:bar, ...) flow through.
const fixtureGraphML = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_name" for="node" attr.name="name" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0">
      <data key="n_id">pkg:foo</data>
      <data key="n_kind">package</data>
      <data key="n_name">foo</data>
    </node>
    <node id="n1">
      <data key="n_id">pkg:foo:bar</data>
      <data key="n_kind">function</data>
      <data key="n_name">Bar</data>
    </node>
    <edge id="e0" source="n0" target="n1">
      <data key="e_kind">contains</data>
    </edge>
  </graph>
</graphml>
`

// newHTTPFixture writes the canonical GraphML to a fresh temp workspace at
// graphs/<slug>/actual.graphml and returns (workspaceRoot, slug).
func newHTTPFixture(t *testing.T, slug string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "graphs", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "actual.graphml"), []byte(fixtureGraphML), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return root
}

// newHTTPTestServer spins up an in-process httptest.Server wrapping the
// archmotif HTTP handler against the fixture workspace.
func newHTTPTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := newHTTPFixture(t, "demo")
	svc := mcpserver.NewService(root)
	handler := mcpserver.NewHTTPHandler(svc)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, root
}

// readBody reads a response body or fails the test, ensuring the body is
// always closed.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func TestHTTPGetGraphs(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	resp, err := http.Get(ts.URL + "/graphs")
	if err != nil {
		t.Fatalf("GET /graphs: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var entries []map[string]any
	body := readBody(t, resp)
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("decode list: %v; body=%s", err, body)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1; entries=%+v", len(entries), entries)
	}
	got := entries[0]
	if got["id"] != "demo:actual" {
		t.Errorf("id = %v, want demo:actual", got["id"])
	}
	if got["name"] != "demo" {
		t.Errorf("name = %v, want demo", got["name"])
	}
	if got["hash"] == nil || got["hash"] == "" {
		t.Errorf("hash should be populated, got %v", got["hash"])
	}
	if got["last_modified_ts"] == nil || got["last_modified_ts"] == "" {
		t.Errorf("last_modified_ts should be populated, got %v", got["last_modified_ts"])
	}
}

func TestHTTPGetGraphSnapshot(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	resp, err := http.Get(ts.URL + "/graphs/demo")
	if err != nil {
		t.Fatalf("GET /graphs/demo: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var snap struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
		Meta  struct {
			Hash          string `json:"hash"`
			SchemaVersion int    `json:"schema_version"`
		} `json:"meta"`
	}
	body := readBody(t, resp)
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode snapshot: %v; body=%s", err, body)
	}
	if len(snap.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(snap.Nodes))
	}
	if len(snap.Edges) != 1 {
		t.Errorf("edges = %d, want 1", len(snap.Edges))
	}
	if snap.Meta.SchemaVersion != mcpserver.HTTPSchemaVersion {
		t.Errorf("schema_version = %d, want %d", snap.Meta.SchemaVersion, mcpserver.HTTPSchemaVersion)
	}
	if snap.Meta.Hash == "" {
		t.Errorf("meta.hash should be populated")
	}
}

func TestHTTPGetGraphSnapshotNotFound(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	resp, err := http.Get(ts.URL + "/graphs/nope")
	if err != nil {
		t.Fatalf("GET /graphs/nope: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHTTPToolReadDispatch exercises POST /graphs/<id>/tool/graph_query and
// confirms the path-derived graph_id reaches the handler, the filter from
// the body is applied, and the response unwraps the tool's JSON output
// (not the MCP envelope).
func TestHTTPToolReadDispatch(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	payload := map[string]any{
		"filter": map[string]any{"kind": "function"},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(
		ts.URL+"/graphs/demo/tool/graph_query",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var result struct {
		Nodes []map[string]any `json:"nodes"`
		Count int              `json:"count"`
	}
	respBody := readBody(t, resp)
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v; body=%s", err, respBody)
	}
	if result.Count != 1 {
		t.Fatalf("count = %d, want 1; body=%s", result.Count, respBody)
	}
	if result.Nodes[0]["id"] != "pkg:foo:bar" {
		t.Errorf("node id = %v, want pkg:foo:bar", result.Nodes[0]["id"])
	}
}

// TestHTTPToolWriteDispatch confirms POST -> graph_add_node mutates on-disk
// state — the second GET /graphs/<id> snapshot sees the new node. Covers
// the AC's "write tools work via HTTP without per-tool wiring".
func TestHTTPToolWriteDispatch(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	payload := map[string]any{
		"kind": "memory",
		"attrs": map[string]any{
			"id":   "mem:test",
			"name": "TestNote",
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(
		ts.URL+"/graphs/demo/tool/graph_add_node",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST add_node: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var addResult struct {
		ID string `json:"id"`
	}
	respBody := readBody(t, resp)
	if err := json.Unmarshal(respBody, &addResult); err != nil {
		t.Fatalf("decode add: %v; body=%s", err, respBody)
	}
	if addResult.ID != "mem:test" {
		t.Errorf("add returned id = %q, want mem:test", addResult.ID)
	}

	// Re-fetch the snapshot and confirm the new node is present.
	resp2, err := http.Get(ts.URL + "/graphs/demo")
	if err != nil {
		t.Fatalf("GET snapshot: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status = %d", resp2.StatusCode)
	}
	var snap struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(readBody(t, resp2), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	found := false
	for _, n := range snap.Nodes {
		if n["id"] == "mem:test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mem:test not in snapshot after add; nodes=%+v", snap.Nodes)
	}
}

// TestHTTPAllBaseToolsReachable confirms every one of the 7 base R/W tools
// from #55 is reachable via POST /graphs/<id>/tool/<tool_name> and returns
// a 200 with parseable JSON. This is the AC item "All 7 base R/W tools from
// #55 work via HTTP without per-tool wiring (generic handler dispatch)".
//
// We do NOT assert on per-tool result shape (that's covered by the unit
// tests in internal/mcpserver/); the point here is purely that the generic
// HTTP dispatch invokes each tool and gets a non-error JSON response.
func TestHTTPAllBaseToolsReachable(t *testing.T) {
	ts, _ := newHTTPTestServer(t)

	cases := []struct {
		tool string
		body map[string]any
	}{
		// Reads
		{"graph_query", map[string]any{}},
		{"graph_neighbors", map[string]any{"node_id": "pkg:foo"}},
		{"graph_path", map[string]any{"from": "pkg:foo", "to": "pkg:foo:bar"}},
		// Writes
		{"graph_activate", map[string]any{"node_ids": []string{"pkg:foo"}, "weight": 0.5}},
		{"graph_add_node", map[string]any{"kind": "memory", "attrs": map[string]any{"id": "mem:reachable", "name": "Reach"}}},
		{"graph_update_weight", map[string]any{"node_id": "pkg:foo", "delta": 1.0}},
		// graph_add_edge needs the new node to exist — sequence after add_node.
		{"graph_add_edge", map[string]any{"from": "pkg:foo", "to": "pkg:foo:bar", "kind": "uses"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			resp, err := http.Post(
				ts.URL+"/graphs/demo/tool/"+tc.tool,
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			respBody := readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s status = %d, want 200; body=%s",
					tc.tool, resp.StatusCode, respBody)
			}
			// All tool outputs are JSON; just confirm the body parses.
			var anyOut any
			if err := json.Unmarshal(respBody, &anyOut); err != nil {
				t.Fatalf("%s: response not JSON: %v; body=%s", tc.tool, err, respBody)
			}
		})
	}
}

// TestHTTPToolUnknownReturns404 confirms an unknown tool name yields 404
// rather than crashing or returning an empty 200.
func TestHTTPToolUnknownReturns404(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	resp, err := http.Post(
		ts.URL+"/graphs/demo/tool/no_such_tool",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHTTPToolBadJSONReturns400 confirms a malformed body is rejected with
// 400 (rather than passing nonsense to the tool handler).
func TestHTTPToolBadJSONReturns400(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	resp, err := http.Post(
		ts.URL+"/graphs/demo/tool/graph_query",
		"application/json",
		strings.NewReader(`{not-json`),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHTTPToolErrorBubblesUp confirms that a tool-reported error (e.g.
// missing required arg) returns 400 with a JSON envelope flagging
// is_tool_error=true, distinct from transport-layer errors.
func TestHTTPToolErrorBubblesUp(t *testing.T) {
	ts, _ := newHTTPTestServer(t)
	// graph_add_edge requires `kind`; omit it to force the tool to surface
	// an error result.
	payload := map[string]any{
		"from": "pkg:foo",
		"to":   "pkg:foo:bar",
		// kind missing
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(
		ts.URL+"/graphs/demo/tool/graph_add_edge",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (tool error); body=%s",
			resp.StatusCode, readBody(t, resp))
	}
	var envelope struct {
		Error       string `json:"error"`
		IsToolError bool   `json:"is_tool_error"`
	}
	if err := json.Unmarshal(readBody(t, resp), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.IsToolError {
		t.Errorf("is_tool_error not set in error envelope: %+v", envelope)
	}
	if envelope.Error == "" {
		t.Errorf("error message empty: %+v", envelope)
	}
}

// TestHTTPServeRandomPort spins up the actual `archmotif mcp serve --http :0`
// command in-process via run() and confirms the CLI flag plumbs through
// end-to-end. AC item 1 ("boots a server on PORT").
//
// The chosen listen address is delivered via the httpListenAddrSink hook
// rather than scraped from stderr, which (a) eliminates a data race on the
// shared bytes.Buffer under -race and (b) removes the 200×10ms polling loop.
func TestHTTPServeRandomPort(t *testing.T) {
	root := newHTTPFixture(t, "demo")

	addrCh := make(chan string, 1)
	httpListenAddrSink = addrCh
	t.Cleanup(func() { httpListenAddrSink = nil })

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"mcp", "serve", "-root", root, "--http", "127.0.0.1:0"}, &stdout, &stderr)
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("server did not publish listen address within 5s; stderr=%q", stderr.String())
	}

	// Hit GET /graphs to confirm the server is live.
	resp, err := http.Get(fmt.Sprintf("http://%s/graphs", addr))
	if err != nil {
		t.Fatalf("GET /graphs: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// run() blocks on its own signal context; the goroutine outlives the
	// test and the OS reaps everything at process exit. Same pattern as
	// TestServeListsToolsAndQueries.
	_ = done
}
