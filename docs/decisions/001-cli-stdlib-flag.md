# ADR-001 — CLI: stdlib `flag` (not Cobra) for Stage 0

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 0 — Project foundations
**Supersedes:** —

## Context

ROADMAP Stage 0 leaves the CLI library open: cobra vs stdlib `flag`.
At Stage 0 the CLI surface is one binary with `--version` and
`--help`. Future stages add subcommands (`graph`, `contracts`,
`metrics`, `anomalies`, `propose`, …) — clearly subcommand-shaped.

## Decision

Use the standard library's `flag` package for Stage 0. Subcommands
will be dispatched manually (`os.Args[1]` switch) until the surface
grows enough to justify a router. Revisit at the first stage that
ships ≥3 real subcommands; expected: Stage 3 (metrics) or Stage 4
(anomalies). At that point either keep a hand-rolled dispatcher or
switch to `cobra` / `urfave/cli`.

## Alternatives considered

- **Cobra now.** Mature, ergonomic for many subcommands, generates
  help. Rejected for now because it's a heavy dependency for a
  Stage 0 scaffold with two flags and no real subcommands. Also,
  introducing it later is mechanical — `flag` doesn't paint us into
  a corner.
- **`urfave/cli`.** Lighter than Cobra but still a dep we don't need
  yet.

## Consequences

- Zero runtime dependencies at Stage 0. `go.sum` stays empty until a
  stage forces a real dep (likely a graph library at Stage 1 or a
  numerics library at Stage 3).
- Subcommand routing is hand-rolled until revisited. Acceptable; the
  current `run()` function exits cleanly with a "not implemented"
  message for any non-flag arg.
- When we revisit: if we move to Cobra, the test surface in
  `cmd/archmotif/main_test.go` may need rewriting against Cobra's
  command tree. Note this in the migration ADR.
