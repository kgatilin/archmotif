# Open research questions

These are questions the spec deliberately does **not** answer. The
implementer should **decide and proceed**, recording the decision as
an ADR under `docs/decisions/`.

This file mirrors the in-stage questions in `ROADMAP.md` and adds
cross-stage / open-ended ones that aren't tied to a single stage.

---

## Cross-stage / strategic

### RQ-1 — Scope of language support

Initial v1 is Go-only. Should the parser/graph layer be designed to
accept other languages later (TypeScript, Python)?

**Default decision in absence of new info:** keep parser Go-specific;
keep graph + metrics + propose + skeleton + verify language-agnostic
(operate on the abstract graph, not on Go AST). This means the
language boundary is at one layer (parser) and everything else
generalises if a second parser is added later.

### RQ-2 — Where does this live in the user's workflow?

CLI batch tool? CI integration? IDE plugin? GitHub PR bot?

**Default:** CLI batch tool. Everything else is downstream.

### RQ-3 — Cost model for LLM materialization

How expensive is each `archmotif refactor` call? What's the cost
budget for the demo?

**Default:** track and report per-run cost; no budget enforcement in v1.

### RQ-4 — Relationship to `archlint`

archlint is the existing rule-based linter. Does archmotif eventually
consume archlint's rule outputs as another input signal?

**Default:** treat as independent for now. May converge later.

### RQ-5 — Multi-scale / renormalization

Once motifs are named, they become single nodes at the next abstraction
level → can the whole pipeline run again on the collapsed graph?

**Default:** out of scope for v1. Note as a future direction.

---

## Algorithmic

### RQ-6 — Pure Go vs spill to Python

Spectral methods on large sparse graphs may exceed `gonum`'s
practical limits. Acceptable to dump graph as JSON, run a Python
helper, parse results back?

**Default:** acceptable as a fallback; document the boundary.

### RQ-7 — Motif counting algorithm

Exact subgraph isomorphism (works up to size ~5) vs gSpan-style
approximation (scales to bigger motifs but is approximate).

**Default:** start exact for sizes 3–5; revisit if motifs at larger
sizes become interesting.

### RQ-8 — Statistical significance for motifs

Need a null model (configuration model, edge swaps, etc.) to say
"this motif is *significantly* over-represented." Worth doing in v1?

**Default:** ship without significance testing in Stage 4; add in a
follow-up if anomaly noise is too high.

### RQ-9 — Subgraph isomorphism for verification (Stage 8)

Strict isomorphism is brittle (any extra edge fails). Structural
similarity (e.g. graph edit distance below threshold) is more
forgiving but harder to define cleanly.

**Default:** start strict; relax only if false-rejects become a
friction point in real use.

---

## Product / UX

### RQ-10 — Output format

JSON / YAML / human-readable / all?

**Default:** JSON for machine consumption (`--format=json`),
human-readable summary by default. No YAML unless asked.

### RQ-11 — Visualisation

Should archmotif output graphviz / mermaid / something for the user
to *see* the graph?

**Default:** add `archmotif graph --format=graphml` and let the user
load it in Gephi / yEd. No built-in renderer in v1.

### RQ-12 — Interactive vs batch

Could be a TUI for browsing anomalies. Worth it?

**Default:** out of scope for v1. CLI only.

---

## How to use this file

When you encounter an open question:

1. Look here first
2. If a default is listed, follow it unless you have new information
3. Record what you actually did in `docs/decisions/NNN-short-name.md`
4. Don't escalate to the user

If you encounter a question **not** listed here, add it to this file
with a default decision, then proceed with the default.
