#!/bin/bash
# Run every conformance scenario against Perl GADS and the Go port, then
# compare the observations.
#   ./run_all.sh            all scenarios
#   ./run_all.sh 003_       scenarios whose file name contains "003_"
#   ./run_all.sh --pg [..]  on PostgreSQL (results/perl-pg, results/go-pg)
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
GO="$HERE/../gads-go"
PG=()
PERL_OUT="$HERE/results/perl"
GO_OUT="$HERE/results/go"
if [ "${1:-}" = "--pg" ]; then
    shift
    PG=(--pg)
    PERL_OUT="$HERE/results/perl-pg"
    GO_OUT="$HERE/results/go-pg"
    # Started once here, for both runners
    . "$HERE/pg_test_env.sh"
fi
FILTER="${1:-}"
FILES=$(ls "$HERE"/scenarios/*.yaml | grep -- "$FILTER")
echo "=== Perl (reference) ==="
"$HERE/run_perl.sh" "${PG[@]}" $FILES
PERL_STATUS=$?
echo
echo "=== Go ==="
"$HERE/run_go.sh" "${PG[@]}" $FILES
GO_STATUS=$?
echo
echo "=== Parity (Perl vs Go) ==="
(cd "$GO" && PATH="$HOME/.local/go/bin:$PATH" go run ./cmd/gads-compare -perl "$PERL_OUT" -go "$GO_OUT" $FILTER)
CMP_STATUS=$?
echo
echo "perl=$PERL_STATUS go=$GO_STATUS compare=$CMP_STATUS"
exit $(( PERL_STATUS || GO_STATUS || CMP_STATUS ))
