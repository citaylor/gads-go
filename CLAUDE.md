# GADS Go port: workspace guide

This workspace ports [ctrlo GADS](https://github.com/ctrlo/GADS), a Perl and
Dancer2 application, to Go. A language-neutral conformance suite proves that
the port behaves the same: every scenario runs against the original Perl
code and against Go, and the two sets of observations must be identical.
As of 2026-11-06, against GADS `dev` at `337d733c` (schema 115), all 190
scenarios are identical.

## Rules

- **Never modify the Perl GADS.** `upstream/GADS-dev` (the main line) and
  `upstream/GADS` (master) must run exactly as upstream ships them, including
  in benchmarks. Only the runtime patches applied by the test harness are
  allowed: `conformance/perl/lib/GADS/Conformance/Patches.pm` freezes the
  clock, makes JSON canonical and makes tokens predictable, and the runner
  (`Runner.pm`) captures emails, answers outgoing HTTP requests (the SMS
  gateway and Yubico, `outgoing_http`) instead of sending them, and makes
  MFA keys (`seed_key`) predictable. Offer any Perl
  fix separately, as a file in `conformance/patches/`.
- **Go reproduces Perl's behaviour, quirks included.** This covers messages,
  SQL shape, row order and bugs. A deliberate deviation must be commented
  and listed under "Deviations" below.
- **Perl bugs.** Record each bug found in the Perl GADS or its modules in
  `PERL-BUGS.md`, with where it was found and whether Go keeps it.
- **Secrets.**
  - `sudo-creds` holds the sudo password. Use it only as
    `sudo -S -p '' CMD < /home/ctaylor/ctrlo/sudo-creds` and never print it.
  - Never print `gads-go/.env`, which holds passwords. Its mode is 600.
  - Keep this parent directory out of Docker build contexts, because it
    contains `sudo-creds`.
- **Version control.** This directory is the git repository
  `github.com/citaylor/gads-go` (public, branch `main`, pushed over HTTPS
  with `gh`; rulesets let only the owner push, except Dependabot's branches). `.gitignore` keeps out `sudo-creds`, `.env`, binaries,
  caches and the regenerated results; only `results/perl`,
  `results/perl-pg` and `results/perl-decoy` are committed. Commit with the GitHub noreply address:
  pushes with a private email are refused.
- **Licences.** `LICENSE` is the AGPL text, unchanged. `NOTICE` lists
  GADS (and what is taken or generated from it) and every other work in
  the repository or the binary, with its licence. After changing
  `go.mod` or `NOTICE`, or adding anything from another project, update
  `NOTICE` and run `gads-go/third-party-licenses.sh`, which regenerates
  `gads-go/THIRD-PARTY-LICENSES` (NOTICE and the licence texts, installed
  in the image's `/usr/share/doc/gads`). CI checks that it is current.
- **CI.** `.github/workflows/ci.yml` runs on every push and weekly. It runs
  the tests (with parity and the hardened scenarios), the scenarios with
  a decoy site (`results/perl-decoy`), and the race detector,
  the linters of "Build and test" (versions pinned there), and on
  PostgreSQL the hardened scenarios and the tests of workers, stored
  sessions and forked imports (`internal/web` runs them on both
  databases when `GADS_TEST_PG` is set). Its `security` job runs gosec, gitleaks on the
  whole history, Trivy on the built image and the Docker files, and
  zizmor on the workflows and `dependabot.yml` (pinned releases checked
  by SHA-256), and sends gosec, Trivy and zizmor results to the Security
  tab, beside CodeQL. Keep it passing, and update its pins with the local
  tools. `scorecard.yml` runs OpenSSF Scorecard on pushes to `main` and
  weekly, and publishes the score.
- **Actions** are pinned to a commit with the version in a comment
  (`uses: actions/checkout@<sha> # v7.0.1`), and checkouts don't keep the
  token (`persist-credentials: false`). Dependabot updates the pins, a
  week after a release (`cooldown`). Check a workflow change with
  `zizmor --offline .github` (no findings).
- **Code scanning.** The repository is public. CodeQL (default setup),
  Dependabot, secret scanning and push protection are on. An alert that
  is not a defect is dismissed on GitHub with its reason; a gosec finding
  of that kind gets `// #nosec Gnnn -- reason` on its line.
- **Upstream submodules.** `upstream/GADS-dev` (branch `dev`) and
  `upstream/GADS` (branch `master`) are submodules of
  `https://github.com/ctrlo/GADS.git`, pinned to the commits the golden
  results come from. To move to a newer dev:
  1. `git -C upstream/GADS-dev pull`.
  2. Rerun Perl to refresh `results/perl` (and `run_perl.sh --decoy` for
     `results/perl-decoy`), then run Go and compare.
  3. If the schema changed, regenerate the DDL (from `gads-go/`:
     `perl -I ../upstream/GADS-dev/lib internal/db/gen/gen_ddl.pl`), port the
     migration of `share/migrations/*/upgrade` to `internal/db/upgrade.go`
     with a test from the previous DDL (`internal/db/testdata`).
  4. Commit the new submodule pointer together with the new golden results.

## Layout

| Path | What |
|---|---|
| `upstream/GADS-dev` | Perl GADS, `dev` branch: the reference. Pull updates from GitHub `ctrlo/GADS` dev. |
| `upstream/GADS` | Perl GADS master `571f2bd`, the old reference (results in `conformance/results/*-master*`). |
| `conformance/` | The suite. `SPEC.md` is the scenario format and the definition of every step. |
| `conformance/scenarios/*.yaml` | Scenarios. Many are generated: the `_generator:` key names the script in `generators/`. |
| `conformance/hardened/*.yaml` | Go-only scenarios for hardened mode (see Security). |
| `conformance/perl/` | Perl reference runner: `run.pl`, plus `lib/GADS/Conformance/{Runner,Web,Patches,Test}.pm`. |
| `conformance/results/perl` | Golden observations from Perl. `results/go` holds Go's; `-pg` suffixes are the PostgreSQL runs. |
| `gads-go/` | Go module `github.com/ctrlo/gads-go` (Go 1.27, cgo). |
| `GADS-security-report-verified.md` | Security review of the Perl GADS. Most findings also apply to Go (see Security). |
| `PERL-BUGS.md` | Every bug found in the Perl GADS (and its CPAN modules), with what Go does about it. Add each new one. |

Go packages, under `gads-go/`:

| Package | Contents |
|---|---|
| `internal/gads` | The engine (GADS::*): layout, columns, datums, records, search SQL, graphs, alerts, import, users, MFA. |
| `internal/web` | The Dancer2 application: router (`routes.go`, generated from GADS.pm and API.pm), `Ctx`, sessions, auth, routes. |
| `internal/tt` | Template Toolkit engine. It renders the original `views/*.tt` unchanged. |
| `internal/cli` | Ports of the `bin/*.pl` scripts. The `Commands` map is keyed by script name. |
| `internal/db` | Database access with the same DDL as GADS (`sql_sqlite.sql`, `sql_postgres.sql`, generated from `GADS::Schema` by `gen/gen_ddl.pl`). `Upgrade` applies GADS's schema migrations to an older database (`upgrade.go`). Drivers: mattn/go-sqlite3 (cgo), or `-tags modernc` for modernc.org/sqlite; pgx for PostgreSQL. |
| `internal/pdf` | Record and report PDFs: ports of CtrlO::PDF, PDF::Table and GADS::PDFGenerator, with the same text flow, wrapping, pagination and errors, and a PDF writer embedding Liberation Sans (Perl uses the system's). `testdata/perl_golden.pl` makes the Perl word positions the tests compare. |
| `internal/chrome` | Headless Chrome for `_page_as_mech` (WWW::Mechanize::Chrome): loads a page and prints it to PDF or captures a PNG, over the DevTools protocol on a pipe, with WMC's switches. |
| `internal/lua` | Lua 5.3.6, vendored and called through cgo, used for calc and RAG fields. Same sandbox as Inline::Lua in GADS. |
| `internal/perl`, `perljson`, `yamlxs`, `cldr`, `mimeinfo`, `saml` | Behaviour of Perl and CPAN modules (truthiness, JSON::PP, YAML::XS, CLDR dates, File::MimeInfo, Net::SAML2). |
| `internal/textcsv` | Text::CSV_PP's `getline` translated (the `binary => 1` options): the CSV reading of the data import, the user upload and `import-user`. A blank line is a row of one empty field, a lone CR ends lines from then on, and the first error ends the rows, as Perl's `while (getline)`. |
| `internal/autoformat` | Text::Autoformat and the parts of Text::Reform it uses, translated: the formatting of the plain text of every email (`GADS::Email::send`), applied by the mailer of `cmd/gads`. The runners capture mail before it, so the suite compares the text unformatted. |
| `internal/perltest` | Builds Go values from the typed form of the golden files (`{"f": IEEE bits}`, `{"i"}`, `{"s"}`, ...). |
| `conformance/` | Go runner and DataSheet port. `go test ./conformance` runs every scenario against the Perl golden. |
| `cmd/gads` | The application: `serve`, `deploy`, `list`, `<script>`. Also reads the config and sends mail. |
| `cmd/gads-conformance`, `cmd/gads-compare`, `cmd/gads-bench`, `cmd/gads-loadtest` | Suite runner, results comparison, benchmarks, load test of a server. |

## Environment

- Go 1.27.3 is in `~/.local/go` (1.27.2 is kept in `~/.local/go1.27.2`).
  Set `export PATH=$HOME/.local/go/bin:$HOME/go/bin:$PATH`. Linters are in
  `~/go/bin`; zizmor is in `~/.local/bin`.
- The Perl dependencies are in `~/perl5`, loaded with
  `eval "$(perl -I ~/perl5/lib/perl5 -Mlocal::lib)"`. The run scripts do
  this for you.
  - Inline::Lua is the ctrlo fork. Its build cache is at
    `~/.cache/gads-inline-dev`.
  - The dev branch needs extra modules: Authen::OATH, Convert::Base32,
    Imager::QRCode, Auth::Yubikey_WebClient, Test::TempDir::Tiny.
- PDFs: CtrlO::PDF needs Liberation Sans at
  `/usr/share/fonts/truetype/liberation/` (links to
  `/usr/share/fonts/liberation-sans/` here), and both runners observe
  PDFs with poppler's `pdftotext` (installed; CI installs poppler-utils).
- Chrome: GADS runs `/usr/bin/chromium` (here a link to
  `/usr/bin/google-chrome`). Scenarios that need it or `pdftotext` say so
  with `requires:`, and the Go and exported Perl tests skip them when
  either is missing. CI's runners have `/usr/bin/chromium`, so they run
  them; the workflow lifts Ubuntu's AppArmor limit on user namespaces
  (`kernel.apparmor_restrict_unprivileged_userns=0`) so that Chromium
  starts with its sandbox.
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
gosec -exclude=G104 -exclude-generated -exclude-dir=conformance \
  -exclude-dir=cmd/gads-bench -exclude-dir=cmd/gads-conformance \
  -exclude-dir=cmd/gads-compare ./...   # as CI: no issues (G104 is the errors Perl ignores)
go test ./...                           # unit tests, all scenarios against the Perl golden, and the hardened scenarios (about 25 s)
go test ./conformance -run 'TestConformance/205_'   # one scenario
```

Conformance suite (from `conformance/`):

```sh
./run_perl.sh scenarios/205_web_record.yaml   # Perl reference results -> results/perl
./run_go.sh   scenarios/205_web_record.yaml   # Go results -> results/go
./run_all.sh [FILTER]                         # Perl, then Go, then compare
./run_all.sh --pg [FILTER]                    # on PostgreSQL (starts docker-compose.test-pg.yml on :55432)
./run_all.sh --decoy [FILTER]                 # with a decoy site: checks that searches keep to the site
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
and `GADS_BENCH=1`; the load test of the servers is
`conformance/loadtest_docker.sh` (`cmd/gads-loadtest`, see
`gads-postgres-performance.md`). See SPEC.md "Benchmarks". Run nothing else that is heavy
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
| `GADS_HARDENED=1` | Go runner: run every scenario in hardened mode (see Security). |

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
  - `HTTPClient` makes the requests to other services (the SMS gateway and
    the Yubico API); the runner's transport answers them.
  - `NoFork` is GADS_NO_FORK (how a record write treats its alerts);
    `Fork` makes the engine of forked work (see Concurrency).
  - `IgnorePermissions` is the server-side admin context used by the
    scripts.
- **JSON for templates and the API goes through `gads.EncodeJSON`.** It
  matches JSON::PP: canonical, with no HTML escaping. The user decided to
  keep `encoding/json`, so don't propose replacing it.
- **TT values.**
  - Go slices given to templates must be a `tt.List`; a nil slice would
    loop once.
  - Lazy hashes (`ttLazy`) stand in for DBIC rows and objects.
- **Concurrency.** As Perl GADS runs worker processes, `gads serve`
  dispatches requests on a pool of engines (`Server.SetWorkers`; `gads:
  workers` or `GADS_WORKERS`, default 5 as Starman, at most 64). Each
  engine has its own database connection and Lua interpreter (made with
  `Engine.Fork`), and serves one request at a time: an engine is not safe
  for concurrent use. A request takes a free engine (`acquire`) only for
  its dispatch; static files, request bodies and responses are transferred
  outside it, and mail is sent by a background queue. Without
  `SetWorkers` (the conformance runner, the tests) there is one engine,
  `Server.E`.
  - Handlers use the request's engine, `c.E`, never `s.E` (which is only
    the first of the pool): anything holding an engine for later, such as
    a template object, takes `c.E`. `s.E.Config` is shared by all the
    engines and read only.
  - The requests of one session run one at a time (`Session.acquire`,
    taken before an engine, so that a request waiting for it holds none,
    and given up when the request ends), so that their changes to the
    session are not mixed.
  - Engines keep no data between requests, so that one sees at once what
    another wrote. Package-level caches and lazily made values need a lock
    or `sync.Once` (`regexCache`, the CLDR patterns, autoformat's
    `rePostClose`, `London`, `widgetTT`). `TestWorkersConcurrent` runs
    requests on several engines and sessions at once (with `-race`).
  - Engine flags changed for a while (`IgnorePermissions`,
    `IgnorePermissionsSearch`, as Perl's `local`) must be restored with
    `defer`, or a failure leaves the engine changed for its later
    requests. `resetEngine` also resets them, rolls back any open
    transaction and clears the reports around each request.
  - Anything that can run without end holds up a worker, so user code
    has limits (see Deviations).
  - The work that Perl forks a process for runs on a forked engine
    (`Engine.Fork`, `Engine.Background`): an engine of its own, with its
    own database connection and Lua interpreter, in a goroutine. The rows
    of a data import run there, so the upload returns at once. `gads
    serve` waits for them at exit (within its stop budget, below). Without `Fork` (the scripts, and the
    conformance runner, as GADS_NO_FORK) the work runs in process;
    `TestImportForked` runs the import scenarios forked. `gads serve`
    waits for those of every engine. On SQLite, as DBD::SQLite,
    transactions are `BEGIN IMMEDIATE` and a locked database is waited for
    30 s (`busy_timeout`), so that several connections write.
- **Dead code.** Keep `staticcheck` and `deadcode -test ./...` clean.
  `goLuaCallback` is a cgo export and deadcode can't see it, so keep
  what it calls inside it.
- Format with gofmt. Test files sit beside the code (`*_test.go`). Expected
  values are taken from Perl wherever possible.
- **Fuzz tests** (`fuzz_test.go`, `filter_fuzz_test.go`) cover the parsers
  of users' input: templates (`tt`), CSV (`textcsv`), CLDR dates,
  autoformat, Perl numification, and the filters (JSON and dates). They
  check that nothing panics or hangs. `go test` runs their seeds and the
  inputs once found (`testdata/fuzz/`); to fuzz one, `go test
  ./internal/tt -run '^$' -fuzz '^FuzzProcess$' -fuzztime 5m`. When a
  finding is Perl's behaviour too, keep it (as a seed) and fix the test.
- **Golden files from Perl.** `conformance/perl/gen_*_golden.pl` run the
  real Perl modules and write `testdata/golden.json`, which the Go tests
  compare with (the usage is at the top of each script; rerun it after
  changing its cases):

  | Script | Go test | Perl |
  |---|---|---|
  | `gen_perl_golden.pl` | `internal/perl` | numbers as strings, numification, `lc`, `substr`, Math::Round |
  | `gen_json_golden.pl` | `internal/perljson`, `gads.EncodeJSON` | JSON::PP |
  | `gen_yaml_golden.pl` | `internal/yamlxs` | YAML::XS (libyaml) |
  | `gen_tt_golden.pl` | `internal/tt` (`testdata/cases.txt`) | Template Toolkit |
  | `gen_lua_golden.pl` | `internal/lua` | GADS's `lua_run` (Inline::Lua) |
  | `gen_mime_golden.pl` | `internal/mimeinfo` (the globs of `testdata`) | File::MimeInfo |
  | `gen_autoformat_golden.pl` | `internal/autoformat` | Text::Autoformat (texts written for its rules, and random ones) |
  | `gen_csv_golden.pl` | `internal/textcsv` | Text::CSV getline (inputs written for its rules, and random ones) |

  Floats are passed by their IEEE bits, so that neither parser rounds
  them. Both runners compare JSON numbers as sent (the Go runner keeps
  their text with `json.Number`).

## Running the application

```sh
gads serve -config DIR [-listen :3000] [-views DIR] [-public DIR]
gads deploy -config DIR      # create the tables
gads upgrade -config DIR     # apply the schema upgrades an older database needs
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
  A session unused for `session_duration` (otherwise `cookie_duration`,
  otherwise 7 days) is dropped.
- Sessions are kept by the engine that `session` names. `Simple` (Dancer2's
  default) keeps them in memory, so a restart logs everyone out. `DBIC`
  (the Docker image's) keeps them in the table `sessions` of the database,
  which `serve` creates (`internal/web/session_db.go`), on a connection of
  their own, so that a session's changes are written whatever the
  request's transaction does. The rows are keyed by the SHA-256 of the
  session ID, so that a reader of the database cannot take over sessions.
  Each request reads its session again and writes it at its end if it
  changed (or a minute after its last write, for the idle expiry). The
  values keep their Go types (`encodeSession`); a value of another type
  is not saved, and logged. Other engines, such as GADS's example `YAML`,
  are kept in memory, with a warning.
- Request bodies are limited to 100 MB (`web.Config.MaxBodyBytes`); a
  larger request gets 413. Hardened, a request with neither a logged in
  user nor an `Authorization` header is limited to 1 MB.
- `serve` stops on SIGINT or SIGTERM within 170 s altogether (compose's
  `stop_grace_period` is 3m): requests in progress get 30 s, then the
  imports running and the queued emails the rest, the emails at least
  20 s. Engines that a request may still be using are not closed. The
  other commands deliver the queued emails before they exit (for up to a
  minute).
- Mail settings are in `gads: mail:` (via sendmail or smtp, host, port,
  security starttls/tls/none, username, password, helo, timeout).
- Uploaded files are stored on disk under `gads: uploads`.
- PDFs and images of pages (dashboards, `/print`, timelines, graphs) run
  Chromium: `gads: chromium` is its path (default `/usr/bin/chromium`,
  as Perl), and `chromium_no_sandbox: 1` runs it without its sandbox
  (set in the Docker image, whose seccomp profile does not allow it).
- `serve` listens on `-listen`, otherwise `GADS_LISTEN`, otherwise `:3000`.
- `serve` dispatches `GADS_WORKERS`, otherwise `gads: workers`, otherwise
  5 requests at once, each on an engine with its own database connection
  (see Concurrency): on PostgreSQL, keep `max_connections` above the
  workers plus the imports running.
- `serve` logs to stdout with `log/slog` (`cmd/gads/logging.go`,
  `internal/web/logging.go`):
  - The request log (`web.Config.AccessLogger`): one line per request with
    its ID, method, path (without query or reset code), status, bytes,
    duration, client, `user` when it came and `user_after` when that
    changed (login, logout). Static files are logged only when they fail;
    health checks are not logged. It ignores the log level, so that GADS's
    `production.yml` (`log: warning`) keeps it; `gads: access_log: 0`
    turns it off.
  - The error log (`web.Config.Logger`): each route error (a PANIC as
    `level=ERROR`, with the Go stack of a runtime panic) and other
    messages, at Dancer2's `log:` level (core/debug, info by default,
    warning, error).
  - `gads: log_format: json` writes JSON. Each response has
    `X-Request-ID`, taken from the proxy with `behind_proxy`. With both
    loggers nil (the conformance runner) nothing is logged or added.
- `GET /healthz` (no login) answers `ok` when an engine is free within
  10 s and its database answers, otherwise 503. One check runs at a time
  and its result is kept for 1 s, so calls can't queue for the engines.
  Each client address may make 60 checks a minute (then 429), hardened
  or not; Docker's own checks come from 127.0.0.1 and count apart.
  `gads healthcheck [-url URL]` checks it (the default URL follows
  `GADS_LISTEN`), for the image's `HEALTHCHECK`.
- On PostgreSQL, a connection the server dropped (a restart) is replaced
  outside a transaction and the statement run again, as DBIC does
  (`db.withConn`): only `driver.ErrBadConn`, where nothing was sent. The
  statement that finds it broken may fail.
- The security fixes are on unless `gads: hardened: 0` is set
  (`perl_password_hashes: 1` keeps Perl's hashes; see Security).

## Docker

Everything runs from `gads-go/`. The images take the views and public files
from the named build context `gads-upstream=../upstream/GADS-dev`; the
parent directory is never sent to the build.

| Service | Purpose |
|---|---|
| `db` | postgres:16-alpine, volume `db-data`. Not published to the host. |
| `app` | Image `gads-go:latest`, not published (port 3000 inside the network), volume `uploads` at `/var/lib/gads/uploads`. Seeds the database on first start. |
| `proxy` | `caddy:2.10.2-alpine` with `docker/Caddyfile`: HTTPS for `GADS_HOST` on `${GADS_HTTPS_PORT:-443}`, HTTP on `${GADS_HTTP_PORT:-80}` redirected, volumes `caddy-data` and `caddy-config`. Starts once `app` is healthy. |
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

- **Sessions** are in the database (`session: DBIC` in
  `docker/config.yml`), so a restart or a new image keeps users logged in.
- **Startup** (`docker/entrypoint.sh`):
  - Writes `environments/docker.yml` from `POSTGRES_*`, `SMTP_*`,
    `GADS_EMAIL_FROM` and `GADS_UPLOADS`.
  - Waits for the database.
  - Seeds it once, with `gads seed-database`; on later starts runs
    `gads upgrade`, which does nothing for a database that is up to date.
  - Sets the password of `GADS_ADMIN_EMAIL` from `GADS_ADMIN_PASSWORD`.
- **Health.** The image's `HEALTHCHECK` runs `gads healthcheck` every
  30 s, so `docker compose ps` shows `app` as healthy or unhealthy (a
  request holding the engine for over 10 s, or a lost database).
- **Configuration.**
  - The image's configuration is `docker/config.yml`. Mount a replacement at
    `/etc/gads/config.yml` to change it.
  - HTTPS is the `proxy` service, as Perl GADS runs behind a TLS web
    server. `GADS_TLS` chooses the certificate: `internal` (Caddy's own
    CA, the default), an email address (Let's Encrypt, for a public
    `GADS_HOST` with ports 80 and 443 open), or
    `/certs/cert.pem /certs/key.pem` (your own, in `docker/certs` or
    `GADS_CERTS`).
  - The entrypoint sets `behind_proxy: 1` (`GADS_BEHIND_PROXY`), the
    Secure session cookie (`GADS_SECURE_COOKIES`) and `gads: url`
    (`GADS_URL`, default `https://GADS_HOST/`, with `GADS_HTTPS_PORT` unless it is 443). Compose sets the first two
    to 1 for `app`; `dev` stays plain HTTP on :3001.
  - The app is not published, because with `behind_proxy` a client
    reaching it directly could forge `X-Forwarded-For`. Caddy replaces
    the header that a client sends.
  - A database seeded with another host needs
    `UPDATE site SET host = 'NEW'`, as GADS redirects other hosts.
- **Chromium.** `GADS_WITH_CHROMIUM=1 docker compose up -d --build`
  (build argument `WITH_CHROMIUM`) adds Chromium and its fonts, for the
  PDFs and images of pages; the image's config runs it without its
  sandbox. It is off by default, as it triples the image and Trivy fails
  CI on its unfixed issues.
- **Rebuilding.** A change to the Go code reaches the `app` container only
  after `docker compose up -d --build`.
- **Other compose files.**
  - `conformance/docker-compose.test-pg.yml` is the disposable PostgreSQL
    for `--pg` runs.
  - `conformance/docker-compose.bench.yml` has the runner containers:
    - `perl-ref` (image `gads-perl:reference`) is the Perl of the golden
      results; use it for conformance runs.
    - `perl` adds the XS backends (JSON::XS, Text::CSV_XS) for
      benchmarks. Its output differs from the golden results in places,
      for example in base64-encoded JSON.
    - `go`, and `test-pg` (profile `pg`).

    All of them mount the GADS checkout at `/upstream/GADS`, with
    `GADS_DIR` set to it. A newcomer's walkthrough is in
    `GETTING-STARTED.md`.

## Security

Changes made in the Go code that don't affect parity:

- Session cookie settings follow Dancer2: Secure, SameSite, duration, and an
  idle expiry.
- Server timeouts: read-header 30 s, read 10 min, idle 2 min.
- A request holds its engine for neither network I/O nor Chrome: a page
  is rendered with the engine, then loaded and printed after releasing
  it, at most two browsers at once and for at most two minutes each. Chrome is driven
  over a pipe, not WMC's port 9222 with `--remote-allow-origins=*`.
- Line breaks are stripped from mail address headers, so they can't add
  headers.
- Plain passwords are compared in constant time.
- Uploaded files are created with modes 0640 and 0750.
- Dependencies are kept clear of known vulnerabilities (`govulncheck`).
- The engine is reset around each request, and request bodies are limited
  (see Concurrency and Running the application).
- Mail is sent in the background, so a slow mail server holds up no
  request.
- User code is limited (Lua instructions and memory, regex match time,
  `working_days_add`), and the API token of a deleted user is refused
  (see Deviations).

Site isolation: as GADS::Schema restricts the resultsets of the tables
with a `site_id` to the request's site, Go adds `e.siteCond`/`siteWhere`
to every search of them, and finds users by ID within the site
(`FindSiteUser`; `FindUser` only through a relationship). `run_all.sh
--decoy` (SPEC.md "Decoy site") runs the suite with a copy of each site's
rows in another site, and `260_web_other_site` asks for another site's
user and group by ID: both are identical to Perl.

SAML responses are always rejected, as in Perl with Net::SAML2 0.89, so the
Perl authentication bypass (C-1) does not exist in Go.

**Hardened mode** (`gads.Config.Hardened`) fixes the other critical, high
and medium findings of `GADS-security-report-verified.md`. All of them were
re-checked against dev `fddd4a15` on 2026-10-28 and are still present in Perl.
Each fix is a deliberate deviation and is marked `Hardened (X-n)` in the
code.

- The application turns it on unless `gads: hardened: 0` is set. The engine
  default (off) is Perl's behaviour, which the conformance suite compares.
- `gads: perl_password_hashes: 1` keeps Perl's hashes, for a database shared
  with Perl GADS.
- Tests:
  - `conformance/hardened/*.yaml` (`hardened: 1`, Go only) attack each
    finding and check that it is refused. `go test ./conformance -run
    TestHardened` checks their expectations; they have no golden results.
  - `GADS_HARDENED=1` runs the whole suite hardened, to review what changes
    compared with Perl.

| Finding | Fix (web layer: `internal/web/hardening.go`) |
|---|---|
| H-1 CSRF | A GET request cannot change data: `DB.WriteCheck` allows only the writes of showing a page (audit, `session_settings`, default dashboards and RAG settings, submission tokens, the OAuth client, the colours of graph values, the SMS code of the MFA page, expired tokens), and mail fails. PUT, PATCH and DELETE need the token in a session. `/login` and `/mfa` take credentials and tokens only in a POST (a GET could not write the failures for the lockout). The cookie is SameSite=Lax unless configured. |
| H-2 | Pages printed by Chrome (`/print`, dashboards, timelines, graphs) load from a server of the public files on the loopback interface, at a random path, instead of `file://`: every other request is refused (`Fetch`), and a CSP keeps connections to that origin. `/print` needs the CSRF token, and its POSTs are rate-limited as M-7's. |
| H-3 | Only a superadmin changes or retires a superadmin (`User.CheckCanManage`). |
| H-4 | A historic purge takes only records that the user sees in this table's view, and the columns the page offers. |
| H-5 | Dashboards are changed by their own scope: the site's by superadmins, a table's with `layout` on that table, a personal one by its owner, who is also the only one who can read it (M-2). Notice HTML is sanitised (bluemonday). |
| M-1 | Graphs, reports, trees, field values and help text are found only in the URL's table, or among the fields the user can read. A view is found only in its own table (Perl's DBIC `find` ignores the instance), and a record dashboard's table widget checks its view with the referenced table's permissions. |
| M-3 | Only superadmins upload independent files. |
| M-4 | PBKDF2-SHA512 (210000 iterations, 91 characters), upgraded at login. The user's `encryption_key` changes with the hash, as it does on a password change. An unknown username at `/login` or the password grant takes the time of a PBKDF2 check, so that the time does not tell which exist. |
| M-5 | The password grant has the lockout and audit of `/login`. Retiring a user revokes their tokens. |
| M-7 | Pages are at most 1000 rows. POSTs to `/login`, `/register`, `/resetpw`, `/api/token` and `/print` are limited to 20 in any minute per address (a sliding window; 429). |
| M-8 | CSP with `object-src`, `base-uri` and `frame-ancestors` on every response, plus nosniff, Referrer-Policy and HSTS (with `is_secure`). |
| M-9 | A request for an existing account resends the pending reset code. Long names and notes are refused. |
| M-11 | CSV downloads quote a cell that starts with `= + - @`, a tab or a CR, unless it is a number. |

Not fixed:
- The OAuth client secret is stored in plain text, because
  `/clientcredentials` shows it.
- Tokens are not refused when the password has expired, because that
  would break API clients every 60 days.
- The other suggestions of H-3: notifying the old address, and
  re-authenticating.

## Deviations and not ported

- Not ported:
  - The scripts `update-many-records` (which doesn't compile in Perl),
    `api-example`, `export-pdf`, `migrate-db` (`gads upgrade` applies the
    upgrades instead), `seed-large`, and the shell scripts.
- Deviations:
  - Record and report PDFs use the Liberation Sans built into the binary.
    CtrlO::PDF loads it from
    `/usr/share/fonts/truetype/liberation/`, and fails without it.
  - A PDF's logo of more than 40 million pixels is refused ("Unable to
    identify image type: image of WxH pixels is too large"), before its
    pixels are decoded.
  - Import's final sort puts filvals last, in a stable order.
  - A script's `--site-id` that is not a whole number is an error
    ("Invalid site ID"). Perl binds it as given, so that it matches no
    rows; read as 0 it would mean every site (`import --purge` among
    others).
  - Hardened, `export` writes its files for the owner only (0600, and
    0700 directories), as `--include-data` exports password hashes.
  - SQLite has the extra index `record_idx_current_id_approval_created`,
    which avoids a quadratic search plan.
  - The SAML callback fails as Perl's does. Working verification would be an
    optional deviation.
  - Limits on user code, which could otherwise hold a worker without
    end (in Perl, a worker process):
    - Lua stops after about 1e9 instructions (about 4 s; "code stopped
      after running more than N instructions") and is limited to 512 MB
      while code runs ("not enough memory"). These are
      `lua.Interpreter.MaxInstructions` and `MaxMemory`. The
      instructions are counted only once a call has run for 100 ms
      (`CountAfter`): counting needs a Lua hook, which slows every
      instruction while set (loops by 1.6 times), so a timer sets it then,
      and usual code runs at Lua's full speed, as in Perl. Why this is
      fair to the benchmarks: `gads-postgres-performance.md`.
    - `working_days_add` with a negative count, or more than 100000 days,
      is an error. Perl's loop never ends for these.
    - A field's regex that runs for over 1 s counts as no match.
    - The steps of Lua's pattern matcher (`string.find`, `match`,
      `gmatch`, `gsub`) count as instructions (vendored `lstrlib.c`
      calls `gads_match_steps`), so a pattern that backtracks without
      end stops too (about 6 s).
    - A Lua table with a number key of ten million or more is an error
      ("table index N is over the limit of 10000000"): Inline::Lua makes
      an array up to the largest key, so that a key of 2^53 takes all
      memory. A key of minus ten million or less is dropped, as
      `av_store` drops a key before the array's start.
    - Converting a Lua value (a result, or a callback's arguments) stops
      at tables nested 200 deep ("tables nested over the limit of 200")
      or ten million values ("table values are over the limit of ..."):
      in Perl a table that contains itself, or is nested deeper than
      Lua's stack, crashes the worker, and one whose parts are shared
      takes time exponential in its depth.
    - A callback (`working_days_diff`, `working_days_add`) that fails
      ends the run, even inside a `pcall`, as Perl's `die` unwinds
      through Inline::Lua: the code stops at its next instruction.
  - Templates (`internal/tt`): templates, blocks and macros nested over 1000
    deep are a `file` error ("recursion into 'NAME' over 1000 levels"),
    where Perl recurses until it runs out of memory and Go's stack would
    overflow; a list item assigned at an index of 2^24 or more, or a
    list of 2^24 items or more from `first`, `last` or `slice` (which
    pad with undef), is "Out of memory!" at once. The views do neither.
  - Lua tables with a key that is not a number or a string (a boolean)
    fail with Inline::Lua's "Illegal type (boolean) in table subscript",
    without its place in Perl. Inline::Lua's Lua state is then broken, so
    every later call fails; Go's is not.
  - Perl numifies an integer string above 2^63 to an unsigned integer
    (UV); `perl.NumifyTyped` gives a float.
  - JSON::PP writes a whole float from 2^53 to 2^64 as a number or as a
    string, depending on the values encoded before it; Go always writes a
    number.
  - Sessions expire after 7 days unused when no duration is configured.
  - The `DBIC` session engine's table, `sessions`, is not part of GADS's
    schema (as with Dancer2::Session::DBIC, which needs a table of the
    application's own), and has a `last_active` column for the expiry.
  - The MFA hook finds the logged in user in the request's site (the
    site is set before it, its redirect still made after it). Perl's
    finds the user in the site of the worker's previous request, so a
    user of the same username in another site could decide whether MFA
    is needed. `conformance/hardened/site_mfa_user.yaml` tests it.
  - The requests of one session run one at a time. With Perl's YAML
    sessions, two at once can each overwrite the other's changes.
  - A CSV download without the download permission redirects with the
    error. Perl has already started its streamed response, so the user
    gets an empty, unfinished download and the error goes to the log.
  - The bearer token of a deleted user, or of an account request, is
    refused. Perl accepts it until it expires.
  - Savepoints need a transaction, as in DBIC. This is not a deviation:
    Go used to begin a transaction that was never committed.
  - Hardened mode, on in the application: see Security.
  - `/healthz` is answered before routing, so it shadows a table whose
    short name is `healthz`.
- PostgreSQL parity (`run_all.sh --pg`, dev `337d733c`, 2026-11-06) is
  189 of 190. The difference is in `013_datatime`, in searches whose sort
  leaves ties, which PostgreSQL returns in the order of its plan: two
  runs of Perl differ there too. The runners move the sequences past the
  fixtures' explicit IDs (SPEC.md "PostgreSQL"). Failed expectations are
  expected there, as the scenarios' expectations are SQLite's.
