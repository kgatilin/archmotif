package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// HTTPSchemaVersion is the version of the snapshot envelope returned by
// `GET /graphs/<id>` and tracked under meta.schema_version. Bump when the
// envelope shape changes in a way that breaks clients (e.g. renaming
// top-level fields, dropping nodes/edges, etc.).
const HTTPSchemaVersion = 1

// NewHTTPHandler builds an http.Handler that exposes the archmotif graph
// workspace + MCP tool set over plain HTTP. It is part 1/3 of the live-graph
// transport (#71): no WebSocket, no mutation broadcast yet.
//
// Endpoints:
//
//	GET  /graphs                              → [{id, name, hash, last_modified_ts}, ...]
//	GET  /graphs/<id>                         → {nodes, edges, meta: {hash, schema_version}}
//	POST /graphs/<id>/tool/<tool_name>        → JSON body = tool inputs; response = tool JSON output
//
// Tool dispatch is generic: every tool registered on the MCP server (the 7
// base R/W tools plus multi-graph, metrics and contracts tools) is reachable
// without per-tool wiring. The `<id>` path segment is injected as the tool's
// `graph_id` argument unless the body already supplies one — useful for
// cross-graph tools like graph_diff that take graph_a + graph_b explicitly.
func NewHTTPHandler(svc *Service) http.Handler {
	mux := http.NewServeMux()
	srv := newHTTPServer(svc)
	mux.HandleFunc("/graphs", srv.handleGraphs)
	mux.HandleFunc("/graphs/", srv.handleGraphsPrefixed)
	return mux
}

// httpServer is the request-handler glue. It owns the MCP server instance so
// that tool dispatch reuses the same Handler functions wired by registerTools.
type httpServer struct {
	svc    *Service
	server *server.MCPServer
}

func newHTTPServer(svc *Service) *httpServer {
	return &httpServer{
		svc:    svc,
		server: New(svc),
	}
}

// ----- GET /graphs --------------------------------------------------------

// graphListEntry is the envelope returned by GET /graphs. It is intentionally
// flatter than GraphRef so callers do not have to peer at the on-disk
// `<slug>:<variant>` layout: `id` is the addressable handle, `name` is the
// slug, `hash` is a short content hash, `last_modified_ts` is the file
// mtime as RFC3339.
type graphListEntry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Hash           string `json:"hash"`
	LastModifiedTS string `json:"last_modified_ts"`
}

func (s *httpServer) handleGraphs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	refs, err := s.svc.ListGraphs()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]graphListEntry, 0, len(refs))
	for _, ref := range refs {
		hash, _ := hashFile(ref.Path)
		ts := ""
		if info, err := os.Stat(ref.Path); err == nil {
			ts = info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
		}
		out = append(out, graphListEntry{
			ID:             ref.ID,
			Name:           ref.Slug,
			Hash:           hash,
			LastModifiedTS: ts,
		})
	}
	writeHTTPJSON(w, http.StatusOK, out)
}

// ----- /graphs/<id> and /graphs/<id>/tool/<tool_name> ---------------------

func (s *httpServer) handleGraphsPrefixed(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/graphs/")
	if rest == "" {
		http.Error(w, "missing graph id", http.StatusBadRequest)
		return
	}

	// Find the /tool/ delimiter (if any). Graph ids may contain `:` but
	// never literal `/tool/`, so this split is unambiguous.
	if idx := strings.Index(rest, "/tool/"); idx >= 0 {
		graphID := rest[:idx]
		toolName := rest[idx+len("/tool/"):]
		if graphID == "" || toolName == "" {
			http.Error(w, "missing graph id or tool name", http.StatusBadRequest)
			return
		}
		if strings.Contains(toolName, "/") {
			http.Error(w, "tool name must not contain '/'", http.StatusBadRequest)
			return
		}
		s.handleToolCall(w, r, graphID, toolName)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleSnapshot(w, r, rest)
}

// graphSnapshot is the response shape of GET /graphs/<id>. Nodes and edges
// echo the on-disk struct so the AC's "snapshot with nodes, edges, meta"
// matches without any field renaming.
type graphSnapshot struct {
	Nodes []Node       `json:"nodes"`
	Edges []Edge       `json:"edges"`
	Meta  snapshotMeta `json:"meta"`
}

type snapshotMeta struct {
	Hash          string `json:"hash"`
	SchemaVersion int    `json:"schema_version"`
}

func (s *httpServer) handleSnapshot(w http.ResponseWriter, _ *http.Request, graphID string) {
	g, err := s.svc.LoadGraph(graphID)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	path, err := s.svc.resolvePath(graphID)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, _ := hashFile(path)
	snap := graphSnapshot{
		Nodes: g.Nodes,
		Edges: g.Edges,
		Meta: snapshotMeta{
			Hash:          hash,
			SchemaVersion: HTTPSchemaVersion,
		},
	}
	if snap.Nodes == nil {
		snap.Nodes = []Node{}
	}
	if snap.Edges == nil {
		snap.Edges = []Edge{}
	}
	writeHTTPJSON(w, http.StatusOK, snap)
}

// handleToolCall dispatches POST /graphs/<id>/tool/<tool_name> to the
// registered MCP tool handler. The graph_id from the path is injected as
// `arguments.graph_id` unless the body already specifies one (so write tools
// like graph_add_node Just Work, and graph-pair tools like graph_diff that
// take graph_a/graph_b in the body still resolve correctly).
func (s *httpServer) handleToolCall(w http.ResponseWriter, r *http.Request, graphID, toolName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entry := s.server.GetTool(toolName)
	if entry == nil {
		writeHTTPError(w, http.StatusNotFound, fmt.Sprintf("unknown tool %q", toolName))
		return
	}

	// Parse the JSON body. An empty body is allowed and treated as `{}`.
	// Cap at 10 MiB — tool inputs are small JSON objects; anything larger
	// would be a misuse or an attempt to OOM the process.
	var args map[string]any
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				writeHTTPError(w, http.StatusBadRequest, fmt.Sprintf("parse body as JSON object: %v", err))
				return
			}
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	// Inject the path graph_id unless the body already supplies one. This is
	// the only piece of path → arguments wiring; everything else is left to
	// the per-tool handler.
	if _, exists := args["graph_id"]; !exists {
		args["graph_id"] = graphID
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = entry.Tool.Name
	req.Params.Arguments = args

	result, err := entry.Handler(r.Context(), req)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		writeHTTPError(w, http.StatusInternalServerError, "tool returned nil result")
		return
	}

	// The AC says: "response = MCP tool output". MCP tools return one or more
	// content blocks; archmotif tools always return a single text block whose
	// body is JSON. We unwrap that JSON so HTTP clients see structured output
	// directly, and fall back to the raw envelope when unwrapping fails.
	status := http.StatusOK
	if result.IsError {
		status = http.StatusBadRequest
	}
	if payload, ok := unwrapJSONResult(result); ok {
		writeHTTPRaw(w, status, payload)
		return
	}
	writeHTTPJSON(w, status, result)
}

// unwrapJSONResult tries to extract a single JSON text content block from
// the tool result and return it verbatim. Returns (nil, false) when the
// result is not in that shape.
func unwrapJSONResult(result *mcp.CallToolResult) ([]byte, bool) {
	if len(result.Content) != 1 {
		return nil, false
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return nil, false
	}
	if result.IsError {
		// Wrap the tool error message in a JSON envelope so callers can
		// distinguish 4xx-from-tool from 4xx-from-transport without parsing
		// the body shape. The text content of an error result is the raw
		// error message, not necessarily JSON.
		envelope, err := json.Marshal(map[string]any{
			"error":         text.Text,
			"is_tool_error": true,
		})
		if err != nil {
			return nil, false
		}
		return envelope, true
	}
	// Validate the body parses as JSON so we don't leak free-form text
	// under a JSON content-type header.
	if !json.Valid([]byte(text.Text)) {
		return nil, false
	}
	return []byte(text.Text), true
}

// ----- helpers ------------------------------------------------------------

func writeHTTPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeHTTPRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func writeHTTPError(w http.ResponseWriter, status int, msg string) {
	writeHTTPJSON(w, status, map[string]any{"error": msg})
}

// hashFile returns a short content hash of the file at path. Returns an
// empty string + error when the file cannot be read; callers may treat that
// as a soft failure (the field is just informational).
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
