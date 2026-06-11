# archmotif

Code architecture as graph: extract structure from source, compute
mathematical properties of that structure, detect locally anomalous
regions, propose **small** local refactorings that improve those
properties, render proposals as structural skeletons an LLM can
materialize into actual code, and verify the result still matches
the proposed shape.

> Status: research project. Spec-first. Pre-code.

## Why

Architecture-analysis tools today (`archlint`, `go-arch-lint`, `dep-tree`
etc.) check **fixed rules** ("no cyclic imports", "layer A can't call B").
They don't measure *structural properties* of the codebase, and they
can't suggest improvements grounded in those properties.

But code structure is a graph. Math + physics give us tools for
understanding large structured graphs:

- **Spectral methods** (eigendecomposition of the Laplacian → bottlenecks)
- **Modularity / community detection** (Newman → real module boundaries
  vs declared ones)
- **Motif analysis** (Milo et al — recurring small subgraphs as the
  vocabulary of the system)
- **Symmetry detection** (regions where nodes play indistinguishable
  roles → candidates for shared abstraction)
- **Renormalization-group thinking** (collapse local detail; ask what
  survives at the next scale)

`archmotif` is the experiment: take those tools seriously, apply them
to code graphs, and close the loop by **applying** what they reveal
back to the source.

See [`docs/concepts.md`](./docs/concepts.md) for the conceptual model
and [`ROADMAP.md`](./ROADMAP.md) for the staged build plan.

Current dogfooding experiments live under
[`docs/experiments/`](./docs/experiments/), including the
[#58 ArchMotif self-convergence experiment](./docs/experiments/archmotif-self-convergence/).

## The loop

```
   Source code
       ↓ parse
   Typed graph  (level 3.5: types + calls + selective control flow)
       ↓ compute
   Structural metrics (motif redundancy, modularity, symmetry, spectral, ...)
       ↓ detect
   Anomalous local region + proposed transformation
       ↓ render
   Structural skeleton (target shape, placeholder names)
       ↓ LLM materializes
   New code in branch
       ↓ verify
   Linter: graph(new code) matches target shape
```

Each arrow is a stage. Every stage is independently shippable. See
[`ROADMAP.md`](./ROADMAP.md).

## Current CLI

Build the typed graph for a module or package:

```bash
archmotif graph --summary .
archmotif graph --pattern ./internal/parser --format json .
archmotif graph --pattern ./internal/parser --format graphml . > parser.graphml
```

`--format graphml` is intended for exploratory tools such as Gephi. It reads
the module-root `.archmotif.yaml` when present, marks declared contracts, and
emits contract attributes in the graph. Nodes carry labels, stable
`archmotif_id`, kind, source position, foreign marker, contract attributes, and
coarse view attributes (`layer`, `detail_level`). Edges carry their exact
relationship kind plus coarse view attributes, so graph tools can filter broad
layers such as structure, dependency/type relationships, and calls without
losing the exact edge kind.

Open the actual graph in the built-in browser view:

```bash
archmotif view --http 127.0.0.1:7140 .
archmotif view --pattern ./internal/parser/... .
archmotif view --exclude-dir tests .
archmotif view --config /path/to/.archmotif.yaml .
archmotif view --root /tmp/archmotif --graph-id parser ./internal/parser
```

`archmotif view` starts one local graph server. It writes the extracted graph to
the MCP graph workspace (`$ARCHMOTIF_HOME` or `~/.archmotif` by default), serves
the browser at `/`, and exposes the same graph store as streamable HTTP MCP at
`/mcp`. The browser starts with a package dependency overview, lets you drill
into one package's public surface, highlights contract nodes from
`.archmotif.yaml`, and can focus a one- or two-hop neighborhood around any
package, type, function, or method.

The same config can exclude known visualization-noise nodes before export:

```yaml
graph:
  exclude:
    dirs:
      - tests
    qnames:
      - fmt.Errorf
```

`dirs` is applied before package loading; the remaining graph exclusions are
applied after graph construction. Use node exclusions for universal utility
sinks that create false visual hubs. Prefer targeted exclusions first; broad
package exclusions can hide useful architecture mechanisms such as
synchronization, serialization, storage, or HTTP boundaries.

Generate local optimization contracts from either source code or a generic
GraphML shape:

```bash
archmotif optimize ./internal/parser
archmotif optimize --mode=architecture --target-graphml-out /tmp/target.graphml ./internal/parser
archmotif optimize memory.graphml
archmotif optimize --predicate contains --parent-direction out code.graphml
archmotif optimize-batch --contract-out /tmp/batch.json --prompt-out /tmp/batch.md memory.graphml
```

Bridge the typed graph into a domain-oriented architecture model
(packages, symbols, dependencies, implementations, stereotypes,
facets) suitable for tools like Archai:

```bash
archmotif export --format archai-model .
archmotif export --format archai-model --encoding yaml .
```

The output is a stable JSON or YAML document; two runs over the same
graph produce byte-identical output. Every entity preserves its
archmotif node id under `archmotifId`, role metadata is surfaced as
`role:*` stereotypes, and contract markers become a `contract`
stereotype. See `docs/decisions/034-archai-bridge.md` for the mapping
table.

`optimize` defaults to `--mode=auto`: `.graphml` inputs use the generic
GraphML shape optimizer, while source directories use the architecture
pipeline (`parser → metrics → anomalies → propose`). The architecture mode
emits deterministic transformation contracts from real graph anomalies. The
first shipped contract is `motif_quotient_extract_interface`: repeated
isomorphic motif instances are factored into a target subgraph with one
interface role and repeated implementation/method roles. `--target-graphml-out`
writes the top contract's target graph for visual inspection in Gephi.

Turn an optimizer contract into a project-level target architecture contract,
scaffold the declared surface, and verify the actual graph against that target:

```bash
archmotif optimize --mode=architecture \
  --contract-out /tmp/archmotif-optimize.json \
  --target-graphml-out /tmp/archmotif-target.graphml \
  .
archmotif target contract --out /tmp/archmotif-target.json /tmp/archmotif-optimize.json
archmotif target scaffold --out /tmp/archmotif-target-scaffold /tmp/archmotif-target.json
archmotif target verify /tmp/archmotif-target.json .
```

The target contract records packages to keep/create, files to create, public
interfaces/types/functions, expected package-level edges, scaffold hints, and
the original target subgraph. See
[`docs/target-contract.md`](./docs/target-contract.md).

The GraphML shape mode is still a proof of concept, independent of the Go parser
and independent of Gephi. It currently detects `flat_star_hub` regions: hubs
with too many direct structural leaf children. Its output is a deterministic
rewrite contract, not an applied patch. It specifies the editable subgraph,
read-only boundary context, target group count, structural edges to replace,
validation metrics, and the semantic materialization task left for an LLM.

`optimize-batch` selects one deterministic next rewrite batch for iterative
cleanup. It prioritizes orphan nodes because degree-zero nodes are not
traversable, but it does not blindly attach every orphan. Orphan contracts use
`resolve_orphans_with_cleanup`: selected nodes may be deleted as noise,
consolidated into a connected summary memory, or kept and attached through a
connected relation. Selection is bounded by `--context-budget-bytes` (default
`12000`); `--orphan-batch-size` is only a hard cap.

## Repo layout (planned)

```
cmd/archmotif/          # CLI entrypoint
internal/parser/        # source → typed graph
internal/graph/         # graph data structures
internal/metrics/       # pluggable metrics (one file per metric)
internal/shape/         # generic GraphML shape optimization POC
internal/contracts/     # contract node identification + tracing
internal/propose/       # anomaly → transformation rules
internal/targetcontract/# target graph → scaffold/verify contract
internal/skeleton/      # transformation → code skeleton
internal/verify/        # graph(new code) ≅ target?
docs/                   # concepts, research questions, decisions
testdata/               # synthetic Go programs for each stage
```

## Test corpus

- `archmotif` itself (recursive analysis)
- [`kgatilin/archlint`](https://github.com/kgatilin/archlint) — small
  real-world Go project
- Larger open-source Go projects (later)

## Language

Go for everything if feasible. If a metric needs heavy numerics
(spectral methods on large sparse graphs) and `gonum` isn't enough,
permitted to **dump graph to JSON and compute in Python** as a
fallback. Don't optimize prematurely — first make it work.
