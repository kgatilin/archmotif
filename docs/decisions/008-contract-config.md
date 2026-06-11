# ADR-008 — Contract declaration: config-only via `.archmotif.yaml`

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 2 — Contract nodes
**Supersedes:** —

## Context

ROADMAP Stage 2 says contracts are user-declared via either
`.archmotif.yaml` or an optional `// archmotif:contract` comment
annotation. Issue #3 narrows this: "start config-only." We need a
concrete file format, a parser, and an identifier-resolution scheme
that maps `pkg/path.Name` strings onto loaded `*types.TypeName` objects.

## Decision

For Stage 2, contracts are declared exclusively in
`<module-root>/.archmotif.yaml`:

```yaml
contracts:
  - interface: example.com/store.UserStore
  - type: example.com/api.Request
```

Each entry is one of:

- `interface: <import-path>.<TypeName>` — the named type must resolve
  to a `*types.Interface` underlying.
- `type: <import-path>.<TypeName>` — any other named type
  (struct/alias). Underlying interface entries should be declared with
  `interface:` so we can validate the kind matches.

**Identifier format.** `<import-path>.<TypeName>` where the dot is the
*last* dot in the string — required because import paths frequently
contain dots (`example.com/foo`). `pkg/store.UserStore` is treated as
the import path `pkg/store` and the type name `UserStore`. Generics
parameters are not supported in v1; document and revisit if needed.

**Resolution.** The contract loader walks every loaded `*packages.Package`
and looks up `tn := pkg.Types.Scope().Lookup(name)` for matching
`pkg.PkgPath`. Resolution succeeds when both the path matches and the
object is a `*types.TypeName`. Unresolved entries are reported as
`Unresolved` warnings on the result, not as fatal errors — matching the
ADR-004 stance that partially-broken or partially-loaded modules still
produce useful output.

**Config file location.** Discovered at the *module root* of the loaded
packages (the directory of the package whose `Module.Dir` was selected
by `parser.Build`). Falls back to the loader's working directory when
no module is detected. This mirrors how `.golangci.yml`, `go.work`, etc.
are discovered.

**Comment annotations are deferred.** ROADMAP allows `// archmotif:contract`
markers. We do not parse comments in Stage 2. Adding them later is
non-breaking: the loader will collect both sources and union them.

**YAML library.** `gopkg.in/yaml.v3`. Standard pick for Go YAML;
already widely used in the Go ecosystem; no dependency conflict with
existing `golang.org/x/tools` and `gonum`. We use only the standard
`yaml:"..."` struct tags — no custom unmarshalers — to keep room for
the comment-annotation merge later.

## Alternatives considered

- **Comment-only declaration (`// archmotif:contract`).** ROADMAP mentions
  it but flags it optional. Comments are convenient for single-file
  contracts but force the user to touch source files for every contract
  declaration; cross-package contracts (ROADMAP example: `pkg/api.Request`)
  are awkward without a config file. Defer to a follow-up.
- **TOML or JSON config.** Both work; YAML matches the Go-tooling norm
  (`golangci`, `gh actions`, `kustomize`) and reads better for nested
  lists. Rejected.
- **Configuration via flags (`--contract pkg/store.UserStore`).** OK
  for ad-hoc runs but scales badly past two contracts and isn't
  shareable. Rejected as primary; may add as a supplement.
- **Reflection-style identifier syntax (`*pkg/store.UserStore`).** The
  receiver-pointer prefix has no meaning at the contract-declaration
  level — contracts are types, not values. Rejected.

## Consequences

- The contract loader has one input (YAML file) and one resolution
  pass over loaded packages. Stage 4+ adds comment merging without
  changing the resolver.
- Adding `gopkg.in/yaml.v3` is the only new top-level dep introduced
  by Stage 2 (beyond what Stage 1 already pulls).
- The contract config file lives next to the user's `go.mod`; check it
  into version control alongside the project.
- Identifier resolution is purely lexical at config-load time — we do
  not chase aliases, generics, or instantiated types. ADRs in later
  stages can extend this.
- An entry that fails to resolve becomes an `Unresolved` warning, not a
  build error. Stage 8 (verification) is the right place to make
  unresolved contracts a hard failure.
