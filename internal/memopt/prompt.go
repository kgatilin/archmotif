package memopt

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed prompts/v1.tmpl
var promptFS embed.FS

// PromptVersion identifies the template version embedded in this
// build. The loop CLI (#38) records this string in the run log so a
// reviewer can correlate a patch with the prompt that produced it.
const PromptVersion = "v1"

// DefaultContextLimit is the bounded-batch size baked into the prompt
// when Contract.ContextLimit is zero. Twenty items per fetch keeps the
// response window predictable while leaving room for a typical orphan
// or flat-star batch (≤ 12 nodes) plus a margin for parents.
const DefaultContextLimit = 20

// RenderPrompt renders the v1 protocol prompt for the given contract.
// The contract is expected to have already passed Contract.Validate;
// a degenerate contract (empty Selected, empty AllowedOps) renders an
// empty-batch prompt that the loop CLI is responsible for not sending.
//
// The prompt is deterministic for a given contract: rendering twice
// with the same input yields byte-identical output. Selected order is
// preserved as supplied (the orchestrator decides batch order); the
// caller can pre-sort if a stable wire shape matters.
func RenderPrompt(c *Contract) (string, error) {
	if c == nil {
		return "", fmt.Errorf("memopt: nil contract")
	}
	view := *c
	if view.ContextLimit == 0 {
		view.ContextLimit = DefaultContextLimit
	}
	raw, err := promptFS.ReadFile("prompts/v1.tmpl")
	if err != nil {
		return "", fmt.Errorf("memopt: read embedded prompt: %w", err)
	}
	tmpl, err := template.New(PromptVersion).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("memopt: parse prompt: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return "", fmt.Errorf("memopt: execute prompt: %w", err)
	}
	return buf.String(), nil
}
