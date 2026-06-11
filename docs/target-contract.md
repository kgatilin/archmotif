# Target architecture contract

ArchMotif can now turn an optimizer contract into a project-level target
architecture contract, scaffold the declared surface, and verify an actual code
graph against that target.

The target contract is the bridge between graph-only architecture planning and
full-code implementation agents. It is intentionally more concrete than a raw
target GraphML shape:

- packages to keep or create;
- files to create;
- public interfaces, types, and functions to expose;
- expected package-level edges;
- scaffold hints for implementation agents;
- the original target subgraph for graph inspection.

## Workflow

Generate the optimizer contract and target graph:

```bash
archmotif optimize \
  --mode=architecture \
  --format=table \
  --top=1 \
  --pattern ./cmd/archmotif/... \
  --contract-out /tmp/archmotif-optimize.json \
  --target-graphml-out /tmp/archmotif-target.graphml \
  .
```

Render the project-level target contract:

```bash
archmotif target contract \
  --out /tmp/archmotif-target.json \
  /tmp/archmotif-optimize.json
```

Scaffold the declared packages/files/public symbols:

```bash
archmotif target scaffold \
  --out /tmp/archmotif-target-scaffold \
  /tmp/archmotif-target.json
```

Verify an implementation against the target:

```bash
archmotif target verify /tmp/archmotif-target.json .
```

The verifier exits `0` on match and `1` on drift. The text report lists missing
packages, files, public interfaces/types/functions, and expected package-level
edges.

## Current command-package split target

For the `command_package_split` proposal, the target contract currently
declares:

- keep `cmd/archmotif` as `CLIAdapter`;
- create `internal/optimize` as `OptimizeOrchestration`;
- create `internal/optimize/run.go`;
- expose `Options`, `Result`, and
  `Run(ctx context.Context, opts Options) (Result, error)`;
- expect `cmd/archmotif --dependsOn--> internal/optimize`.

This is a planning/scaffold contract. It does not claim that the optimize
orchestration extraction is done until `archmotif target verify` matches after
the implementation agent has moved the real behavior.

## Limits

The first version verifies concrete project-surface facts:

- package existence;
- file existence;
- public interface/type/function existence;
- package-level expected edges.

It does not yet verify full function signatures, method sets, private folder
layout, forbidden edges, or semantic behavior. Those are expected next
extensions to the target contract schema.
