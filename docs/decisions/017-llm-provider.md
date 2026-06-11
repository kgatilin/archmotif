# ADR-017 — LLM provider, prompt strategy, output format, cost tracking

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 7 — LLM materialization (prep)
**Supersedes:** —

## Context

Stage 7 turns a structural skeleton (Stage 6) into actual Go code by
calling an LLM. Three sub-decisions block any concrete implementation
and are independent of Stages 4–6:

1. **Which provider.** Closed (Anthropic, OpenAI), open-weights
   (Llama, DeepSeek), or multi.
2. **What the LLM returns.** Whole files, a per-file replacement set,
   or a unified diff.
3. **How we observe and bound cost.** Tokens and dollars per call must
   be inspectable; runaway calls must be debuggable post-hoc.

The roadmap (Stage 7) explicitly asks for the LLM call to sit behind
an interface so the provider is swappable. It does **not** ask for a
multi-provider abstraction beyond that interface — that's deferred
until a second provider is actually needed.

This ADR commits the contract so the implementation in Stage 7 is a
fill-in exercise and so Stage 8 (verification) and Stage 9
(end-to-end) can wire against a stable surface today.

## Decision

### Provider

Default **Anthropic Claude**, model `claude-sonnet-4-6`. A `--model`
flag overrides to `claude-opus-4-7` for hard cases. The deskd runtime
already wires `ANTHROPIC_API_KEY` in this environment, so no new
secrets plumbing.

The Stage 7 implementation uses Anthropic's HTTP API directly (no SDK
import in v1) to keep the dependency footprint minimal and avoid
pinning to an SDK release ahead of need. A future ADR can revisit if
prompt caching, the Tools API, or batch endpoints become load-bearing.

### Output format

The LLM returns **one unified diff** wrapped in a single fenced
` ```diff ` block, nothing before or after. Diff paths are relative
to module root and the diff applies with `git apply` from the repo
root.

Failure modes are surfaced as distinct, named errors (Stage 7 work,
not this ADR's scope):

- **Parse fail** — no fenced diff block, or content outside the fence.
- **Apply fail** — `git apply --check` rejects the patch.
- **Build fail** — the patched tree fails `go build ./...`.

We do **not** auto-retry on any of these. Auto-retry burns tokens,
hides bugs, and produces non-reproducible runs. The orchestrator
fails loud and writes the diff to disk for human inspection.

### Prompt template

The template lives at `internal/llm/prompts/v1.tmpl` and is rendered
with `text/template`. It is versioned by filename (`v1.tmpl`,
`v2.tmpl`, …) rather than embedded mutable content, so a prompt
change is a code change visible in `git log`.

Inputs to the template (the `Proposal` struct, defined in
`internal/llm/materializer.go`):

- `Description` — human-readable proposal text from Stage 5.
- `SkeletonGo` — annotated Go skeleton from Stage 6.
- `SkeletonYAML` — companion YAML (Stage 8 verifier consumes this; the
  template currently does not emit it but the field travels with the
  proposal so verification can run against the same instance).
- `Samples` — list of role → existing instance name maps.
- `AffectedFiles` — `path → original contents` map for the regions the
  refactor will touch.

The template is intentionally short and structural. Tone-setting,
chain-of-thought scaffolding, and few-shot exemplars are deferred
until we have empirical data on quality from Stage 7.

### Cost / observability

Per-call line written to `usage.jsonl` at repo root (gitignored;
matches the `*.local.yaml` / `.archmotif/` convention):

```json
{"proposal_id":"motif-001","model":"claude-sonnet-4-6","input_tokens":4123,"output_tokens":812,"cost_usd":0.0246,"duration_ms":11320,"ts":"2026-05-05T12:34:56Z"}
```

`cost_usd` is computed by `internal/llm/pricing.Cost(model, in, out)`
from per-1M-token constants in the same file. The constants are
hard-coded today and are an obvious place to wire a config file or
remote pricing fetch later — but that's premature: Anthropic's
pricing has been stable across the 4.x series and a code change is
visible in review.

## Alternatives considered

- **OpenAI GPT-4o.** Comparable code-gen quality. Adding it costs a
  second integration without paying for one yet. Deferred until a
  concrete reason to dual-provider appears.
- **Local Llama / DeepSeek.** Removes the API-key dependency but
  current open-weights models lag closed models on multi-file
  structural refactors and require GPU infrastructure not present in
  the deskd runtime. Reconsider when local quality closes the gap or
  when we hit a hard constraint (privacy, cost, latency).
- **Whole-file output.** Easiest to validate (parse each as Go) but
  the LLM regularly drops unchanged regions when the file is large,
  silently corrupting code. Diffs fail loud.
- **Function-level patch (`old → new` blocks).** A middle ground used
  by some refactor tools. Requires either a custom parser or a
  second LLM pass to align blocks; unified diff is the lingua franca
  with mature `git apply` tooling.
- **Multi-block diff (one per file).** Splits naturally per file but
  multiplies parse failure surfaces. One fenced diff is simpler and
  `git apply` happily handles multi-file diffs in a single hunk
  stream.
- **Auto-retry on parse / apply failure.** Tempting and almost
  always wrong: the second call burns tokens producing the same
  failure for non-deterministic reasons, and successful retries
  paper over prompt bugs. Surface failures, fix the prompt, re-run.
- **Tools API for structured output.** Anthropic supports a
  forced-JSON / tool-call shape that would eliminate the parse step.
  Worth evaluating in Stage 7 once we have baseline numbers; not
  blocking the interface here. The interface accepts `[]byte` for
  `Branch.Diff` so swapping to a tool-call response that decodes to a
  diff is non-breaking.
- **Embedded prompt as a Go string constant.** Rejected because
  prompt-as-file makes diffs reviewable and tests can read the same
  bytes the runtime uses. The template is loaded with `embed.FS`
  in Stage 7.

## Consequences

- The `Materializer` interface in `internal/llm/materializer.go` is
  the swap point: any future provider implements it. The orchestrator
  (Stage 9) takes a `Materializer` and never imports
  `internal/llm/anthropic.go` directly.
- Stage 7's HTTP work is bounded: render prompt, POST to
  `https://api.anthropic.com/v1/messages`, extract text, parse diff,
  write usage log. No SDK import, no streaming, no retries.
- Stage 8 (verification) consumes the post-apply tree, not the LLM
  output. The verifier's input is "graph of code after `git apply`"
  vs "skeleton YAML"; the LLM is opaque to it.
- New providers cost an ADR + an additional file in `internal/llm/`.
  No interface change. If a second provider needs a richer
  `Proposal` (e.g. system-prompt overrides), bump the proposal struct,
  not the interface.
- `usage.jsonl` is intentionally append-only and unstructured at the
  file level. A future Stage adds a `usage` subcommand to summarise
  it; for v1 the file is grep-friendly.
- Pricing constants in `internal/llm/pricing.go` are a known follow-
  up: when Anthropic publishes the next price revision, we either
  bump the constants in a focused PR or wire a config override.
  Either is fine; both are out of scope for this issue.
