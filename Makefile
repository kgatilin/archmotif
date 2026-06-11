.PHONY: build install test lint fmt vet clean tidy all graph-self metrics-self refactor-list demo optimize-dry arch-check arch-check-llm

GO            ?= go
BIN_DIR       ?= bin
BIN           := $(BIN_DIR)/archmotif
PKG           := ./...
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS       := -X main.version=$(VERSION)

# Self-dogfooding: archmotif analysing its own architecture. Outputs land in
# .archmotif/ (gitignored). See CLAUDE.md "Self-dogfooding" for when to run.
ARCH_SELF       := .archmotif/self
ARCH_REGION     ?= europe-west4

all: build test lint

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/archmotif

install:
	$(GO) install -ldflags '$(LDFLAGS)' ./cmd/archmotif

test:
	$(GO) test -race -count=1 $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

# Lint runs golangci-lint when available; otherwise falls back to go vet
# so the target is usable in plain dev environments. CI installs golangci-lint.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run $(PKG); \
	else \
		echo "golangci-lint not found; falling back to 'go vet'"; \
		$(GO) vet $(PKG); \
	fi

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR) dist

# Build archmotif and run `graph` against this repo. Useful for the
# Stage 1 verify step: confirms the tool builds a graph of itself
# without crashing and prints a node/edge summary.
graph-self: build
	$(BIN) graph --summary .

# Build archmotif and run all Stage 3 metrics against this repo. Useful
# for the Stage 3 verify step: confirms every metric produces a number
# without crashing on the project's own graph.
metrics-self: build
	$(BIN) metrics --format pretty .

# List all proposals archmotif would auto-pick on this repo, in
# auto-pick order (highest-impact first). No LLM call. Useful for
# demoing Stage 9 wiring without spending API tokens.
refactor-list: build
	$(BIN) refactor --list .

# End-to-end Stage 9 demo: pick the top proposal on archmotif itself
# and render the LLM prompt that would be sent to the materializer.
# Dry-run by default — no API call, no branch created. Set
# ANTHROPIC_API_KEY and drop --dry-run to actually materialize.
demo: build
	$(BIN) refactor --dry-run .

# Dry-run a single optimize-loop batch against this repo. No materializer
# call; writes prompt + contract + graph artifacts under .archmotif/runs/.
# Useful for confirming the pipeline is healthy end-to-end without
# spending API tokens.
optimize-dry: build
	$(BIN) optimize-loop --dry-run --max-batches 1 .

# ── Self-dogfooding the graph-metrics contract ────────────────────────────────
# arch-check: the NON-LLM architecture check. Build archmotif's own package
# graph, then run the structural suite + macro shape + the import-flow ratchet.
# No API tokens. Run before committing changes to archmotif itself; read the
# output per the `archmotif` skill (interpret -> recommend). The agent makes the
# suggestions — the target only emits the facts.
arch-check: build
	@mkdir -p $(ARCH_SELF)
	$(BIN) graph --format=json . > $(ARCH_SELF)/graph.json
	$(BIN) pkg-graph $(ARCH_SELF)/graph.json \
		--out $(ARCH_SELF)/pkg.graphml --policy $(ARCH_SELF)/baseline.policy.yaml
	@echo "── analyze (structural metrics) ──"
	$(BIN) analyze $(ARCH_SELF)/pkg.graphml
	@echo "── quotient (macro shape; acyclic=true is the goal) ──"
	$(BIN) quotient $(ARCH_SELF)/pkg.graphml --partition group
	@echo "── policy (import-flow ratchet) ──"
	@if [ -f arch-policy.yaml ]; then \
		$(BIN) policy $(ARCH_SELF)/pkg.graphml arch-policy.yaml; \
	else \
		echo "no arch-policy.yaml — baseline written to $(ARCH_SELF)/baseline.policy.yaml;"; \
		echo "review it and 'cp $(ARCH_SELF)/baseline.policy.yaml arch-policy.yaml' to enforce."; \
	fi

# arch-check-llm: the LLM/embeddings check. Embeds each package's semantic text
# with Vertex (gemini-embedding-001) and reports the EMERGENT clustering — where
# the code's meaning disagrees with its declared package layout. Needs Vertex
# access: export ARCHMOTIF_GCP_PROJECT (and optionally ARCH_REGION). Spends API
# tokens, so it is a separate target from arch-check.
arch-check-llm: build
	@test -n "$(ARCHMOTIF_GCP_PROJECT)" || { \
		echo "set ARCHMOTIF_GCP_PROJECT to a Vertex-enabled project first"; exit 2; }
	@mkdir -p $(ARCH_SELF)
	$(BIN) graph --format=json . > $(ARCH_SELF)/graph.json
	$(BIN) pkg-graph $(ARCH_SELF)/graph.json \
		--out $(ARCH_SELF)/pkg-text.graphml --text-key doc
	$(BIN) embed $(ARCH_SELF)/pkg-text.graphml --text-key doc \
		--project $(ARCHMOTIF_GCP_PROJECT) --location $(ARCH_REGION) \
		--out $(ARCH_SELF)/pkg-vec.graphml
	@echo "── semantic-clusters (emergent grouping vs declared packages) ──"
	$(BIN) calculate semantic-clusters $(ARCH_SELF)/pkg-vec.graphml
