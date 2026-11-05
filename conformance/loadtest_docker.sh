#!/bin/bash
# The load test of gads-postgres-performance.md ("Under load"): Perl GADS
# under Starman and Go's gads serve, each with 5 workers, on copies of the
# same PostgreSQL database (the 1000 records of bench/b04_web.yaml), one
# after the other, measured by cmd/gads-loadtest:
#   ./loadtest_docker.sh [-c 1,5,10,20] [-d 60s]
# Perl runs in gads-perl-bench with YAML sessions (GADS's
# config.yml-example), Go in gads-go-builder with its sessions in the
# database and hardened: 0, both on the network of the test PostgreSQL
# (docker-compose.test-pg.yml). GADS_SRC is the GADS checkout (default
# ../upstream/GADS-dev). Run nothing else heavy meanwhile.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"
export PATH="$HOME/.local/go/bin:$PATH"
COUNTS=1,5,10,20
DUR=60s
while [ $# -gt 0 ]; do
    case "$1" in
        -c) COUNTS="$2"; shift ;;
        -d) DUR="$2"; shift ;;
        *) echo "usage: $0 [-c 1,5,10,20] [-d 60s]" >&2; exit 2 ;;
    esac
    shift
done
GADS_SRC="$(realpath "${GADS_SRC:-../upstream/GADS-dev}")"
GO_DIR="$(realpath ../gads-go)"
PG=(docker compose -f "$HERE/docker-compose.test-pg.yml")
BENCH=(docker compose -f "$HERE/docker-compose.bench.yml")
NET=gads-test-pg_default
WORK="$(mktemp -d)"
SERVER_BIN="$GO_DIR/bin/gads-loadtest-server"
psql_() { "${PG[@]}" exec -T test-pg psql -q -v ON_ERROR_STOP=1 -U gads_test -d "$1" -c "$2"; }
cleanup() {
    docker rm -f gads-lt-perl gads-lt-go >/dev/null 2>&1 || true
    for db in gads_lt_perl gads_lt_go ${SRC:-}; do
        psql_ postgres "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 || true
    done
    rm -rf "$WORK" "$SERVER_BIN"
}
trap cleanup EXIT

"${PG[@]}" up -d --wait >/dev/null
"${BENCH[@]}" build perl go >/dev/null

echo "Building the data (bench/b04_web.yaml)..."
(cd "$GO_DIR" && GADS_TEST_PG="host=127.0.0.1 port=55432 user=gads_test" PGPASSWORD=gads_test \
    GADS_TEST_PG_KEEP=1 go run ./cmd/gads-conformance -out "$WORK/results" \
    "$HERE/bench/b04_web.yaml" >/dev/null 2>"$WORK/runner.err")
SRC="$(sed -n 's/^Kept database \(.*\)$/\1/p' "$WORK/runner.err" | head -1)"
if [ -z "$SRC" ]; then
    cat "$WORK/runner.err" >&2
    exit 1
fi
for db in gads_lt_perl gads_lt_go; do
    psql_ postgres "CREATE DATABASE $db TEMPLATE $SRC"
    # The runner's clock is in 2014: the passwords would have expired
    psql_ "$db" 'UPDATE "user" SET pwchanged = now()'
done

echo "Building gads and gads-loadtest..."
"${BENCH[@]}" run --rm -w /gads-go go go build -o bin/gads-loadtest-server ./cmd/gads >/dev/null
(cd "$GO_DIR" && go build -o "$WORK/gads-loadtest" ./cmd/gads-loadtest)

mkdir -p "$WORK/perl" "$WORK/go" "$WORK/sessions"
chmod 777 "$WORK/sessions"
sed -e 's|dsn: dbi:Pg:.*|dsn: dbi:Pg:database=gads_lt_perl;host=test-pg|' \
    -e 's|user: dbuser|user: gads_test|' -e 's|password: dbpass|password: gads_test|' \
    -e 's|session_dir: .*|session_dir: "/sessions"|' -e 's|is_secure: 1.*||' \
    "$GADS_SRC/config.yml-example" > "$WORK/perl/config.yml"
sed -e 's|  # hardened: 1|  hardened: 0\n  access_log: 0\n  workers: 5|' \
    -e 's|uploads: /var/lib/gads/uploads|uploads: /tmp/uploads|' \
    -e 's|      schema_class: GADS::Schema|      dsn: dbi:Pg:dbname=gads_lt_go;host=test-pg\n      user: gads_test\n      password: gads_test\n      schema_class: GADS::Schema|' \
    "$GO_DIR/docker/config.yml" > "$WORK/go/config.yml"

USER_ID="$(id -u):$(id -g)"
docker run -d --name gads-lt-perl --network "$NET" -p 127.0.0.1:3101:3000 --user "$USER_ID" \
    -v "$GADS_SRC:/upstream/GADS:ro" -v "$WORK/perl:/conf:ro" -v "$WORK/sessions:/sessions" \
    -v gads-bench_perl-inline:/cache/inline -e DANCER_CONFDIR=/conf -e DANCER_ENVIRONMENT=none \
    -w /upstream/GADS gads-perl-bench:latest starman --workers 5 --listen :3000 bin/app.psgi >/dev/null
docker run -d --name gads-lt-go --network "$NET" -p 127.0.0.1:3102:3000 --user "$USER_ID" \
    -v "$SERVER_BIN:/usr/local/bin/gads:ro" -v "$GADS_SRC:/upstream/GADS:ro" -v "$WORK/go:/conf:ro" \
    gads-go-builder:latest gads serve -config /conf -views /upstream/GADS/views \
    -public /upstream/GADS/public -listen :3000 >/dev/null

for port in 3101 3102; do
    for _ in $(seq 120); do
        curl -s -o /dev/null -H 'Host: localhost' "http://127.0.0.1:$port/login" && break
        sleep 1
    done
done

for app in "Perl (Starman):3101" "Go (gads serve):3102"; do
    echo "== ${app%:*}"
    "$WORK/gads-loadtest" -url "http://127.0.0.1:${app##*:}" -user user1@example.com \
        -sheet table1 -record 51 -c "$COUNTS" -d "$DUR"
done
echo "== Memory"
docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' gads-lt-perl gads-lt-go
