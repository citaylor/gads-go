# GADS Go port

[![CI](https://github.com/citaylor/gads-go/actions/workflows/ci.yml/badge.svg)](https://github.com/citaylor/gads-go/actions/workflows/ci.yml)

A port to Go of [GADS](https://github.com/ctrlo/GADS), the Perl and
Dancer2 web application for shared, structured data: tables (sheets) with
typed fields, records with full history, views, graphs, alerts, approval
and user permissions.

The port is a drop-in replacement. It serves the original GADS templates
and JavaScript unchanged, uses the same database schema, and reproduces
Perl's behaviour, quirks included. That claim is tested: a
language-neutral conformance suite runs every scenario against Perl GADS
and against Go, and requires the two sets of observations to be
identical.

> This is an independent project. It is not made, endorsed or supported by
> Ctrl O, the authors of GADS. Report problems with this port here, not to
> the GADS project.

## Status

- **Parity:** all 176 scenarios are identical to Perl GADS `dev` at
  [`fddd4a15`](https://github.com/ctrlo/GADS/tree/fddd4a15), on SQLite.
  They cover the engine (fields, calculated values, search, sorting, views,
  graphs, imports, alerts) and the web application (pages, forms, the API,
  sessions, MFA).
- **Databases:** SQLite and PostgreSQL.
- **Security:** the application runs in *hardened mode* by default, which
  fixes the critical, high and medium findings of a security review of
  Perl GADS (CSRF, permission checks, password hashing, rate limits,
  security headers and more). `gads: hardened: 0` gives Perl's exact
  behaviour. See [Security](#security).
- **Not ported:** PDF output, the network calls for SMS and Yubikey MFA,
  and a few maintenance scripts. The full list is in
  [`CLAUDE.md`](CLAUDE.md#deviations-and-not-ported).

## Quick start

You need git and Docker with Compose v2.

```sh
git clone --recurse-submodules https://github.com/citaylor/gads-go.git
cd gads-go/gads-go
cp .env.example .env && chmod 600 .env   # set POSTGRES_PASSWORD and GADS_ADMIN_PASSWORD
docker compose up -d --build
```

Open <https://localhost/> and log in as `admin@localhost` with
`GADS_ADMIN_PASSWORD`. This starts PostgreSQL, the GADS server and a Caddy
proxy that serves HTTPS (with its own local certificate unless you
configure one). [`GETTING-STARTED.md`](GETTING-STARTED.md) covers
certificates, a real host name, logs and the tests.

## Building from source

Go 1.27 and a C compiler (cgo, for SQLite and the embedded Lua):

```sh
cd gads-go
go build -o bin/gads ./cmd/gads
bin/gads deploy -config DIR          # create the tables
bin/gads serve  -config DIR          # listen on :3000
bin/gads list                        # the ported bin/*.pl scripts
```

`DIR/config.yml` is a Dancer2 configuration, as for Perl GADS. The views
and public files come from a GADS checkout (`-views`, `-public`, or the
`upstream/GADS-dev` submodule by default). Build with `-tags modernc` for
a pure Go SQLite driver.

## Testing

```sh
cd gads-go
go test ./...      # unit tests, every scenario against the Perl golden results, the hardened tests
```

The Perl results (`conformance/results/perl`) are committed, so this needs
no Perl. To regenerate them from Perl GADS and compare, see
[`GETTING-STARTED.md`](GETTING-STARTED.md#run-the-tests). The scenario
format is in [`conformance/SPEC.md`](conformance/SPEC.md), and the suite can
also be exported as standalone Perl tests for any GADS checkout
([`conformance/STANDALONE.md`](conformance/STANDALONE.md)).

CI runs the tests and the race detector, staticcheck, govulncheck,
deadcode, the hardened tests on PostgreSQL, and security scans (CodeQL,
gosec, gitleaks, and Trivy on the Docker image).

## Repository layout

| Path | Contents |
|---|---|
| [`gads-go/`](gads-go) | The Go module: the application (`cmd/gads`), the engine (`internal/gads`), the web layer (`internal/web`), a Template Toolkit engine (`internal/tt`), the ported scripts (`internal/cli`), and the Docker setup |
| [`conformance/`](conformance) | The scenarios, the Perl reference runner, and the golden results |
| `upstream/GADS-dev`, `upstream/GADS` | Perl GADS as git submodules, pinned to the commits the golden results come from (`dev` and `master`). They are never modified. |
| [`GETTING-STARTED.md`](GETTING-STARTED.md) | Running the server and the tests |
| [`CLAUDE.md`](CLAUDE.md) | The full workspace guide: conventions, configuration, Docker, security, deviations |
| [`IMPROVEMENTS.md`](IMPROVEMENTS.md) | Planned changes: concurrent requests and persistent sessions |

## Security

Hardened mode, the deliberate deviations from Perl, and what is not fixed
are described in [`CLAUDE.md`](CLAUDE.md#security), and the review it is
based on is
[`GADS-security-report-verified.md`](GADS-security-report-verified.md).
Report a vulnerability in this port privately through
[GitHub's vulnerability reporting](https://github.com/citaylor/gads-go/security/advisories/new).

## Licence

GADS is licensed under the GNU Affero General Public License version 3,
and so is this port: see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
If you run a modified version as a network service, the AGPL requires you
to offer its source to its users.
