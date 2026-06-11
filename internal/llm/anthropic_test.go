package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// roundTripFunc adapts a function into an http.RoundTripper so tests
// can stub the Anthropic API without spinning up a live server. The
// captured request and the response are both controlled by the test.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// withHTTPClient swaps the package httpClient for the duration of a
// test. Returns a restore function the caller defers. Tests use this
// instead of an exported setter so production code keeps no test
// surface area.
func withHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	prev := httpClient
	httpClient = c
	t.Cleanup(func() { httpClient = prev })
}

// withUsageLogPath redirects usage log writes to a tmp file for the
// duration of a test.
func withUsageLogPath(t *testing.T, path string) {
	t.Helper()
	prev := usageLogPath
	usageLogPath = path
	t.Cleanup(func() { usageLogPath = prev })
}

// jsonResponse builds an *http.Response with the given status and a
// JSON-encoded body. Used by the mock RoundTripper below.
func jsonResponse(status int, body any) *http.Response {
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(b)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// initGitRepo bootstraps a tmp dir as a git repo with two seed files
// so a tested diff has a base to apply against. Returns the repo path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "seed")
	return dir
}

// validDiff is a unified diff that applies to the seed files in
// initGitRepo. Single-file change keeps the test focused on the
// happy-path glue, not on diff semantics.
const validDiff = `diff --git a/a.txt b/a.txt
index ce01362..3b18e51 100644
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-hello
+goodbye
`

// TestAnthropicMaterializer_ImplementsInterface is a redundancy check
// in test form: the package-level _ = (Materializer)(nil) assertion
// already enforces this at compile time, but a test makes the contract
// visible in test output too.
func TestAnthropicMaterializer_ImplementsInterface(t *testing.T) {
	var _ Materializer = NewAnthropic("test-key")
}

// TestApply_HappyPath exercises the full glue: prompt rendered, API
// called, fenced diff extracted, branch created, diff applied, usage
// record written.
func TestApply_HappyPath(t *testing.T) {
	repo := initGitRepo(t)
	logPath := filepath.Join(repo, "usage.jsonl")
	withUsageLogPath(t, logPath)

	var capturedReq *http.Request
	var capturedBody []byte
	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			b, _ := io.ReadAll(req.Body)
			capturedBody = b
			body := anthropicResponse{
				Content: []anthropicContentBlock{
					{Type: "text", Text: "```diff\n" + validDiff + "```\n"},
				},
				Usage: anthropicUsage{InputTokens: 100, OutputTokens: 50},
				Model: ModelSonnet,
			}
			return jsonResponse(200, body), nil
		}),
	})

	a := &AnthropicMaterializer{
		APIKey:       "test-key",
		DefaultModel: ModelSonnet,
		RepoDir:      repo,
	}
	br, err := a.Apply(context.Background(), Proposal{
		ID:          "motif-001",
		Description: "extract interface",
		AffectedFiles: map[string][]byte{
			"a.txt": []byte("hello\n"),
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if br.Name != "archmotif/refactor/motif-001" {
		t.Errorf("branch name = %q, want archmotif/refactor/motif-001", br.Name)
	}
	if !bytes.Contains(br.Diff, []byte("-hello")) {
		t.Errorf("Diff missing expected hunk: %s", br.Diff)
	}
	if br.AppliedAt == "" {
		t.Error("AppliedAt empty")
	}

	// Request shape: api key, anthropic-version header, model in body.
	if got := capturedReq.Header.Get("x-api-key"); got != "test-key" {
		t.Errorf("x-api-key = %q, want test-key", got)
	}
	if got := capturedReq.Header.Get("anthropic-version"); got != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want %s", got, anthropicAPIVersion)
	}
	var sent anthropicRequest
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Model != ModelSonnet {
		t.Errorf("sent model = %q, want %s", sent.Model, ModelSonnet)
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want one user message", sent.Messages)
	}
	if !strings.Contains(sent.Messages[0].Content, "extract interface") {
		t.Errorf("prompt missing description, got: %s", sent.Messages[0].Content)
	}

	// Branch was created and is checked out.
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "archmotif/refactor/motif-001" {
		t.Errorf("HEAD = %q, want archmotif/refactor/motif-001", got)
	}

	// Diff applied: a.txt now reads "goodbye".
	got, err := os.ReadFile(filepath.Join(repo, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "goodbye\n" {
		t.Errorf("a.txt = %q, want \"goodbye\\n\"", got)
	}

	// Usage record written with cost computed.
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read usage log: %v", err)
	}
	var rec usageRecord
	if err := json.Unmarshal(bytes.TrimSpace(logBytes), &rec); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if rec.ProposalID != "motif-001" || rec.Model != ModelSonnet ||
		rec.InputTokens != 100 || rec.OutputTokens != 50 {
		t.Errorf("usage record mismatch: %+v", rec)
	}
	wantCost := Cost(ModelSonnet, 100, 50)
	if rec.CostUSD != wantCost {
		t.Errorf("cost = %v, want %v", rec.CostUSD, wantCost)
	}
}

// TestApply_ProposalModelOverride confirms Proposal.Model wins over
// DefaultModel when set. Pins the ADR-017 contract for --model.
func TestApply_ProposalModelOverride(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	var sentModel string
	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(req.Body)
			var sent anthropicRequest
			_ = json.Unmarshal(b, &sent)
			sentModel = sent.Model
			return jsonResponse(200, anthropicResponse{
				Content: []anthropicContentBlock{
					{Type: "text", Text: "```diff\n" + validDiff + "```\n"},
				},
			}), nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "k", DefaultModel: ModelSonnet, RepoDir: repo}
	if _, err := a.Apply(context.Background(), Proposal{ID: "p1", Model: ModelOpus}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sentModel != ModelOpus {
		t.Errorf("model = %q, want %q", sentModel, ModelOpus)
	}
}

// TestApply_NoFencedDiff pins the ADR-017 parse-fail contract: a
// response without a fenced ```diff block surfaces ErrNoFencedDiff,
// not a generic error.
func TestApply_NoFencedDiff(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(200, anthropicResponse{
				Content: []anthropicContentBlock{
					{Type: "text", Text: "Sorry, I cannot do that."},
				},
			}), nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "k", DefaultModel: ModelSonnet, RepoDir: repo}
	_, err := a.Apply(context.Background(), Proposal{ID: "p1"})
	if !errors.Is(err, ErrNoFencedDiff) {
		t.Fatalf("err = %v, want ErrNoFencedDiff", err)
	}
}

// TestApply_MultipleFencedDiffs pins the "exactly one fenced block"
// contract. Two ```diff blocks → ErrMultipleFencedDiffs.
func TestApply_MultipleFencedDiffs(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			text := "```diff\n" + validDiff + "```\n\nand also:\n\n```diff\n" + validDiff + "```\n"
			return jsonResponse(200, anthropicResponse{
				Content: []anthropicContentBlock{{Type: "text", Text: text}},
			}), nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "k", DefaultModel: ModelSonnet, RepoDir: repo}
	_, err := a.Apply(context.Background(), Proposal{ID: "p1"})
	if !errors.Is(err, ErrMultipleFencedDiffs) {
		t.Fatalf("err = %v, want ErrMultipleFencedDiffs", err)
	}
}

// TestApply_4xxErrorPropagated checks that an Anthropic 4xx (e.g. 401
// missing key) is surfaced with the body included so debugging is
// possible. Does not assert the exact message.
func TestApply_4xxErrorPropagated(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 401,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad key"}}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "bad", DefaultModel: ModelSonnet, RepoDir: repo}
	_, err := a.Apply(context.Background(), Proposal{ID: "p1"})
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err missing status: %v", err)
	}
	if !strings.Contains(err.Error(), "bad key") {
		t.Errorf("err missing body: %v", err)
	}
}

// TestApply_ApplyCheckFails surfaces a malformed diff as
// *ApplyCheckError with stderr captured.
func TestApply_ApplyCheckFails(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	bogus := "```diff\ndiff --git a/missing.txt b/missing.txt\n--- a/missing.txt\n+++ b/missing.txt\n@@ -1 +1 @@\n-was\n+now\n```\n"
	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(200, anthropicResponse{
				Content: []anthropicContentBlock{{Type: "text", Text: bogus}},
			}), nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "k", DefaultModel: ModelSonnet, RepoDir: repo}
	_, err := a.Apply(context.Background(), Proposal{ID: "p1"})
	var apErr *ApplyCheckError
	if !errors.As(err, &apErr) {
		t.Fatalf("err = %v, want *ApplyCheckError", err)
	}
	if apErr.Stderr == "" {
		t.Errorf("ApplyCheckError.Stderr is empty")
	}
}

// TestApply_BranchExists fails fast (no overwrite) when the target
// branch already exists, per ADR-024.
func TestApply_BranchExists(t *testing.T) {
	repo := initGitRepo(t)
	withUsageLogPath(t, filepath.Join(repo, "usage.jsonl"))

	// Pre-create the target branch.
	if out, err := exec.Command("git", "-C", repo, "branch", "archmotif/refactor/p1").CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}

	withHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(200, anthropicResponse{
				Content: []anthropicContentBlock{
					{Type: "text", Text: "```diff\n" + validDiff + "```\n"},
				},
			}), nil
		}),
	})

	a := &AnthropicMaterializer{APIKey: "k", DefaultModel: ModelSonnet, RepoDir: repo}
	_, err := a.Apply(context.Background(), Proposal{ID: "p1"})
	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want ErrBranchExists", err)
	}
}

// TestRenderPrompt renders the embedded v1 template and checks the
// load-bearing structural anchors. Mirrors prompts_test.go but goes
// through the embed path the runtime uses.
func TestRenderPrompt(t *testing.T) {
	out, err := RenderPrompt(Proposal{
		ID:          "motif-001",
		Description: "extract interface from repeated method shape",
		AffectedFiles: map[string][]byte{
			"foo.go": []byte("package foo\n"),
		},
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	for _, want := range []string{
		"PROPOSAL: extract interface from repeated method shape",
		"--- foo.go ---",
		"```diff",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n%s", want, out)
		}
	}
}

// TestExtractFencedDiff_TrailingNewline pins behaviour when the
// fenced body has no trailing newline: extractFencedDiff appends one
// so `git apply` accepts the input.
func TestExtractFencedDiff_TrailingNewline(t *testing.T) {
	body, err := extractFencedDiff("```diff\nx\n```")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		t.Errorf("body missing trailing newline: %q", body)
	}
}
