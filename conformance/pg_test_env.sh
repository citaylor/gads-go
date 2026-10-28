# Sourced by run_perl.sh, run_go.sh, run_all.sh and the bench scripts for
# their --pg option: points the runners at a disposable PostgreSQL server.
# * GADS_TEST_PG already set: that server is used as it is.
# * GADS_TEST_PG_HOST set (in the containers of docker-compose.bench.yml):
#   the test-pg service on the containers' network.
# * otherwise the container of docker-compose.test-pg.yml is started if it
#   is not running, on 127.0.0.1:55432.
if [ -z "${GADS_TEST_PG:-}" ]; then
    if [ -n "${GADS_TEST_PG_HOST:-}" ]; then
        export GADS_TEST_PG="host=$GADS_TEST_PG_HOST port=5432 user=gads_test"
    else
        if ! docker compose -f "$HERE/docker-compose.test-pg.yml" up -d --wait >&2; then
            echo "Could not start the test PostgreSQL container (is Docker running, and are you in the docker group?)" >&2
            exit 1
        fi
        export GADS_TEST_PG="host=127.0.0.1 port=55432 user=gads_test"
    fi
    export PGPASSWORD=gads_test
fi
