# ADR-018 — Subgraph isomorphism: role-hinted backtracking, strict by default

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 8 — Verification linter
**Supersedes:** —

## Context

Stage 8 closes the archmotif loop: given a target subgraph `T` (the
proposal skeleton from Stage 6) and a typed graph `G_new` built from
the LLM-produced code (Stage 7), decide whether `G_new` contains a
subgraph isomorphic to `T` under the role mapping declared in `T`. A
match exits 0; a mismatch must surface a *role-named* diagnostic
(file:line where possible) so the operator and downstream automation
can act on it.

Three independent decisions were on the table:

1. **Algorithm.** Generic VF2/VF3, color refinement (1-WL), graph
   edit distance, or custom role-hinted backtracking.
2. **Match strictness.** Strict subgraph isomorphism vs structural
   similarity / edit-distance threshold.
3. **Diagnostic strategy.** Generic "no isomorphism found" vs
   role-aware messages that name the failing role/edge.

Issue #18 specifies the algorithmic shape and types verbatim. This
ADR records the rationale, fixes the public types, and pins the
strict-mode default and diagnostic format that the verifier renders.

## Decision

### 1. Algorithm: role-hinted backtracking (custom, k ≤ 5)

`internal/verify/backtrack.go` runs DFS over role assignments:

```
1. For each role R in T:
     C[R] = { n in G_new | kind(n) == kind(R)
                         ∧ (R has method shape ⇒ n has matching signature shape)
                         ∧ (R has receiver_role ⇒ n's receiver kind matches) }
   If any C[R] is empty → Mismatch with reason "no candidate for role R: …".

2. Order roles by ascending |C[R]| (smallest-first).

3. DFS over assignments, pruning the moment any fully-assigned edge
   constraint (R_a, kind, R_b) ∈ T is missing in G_new.

4. On exhaustion without a solution, emit a Mismatch with the partial
   assignment and the first failing edge constraint as the diagnostic.

5. On success, emit Match with the role→node mapping and the inverse
   role→name binding for downstream tooling.
```

The skeleton already declares role labels with kind constraints
(per ADR-016/#16): that collapses the candidate set per role from
"all nodes" to "nodes with matching kind plus optional signature
shape". For typical proposals (k ≤ 5 roles, ≤ 10 candidates per
role) naive backtracking explores ≤ 10^5 states — trivial.

Role-hinted backtracking wins for v1 because:

- **No external dep.** Lives entirely in `internal/verify/`.
- **Pruning falls out for free.** Role kinds + edges give natural
  hooks; we don't need an extra "compatibility refinement" pass.
- **Diagnostics are role-aware.** "Role `<Iface>` has no candidate
  matching kind=interface with method shape (Param,RetType)" beats
  "no isomorphism found" for the operator and for downstream tools
  like `archmotif refactor` that want to feed the diff back to the
  LLM.
- **Simple to test.** The DFS is ~50 lines. Failure modes are easy
  to enumerate (no candidates, candidate exhaustion, edge violation).

VF2 + role-hinted candidate sets is the obvious next optimisation if
proposal subgraph size grows past k = 10 or so. Not v1.

### 2. Strictness: strict subgraph match by default

RQ-9 says strict; we honour it. A successful match requires every
role and every edge constraint in `T` to land in `G_new` with the
exact node kind, edge kind, and direction declared in the skeleton.
There is no edit-distance fallback in v1.

Structural similarity (allow N missing edges, fuzzy method shape)
is deferred to a future ADR if false-rejects become a real problem.
The verifier interface is shaped so a future `StructuralVerifier`
implementing the same `Verifier` contract can plug in without
breaking callers.

### 3. Diagnostics: human-readable text + structured JSON

`internal/verify/diff.go` renders mismatches in two formats:

**Text** (default, `archmotif verify --format=text`):

```
Mismatch on proposal motif-001:
  Role <Iface>:    candidate kind=interface required, no matches in pkg/store/*.go
  Role <Method>:   bound to UserStore.Find at pkg/store/user.go:42, but
                   expected edge (<Method>, calls, <Iface>) — found edge
                   (<Method>, calls, *sql.DB) instead.
Removed roles: -
Extra nodes:    none considered (strict subgraph match).
```

**JSON** (`archmotif verify --format=json`): a `{reason, missing_roles[],
failing_edges[], partial_mapping}` envelope so downstream tools (esp.
Stage 9 orchestrator + Stage 7 prompt-feedback) can consume the
diff without re-parsing text.

Both formats live in `diff.go` and round-trip through the same
`Diff` struct. The struct itself is exported so tests and the CLI
can synthesise/assert on diffs without hitting the renderer.

### 4. Public API (locked)

```go
type Verifier interface {
    Verify(ctx context.Context, target Skeleton, code *graph.Graph) Result
}

type Skeleton struct {
    ProposalID string
    Roles      []Role
    Edges      []EdgeConstraint
}

type Role struct {
    ID           string
    Kind         graph.NodeKind
    MethodShape  *MethodShape   // optional; for method/function roles
    ReceiverRole string         // optional; for method roles
    Realises     *Realisation   // optional; for method roles realising interface methods
}

type EdgeConstraint struct {
    From string
    To   string
    Kind graph.EdgeKind
}

type MethodShape struct {
    ParamKinds []graph.NodeKind  // ordered
    ReturnKind graph.NodeKind    // optional; zero value = unconstrained
}

type Realisation struct {
    Role   string
    Method string
}

type Result struct {
    Match    bool
    Mapping  map[string]graph.NodeID
    Bindings map[string]string         // role.ID → instance name (e.g. <Iface>=UserStore)
    Diff     *Diff
}

type Diff struct {
    Reason         string
    MissingRoles   []MissingRole
    FailingEdges   []FailingEdge
    PartialMapping map[string]graph.NodeID
}
```

`graph.NodeID` is an alias of `string` (the existing stable ID
returned by `graph.MakeID`). Adding the alias to `internal/graph`
is intentionally out of scope for this PR — the verifier types use
`string` as the ID type and the alias decision is deferred. The
ADR documents the *intent* (an opaque ID type) so downstream code
does not encode assumptions about its representation.

### 5. Cross-ticket alignment with #19 (Proposer)

Issue #19 (running in parallel) defines `Proposal` and
`TargetSubgraph` in `internal/propose/`. Per #19's spec it is the
single source of truth for those types, and #16's skeleton format
is the on-disk representation that bridges Stage 6 (renderer) and
Stage 8 (verifier).

To keep the verifier independent of the proposer (and avoid a
circular dep + unfinished interface coupling during parallel work),
the verifier defines its own `Skeleton`, `Role`, and `EdgeConstraint`
types locally, as ticket #18 specifies verbatim. Consolidation —
either making `verify.Skeleton` a thin re-export of
`propose.TargetSubgraph`, or moving the skeleton type to a shared
`internal/skeleton` package — is intentionally deferred to a
follow-up PR once both packages have shipped their first cut.

The trade-off: a small amount of type duplication today, in
exchange for two PRs that land in parallel without blocking each
other or fighting over the shape of a shared type before either
package has been used in anger.

## Alternatives considered

- **Generic VF2.** Worst-case exponential, but fast in practice on
  small subgraphs. No standard-library Go implementation; adopting
  one means adding a vendored or third-party dep. Diff quality is
  poor — VF2 reports "no isomorphism" without the role-named context
  the operator needs. Reject for v1; revisit if k grows past 10.
- **Color refinement (1-WL).** Polynomial, proves *non*-isomorphism
  fast, but doesn't recover the role mapping or surface a diff that
  names the failing role. Useful as a fast pre-filter; not enough on
  its own. Reject.
- **Graph edit distance.** Tunable threshold gives a structural-
  similarity match. Conflicts with RQ-9 ("start strict"). Reject for
  v1; revisit only if false-rejects become a real problem in Stage 9
  end-to-end runs.
- **Generic isomorphism with post-hoc role labelling.** Run any
  iso algorithm, then attach role names by sorting candidate
  matchings. Inverts the natural pruning order — kinds and method
  shapes are the strongest constraints; using them only at the end
  wastes the work the skeleton already did.

## Consequences

- The verifier ships with role-aware diagnostics out of the gate.
  Stage 9's "feed the diff back to the LLM and ask for a fix" loop
  has machine-readable input from day one.
- Strict by default means the LLM has to land the exact target
  shape; it cannot introduce extra edges and "win" on structural
  similarity. This is the right v1 stance — looseness can erode
  the contract between Stage 6 (skeleton) and Stage 8
  (verification) silently.
- The candidate-set construction depends on the kind of every node
  in `G_new` — all kinds in `internal/graph/kinds.go` are stable as
  of ADR-005. Adding a new node kind doesn't break verifier code,
  only the universe of skeleton role kinds.
- Method shape matching uses the *kind* of each parameter and the
  return (e.g. `[type, type] → type`). It does *not* compare full
  type identity, on purpose: the skeleton names types via role
  placeholders (`<ParamType>`, `<RetType>`), and matching by kind
  preserves that abstraction. Tighter shape matching is a follow-up
  if false-positives appear.
- The `Verifier` interface is the seam for future variants
  (VF2-backed, structural-similarity, contract-aware). Adding one
  is a new file in `internal/verify/` plus a CLI flag — no caller
  bumps.
