# archmotif — user-facing contract

archmotif takes **a graph** and tells you things about its structure. It does
not care what the graph represents: a node is an id + metadata, an edge is a
typed, weighted link. Same engine works on a dependency graph, a call graph, or
any GraphML file.

## What you can do — and how

| You want to… | You run | You get |
|---|---|---|
| Score a graph's structure | `analyze GRAPH [--json]` | metrics in one payload: spectral (λ₂, eigengap), modularity/communities, coupling, curvature, layering score |
| Compute one specific metric | `calculate METRIC GRAPH` | just that metric's value/result |
| Build node vectors (embeddings) | `embed GRAPH --text-key KEY` | the same graph with a `vec` per node, for similarity metrics |
| Collapse a graph to its macro shape | `quotient GRAPH --partition PARTITION` | super-graph of groups (condensation) |
| Check a policy ("who may depend on whom") | `policy GRAPH RULES` | the residual: edges that violate the policy |
| Choose which metrics to compute | per-project config (`.archmotif.yaml`) | only the metrics you enabled, with your parameters |
| Drive it from an agent | MCP server | query/decompose tools over a loaded graph |

Every `GRAPH` argument is a GraphML file.

`analyze` reads a per-project config selecting **which** metrics run and their
parameters (e.g. enable spectral + layering, set the community resolution, the
embedding model + text key, name your partition for `policy`). No config → a
sensible default set. The config is committed with the project, so the metric
suite is reproducible per repo.

Output is **structured JSON** (primary) plus a terse human summary. Same input →
same output (deterministic, diffable).

## The graph it accepts

archmotif does **not** invent a format — it speaks **GraphML**, a standard every
graph tool already reads and writes (Gephi, yEd, networkx, …).

- A node needs only an `id`; any other GraphML data keys are opaque metadata the
  metrics ignore.
- Edges may carry a `weight` (used by metrics) and any other data keys.
- A node may carry a feature vector (e.g. an embedding) as a data key for
  similarity-based clustering.

If a lighter interchange is ever needed it is a thin JSON mirror of the same
node/edge model — never a new semantics. GraphML is the contract.

## Partitions (groups of nodes)

`quotient` and `policy` work over a **partition** — a grouping of nodes.
archmotif never computes domains from source; a partition is one of:

- **Declared** — group by a node attribute, e.g. `--partition domain`. The
  attribute (`domain`, `bounded_context`, …) is set **by the producer**, not by
  archmotif. archmotif just buckets nodes by that key without interpreting it.
- **Computed** — a clustering archmotif derives itself (community detection, or
  similarity clustering when nodes carry feature vectors).

The producer is responsible for declared groupings: e.g. archai reads a code
docstring/annotation convention and emits the `domain` attribute on each node.
Because the label rides on the node, it also strengthens feature-vector
clustering — so declared and computed partitions can be overlaid and compared.

## Under the hood

Graph topology + edge weights (+ optional node vectors) run through standard
graph math: Laplacian spectral methods, modularity/community detection,
condensation into quotient graphs, Forman-Ricci curvature, and a layering/
acyclicity score for hub-shaped graphs. When nodes carry text, `embed` turns it
into vectors (Vertex `gemini-embedding-001`, configurable) so the same engine
can cluster nodes by meaning and fuse that with the topology. Policy checks are a
**residual** — edges that break a declared partition's allowed directions. No
domain knowledge, no source parsing, no rule catalog.

## Metrics

`calculate METRIC GRAPH` runs one; `analyze` runs the enabled set. All operate
on topology + weights only — except the feature metrics, which need node
vectors.

**Structural** (topology + edge weights):

| METRIC | measures |
|---|---|
| `degree` | in/out degree and centrality per node |
| `spectral` | Laplacian algebraic connectivity λ₂, eigengap, spectral gap |
| `modularity` | community-structure modularity Q |
| `communities` | community membership (e.g. Louvain/Newman) |
| `quotient` | condensation into a macro graph over a partition |
| `curvature` | Forman-Ricci edge curvature (bottlenecks) |
| `coupling` | afferent/efferent coupling per node |
| `motifs` | recurring small subgraphs |
| `layering` | layering / acyclicity score (DAG-ness at the group level) |
| `cycles` | strongly-connected components (dependency cycles) |

**Feature-based** (need node vectors — build them with `embed` first):

| METRIC | measures |
|---|---|
| `embed` | build a `vec` per node from a text attribute (Vertex `gemini-embedding-001`, model/dim/text-key configurable) |
| `semantic-clusters` | cluster nodes by vector similarity; k auto-picked (silhouette / eigengap) |
| `fused-clusters` | one decomposition combining structural eigenvectors + semantic vectors |

Vectors and clusters are just another metric output. Naming the resulting
clusters and deciding what they mean stays with the agent/engineer.

### Embedding cache (incremental)

`embed` never re-embeds unchanged content. It keeps a **content-addressed**
cache keyed by `hash(node text + model + dimension)`:

- cache **hit** → reuse the stored vector, no API call;
- cache **miss** (new node, changed text, or a different model/dimension) →
  call the model once and store the result.

The key is the *content*, not the node id, so regenerating the graph or renaming
/ moving a node reuses its vector as long as the text is identical; switching the
model invalidates everything (the model is part of the key). The cache is a
portable `hash → vector` store (e.g. `.archmotif/embed-cache/`) holding no ids
and no source text; it can be committed so CI and teammates reuse vectors with
zero API calls, keeping `embed` deterministic and cheap.

## Not in scope

Parsing source, drawing diagrams, a UI, or proposing changes. archmotif emits
facts; producers build the graph, and the agent/engineer decides what to do with
the numbers.

## Acceptance

The user-facing contract is pinned by an executable Signet spec at
[`acceptance.yaml`](../../acceptance.yaml). It uses a tiny domain-free GraphML
fixture (no source parsing) plus two policy files. Run with
`signet run acceptance.yaml --yes` once the commands are implemented; inspect
with `signet cases acceptance.yaml --checks`.

| Case id | Proves |
|---|---|
| `help-lists-commands` | top-level help lists `analyze`, `calculate`, `quotient`, `policy` |
| `analyze-suite` | `analyze GRAPH` runs the metric suite (modularity, λ₂, layering) |
| `analyze-json` | `analyze --json` emits a machine-readable payload |
| `calculate-one-metric` | `calculate METRIC GRAPH` computes a single named metric |
| `semantic-clusters-from-vectors` | `calculate semantic-clusters` groups nodes by their vectors |
| `quotient-by-attribute` | `quotient --partition domain` collapses by a declared node attribute |
| `policy-conformant-passes` | `policy` exits 0 when the residual is empty |
| `policy-violation-fails` | `policy` exits non-zero and lists the violating edges |

A separate live spec [`embed.acceptance.yaml`](../../embed.acceptance.yaml)
(`embed-builds-vectors`, gated by `ARCHMOTIF_GCP_PROJECT`) proves `embed` builds
vectors from a node text attribute via Vertex.
