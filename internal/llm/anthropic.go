package llm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// Default Anthropic model identifiers. ModelSonnet is the default code-
// generation model per ADR-017; ModelOpus is the override for hard
// cases (the orchestrator passes it via Proposal.Model).
const (
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-7"
)

// anthropicAPIURL is the Messages API endpoint per ADR-017.
const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

// anthropicAPIVersion pins the Anthropic API version header. Bumping
// requires a focused PR; ADR-017 commits to this value.
const anthropicAPIVersion = "2023-06-01"

// defaultMaxTokens caps a single completion. 8192 is comfortably above
// the size of any single-file refactor diff we expect in v1; raise in
// a focused PR if a real workload bumps against the cap.
const defaultMaxTokens = 8192

// httpClient is the package-level HTTP client used for Anthropic API
// calls. Tests substitute this with a fake transport; production code
// uses http.DefaultClient with a sensible timeout.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// usageLogPath is the per-call usage log file written next to the
// repo root. Tests substitute via package-level setter (see
// SetUsageLogPath); production code uses "usage.jsonl" in CWD.
var usageLogPath = "usage.jsonl"

//go:embed prompts/v1.tmpl
var promptFS embed.FS

// Distinct error sentinels for ADR-017's named failure modes. Tests
// and the orchestrator (Stage 9) match against these via errors.Is.
var (
	// ErrNoFencedDiff is returned when the LLM response contains no
	// ```diff fenced block.
	ErrNoFencedDiff = errors.New("llm: no fenced ```diff block in response")
	// ErrMultipleFencedDiffs is returned when the LLM response
	// contains more than one ```diff fenced block. ADR-017 commits to
	// "one fenced block, nothing before or after" — multiple blocks
	// indicate prompt drift.
	ErrMultipleFencedDiffs = errors.New("llm: multiple fenced ```diff blocks in response")
	// ErrEmptyResponse is returned when the API returned 200 but no
	// text content blocks (or all empty). Distinct from network errors.
	ErrEmptyResponse = errors.New("llm: empty response from anthropic api")
	// ErrBranchExists is returned when the materializer cannot create
	// the target branch because it already exists.
	ErrBranchExists = errors.New("llm: target branch already exists")
)

// ApplyCheckError wraps a `git apply --check` failure with the
// captured stderr so the caller can surface it for human inspection.
type ApplyCheckError struct {
	Stderr string
}

func (e *ApplyCheckError) Error() string {
	return fmt.Sprintf("llm: git apply --check failed: %s", strings.TrimSpace(e.Stderr))
}

// AnthropicMaterializer is the Stage 7 Anthropic provider. It renders
// the v1 prompt, POSTs to https://api.anthropic.com/v1/messages,
// extracts the fenced diff, validates with `git apply --check`,
// applies the patch on a fresh branch, and appends a usage record to
// usage.jsonl.
//
// APIKey is read from ANTHROPIC_API_KEY at the call site that
// constructs the materializer; storing it on the struct keeps the
// dependency on env-var lookup out of this package and lets tests
// inject a fake. DefaultModel is used when Proposal.Model is empty.
//
// RepoDir is the working tree where the diff will be applied and the
// branch created. The CLI populates it from the user's argv; tests
// pass a tmp dir initialised with `git init`.
type AnthropicMaterializer struct {
	APIKey       string
	DefaultModel string
	RepoDir      string
}

// NewAnthropic returns a materializer with the package default model
// (claude-sonnet-4-6 per ADR-017). The caller is responsible for
// passing a real API key; the constructor does no validation so unit
// tests can pass an empty string. RepoDir defaults to "." (CWD).
func NewAnthropic(apiKey string) *AnthropicMaterializer {
	return &AnthropicMaterializer{
		APIKey:       apiKey,
		DefaultModel: ModelSonnet,
		RepoDir:      ".",
	}
}

// anthropicRequest is the wire shape for POST /v1/messages.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the wire shape for a successful 200 response.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
	Model   string                  `json:"model"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// usageRecord is one line of usage.jsonl per ADR-017.
type usageRecord struct {
	ProposalID   string  `json:"proposal_id"`
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	DurationMS   int64   `json:"duration_ms"`
	TS           string  `json:"ts"`
}

// RenderPrompt builds the user message body from the v1 prompt
// template. Exposed (capitalised) so the CLI's --dry-run path can
// produce the exact prompt the runtime would send.
func RenderPrompt(p Proposal) (string, error) {
	raw, err := promptFS.ReadFile("prompts/v1.tmpl")
	if err != nil {
		return "", fmt.Errorf("read embedded prompt: %w", err)
	}
	tmpl, err := template.New("v1").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse prompt: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("execute prompt: %w", err)
	}
	return buf.String(), nil
}

// Apply implements the Stage 7 contract. See ADR-017 for the full
// failure-mode taxonomy; in summary:
//
//   - Render prompt from prompts/v1.tmpl.
//   - POST to https://api.anthropic.com/v1/messages with the rendered
//     prompt as a single user message.
//   - Extract the single ```diff fenced block; surface ErrNoFencedDiff
//     or ErrMultipleFencedDiffs if the contract is violated.
//   - Validate with `git apply --check` against RepoDir; surface
//     *ApplyCheckError on rejection.
//   - Create branch archmotif/refactor/<proposal-id>; fail with
//     ErrBranchExists if it already exists (no auto-overwrite).
//   - Apply the patch on the new branch.
//   - Append a usage record to usage.jsonl.
func (a *AnthropicMaterializer) Apply(ctx context.Context, p Proposal) (Branch, error) {
	model := a.DefaultModel
	if p.Model != "" {
		model = p.Model
	}
	if model == "" {
		model = ModelSonnet
	}

	prompt, err := RenderPrompt(p)
	if err != nil {
		return Branch{}, err
	}

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: defaultMaxTokens,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return Branch{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return Branch{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return Branch{}, fmt.Errorf("anthropic call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Branch{}, fmt.Errorf("read response: %w", err)
	}
	duration := time.Since(start)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Branch{}, fmt.Errorf("anthropic api status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return Branch{}, fmt.Errorf("decode response: %w", err)
	}

	text := firstTextBlock(parsed.Content)
	if text == "" {
		return Branch{}, ErrEmptyResponse
	}

	diff, err := extractFencedDiff(text)
	if err != nil {
		return Branch{}, err
	}

	// Validate diff against the working tree before mutating it.
	if err := gitApplyCheck(a.RepoDir, diff); err != nil {
		return Branch{}, err
	}

	branchName := fmt.Sprintf("archmotif/refactor/%s", p.ID)
	if err := createAndApplyBranch(a.RepoDir, branchName, diff); err != nil {
		return Branch{}, err
	}

	appliedAt := time.Now().UTC().Format(time.RFC3339)

	// Best-effort usage logging — a write failure here must not
	// invalidate the successful refactor; surface as a stderr line.
	if logErr := writeUsageRecord(usageRecord{
		ProposalID:   p.ID,
		Model:        model,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		CostUSD:      Cost(model, parsed.Usage.InputTokens, parsed.Usage.OutputTokens),
		DurationMS:   duration.Milliseconds(),
		TS:           appliedAt,
	}); logErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warn: usage log write failed: %v\n", logErr)
	}

	return Branch{
		Name:      branchName,
		Diff:      diff,
		AppliedAt: appliedAt,
	}, nil
}

// firstTextBlock returns the text of the first content block whose
// type is "text", per the Anthropic Messages API shape.
func firstTextBlock(blocks []anthropicContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

// fencedDiffRE matches a ```diff fenced block. Lazy match on body so
// we can detect "multiple blocks" by looking at all matches.
var fencedDiffRE = regexp.MustCompile("(?s)```diff\\s*\\n(.*?)\\n```")

// extractFencedDiff returns the body of the single ```diff fenced
// block in text, or one of the named errors if zero or many are
// present. Trailing newline is preserved so `git apply` is happy.
func extractFencedDiff(text string) ([]byte, error) {
	matches := fencedDiffRE.FindAllStringSubmatch(text, -1)
	switch len(matches) {
	case 0:
		return nil, ErrNoFencedDiff
	case 1:
		body := matches[0][1]
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return []byte(body), nil
	default:
		return nil, ErrMultipleFencedDiffs
	}
}

// gitApplyCheck shells out to `git apply --check` against repoDir.
// Returns nil on success, *ApplyCheckError on rejection (with stderr
// captured), or a generic error for tooling failures.
func gitApplyCheck(repoDir string, diff []byte) error {
	cmd := exec.Command("git", "-C", repoDir, "apply", "--check", "-")
	cmd.Stdin = bytes.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &ApplyCheckError{Stderr: stderr.String()}
	}
	return nil
}

// createAndApplyBranch creates a fresh branch off HEAD in repoDir and
// applies diff on it. Fails with ErrBranchExists if the branch is
// already present (no auto-overwrite per ADR-024).
func createAndApplyBranch(repoDir, branch string, diff []byte) error {
	// Probe for branch existence. `git rev-parse --verify` exits 0 if
	// the ref resolves, non-zero otherwise.
	probe := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "refs/heads/"+branch)
	probe.Stdout = io.Discard
	probe.Stderr = io.Discard
	if probe.Run() == nil {
		return fmt.Errorf("%w: %s", ErrBranchExists, branch)
	}

	checkout := exec.Command("git", "-C", repoDir, "checkout", "-b", branch)
	var coStderr bytes.Buffer
	checkout.Stderr = &coStderr
	if err := checkout.Run(); err != nil {
		return fmt.Errorf("git checkout -b %s: %w: %s", branch, err, strings.TrimSpace(coStderr.String()))
	}

	apply := exec.Command("git", "-C", repoDir, "apply", "-")
	apply.Stdin = bytes.NewReader(diff)
	var apStderr bytes.Buffer
	apply.Stderr = &apStderr
	if err := apply.Run(); err != nil {
		return fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(apStderr.String()))
	}
	return nil
}

// writeUsageRecord appends one JSON line to usage.jsonl in CWD.
// Created with 0644 if absent.
func writeUsageRecord(rec usageRecord) error {
	path := usageLogPath
	if !filepath.IsAbs(path) {
		// Anchor at CWD; resolving here means tests that chdir into
		// a tmp dir get an isolated log without touching the repo.
		path = filepath.Join(".", path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	return enc.Encode(rec)
}

// Compile-time check that AnthropicMaterializer satisfies the
// Materializer interface. If the interface drifts, this fails at build
// time rather than at the first orchestrator call site.
var _ Materializer = (*AnthropicMaterializer)(nil)
