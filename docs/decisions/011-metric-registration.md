# ADR-011 — Metric registration via init()

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 3 — Metrics infrastructure
**Supersedes:** —

## Context

Issue #4 requires a `Metric` interface in `internal/metrics/` such that
adding a new metric is "ONE new Go file, auto-registered." Three
realistic options:

1. `init()` registration into a package-level registry.
2. Explicit `metrics.Register(myMetric)` from `cmd/archmotif/metrics.go`.
3. Reflection over a known package (scan exported types implementing
   the interface).

## Decision

Use option 1: every built-in and future metric writes a single file
that ends with

```go
func init() { Register(MyMetric{}) }
```

The file lives in `internal/metrics/`. The runtime registry is a
`map[string]Metric` guarded by `sync.RWMutex`. Duplicate names panic at
init() — a build-time bug, not user input.

The runner (`metrics.Run`) reads from the registry; the CLI lists
registered metrics via `--list`. Adding `metric_zero.go` with a fresh
type and an init() call is the discipline check (issue #4 verify item):
no other files change, no test wiring required, the metric is
discoverable through `metrics.All()` immediately.

## Alternatives considered

- **Explicit registration in `main.go` or a setup function.** Forces
  every new metric to touch a second file. Issue #4 explicitly rules
  this out ("ONE new Go file").
- **Reflection over the metrics package.** Heavier, requires
  `go/ast` or build-time codegen, fragile across refactors. The
  init()-registry pattern is the canonical Go plugin idiom (used by
  database/sql, image/png, image/gif, …) and reads more clearly.
- **Constructor map keyed by name string.** Same as init() but more
  verbose and offers no advantage when each metric is a single
  zero-arg struct.

## Consequences

- Registration runs at package import time. The CLI subcommand imports
  `internal/metrics`, which transitively pulls every metric file via
  the package-level identifier resolution Go does to satisfy the
  init()s. No import cycle risk.
- Tests must not assume an empty registry — every test process starts
  with all built-ins registered. The `reset()` helper is package-private
  and only used inside the metrics test files when needed (none of the
  Stage-3 tests need it).
- Duplicate `Name()` returns from two files cause a panic at startup.
  This is intentional: silent shadowing would be far harder to debug.
- Metric authors who want runtime-tunable knobs (motif size cap,
  modularity resolution) build a typed struct and either expose flags
  through the CLI's `metrics.go` or rely on `Configurable()` defaults.
  ADR-013 covers the motif knob.
