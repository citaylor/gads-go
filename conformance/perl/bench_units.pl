#!/usr/bin/env perl
# Unit micro-benchmarks of the Perl GADS building blocks, with the same
# names and work as the Go units (gads-go/internal/benchunits):
#
#   perl bench_units.pl --gads ../upstream/GADS [--time 1] [--fixture FILE]
#       [--out units-perl.json] [unit ...]
#
# Each unit is run once to warm up, then in 5 samples of at least --time/5
# seconds (and at least 3 runs) each; the result is the median of the
# samples' mean time per run. Each unit's operation returns its result,
# rendered as the same text as the Go unit renders it; its SHA-256 is
# reported with the times, and gads-bench compare checks that it is the
# Go one. The db/ and calc/ units time GADS calls on the database of the
# fixture scenario (--fixture, by default bench/fixtures/units_db.yaml),
# set up by the conformance runner. GADS runs as it is (its schema as
# deployed, without the record index the Go port adds on SQLite).
use strict;
use warnings;

BEGIN {
    # As perl/run.pl: the patches making GADS deterministic, before
    # DateTime and GADS are loaded (the fixture is set up as the
    # conformance runner sets up a scenario)
    require FindBin;
    unshift @INC, "$FindBin::Bin/lib";
    require GADS::Conformance::Patches;
}

use Getopt::Long;
use File::Temp ();
use Time::HiRes ();
use Digest::SHA ();
use Encode ();
use MIME::Base64 ();

my ($gads, $out, $time) = (undef, undef, 1);
my $fixture_file = "$FindBin::Bin/../bench/fixtures/units_db.yaml";
GetOptions('gads=s' => \$gads, 'out=s' => \$out, 'time=f' => \$time, 'fixture=s' => \$fixture_file)
    or die "usage: $0 --gads DIR [--time SECS] [--fixture FILE] [--out FILE] [unit ...]\n";
$gads or die "--gads is required\n";
unshift @INC, "$gads/lib", "$gads/t/lib";

require JSON;
require Text::CSV;
require Text::CSV::Encoded;
require Template;
require DateTime;
require DateTime::Format::CLDR;
require Crypt::SaltedHash;
require GADS::Layout; # (loads the columns, GADS::Column::Code among them, in order)
require GADS::Filter;
require GADS::Column::Code; # Inline::Lua lua_run
require GADS::Column::Calc;
require GADS::Records;
require GADS::Record;
require GADS::View;
require GADS::Conformance::Runner;

# The code given to lua_run, as GADS::Column::Code::eval builds it: the
# variable mapping, then the body of the evaluate function
my $lua_simple = qq(L1integer1 = vars["L1integer1"]\n return L1integer1 * 2 );
my $lua_loop = qq(L1integer1 = vars["L1integer1"]\n\n  local t = 0\n  for i = 1, 10000 do t = t + (i * L1integer1) % 13 end\n  return t\n);
my $lua_strings = qq(L1string1 = vars["L1string1"]\n\n  local parts = {}\n  for w in string.gmatch(L1string1, "%a+") do table.insert(parts, string.upper(w)) end\n  return table.concat(parts, "-")\n);

my $tt_template = <<'TT';
<h1>[% title | html %]</h1>
<table>
[% FOREACH row IN rows %]
  <tr class="[% loop.index % 2 ? 'odd' : 'even' %]">
    <td>[% row.id %]</td>
    <td>[% row.name | html %]</td>
    [% IF row.value > 500 %]<td class="high">[% row.value %]</td>[% ELSE %]<td>[% row.value %]</td>[% END %]
  </tr>
[% END %]
</table>
[% rows.size %] rows
TT

my $filter_json = '{"rules":[{"id":"9","type":"integer","operator":"greater","value":"5000"},'
    . '{"condition":"OR","rules":[{"id":"10","type":"string","operator":"equal","value":"foo2"},'
    . '{"id":"12","type":"date","operator":"less","value":"2003-01-01"}]}],"condition":"AND"}';

# The JSON units do the work of the Go ones: encode the rows as the Go
# application does (sorted keys; JSON does not escape HTML) and decode that
# encoding (the outputs are those of benchunits.JSONSHA256)
my $json_enc = JSON->new->utf8->canonical;
my $json_dec = JSON->new->utf8;

# The data of the JSON, template and CSV units, as benchunits.Rows and CSV
sub rows
{   my $n = shift;
    [ map +{
        id    => $_ + 1,
        name  => sprintf('Row <%d> & "name"', $_ + 1),
        value => ($_ * 37) % 1000,
        tags  => [ 'a', 'b', 't'.($_ % 7) ],
    }, 0 .. $n - 1 ];
}

sub csv_data
{   my $n = shift;
    my $s = "string1,integer1,enum1,date1,text1\n";
    $s .= sprintf qq("Name %d, the ""first""",%d,foo%d,2014-%02d-%02d,"multi\nline %d"\n),
        $_, $_ * 3, $_ % 3 + 1, $_ % 12 + 1, $_ % 28 + 1, $_
        foreach 0 .. $n - 1;
    $s;
}

my $noop = sub { };
my $password = 'correct horse battery staple';

# The fixture of the db/ and calc/ units, set up on first use (they do not
# change it)
my $fixture;
sub fixture
{   return $fixture if $fixture;
    my $runner = GADS::Conformance::Runner->new(file => $fixture_file, gads => $gads);
    my $result = $runner->run;
    die "$fixture_file: $result->{fatal}\n" if $result->{fatal};
    my @failed = map "$_->{id}: @{$_->{failures}}", grep !$_->{pass}, @{$result->{steps}};
    die "$fixture_file: steps failed: @failed\n" if @failed;
    my $sheet  = $runner->_sheet('main');
    my $layout = $sheet->layout;
    $fixture = {
        runner => $runner,
        schema => $runner->schema,
        layout => $layout,
        user   => $sheet->user,
        # The columns of the db/ units (those of the page step of b01_records)
        columns => [ map $runner->_col($layout, $_),
            qw/string1 integer1 enum1 tree1 date1 daterange1 curval1 calc1/ ],
    };
}
END { GADS::Conformance::Runner::pg_cleanup() if $fixture }

# The current ID of the record and the strings of its values
sub record_line
{   my ($rec, @cols) = @_;
    join "\t", $rec->current_id, map {
        my $d = $rec->get_field_value($_);
        defined $d ? $d->as_string : '';
    } @cols;
}

sub lines_output { join "\n", @{$_[0]} }

# The lines sorted (for results in no defined order)
sub sorted_lines_output { join "\n", sort @{$_[0]} }

# The view of the fixture of the alias (the "as" of its view step)
sub fixture_view
{   my ($f, $alias) = @_;
    my $id = $f->{runner}->{views}->{$alias} // die "Unknown view $alias";
    GADS::View->new(id => $id, instance_id => $f->{layout}->instance_id,
        layout => $f->{layout}, schema => $f->{schema});
}

# A unit timing a GADS call on the fixture, whose setup returns the timed
# operation
sub fixture_unit
{   my ($setup, $output) = @_;
    {
        setup  => sub { $setup->(fixture()) },
        output => $output,
    };
}

# The code of calc/eval_column: that of the calc of the benchmark sheets
my $calc_code = <<'LUA';
function evaluate (L1integer1, L1date1, L1string1)
  if L1integer1 == nil then return 0 end
  local total = 0
  for i = 1, 20 do
    total = total + (L1integer1 * i) % 7
  end
  if L1date1 then total = total + L1date1.year end
  if L1string1 and string.find(L1string1, "alpha") then total = total + 1000 end
  return total
end
LUA
chomp $calc_code;

sub lua_unit
{   my ($code, $vars) = @_;
    {
        setup => sub {
            sub {
                my $ret = GADS::Column::Code::lua_run($code, $vars, $noop, $noop);
                die "lua: $ret->{error}" if $ret->{error};
                $ret;
            };
        },
        output => sub { $_[0]->{return} },
    };
}

sub db_unit
{   my ($op, $output) = @_;
    {
        setup => sub {
            my $f = fixture();
            sub { $op->($f) };
        },
        output => $output,
    };
}

# name => { setup returning the operation that is timed, which returns its
# result; output rendering a result as the text of the Go unit (none: the
# result is a string) }
my @units = (
    'lua/simple'  => lua_unit($lua_simple, { L1integer1 => 21 }),
    'lua/loop'    => lua_unit($lua_loop, { L1integer1 => 7 }),
    'lua/strings' => lua_unit($lua_strings, { L1string1 => 'the quick brown fox jumps over the lazy dog' }),
    'tt/table_100' => { setup => sub {
        my $dir = File::Temp->newdir;
        open my $fh, '>:encoding(UTF-8)', "$dir/table.tt" or die $!;
        print $fh $tt_template;
        close $fh;
        my $tt = Template->new(INCLUDE_PATH => "$dir", ENCODING => 'UTF-8', ANYCASE => 1);
        my $vars = { title => 'Records & <values>', rows => rows(100) };
        sub {
            my $keep = $dir;
            my $output = '';
            $tt->process('table.tt', $vars, \$output) or die $tt->error;
            $output;
        };
    } },
    'json/encode_1000' => { setup => sub {
        my $rows = rows(1000);
        sub { $json_enc->encode($rows) };
    } },
    'json/decode_1000' => {
        setup => sub {
            my $js = $json_enc->encode(rows(1000));
            sub { $json_dec->decode($js) };
        },
        output => sub { $json_enc->encode($_[0]) },
    },
    'csv/parse_1000' => {
        setup => sub {
            my $data = csv_data(1000);
            sub {
                # As GADS::Import
                my $csv = Text::CSV->new({ binary => 1 }) or die Text::CSV->error_diag;
                open my $fh, '<', \$data or die $!;
                my @rows;
                while (my $row = $csv->getline($fh)) { push @rows, $row }
                $csv->eof or die $csv->error_diag;
                \@rows;
            };
        },
        output => sub { join "\x1e", map { join "\x1f", @$_ } @{$_[0]} },
    },
    'csv/write_1000' => { setup => sub {
        my $rows = rows(1000);
        # As GADS::Records csv_line: one Text::CSV::Encoded, each line
        # combined and ended with a newline
        my $csv = Text::CSV::Encoded->new({ encoding => undef });
        sub {
            my $s = '';
            foreach my $r (@$rows)
            {
                $csv->combine($r->{id}, $r->{name}, $r->{value}) or die $csv->error_diag;
                $s .= $csv->string."\n";
            }
            $s;
        };
    } },
    'date/parse_cldr' => {
        # As GADS::DateTime::parse_datetime, which creates the parser each time
        setup => sub {
            sub {
                my $cldr = DateTime::Format::CLDR->new(pattern => 'yyyy-MM-dd HH:mm:ss');
                $cldr->parse_datetime('2014-10-10 12:34:56') or die "parse";
            };
        },
        output => sub { $_[0]->iso8601 },
    },
    'date/format_cldr' => { setup => sub {
        my $dt = DateTime->new(year => 2014, month => 10, day => 10,
            hour => 12, minute => 34, second => 56, time_zone => 'UTC');
        sub { $dt->format_cldr('EEEE dd MMMM yyyy HH:mm:ss') };
    } },
    'filter/parse_json' => {
        setup => sub {
            sub { GADS::Filter->new(as_json => $filter_json)->as_hash };
        },
        output => sub { $json_enc->encode($_[0]) },
    },
    'password/hash' => {
        setup => sub {
            sub {
                my $csh = Crypt::SaltedHash->new(algorithm => 'SHA-512');
                $csh->add($password);
                $csh->generate;
            };
        },
        # The scheme and length of the hash and whether the password
        # matches it (the salt is random)
        output => sub {
            my ($scheme, $b64) = $_[0] =~ /^\{(\w+)\}(.*)$/ or die "hash $_[0]";
            sprintf '%s %d %s', $scheme, length(MIME::Base64::decode_base64($b64)),
                Crypt::SaltedHash->validate($_[0], $password) ? 1 : '';
        },
    },
    'password/match' => {
        setup => sub {
            my $csh = Crypt::SaltedHash->new(algorithm => 'SHA-512');
            $csh->add($password);
            my $hash = $csh->generate;
            sub { Crypt::SaltedHash->validate($hash, $password) };
        },
        output => sub { $_[0] ? 1 : '' },
    },
    'db/count' => db_unit(sub {
        my $f = shift;
        GADS::Records->new(user => $f->{user}, layout => $f->{layout}, schema => $f->{schema})->count;
    }, sub { $_[0] }),
    'db/page_50' => db_unit(sub {
        my $f = shift;
        my @cols = @{$f->{columns}};
        my $records = GADS::Records->new(user => $f->{user}, layout => $f->{layout},
            schema => $f->{schema}, rows => 50, page => 2, columns => [ map $_->id, @cols ]);
        $records->sort([{ id => $cols[1]->id, type => 'desc' }]);
        [ map record_line($_, @cols), @{$records->results} ];
    }, \&lines_output),
    'db/quick_search' => db_unit(sub {
        my $f = shift;
        my $records = GADS::Records->new(user => $f->{user}, layout => $f->{layout},
            schema => $f->{schema}, search => 'bravo');
        [ map record_line($_, $f->{columns}->[0]), @{$records->results} ];
    }, \&lines_output),
    'db/record_get' => db_unit(sub {
        my $f = shift;
        my $rec = GADS::Record->new(user => $f->{user}, layout => $f->{layout}, schema => $f->{schema});
        $rec->find_current_id(51);
        [ record_line($rec, @{$f->{columns}}) ];
    }, \&lines_output),
    'db/filter_view' => fixture_unit(sub {
        my $f = shift;
        my $view = fixture_view($f, 'v_filter');
        my @cols = @{$f->{columns}}[0, 1];
        sub {
            my $records = GADS::Records->new(user => $f->{user}, layout => $f->{layout},
                schema => $f->{schema}, view => $view);
            [ map record_line($_, @cols), @{$records->results} ];
        };
    }, \&lines_output),
    # (the groups are in no defined order: their lines are sorted)
    'db/group_enum' => fixture_unit(sub {
        my $f = shift;
        my $view = fixture_view($f, 'v_group');
        my @cols = @{$f->{columns}}[2, 1, 7];
        sub {
            my $records = GADS::Records->new(user => $f->{user}, layout => $f->{layout},
                schema => $f->{schema}, view => $view);
            [ map {
                my $rec = $_;
                # (the values first: GADS sets the count of a group as it
                # builds the record's values on their first use)
                my @values = map {
                    my $d = $rec->get_field_value($_);
                    defined $d ? $d->as_string : '';
                } @cols;
                join "\t", $rec->id_count, @values;
            } @{$records->results} ];
        };
    }, \&sorted_lines_output),
    'db/csv_100' => fixture_unit(sub {
        my $f = shift;
        my $view = fixture_view($f, 'v_csv');
        sub {
            my $records = GADS::Records->new(user => $f->{user}, layout => $f->{layout},
                schema => $f->{schema}, view => $view, rows => 100, page => 1);
            my $csv = $records->csv_header;
            while (my $line = $records->csv_line) { $csv .= $line }
            $csv;
        };
    }),
    'calc/eval_column' => db_unit(sub {
        my $f = shift;
        my $col = GADS::Column::Calc->new(schema => $f->{schema}, user => undef,
            layout => $f->{layout}, return_type => 'integer', multivalue => 0);
        my $ret = $col->eval($calc_code, {
            L1integer1 => 1234,
            L1date1    => { year => 2010, month => 5, day => 3 },
            L1string1  => 'alpha bravo',
        });
        die "calc: $ret->{error}" if $ret->{error};
        $ret->{return};
    }, sub { $_[0] }),
    # (last: it changes the fixture, a new version of the record each run)
    'db/record_update' => fixture_unit(sub {
        my $f = shift;
        my $n = 0;
        my $string1 = $f->{columns}->[0];
        sub {
            $n++;
            my $rec = GADS::Record->new(user => $f->{user}, layout => $f->{layout}, schema => $f->{schema});
            $rec->find_current_id(52);
            $rec->get_field_value($string1)->set_value("updated $n");
            $rec->write(no_alerts => 1);
            my $after = GADS::Record->new(user => $f->{user}, layout => $f->{layout}, schema => $f->{schema});
            $after->find_current_id(52);
            [ record_line($after, @{$f->{columns}}) ];
        };
    }, \&lines_output),
);

my $samples = 5;
my %want = map { $_ => 1 } @ARGV;
my @results;
while (my ($name, $unit) = splice @units, 0, 2)
{
    next if %want && !grep { index($name, $_) >= 0 } keys %want;
    my $op = $unit->{setup}->();
    my $output = $op->();
    $output = $unit->{output}->($output) if $unit->{output};
    $output = '' if !defined $output;
    $output = Encode::encode_utf8($output) if utf8::is_utf8($output);
    my ($iterations, @ns) = (0);
    foreach (1..$samples)
    {
        my ($n, $elapsed) = (0, 0);
        my $start = [Time::HiRes::gettimeofday()];
        while ($n < 3 || $elapsed < $time / $samples)
        {
            $op->();
            $n++;
            $elapsed = Time::HiRes::tv_interval($start);
        }
        $iterations += $n;
        push @ns, $elapsed * 1e9 / $n;
    }
    my @sorted = sort { $a <=> $b } @ns;
    my $median = $sorted[@sorted / 2];
    printf STDERR "%-24s %10d %16.1f ns/op ±%.0f%%\n", $name, $iterations, $median,
        ($sorted[-1] - $sorted[0]) / 2 / $median * 100;
    my $r = sub { 0 + sprintf '%.1f', $_[0] };
    push @results, {
        name          => $name,
        iterations    => $iterations,
        ns_per_op     => $r->($median),
        min_ns        => $r->($sorted[0]),
        max_ns        => $r->($sorted[-1]),
        samples       => [ map $r->($_), @ns ],
        output_sha256 => Digest::SHA::sha256_hex($output),
    };
}

my $result = {
    runner   => 'perl',
    perl     => sprintf('%vd', $^V),
    backends => { json => JSON->backend, csv => Text::CSV->backend },
    units    => \@results,
};
my $json = JSON->new->canonical->pretty->encode($result);
if ($out)
{
    open my $fh, '>', $out or die "$out: $!";
    print $fh $json;
}
else {
    print $json;
}
