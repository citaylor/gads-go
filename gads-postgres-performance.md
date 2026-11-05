# GADS on PostgreSQL: Perl and Go performance

Measured on 2026-11-05 against GADS `dev` at `337d733c`, for information.

## Summary

- **One request at a time** (the suite's benchmarks, in one process): Go
  is **4.3 times faster** overall, and between 1.5 and 84 times faster on
  each step. Perl and Go give the same output on every benchmark scenario.
- **Under load** (HTTP, 5 workers each): Go serves **about 53 requests a
  second**, and Perl about 8, or **6.5 times** more. Its median response
  time is 5 to 12 times lower.
- **More clients than workers:** Go shares the wait evenly among the
  clients. Under Starman, clients beyond the 5 workers get almost no
  service: at 20 clients Perl's slowest 1% of requests took 62 s, and
  Go's 0.6 s.
- **Memory under load:** Perl's 5 workers used 1.24 GB, and Go 29 MB.
- **Slower in Go:** JSON encoding and decoding (against Perl's XS
  modules), and long Lua loops since the limit on Lua instructions (see
  [Where Go is slower](#where-go-is-slower)).

## Setup

| | |
|---|---|
| Machine | AMD Ryzen 9 6900HX, 16 CPUs, 60 GB; containers not CPU-limited |
| Database | `postgres:16-alpine` in Docker, data in memory, with `fsync`, `synchronous_commit` and `full_page_writes` off |
| Perl | Image `gads-perl-bench` (Debian trixie, Perl 5.40.5, the pinned modules of the reference install plus JSON::XS and Text::CSV_XS) |
| Go | Go 1.27.3, built in `gads-go-builder` (Debian trixie) |
| Data | 1000 records with text, integer, enum, tree, date, date range, curval and calc fields, and a table of 50 records for the curval (`conformance/bench`, made by `generators/gen_bench.py`) |

As the database keeps nothing on disk, the timings are those of the
applications and of PostgreSQL's queries, not of the storage.

## One request at a time

`conformance/bench_docker.sh --pg` runs the benchmark scenarios and unit
benchmarks in each runner, in one process with no HTTP server; web steps
call the application directly. Each step is repeated (`runs`) and the
median is reported. The full report is
[`conformance/results/bench-report-pg-docker.md`](conformance/results/bench-report-pg-docker.md).

| Scenario | Perl | Go | Go faster | Same output |
|---|---:|---:|---:|:---:|
| `b01_records`: records, searches, filters, graphs, CSV | 66.6 s | 16.0 s | 4.2× | yes |
| `b02_import`: importing 1000 rows | 25.9 s | 4.4 s | 5.8× | yes |
| `b03_lua`: calculated fields | 1.6 s | 0.7 s | 2.3× | yes |
| `b04_web`: pages and the API | 93.8 s | 22.4 s | 4.2× | yes |
| **Total** | **187.9 s** | **43.5 s** | **4.3×** | |

Median time of some steps:

| Step | Perl | Go | Go faster |
|---|---:|---:|---:|
| Get a record | 73 ms | 5.1 ms | 14× |
| Update a record | 59 ms | 5.2 ms | 11× |
| Page of 50 records, sorted | 232 ms | 21.5 ms | 11× |
| Quick search | 154 ms | 54 ms | 2.9× |
| Filter (AND) | 95 ms | 34 ms | 2.8× |
| Line graph by date | 65 ms | 7.3 ms | 9.0× |
| Log in | 635 ms | 7.5 ms | 84× |
| Data table page | 77 ms | 8.7 ms | 8.8× |
| Records API, 100 records | 989 ms | 179 ms | 5.5× |
| Record page | 211 ms | 33 ms | 6.4× |
| CSV download, 1000 records | 8.7 s | 3.7 s | 2.4× |
| Import 1000 rows | 24.5 s | 3.8 s | 6.4× |

## Under load

The two applications were served as they would be in production, one
after the other, on copies of the same database (the `b04_web` data):

- **Perl:** Starman 0.4018 with 5 workers (`starman --workers 5
  bin/app.psgi`), and YAML sessions, as GADS's `config.yml-example`.
- **Go:** `gads serve` with 5 workers (the default) and its sessions in
  the database (`session: DBIC`). `hardened: 0`, so that both do the same
  work.

Each client logged in with a session of its own, then sent these four
requests in turn for 60 seconds, each waiting for the previous answer:
the data table page, the records API (100 records), the records API with a
search, and a record page. No request failed.

| Clients | Perl req/s | Go req/s | Perl median | Go median | Perl 99% | Go 99% |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 2.2 | 13.8 | 511 ms | 43 ms | 996 ms | 170 ms |
| 5 | 8.1 | 52.6 | 715 ms | 103 ms | 1.4 s | 230 ms |
| 10 | 8.1 | 53.1 | 718 ms | 173 ms | 1.5 s | 382 ms |
| 20 | 8.0 | 52.8 | 729 ms | 364 ms | **62 s** | 598 ms |

Median time of each request with 5 clients (all workers busy):

| Request | Perl | Go |
|---|---:|---:|
| Data table page | 91 ms | 11 ms |
| Records API, 100 records | 1311 ms | 212 ms |
| Records API, search | 758 ms | 115 ms |
| Record page | 255 ms | 39 ms |

What the figures show:

- **Both stop at their 5 workers.** Neither serves more requests a second
  beyond 5 clients, as each worker serves one request at a time. The
  machine had CPUs to spare, so `GADS_WORKERS` (or Starman's
  `--workers`) would raise the ceiling of either.
- **Go shares the wait evenly.** With more clients than workers, a request
  waits for the next free engine, so every client's response time grows
  alike (20 clients: median 364 ms, 99% under 0.6 s).
- **Starman does not.** Each Starman worker keeps serving the client
  whose connection it holds, as long as that client keeps the connection
  open, so the other clients wait in the queue of the socket. At 10 and
  20 clients the median stays at 5 clients' level, because 5 clients are
  served and the others are starved, which shows in the slowest 1% (62 s
  at 20 clients). A web server in front of Starman that does not reuse
  its connections avoids this.
- **Memory:** after the runs, Perl's container used 1.24 GB (5 workers),
  Go's 29 MB, and PostgreSQL 313 MB.

## Where Go is slower

- **JSON** (units `json/encode_1000` and `json/decode_1000`): Go takes 5
  and 3 times as long as Perl's JSON::XS. GADS spends little time there
  (about 1% of the benchmark's CPU), and the decision is to keep Go's
  `encoding/json`. Against JSON::PP, which the reference install uses,
  Go is faster.
- **Long Lua loops** (unit `lua/loop`: Go 278 µs, Perl 174 µs; it was a
  tie before). Go stops user code after about 1e9 Lua instructions, a
  deliberate deviation so that a calc field cannot hold a worker for ever.
  The count needs a Lua hook, and while one is set Lua 5.3 makes an extra
  call before every instruction. Short calculations are not affected
  (`lua/simple`: 7.9 µs in both). Checking the count only after a time
  limit would remove the cost.
- The other Lua units are within 10% of each other: both run the same C
  Lua 5.3.

## Running it again

The in-process benchmark (about 5 minutes; run nothing else heavy at the
same time):

```sh
cd conformance
./bench_docker.sh --pg      # report: results/bench-report-pg-docker.md
```

The load test has no script in the repository. Its steps were:

1. Build the data and keep its database: from `gads-go/`, with the
   container of `conformance/docker-compose.test-pg.yml` running,
   `GADS_TEST_PG="host=127.0.0.1 port=55432 user=gads_test"
   PGPASSWORD=gads_test GADS_TEST_PG_KEEP=1 go run ./cmd/gads-conformance
   ../conformance/bench/b04_web.yaml`. The runner prints the name of the
   database it kept.
2. Copy that database once for each application (`CREATE DATABASE ...
   TEMPLATE ...`), and set `pwchanged = now()` for the users: the
   runner's clock is in 2014, so their passwords have expired.
3. Start each application in a container on the network of the database,
   with 5 workers: Starman in `gads-perl-bench`, with
   `DANCER_CONFDIR` pointing at a copy of `config.yml-example` that names
   the database; `gads serve` in `gads-go-builder`, with
   `docker/config.yml` plus the database, `hardened: 0` and
   `access_log: 0`.
4. Run the clients against one application at a time: each logs in as
   `user1@example.com` (password `password`), with `Host: localhost`, and
   then sends the four requests in turn.
