#!/usr/bin/env bash
# Run the three matrix-based architecture validators against archmotif's
# own source tree and write the records to dogfood/matrix-validators-<date>.json.
#
# This is the canonical "run the validators on ourselves" step.
#
# Output JSON shape:
#   { "version": <runner schema version>, "records": [...], "ran": [...] }
#
# The dogfood/ directory is gitignored — the script is committed, the
# generated files are not.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN="$ROOT/bin/archmotif"
DATE="$(date -u +%Y%m%d)"
OUT_DIR="$ROOT/dogfood"
OUT_FILE="$OUT_DIR/matrix-validators-$DATE.json"

mkdir -p "$OUT_DIR"

if [[ ! -x "$BIN" ]]; then
  echo "[dogfood] building archmotif..." >&2
  make -C "$ROOT" build >/dev/null
fi

echo "[dogfood] running layer_mask, cycle_matrix, instability_matrix on $ROOT..." >&2
"$BIN" metrics \
  --metric layer_mask,cycle_matrix,instability_matrix \
  --format json \
  "$ROOT" > "$OUT_FILE"

echo "[dogfood] wrote $OUT_FILE" >&2
echo "[dogfood] summary:" >&2
if command -v jq >/dev/null 2>&1; then
  jq -r '
    .records
    | group_by([.metric, .scope])
    | .[]
    | . as $records
    | $records[0] as $first
    | if $first.scope == "graph" then
        "  \($first.metric) graph value = \($first.value)"
      else
        "  \($first.metric) \($first.scope) records = \($records | length)"
      end
  ' "$OUT_FILE"
else
  echo "  jq not found; summary skipped"
fi
