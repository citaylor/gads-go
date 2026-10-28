#!/bin/bash
# Benchmark both implementations and compare them:
#   ./bench_all.sh [--pg] [--time SECS] [--no-units] [--no-scenarios] [bench/scenario.yaml ...]
# Runs bench_perl.sh and bench_go.sh (with the same options), then
# gads-bench compare, which prints the step, scenario and unit times side
# by side, and checks that the observations of the two are the same. The
# comparison is also written as Markdown to results/bench-report.md.
#
# Regenerate the scenarios at another size with
#   generators/gen_bench.py --records N
HERE="$(cd "$(dirname "$0")" && pwd)"
# BENCH_SUFFIX is appended to the names of the results (e.g. -docker)
SFX="${BENCH_SUFFIX:-}"
GO="$HERE/../gads-go"
PERL_OUT="$HERE/results/bench-perl$SFX"
GO_OUT="$HERE/results/bench-go$SFX"
OPTS=()
PGS=
UNITS=1
SCENARIOS=1
while [ $# -gt 0 ]; do
    case "$1" in
        --pg)
            . "$HERE/pg_test_env.sh"
            OPTS+=(--pg)
            PGS=-pg
            PERL_OUT="$HERE/results/bench-perl-pg$SFX"
            GO_OUT="$HERE/results/bench-go-pg$SFX"
            ;;
        --time) OPTS+=(--time "$2"); shift ;;
        --no-units) OPTS+=(--no-units); UNITS= ;;
        --no-scenarios) OPTS+=(--no-scenarios); SCENARIOS= ;;
        *) break ;;
    esac
    shift
done
"$HERE/bench_perl.sh" "${OPTS[@]}" "$@"
PERL_STATUS=$?
"$HERE/bench_go.sh" "${OPTS[@]}" "$@"
GO_STATUS=$?
CMP=(-perl "$PERL_OUT" -go "$GO_OUT")
if [ -z "$SCENARIOS" ]; then
    CMP=(-perl /nonexistent -go /nonexistent)
fi
if [ -n "$UNITS" ]; then
    CMP+=(-units-perl "$HERE/results/bench-units-perl$SFX.json" -units-go "$HERE/results/bench-units-go$SFX.json")
fi
echo
cd "$GO" || exit 1
export PATH="$HOME/.local/go/bin:$PATH"
go run ./cmd/gads-bench compare "${CMP[@]}"
CMP_STATUS=$?
go run ./cmd/gads-bench compare -md "${CMP[@]}" > "$HERE/results/bench-report$PGS$SFX.md"
echo "perl=$PERL_STATUS go=$GO_STATUS compare=$CMP_STATUS (report: results/bench-report$PGS$SFX.md)"
exit $(( PERL_STATUS || GO_STATUS || CMP_STATUS ))
