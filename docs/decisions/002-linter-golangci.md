# ADR-002 — Linter: golangci-lint with curated default set

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 0 — Project foundations
**Supersedes:** —

## Context

ROADMAP Stage 0 requires a "sane golangci-lint config." We need
`make lint` to be useful both locally and in CI, without forcing every
contributor to install golangci-lint immediately.

## Decision

- Use `golangci-lint` as the canonical linter. Pin to v1.59.1 in CI.
- Ship a minimal `.golangci.yml` with `disable-all: true` and an
  explicit allow-list: `errcheck`, `gofmt`, `goimports`, `govet`,
  `ineffassign`, `misspell`, `revive`, `staticcheck`, `unused`.
- `make lint` runs `golangci-lint` if available, otherwise falls back
  to `go vet ./...`. CI is authoritative — local fallback exists only
  to keep the dev loop from breaking on a fresh checkout.

## Alternatives considered

- **`go vet` only.** Too weak — misses the import / unused-var /
  staticcheck class of issues we care about for a research codebase
  that will accumulate fast.
- **golangci-lint default linter set.** Default enables a handful of
  noisy linters (`gosimple`, etc.) that we may want, but pinning the
  enabled list explicitly makes config drift across versions visible
  and reviewable.

## Consequences

- CI is the source of truth for lint failures.
- Adding a linter is a one-line edit in `.golangci.yml` plus an ADR
  if it changes accepted code patterns materially.
- We don't yet enforce import groupings beyond `goimports` defaults;
  revisit if it becomes noisy.
