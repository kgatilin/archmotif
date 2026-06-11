# ADR-016 — Skeleton format: annotated Go + YAML companion

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 6 — Structural skeleton rendering (contract for Stages 6/7/8)
**Supersedes:** —

## Context

Stage 6 (renderer), Stage 7 (LLM consumer), and Stage 8 (verifier) all
read the same artefact: a *skeleton* describing the target subgraph
of a proposed transformation. Pinning the format now lets the three
stages be built against the same spec without round-tripping or
mutual blocking.

`docs/concepts.md` §6 already shows annotated Go syntax with
`<Placeholder>` names. Two consumer needs constrain the format:

- **LLM (Stage 7)** wants something it can read alongside the original
  code. Go-shaped is best — the model already parses Go fluently and
  fills in placeholders inline. Annotated Go with `<Role>` placeholders
  and inline `// SAMPLES:` lines is enough.
- **Verifier (Stage 8)** wants a machine-readable subgraph spec with
  role↔node mapping, edge kinds, and node-kind constraints. YAML is
  cheap, obvious, and lets the verifier read the target subgraph
  without re-implementing a Go-syntax-aware role extractor.

Trying to force one format to do both is a false economy: parsing
annotated Go for the verifier means writing a placeholder-tolerant
Go parser that extracts role↔kind↔edge tuples; serialising YAML for
the LLM costs context tokens while losing the in-language reading
the LLM is good at. Two files generated together is simpler than
either union.

ROADMAP Stage 6 lists the format as an open question with the
current default "annotated Go for the LLM; YAML companion for the
verifier". This ADR commits to that default and pins the grammar.

## Decision

Stage 6 emits **two files per proposal**, both pinned to a single
`proposal-id`:

- `<proposal-id>.skeleton.go` — annotated Go, for human review and
  LLM input.
- `<proposal-id>.skeleton.yaml` — structured target subgraph, for
  the verifier.

### Annotated Go grammar

```go
// PROPOSAL: <one-line description>
// AFFECTED: <pkg/file>, <pkg/file>, ...
//
// ROLE Iface : interface
type <Iface> interface {
    <Method>(<Param> <ParamType>) <RetType>
}

// ROLE Impl : struct
type <Impl> struct { ... }

// ROLE Method : method on Impl realising Iface.Method
func (i *<Impl>) <Method>(<Param> <ParamType>) <RetType> { ... }

// SAMPLES:
//   Iface=UserStore   Impl=SQLUserStore   Method=Find
//   Iface=OrderStore  Impl=SQLOrderStore  Method=Find
//   Iface=ProductStore Impl=PgProductStore Method=Lookup
```

Rules:

- Placeholders use `<Name>` form (matches `docs/concepts.md` §6).
- Each placeholder has exactly one `// ROLE Name : kind` comment line
  above the first declaration that introduces it. `kind` ∈
  `{interface, struct, method, function, field, type}`.
- A `// SAMPLES:` block is mandatory; minimum 3, maximum 5 instances.
  Sample lines are `Role=Value Role=Value …`, whitespace-separated.
- The file parses with `go/parser` *as Go source* — the placeholder
  identifiers (`<Iface>`, `<Method>` etc.) are not directly parseable
  Go, so the renderer emits them as ordinary identifiers stripped of
  the angle brackets in the on-disk Go file (e.g. `Iface`, `Method`),
  while the `ROLE` comments mark which identifiers are roles. The
  angle-bracket form shown in `concepts.md` §6 is the *display* form
  used in documentation and the LLM prompt; the file on disk is
  syntactically valid Go.

The display-vs-on-disk split keeps two properties at once:

- `go/parser` accepts the file (Stage 8 and `gofmt`-style tools work).
- The LLM prompt presents the angle-bracket display form by
  re-decorating identifiers from the `ROLE` comments at prompt-build
  time (Stage 7's concern).

### YAML companion grammar

```yaml
proposal_id: motif-001
description: extract interface from repeated motif (size 3)
affected:
  - pkg/store/user.go
  - pkg/store/order.go
  - pkg/store/product.go
target_subgraph:
  roles:
    - id: Iface
      kind: interface
      methods:
        - name_role: Method
          params:
            - {role: Param, type_role: ParamType}
          return_role: RetType
    - id: Impl
      kind: struct
    - id: Method
      kind: method
      receiver_role: Impl
      realises: {role: Iface, method: Method}
  edges:
    - {from: Impl, to: Iface, kind: implements}
    - {from: Method, to: Impl, kind: contains}
samples:
  - {Iface: UserStore,    Impl: SQLUserStore, Method: Find}
  - {Iface: OrderStore,   Impl: SQLOrderStore, Method: Find}
  - {Iface: ProductStore, Impl: PgProductStore, Method: Lookup}
```

Rules:

- `roles[*].id` matches the role names in the Go file's `ROLE`
  comments 1:1.
- `roles[*].kind` constrains the node-kind candidates the verifier
  considers and uses one of the `NodeKind` strings from
  `internal/graph/kinds.go` (`interface` is a `Type` with the
  interface tag — see Consequences for the resolution).
- `target_subgraph.edges[*].kind` uses one of the `EdgeKind` strings
  from `internal/graph/kinds.go` (`implements`, `contains`, `calls`,
  `embeds`, `callsFrom`, `references`, `dependsOn`, `returns`,
  `usesType`).
- `samples` mirrors the annotated-Go SAMPLES block; same content,
  structured for machine consumption.

### Pinning

ADR-016 freezes:

- Filename pattern: `<proposal-id>.skeleton.{go,yaml}`.
- Comment markers: `// PROPOSAL:`, `// AFFECTED:`, `// ROLE`, `// SAMPLES:`.
- Role-kind enum: `{interface, struct, method, function, field, type}`.
- YAML top-level keys: `proposal_id`, `description`, `affected`,
  `target_subgraph`, `samples`.

Stage 6/7/8 cite this ADR as the input contract. Changes to the
format require a new ADR superseding this one.

## Alternatives considered

- **One format only — annotated Go.** Pros: one file. Cons: forces
  the verifier to re-parse Go with placeholder tolerance and infer
  edges from comment scaffolding. Verifier becomes brittle: a
  reordered method comment could change the inferred subgraph.
  Rejected.
- **One format only — YAML.** Pros: one machine-readable file. Cons:
  the LLM works best with code-shaped input. Rendering YAML to a
  faux-Go template inside the prompt at Stage 7 adds a serialisation
  step in the hot path and loses the Go-syntax cues the LLM uses to
  fill names sensibly. Rejected.
- **Annotated Go with raw `<Placeholder>` syntax in the on-disk
  file.** Pros: matches `concepts.md` §6 display form exactly. Cons:
  doesn't parse with `go/parser`; Stage 8 verification depends on
  parsing the file when checking placeholder mapping; CI's `gofmt`
  hooks reject it. Rejected — the angle-bracket display form is
  generated for docs/LLM prompt only.
- **Drop the SAMPLES block from the Go file (keep only in YAML).**
  Pros: smaller Go file. Cons: human reviewers and the LLM both
  benefit from seeing samples next to the structure. Cheap to
  duplicate; keep both.
- **JSON instead of YAML.** Pros: stdlib. Cons: hand-editing for
  fixtures is awful (no comments, trailing commas). The project
  already uses YAML for `.archmotif.yaml` (Stage 2) — pick the
  consistent option.

## Consequences

- Stage 6 ships a renderer that emits both files atomically per
  proposal. The renderer's input is `(proposal-id, target-subgraph,
  sample-instances)`; both files are derivable from the same
  in-memory representation.
- Stage 7's prompt template includes the `.skeleton.go` file
  verbatim plus an angle-bracket-decorated rendering for visual
  clarity. The YAML stays out of the prompt.
- Stage 8's verifier reads only the `.skeleton.yaml` file. Edge
  kinds map directly to `EdgeKind` strings; node kinds map to
  `NodeKind` with one wrinkle (below).
- **Node-kind reconciliation.** The skeleton role-kind enum is
  `{interface, struct, method, function, field, type}`; the graph
  uses `Type` for both struct and interface nodes (interface-vs-
  struct is an attribute, see ADR-009 / `internal/graph/kinds.go`).
  Stage 8 maps role-kind `interface` and `struct` to graph
  `NodeKind = type` plus the appropriate type-tag attribute,
  `method` → `NodeMethod`, etc. The role-kind names are richer
  than `NodeKind` because the LLM benefits from `interface` being
  visible in the YAML.
- **Edge kinds use existing graph kinds.** `internal/graph/kinds.go`
  ships `contains`, `implements`, `embeds`, `calls`, `callsFrom`,
  `references`, `dependsOn`, `returns`, `usesType`. The motif-001 worked example uses
  `implements` and `contains` only. No new edge kind is required for
  the extract-interface rule.
- **Sample count.** Mandatory 3 ≤ samples ≤ 5. Less than 3 means the
  motif redundancy metric (ADR-013) wouldn't have flagged it; more
  than 5 wastes prompt budget. The renderer truncates by lowest
  metric-detail rank.
- **Adding a new role-kind** is a non-breaking ADR-bumping change
  (add to the enum, document in §grammar, no file-format change).
  Adding a new edge kind requires extending
  `internal/graph/kinds.go` first; that's a Stage-1 change, not a
  skeleton-format change.
- This ADR is purely spec; no code lands beyond the worked example
  fixture and a `go/parser` smoke test in
  `internal/skeleton/format_test.go`. Stage 6 builds the renderer
  against this spec.
