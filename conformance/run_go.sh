#!/bin/bash
# Run conformance scenarios against the Go port (as run_perl.sh for Perl)
#   ./run_go.sh [--pg] [scenario.yaml ...]
# With --pg the scenarios run on PostgreSQL (see pg_test_env.sh) and the
# observations go to results/go-pg instead of results/go.
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
GO="$HERE/../gads-go"
OUT="$HERE/results/go"
if [ "${1:-}" = "--pg" ]; then
    shift
    . "$HERE/pg_test_env.sh"
    OUT="$HERE/results/go-pg"
fi
export PATH="$HOME/.local/go/bin:$PATH"
if [ $# -eq 0 ]; then set -- "$HERE"/scenarios/*.yaml; fi
ARGS=()
for a in "$@"; do ARGS+=("$(readlink -f "$a")"); done
mkdir -p "$OUT"
cd "$GO"
exec go run ./cmd/gads-conformance -out "$OUT" "${ARGS[@]}"
