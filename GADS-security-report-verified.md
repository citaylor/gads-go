# GADS: Verified Security and Bug Report

| | |
|---|---|
| **Repository** | https://github.com/ctrlo/GADS |
| **Commit** | `571f2bd9340159af9eeaf0582756d6b792fbb31a` (3 Dec 2025, `dev`) |
| **Date** | 8 Oct 2026 |
| **Method** | Manual static review of `lib/`, `views/`, selected `src/` front-end code and deployment files, followed by a verification pass. Library behaviour was checked against CPAN sources (Net::SAML2 0.85/0.89, Dancer2 2.2.2, DBIx::Class 0.082844, SQL::Abstract::Classic 1.91, WWW::Mechanize::Chrome 0.80, Net::OAuth2::AuthorizationServer 0.28, Dancer2::Plugin::Auth::Extensible 0.714 + DBIC provider 0.624, Crypt::SaltedHash 0.12, Text::Markdown, Text::CSV 2.06). Browser-dependent behaviour was tested locally with headless Google Chrome 151. The GADS application itself was not deployed. |
| **Supersedes** | `GADS-security-assessment.md` (initial report; that file is now truncated) |

**Verdicts:**
- **Confirmed** means the code path was traced end to end, including the library code it depends on.
- **Confirmed (tested)** means, in addition, the browser or library behaviour it relies on was reproduced locally.
- **Conditional** means it applies only under the stated version or configuration.

Line numbers refer to the commit above.

---

## 1. Summary

| Severity | Count |
|---|---|
| Critical | 1 |
| High | 5 |
| Medium | 8 |
| Low / Informational | 15 |
| Functional bugs | 14 |
| Withdrawn after verification | 5 |

**Main themes**
1. **SAML responses are not verified against the IdP certificate.** This is an authentication bypass on Net::SAML2 ≤ 0.85. On ≥ 0.86 SAML login fails outright instead.
2. **CSRF checks cover POST only, while most state-changing routes also accept GET.**
3. **`POST /print` renders user-supplied HTML in server-side Chromium.** That allows SSRF and local file disclosure.
4. **Many object lookups are not scoped to the current table or owner.** Rights on one table are enough to modify or destroy objects in others.
5. **A useradmin can take over superadmin accounts.**

**Top-priority actions**
1. Pin the IdP signing certificate in the SAML binding and require Net::SAML2 ≥ 0.89 (C-1).
2. Enforce CSRF for every non-safe method and make all mutations POST-only via `body_parameters` (H-1).
3. Disable or isolate `POST /print` (H-2).
4. Scope every ID lookup by `instance_id` and owner (H-4, H-5, M-1).
5. Protect superadmin accounts from non-superadmin edits (H-3).

---

## 2. Critical

### C-1. SAML signature not anchored to the IdP: authentication bypass *(Confirmed, Conditional on Net::SAML2 ≤ 0.85)*

**Location:** `lib/GADS/SAML.pm:49-63`; callback `lib/GADS.pm:478-506`

**Issue:** `Net::SAML2::Binding::POST->new` is called without `cacert` or `cert_text`, and the stored IdP metadata (`Authentication->xml`) is never used to validate responses.

**Verification:**
- In Net::SAML2 0.85, `Role::VerifyXML::verify_xml` checks the signature with the certificate embedded in the response, then returns success when no trust anchor is supplied (`VerifyXML.pm:30-32`).
- `Assertion->valid` checks only audience, `InResponseTo` and validity times (`Assertion.pm:327-344`), and none of those authenticate the issuer.
- Net::SAML2 0.86 (30 Jul 2026) fixed this class of issue (CVE-2026-18089, CVE-2026-18092, CVE-2026-18108). From that release the binding refuses to run without a trust anchor (0.89 `Binding/POST.pm:31-40`).

**Impact:**
- **Net::SAML2 ≤ 0.85:** on sites with SAML enabled, a forged response can authenticate as any active user, including superadmins.
- **Net::SAML2 ≥ 0.86:** not exploitable, but all SAML logins fail (see B-17). `Makefile.PL` pins `Net::SAML2 => 0`.

**Fix:**
- Construct the binding with the IdP signing certificate from the stored metadata (`cert_text` or `cacert`).
- Require Net::SAML2 ≥ 0.89.
- Check that the referenced `Authentication` row is enabled and of type `saml2`.
- Sign AuthnRequests (remove `insecure => 1`, `SAML.pm:95`).
- Add a test proving that a response signed by an untrusted key is rejected.

---

## 3. High

### H-1. CSRF protection applies only to POST; state changes accept GET *(Confirmed)*

**Location:** `lib/GADS.pm:201`; routes declared `any ['get','post']` that read actions via `param()`

**Verification:**
- Dancer2 `param()` merges query-string, body and route parameters (`Dancer2/Core/Request.pm:507-524`).
- GADS sets no `SameSite` attribute on the session cookie. Dancer2 emits it only when configured (`SessionFactory.pm:233`).
- Top-level cross-site GET navigations therefore carry the session and skip the token check.

**Affected actions (examples):**

| Action | Location |
|---|---|
| Admin edit of a user, including email/username (`/user/:id`). Combined with password reset, this leads to account takeover. Group and view-limit lists are cleared for the groups the acting admin controls. | `GADS.pm:1582-1625` |
| Retire user (`/user/:id`, `/user_requests/`) | `GADS.pm:1552, 1627` |
| Delete group | `GADS.pm:809` |
| Send email to all users (`/user_overview/`) | `GADS.pm:1507` |
| Delete titles, organisations, departments, teams | `GADS.pm:957-1306` |
| Bulk-delete records in current view; email people in view | `GADS.pm:2320, 2684` |
| Delete single record (`/record/:id` with `delete`) | `GADS.pm:4702` |
| Delete table, view, graph, metric, topic | various |
| Overwrite tree field values | `GADS.pm:3043-3061` |
| Logout (low impact) | `GADS.pm:1962` |

PUT and DELETE requests are also never token-checked. Cross-origin preflight currently prevents abuse, but only by accident.

**Fix:**
- Validate the CSRF token for POST, PUT, PATCH and DELETE.
- Split each `any ['get','post']` route into a read-only GET and a mutating POST that reads `body_parameters`.
- Set `SameSite=Lax` (or `Strict`) and `HttpOnly` on the session cookie.

---

### H-2. `POST /print`: SSRF and local file disclosure via headless Chromium *(Confirmed (tested))*

**Location:** `lib/GADS.pm:2084-2095`; `_page_as_mech` at `GADS.pm:4624-4668`

**Issue:** Any logged-in user can submit arbitrary HTML. It is written to a temporary file, loaded in headless Chromium via a `file://` URL, and returned as a PDF. The route is excluded from CSRF checking (`GADS.pm:201`).

**Verification:** In a local test with Chrome 151, an HTML page loaded from `file://` and printed to PDF included both the contents of another local file and the response from a service bound to 127.0.0.1. Both were embedded through ordinary iframes.

**Impact:**
- Disclosure of server files readable by the app user, such as `config.yml` with database credentials.
- Access to internal-only HTTP services and cloud metadata endpoints.
- Resource exhaustion: one Chromium process per request plus `sleep 3`.

The `--remote-allow-origins=*` flag and default DevTools port 9222 (WWW::Mechanize::Chrome default) widen the attack surface. Exploiting them would require the browser's random WebSocket ID, so this part is theoretical.

**Fix:**
- Stop accepting client HTML. Render PDFs only from server-side templates and data.
- If client HTML must stay, sanitise it to a strict allow-list and run Chromium isolated: no network egress, no `file://` access, no remote-debugging origin wildcard.
- Remove the route from the CSRF exemption list and rate-limit it.

---

### H-3. Useradmin can take over superadmin accounts *(Confirmed)*

**Location:** `/user/:id` (`GADS.pm:1582-1640`), `/api/user_account/:id` (`API.pm:713-750`), `update_user` and `retire` in `Schema/Result/User.pm`

**Issue:** The only superadmin-specific guard in `update_user` covers the global `permissions` list (`User.pm:916-919`). A useradmin can change any user's email and username, including a superadmin's, and can retire superadmins.

**Verification:**
- Password-reset emails go to the account's current `email` (`Auth/Extensible.pm:695`).
- `update_user` sets the username from the email (`User.pm:883-889`).
- The only other `superadmin` checks in `User.pm` restrict group assignment to groups the acting admin already belongs to (`User.pm:812-817`).

**Fix:**
- Block non-superadmins from editing, changing the email of, or retiring superadmin (and probably useradmin) accounts.
- Notify the previous address when an email changes.
- Consider requiring re-authentication for admin edits.

---

### H-4. Historic purge is not scoped to the table *(Confirmed)*

**Location:** `GADS.pm:3121-3131`; `Schema/ResultSet/Current.pm:18-28`; `Schema/Result/Current.pm:388-406`

**Issue:** The route checks `purge` permission on the table in the URL. The submitted record IDs and column IDs are then used without restricting them to that table, the user's view limits, or readable columns. `Current` has no `site_id` column, so the schema's automatic site filter (`Schema.pm:22-28`) doesn't apply either.

**Impact:** A user with purge rights on any one table can irreversibly null values (all versions) in records of other tables, and of other sites on multi-site installs.

**Fix:** Fetch records through `GADS::Records` with `limit_current_ids` and the user's view limits, filter by `instance_id`, and validate each column with `column_this_instance` and `user_can('read')`.

---

### H-5. Dashboard write authorization uses the URL's table; notice widgets render raw HTML *(Confirmed (tested))*

**Location:** `API.pm:1267-1289` (`_get_dashboard`, `_get_dashboard_write`); `Schema/Result/Widget.pm:160`; `views/snippets/widget_content.tt:12`; `views/index.tt:21`

**Issue:**
- `_get_dashboard_write` grants write access when the user has `layout` permission on the table in the URL, regardless of which dashboard the ID refers to. That includes other tables' dashboards and the site-wide home dashboard.
- Notice-widget content is stored and rendered unescaped. Widgets are rendered server-side into the page.

**Verification of CSP bypass:**
- The CSP is only `script-src 'self'`, and responses lack `X-Content-Type-Options: nosniff`.
- Any user can upload independent files (M-3), and libmagic classifies a JavaScript file with a `.txt` name as `text/plain`, which the allow-list accepts.
- Dancer2 serves such files as `text/plain` with `Content-Disposition: attachment` (`Core/App.pm:1225-1234`).
- In a local Chrome 151 test, a same-origin `<script src>` pointing at exactly such a response executed under that CSP.

**Impact:** A user with `layout` rights on any one table can place active content on the site home dashboard, giving stored XSS against all users, including superadmins.

**Fix:**
- Authorize against the dashboard's own scope: site dashboards for superadmin only, table dashboards for `layout` on that dashboard's table, personal dashboards for the owner only.
- Sanitise notice HTML with an allow-list.
- Send `nosniff`.
- Fix M-3.

---

## 4. Medium

### M-1. Cross-table IDOR on graphs, reports and tree fields *(Confirmed)*

`GADS::Layout::column($id)` resolves columns from every table in the site (`Layout.pm:951-956, 1314-1321`). `Graph`, `Report` and tree lookups are not filtered by table, and `Graph` and `Report` have no `site_id` column.

| Route | Problem | Location |
|---|---|---|
| `/<t>/graph/:id` | Any graph can be edited or deleted by a user with `layout` on the URL table; `write` re-homes it (`Graph.pm:413-425`) | `GADS.pm:3206`; `Graph.pm:41-56, 284-298` |
| `/<t>/report` delete | `Report->find` unscoped (soft delete) | `GADS.pm:2873` |
| `/<t>/report/edit:id` | `load_for_edit` unscoped; read/modify any report | `GADS.pm:2995, 3015`; `ResultSet/Report.pm:50-56` |
| `/<t>/tree/:layout_id` | Update any tree column; read tree values with no read-permission check (`Column/Tree.pm:281, 417`) | `GADS.pm:3050-3069` |
| `/helptext/:id` | Any column's help text | `GADS.pm:1656` |
| `/<t>/api/field/values/:id` | Column from any table accepted | `API.pm:423` |

**Fix:** Scope lookups by `instance_id` and owner, and use `column_this_instance` with `permission => 'read'`.

### M-3. Any authenticated user can upload independent files *(Confirmed)*

`POST /file/:id?` (`GADS.pm:1694-1710`) creates `is_independent` files before the superadmin check, which only protects deletion. Every logged-in user can read independent files (`ResultSet/Fileval.pm:72-76`). This enables same-origin hosting of attacker content, which is the enabler for H-5. **Fix:** require superadmin at the start of the handler.

### M-4. Weak password hashing *(Confirmed)*

`encryption_algorithm: SHA-512` (`config.yml-example:93`) means a single salted SHA-512 pass (`Auth/Extensible/Role/Provider.pm:111-116`). Generated passwords are three EFF words plus two digits, about 45 bits of entropy. **Fix:** migrate to bcrypt or argon2 and re-hash on login.

### M-5. API token endpoint weaknesses *(Confirmed)*

`API.pm:46-130, 328-419`:
- The password grant ignores the login lockout (`failcount`) and writes no audit entries.
- Issued tokens are not re-checked against the user's active or password-expiry status (`ResultSet/Oauthtoken.pm`). They are not revoked when a user is retired; access tokens stay valid until they expire.
- The single client secret is stored in plaintext.

(The refresh-token grant is non-functional; see B-15.)

**Fix:** reuse the `/login` lockout and audit, re-validate the user on each API request, revoke tokens on retire, and hash client secrets.

### M-7. Resource exhaustion *(Confirmed)*

- Page sizes `rows` (`GADS.pm:2434`) and `length` (`API.pm:1089, 1354`) are unbounded.
- `/print` and dashboard PDF generation start Chromium per request.
- Lua calculated fields have no instruction or memory limits (`Column/Code.pm:30-118`). No `sethook` or alarm exists anywhere in `lib/`.
- `/register`, `/resetpw`, `/login` and `/api/token` have no rate limiting.

**Fix:** clamp page sizes, add Lua instruction-count and memory limits, and rate-limit public endpoints.

### M-8. Missing security headers and cookie attributes *(Confirmed for the application; a reverse proxy may add some)*

- Security headers are only set for logged-in requests (`GADS.pm:268-275`), so login, register and reset pages are frameable.
- The CSP lacks `object-src`, `base-uri`, `frame-ancestors` and `form-action`.
- There's no `nosniff`, HSTS or `Referrer-Policy`. The last matters because reset codes travel in URLs.
- The session cookie has no `SameSite`. YAML sessions live in `/tmp/dancer-sessions`.
- `bin/app.psgi` adds no header middleware.

**Fix:** add header middleware for all responses, tighten the CSP, and use a private session store.

### M-9. Unauthenticated `/register` abuse *(Confirmed)*

`GADS.pm:674-741`:
- Accepts GET, so the token check never applies.
- No rate limit or captcha, and each request emails all admins.
- For an existing account it overwrites the reset code and emails a new link, which invalidates pending resets.
- Name lengths aren't validated.

**Fix:** POST-only with CSRF, rate limiting, and no state change for existing accounts.

### M-11. CSV formula injection in downloads *(Confirmed)*

`Records.pm:2687-2706` writes cell values unmodified. Text::CSV's `formula` option only affects parsing, not output. **Fix:** prefix cells starting with `=`, `+`, `-`, `@`, tab or CR with `'`.

---

## 5. Low / Informational

| ID | Finding | Location |
|---|---|---|
| M-2 *(downgraded)* | `_get_dashboard` performs no read check. Widget IDs are random 32-character tokens (`Widget.pm:117-125`), so this isn't practically exploitable; fix as hardening. | `API.pm:1267` |
| M-6 *(downgraded)* | `views_other_user_id` is accepted from any user (`GADS.pm:2386-2388`), letting them create or remove alerts for other users. The views involved are still access-checked against the acting user during `Alert::write`. | `GADS.pm:2404` |
| L-1 | `debug_login` logs plaintext passwords to syslog; the shipped rsyslog config writes them to `/var/log/linkspace.log` | `GADS.pm:651, 2044`; `contrib/linkspace.conf` |
| L-2 | Markdown is rendered after entity-encoding, so `javascript:` links survive (verified with Text::Markdown); mitigated by CSP | `Column.pm:605`, `Topic.pm:94` |
| L-4 | Per-username lockout only: anyone can lock out an account, and no IP throttling | `GADS.pm:620-631` |
| L-5 | SVG accepted (libmagic reports `image/svg+xml`); site logo served inline; superadmin-only, CSP-mitigated | `Filecheck.pm:54`; `GADS.pm:1353-1362` |
| L-6 | `/api/user_account/:id` looks up the user before checking authorization (ID enumeration) | `API.pm:719-729` |
| L-7 | Reset and welcome codes have no expiry (`get_user_by_code` matches the code only) | DBIC provider `DBIC.pm:685-695` |
| L-8 | `remember_me` cookie contains the username | `GADS.pm:522-530` |
| L-9 | Generic unauthenticated requests aren't audited (login failures are) | `GADS.pm:4604-4616` |
| L-10 | Docker dev config: hard-coded credentials, Postgres exposed on all interfaces, `docker.sock` mounted into the app container, password passed on the command line | `docker-compose.yml`, `Dockerfile`, `bin/docker.sh` |
| L-11 | `GADS::Safe` is imported but never used; remove it | `Safe.pm`; `Datum/Code.pm:22` |
| L-12 | Lua environment exposes `os.date` and the shared string metatable (hardening) | `Column/Code.pm:72-75` |
| L-13 | CSRF token compared with `ne` (not constant-time; negligible) | `GADS.pm:214` |
| L-14 | Unpinned security-relevant dependencies (`Net::SAML2 => 0`, `WWW::Mechanize::Chrome => 0`) | `Makefile.PL` |

---

## 6. Functional bugs

| ID | Bug | Location | Effect |
|---|---|---|---|
| B-1 | `split 'Basic ', $auth` yields `("", <b64>)`, so `$encoded` is always empty (verified in Perl) | `API.pm:335` | Basic client authentication to `/api/token` never works; body parameters are ignored when an `Authorization` header is present |
| B-3 | Audit is written only when the welcome email fails | `GADS.pm:700-706` | Missing audit trail |
| B-5 | `/user_upload/` always passes a `permissions` arrayref, which `update_user` rejects for non-superadmins | `GADS.pm:1439`; `User.pm:916-919` | Bulk upload always fails for useradmins |
| B-6 | `$params{ $site->user_field_is_editable(...) }` indexes params by a boolean | `User.pm:936, 943, 950` | Mandatory-field logic broken |
| B-7 | `create_user` audits `$params{username}`, which is always undef | `ResultSet/User.pm:126-130` | Audit lines lack the username |
| B-8 | Unknown numeric filter column on `/data` calls a method on undef | `GADS.pm:2306-2312` | 500 error |
| B-9 | `/match/layout/:id` with an unreadable or unknown column calls a method on undef | `GADS.pm:4466-4473` | 500 instead of 403/404 |
| B-10 | `POST /api/file` delete with an invalid ID calls `->delete` on undef | `GADS.pm:1786-1788` | 500 error |
| B-11 | Session `views_other_user_id` naming a missing user crashes `before_template` | `GADS.pm:352-356` | Persistent 500s on data/view pages for that session |
| B-12 | Temp HTML file (`UNLINK => 0`) only removed on success | `GADS.pm:4632-4666` | Record data left in `/tmp` on errors |
| B-14 | Every request runs `Site->count`, `GADS::DB->update` and an audit insert | `GADS.pm:156-240` | Avoidable DB load |
| B-15 | Refresh grant: the access-token callback returns a single row object, so `($client_id, $error, $scopes, $user_id)` receives the object and `undef` | `API.pm:111-128, 365-394` | `grant_type=refresh_token` cannot succeed |
| B-16 | API sort direction isn't allow-listed; SQL::Abstract::Classic throws on keys other than `-asc` / `-desc` (`Classic.pm:1266-1270`) | `API.pm:1171`; `Records.pm:2071` | 500 error (not injectable) |
| B-17 | With Net::SAML2 ≥ 0.86 the binding throws because no trust anchor is supplied | `SAML.pm:49` | All SAML logins fail on current library versions |

---

**Regression tests to add:**
- SAML: a response signed by an untrusted key is rejected.
- CSRF: GET or token-less requests to mutating routes are rejected.
- Scoping: a user with rights on table A is refused for table B's graphs, reports, trees, dashboards and historic purge.
- Account protection: a useradmin cannot change a superadmin's email.
- `/print`: generated PDFs contain no content from `file://` or internal URLs.

---

## Appendix A: Verification experiments

| Finding | Experiment | Result |
|---|---|---|
| H-2 | Headless Chrome 151 printed a local `file://` HTML page to PDF. The page embedded a second local file and a URL on a 127.0.0.1-only HTTP server. | Both contents appeared in the PDF text |
| H-5 | Local server returned a page with CSP `script-src 'self'` that referenced a same-origin script served as `text/plain`, `Content-Disposition: attachment`, without `nosniff`. Separately, `file --mime-type` was run on a JS file with a `.txt` name. | Script executed in Chrome 151; libmagic reported `text/plain` |
| L-2 | Text::Markdown rendered a Markdown link with a `javascript:` URL | Output contained the `javascript:` href unchanged |
| L-5 | `file --mime-type` on SVG files | `image/svg+xml` (passes the `image/` allow-list) |
| B-1 | Ran the exact `split 'Basic ', ...` expression in Perl | First element is an empty string |

*Point-in-time review. It does not guarantee the absence of other defects.*
