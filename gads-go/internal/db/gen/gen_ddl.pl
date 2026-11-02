#!/usr/bin/env perl
# Writes sql_sqlite.sql and sql_postgres.sql: the tables that GADS deploys
# (DBIx::Class deployment_statements of GADS::Schema, through
# SQL::Translator), in the format that db.go embeds. Run from gads-go/:
#
#   perl -I ../upstream/GADS-dev/lib internal/db/gen/gen_ddl.pl
#
# SQLite also gets the Go-only index record_idx_current_id_approval_created
# (see CLAUDE.md, Deviations).
use strict;
use warnings;
use GADS::Schema;

my $schema = GADS::Schema->connect("dbi:SQLite::memory:", "", "", { quote_names => 1 });

sub statements
{   my $db = shift;
    my @st = $schema->deployment_statements($db, undef, undef,
        { add_drop_table => 0, no_comments => 1, quote_identifiers => 1 });
    s/^\s+|\s+$//g for @st;
    grep { $_ ne '' && !/^(BEGIN TRANSACTION|COMMIT)$/ } @st;
}

sub write_file
{   my ($file, $content) = @_;
    open my $fh, '>', $file or die "$file: $!";
    print $fh $content;
    close $fh or die "$file: $!";
}

my @sqlite = map {
    /^CREATE INDEX "record_idx_current_id" /
        ? ($_, 'CREATE INDEX "record_idx_current_id_approval_created" ON "record" ("current_id", "approval", "created", "id")')
        : $_
} statements('SQLite');
write_file('internal/db/sql_sqlite.sql', join(";\n\n", @sqlite).";\n");
write_file('internal/db/sql_postgres.sql', join "", map "$_;\n\n", statements('PostgreSQL'));
