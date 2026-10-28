#!/bin/bash
# Benchmark the Go port (as bench_perl.sh does the Perl original):
#   ./bench_go.sh [--pg] [--time SECS] [--no-units] [--no-scenarios] [bench/scenario.yaml ...]
# Runs the benchmark scenarios (bench/*.yaml) with GADS_BENCH set into
# results/bench-go (or results/bench-go-pg with --pg), and the unit
# micro-benchmarks (gads-bench units) into results/bench-units-go.json. The
# commands are built first, so compilation is not timed.
HERE="$(cd "$(dirname "$0")" && pwd)"
# BENCH_SUFFIX is appended to the names of the results (e.g. -docker)
SFX="${BENCH_SUFFIX:-}"
GO="$HERE/../gads-go"
OUT="$HERE/results/bench-go$SFX"
TIME=1
UNITS=1
SCENARIOS=1
while [ $# -gt 0 ]; do
    case "$1" in
        --pg) . "$HERE/pg_test_env.sh"; OUT="$HERE/results/bench-go-pg$SFX" ;;
        --time) TIME="$2"; shift ;;
        --no-units) UNITS= ;;
        --no-scenarios) SCENARIOS= ;;
        *) break ;;
    esac
    shift
done
export PATH="$HOME/.local/go/bin:$PATH"
if [ $# -eq 0 ]; then set -- "$HERE"/bench/*.yaml; fi
ARGS=()
for a in "$@"; do ARGS+=("$(readlink -f "$a")"); done
mkdir -p "$OUT"
BIN="$(mktemp -d)"
trap 'rm -rf "$BIN"' EXIT
cd "$GO" || exit 1
go build -o "$BIN/" ./cmd/gads-conformance ./cmd/gads-bench || exit 1
STATUS=0
if [ -n "$SCENARIOS" ]; then
    GADS_BENCH=1 "$BIN/gads-conformance" -out "$OUT" "${ARGS[@]}" || STATUS=1
fi
if [ -n "$UNITS" ]; then
    "$BIN/gads-bench" units -time "${TIME}s" -fixture "$HERE/bench/fixtures/units_db.yaml" -out "$HERE/results/bench-units-go$SFX.json" || STATUS=1
fi
exit $STATUS
