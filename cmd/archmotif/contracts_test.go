package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestContractsCommandWithoutPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"contracts"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestContractsCommandFixturePretty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"contracts", "../../internal/contracts/testdata/userstore"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"contracts: 3 declared",
		"userstore/store.UserStore",
		"userstore/api.Request",
		"[implements] userstore/store.MemStore",
		"[returns] userstore/store.NewMemStore",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestContractsCommandFixtureJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"contracts", "--format=json", "../../internal/contracts/testdata/userstore"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("expected JSON object output, got: %.120s", out)
	}
	for _, want := range []string{
		`"version": 1`,
		`"qname": "userstore/store.UserStore"`,
		`"kind": "implements"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("JSON output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestContractsCommandUnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"contracts", "--format=yaml", "../../internal/contracts/testdata/userstore"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown format") {
		t.Fatalf("expected unknown format error, got: %q", stderr.String())
	}
}
