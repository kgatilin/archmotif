package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// rpcRequest is a minimal JSON-RPC 2.0 client used for the integration test.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// TestServeListsToolsAndQueries spins up the stdio MCP server in-process
// against pipe-backed stdin/stdout, sends initialize + tools/list, asserts the
// base 7 tools (and the multi-graph / metrics tools added in #56) are exposed,
// then calls graph_query and confirms a known node comes back.
func TestServeListsToolsAndQueries(t *testing.T) {
	root := installFixture(t, "demo")
	svc := NewService(root)

	clientIn, serverIn := io.Pipe()   // server reads from serverIn (= clientIn output)
	serverOut, clientOut := io.Pipe() // server writes to clientOut (read via serverOut)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, svc, clientIn, clientOut)
	}()

	reader := bufio.NewReader(serverOut)

	send := func(req rpcRequest) {
		t.Helper()
		req.JSONRPC = "2.0"
		body, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = append(body, '\n')
		if _, err := serverIn.Write(body); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	recv := func() rpcResponse {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		return resp
	}

	// 1. initialize
	initParams, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})
	send(rpcRequest{ID: 1, Method: "initialize", Params: initParams})
	if resp := recv(); resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}

	// 2. tools/list
	send(rpcRequest{ID: 2, Method: "tools/list"})
	resp := recv()
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("parse tools/list: %v", err)
	}
	have := map[string]bool{}
	for _, tool := range list.Tools {
		have[tool.Name] = true
	}
	// Base 7 (read + write).
	must := []string{
		"graph_query",
		"graph_neighbors",
		"graph_path",
		"graph_activate",
		"graph_add_node",
		"graph_add_edge",
		"graph_update_weight",
		// Multi-graph (#56).
		"graph_list",
		"graph_checkout",
		"graph_fork",
		"graph_merge",
		"graph_diff",
		"graph_compare_metrics",
		"graph_drift",
		"graph_metrics_list",
		// Target architecture graphs.
		"target_put",
		"target_list",
		"target_show",
		// Contract lens (#57).
		"contracts_tag",
		"contracts_list",
		"contracts_diff",
		"contracts_consumers",
		"contracts_producers",
		"contracts_field_history",
		"contracts_export",
	}
	for _, name := range must {
		if !have[name] {
			t.Errorf("missing tool %q", name)
		}
	}
	// At least one per-metric tool must be registered.
	anyMetric := false
	for name := range have {
		if strings.HasPrefix(name, "graph_metric_") {
			anyMetric = true
			break
		}
	}
	if !anyMetric {
		t.Errorf("expected at least one graph_metric_* tool, got none")
	}

	// 3. tools/call -> graph_query
	callParams, _ := json.Marshal(map[string]any{
		"name": "graph_query",
		"arguments": map[string]any{
			"graph_id": "demo",
			"filter":   map[string]any{"kind": "function"},
		},
	})
	send(rpcRequest{ID: 3, Method: "tools/call", Params: callParams})
	resp = recv()
	if resp.Error != nil {
		t.Fatalf("tools/call error: %+v", resp.Error)
	}
	var call struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &call); err != nil {
		t.Fatalf("parse tools/call: %v", err)
	}
	if call.IsError {
		t.Fatalf("tool reported error: %+v", call)
	}
	if len(call.Content) == 0 {
		t.Fatalf("no content in tool result: %s", resp.Result)
	}
	if !strings.Contains(call.Content[0].Text, "pkg:foo:bar") {
		t.Fatalf("expected query result to mention pkg:foo:bar, got %q", call.Content[0].Text)
	}

	// Tear down.
	cancel()
	_ = serverIn.Close()
	_ = clientOut.Close()
	select {
	case err := <-done:
		// Listen returns on ctx cancel; surface only truly unexpected errors.
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Logf("Serve returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Logf("server did not shut down in 2s; that is fine for the test")
	}
}

// silence unused fmt import when this file is built alone
var _ = fmt.Sprintf
