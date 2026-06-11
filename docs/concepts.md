# Concepts

The conceptual model behind `archmotif`. What the words mean, what
the system cares about, what it explicitly does not.

---

## 1. Code as a typed graph

A codebase is a typed graph. Nodes are program elements (packages,
files, types, functions, methods, fields, control-flow primitives).
Edges are relationships (contains, implements, embeds, calls,
references, depends-on, returns, uses-type,
calls-from-control-primitive).

This is **not new** — every static-analysis tool builds a graph. What's
different here is the *level of abstraction* we hold and the
operations we perform on it.

### Graph abstraction levels (1–8 from earlier brainstorm)

1. AST per file
2. Symbol table (names + types)
3. Typed call graph
4. Call graph + control flow per function (CFG inside each node)
5. Call graph + data flow
6. Call graph + effects / state
7. Call graph + concurrency model
8. Full operational semantics

`archmotif` targets **level ~3.5**: typed call graph, plus *selective*
control-flow primitives surfaced as their own nodes — loops, branches,
goroutines, defers, channel ops, sync primitives. Stops short of full
CFG (which explodes the graph). Sweet spot for pattern-level reasoning.

---

## 2. Structural metrics

Properties of the graph that say something architectural. None of
these are unique to code — they're standard graph-theory measures.
The novelty is in **applying them** to architectural questions.

Starter set:

- **Motif redundancy** — how often does the same small subgraph
  pattern recur without an extracted abstraction
- **Local symmetry** — clusters of nodes playing indistinguishable
  roles (same neighbour types, same edge patterns)
- **Modularity (Newman)** — does the graph naturally partition into
  communities; do those match declared package boundaries
- **Spectral gap** — algebraic connectivity; small gap = bottleneck
  edges holding things together
- **Cycle rank** — counts and locates cycles in dependency subgraph

The point isn't "compute all these and rank." The point is
**experiment**: each metric is a different lens on the same graph,
each surfaces different things, and the right lens depends on what
question you're asking. The infrastructure must make adding a new
metric trivial.

---

## 3. Contracts (user-declared stable nodes)

Some nodes in the graph are **contracts**: the user has declared "this
interface / type is the boundary; what's inside can change but this
shape must not." Contracts are first-class:

- The user marks them (config or annotation)
- The system tracks them as a distinct node category
- Refactorings must preserve them
- Each contract field can be traced back through the graph to where
  its values originate (basic data-flow connection)

Precedent: published client-API contracts in any service codebase. Same
idea — some boundaries are commitments, others are implementation. The
system needs to know the difference.

This is the wedge that lets refactorings be safe: "anything inside
the contract can be reshaped; the contract itself must be preserved."

---

## 4. Anomaly, not optimization

A naive read of "use metrics to improve graphs" is "maximize each
metric." That's wrong:

- Maximize symmetry → over-abstraction, generic ratholes
- Maximize modularity → artificial boundaries that don't match domain
- Minimize cycles to zero → pessimal in some legitimate domains

The right framing is **anomaly detection**: surface places where a
metric is *unusually high or low compared to the rest of the graph*.
Treat that as "look here", not "fix to threshold X."

The user (or downstream LLM) decides whether the anomaly is
intentional or worth changing. The tool **proposes**, doesn't impose.

---

## 5. Local transformations

When `archmotif` proposes a refactoring, it's **local**: one region
of the graph, one rewrite rule, one shift of one metric. Not whole-
graph optimization. Not "rewrite everything cleaner."

Reasons:

- Local rewrites are tractable (AST-level tools can apply them)
- Local rewrites are reviewable (human or LLM can verify the diff)
- Local rewrites compose (apply one, recompute, apply next — like
  gradient descent vs solving the system in closed form)
- Whole-graph rewrites are basically rewrites of the project, which
  is what humans do over months, not what a tool should do in one
  shot

Initial transformation rules will mirror the metrics:

- Repeated motif → extract interface / shared function
- Cycle in dependencies → invert one edge
- Bottleneck through mutable state → pass-as-arg

Each rule = a function that takes (graph, anomalous region) and
returns a target subgraph + a rewrite description.

---

## 6. Skeletons for LLM materialization

A proposed transformation is rendered as a **structural code skeleton**
— a Go-ish syntax fragment with placeholder names — that shows the
target shape:

```go
// Proposed transformation: extract interface for repeated motif M-001
// Affected files: pkg/store/user.go, pkg/store/order.go, pkg/store/product.go

type <Iface> interface {
    <Method>(<Param> <ParamType>) <RetType>
}

type <Impl> struct { ... }

func (i *<Impl>) <Method>(<Param> <ParamType>) <RetType> { ... }

// Sample existing instances:
//   <Iface>=UserStore   <Impl>=SQLUserStore   <Method>=Find
//   <Iface>=OrderStore  <Impl>=SQLOrderStore  <Method>=Find
//   <Iface>=ProductStore <Impl>=PgProductStore <Method>=Lookup
```

The LLM's job:

1. Decide what `<Iface>`, `<Method>` etc. **mean** in this domain
2. Rename consistently
3. Generate the new code in a branch

The LLM is not asked to invent architecture — it's asked to **fill in
names and bodies** in a structure the tool already chose. Cheap,
constrained, and verifiable.

---

## 7. Verification: graph as contract

After the LLM produces new code, build the graph from that new code
and verify it matches the target subgraph the tool emitted. If it
doesn't, the LLM drifted or hallucinated; reject.

This is the closing of the loop. **The graph is the source of truth;
the code is the materialization; the linter ensures the
materialization stays faithful.**

This is also what makes the whole approach safer than "ask the LLM to
refactor for you" — the LLM has a verifiable target, not just a
prompt.

---

## 8. Out of scope (explicitly)

- **Whole-codebase rewrites.** One local change per loop iteration.
- **Auto-applying refactorings without review.** Always emit branch +
  diff; human or CI gates merge.
- **Rule-based "this is bad practice" linting.** That's `archlint`.
  We're a different tool.
- **Cross-language portability** (initial). Go-only.
- **Live-edit IDE plugin** (initial). Batch CLI is enough.
- **Replacing the user's design judgement.** The tool surfaces and
  proposes; humans decide.

---

## 9. The physics-math analogy (where this came from)

In physics, you don't analyze a complex system by enumerating every
state. You find the *symmetries*, *conserved quantities*, *natural
scales*, and *coupling constants*, then reason about the system in
those terms. Renormalization group: collapse local detail to see what
survives at the next scale.

Software architecture today is mostly enumeration: list patterns,
list anti-patterns, check rules. There's almost no use of structural
mathematics on code graphs, even though the graphs are right there.

`archmotif` is the bet that: **the same lenses (symmetry, modularity,
spectral, motif) that work on physical/biological/social graphs also
say something useful about code graphs.** And that those insights can
close a loop back into the code, not just sit in a dashboard.
