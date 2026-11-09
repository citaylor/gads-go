# Bugs found in the Perl GADS

The bugs found in the Perl GADS (and the CPAN modules it runs) while porting
it to Go, as of 2026-11-09, against GADS `dev` at `337d733c`. It is a record
of what was seen, not a plan to fix the Perl, which stays the unmodified
reference.

The last column says what the Go port does:

- **Same**: Go reproduces the bug, so that the conformance suite matches.
- **Deviation**: Go differs on purpose (listed under "Deviations" in
  `CLAUDE.md`).
- **Hardened**: fixed in hardened mode, on in the application, while the
  engine's default keeps Perl's behaviour for the suite.

Add each new Perl bug here when it is found, with where it showed up.

## Security

The security findings are in `GADS-security-report-verified.md`, with their
evidence. All were still present in dev `fddd4a15` (2026-10-28).

| ID | Bug | Go |
|---|---|---|
| C-1 | SAML signature not anchored to the IdP: authentication bypass (Net::SAML2 ≤ 0.85) | Not present: SAML responses are always rejected |
| H-1 | CSRF protection only for POST: GET requests change data | Hardened |
| H-2 | `POST /print`: SSRF and local file disclosure through headless Chromium | Hardened |
| H-3 | A useradmin can take over superadmin accounts | Hardened |
| H-4 | Historic purge is not limited to the table | Hardened |
| H-5 | Dashboard writes are authorised by the URL's table; notice widgets render raw HTML | Hardened |
| M-1 | Graphs, reports, trees and views of other tables found by ID (IDOR) | Hardened |
| M-2 | A dashboard is read without a read check (random IDs; downgraded) | Hardened |
| M-3 | Any logged in user can upload independent files | Hardened |
| M-4 | Weak password hashing | Hardened |
| M-5 | API token endpoint: no lockout or audit, tokens survive retirement | Hardened |
| M-6 | `views_other_user_id` accepted from any user (downgraded) | Same |
| M-7 | Resource exhaustion: unlimited page sizes, no rate limits | Hardened |
| M-8 | Missing security headers and cookie attributes | Hardened |
| M-9 | `/register` abuse, unbounded names and notes | Hardened |
| M-11 | CSV formula injection in downloads | Hardened |
| L-1 to L-14 | Low findings (plaintext passwords in the debug log, Markdown `javascript:` links, per-username lockout only, and others) | Mostly Same; see the report |

Found later, not in the report:

| ID | Bug | Where found | Go |
|---|---|---|---|
| P-1 | `search_view` without a filter ignores the user's view limits | Porting the alerts | Same |
| P-2 | `/record/:id` of a table the user has no permission on shows the record's metadata (created and edited by) | Docker test, 2026-11-04 | Same |
| P-3 | The MFA hook finds the logged in user in the site of the worker's previous request, so a user of the same name in another site decides whether MFA is asked | Porting MFA | Deviation (`conformance/hardened/site_mfa_user.yaml`) |
| P-4 | The bearer token of a deleted user, or of an account request, is accepted until it expires | Code review | Deviation |

## Crashes and hangs

These hold up or kill a worker process.

| ID | Bug | Where found | Go |
|---|---|---|---|
| P-5 | `working_days_add` with a negative count never ends (it counts down a day at a time), and a very large count takes as long | Porting calc fields | Deviation: an error |
| P-6 | Lua code has no limit on instructions, memory or pattern backtracking, so code that runs without end holds the worker | Code review | Deviation: limits |
| P-7 | A field's regex has no time limit | Code review | Deviation: 1 s |
| P-8 | Inline::Lua: a Lua table that contains itself, or is nested deeper than Lua's stack, crashes the worker; one with shared parts takes time exponential in its depth | Code review | Deviation: limits |
| P-9 | Inline::Lua: a table with a number key such as 2^53 makes an array up to that key, taking all memory | Code review | Deviation: an error from 10 million |
| P-10 | Inline::Lua: a table with a boolean key fails with no place, and leaves its Lua state broken, so every later call fails | Code review | Deviation: only that call fails |
| P-11 | Template Toolkit: a macro that calls itself crashes Perl (segfault) | Fuzzing, 2026-11-09 | Deviation: an error at 1000 levels |
| P-12 | Template Toolkit: blocks that include each other without end recurse until memory runs out | Code review | Deviation: an error at 1000 levels |
| P-13 | Template Toolkit: `first`, `last` and `slice` with a huge count, or a list item assigned at a huge index, run out of memory (they pad with undef) | Code review and fuzzing | Deviation: "Out of memory!" at once |
| P-14 | An overlay remembered for a table that no longer exists makes the page fail (PANIC), and stays in the persistent settings until the options are changed | Docker test | Same |
| P-15 | An unknown sort or group field makes the page fail (PANIC: `user_can` on undef) | Porting views | Same |
| P-16 | A dropdown curval over a large table (14,000 records) renders a 22 MB page, taking 3 s | Docker test | Same |

## Wrong results and failures

| ID | Bug | Where found | Go |
|---|---|---|---|
| B-1 | Basic authentication on `/api/token` never works: `split 'Basic ', $auth` gives `""` first | Security review | Same |
| B-3 | Registering with an existing email is audited only when the email fails | Security review | Same |
| B-5 | `/user_upload/` always fails for useradmins: it passes a `permissions` list, which `update_user` refuses | Security review | Same |
| B-6 | The mandatory field checks of a user index the parameters by a boolean | Security review | Same |
| B-7 | `create_user` audits `$params{username}`, which is always undef | Security review | Same |
| B-8 | An unknown numeric filter column on `/data` is a 500 | Security review | Same |
| B-9 | `/match/layout/:id` with an unknown or unreadable column is a 500 | Security review | Same |
| B-10 | `POST /api/file` deleting an invalid ID is a 500 | Security review | Same |
| B-11 | A session's `views_other_user_id` naming a missing user makes every data and view page a 500 for that session | Security review | Same |
| B-12 | The temporary HTML file of a PDF is removed only on success, leaving record data in `/tmp` | Security review | Deviation: always removed (hardened: no file, the page is served from memory) |
| B-14 | Every request runs `Site->count`, `GADS::DB->update` and an audit insert | Security review | Same |
| B-15 | The refresh grant never succeeds (the token callback returns the row object), after deleting the old access token; a second refresh fails on `$prev_at->delete` | Security review | Same |
| B-16 | An API sort direction other than `asc` or `desc` is a 500 | Security review | Same |
| B-17 | Every SAML login fails: GADS gives Net::SAML2 no trust anchor | Security review | Same |
| P-17 | `/api/record/:id` always fails (no layout) | Porting the API | Same |
| P-18 | API POSTs need a CSRF token, which an API client without a session can't have ("csrf-token missing") | Porting the API | Same |
| P-19 | `/api/settings/logo` with a file that is not an image is a 500 (`get_filetype` is given the upload object) | Porting the settings | Same |
| P-20 | Sending a message to the records of a view: the "no records" check never fires (it tests an array reference), and any record makes it fail (no datum has an `email` method in dev) | Porting `modal_sendemail` | Same |
| P-21 | `has_view_limit` looks up `view_limit_id` in `layout`, which has no such column (`instance` has). SQLite reads it as a string, so every table with a field has a view limit; PostgreSQL fails the statement, so alerts fail there with an empty error | Porting users, and the PostgreSQL runs | Same |
| P-22 | A view is found by its ID alone (DBIC `find` uses the primary key, ignoring the table), so another table's view is checked with this table's permissions | Porting views | Same; Hardened finds it only in its own table (M-1) |
| P-23 | A view is written before its sorts are checked, so a bad sort still creates the view | Porting views | Same |
| P-24 | A missing graph or view fails during the page render, after the session's messages were taken, so they are lost | Porting graphs | Same, but the messages are kept |
| P-25 | A new linked record is written twice (two current versions) | Porting linked records | Same |
| P-26 | A `linked_id` of `""` is an unexpected error (datatype mismatch) | Porting linked records | Same |
| P-27 | Enum value IDs from a form fail with "Bad ID x" | Porting the record form | Same |
| P-28 | `Datum::Person->new(set_value => ...)` ignores the value, so an approval's "Submitted by" is blank | Porting approvals | Same |
| P-29 | `/edits` with invalid JSON gives no records (`$@` is cleared), and takes the records in hash order | Porting bulk edits | Same |
| P-30 | A column's `set_permissions` after `write` is never saved | Porting columns | Same |
| P-31 | `update_cached` ignores `no_alerts` (it checks `no_alert_send`) | Porting alerts | Same |
| P-32 | `add_aggregate` reuses an existing aggregate only when the operator differs | Porting dev's search | Same |
| P-33 | After a failed user update, `after_template_render` validates the failed values again, so an invalid mobile number on `/mfa` redirects to `/` | Porting MFA | Same |
| P-34 | A globe of a view keeps the view's columns (the Moo `columns` setter has no trigger) | Porting globes | Same |
| P-35 | Dates in tables with link placeholders render empty (TT `.replace` on a DateTime is undef) | Porting the data table | Same |
| P-36 | A CSV download without the download permission gives an empty, unfinished file; the error goes only to the log (the streamed response has started) | Porting the download | Deviation: a redirect with the error |
| P-37 | With YAML sessions, two requests of a session at once each overwrite the other's changes | Porting sessions | Deviation: one at a time |
| P-38 | Searches whose sort leaves ties return them in the database's plan order (no tie-break), so PostgreSQL results can differ between runs | PostgreSQL runs (`013_datatime`) | Same |
| P-39 | On SQLite, the search joins `record_later` through an index that matches almost every row, so searches are quadratic in the number of records | Benchmarks | Deviation: an extra index |

## Scripts (`bin/`)

| ID | Bug | Go |
|---|---|---|
| P-40 | `update-many-records` doesn't compile | Not ported |
| P-41 | `move-layout` fails for each column: it calls `display_field`, a method columns no longer have | Same |
| P-42 | `update-alertcache`, `update-date-format` and `update-snapshot` do nothing: without `IGNORE_PERMISSIONS` they see no tables | Same |
| P-43 | `import --merge` and `--purge` fail (PANIC) | Same |
| P-44 | `onboard` writes as no user: new records and changed values always fail on permissions | Same |
| P-45 | `--update-unique ID` always dies (the internal column is named ID) | Same |

## CPAN modules

| ID | Module | Bug | Go |
|---|---|---|---|
| P-46 | Text::Autoformat | Centred paragraphs are never found: the test for a hang is of an object, always true | Same |
| P-47 | Lingua::EN::Inflect (plurals) | A capitalised word ending in consonant and y gets an s ("Category" becomes "Categorys") | Same |
| P-48 | JSON::PP | A whole float from 2^53 to 2^64 is written as a number or as a string, depending on what was encoded before it | Deviation: always a number |
