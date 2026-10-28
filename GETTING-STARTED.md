# Getting started

How to run the Go GADS server and check that it behaves like the Perl
GADS. Everything runs in Docker. [`CLAUDE.md`](CLAUDE.md) has the full
reference.

**What is tested:** the conformance suite (`conformance/`) has 176
scenarios. Each one runs against Perl GADS (`upstream/GADS-dev`, never
modified) and against Go GADS (`gads-go/`), recording what GADS returns at
every step. Perl's recordings are committed as the *golden results*
(`conformance/results/perl`). Go passes when its recordings are identical.

## Setup

You need git and Docker with Compose v2, with your user in the `docker`
group (`sudo usermod -aG docker $USER`, then log in again). If your user ID
isn't 1000, first run
`export BUILD_UID=$(id -u) BUILD_GID=$(id -g) BENCH_UID=$(id -u) BENCH_GID=$(id -g)`,
so that files written by the containers belong to you.

```sh
git clone --recurse-submodules https://github.com/citaylor/gads-go.git
cd gads-go
```

The submodules fetch the Perl GADS at the commits the golden results came
from. The first image builds take 10–20 minutes.

## Run the Go server

```sh
cd gads-go
cp .env.example .env && chmod 600 .env   # set POSTGRES_PASSWORD and GADS_ADMIN_PASSWORD
docker compose up -d --build
```

Open <http://localhost:3000/> and log in as `admin@localhost` with
`GADS_ADMIN_PASSWORD`. Use `localhost` in the URL, because GADS redirects
any other host name.

| Command (in `gads-go/`) | Does |
|---|---|
| `docker compose logs -f app` | Follow the log |
| `docker compose up -d --build` | Rebuild after a code change |
| `docker compose --profile build up -d dev` | Run your working copy on port 3001 (`docker compose restart dev` after edits) |
| `docker compose down` | Stop, keeping the data |
| `docker compose down -v` | Stop and delete the database and uploads |

The admin password is set only on the first start. To change it, run
`down -v` and start again.

## Run the tests

**Go tests and parity** (about 1 minute). These run the unit tests, plus
every scenario against Go, compared with the golden results:

```sh
cd gads-go
docker compose build builder
docker compose run --rm builder go test ./...
```

A difference is reported as
`parity: <step> observed.status: perl="200" go="302"`.

The server fixes the security findings of the Perl GADS ("hardened mode",
see `CLAUDE.md`). The suite compares Go with Perl as it is, so hardened mode
is off there, and `conformance/hardened/` tests the fixes on their own.

**The Perl reference run** (about 40 minutes). This re-runs the scenarios
on Perl GADS, which proves that the golden results are Perl's behaviour:

```sh
cd conformance
DC="docker compose -f docker-compose.bench.yml"
$DC build perl-ref
$DC run --rm perl-ref ./run_perl.sh      # rewrites results/perl
git status results/perl                  # no changes = Perl matches the golden results
```

**Perl and Go side by side.** Run Go into `results/go`, then compare it
with the Perl results:

```sh
$DC run --rm go ./run_go.sh
$DC run --rm -w /gads-go go go run ./cmd/gads-compare \
    -perl /conformance/results/perl -go /conformance/results/go
```

The comparison should end with `176 identical, 0 different`.

- To run some scenarios only, pass their paths: `./run_perl.sh scenarios/205_web_record.yaml`.
- To run on PostgreSQL, start it first with `$DC --profile pg up -d --wait test-pg`,
  then add `--pg` to the run scripts. The results go to `results/perl-pg` and `results/go-pg`.

## Perl tests in a GADS checkout

The scenarios can be exported as ordinary `.t` files, for example to hand
to the GADS developers. Export into a copy of GADS, not into `upstream/`:

```sh
mkdir /tmp/gads && git -C upstream/GADS-dev archive HEAD | tar -x -C /tmp/gads
conformance/export_perl_tests.sh /tmp/gads
cd conformance
docker compose -f docker-compose.bench.yml run --rm -v /tmp/gads:/gads -w /gads \
    perl-ref prove -r -j4 t/conformance
```

In a checkout with the GADS dependencies installed, run
`prove -r t/conformance` directly.

## Without Docker

- **Go:** install Go 1.27 and gcc, then run `go test ./...` in `gads-go/`.
- **Perl:** install the modules into `~/perl5` as
  `conformance/docker/perl/Dockerfile` does. Then `conformance/run_all.sh`
  runs Perl, then Go, then the comparison.

## Troubleshooting

| Problem | Fix |
|---|---|
| `permission denied ... docker API` | Log in again after joining the `docker` group, or use `sg docker -c '...'`. |
| The site redirects elsewhere | Use the `GADS_HOST` name (`localhost`). |
| Your change doesn't show | Run `docker compose up -d --build` (app) or `docker compose restart dev`. |
| `go: downloading` in the builder | Run `docker compose build builder`. |
| `results/perl` changed after a Perl run | Perl now differs from the golden results. Commit the new results only if the scenario changed on purpose; otherwise run `git checkout conformance/results/perl`. |

## Cleaning up

```sh
(cd gads-go && docker compose --profile build down -v)
docker image rm gads-go gads-go-builder gads-perl:reference gads-perl-bench
```
