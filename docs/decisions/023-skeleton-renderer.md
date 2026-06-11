# ADR-023: Skeleton renderer (Stage 6)

Status: accepted (2026-05-05)

Supersedes: none.
Related: ADR-016 (skeleton format), ADR-019 (v1 transformation rule),
ADR-022 (Stage 5 implementation).

## Context

ADR-016 pins the on-disk skeleton format: an annotated Go file plus a
YAML companion, generated per accepted Proposal. Stage 5 (ADR-022)
produces the `propose.Proposal` values; Stage 7 will turn skeletons
into LLM prompts; Stage 8 will verify the rewritten code. Stage 6 is
the renderer in between — pure function from `*propose.Proposal` to
`(go bytes, yaml bytes)`.

We need to decide:

1. Where the renderer lives and what its API is.
2. How role placeholders are emitted (bare identifiers vs angle
   brackets).
3. How the Go and YAML files stay in sync with the worked-example
   fixture (`testdata/skeletons/motif-001.skeleton.{go,yaml}`).
4. How the renderer copes with proposer outputs that don't match the
   motif-001 worked example one-for-one (e.g. fewer than 3 samples,
   missing signature roles).

## Decision

**Package layout.** The renderer is `internal/skeleton`. Two entry
points:

- `RenderGo(p *propose.Proposal) ([]byte, error)`
- `RenderYAML(p *propose.Proposal) ([]byte, error)`

A new CLI verb `archmotif skeleton <path> [--id=<id>] [--out=<dir>]`
drives the same Stage 3 → 4 → 5 pipeline as `archmotif propose` and
writes one `<id>.skeleton.go` + `<id>.skeleton.yaml` pair per accepted
proposal under `--out=` (default `./skeletons/`). With `--id`, only
that proposal is rendered; without it, all are.

**Bare identifiers in Go.** ADR-016 allows two display forms for role
placeholders: bare identifiers (`Iface`) or angle-bracket
(`<Iface>`). The on-disk Go file uses bare identifiers exclusively,
because `go/parser.ParseFile` must accept the file unchanged (Stage
8's verifier and any downstream tooling load skeletons via the same
parser). The angle-bracket form is reconstructed from the `// ROLE`
comment cluster at prompt-build time (Stage 7) — it is not stored on
disk. This matches the worked-example fixture pinned by ADR-016 and
the format guard in `internal/skeleton/format_test.go`.

**Hand-emitted YAML.** The companion YAML is hand-written into a
`bytes.Buffer` rather than encoded with `yaml.v3`. Two reasons:

1. Field ordering, comment placement, and inline-flow style are part
   of the format pin — they need to survive a byte-for-byte
   comparison against `motif-001.skeleton.yaml`. `yaml.v3`'s encoder
   makes no such guarantees.
2. The renderer aligns sample rows visually so the Go SAMPLES block
   and the YAML samples list have matching column shape. That's
   easier with explicit `fmt.Fprintf` than with a Marshal hook.

Round-trip safety is enforced by `TestRenderYAMLRoundTrip` — the
rendered YAML must decode back to the same target_subgraph shape via
`yaml.Unmarshal`.

**Sample count fallback.** ADR-016 requires the SAMPLES block to
have 3..5 entries. The Stage-5 proposer normally emits ≥3 (the
extract-interface trigger threshold). When it emits fewer, the
renderer pads by repeating the last sample; when it emits more, it
truncates to 5. This keeps the on-disk format predictable without
forcing the proposer to do format-aware work. Pure renderer-side
fallback; the verifier doesn't rely on padding semantics.

**Signature-role fallback.** The motif-001 grammar names five roles
(Iface, Impl, Method, Param, ParamType, RetType). The current
extract_interface rule emits only the first three (Iface, Impl,
Method); ADR-019 anticipated this. The renderer falls back to
canonical names `Param` / `ParamType` / `RetType` when those roles
aren't declared, and emits stub `type ParamType struct{}` /
`type RetType struct{}` declarations so the file remains
self-contained for `go/parser`.

**Sample lookup.** The Stage-5 proposer puts both raw graph IDs (key
= role name) and human-friendly names (key = role + "Name") in the
sample map (per ADR-019 §"Samples"). The renderer prefers the
human-friendly form (`IfaceName` over `Iface`) so the rendered
SAMPLES block reads as concrete code rather than internal node IDs.

**Validation.** The renderer rejects: nil Proposal, empty ID, no
roles, no samples. Beyond that it trusts the proposer — graph and
metric validation belong to Stages 3–5.

## Consequences

- The renderer is a pure function: same Proposal → same bytes. Tests
  pin byte-for-byte equality against the motif-001 fixture so
  unintended drift in either direction is caught.
- `go/parser` accepts every rendered Go file. Stage 7's prompt
  builder and Stage 8's verifier can both consume rendered skeletons
  without preprocessing.
- Round-trip property on YAML means Stage 8 can decode the companion
  back into a typed structure for verification.
- Padding/truncation hides upstream proposer quirks from the format
  consumers. If we want stricter contracts later (e.g. fail when
  count < 3) we move the check from the renderer to the proposer or
  a separate gate.
- Adding a new transformation rule (Stage 5 extension) only requires
  the rule to populate `Proposal.TargetSubgraph` and `Samples`; the
  renderer needs no rule-specific code. New role kinds (beyond
  interface/struct/method) plug in via `classifyRole` in
  `render.go`.

## Alternatives considered

- **Use `yaml.v3` Marshal for the YAML.** Rejected: no control over
  field order or inline-flow style, so the byte-for-byte fixture pin
  becomes impossible.
- **Use angle-bracket identifiers on disk.** Rejected: breaks
  `go/parser`. ADR-016 already documented this trade-off; ADR-023
  just records the implementation choice.
- **Keep a separate per-rule renderer.** Rejected: motif-001 is the
  only Stage 5 rule (ADR-019); a rule-aware classifier inside one
  renderer is simpler than a registry of renderers, and keeps the
  format pin in one place.
- **Have the proposer emit format-ready samples.** Rejected: the
  proposer's contract (ADR-022) is "produce structurally correct
  proposals". Format-side concerns (padding, alignment, column
  widths) belong to the renderer.
