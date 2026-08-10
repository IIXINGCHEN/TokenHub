#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BENCHTIME="${TOKENHUB_BENCHMARK_BENCHTIME:-3s}"
COUNT="${TOKENHUB_BENCHMARK_COUNT:-5}"
RESULT_PATH="${TOKENHUB_BENCHMARK_GO_RESULT:-$ROOT_DIR/output/benchmarks/go-benchmarks.txt}"

mkdir -p "$(dirname "$RESULT_PATH")"
cd "$ROOT_DIR/backend"
go test ./internal/server \
  -run '^$' \
  -bench '^BenchmarkGateway' \
  -benchmem \
  -benchtime "$BENCHTIME" \
  -count "$COUNT" | tee "$RESULT_PATH"
