# PoC report — graph-metrics contract, dogfooded on archmotif

Full cycle of the PRD (`docs/prd/archmotif-graph-metrics-library.md`): build the
graph-agnostic contract, measure archmotif with it, act on the findings,
iterate, re-measure. Pure Go (no Python/Julia). Branch
`poc/graph-metrics-contract`.

## 1. What was built

A graph-agnostic engine in `internal/contract/` over `graphmlx` (GraphML in,
data out — never touches source):

- **metrics** (`contract.go`): λ₂ / components (gonum Laplacian), dependency
  cycles (Tarjan SCC), **layering score** (1 − backedges/edges), god-nodes
  (fan-in/out > mean+2σ), per-node coupling (Ca/Ce/instability), and
  `semantic-clusters` (k-means on node `vec`, k by silhouette).
- **partition + quotient** (`partition.go`): group by any node attribute,
  collapse to a macro graph, report macro-DAG acyclicity + modularity.
- **policy** (`policy.go`): `Residual` = edges violating a declared
  partition+allow+sink policy. The generic "who may depend on whom".
- **operations** (`ops.go`): the 5 primitives (Move/Split-via/Merge/Introduce/
  Redirect, Reverse+RemoveEdge as Redirect cases) on a `Mutable`, output = a new
  target graph; `InvertDependency` recipe.
- **CLI**: `analyze`, `calculate`, `quotient --partition`, `policy` wired into
  `cmd/archmotif`. The legacy `quotient` is preserved (new path only on
  `--partition`).

Pinned by Signet acceptance (`acceptance.yaml`) — **8/8 cases green** against the
real binary.

## 2. How the repo was analyzed

archmotif's own internal package graph (27 nodes, 55 edges) was emitted as
GraphML, with each node carrying `group` = the semantic cluster from the earlier
embedding experiment (core/pipeline/optimizer/test/research/codegen) and the
real 768→3072-d `vec`. Then: `analyze`, `quotient --partition group`, `policy`
against a layered policy (everything → core; **core is a sink**; peers don't
cross).

## 3. Results (before)

```
nodes=27 edges=55 components=2
lambda2=0.0000  modularity=-0.0193  layering=1.0000
cycles: 0
god-nodes: 2
  cmd/archmotif   — fan-out 19 (does too much)
  internal/graph  — fan-in 22 (shared sink / god dependency)
quotient by group — 6 groups, acyclic=FALSE
  core: fan-in 26, fan-out 2     (should be a pure sink)
policy — 2 violations:
  internal/mcpserver      -> internal/graphmlx   (core -> test)
  internal/targetcontract -> internal/propose    (core -> pipeline)
```

## 4. Conclusions (agent interpretation)

- **λ₂=0, modularity<0** confirm a hub graph (star around `internal/graph`, the
  god-sink). Modularity is the wrong fitness here; **layering=1.0** (the node
  graph is acyclic) is the honest structural read. The real defect is at the
  **group** level: the macro graph is *not* a DAG.
- The 2 policy violations split into one real and one artifact:
  - `targetcontract -> propose` — **real inversion**: a foundational contract
    type depending upward on the pipeline. `targetcontract` only needed 4 types
    (`Proposal`, `TargetSubgraph`, `Role`, `EdgeConstraint`) that happen to live
    in `propose`.
  - `mcpserver -> graphmlx` — **false positive**: `graphmlx` (GraphML
    serialization) was mis-clustered into `test`; it is really `core`. A
    pure-metric tool would have reported it; the agent discards it.

## 5. Operations taken

- **Code refactor — extract shared kernel** (commit `f88ccf6`): moved the 4
  types to a new leaf package `internal/proposal`; `propose` keeps them as type
  aliases (zero churn in-package); `targetcontract` now imports `proposal` and
  drops `propose`. Inverts the edge cleanly with no cycle. Full build + all
  tests green.
- **Reclassification** (agent judgment, not code): `graphmlx` → `core`.

## 6. Iteration (after)

```
                       before     after
policy violations         2          0
quotient acyclic        false      TRUE
core fan-out              2          0   (pure sink)
modularity            -0.0193    -0.0067
build + tests          green      green
acceptance              n/a        8/8
```

The code refactor removed the real violation (2→1); the reclassification cleared
the artifact (1→0). The macro graph is now a DAG and `core` is a clean sink —
exactly the target the policy declared.

## 7. Honest limitations

- The "after" graph was produced by applying the exact, known import delta to the
  GraphML (faithful to the refactor), not by re-running a full extractor — the
  numbers reflect the real new import structure.
- `god-nodes` still flags `cmd/archmotif` (fan-out 19) and `internal/graph`
  (fan-in 23). These are intended hubs (composition root; shared graph model),
  not defects — an example of intrinsic metrics needing human/agent judgment,
  not auto-action.
- Community detection / Leiden was deliberately **not** used: modularity is
  degenerate on hub graphs (our own finding), so the Python Leiden helper and a
  Julia sidecar were both judged unnecessary for this roadmap.
