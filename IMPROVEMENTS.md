# GADS Go: Recommended Improvements

2026-10-28

Three changes take the Go GADS from a single-user test install to a service
many people can rely on: serve it over HTTPS, let it handle requests in
parallel, and keep sessions across restarts.

## At a glance

| Order | Change | What it fixes | Effort |
|---|---|---|---|
| 1 | HTTPS in front of the app ([section 1](#1-serve-it-over-https)) | Passwords, session cookies and API tokens cross the network in clear text | About half a day |
| 2 | Sessions in PostgreSQL ([section 3](#3-keep-sessions-across-restarts)) | Every restart or deploy logs everyone out; a second process can't share logins | 1–2 days |
| 3 | Concurrent requests ([section 2](#2-handle-requests-in-parallel)) | One slow page, search or import holds up every other user | 2–3 weeks |

Do them in this order. HTTPS is configuration and needs almost no Go code.
Persistent sessions are small and independent, and they also allow several
`gads serve` processes behind the proxy, which helps with load before the
larger concurrency work is done.

## 1. Serve it over HTTPS

Put a Caddy container in front of the app to terminate TLS with automatic
Let's Encrypt certificates, and stop publishing the app's port. Today the app
listens on plain HTTP at :3000, so passwords, session cookies and the CSRF
token cross the network unencrypted.

The Go code already supports this: with `behind_proxy: 1`, `Ctx.Scheme()` and
`Ctx.Host()` read `X-Forwarded-Proto` and `X-Forwarded-Host`, and the rate
limiter reads the client address from `X-Forwarded-For`. With `is_secure: 1`
the session cookie is marked Secure, and hardened mode then also sends HSTS.

1. **Add a proxy service** to `gads-go/docker-compose.yml`: `caddy:2-alpine`
   publishing ports 80 and 443, with volumes `caddy-data` and `caddy-config`
   so certificates survive restarts. Its whole `Caddyfile` is two lines:

   ```
   {$GADS_DOMAIN} {
       reverse_proxy app:3000
   }
   ```

2. **Stop publishing the app's port.** Remove
   `ports: "${GADS_PORT:-3000}:3000"` from `app` (or bind it to `127.0.0.1`
   only). This matters for security, not just tidiness: with
   `behind_proxy: 1` the app trusts `X-Forwarded-*`, so a client that reaches
   :3000 directly could forge its address and get around the rate limit.
3. **Turn on the two settings** in `docker/config.yml`: `behind_proxy: 1`, and
   `is_secure: 1` under `engines: session: Simple:`. Better, have
   `docker/entrypoint.sh` write them from two new variables
   (`GADS_BEHIND_PROXY`, `GADS_SECURE_COOKIES`), as it already does for the
   database and mail, so that no file in the image needs editing.
4. **Point GADS at the new name.** GADS redirects any request whose host isn't
   the site's `host`, so an existing database needs
   `UPDATE site SET host = 'gads.example.com'` (new ones take `GADS_HOST` at
   seeding). Set `gads: url` to `https://gads.example.com/`, because alert and
   welcome emails build their links from it. A `GADS_URL` variable in the
   entrypoint would make this a one-line change in `.env`.
5. **Check it.** `curl -sI https://gads.example.com/login` should show
   `Strict-Transport-Security` and a session cookie with
   `Secure; HttpOnly; SameSite=Lax`. `http://` should redirect to `https://`
   (Caddy does this by default). Log in once in a browser and confirm that the
   login page and a record edit both work.

Effort: about half a day, including the entrypoint variables and a section in
`GETTING-STARTED.md`. The server needs a DNS name and ports 80 and 443
reachable from the internet for Let's Encrypt; an internal-only server can use
Caddy's `tls internal` or a certificate from your own CA instead.

The alternative is TLS in Go itself (`golang.org/x/crypto/acme/autocert`
behind a `-tls` flag on `gads serve`). It saves a container but makes the app
own certificates, port 443 and the HTTP redirect, which is more code to
maintain than a two-line Caddyfile.

## 2. Handle requests in parallel

Give each request its own engine and database connection, and remove the
single lock that now makes every request wait for the one before it. Today
`Server.mu` serialises all dispatch, like Dancer2 with one worker: one slow
CSV export, import or report holds up every other user. Production Perl GADS
runs several workers, each with its own connection, so per-request engines
also behave more like a real Perl deployment.

The lock exists because one `gads.Engine` is shared by everyone, and it holds
per-request state. That state, and what each part becomes:

| Shared today | Why it can't be shared | Becomes |
|---|---|---|
| `db.DB`: one `*sql.Conn` (`SetMaxOpenConns(1)`), with the transaction depth and savepoint counter | Two requests would share a transaction | A pool plus a per-request session that owns one connection |
| `Engine.SiteID`, `Reports`, `IgnorePermissions`, `IgnorePermissionsSearch` | Per-request values, reset by `Server.resetEngine` | Fields of a per-request engine |
| `Engine.Emailer` and `DB.WriteCheck`, replaced during a hardened GET | One request's read-only guard would apply to another's | Per-request, so the swap needs no restore |
| `Engine.Lua`: one Lua state behind its own mutex | Calc and RAG fields would queue on it | A pool of interpreters, each with the same limits |
| `regexCache` in `internal/gads/datum.go`, a plain map | Concurrent writes crash the process | A `sync.Map` |
| `tt.Engine.cache` of compiled templates in `internal/tt/exec.go` | Same | A `sync.RWMutex` around it |

The steps, each one mergeable and tested on its own:

1. **Make the shared caches safe** (`regexCache`, the template cache), and add
   an interpreter pool in `internal/lua`: `Pool.Get()` returns a ready
   `*Interpreter` with `MaxInstructions` and `MaxMemory` set, and `Put()`
   returns it. About 1–2 days.
2. **Split the database layer.** `db.Open` returns a `*db.Pool` wrapping
   `*sql.DB` with `SetMaxOpenConns(N)` (default 20 on PostgreSQL).
   `pool.Session(ctx)` takes one `*sql.Conn` and returns today's `*db.DB`,
   with its own `depth`, `svp`, `Trace` and `WriteCheck`. `Session.Close()`
   rolls back anything still open, as `RollbackAll` does now, and returns the
   connection. Nothing that uses `*db.DB` changes. About 2–3 days.
3. **Create an engine per request.** Add
   `func (e *Engine) ForRequest(d *db.DB, l *lua.Interpreter) *Engine`. It
   copies the shared fields (`Config`, `Clock`, `AlertSender`, `Emailer`, the
   generators) and starts every per-request field at zero. All GADS objects
   already hold the engine they were made with, so a request's layouts,
   records and users then use only its own. About 3–5 days, mostly finding the
   places that reach a shared engine through `Server.E` instead of `c.E`.
4. **Remove the lock.** `ServeHTTP` takes a session and an interpreter, builds
   `c.E`, dispatches, then closes the session and returns the interpreter.
   `resetEngine` goes, because a fresh engine has nothing to reset. A
   semaphore the size of the pool caps requests in flight, so a burst queues
   instead of failing for want of a connection. About 1 day.
5. **Prove it.** Add a load test under the race detector: an `httptest` server
   on a seeded database, with 50 goroutines mixing record views, edits, CSV
   downloads and logins. It checks for no races, no deadlocks, and a database
   that is consistent afterwards. Measure before and after with `gads-bench`.
   About 2–3 days.

The conformance suite needs no change: its runner drives one request at a time
through the same `ServeHTTP`, so parity still proves behaviour.

SQLite needs care, because it allows one writer at a time. Open file databases
in WAL mode with `busy_timeout=5000`, begin write transactions with
`BEGIN IMMEDIATE` (so two of them can't deadlock upgrading their locks), and
keep in-memory databases at one connection. PostgreSQL, the Docker setup's
database, needs none of this.

Effort: 2–3 weeks in all. The risk is in step 3: any per-request value still
reached through a shared pointer is a race. The race detector in step 5 and in
CI is what finds those.

## 3. Keep sessions across restarts

Store sessions in PostgreSQL instead of process memory. Today `sessionStore`
in `internal/web/session.go` is a map, so every deploy or restart logs
everyone out, and a second `gads serve` process could never see the first
one's logins. A database store fixes both, and it is a prerequisite for
running more than one process behind the proxy.

1. **Put the store behind an interface** in `internal/web/session.go`:
   `Get(ctx, id) (*Session, error)`, `Put(ctx, *Session) error`,
   `Delete(ctx, id) error` and `Sweep(ctx, now) error`. The present map
   becomes the `memory` implementation, which the conformance runner and the
   tests keep using.
2. **Add the database store** with its own table, created at startup by
   `gads serve` with `CREATE TABLE IF NOT EXISTS` and kept out of the GADS
   DDL, so the schema stays identical to Perl's:

   ```sql
   CREATE TABLE gads_go_session (
       id_hash  text PRIMARY KEY,   -- SHA-256 of the cookie value
       data     bytea NOT NULL,     -- encoding/gob of Session.Data
       expires  timestamp NOT NULL
   );
   CREATE INDEX gads_go_session_expires ON gads_go_session (expires);
   ```

   The key is a hash of the session ID, so a database dump or backup gives
   nobody a live login. The data is encoded with `encoding/gob`, after
   `gob.Register` of the types sessions hold (`map[string]any`, `[]any`,
   `int64`, `[]*Message`). Gob gives them back with their exact types; JSON
   would turn `int64` into `float64` and lose `*Message`.
3. **Write once per request.** `SessionSet` and `SessionDelete` now call `put`
   every time; with a database that would be several writes per page. Mark the
   session dirty instead, and save it once in `ServeHTTP` after dispatch,
   before the response is sent, as Dancer2 does in its after hook. Store
   `expires = now + idle` on save, and update it on a read only when it is
   more than a minute old, so a page view doesn't always cost a write.
4. **Use its own connections.** The store writes through a small separate pool
   (2 connections), not the request's session. A request that fails and rolls
   back still keeps its session messages and logout, as with Dancer2, where
   sessions are separate from the database transaction. This also means the
   change doesn't depend on section 2.
5. **Sweep expired sessions** in a goroutine started by `serve`:
   `DELETE FROM gads_go_session WHERE expires < now()` every 10 minutes,
   stopped with the server on shutdown.
6. **Select it in the configuration** with the session engine name, as
   Dancer2 does: `session: DBIC` uses the database, and `Simple` (the default)
   keeps memory. The Docker image's `config.yml` switches to `DBIC`. The
   cookie settings (`is_secure`, `cookie_same_site`, `session_duration`) work
   as now.

Tests: a round trip of every stored type, expiry, a rollback that keeps its
session write, and a restart test. The restart test logs in through one
`Server`, then makes a request through a second `Server` on the same database,
which proves both restarts and running several processes.

Effort: 1–2 days. Two parallel requests on one session save in turn and the
last one wins, as with Dancer2's own stores; GADS keeps little in its sessions
(messages, the current view, search and sort), so this matters little in
practice.
