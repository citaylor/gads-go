# GADS on PostgreSQL: Perl and Go performance

Measured on 2026-11-05 against GADS `dev` at `337d733c`, for information.
The figures of one request at a time are those of the second run, after
the change to the Lua limit (see [The Lua limit, and why the comparison
is fair](#the-lua-limit-and-why-the-comparison-is-fair)); the load test
ran before it, and uses no Lua.

## Summary

- **One request at a time** (the suite's benchmarks, in one process): Go
  is **4.2 times faster** overall, and between 1.2 and 82 times faster on
  each step. Perl and Go give the same output on every benchmark scenario.
- **Under load** (HTTP, 5 workers each): Go serves **about 53 requests a
  second**, and Perl about 8, or **6.5 times** more. Its median response
  time is 5 to 12 times lower.
- **More clients than workers:** Go shares the wait evenly among the
  clients. Under Starman, clients beyond the 5 workers get almost no
  service: at 20 clients Perl's slowest 1% of requests took 62 s, and
  Go's 0.6 s.
- **Memory under load:** Perl's 5 workers used 1.24 GB, and Go 29 MB.
- **Slower in Go:** JSON encoding and decoding, against Perl's XS
  modules (see [Where Go is slower](#where-go-is-slower)). Lua runs at
  the same speed in both, the limit on Lua instructions included.

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
| `b01_records`: records, searches, filters, graphs, CSV | 59.8 s | 14.6 s | 4.1× | yes |
| `b02_import`: importing 1000 rows | 23.6 s | 4.3 s | 5.5× | yes |
| `b03_lua`: calculated fields | 1.6 s | 0.7 s | 2.4× | yes |
| `b04_web`: pages and the API | 89.3 s | 21.9 s | 4.1× | yes |
| **Total** | **174.3 s** | **41.4 s** | **4.2×** | |

Median time of some steps:

| Step | Perl | Go | Go faster |
|---|---:|---:|---:|
| Get a record | 69 ms | 5.1 ms | 13× |
| Update a record | 55 ms | 5.1 ms | 11× |
| Page of 50 records, sorted | 138 ms | 21.3 ms | 6.5× |
| Quick search | 150 ms | 36 ms | 4.2× |
| Filter (AND) | 90 ms | 26 ms | 3.4× |
| Line graph by date | 60 ms | 7.4 ms | 8.1× |
| Log in | 581 ms | 7.0 ms | 82× |
| Data table page | 68 ms | 8.4 ms | 8.0× |
| Records API, 100 records | 942 ms | 173 ms | 5.4× |
| Record page | 197 ms | 30 ms | 6.5× |
| CSV download, 1000 records | 8.4 s | 3.6 s | 2.3× |
| Import 1000 rows | 22.3 s | 3.7 s | 6.0× |

A single step can differ by a third or more between two runs, in both
applications (Perl's sorted page took 232 ms in the first run and 138 ms
in the second); the totals vary by a few per cent.

The unit `db/csv_100` swung most: on PostgreSQL Go took 203 ms (±36%) in
the first run and 436 ms in the second, against Perl's 293 and 258 ms
(on SQLite it takes 82 ms either way). The cause was PostgreSQL's
statistics, not either application. The unit's data is inserted just
before it is timed, and PostgreSQL plans its queries from statistics that
exist only once autovacuum has analysed the tables, at a moment of its
own. The CSV export runs the same few small queries thousands of times,
and their plans before and after the analysis differ by up to 20 times
(a check for child records took 3 ms with a nested loop over the
records, and 0.16 ms once the statistics showed there were none). Six
runs of the Go unit took between 217 and 455 ms, one swinging by 32% as
autovacuum ran during it. The runners now run `ANALYZE` after each step
of a benchmark (SPEC.md "PostgreSQL"), as a database in use would have
its statistics, and six runs took between 211 and 222 ms (Perl, three
runs outside Docker: 321 to 335 ms). The figures above were measured
before this change.

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
- The other units where Go is not faster are Lua's (0.9×, see below).

## The Lua limit, and why the comparison is fair

Perl GADS runs the Lua of calc and RAG fields without any limit, so code
that never ends holds its worker process for ever. Go stops it after
about 1e9 Lua instructions (a deliberate deviation, listed in
[`CLAUDE.md`](CLAUDE.md#deviations-and-not-ported)), since in Go all the
workers share one process.

Counting instructions needs a Lua hook, and while a count hook is set,
Lua 5.3 calls `luaG_traceexec` before every instruction, whatever the
count (`lvm.c`, `vmfetch`). In the first run this made the unit
`lua/loop` (a loop of 10,000 iterations) take 278 µs in Go against Perl's
174 µs (0.6×), where both had been equal.

Go now counts only once a call has run for 100 ms
(`lua.Interpreter.CountAfter`): a timer then sets the hook, and the limit
applies from there. Calculations ending sooner, which are all usual ones,
run with no hook. The timer arms only the call in progress, so that a
late timer cannot arm the next call (`TestCountAfter`).

| Unit | Perl | Go, counting from the start | Go, counting after 100 ms |
|---|---:|---:|---:|
| `lua/simple` | 6.6 µs | 7.9 µs | 7.4 µs |
| `lua/loop` | 164 µs | 278 µs | 182 µs |
| `lua/strings` | 12.0 µs | 13.7 µs | 13.1 µs |

(Perl and the last column are from the second run; the middle column is
the first. On this machine outside Docker, `lua/loop` took 268 µs before
the change and 162 µs after.)

Why this is fair:

- **The benchmark measures what users run.** The benchmark sets no option
  of its own: the limit is on, with its defaults, as in `gads serve`.
  Turning the limit off for the benchmark would have made the numbers
  look the same while users kept paying for it.
- **Both run the same Lua.** Perl's Inline::Lua and Go embed the same C
  Lua 5.3.6, unchanged (Go's is vendored, with `-O2`; Perl's is Debian's
  `liblua5.3`). Go's VM was not modified to make the count cheaper, so
  each instruction costs the same in both. The tenth that remains in
  `lua/loop` is of the size of the difference between two runs of Perl
  (164 and 174 µs); Go's call into C and its conversion of the values
  cost about 1 µs a call, which is most of the difference in the short
  units.
- **Go's timings include its safety checks; Perl's include none.** Perl
  runs Lua with no limits at all, while Go still checks on every call:
  the memory limit (in the allocator, at each allocation), the timer of
  the instruction count (a lock and a timer reset at the start and end
  of the call), and the size of tables coming back. Any difference left
  is to Go's disadvantage. Go needs these checks because its workers
  share one process, where a Perl worker that runs out of memory or
  loops for ever affects only itself. Endless code is still stopped,
  only 100 ms later than before, out of the about 4 s it is allowed.
- **Nothing was traded for speed.** The memory limit (512 MB) applies from
  the start, as before, and costs nothing (the allocator checks it).
  Code running longer than 100 ms is counted with the same hook as
  before, and pays the same cost as before.

## Running it again

The in-process benchmark (about 5 minutes; run nothing else heavy at the
same time):

```sh
cd conformance
./bench_docker.sh --pg      # report: results/bench-report-pg-docker.md
```

The load test (about 10 minutes with the defaults; also with nothing else
heavy running):

```sh
cd conformance
./loadtest_docker.sh [-c 1,5,10,20] [-d 60s]
```

It builds the `b04_web` data on the test PostgreSQL, keeping its database
(`GADS_TEST_PG_KEEP=1`), copies it for each application (setting
`pwchanged = now()`, as the runner's clock is in 2014 and the passwords
would have expired), starts Starman in `gads-perl-bench` and `gads serve`
in `gads-go-builder` on the network of the database as above, runs
`gads-go/cmd/gads-loadtest` against each in turn, prints their memory, and
removes all of it. `gads-loadtest` can also measure any other GADS server:
`gads-loadtest -url URL -user EMAIL [-password P] [-host HOST] [-sheet
SHORT_NAME] [-record ID] [-c 1,5,10,20] [-d 60s]`.
