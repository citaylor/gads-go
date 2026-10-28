#!/bin/bash
# Benchmark the original Perl GADS (as bench_go.sh does the Go port):
#   ./bench_perl.sh [--pg] [--time SECS] [--no-units] [--no-scenarios] [bench/scenario.yaml ...]
# Runs the benchmark scenarios (bench/*.yaml) with GADS_BENCH set, so that
# each step's observation has its timing, into results/bench-perl (or
# results/bench-perl-pg with --pg), and the unit micro-benchmarks
# (perl/bench_units.pl, --time seconds each) into results/bench-units-perl.json.
HERE="$(cd "$(dirname "$0")" && pwd)"
# BENCH_SUFFIX is appended to the names of the results (e.g. -docker)
SFX="${BENCH_SUFFIX:-}"
GADS="${GADS_DIR:-$HERE/../upstream/GADS-dev}"
OUT="$HERE/results/bench-perl$SFX"
TIME=1
UNITS=1
SCENARIOS=1
while [ $# -gt 0 ]; do
    case "$1" in
        --pg) . "$HERE/pg_test_env.sh"; OUT="$HERE/results/bench-perl-pg$SFX" ;;
        --time) TIME="$2"; shift ;;
        --no-units) UNITS= ;;
        --no-scenarios) SCENARIOS= ;;
        *) break ;;
    esac
    shift
done
eval "$(perl -I "$HOME/perl5/lib/perl5" -Mlocal::lib 2>/dev/null)"
export PERL_INLINE_DIRECTORY="${PERL_INLINE_DIRECTORY:-$HOME/.cache/gads-inline-dev}"
mkdir -p "$PERL_INLINE_DIRECTORY" "$OUT"
export GADS_NO_FORK=1
if [ $# -eq 0 ]; then set -- "$HERE"/bench/*.yaml; fi
ARGS=()
for a in "$@"; do ARGS+=("$(readlink -f "$a")"); done
cd "$GADS" || exit 1
STATUS=0
if [ -n "$SCENARIOS" ]; then
    GADS_BENCH=1 perl "$HERE/perl/run.pl" --gads "$GADS" --out "$OUT" "${ARGS[@]}" || STATUS=1
fi
if [ -n "$UNITS" ]; then
    perl "$HERE/perl/bench_units.pl" --gads "$GADS" --time "$TIME" --out "$HERE/results/bench-units-perl$SFX.json" || STATUS=1
fi
exit $STATUS
