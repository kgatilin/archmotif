# ADR-024 — Stage 7 LLM materialization implementation

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 7 — LLM materialization
**Supersedes:** —
**Refines:** ADR-017

## Context

ADR-017 picked the provider (Anthropic), output format (one fenced
unified diff), and observability shape (`usage.jsonl`). It deliberately
left the implementation details for Stage 7 once the wiring (interface,
Proposal, Branch, prompt template, pricing constants) had landed.
This ADR records the decisions made when filling in
`AnthropicMaterializer.Apply` and the surrounding `archmotif refactor`
CLI command (issue #8).

## Decisions

### HTTP-direct, no SDK

We POST to `https://api.anthropic.com/v1/messages` with `net/http`
directly. No `github.com/anthropics/anthropic-sdk-go` import. Reasons:

- Keeps the dependency footprint small. The whole call is ~50 lines —
  marshal a request, set two headers, decode the response.
- No SDK release pin. The Anthropic API version we depend on is
  stable (`anthropic-version: 2023-06-01`) and the wire shape we use
  (`messages` endpoint, text content blocks) hasn't changed in years.
- An SDK would buy us streaming, the Tools API, batch endpoints, and
  retry helpers — none of which v1 needs (ADR-017 pins "no auto-retry"
  and the prompt is single-turn).

If a future stage needs prompt caching, tool-use forced-JSON, or batch
calls, that ADR can revisit. The interface (`Materializer`) is the swap
point and absorbs the change without touching the orchestrator.

### Branch name format

Branches are named `archmotif/refactor/<proposal-id>`. The slash in
the prefix gives `git branch -a` natural grouping (all archmotif
branches collapse under one node in clients that render the slashes).
The proposal ID is already required to be unique within a run
(ADR-022) and is short and slug-friendly (`motif-001`,
`extract_interface-foo`), so it doubles as a branch suffix without
sanitisation.

We **do not** auto-overwrite an existing branch. If the target branch
already exists, `Apply` returns `ErrBranchExists`. The user can delete
or rename and re-run. Auto-overwriting destroys local edits and makes
re-runs of `archmotif refactor` non-idempotent in a confusing way.

### No retry on bad output

ADR-017 already commits to "no auto-retry on parse / apply / build
failure". This ADR upgrades that to concrete error sentinels so the
orchestrator (Stage 9) and the CLI can distinguish failure modes via
`errors.Is`:

- `ErrNoFencedDiff` — response had no ```` ```diff ```` block.
- `ErrMultipleFencedDiffs` — response had more than one ```` ```diff ```` block.
- `ErrEmptyResponse` — 200 OK but no text content.
- `ErrBranchExists` — target branch is occupied; refusing to overwrite.
- `*ApplyCheckError` — `git apply --check` rejected the diff; carries
  captured stderr for human inspection.

Each error fails the run with exit 1 and a human-readable message.
We never silently retry, never silently swallow.

### `usage.jsonl` schema

One JSON object per line, appended to `usage.jsonl` at CWD. Schema:

```json
{
  "proposal_id": "motif-001",
  "model": "claude-sonnet-4-6",
  "input_tokens": 4123,
  "output_tokens": 812,
  "cost_usd": 0.0246,
  "duration_ms": 11320,
  "ts": "2026-05-05T12:34:56Z"
}
```

Fields:

- `proposal_id` — the `Proposal.ID` passed to `Apply`.
- `model` — the model actually called (after `Proposal.Model` override).
- `input_tokens`, `output_tokens` — verbatim from the Anthropic
  response's `usage` block.
- `cost_usd` — `pricing.Cost(model, in, out)`. Zero for unknown
  models (per ADR-017's "unknown = 0, log warning" contract; the
  warning lands in stderr at call time).
- `duration_ms` — wall-clock from request send to response read end.
  Includes network + LLM compute. Useful for the latency-vs-cost
  tradeoff a future `archmotif usage` summary would surface.
- `ts` — RFC3339 UTC timestamp captured at call completion.

The file is gitignored (added to `.gitignore` in this PR). A usage-log
write failure does **not** fail the refactor — the branch is already
applied at that point and losing one line of telemetry is preferable
to rolling back a successful diff. The failure is logged to stderr.

### CLI surface

```
archmotif refactor [flags] <path>
  --id=<proposal-id>   required
  --model=...          override default Sonnet
  --dry-run            render prompt; do not call API
  --tests              include _test.go in upstream pipeline
  --pattern=...        go/packages pattern
```

`--dry-run` exists so a user (or CI) can audit the exact prompt
before paying for tokens. The dry-run output is the bytes the runtime
would send.

The CLI re-runs the full Stage 3 → 4 → 5 pipeline on every invocation
to find the proposal by ID. This is wasteful on large repos but keeps
the CLI stateless. A future stage can add a `--proposals=path/to.json`
short-circuit if startup latency matters.

## Alternatives considered

- **SDK import.** Rejected; see above.
- **Retry on transient 5xx.** Tempting and might still be right —
  Anthropic's network errors are real. Deferred until we observe one
  in practice; trivially additive to the HTTP layer.
- **Tools API (forced JSON).** Removes the parse step entirely. Worth
  evaluating in a v2 prompt template once Stage 7 has baseline numbers.
  Non-breaking: the `Materializer` interface returns `Branch.Diff
  []byte` regardless of how the LLM produced the bytes.
- **Branch name `refactor/<id>` (no archmotif/ prefix).** Cleaner but
  collides with users' own refactor branches. The prefix is cheap.
- **Stream the response.** No streaming until a refactor's response
  exceeds the `defaultMaxTokens` cap and the latency-to-first-token
  becomes load-bearing. Not v1.

## Consequences

- Stage 9 (end-to-end orchestrator) takes the `Materializer` interface;
  no change required from this ADR.
- Stage 8 (verifier) consumes the post-apply tree, not the LLM output —
  unchanged from ADR-017.
- The `usage.jsonl` file is human-readable today and machine-summarised
  later. A future `archmotif usage` subcommand is the obvious follow-up.
- Adding a second provider (OpenAI, local) costs one new file in
  `internal/llm/` and an ADR; no interface change.
