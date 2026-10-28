# GADS conformance scenario format

The conformance suite is a **language-neutral** description of GADS behaviour.
Each scenario is a YAML file in `scenarios/`. A scenario is executed by a
*runner*:

* `perl/run.pl` drives the original Perl GADS code (the reference).
* `gads-go/cmd/gads-conformance` (and `go test ./conformance/...`) drives the
  Go re-implementation.

Every runner executes the scenario's steps in order against a fresh in-memory
SQLite database deployed with the GADS schema, and writes an **observation
file** (JSON) containing one entry per step. Two checks prove equivalence:

1. **Expectations** – a step may contain `expect:`; the runner fails the step if
   the observation does not contain the expected values. Expectations are
   ported from the assertions in the original `t/*.t` test files, so passing
   them with the Perl runner proves the scenario captures real GADS behaviour.
2. **Parity** – `tools/compare` diffs the Perl and Go observation files. Every
   observed value (not just the expected subset) must be identical.

## Top level keys

| key          | meaning |
|--------------|---------|
| `name`       | unique scenario name (defaults to file name) |
| `source`     | list of upstream test files the scenario was derived from |
| `description`| free text |
| `fixed_time` | `YYYY-MM-DD HH:MM:SS` UTC time to freeze the clock at (Test::MockTime equivalent). Omitted = frozen at `2014-10-10 01:00:00` for determinism. |
| `config`     | GADS config overrides, e.g. `{dateformat: dd-MM-yyyy}` |
| `steps`      | ordered list of steps |
| `hardened`   | Go only: run with the security fixes of hardened mode (the scenarios of `hardened/`, which Perl does not run) |

## Steps

Each step is a single-key mapping: `- <type>: {...options}`. Common options:

* `id` – optional label, used in reports (defaults to `<index>:<type>`)
* `expect` – expected subset of the observation
* `expect_error` – a regex the error message must match (the step must fail)
* `capture_warnings` – also report WARNING messages (code evaluation
  parameter dumps, which are in random hash order, are stripped)
* `expect_messages_match` – a regex that at least one message must match
* `expect_body_contains` – strings that the observed `body` must contain
* `ignore_permissions` – run the step with `$GADS::Schema::IGNORE_PERMISSIONS`
* `env` – environment variables set for the step (`GADS_ALLOW_BLANK_CURVAL`,
  `PANIC_ON_CURVAL_BUILD_VALUES`)
* `runs` – run the step that many times (benchmarks); the observation is
  that of the last run, and with `GADS_BENCH` set the step's `timing` covers
  every run
* `user` – one of `superadmin`, `audit`, `useradmin`, `normal1`, `normal2`, a
  numeric user id, a user created with `as`, `@sheet:<alias>` (that sheet's
  `$sheet->user`), or `none` (no user / permissions ignored)

### `sheet`
Create a `Test::GADS::DataSheet` equivalent. Options mirror DataSheet:
`as` (alias, required), `instance_id`, `data` (`default` or list of rows),
`create_records` (default true), `multivalue`, `curval` (alias or instance id
of the sheet referred to), `curval_fields` (list of column names of the
referred sheet), `column_count`, `enumvals_count`, `calc_code`,
`calc_return_type`, `rag_code`, `has_rag`, `optional`, `no_groups`,
`users_to_create`, `multivalue_columns`, `curval_offset`, `site_id` (only for the first sheet,
which builds the schema: creates the site and sets the schema's site),
`share_users` (use the first sheet's users rather than creating them again),
`build_columns: false` (do not build the sheet's columns).

Rows in `data` use the DataSheet column names (`string1`, `integer1`,
`enum1`, `tree1`, `date1`, `daterange1`, `curval1`, `person1`, `file1`).
Enum/tree values may be textual (converted to ids, as DataSheet does).
In curval values, `@field:<name>` is replaced by the field name
(`field<id>`) of that column in the referred sheet, for query values such as
`@field:integer1=20`.

### Column references
Columns are referenced as `name` (in the step's sheet), `alias:name` (in
another sheet) or `parent_child` (curval sub-field, e.g. `curval1_string1`,
child resolved in the sheet the curval refers to). Internal columns use their
short names (`_id`, `_serial`, `_version_datetime`, `_version_user`,
`_created`, `_created_user`, `_deleted_by`).

### `column`
`op: create|update|delete`, `sheet`, `type`, `name`, `name_short`, plus any
column attribute (`multivalue`, `optional`, `isunique`, `can_child`,
`remember`, `code`, `return_type`, `decimal_places`, `enumvals`, `tree`,
`ordering`, `end_node_only`, `force_regex`, `max_length`, `textbox`, `refers_to`,
`curval_fields`, `related_field`, `filter`, `display_fields`,
`display_condition`, `value_selector`, `show_add`, `delete_not_used`,
`override_permissions`, `limit_rows`, `aggregate`, `group_display`,
`link_parent`, `topic`, `default_today`, `show_in_edit`, `permissions`).
`permissions: default` gives the first sheet's group (or that of
`permissions_sheet`) the default permissions. `as` names the column so later
steps can refer to it as `@col:<as>`. Extra op `update_cached` recalculates a
code column's stored values (with `layout_user: true` and `user`, taking the
column from a layout for that user, e.g. `none` as for overnight updates).
`permissions: none` gives no permissions. `write_options` are passed to
the column write (e.g. `include_approval_perms`). `table` (a `layout
create_table` alias) creates or deletes the column in that table.
`op: hold` (`as`) keeps the column object, which later steps use with
`held_col: <as>` instead of a column reference (so that, as in tests reusing
a column object, it can be written again after deletion, recreating it);
`op: clear` clears its caches. `get` also takes `subvals_input_required`
(sorted names of the fields a curval filter needs).

### `record`
`op: create|update|get|delete|purge|restore|versions|find_unique`, `sheet`,
`user`, `current_id` (or `serial`, `record_id`), `values` (column => value as
passed to `set_value`), `options` (write options e.g. `draft`, `no_alerts`,
`force_mandatory`, `update_only`), `observe` (list of columns to report;
default all), `parent_id`, `linked_id`, `rewind`.

### `held`
Operations on a record object kept between steps (for tests that reuse the
same `GADS::Record`). `as` names the object. `op`:
`new` (initialise a new record), `single` (first record via
`Records->single`; `panic_on_code: true` fails if code values are evaluated
instead of loaded), `find` (`current_id`/`record_id`/`serial`, optional
`clear: true`; creates the object if needed; with `chronology: true`
with `version: n` then loads the record's nth version, latest first;
with `chronology: true` uses `find_chronology_id` and observes `chronology`: per version `editor`,
`datetime` and `changed` – `name_short` and `value` of each changed field), `clear`, `set` (`values`),
`user` (set the record's user), `write` (`options`), `re_evaluate` and
`write_value` (`column`), `delete`, `purge` (whole record), `purge_version`
(just this version, `Record->purge`), `remove_id` (so the next write creates
a new record), `write_linked_id` (`linked: "@held:<as>"` or null), `restore`, `get`.
Any op may also take `clear_layout_first`, `clear_layout` (clear the record's
layout before/after), and observe `observe` (as_string per column),
`changed` (changed flag per column), `observe_parent` (parent ID),
`child_record_ids`, `columns_render` (names of the columns to render, as set
by the records the record came from), `record_meta` (`edited_user`,
`deletedby` as strings and `deleted` as `YYYY-MM-DDTHH:MM:SS`), `value_refs` (Perl ref type of each
value: `DateTime` or empty), `serial` and `versions` (created date of each
version, latest first). `write` takes `options: {version_datetime: ...}`, and
`submitted_fields_of: column` (the fields that column's filter needs, as
`submitted_fields`). `clone_oldvalue` (`columns`) sets each datum's
`oldvalue` to a clone of itself, so it looks as if it has been written.
`current_id: "@held:<as>"` refers to a held record's current ID in any step.
In `set` values, `@own_ids` is the datum's own current IDs (a no-op edit)
and `{own_ids_matching: regex}` the IDs of its curval values whose text
matches. `@own_queries` is the `as_query` of each of its curval values
plus `&current_id=<id>` (an unchanged curval edit form). In a list, an
element `{own_id: regex}` is the ID of its curval value whose text matches,
and `{query: q, own_id: regex}` is `q&current_id=<that id>`.
Further ops: `initialise` and `load_remembered_values` (the user's draft or
remembered values of the last record); `clone` (`to`: held alias for
`Record->clone`); `set_from_form` (`from` held record, `columns`: sets each
column to the `html_form` of the other record's value, or with `as_query`
the queries of its curval values, optionally with `replace: [from, to]`); `filtered_values: column` observes
the values of a curval field from the record's own layout (with
`submission_token: true`, stored against the record's submission token, as
when a form is displayed; a `write` with `options: {submission_token: 1}`
then uses that token; `filtered_full: true` also observes each value's
`value_id`, `html` and `selector_id`, with a new record's random token
shown as `new_*`). `same_as: {as,
columns}` observes per column whether the value equals that of another held
record (for values that differ between runs, such as Lua's `os.time`).

Held ops also take `datum: [columns]` with `datum_props` (default
`as_string, blank, changed`; also `html`, `filter_value`, `html_form`,
`oldvalue` ({as_string, blank} or null), `empty_value`, `id`, `id_hash`,
`hours` (hour of each datetime value as held, i.e. UTC), `text_all`,
`ids`, `deleted_values` (values of enum options since deleted),
`html_form_query` (the `as_query` of each curval form value),
`dependent_not_shown`),
`for_code_curval: column` (count and field_values keys of the curval's code
value; with `for_code_fields: [names]` only those fields are requested and
the first value's `field_values` are observed), `parent_id` (on `new`, a current ID or `@held:<as>`), and `view_ref`,
`is_deleted` and `nth` (on `single`: the record from calling `single` that
many times on the same records object).

### `datum`
A datum created directly from a column (`sheet`, `column`) rather than a
record: `init_value: [...]` (as for grouped results) and/or
`init_no_value: true` (a value that starts as undefined), then optionally
`set_value` (with `set_options`). Observes `datum_props` as above.

### `reset`
Start again with a fresh database (as tests that create a new DataSheet
without passing a schema). All sheets, users, views and held records are
forgotten.

### `search`
`sheet`, `user`, `filter` (GADS filter JSON with column references in `id`
or `name`), `sort` (`[{name, type, sheet, parent}]`: `sheet` when the column
is in another sheet, `parent` the curval column it is a subfield of),
`view` (`{name, columns, sorts, groups, global, is_admin}`), `columns`,
`rows`, `page`, `search` (quick search text), `rewind`, `from`, `to`,
`include_children`, `is_deleted`, `view_limit_extra` and
`view_limit_override` (view aliases), `observe` (columns to report).
Observation: `count`, `pages`, `ids` (current ids in order) and `rows`
(per record `as_string` of observed columns). `record_columns: true` adds
the names of each record's `columns_render` (the presentation columns). `relations: true` adds the
`parent_id` and `child_record_ids` of each result. `single: true` observes the IDs from
iterating with `Records->single` (`single`), with
`single_dependent_not_shown: column` also that column's
`dependent_not_shown` for each.

### `datum`
Unit level: build a column (as `column` above, written or virtual) and call
`set_value` repeatedly; observe `as_string`, `blank`, `changed`, `html_form`,
`for_code`, or the error.

### `code`
Evaluate a calc/rag column for a record: `sheet`, `column`, `current_id`.

### `set_time`
`{time: "YYYY-MM-DD HH:MM:SS"}` – move the frozen clock.

### `view`
`op: create|update|delete|get`, `sheet`, `user`, `name`, `filter`, `columns`,
`sorts`, `groups`, `global`, `is_admin`, `group` (name or `default`), `as`
(alias for later reference as `view: alias`), `ref` (the view to update).
`filter` replaces the view's filter object (so the change is not tracked);
`set_filter` (a hash) and `set_filter_json` set the existing filter as
`$view->filter->as_hash(...)` and `->as_json(...)` do, so that a changed
filter updates the alert cache.

### `views`
The views available to the user of the sheet's layout
(`GADS::Views->user_views_all`): `sheet`, `other_user` (as `other_user_id`).
Observes `names` and `ids` in order.

### `view_held`
A view object kept between steps (`as`), built on the sheet's shared layout
so that its user is the one set with `layout op: set_user`. `op`: `new`
(attributes `name`, `global`, `is_admin`, `columns`, `group`, `other_user`,
`filter`; `id` loads a view: an alias from `view as`/`view_as`, a literal id
such as `-10`, or `@held:<as>`; loading is lazy as in Perl, so the access
check happens at the first later op), `set` (attributes, `groups` to group
by, `filter: null` to remove the filter), `write` (`view_as` names it;
observes `id` and the stored `user_id`), `filter` (reads the filter, which
checks access), `delete`, `get`. Any op observes `name` and `columns` with
`observe: true`, and the decoded `filter->base64` with `filter_base64: true`.

### `layout`
Table level operations: `op: set_user|has_children|user_can|columns|set_alert_columns|position|permissions|update|delete`
(`create_table`: `name`, `as` – a new table as the sheet's user;
`delete_table`: `table`; `purge_table`: `table` (`Layout->purge`);
`set_user`: `user`, changes the user of the sheet's layout and clears it;
`has_column`: `column` as `alias:name` of another sheet and `by: id|name_short`,
observes whether this layout returns it)
(`user_can`: `user`, `permissions` list, `with_columns` to build all columns
first, which changes how permissions are checked), `sheet`,
`columns`, `set_groups`, attributes (`name`, `name_short`, `forget_history`,
`sort_layout_id`, `sort_type`, `view_limit_id`).
`write_internal` tries writing each internal column and observes `name` and
`error` for each; `delete_all_columns` deletes every column in reverse
dependency order, observing `name`, `existed`, `error` and `exists` (after).

### `user`
`op: create|view_limits|group|remove_from_group|groups_viewable|set_groups`, plus
attributes. `set_groups` (`user`, `groups` names or `default`, `by` the logged
in user, default `superadmin`) sets groups as `$user->groups($by, [...])`;
`save: name` first keeps the user's current groups, and `restore: name` sets
those instead of `groups`. `create_user` calls `ResultSet::User->create_user`
with `params` (form values; `groups` as names, `view_limits` as view aliases)
as `current_user` (default `superadmin`), observing `id`, `value` and
`username`. `update_row` (`user`, `set`) and `insert_row` (`set`) update or
create a user row directly, with the row validation (`insert_row` with `as`
names the user for later `user` references).

### `import`
Import CSV `content` into a sheet (`GADS::Import`, without forking) as `user`
(default the sheet's user). In the content, `{user:<name>}` is replaced by
that user's ID. `options` are Import's options (`dry_run`, `force_mandatory`,
`blank_invalid_enum`, `take_first_enum`, `split_multiple`,
`ignore_string_zeros`, `round_integers`, `short_names`, `update_only`,
`no_change_unless_blank`, `report_changes`), with `update_unique` and
`skip_existing_unique` a column reference (`ID` for the ID column) and
`append` a list of columns. Observes the import status (`row_count`,
`written_count`, `error_count`, `skipped_count`, `result`, `completed` as
0/1) and its `rows` (`status`, `content`, `errors`, `changes`) in order.

### `filter_held`
A filter object (`GADS::Filter`) kept between steps (`as`). `op`: `new` (from
`as_json` – a structure encoded to JSON – or `as_hash`, or neither),
`set_json`/`set_hash` (`value`), `get`. `observe` lists what to report, in
order, as each may build lazy values: `as_hash`, `as_json` (decoded),
`changed`, `column_ids` (joined with spaces).

### `topic`
`op: create` (`name`, `sheet` or `table`, `as`) or `op: update` (`as`,
`prevent_edit`: alias of the topic whose fields must be completed first).
Columns take `topic: <alias>`.

### `download`
A CSV download (`Records->csv_header`, `csv_line`): `sheet`, `user`,
`layout_user`, and a view as for `search`. `insert_after: {line, values}`
writes a new record once that line has been produced. Observes `header` and
`lines` (as text; Perl produces UTF-8 bytes, decoded by the runner).

### `historic_purge`
Purge the values of `columns` in all versions of record `current_id` of
`sheet`, as `user` (`Result::Current->historic_purge`).

### `site`
`{id: N}` sets the site of the schema (`$schema->site_id`).

### `instances`
The tables a user can access (`GADS::Instances->all`): `user`. Observes
`names` in order, and with `is_valid: [ids]` whether each is valid. `groups_viewable` (`user`, optional `match` regex) observes the
sorted names of the groups the user can see for sharing.

### `graph`
Create a graph (`GADS::Graph`) and observe its data (`GADS::Graph::Data`).
Definition: `title`, `type`, `x_axis` (column, or null for all columns of the
view), `x_axis_link`, `x_axis_grouping`, `x_axis_range`, `from`, `to`,
`y_axis`, `y_axis_link` (the curval of a Y-axis field in another sheet),
`y_axis_stack`, `group_by`, `as_percent`, `trend`, `is_shared`,
`group` (name, or `default` for the first sheet's group); `as` names it for
`graph_ref` in later steps (which then skip the definition). `no_data: true`
only writes it. Data options: `sheet`, `user`, `layout_user`, `rewind`,
`search`, and a view (`view`/`filter`/`columns`/`view_ref` as for `search`).
Observation: `points`, `xlabels`, `labels` (series names only: colours are
chosen randomly by Perl except for RAG values) and `options`.

### `globe`
Observe a globe (`GADS::Globe`) of a sheet's records: `sheet`, `user`,
`view_ref` (or `view`/`columns`), and `group`, `color`, `label`, each a column
reference, a literal number (`-1` for colour by count, or an invalid ID such as
999) or `[parent, child]` for a curval sub-field (`parentid_childid`).
Observation: `traces`, each with `type`, `hoverinfo`, `locationmode`, `mode`,
`showscale` and `countrycolors` (0/1/null for undef/defined/absent), `marker`,
and `items` sorted by location: `location`, `text`, `hovertext`, `z` and
`countrycolor` (a random colour shows as `color`). Perl's countries and their
hover counts are hashes, so the `<br>`-separated parts of each text are sorted
(after the leading country of hover texts). Also `params` (`has_view`,
`layout_identifier`, `globe_fields`, `default_view_limit_extra_id`) and the
number of `colors` used.

### `graphs`
The graphs a user can see (`GADS::Graphs->all`): `sheet`, `user`,
`layout_user`. Observes `count` and sorted `ids`.

### `alert`
Write an alert (`GADS::Alert`): `sheet`, `view` (alias of a view created
with `as`), `user` (the alert's owner) and `frequency` (0, 24, or null to
remove the alert). Observes the alert `id`.

### `alert_send`
Process alerts as GADS does after updating code values (`GADS::AlertSend`):
`sheet`, `current_ids` (list) and `columns` (the changed columns).

### `alert_description`
Descriptions and links of records for alert emails (`GADS::AlertDescription`):
`sheet`, `current_ids` (one or a list) and `users`. Observes a list of
`{description, link}`, one per user, sharing one description cache.

Record writes queue alerts unless `no_alerts` is set; record and held steps
default to `no_alerts: 1`, so alert scenarios pass `options: {no_alerts: 0}`.
Alerts are processed inline as with `GADS_NO_FORK`, so after a write that
processed alerts the record keeps its new entry flags.

### `emails`
Emails are captured instead of sent (Perl: `GADS::Email->send` is replaced;
Go: the engine's `Emailer`), keeping the checks of `send` (recipients,
subject and a text or HTML body) and its removal of duplicate recipients.
Observes the list of emails sent since the scenario started or the last
`emails` step, each `{subject, to, text, html, reply_to}` (the text before
`Text::Autoformat` wrapping; absent parts are null), and clears it. Password reset emails of `Dancer2::Plugin::Auth::Extensible` (sent with
Mail::Message rather than GADS::Email) are captured the same way, with the
one recipient in `to`.

### `clear_layouts`
Clear the cached layout of every sheet (as tests do with `$layout->clear`,
needed for example after creating a sheet so that existing layouts see it).

### `db`
`op: update|count|delete|insert|select` on `table`, with `where` and `set`.
A list as a `where` value matches any of its values (`null` matching NULL),
as a DBIx::Class arrayref does. Values may refer to runner objects: `@now`, `@user:<name>`, `@record:<as>`,
`@group:<name|default>`, `@column:<sheet>:<name>`, `@instance:<sheet>`,
`@view:<alias>`. `select` orders by `id`, or by the columns of `order_by`
(for tables without an id column, such as `oauthtoken`). In a `select`, a
mapping as a `where` value is an SQL::Abstract operator: `{ ">": 35 }`
(`>`, `<`, `>=`, `<=`, `!=`). The tables with a `site_id` (such as `user`,
`group` and `instance`) are limited to the site of the runner (site 1), as
the schema of the tests is; rows of another site are observed through the
tables without one, or by a script (such as `export.pl`).

### `repeat_single`
Retrieve the first record (`Records->single`) `repeat` times (default 10)
with a view (as for `search`), for random sorts: `sheet`, `user`,
`columns`. Observes per column `many` if the values differed between
retrievals, otherwise `one: <value>`.

### `file_access`
Retrieve the file of record `current_id` with
`ResultSet::Fileval->find_with_permission` as `user`. Observes `found`.

### `sleep`
Wait `seconds` in real time.

### `table_permission`
One table-level permission test of t/018_instance.t: `test` (`delete`,
`purge`, `download`, `layout`, `view_create`, `link`) as `user` on `sheet`,
with a layout built anew for the user (`layout_from: new`, which also becomes
the sheet's layout) or from `GADS::Instances->layout` (`layout_from:
instances`). `allowed: true` undoes what the test did (and for `purge`
deletes and purges the record, then creates the sheet's records again);
`reset_groups` then removes all the table's permissions through that layout.
Observes `errors`, the error of each attempted action (null if none).

### `template`
Render a Template Toolkit template with GADS's settings (`ANYCASE`,
`ENCODING => UTF-8`, `RECURSION`): `template` (the text), `vars` (the
template variables) and `files` (other templates it may `INCLUDE`, by name).
Observes `output`.

### `http`
A request to the web application (Perl: the Dancer2 app through
`Plack::Test`; Go: `internal/web`). Options: `method` (default `GET`),
`path`, `host` (default `localhost`), `query` and `form` (hashes; a list
value repeats the parameter; keys are sorted), `json` (a JSON body; the CSRF token is added to a hash, or sent as the
`csrf-token` query parameter for a list),
`body` (a raw request body, sent as is: give its `Content-Type` in
`headers`; the CSRF token is sent as the `csrf-token` query parameter),
`upload` (a multipart form: a hash of field name to
`{filename, content | content_b64, content_type}` (default
`application/octet-stream`), sent with the `form` values and the CSRF
token),
`headers`, `as` (the name of a client: each has its own cookie jar and CSRF
token; default `anon`), `csrf` (default true: send the client's CSRF token
with forms and JSON) and `no_body` (do not observe the body).
References in `path` and in the keys and values of `query`, `form` and
`json` are substituted: `{user:<name>}` (user ID), `{record:<alias>}`,
`{view:<alias>}` (view ID), `{sheet:<alias>}` (table identifier),
`{instance:<alias>}` (instance ID), `{column:<name>}` (column ID in the
first sheet, or `{column:<sheet alias>:<name>}`), `{var:<name>}` (a
captured value) and `{totp:<base32 secret>}` (the current OTP token of an
MFA secret, as Authen::OATH totp at the runner's time). References are
also substituted in `headers` values.
`capture` (a hash of variable name to JSON key) remembers top level values
of a JSON response (e.g. random OAuth tokens) for later steps; a missing
value leaves the variable unset. Captured values are replaced by
`{var:<name>}` in all later observations (and in that of the capturing
step).
Observes `status`, `content_type`, `location` (without the scheme and host),
`headers` (`X-Frame-Options`, `Content-Security-Policy`,
`Content-Disposition`, when sent), `cookies` (sorted names in the jar), and
`json` (JSON responses: scalars as strings, booleans as `true`/`false`) or
`body`. In bodies the client's CSRF token is replaced by `{csrf}` and the
source locations of Perl error messages (` at FILE line N.`) are removed. Stringified Perl references (`HASH(0x55d4…)`, `ARRAY(…)`, `CODE(…)`, as
printed when a template outputs a hash) become `HASH(0x)` etc., since the
addresses differ between runs.
A redirect with a SAML request (`SAMLRequest` in the `Location`) is
observed as `saml_request`: the request decoded (URL escaping, base64 and
raw deflate, whose bytes differ between zlib and Go) with the attributes of
each element sorted (Perl builds them from a hash). The parameter is
`{saml_request}` in the location and in the body.
The CSRF token of a client is taken from each page (`data-csrf` or a
`csrf_token` field).

### `login`
Log in as a user: sets the user's password, fetches `/login` and posts the
login form. Options: `user`, `as` (client name, default the user name),
`password` (default the password set) and `path` (default `/login`).
Observes the login response as `http` with `no_body`.

### `cli`
Runs a script of GADS `bin` as a command line would, in the process of the
runner: `script` (its file name, such as `export.pl`), `args` (its
arguments), `stdin` (its standard input) and `files` (files written in its
directory before it runs, by name; a name may have directories, which are
created). `{file:<name>}` in an argument is the path of one of those files.
The script runs in a new temporary directory, its current directory
(`$0` is `bin/<script>`); `dir: <name>` keeps the directory for the later
`cli` steps of the scenario with the same name (to import an export, for
example). The site of the schema is restored after the script.

Observes `exit` (its exit status), `stdout`, `stderr` and, for a Perl `die`
that ends it, `died` (its message). Reports of Log::Report are written to
the standard error as its default dispatcher writes them, when they are
made: the reason in lower case and the message (`notice: ...`,
`error: ...`, with `; <errno text>` for a fault). A fatal report ends the
script with status 1 (or the errno of a fault), a `die` with 255 and `exit`
with its status. `observe_files: content` observes the files that the
script writes in its directory (other than those of `files`), by path, and
`observe_files: lines` only their number of lines and first line (for
output that is random, such as that of `generate.pl`).

So that the outputs are comparable they are normalised: the temporary
directory is `{tmp}`, ` at <file> line <n>.` (and `, <$fh> line <n>`) is
removed from warnings and dies, as are `Subroutine X redefined` warnings
(from loading a script again in one process) and the stack traces of
croaks, and addresses of references are `(0x0)`. Hashes dumped with
Data::Dumper are sorted (`$Data::Dumper::Sortkeys`).

### Determinism of web pages
To make pages comparable both runners generate random tokens
(`Session::Token`: CSRF and submission tokens, session IDs, widget grid IDs,
password reset codes) as a counter, `T` followed by the number padded with
zeros to the token length, reset for each scenario. Random passwords
(`GADS::_random_pw`, `CtrlO::Crypt::XkcdPassword`) come from the same
counter as `Pw` followed by the number padded to 4 digits, and SAML request
IDs (`Net::SAML2::Util::generate_id`) as `NETSAML2_T` followed by the number
padded to 63 digits. Perl's JSON encoding is
made canonical (sorted keys); Go always sorts keys.

The user export (`GADS::Users` csv) runs in a forked grandchild in GADS,
which would write to its own copy of the in-memory database. The Perl runner
makes `fork` return 0 and `POSIX::_exit` do nothing when called from
`GADS::Users`, so the export is produced inline before the response; the Go
port always produces it inline.

## Observation file

```json
{
  "scenario": "search_basic",
  "runner": "perl",
  "steps": [
    {"id": "1:sheet", "observed": {...}, "error": null, "pass": true, "failures": []}
  ]
}
```

With `GADS_BENCH` set, each step also has `"timing": {"runs": 1,
"total_ms": ..., "min_ms": ..., "median_ms": ..., "max_ms": ...}` and the
result an `"elapsed_ms"` (see Benchmarks); they are not compared.

`db` steps take `op: update|delete|insert|select|count|max` with `table`,
`where`, `set` (with `clear_layouts: true` to clear cached layouts after an
update), for `max` the `column`, and for `select` the `columns` to return
(ordered by id, as stored text) with optional `columns_match: {column: regex}`
to report only whether each value matches (1/0), for values such as JSON
whose key order Perl does not fix.

Values are normalised before comparison: Perl reference addresses
(`HASH(0x...)`) are compared as `HASH(0x0)`, Lua chunk names in error messages
(`[string "..."]`) are replaced by `[string]`, and wall-clock dependent
values are only reported when `fixed_time` controls them.

## Running

* `./run_perl.sh [--pg] [scenario.yaml ...]` – the Perl runner alone; its
  observations are the reference results (`results/perl`).
* `./run_go.sh [--pg] [scenario.yaml ...]` – the Go runner alone
  (`results/go`); `go test ./conformance` in gads-go also checks parity
  against the saved reference results.
* `./run_all.sh [--pg] [filter]` – both, then `gads-compare`.

With `--pg` the scenarios run on PostgreSQL: a disposable server is started
with Docker if needed (`docker-compose.test-pg.yml`, 127.0.0.1:55432, data
in memory) unless `GADS_TEST_PG` is already set, and the observations go to
`results/perl-pg` and `results/go-pg` (so the SQLite references are kept).
`go test ./conformance` compares with `results/perl-pg` when
`GADS_TEST_PG` is set. Stop the server with
`docker compose -f docker-compose.test-pg.yml down`.

## Benchmarks

The same machinery measures performance. With `GADS_BENCH` set in the
environment, both runners add to each step's observation a `timing`
(`runs`, `total_ms`, `min_ms`, `median_ms`, `max_ms`, the times of its runs)
and to the result an `elapsed_ms` for the whole scenario. The comparison of
observations ignores them, so a benchmark run is a parity check too.

* `bench/*.yaml` – the benchmark scenarios, generated by
  `generators/gen_bench.py [--records N]` (default 1000), with pseudo-random
  but deterministic data: `b01_records` (record creation, get and update,
  searches, sorts, filters, grouping, graphs, CSV download, recalculating the
  calculated fields), `b02_import` (CSV import), `b03_lua` (code
  evaluation) and `b04_web` (login, data table, records API, record page,
  download). Steps are repeated with `runs`; the medians are compared.
* Unit micro-benchmarks, of the same name and work in both: Lua evaluation
  (`lua_run`), Template Toolkit, JSON, CSV, CLDR dates, filters and password
  hashing, and on a database (`db/`, `calc/`): counting, a sorted page of
  records, a quick search, a record fetch, a view filter, grouped results,
  a CSV download (`csv_header`/`csv_line`), a record update and a calculated
  field evaluated through the column code. Results without a defined order
  (groups) are compared sorted; the record update runs last, as it changes
  the fixture. Go: `internal/benchunits` (`go test -run '^$'
  -bench . ./internal/benchunits`, or `gads-bench units` for JSON); Perl:
  `perl/bench_units.pl`. Each unit runs once to warm up, then in 5 samples
  of at least `--time`/5 seconds; the result is the median of the samples'
  mean time per run, with their spread (half the range, as a percentage of
  the median).
  * Each unit's operation returns its result, which both render as the same
    text (e.g. the lines of the records, the re-encoded JSON); its SHA-256
    is reported as `output_sha256`, and `gads-bench compare` marks a unit
    whose Perl and Go outputs differ as DIFFERENT (and fails), so that the
    two cannot silently do different work.
  * The units use the code the applications use: the JSON units encode
    with sorted keys and no HTML escaping (as the Go application does) and
    decode that encoding; the CSV units parse as GADS::Import and write as
    GADS::Records csv_line (one Text::CSV::Encoded) and its Go port; the
    date units parse as GADS::DateTime::parse_datetime (a parser created
    for each value).
  * The Lua units run the same C Lua 5.3.6 on both sides (vendored through
    cgo in Go, Debian's liblua5.3 under Inline::Lua, built alike: -O2), so
    they measure the glue around it.
  * The `db/` and `calc/` units time engine calls on the database of the
    fixture scenario `bench/fixtures/units_db.yaml` (the sheets of
    `b01_records` and the views of the units, generated by `gen_bench.py`), set up once by each
    conformance runner (`conformance.Setup` in Go;
    `GADS::Conformance::Runner` in `bench_units.pl`, which loads the
    runner's patches, `GADS::Conformance::Patches`, as `run.pl` does). The
    Perl GADS runs as it is: on SQLite its schema lacks the record index
    that the Go port adds, so its searches are quadratic in the number of
    records (see `patches/record-later-sqlite-index.patch`).
  The Perl output records the JSON and CSV backends in use, as the
  pure-Perl ones are much slower than the XS ones.
* `./bench_perl.sh` / `./bench_go.sh [--pg] [--time SECS] [--no-units]
  [--no-scenarios] [bench/x.yaml ...]` – one implementation, into
  `results/bench-perl` / `results/bench-go` (`-pg` with `--pg`) and
  `results/bench-units-perl.json` / `results/bench-units-go.json`.
* `./bench_all.sh [same options]` – both, then `gads-bench compare`: each
  step's median times side by side with the ratio ("go faster", Perl/Go),
  the geometric mean of the ratios, each scenario's elapsed time with its
  parity, and the units; also written as Markdown to
  `results/bench-report.md`. It fails if a step fails or the observations
  differ.

* `./bench_docker.sh [same options]` – the same in containers
  (`docker-compose.bench.yml`), so that both are measured on equal terms:
  the Perl GADS in `gads-perl-bench` (`docker/perl/Dockerfile`: Perl 5.40 on
  Debian trixie, the CPAN modules at the versions of the reference install,
  pinned in `docker/perl/cpanfile.pinned` by `docker/perl/gen-cpanfile.sh`,
  the ctrlo Inline::Lua on Lua 5.3, and the XS JSON and CSV backends of a
  production install), and the Go port in the Go builder image (Debian
  trixie). With `--pg` both use a PostgreSQL container on their network.
  The results have the suffix `-docker` (`results/bench-perl-docker`,
  `results/bench-report-docker.md`, ...); `BENCH_CPUS` limits the CPUs of
  each container. (Reports are `bench-report[-pg][-docker].md`.) The
  conformance suite on the image's Perl matches the reference results only
  with `PERL_JSON_BACKEND=JSON::PP`: with JSON::XS, GADS emits some numbers
  (ids, column options) as JSON strings.

Timings are of the runners in one process (no HTTP server: web steps call
the application in-process), so they measure the application code, not
start-up or the network. The Go SQLite driver is the C library (mattn/go-sqlite3, as DBD::SQLite); `go build -tags modernc` uses the pure Go one instead, which is about 2.5 times slower on large queries. On PostgreSQL (`--pg`) the test server runs with
`fsync` off and its data in memory.

## PostgreSQL

With `GADS_TEST_PG` set to libpq connection parameters (for example
`host=/var/run/postgresql`, with the password, if any, in `~/.pgpass`) both
runners run each scenario against a new PostgreSQL database
(`gads_conf_perl_<pid>_<n>` / `gads_conf_go_<pid>_<n>`, dropped afterwards)
deployed with the PostgreSQL schema, instead of SQLite. The role needs
`CREATEDB`. The observations of the two runners are compared as for SQLite
(they are not expected to equal the SQLite ones, as GADS itself behaves
differently: case-sensitive `LIKE`, sequences that collide with the
explicit IDs of the fixtures, errors of SQL that SQLite accepts).

For PostgreSQL:

* Errors of DBD::Pg are reported as GADS reports them through Log::Report:
  `DBI Exception: DBD::Pg::st execute failed: ERROR:  <message>`, or an
  empty message when libpq gives the error over several lines (with a
  detail, hint, context or position), as only a first line ending with its
  location is decoded. Once a web step has loaded the web application (which
  sets the schema's `exception_action` to panic), errors are reported in
  full. The `[for Statement "..."]` suffix is removed by both runners.
* `db` `select` returns values as the driver gives them (DBD::Pg gives text,
  so binary columns are not cast).
* `layout` `write_internal` reports the internal columns ordered by name, as
  they share a position, which PostgreSQL orders by its plan.
* `GADS_RAW_ERRORS=1` makes the Perl runner print each raw error (debugging).
