# Skeleton format

The skeleton format is the contract between Stage 6 (renderer),
Stage 7 (LLM consumer), and Stage 8 (verifier). See
[ADR-016](./decisions/016-skeleton-format.md) for the why.

Each proposal ships **two files**, sharing one `proposal-id`:

- `<proposal-id>.skeleton.go` — annotated Go, for human review and
  LLM input.
- `<proposal-id>.skeleton.yaml` — structured target subgraph, for
  the verifier.

A worked example lives at
[`testdata/skeletons/motif-001.skeleton.go`](../testdata/skeletons/motif-001.skeleton.go)
and
[`testdata/skeletons/motif-001.skeleton.yaml`](../testdata/skeletons/motif-001.skeleton.yaml).

## 1. Annotated Go grammar

```go
// PROPOSAL: <one-line description>
// AFFECTED: <pkg/file>, <pkg/file>, ...
//
// ROLE Iface : interface
type Iface interface {
    Method(Param ParamType) RetType
}

// ROLE Impl : struct
type Impl struct{}

// ROLE Method : method on Impl realising Iface.Method
func (i *Impl) Method(Param ParamType) RetType { return RetType{} }

// SAMPLES:
//   Iface=UserStore   Impl=SQLUserStore   Method=Find
//   Iface=OrderStore  Impl=SQLOrderStore  Method=Find
//   Iface=ProductStore Impl=PgProductStore Method=Lookup
```

Rules:

| Rule | Detail |
|------|--------|
| Header | First two comment lines are `// PROPOSAL: …` and `// AFFECTED: …`. |
| Role declaration | `// ROLE <Name> : <kind>` above the first declaration introducing the role. |
| Role kinds | `interface | struct | method | function | field | type`. |
| Placeholder identifiers | Roles appear in code as bare Go identifiers (e.g. `Iface`). The angle-bracket form `<Iface>` is the *display* form for docs and LLM prompts; the on-disk file is valid Go so `go/parser` accepts it. |
| Samples block | `// SAMPLES:` followed by 3–5 indented lines of `Role=Value Role=Value …`. |
| Validity | The file parses with `go/parser.ParseFile(parser.ParseComments)` without errors. |

## 2. YAML companion grammar

```yaml
proposal_id: <id>
description: <one-line description>
affected:
  - <pkg/file>
target_subgraph:
  roles:
    - id: <Role>
      kind: <interface|struct|method|function|field|type>
      # Optional structural fields per kind:
      methods:                      # for kind=interface
        - name_role: <Role>
          params:
            - {role: <Role>, type_role: <Role>}
          return_role: <Role>
      receiver_role: <Role>         # for kind=method
      realises:                     # for kind=method realising an interface method
        role: <Role>
        method: <Role>
  edges:
    - {from: <Role>, to: <Role>, kind: <edge-kind>}
samples:
  - {<Role>: <Value>, …}
```

Rules:

| Rule | Detail |
|------|--------|
| `proposal_id` | Stable id, matches the filename stem. |
| `roles[*].id` | One-to-one with role names in the `.skeleton.go` `ROLE` comments. |
| `roles[*].kind` | Same enum as the Go file. |
| `edges[*].kind` | One of the [`EdgeKind`](../internal/graph/kinds.go) strings: `contains`, `implements`, `embeds`, `calls`, `callsFrom`, `references`, `dependsOn`, `returns`, `usesType`. |
| `samples` | 3–5 entries, each a `Role: Value` map mirroring the Go SAMPLES block. |

## 3. Reading order

- Stage 7 (LLM): reads `.skeleton.go`, ignores `.skeleton.yaml`.
- Stage 8 (verifier): reads `.skeleton.yaml`, ignores `.skeleton.go`.
- Stage 6 (renderer): writes both atomically from one in-memory model.

## 4. Validation

- Annotated Go: `go/parser.ParseFile` must succeed. A smoke test
  enforcing this lives at
  [`internal/skeleton/format_test.go`](../internal/skeleton/format_test.go).
- YAML: schema-conform parsing happens in Stage 8 — out of scope here.
