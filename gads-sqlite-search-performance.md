# GADS: searches are quadratic in the number of records on SQLite

## Summary

On SQLite, every GADS search (record counts, data table pages, filters,
quick search, grouping, graphs) takes time quadratic in the number of
records. SQLite looks up the later versions of each record (the
`record_later` join) through the index on `record.approval`, which matches
nearly every row, when it should use `current_id`. With 1,000 records a
simple count of the records in a sheet takes about **1 second**.

Adding one composite index on `record` fixes it: the same count takes
**about 2 ms**, and searches in the GADS test workload became 4–30 times
faster.

PostgreSQL is **not** affected. Its planner picks a hash anti-join from
its table statistics and runs the same query in about 2 ms.

## Affected

- GADS on SQLite: the test suite, development setups and any SQLite
  deployment.
- Not PostgreSQL. MySQL was not tested.
- Tested with Perl 5.32.1 and DBD::SQLite 1.78 (SQLite 3.51.1).

## The cause

To show only the latest version of each record, searches join `record` to
itself through the `record_later` relationship
(`lib/GADS/Schema/Result/Record.pm`, `$join_sub`). The generated SQL looks
like this:

```sql
SELECT COUNT(*) FROM (
  SELECT me.id FROM current me
  LEFT JOIN record record_single ON record_single.current_id = me.id
  LEFT JOIN record record_later ON (
        (record_later.created > record_single.created
         OR (record_later.created = record_single.created
             AND record_later.id > record_single.id))
    AND record_later.approval   = ?
    AND record_later.current_id = record_single.current_id )
  WHERE me.instance_id = ? AND me.deleted IS NULL AND me.parent_id IS NULL
    AND me.draftuser_id IS NULL
    AND record_later.current_id IS NULL      -- no later version exists
  GROUP BY me.id
) me
```

For each version of a record, SQLite looks for a later version of the same
record. Two single-column indexes could serve that lookup:
`record_idx_current_id` and `record_idx_approval` (added in
`Record::sqlt_deploy_hook`). Without table statistics (no `ANALYZE`),
SQLite rates them as equally selective and chooses `approval`:

```
SEARCH record_later USING INDEX record_idx_approval (approval=?) LEFT-JOIN
```

Almost every record has `approval = 0`, so each lookup scans nearly the
whole `record` table. With 1,000 records of 1–5 versions each (3,015 rows),
that is about 9 million row comparisons for one count. Every search
contains this join, so they are all slow, and they get slower with the
square of the data.

## Measurements

The query above was run through DBI with DBD::SQLite 1.78 (SQLite
3.51.1), on 1,000 records with 3,015 versions (2% awaiting approval). Times
are the median of 7 runs, and every variant returned the same 1,000 rows.

| Variant | Time | Plan for `record_later` |
|---|---:|---|
| Current schema | **953 ms** | `record_idx_approval (approval=?)` |
| After `ANALYZE` | 2.3 ms | `record_idx_current_id (current_id=?)` |
| **Composite index (proposed)** | **1.8 ms** | covering index `(current_id=? AND approval=?)` |
| `+record_later.approval = ?` (disables that index) | 2.3 ms | `record_idx_current_id` |
| Rewritten as `NOT EXISTS` | 296 ms | still `record_idx_approval` |
| PostgreSQL 16, current schema | 2.1 ms | hash anti-join |

`ANALYZE` also fixes it, but only until the data changes, and someone has
to run it. Changing the SQL is awkward to do from DBIx::Class. The index
needs neither.

## Proposed fix

Add an index that matches the join exactly, on SQLite only:

```sql
CREATE INDEX record_idx_current_id_approval_created
    ON record (current_id, approval, created, id);
```

SQLite then finds the versions of one record through the index, and checks
their time order from the index alone, without reading the table:

```
SEARCH record_later USING COVERING INDEX record_idx_current_id_approval_created (current_id=? AND approval=?)
```

The patch below makes that change. It only adds the index when deploying to
SQLite, so the PostgreSQL and MySQL schemas, and therefore their
migrations, do not change. SQLite databases are deployed fresh, so no
migration is needed for them either.

```diff
--- a/lib/GADS/Schema/Result/Record.pm
+++ b/lib/GADS/Schema/Result/Record.pm
@@ -409,6 +409,17 @@
 sub sqlt_deploy_hook {
     my ($self, $sqlt_table) = @_;
     $sqlt_table->add_index(name => 'record_idx_approval', fields => [ 'approval' ]);
+    # Find the later versions of a record (the record_later join of every
+    # search) by record. Without this, SQLite uses record_idx_approval for
+    # the join, which matches almost every row, so that searches take time
+    # quadratic in the number of records (about 1 second to count 1000
+    # records). PostgreSQL chooses a hash join from its statistics and does
+    # not need it, so it is only added for SQLite (which is not migrated).
+    my $producer = $sqlt_table->schema->translator->producer_type || '';
+    $sqlt_table->add_index(
+        name   => 'record_idx_current_id_approval_created',
+        fields => [qw/current_id approval created id/],
+    ) if $producer =~ /SQLite/;
 }
 
 # Enable finding of latest record for current ID
```

## The Perl fix in full

### 1. Schema: `lib/GADS/Schema/Result/Record.pm`

After the patch, `sqlt_deploy_hook` reads:

```perl
sub sqlt_deploy_hook {
    my ($self, $sqlt_table) = @_;
    $sqlt_table->add_index(name => 'record_idx_approval', fields => [ 'approval' ]);
    # Find the later versions of a record (the record_later join of every
    # search) by record. Without this, SQLite uses record_idx_approval for
    # the join, which matches almost every row, so that searches take time
    # quadratic in the number of records (about 1 second to count 1000
    # records). PostgreSQL chooses a hash join from its statistics and does
    # not need it, so it is only added for SQLite (which is not migrated).
    my $producer = $sqlt_table->schema->translator->producer_type || '';
    $sqlt_table->add_index(
        name   => 'record_idx_current_id_approval_created',
        fields => [qw/current_id approval created id/],
    ) if $producer =~ /SQLite/;
}
```

To apply the diff above from the GADS checkout, with the diff saved as
`record-later-sqlite-index.patch`:

```sh
patch -p1 < record-later-sqlite-index.patch
```

Every SQLite database deployed after that has the index, including the
ones the test suite creates.

### 2. Existing SQLite databases

A database deployed before the change does not get the index
automatically, because SQLite databases are not migrated. Add it once with
this script, which is safe to run more than once:

```perl
#!/usr/bin/env perl
# Usage: add-record-index.pl /path/to/gads.db
use strict;
use warnings;
use DBI;

my $file = shift or die "Usage: $0 /path/to/gads.db\n";
my $dbh = DBI->connect("dbi:SQLite:dbname=$file", "", "", { RaiseError => 1 });
$dbh->do(q(CREATE INDEX IF NOT EXISTS "record_idx_current_id_approval_created"
    ON "record" ("current_id", "approval", "created", "id")));
# Optional: give the planner statistics too
$dbh->do("ANALYZE");
print "Index added\n";
```

The same statement can also be run from the `sqlite3` shell.

### 3. Regression test: `t/009_record_later_index.t`

This test checks two things:

- the index is deployed;
- the SQL GADS actually generates for a records count looks up
  `record_later` through it.

It passes with the patch. Without it, it fails twice: on the index check,
and because the plan uses `record_idx_approval`. On a database other than
SQLite, it skips.

```perl
use Test::More;
use strict;
use warnings;

use Log::Report;
use GADS::Records;

use lib 't/lib';
use Test::GADS::DataSheet;

# Searches find the later versions of a record (record_later) by record, not
# through record_idx_approval, which made them quadratic in the number of
# records on SQLite
my $sheet = Test::GADS::DataSheet->new;
my $schema = $sheet->schema;
$sheet->create_records;

plan skip_all => 'SQLite only'
    unless $schema->storage->sqlt_type eq 'SQLite';

my $dbh = $schema->storage->dbh;
ok($dbh->selectrow_array(q(SELECT 1 FROM sqlite_master WHERE type = 'index'
    AND name = 'record_idx_current_id_approval_created')), "Index deployed");

# Capture the SQL of a records count, and check its plan
my @queries;
$schema->storage->debugobj(Test::RecordLater::Capture->new(\@queries));
$schema->storage->debug(1);
my $records = GADS::Records->new(
    user   => $sheet->user,
    layout => $sheet->layout,
    schema => $schema,
);
is($records->count, 2, "Record count");
$schema->storage->debug(0);

my ($count) = grep { /record_later/ && /COUNT/ } map { $_->[0] } @queries
    or die "No count query captured";
my ($bind) = grep { $_->[0] eq $count } @queries;
my $plan = join "\n", map { $_->[3] }
    @{$dbh->selectall_arrayref("EXPLAIN QUERY PLAN $count", undef, @{$bind->[1]})};
my @later = grep { /record_later/ } split /\n/, $plan;
ok(@later, "Plan joins record_later");
like($_, qr/record_idx_current_id_approval_created/, "record_later found by record: $_")
    foreach @later;

done_testing;

package Test::RecordLater::Capture;

sub new { my ($class, $queries) = @_; bless { queries => $queries }, $class }

sub query_start
{   my ($self, $sql, @bind) = @_;
    # Bind values are given quoted by DBIx::Class
    push @{$self->{queries}}, [ $sql, [ map { s/^'(.*)'$/$1/r } @bind ] ];
}

sub query_end {}
sub txn_begin {}
sub txn_commit {}
sub txn_rollback {}
sub svp_begin {}
sub svp_release {}
sub svp_rollback {}
```

Run it with:

```sh
prove -Ilib t/009_record_later_index.t
```

## Results with the patch

These are timings from a benchmark workload of 1,000 records (median per
step, in ms) on the Perl application, before and after the patch:

| Step | Before | After | Faster |
|---|---:|---:|---:|
| Count all records | 163 | 5.4 | 30× |
| Page of 50, sorted | 458 | 115 | 4.0× |
| Filter (date OR string OR curval) | 440 | 62 | 7.1× |
| Quick search | 1,384 | 141 | 9.8× |
| Group by enum | 494 | 19 | 26× |
| Group by curval | 643 | 29 | 22× |
| Bar graph (sum by enum) | 321 | 21 | 15× |
| Data table page (web) | 230 | 71 | 3.2× |
| Records API search (web) | 1,740 | 578 | 3.0× |

The results of every step (records, counts, values) are identical with and
without the patch.

## How to reproduce

1. Create a sheet on SQLite with about 1,000 records.
2. Time a records count (`GADS::Records->count`), or open the data table.
3. Run
   `EXPLAIN QUERY PLAN` on the search SQL (`DBIC_TRACE=1` shows it). The
   plan for `record_later` uses `record_idx_approval`.
4. Run the `CREATE INDEX` above and repeat: the time drops by orders of
   magnitude, and the plan uses the new index.

## Remaining slow query

One more search stays slow after the fix: a filter combining an integer
comparison with an enum value (`integer1 > 5000 AND enum1 = foo2`) takes
about 300 ms on 1,000 records. It is probably a similar planning issue in
the enum join, but it has not been investigated yet.
