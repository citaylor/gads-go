#!/bin/sh
# The dev service of docker-compose.yml: builds gads from the mounted source
# (/src) and runs it as the application container does (entrypoint.sh:
# configuration, seeding, admin password), against its own database
# (created if missing) on the same PostgreSQL server. Restart the service to
# rebuild after a change.
set -eu

: "${POSTGRES_HOST:=db}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=gads_dev}"
: "${POSTGRES_USER:=gads}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set}"
: "${GADS_VIEWS:=/upstream/GADS/views}"
: "${GADS_PUBLIC:=/upstream/GADS/public}"

case "$POSTGRES_DB" in
    *[!A-Za-z0-9_]* | "")
        echo "Invalid database name $POSTGRES_DB" >&2
        exit 1
        ;;
esac

out=/tmp/gads-dev
mkdir -p "$out/config/environments"
echo "Building gads from /src..."
cd /src
go build -trimpath -o "$out/gads" ./cmd/gads
# The configuration of the image, unless the source has a dev one
if [ -f /src/docker/config.dev.yml ]; then
    cp /src/docker/config.dev.yml "$out/config/config.yml"
else
    cp /src/docker/config.yml "$out/config/config.yml"
fi

# Uploaded files are kept in the container only (lost when it is recreated)
: "${GADS_UPLOADS:=$out/uploads}"
mkdir -p "$GADS_UPLOADS"
export GADS_UPLOADS
export PATH="$out:$PATH" GADS_CONFIG="$out/config" DANCER_ENVIRONMENT=docker \
    POSTGRES_HOST POSTGRES_PORT POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD
export PGHOST="$POSTGRES_HOST" PGPORT="$POSTGRES_PORT" PGUSER="$POSTGRES_USER" \
    PGPASSWORD="$POSTGRES_PASSWORD"

tries=0
until pg_isready -q -d postgres; do
    tries=$((tries + 1))
    if [ "$tries" -ge 60 ]; then
        echo "The database did not become ready" >&2
        exit 1
    fi
    sleep 1
done
if [ -z "$(psql -XtAd postgres -c "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")" ]; then
    echo "Creating the database $POSTGRES_DB..."
    psql -Xq -v ON_ERROR_STOP=1 -d postgres <<SQL
CREATE DATABASE "$POSTGRES_DB" ENCODING 'UTF8' TEMPLATE template0;
ALTER DATABASE "$POSTGRES_DB" SET timezone TO 'UTC';
ALTER DATABASE "$POSTGRES_DB" SET client_encoding TO 'UTF8';
SQL
fi

exec /src/docker/entrypoint.sh gads serve -listen :3000 -views "$GADS_VIEWS" -public "$GADS_PUBLIC"
