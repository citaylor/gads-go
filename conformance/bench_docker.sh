#!/bin/bash
# Benchmark both implementations in containers (docker-compose.bench.yml):
# the Perl GADS in gads-perl-bench (the pinned modules of the reference
# install, with the XS JSON and CSV backends) and the Go port in the Go
# builder, both on Debian trixie, one after the other, then compare them:
#   ./bench_docker.sh [--pg] [--time SECS] [--no-units] [--no-scenarios] [bench/x.yaml ...]
# The options are those of bench_all.sh. The results go to the same files
# with the suffix -docker (BENCH_SUFFIX), e.g. results/bench-perl-docker and
# results/bench-report-docker.md. BENCH_CPUS limits the CPUs of each
# container (e.g. BENCH_CPUS=1); by default they are not limited. GADS_SRC is
# the GADS checkout mounted in both (default ../upstream/GADS-dev), e.g.
#   GADS_SRC=../upstream/GADS BENCH_SUFFIX=-docker-master ./bench_docker.sh
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE" || exit 1
export BENCH_SUFFIX="${BENCH_SUFFIX:--docker}"
export BENCH_UID="${BENCH_UID:-$(id -u)}" BENCH_GID="${BENCH_GID:-$(id -g)}"
SFX="$BENCH_SUFFIX"
DC=(docker compose -f "$HERE/docker-compose.bench.yml")
OPTS=()
PG=
PGS=
UNITS=1
SCENARIOS=1
PERL_OUT=/conformance/results/bench-perl$SFX
GO_OUT=/conformance/results/bench-go$SFX
while [ $# -gt 0 ]; do
    case "$1" in
        --pg)
            PG=1
            PGS=-pg
            OPTS+=(--pg)
            PERL_OUT=/conformance/results/bench-perl-pg$SFX
            GO_OUT=/conformance/results/bench-go-pg$SFX
            ;;
        --time) OPTS+=(--time "$2"); shift ;;
        --no-units) OPTS+=(--no-units); UNITS= ;;
        --no-scenarios) OPTS+=(--no-scenarios); SCENARIOS= ;;
        *) break ;;
    esac
    shift
done
# Scenario files as seen in the containers (the directory is /conformance)
ARGS=()
for a in "$@"; do ARGS+=("/conformance/$(realpath --relative-to="$HERE" "$a")"); done

"${DC[@]}" build perl go || exit 1
if [ -n "$PG" ]; then
    "${DC[@]}" --profile pg up -d --wait test-pg || exit 1
    trap '"${DC[@]}" --profile pg down >/dev/null 2>&1' EXIT
fi
"${DC[@]}" run --rm perl ./bench_perl.sh "${OPTS[@]}" "${ARGS[@]}"
PERL_STATUS=$?
"${DC[@]}" run --rm go ./bench_go.sh "${OPTS[@]}" "${ARGS[@]}"
GO_STATUS=$?

CMP=(-perl "$PERL_OUT" -go "$GO_OUT")
if [ -z "$SCENARIOS" ]; then
    CMP=(-perl /nonexistent -go /nonexistent)
fi
if [ -n "$UNITS" ]; then
    CMP+=(-units-perl "/conformance/results/bench-units-perl$SFX.json" -units-go "/conformance/results/bench-units-go$SFX.json")
fi
echo
"${DC[@]}" run --rm -w /gads-go go sh -c '
    go build -o /tmp/gads-bench ./cmd/gads-bench || exit 1
    /tmp/gads-bench compare "$@"; status=$?
    /tmp/gads-bench compare -md "$@" > /conformance/results/bench-report'"$PGS$SFX"'.md
    exit $status' sh "${CMP[@]}"
CMP_STATUS=$?
echo "perl=$PERL_STATUS go=$GO_STATUS compare=$CMP_STATUS (report: results/bench-report$PGS$SFX.md)"
exit $(( PERL_STATUS || GO_STATUS || CMP_STATUS ))
