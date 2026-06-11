package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestMCPMissingSubcommand verifies that `archmotif mcp` without a subcommand
// prints a helpful error and returns exit 2.
func TestMCPMissingSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "missing subcommand") {
		t.Fatalf("expected 'missing subcommand', got %q", stderr.String())
	}
}

// TestMCPUnknownSubcommand verifies an unknown subcommand is rejected.
func TestMCPUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp", "blah"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected 'unknown subcommand', got %q", stderr.String())
	}
}

// TestMCPServeHelp verifies that `archmotif mcp serve -h` exits 0 and prints usage.
func TestMCPServeHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp", "serve", "-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "stdio MCP server") {
		t.Fatalf("expected help text mentioning stdio MCP server, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--http") {
		t.Fatalf("expected help text to advertise --http flag, got %q", stderr.String())
	}
}
