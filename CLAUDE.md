# GADS Go port: workspace guide

This workspace ports [ctrlo GADS](https://github.com/ctrlo/GADS), a Perl and
Dancer2 application, to Go. A language-neutral conformance suite proves that
the port behaves the same: every scenario runs against the original Perl
code and against Go, and the two sets of observations must be identical.
As of 2026-10-27, against GADS `dev` at `fddd4a15`, all 176 scenarios are
identical.

## Rules

- **Never modify the Perl GADS.** `upstream/GADS-dev` (the main line) and
  `upstream/GADS` (master) must run exactly as upstream ships them, including
  in benchmarks. Only the runtime patches applied by the test harness are
  allowed: `conformance/perl/lib/GADS/Conformance/Patches.pm` freezes the
  clock, makes JSON canonical and makes tokens predictable. Offer any Perl
  fix separately, as a file in `conformance/patches/`.
- **Go reproduces Perl's behaviour, quirks included.** This covers messages,
  SQL shape, row order and bugs. A deliberate deviation must be commented
  and listed under "Deviations" below.
- **Secrets.**
  - `sudo-creds` holds the sudo password. Use it only as
    `sudo -S -p '' CMD < /home/ctaylor/ctrlo/sudo-creds` and never print it.
  - Never print `gads-go/.env`, which holds passwords. Its mode is 600.
  - Keep this parent directory out of Docker build contexts, because it
    contains `sudo-creds`.
- **Version control.** This directory is the git repository
  `github.com/citaylor/gads-go` (private, branch `main`, pushed over HTTPS
  with `gh`). `.gitignore` keeps out `sudo-creds`, `.env`, `upstream/`
  (separate clones of ctrlo/GADS, see README.md), binaries, caches and the
  regenerated results; only `results/perl` and `results/perl-pg` are
  committed. Commit with the GitHub noreply address: pushes with a private
  email are refused.

## Layout

| Path | What |
|---|---|
| `upstream/GADS-dev` | Perl GADS, `dev` branch: the reference. Pull updates from GitHub `ctrlo/GADS` dev. |
| `upstream/GADS` | Perl GADS master `571f2bd`, the old reference (results in `conformance/results/*-master*`). |
| `conformance/` | The suite. `SPEC.md` is the scenario format and the definition of every step. |
| `conformance/scenarios/*.yaml` | Scenarios. Many are generated: the `_generator:` key names the script in `generators/`. |
| `conformance/perl/` | Perl reference runner: `run.pl`, plus `lib/GADS/Conformance/{Runner,Web,Patches,Test}.pm`. |
| `conformance/results/perl` | Golden observations from Perl. `results/go` holds Go's; `-pg` suffixes are the PostgreSQL runs. |
| `gads-go/` | Go module `github.com/ctrlo/gads-go` (Go 1.27, cgo). |
| `GADS-security-report-verified.md` | Security review of the Perl GADS. Most findings also apply to Go (see Security). |

Go packages, under `gads-go/`:

| Package | Contents |
|---|---|
| `internal/gads` | The engine (GADS::*): layout, columns, datums, records, search SQL, graphs, alerts, import, users, MFA. |
| `internal/web` | The Dancer2 application: router (`routes.go`, generated from GADS.pm and API.pm), `Ctx`, sessions, auth, routes. |
| `internal/tt` | Template Toolkit engine. It renders the original `views/*.tt` unchanged. |
| `internal/cli` | Ports of the `bin/*.pl` scripts. The `Commands` map is keyed by script name. |
| `internal/db` | Database access with the same DDL as GADS (`sql_sqlite.sql`, `sql_postgres.sql`). Drivers: mattn/go-sqlite3 (cgo), or `-tags modernc` for modernc.org/sqlite; pgx for PostgreSQL. |
| `internal/lua` | Lua 5.3.6, vendored and called through cgo, used for calc and RAG fields. Same sandbox as Inline::Lua in GADS. |
| `internal/perl`, `perljson`, `yamlxs`, `cldr`, `mimeinfo`, `saml` | Behaviour of Perl and CPAN modules (truthiness, JSON::PP, YAML::XS, CLDR dates, File::MimeInfo, Net::SAML2). |
| `conformance/` | Go runner and DataSheet port. `go test ./conformance` runs every scenario against the Perl golden. |
| `cmd/gads` | The application: `serve`, `deploy`, `list`, `<script>`. Also reads the config and sends mail. |
| `cmd/gads-conformance`, `cmd/gads-compare`, `cmd/gads-bench` | Suite runner, results comparison, benchmarks. |

## Environment

- Go 1.27.3 is in `~/.local/go` (1.27.2 is kept in `~/.local/go1.27.2`).
  Set `export PATH=$HOME/.local/go/bin:$HOME/go/bin:$PATH`. Linters are in
  `~/go/bin`.
- The Perl dependencies are in `~/perl5`, loaded with
  `eval "$(perl -I ~/perl5/lib/perl5 -Mlocal::lib)"`. The run scripts do
  this for you.
  - Inline::Lua is the ctrlo fork. Its build cache is at
    `~/.cache/gads-inline-dev`.
  - The dev branch needs extra modules: Authen::OATH, Convert::Base32,
    Imager::QRCode, Auth::Yubikey_WebClient, Test::TempDir::Tiny.
- Docker CE runs as a system service, and `ctaylor` is in the `docker`
  group. In an older login session, use `sg docker -c '...'`. The host's own
  PostgreSQL is disabled; use the containers instead.

## Build and test

```sh
cd gads-go
go build ./...                          # everything (cgo)
go build -o bin/gads ./cmd/gads         # the application binary
go vet ./... && staticcheck ./...       # staticcheck.conf disables ST1005 and SA6005, with reasons
govulncheck ./...                       # must report no vulnerabilities
go test ./...                           # unit tests, plus all scenarios against the Perl golden (about 25 s)
go test ./conformance -run 'TestConformance/205_'   # one scenario
```

Conformance suite (from `conformance/`):

```sh
./run_perl.sh scenarios/205_web_record.yaml   # Perl reference results -> results/perl
./run_go.sh   scenarios/205_web_record.yaml   # Go results -> results/go
./run_all.sh [FILTER]                         # Perl, then Go, then compare
./run_all.sh --pg [FILTER]                    # on PostgreSQL (starts docker-compose.test-pg.yml on :55432)
(cd ../gads-go && go run ./cmd/gads-compare -perl ../conformance/results/perl -go ../conformance/results/go)
```

- Pass scenario paths, not bare names.
- Each Perl run uses its own database, so several runs can go in parallel.
- Don't edit Go code while a full Go run is in progress, because `go run`
  rebuilds the code for every scenario.

To add or change a scenario:

1. Edit the generator, or the YAML for a scenario without one.
2. Run Perl to refresh the golden results.
3. Run Go and compare.

When a new step type or observation is added, implement it in both runners
(`perl/lib/GADS/Conformance/Runner.pm` and `gads-go/conformance/steps*.go`)
and document it in `SPEC.md`. A scenario's expectations are optional; the
comparison with the golden results is what proves parity.

Standalone Perl tests, to copy into any GADS checkout:

```sh
conformance/export_perl_tests.sh /path/to/GADS    # writes t/conformance (one .t per scenario + golden results)
cd /path/to/GADS && prove -r -j8 t/conformance     # GADS_CONFORMANCE_NO_GOLDEN=1: expectations only
```

Benchmarks: `conformance/bench_all.sh [--pg] [--time]`, `bench_docker.sh`,
and `GADS_BENCH=1`. See SPEC.md "Benchmarks". Run nothing else that is heavy
at the same time.

Debugging environment variables:

| Variable | Effect |
|---|---|
| `GADS_SQL_TRACE=1` | Go prints its SQL (the Perl equivalent is `DBIC_TRACE=1`). |
| `GADS_STACK=1` | Prints the Go stack of a runtime panic. |
| `GADS_WEB_ERRORS=1` | Logs web errors to stderr. |
| `GADS_TRACE=1` | SQL trace in the Go conformance runner. |
| `GADS_DIR` | GADS checkout for views and public files (the default is `upstream/GADS-dev`). |
| `GADS_TEST_PG` | libpq connection string: the runners create one database per scenario. |
| `GADS_RAW_ERRORS` | Perl runner: unformatted errors. |

## Conventions in the Go code

- **Mirror the Perl structure.**
  - One Go type per Perl class, with Perl's method names in Go form.
  - Doc comments name the original, for example
    `// FilevalCreate is ResultSet::Fileval create_with_file`.
  - Comments explain Perl behaviour that the Go reproduces: "(as DBIC ...)",
    "Perl quirk: ...".
- **Errors follow Log::Report.**
  - `gads.Fail` is `error`: the user sees the message.
  - `gads.Panic` is `panic`: an internal error, shown as "unexpected".
  - `gads.Try(func())` is `try {}` and returns an `*gads.Error`.
  - `e.Mistake`, `e.Warn` and `e.Notice` report without stopping; the web
    layer flushes them into session messages.
  - Message text must equal Perl's, including capitalisation and
    punctuation, so staticcheck's ST1005 is disabled.
- **SQL keeps DBIC's shape.**
  - Join aliases are `value`, `value_2` and so on, with
    `GROUP BY me.id ... ORDER BY MIN/MAX`. This keeps SQLite's row order
    identical to Perl's.
  - Values always go through `?` bind parameters.
  - Identifiers are quoted with `db.Q` or `q()` and come from the layout,
    never from raw input.
- **Determinism hooks** on `gads.Engine`:
  - `Clock`, `TokenGenerator`, `PasswordGenerator` and `SAMLIDGenerator`
    are replaced by the runner.
  - `NoFork` is GADS_NO_FORK.
  - `IgnorePermissions` is the server-side admin context used by the
    scripts.
- **JSON for templates and the API goes through `gads.EncodeJSON`.** It
  matches JSON::PP: canonical, with no HTML escaping. The user decided to
  keep `encoding/json`, so don't propose replacing it.
- **TT values.**
  - Go slices given to templates must be a `tt.List`; a nil slice would
    loop once.
  - Lazy hashes (`ttLazy`) stand in for DBIC rows and objects.
- **Concurrency.** One `gads.Engine` serves all requests behind a single
  mutex (`Server.mu`), like a one-worker Dancer2. Only dispatch holds the
  lock; static files, request bodies and responses are transferred outside
  it. The engine is not safe for concurrent use.
- **Dead code.** Keep `staticcheck` and `deadcode -test ./...` clean.
  `goLuaCallback` is a cgo export and deadcode can't see it.
- Format with gofmt. Test files sit beside the code (`*_test.go`). Expected
  values are taken from Perl wherever possible.

## Running the application

```sh
gads serve -config DIR [-listen :3000] [-views DIR] [-public DIR]
gads deploy -config DIR      # create the tables
gads list                    # list the scripts
gads <script> [options]      # a bin/*.pl port (export.pl or export); GADS_CONFIG=DIR
```

Configuration works as in Dancer2:

- `DIR/config.yml`, with `DIR/environments/$DANCER_ENVIRONMENT.yml` merged
  over it.
- The database comes from `plugins: DBIC: default: dsn`. `dbi:SQLite:FILE`
  or `dbi:Pg:...` is mapped to sqlite or pgx.
- The session cookie comes from `engines: session: <session engine>:`:
  `cookie_name`, `cookie_path`, `cookie_domain`, `cookie_duration`,
  `is_secure`, `is_http_only`, `cookie_same_site`, `session_duration`.
  Sessions are kept in memory, so a restart logs everyone out. Without a
  duration they are kept until the process ends.
- Mail settings are in `gads: mail:` (via sendmail or smtp, host, port,
  security starttls/tls/none, username, password, helo, timeout).
- Uploaded files are stored on disk under `gads: uploads`.

## Docker

Everything runs from `gads-go/`. The images take the views and public files
from the named build context `gads-upstream=../upstream/GADS-dev`; the
parent directory is never sent to the build.

| Service | Purpose |
|---|---|
| `db` | postgres:16-alpine, volume `db-data`. Not published to the host. |
| `app` | Image `gads-go:latest` on `${GADS_PORT:-3000}`, volume `uploads` at `/var/lib/gads/uploads`. Seeds the database on first start. |
| `builder` (profile `build`) | Image `gads-go-builder`: Go and gcc, with the source mounted at `/src`, and the conformance suite and GADS-dev mounted read-only. |
| `dev` (profile `build`) | Builds from the mounted source and runs on `${GADS_DEV_PORT:-3001}` with its own database, `gads_dev`. |

```sh
cp .env.example .env                       # then set the passwords (mode 600)
docker compose up -d --build               # build the image and start db + app
docker compose logs -f app
docker compose ps
docker compose down                        # stop (keeps the volumes)
docker compose down -v                     # also DELETES the database and uploads
docker compose run --rm builder            # build bin/gads in the container
docker compose run --rm builder go test ./...
docker compose --profile build up -d dev   # the source version on :3001
docker compose restart dev                 # rebuild and rerun after a change
```

- **Startup** (`docker/entrypoint.sh`):
  - Writes `environments/docker.yml` from `POSTGRES_*`, `SMTP_*`,
    `GADS_EMAIL_FROM` and `GADS_UPLOADS`.
  - Waits for the database.
  - Seeds it once, with `gads seed-database`.
  - Sets the password of `GADS_ADMIN_EMAIL` from `GADS_ADMIN_PASSWORD`.
- **Configuration.**
  - The image's configuration is `docker/config.yml`. Mount a replacement at
    `/etc/gads/config.yml` to change it.
  - For HTTPS, set `engines: session: Simple: is_secure: 1`. Behind a proxy,
    also set `behind_proxy: 1`.
- **Rebuilding.** A change to the Go code reaches the `app` container only
  after `docker compose up -d --build`.
- **Other compose files.**
  - `conformance/docker-compose.test-pg.yml` is the disposable PostgreSQL
    for `--pg` runs.
  - `conformance/docker-compose.bench.yml` runs the Perl and Go benchmark
    containers.

## Security

Changes made in the Go code that don't affect parity:

- Session cookie settings follow Dancer2: Secure, SameSite, duration, and an
  idle expiry.
- Server timeouts: read-header 30 s, read 10 min, idle 2 min.
- The engine lock no longer covers network I/O.
- Line breaks are stripped from mail address headers, so they can't add
  headers.
- Plain passwords are compared in constant time.
- Uploaded files are created with modes 0640 and 0750.
- Dependencies are kept clear of known vulnerabilities (`govulncheck`).

SAML responses are always rejected, as in Perl with Net::SAML2 0.89, so the
Perl authentication bypass (C-1) does not exist in Go.

The other findings of `GADS-security-report-verified.md` belong to GADS
itself, and **Go reproduces them** for parity:

- CSRF is checked only on POST (H-1).
- Object lookups are not scoped to their table (H-4, H-5, M-1).
- A useradmin can edit superadmins (H-3).
- Password hashing is weak: SSHA512 with a 4-byte salt, kept so that the
  database is shared with Perl (M-4).
- There is CSV formula injection (M-11).

Fixing these is a deliberate deviation from Perl. Decide on each one,
implement it in Go, and record it below. A further Go-specific risk: a calc
field with an endless Lua loop blocks every request, because all requests
share the one engine lock. A Lua instruction limit would fix it.

## Deviations and not ported

- Not ported:
  - PDF output: PDFGenerator, and record, report and dashboard PDFs.
  - SMS and Yubikey network calls. Only the "not configured" paths exist.
  - The scripts `update-many-records` (which doesn't compile in Perl),
    `api-example`, `export-pdf`, `migrate-db`, `seed-large`, and the shell
    scripts.
- Deviations:
  - Import's final sort puts filvals last, in a stable order.
  - SQLite has the extra index `record_idx_current_id_approval_created`,
    which avoids a quadratic search plan.
  - The SAML callback fails as Perl's does. Working verification would be an
    optional deviation.
- PostgreSQL parity is 160 of 166 (master era). The remaining differences
  are Perl's own plan-dependent row order and fixture ID collisions.
