#!/bin/bash
# Run conformance scenarios against the original Perl GADS
#   ./run_perl.sh [--pg] [scenario.yaml ...]
# The observations are the reference results (results/perl). With --pg the
# scenarios run on PostgreSQL (see pg_test_env.sh) and the observations go
# to results/perl-pg.
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
GADS="${GADS_DIR:-$HERE/../upstream/GADS-dev}"
OUT="$HERE/results/perl"
if [ "${1:-}" = "--pg" ]; then
    shift
    . "$HERE/pg_test_env.sh"
    OUT="$HERE/results/perl-pg"
fi
eval "$(perl -I "$HOME/perl5/lib/perl5" -Mlocal::lib 2>/dev/null)"
export PERL_INLINE_DIRECTORY="${PERL_INLINE_DIRECTORY:-$HOME/.cache/gads-inline-dev}"
mkdir -p "$PERL_INLINE_DIRECTORY"
export GADS_NO_FORK=1
if [ $# -eq 0 ]; then set -- "$HERE"/scenarios/*.yaml; fi
ARGS=()
for a in "$@"; do ARGS+=("$(readlink -f "$a")"); done
set -- "${ARGS[@]}"
cd "$GADS"
exec perl "$HERE/perl/run.pl" --gads "$GADS" --out "$OUT" "$@"
