#!/bin/sh
# Entrypoint of the GADS container: writes the database configuration from
# the environment, waits for PostgreSQL, seeds the database on first start
# (as bin/docker.sh of GADS) and then runs the command (gads serve).
set -eu

: "${POSTGRES_HOST:=db}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=gads}"
: "${POSTGRES_USER:=gads}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set}"
: "${GADS_HOST:=localhost}"
: "${GADS_INSTANCE_NAME:=datasheet}"
: "${GADS_ADMIN_EMAIL:=admin@localhost}"
: "${GADS_ADMIN_PASSWORD:=}"

# A YAML single-quoted string
yq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }

# The DBIC section, merged over config.yml (DANCER_ENVIRONMENT=docker)
cat > "$GADS_CONFIG/environments/$DANCER_ENVIRONMENT.yml" <<EOF
# Written by the container entrypoint from the environment
plugins:
  DBIC:
    default:
      dsn: $(yq "dbi:Pg:database=$POSTGRES_DB;host=$POSTGRES_HOST;port=$POSTGRES_PORT")
      user: $(yq "$POSTGRES_USER")
      password: $(yq "$POSTGRES_PASSWORD")
EOF
# The sender and transport of emails: SMTP if SMTP_HOST is set, otherwise
# sendmail (which the image does not include)
# and the directory of uploaded files if GADS_UPLOADS is set
if [ -n "${GADS_EMAIL_FROM:-}" ] || [ -n "${SMTP_HOST:-}" ] || [ -n "${GADS_UPLOADS:-}" ]; then
    {
        echo "gads:"
        if [ -n "${GADS_UPLOADS:-}" ]; then
            echo "  uploads: $(yq "$GADS_UPLOADS")"
        fi
        if [ -n "${GADS_EMAIL_FROM:-}" ]; then
            echo "  email_from: $(yq "$GADS_EMAIL_FROM")"
        fi
        if [ -n "${SMTP_HOST:-}" ]; then
            echo "  mail:"
            echo "    via: smtp"
            echo "    host: $(yq "$SMTP_HOST")"
            if [ -n "${SMTP_PORT:-}" ]; then echo "    port: $(yq "$SMTP_PORT")"; fi
            if [ -n "${SMTP_SECURITY:-}" ]; then echo "    security: $(yq "$SMTP_SECURITY")"; fi
            if [ -n "${SMTP_USERNAME:-}" ]; then echo "    username: $(yq "$SMTP_USERNAME")"; fi
            if [ -n "${SMTP_PASSWORD:-}" ]; then echo "    password: $(yq "$SMTP_PASSWORD")"; fi
            if [ -n "${SMTP_HELO:-}" ]; then echo "    helo: $(yq "$SMTP_HELO")"; fi
        fi
    } >> "$GADS_CONFIG/environments/$DANCER_ENVIRONMENT.yml"
fi
chmod 600 "$GADS_CONFIG/environments/$DANCER_ENVIRONMENT.yml"

export PGHOST="$POSTGRES_HOST" PGPORT="$POSTGRES_PORT" PGDATABASE="$POSTGRES_DB" \
    PGUSER="$POSTGRES_USER" PGPASSWORD="$POSTGRES_PASSWORD"

echo "Waiting for the database at $PGHOST:$PGPORT..."
tries=0
until pg_isready -q; do
    tries=$((tries + 1))
    if [ "$tries" -ge 60 ]; then
        echo "The database did not become ready" >&2
        exit 1
    fi
    sleep 1
done

seeded=0
if [ -z "$(psql -XtAc "SELECT to_regclass('public.site')")" ] \
    || [ "$(psql -XtAc 'SELECT COUNT(*) FROM site')" = 0 ]; then
    echo "Seeding the database..."
    gads seed-database --site "$GADS_HOST" --instance_name "$GADS_INSTANCE_NAME" \
        --initial_username "$GADS_ADMIN_EMAIL" < /dev/null
    seeded=1
fi

if [ "$seeded" = 1 ]; then
    if [ -n "$GADS_ADMIN_PASSWORD" ]; then
        # The password as Crypt::SaltedHash SSHA512 (4 byte salt), as GADS
        # stores it
        tmp=$(mktemp -d)
        head -c 4 /dev/urandom > "$tmp/salt"
        { printf '%s' "$GADS_ADMIN_PASSWORD"; cat "$tmp/salt"; } \
            | openssl dgst -sha512 -binary > "$tmp/digest"
        hash="{SSHA512}$(cat "$tmp/digest" "$tmp/salt" | openssl base64 -A)"
        rm -rf "$tmp"
        psql -X -q -v ON_ERROR_STOP=1 -v pw="$hash" -v u="$GADS_ADMIN_EMAIL" <<'SQL'
UPDATE "user" SET password = :'pw', pwchanged = NOW(), account_request = 0
    WHERE username = :'u';
SQL
        echo "Set the password of $GADS_ADMIN_EMAIL"
    else
        echo "GADS_ADMIN_PASSWORD is not set: use the password reset of the login page for $GADS_ADMIN_EMAIL" >&2
    fi
fi

exec "$@"
