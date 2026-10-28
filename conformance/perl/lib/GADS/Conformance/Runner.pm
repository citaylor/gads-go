package GADS::Conformance::Runner;

# Reference conformance runner: executes YAML scenarios against Perl GADS.

use strict;
use Time::HiRes ();
use warnings;

use B ();
use Encode ();
use JSON ();
use Log::Report 'linkspace', syntax => 'SHORT';
use Scalar::Util qw(blessed looks_like_number);
use YAML::XS ();

# The user export of GADS::Users (csv) forks a grandchild to produce the
# CSV, which with the in-memory database would write to the child's copy:
# it runs inline instead (fork returns 0 and the grandchild's exit is
# skipped), as other background work runs with GADS_NO_FORK
use POSIX ();
BEGIN {
    *CORE::GLOBAL::fork = sub { return 0 if caller eq 'GADS::Users'; CORE::fork() };
    my $exit = \&POSIX::_exit;
    no warnings 'redefine';
    *POSIX::_exit = sub { return if caller eq 'GADS::Users'; $exit->(@_) };
}

use GADS::Config;
use GADS::Filter;
use GADS::Layout;
use GADS::Column::Calc;
use GADS::Record;
use GADS::Records;
use GADS::RecordsGraph;
use GADS::Schema;
use GADS::View;
use GADS::Views;
use GADS::Instances;
use GADS::Graph;
use GADS::Graph::Data;
use GADS::Globe;
use GADS::Import;
use GADS::Users;
use GADS::Alert;
use GADS::AlertSend;
use GADS::AlertDescription;
use MIME::Base64 ();
use Encode ();
# Test::TempDir::Tiny (the uploads of Test::GADS::DataSheet) makes its
# directory under the directory it is loaded from: one of our own, so that
# the GADS checkout is not written to (it is read-only in the containers)
BEGIN {
    require Cwd;
    require File::Path;
    require File::Spec;
    my $dir = File::Spec->catdir(File::Spec->tmpdir, "gads-conformance-$<");
    File::Path::make_path($dir);
    my $cwd = Cwd::getcwd();
    chdir $dir or die "Unable to chdir to $dir: $!";
    require Test::TempDir::Tiny;
    chdir $cwd or die "Unable to chdir to $cwd: $!";
}
use Test::GADS::DataSheet;
use Template;
use GADS::Conformance::Web;
use File::Temp ();
use File::Path ();
use GADS::Email;

# Emails are captured instead of sent (see the emails step): the arguments
# given to GADS::Email->send, with its checks and its removal of duplicate
# recipients
our @EMAILS;
{
    no warnings 'redefine';
    *GADS::Email::send = sub {
        my ($self, $args) = @_;
        my $emails  = $args->{emails} or error __"Please specify some recipients to send an email to";
        my $subject = $args->{subject} or error __"Please enter a subject for the email";
        $args->{text} || $args->{html} or panic "No plain or HTML email text supplied";
        my %done;
        push @EMAILS, {
            subject  => $subject,
            to       => [ grep !$done{$_}++, @$emails ],
            text     => $args->{text},
            html     => $args->{html},
            reply_to => $args->{reply_to},
        };
    };
    # Emails of Dancer2::Plugin::Auth::Extensible (password resets) are
    # sent with Mail::Message, not GADS::Email
    require Dancer2::Plugin::Auth::Extensible;
    *Dancer2::Plugin::Auth::Extensible::_email_mail_message = sub {
        my ($plugin, %params) = @_;
        $params{plain} || $params{html} or die "No plain or HTML email text supplied";
        push @EMAILS, {
            subject  => $params{subject},
            to       => [ $params{to} ],
            text     => $params{plain},
            html     => $params{html},
            reply_to => undef,
        };
        1;
    };
}

sub new
{   my ($class, %args) = @_;
    bless {
        file    => $args{file},
        verbose => $args{verbose},
        gads_dir => $args{gads},
        sheets  => {},
        views   => {},
        records => {},
    }, $class;
}

sub write_json
{   my ($self, $file, $data) = @_;
    open my $fh, '>:raw', $file or die "Unable to write $file: $!";
    print $fh JSON->new->canonical->pretty->utf8->encode($data);
    close $fh;
}

# ----------------------------------------------------------------------
# Value normalisation for observations
# ----------------------------------------------------------------------

# Convert any Perl structure into plain strings/arrays/hashes so that the
# observation is independent of Perl's internal numeric/string flags
sub _plain
{   my ($v) = @_;
    return undef if !defined $v;
    if (ref $v eq 'ARRAY')
    {
        return [ map _plain($_), @$v ];
    }
    elsif (ref $v eq 'HASH')
    {
        return { map { $_ => _plain($v->{$_}) } keys %$v };
    }
    elsif (blessed $v && $v->isa('DateTime'))
    {
        return $v->clone->set_time_zone('UTC')->strftime('%Y-%m-%dT%H:%M:%SZ');
    }
    elsif (blessed $v && $v->isa('DateTime::Span'))
    {
        return _plain($v->start).'/'._plain($v->end);
    }
    elsif (ref $v eq 'JSON::PP::Boolean')
    {
        return $v ? 'true' : 'false';
    }
    elsif (ref $v)
    {
        return "$v";
    }
    return "$v";
}

# Tag scalars with the type that Inline::Lua would push to Lua
sub _lua_typed
{   my ($v) = @_;
    return undef if !defined $v;
    if (ref $v eq 'ARRAY')
    {
        return [ map _lua_typed($_), @$v ];
    }
    elsif (ref $v eq 'HASH')
    {
        return { map { $_ => _lua_typed($v->{$_}) } keys %$v };
    }
    elsif (ref $v)
    {
        return "ref:".ref($v);
    }
    my $class = B::class(B::svref_2object(\$v));
    # Mirror Inline::Lua push_val switch on SvTYPE
    return "i:$v" if $class eq 'IV';
    return "n:"._num($v) if $class eq 'NV';
    return "s:$v";
}

sub _num
{   my $n = shift;
    my $s = "$n";
    $s;
}

sub _error_text
{   my ($e) = @_;
    # GADS_RAW_ERRORS prints the errors as raised (debugging)
    if ($ENV{GADS_RAW_ERRORS})
    {
        my $raw = blessed $e && $e->can('died') && $e->died ? $e->died : $e;
        print STDERR "RAW ERROR: $raw\n";
    }
    my $msg = blessed $e && $e->can('message') ? $e->message : $e;
    $msg = $msg->toString if blessed $msg && $msg->can('toString');
    $msg = "$msg";
    $msg =~ s/\s+$//;
    $msg = _normalise_error($msg);
    $msg;
}

sub _normalise_error
{   my $msg = shift;
    return $msg if !defined $msg;
    # Lua chunk names depend on Perl hash ordering
    $msg =~ s/\[string "(?:[^"\\]|\\.)*"\]/[string]/g;
    # Perl internals: remove " at FILE line N." suffixes
    $msg =~ s/ at \S+ line \d+\.?$//mg;
    # The SQL of a DBI exception (generated differently by DBIx::Class and
    # the Go port)
    $msg =~ s/ \[for Statement ".*\z//s;
    $msg;
}

# ----------------------------------------------------------------------
# Main loop
# ----------------------------------------------------------------------

sub run
{   my $self = shift;
    my $file = $self->{file};
    my $scenario = YAML::XS::LoadFile($file);
    my $name = $scenario->{name} || basename($file) =~ s/\.ya?ml$//r;
    $self->{scenario} = $scenario;
    my $result = {
        scenario => $name,
        runner   => 'perl',
        steps    => [],
    };

    my $started = [Time::HiRes::gettimeofday()];
    my $time = $scenario->{fixed_time} || '2014-10-10 01:00:00';
    $self->_set_time($time);

    # The directory of uploaded files (GADS versions that keep files on
    # disk rather than in the database), one for each scenario, as the
    # upstream test library has
    $self->{uploads} = File::Temp->newdir;
    GADS::Config->instance->config({
        gads => {
            dateformat => 'yyyy-MM-dd',
            url        => 'http://localhost',
            uploads    => $self->{uploads}->dirname,
            %{$scenario->{config} || {}},
        },
    });

    $GADS::Conformance::TOKENS = 0;
    @EMAILS = ();
    my $i = 0;
    foreach my $step (@{$scenario->{steps} || []})
    {
        $i++;
        my ($type, $args) = %$step;
        $args ||= {};
        my $id = $args->{id} || "$i:$type";
        my $handler = $self->can("step_$type")
            or do {
                push @{$result->{steps}}, { id => $id, pass => JSON::false, failures => ["Unknown step type $type"] };
                next;
            };
        my ($observed, $error);
        my @messages;
        local $ENV{GADS_PANIC_ON_ENTERING_CODE} = 0;
        local $GADS::Schema::IGNORE_PERMISSIONS = 1 if $args->{ignore_permissions};
        # env: environment variables for the step
        my %env = %{$args->{env} || {}};
        local @ENV{keys %env} = values %env;
        my @exceptions;
        # runs: run the step several times (benchmarks), observing the
        # last run; the time of each run is reported with GADS_BENCH
        my $repeat = $args->{runs} || 1;
        my @times;
        my $run = sub {
            foreach (1..$repeat)
            {
                my $t0 = [Time::HiRes::gettimeofday()];
                $observed = $self->$handler($args);
                push @times, Time::HiRes::tv_interval($t0);
            }
        };
        if ($type eq 'http' || $type eq 'login')
        {
            # Web requests run outside a try block, so that the
            # application's own dispatchers (Dancer2::Plugin::LogReport)
            # handle its reports as in production
            eval { $run->(); 1 }
                or $error = "$@";
        }
        else {
            try {
                $run->();
            } hide => 'ALL';
            my $try = $@;
            @exceptions = $try->exceptions;
            if (my $e = $try->wasFatal)
            {
                $error = _error_text($e);
            }
        }
        foreach my $ex (@exceptions)
        {
            next if $ex->isFatal;
            if ($ex->reason eq 'WARNING' && $args->{capture_warnings})
            {
                # Warnings from code evaluation include a dump of the
                # parameters, which is in random hash order. Strip it.
                my $text = _error_text($ex);
                $text =~ s/ \(code: .*//s;
                push @messages, "WARNING: $text";
                next;
            }
            next unless $ex->reason eq 'ERROR' || $ex->reason eq 'MISTAKE';
            push @messages, $ex->reason.": "._error_text($ex);
        }
        my $obs = {
            id       => $id,
            type     => $type,
            observed => _plain($observed),
            error    => $error,
        };
        $obs->{lua} = $self->{_lua_obs} if $self->{_lua_obs};
        delete $self->{_lua_obs};
        $obs->{messages} = \@messages if @messages;
        $obs->{timing} = _timing(@times) if $ENV{GADS_BENCH};
        my @failures = $self->_check($args, $obs);
        $obs->{pass} = @failures ? JSON::false : JSON::true;
        $obs->{failures} = \@failures;
        push @{$result->{steps}}, $obs;
        if ($self->{verbose})
        {
            print STDERR "[$id] ".(@failures ? "FAIL: @failures" : "ok")."\n";
        }
    }
    $result->{elapsed_ms} = _ms(Time::HiRes::tv_interval($started)) if $ENV{GADS_BENCH};
    $result;
}

# The times of the runs of a step (GADS_BENCH), in milliseconds
sub _ms { 0 + sprintf "%.3f", $_[0] * 1000 }

sub _timing
{   my @t = sort { $a <=> $b } @_;
    return undef if !@t;
    my $total = 0;
    $total += $_ foreach @t;
    my $median = @t % 2 ? $t[$#t / 2] : ($t[@t / 2 - 1] + $t[@t / 2]) / 2;
    +{
        runs      => scalar @t,
        total_ms  => _ms($total),
        min_ms    => _ms($t[0]),
        median_ms => _ms($median),
        max_ms    => _ms($t[-1]),
    };
}

sub _check
{   my ($self, $args, $obs) = @_;
    my @failures;
    if (exists $args->{expect_error})
    {
        if (!defined $obs->{error})
        {
            push @failures, "expected error matching /$args->{expect_error}/ but step succeeded";
        }
        elsif ($obs->{error} !~ /$args->{expect_error}/)
        {
            push @failures, "error '$obs->{error}' does not match /$args->{expect_error}/";
        }
    }
    elsif (defined $obs->{error} && !$args->{allow_error})
    {
        push @failures, "unexpected error: $obs->{error}";
    }
    if (my $expect = $args->{expect})
    {
        push @failures, _compare($expect, $obs->{observed}, 'observed');
    }
    if (my $expect = $args->{expect_first})
    {
        my $rows = $obs->{observed} && $obs->{observed}->{rows};
        push @failures, _compare($expect, $rows && $rows->[0], 'observed.rows[0]');
    }
    if (my $expect = $args->{expect_messages})
    {
        push @failures, _compare($expect, $obs->{messages} || [], 'messages');
    }
    if (my $re = $args->{expect_messages_match})
    {
        push @failures, "no message matching /$re/"
            unless grep { $_ =~ /$re/ } @{$obs->{messages} || []};
    }
    @failures;
}

# Check that $exp is a subset of $got
sub _compare
{   my ($exp, $got, $path) = @_;
    if (ref $exp eq 'HASH')
    {
        return ("$path: expected hash, got ".(defined $got ? _show($got) : 'undef'))
            if ref $got ne 'HASH';
        my @f;
        push @f, _compare($exp->{$_}, $got->{$_}, "$path.$_") foreach sort keys %$exp;
        return @f;
    }
    elsif (ref $exp eq 'ARRAY')
    {
        return ("$path: expected list "._show($exp).", got "._show($got))
            if ref $got ne 'ARRAY';
        return ("$path: expected "._show($exp).", got "._show($got))
            if @$exp != @$got;
        my @f;
        push @f, _compare($exp->[$_], $got->[$_], "$path\[$_\]") foreach 0..$#$exp;
        return @f;
    }
    else {
        my $e = defined $exp ? "$exp" : undef;
        return () if !defined $e && !defined $got;
        if (defined $e && $e eq '@any')
        {
            return defined $got ? () : ("$path: expected a value, got undef");
        }
        if (defined $e && $e =~ /^~(.*)$/s)
        {
            my $re = $1;
            return ("$path: expected match /$re/, got "._show($got))
                if !defined $got || ref $got || $got !~ /$re/;
            return ();
        }
        return ("$path: expected "._show($e).", got "._show($got))
            if !defined $e || !defined $got || ref $got || $e ne $got;
        return ();
    }
}

sub _show
{   my $v = shift;
    return 'undef' if !defined $v;
    return JSON->new->canonical->allow_nonref->encode($v);
}

# ----------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------

sub _set_time
{   my ($self, $time) = @_;
    $time =~ /^(\d{4})-(\d\d)-(\d\d)[ T](\d\d):(\d\d):(\d\d)/
        or die "Invalid time $time";
    Test::MockTime::set_fixed_time("$1-$2-$3T$4:$5:$6Z");
}

sub schema { $_[0]->{schema} }

sub _sheet
{   my ($self, $alias) = @_;
    $alias ||= $self->{default_sheet};
    my $sheet = $self->{sheets}->{$alias}
        or error "Unknown sheet $alias";
    $sheet;
}

sub _first_sheet
{   my $self = shift;
    $self->{sheets}->{$self->{first_sheet}};
}

sub _user
{   my ($self, $ref) = @_;
    return $self->_first_sheet->user if !defined $ref;
    return undef if $ref eq 'none';
    # The user of a sheet (as $sheet->user)
    return $self->_sheet($1)->user if $ref =~ /^\@sheet:(\w+)$/;
    if ($ref =~ /^[0-9]+$/)
    {
        return $self->schema->resultset('User')->find($ref)
            || error "User $ref not found";
    }
    my $users = $self->_first_sheet->_users;
    return $users->{$ref} if $users->{$ref};
    return $self->{users}->{$ref} if $self->{users}->{$ref};
    error "Unknown user $ref";
}

# Layout for a sheet, optionally for a particular user
sub _layout
{   my ($self, $sheet, $user_ref) = @_;
    my $s = $self->_sheet($sheet);
    return $s->layout if !defined $user_ref;
    my $user = $self->_user($user_ref);
    GADS::Layout->new(
        user        => $user,
        schema      => $self->schema,
        config      => GADS::Config->instance,
        instance_id => $s->layout->instance_id,
    );
}

sub _sheet_alias_for_instance
{   my ($self, $instance_id) = @_;
    foreach my $alias (keys %{$self->{sheets}})
    {
        return $alias if $self->{sheets}->{$alias}->layout->instance_id == $instance_id;
    }
    return;
}

# Resolve a column reference to a column object within a layout
sub _col
{   my ($self, $layout, $ref, %options) = @_;
    defined $ref or error "Missing column reference";
    if ($ref =~ /^\@col:(\w+)$/)
    {
        my $id = $self->{cols}->{$1} or error "Unknown column alias $1";
        return $layout->column($id) || error "Column $1 not in layout";
    }
    if ($ref =~ /^([^:]+):(.+)$/)
    {
        my ($alias, $name) = ($1, $2);
        my $other = $self->_sheet($alias)->layout;
        # Use the requested layout if it covers the same instance, so that
        # user-specific permissions are honoured
        $other = $layout->clone(instance_id => $other->instance_id)
            if $layout && $other->instance_id != $layout->instance_id;
        $other = $layout if $layout && $layout->instance_id == $other->instance_id;
        return $self->_col($other, $name, %options);
    }
    return $layout->column($ref) if $ref =~ /^[0-9]+$/;
    if ($ref =~ /^_/)
    {
        return $layout->column_by_name_short($ref)
            || error "Unknown internal column $ref";
    }
    my $col = $layout->column_by_name($ref) || $layout->column_by_name_short($ref);
    return $col if $col;
    error "Unknown column $ref" if $options{no_split};
    error "Unknown column $ref";
}

# Resolve a filter id reference which may be parent_child
sub _filter_id
{   my ($self, $layout, $ref) = @_;
    return $ref if $ref =~ /^[0-9]+(_[0-9]+)?$/;
    # Try whole name first
    my $col = eval { $self->_col($layout, $ref) };
    return $col->id if $col;
    # Split on underscore: curval1_string1
    if ($ref =~ /^(.+?)_(.+)$/)
    {
        my ($p, $c) = ($1, $2);
        # Try progressively
        my @parts = split /_/, $ref;
        for my $i (1..$#parts)
        {
            my $pn = join '_', @parts[0..$i-1];
            my $cn = join '_', @parts[$i..$#parts];
            my $parent = eval { $self->_col($layout, $pn) } or next;
            $parent->is_curcommon or next;
            my $child = eval { $self->_col($parent->layout_parent, $cn) } or next;
            return $parent->id.'_'.$child->id;
        }
    }
    error "Unable to resolve filter column $ref";
}

sub _convert_filter
{   my ($self, $layout, $filter) = @_;
    return undef if !defined $filter;
    return $filter if !ref $filter;
    my %new = %$filter;
    if ($filter->{rules})
    {
        $new{rules} = [ map $self->_convert_filter($layout, $_), @{$filter->{rules}} ];
    }
    else {
        if (my $name = delete $new{name})
        {
            $new{id} = $self->_filter_id($layout, $name);
        }
        elsif (defined $new{id})
        {
            $new{id} = $self->_filter_id($layout, $new{id});
        }
        $new{field} = $new{id} if exists $new{id} && !exists $new{field};
        $new{type} ||= 'string' if exists $new{id};
        if (defined $new{value} && !ref $new{value} && $new{value} =~ /^\@record:(\w+)$/)
        {
            $new{value} = $self->{records}->{$1} // error "Unknown record alias $1";
        }
    }
    \%new;
}

# Convert a value for set_value, translating textual enum/tree values,
# record aliases etc
sub _convert_value
{   my ($self, $col, $value) = @_;
    return $value if !defined $value;
    if (ref $value eq 'ARRAY')
    {
        # Dateranges are passed as pairs; preserve structure
        return [ map $self->_convert_value_single($col, $_), @$value ];
    }
    if (ref $value eq 'HASH' && $col->type eq 'daterange')
    {
        return [$value->{from}, $value->{to}];
    }
    return $self->_convert_value_single($col, $value);
}

sub _convert_value_single
{   my ($self, $col, $v) = @_;
    return $v if !defined $v || ref $v;
    if ($v =~ /^\@record:(\w+)$/)
    {
        return $self->{records}->{$1} // error "Unknown record alias $1";
    }
    # Query values of curvals: @field:name is the field name of a column in
    # the referred sheet
    $v =~ s/\@field:(\w+)/$self->_col($col->layout_parent, $1)->field/ge
        if $col->is_curcommon;
    if (($col->type eq 'enum' || $col->type eq 'tree') && $v ne '' && $v !~ /^[0-9]+$/)
    {
        my ($e) = grep { $_->{value} eq $v } @{$col->enumvals};
        error "Unknown enum value $v for ".$col->name if !$e;
        return $e->{id};
    }
    $v;
}

# ----------------------------------------------------------------------
# Observation helpers
# ----------------------------------------------------------------------

sub _record_columns
{   my ($self, $record, $layout, $observe) = @_;
    if ($observe)
    {
        return map { [$_, $self->_col($layout, $_)] } @$observe;
    }
    my @cols = $layout->all;
    return map { [$_->name, $_] } @cols;
}

sub _observe_record
{   my ($self, $record, $layout, %options) = @_;
    my %values;
    foreach my $c ($self->_record_columns($record, $layout, $options{observe}))
    {
        my ($name, $col) = @$c;
        # get_field_value of a field of current (ID, deleted by) that is not
        # loaded searches current by record and field, which it has neither
        # of: SQLite takes the names for strings and finds nothing, PostgreSQL
        # fails. Its datum is made from no values without that search
        my $datum = !$record->fields->{$col->id} && $col->table eq 'Current'
            && !($col->name_short && $col->name_short eq '_serial')
            ? ($record->fields->{$col->id} = $record->_create_datum($col, []))
            : $record->get_field_value($col);
        $values{$name} = defined $datum ? $datum->as_string : undef;
    }
    my $obs = {
        current_id => $record->current_id,
        values     => \%values,
    };
    $obs->{record_id} = $record->record_id if $options{with_record_id};
    $obs->{parent_id} = $record->parent_id if $options{with_parent};
    $obs;
}

# ----------------------------------------------------------------------
# Step handlers
# ----------------------------------------------------------------------

my @sheet_options = qw/instance_id multivalue column_count enumvals_count
    calc_code calc_return_type rag_code has_rag optional no_groups
    users_to_create multivalue_columns site_id curval_offset/;

sub step_sheet
{   my ($self, $args) = @_;
    my $alias = $args->{as} or error "Sheet alias (as) required";
    my %params;
    foreach my $opt (@sheet_options)
    {
        $params{$opt} = $args->{$opt} if exists $args->{$opt};
    }
    $params{multivalue_columns} = { map { $_ => 1 } @{$params{multivalue_columns}} }
        if ref $params{multivalue_columns} eq 'ARRAY';
    $params{schema} = $self->schema || $self->_pg_schema($params{site_id});
    delete $params{schema} if !$params{schema};
    if (my $curval = $args->{curval})
    {
        my $s = $self->{sheets}->{$curval};
        $params{curval} = $s ? $s->layout->instance_id : $curval;
        if (my $cf = $args->{curval_fields})
        {
            my $cs = $s or error "curval_fields requires sheet alias for curval";
            $params{curval_field_ids} = [ map { $self->_col($cs->layout, $_)->id } @$cf ];
        }
    }
    # Share users and groups with the first sheet
    if (my $first = $self->{first_sheet} && $self->_first_sheet)
    {
        $params{group} = $first->group if !$args->{own_group};
        # Use the same users as the first sheet
        $params{_users} = $first->_users if $args->{share_users};
    }
    if (exists $args->{data})
    {
        $params{data} = $args->{data} if ref $args->{data} eq 'ARRAY';
    }
    my $sheet = Test::GADS::DataSheet->new(%params);
    $self->{schema} ||= $sheet->schema;
    $self->{sheets}->{$alias} = $sheet;
    $self->{first_sheet}   ||= $alias;
    $self->{default_sheet} ||= $alias;
    # Force build of columns (unless build_columns is false, as a DataSheet
    # whose columns are never used)
    my $columns = !exists $args->{build_columns} || $args->{build_columns} ? $sheet->columns : {};
    if (!exists $args->{create_records} || $args->{create_records})
    {
        $sheet->create_records;
    }
    my %cols = map { $_ => $columns->{$_}->id } keys %$columns;
    my @internal = map { $_->name_short => $_->id } $sheet->layout->all(only_internal => 1, include_hidden => 1);
    return {
        instance_id => $sheet->layout->instance_id,
        columns     => \%cols,
        internal    => {@internal},
        records     => [ $self->schema->resultset('Current')->search({
            instance_id => $sheet->layout->instance_id,
        }, { order_by => 'id' })->get_column('id')->all ],
    };
}

# With GADS_TEST_PG (libpq parameters of the server, such as
# "host=/var/run/postgresql"), the schema of the tests is a new PostgreSQL
# database instead of an in-memory SQLite one, built as Test::GADS::DataSheet
# builds its schema. Each is dropped when the next is made, and at the end.
my (@pg_databases, @pg_schemas);
my $pg_count = 0;

sub _pg_dsn
{   my $dbname = shift;
    my $params = join ';', grep length, split /\s+/, $ENV{GADS_TEST_PG};
    "dbi:Pg:$params;dbname=$dbname";
}

sub _pg_admin
{   require DBI;
    DBI->connect(_pg_dsn('postgres'), undef, undef, { RaiseError => 1, PrintError => 0, AutoCommit => 1 });
}

sub _pg_drop_all
{   $_->storage->disconnect foreach @pg_schemas;
    @pg_schemas = ();
    return if !@pg_databases;
    my $dbh = _pg_admin();
    $dbh->do(qq(DROP DATABASE IF EXISTS "$_")) foreach @pg_databases;
    @pg_databases = ();
    $dbh->disconnect;
}

sub _pg_schema
{   my ($self, $site_id) = @_;
    $ENV{GADS_TEST_PG} or return;
    _pg_drop_all();
    my $name = sprintf "gads_conf_perl_%d_%d", $$, ++$pg_count;
    my $dbh = _pg_admin();
    $dbh->do(qq(CREATE DATABASE "$name"));
    $dbh->disconnect;
    push @pg_databases, $name;
    my $schema = GADS::Schema->connect({
        dsn         => _pg_dsn($name),
        quote_names => 1,
    });
    push @pg_schemas, $schema;
    {
        # The notices of the creation of tables and indexes
        local $SIG{__WARN__} = sub { warn @_ unless $_[0] =~ /NOTICE/ };
        $schema->storage->dbh->do('SET client_min_messages = WARNING');
        $schema->deploy;
    }
    if ($site_id)
    {
        $schema->resultset('Site')->create({ id => $site_id });
        $schema->site_id($site_id);
    }
    $schema;
}

sub pg_cleanup { eval { _pg_drop_all() }; warn "Failed to drop databases: $@" if $@ }

END { pg_cleanup() if @pg_databases }

# Start again with a fresh database, as tests that create new DataSheets
# without passing a schema do
sub step_reset
{   my ($self, $args) = @_;
    delete $self->{$_} foreach qw/schema sheets first_sheet default_sheet views
        records users groups held held_cols cols topics graphs held_views saved_groups held_filters tables/;
    return {};
}

# Create a graph (or use an existing one) and observe its data, as
# GADS::Graph and GADS::Graph::Data
sub step_graph
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my $graph_id;
    my $obs = {};
    if (my $ref = $args->{graph_ref})
    {
        $graph_id = $self->{graphs}->{$ref} // error "Unknown graph $ref";
    }
    else {
        my $graph = GADS::Graph->new(
            layout       => $layout,
            schema       => $self->schema,
            current_user => $user,
        );
        $graph->title($args->{title});
        $graph->type($args->{type});
        $graph->x_axis(defined $args->{x_axis} ? $self->_col($layout, $args->{x_axis})->id : undef);
        $graph->x_axis_link($self->_col($layout, $args->{x_axis_link})->id)
            if $args->{x_axis_link};
        $graph->x_axis_grouping($args->{x_axis_grouping})
            if $args->{x_axis_grouping};
        $graph->x_axis_range($args->{x_axis_range})
            if $args->{x_axis_range};
        $graph->from($args->{from} ? $self->_parse_dt($args->{from}) : undef);
        $graph->to($args->{to} ? $self->_parse_dt($args->{to}) : undef);
        $graph->y_axis(defined $args->{y_axis} ? $self->_col($layout, $args->{y_axis})->id : undef);
        # y_axis_link: the curval of a Y-axis field within a curval
        $graph->y_axis_link($self->_col($layout, $args->{y_axis_link})->id)
            if defined $args->{y_axis_link};
        $graph->y_axis_stack($args->{y_axis_stack});
        $graph->group_by($self->_col($layout, $args->{group_by})->id)
            if $args->{group_by};
        $graph->as_percent($args->{as_percent});
        $graph->trend($args->{trend}) if $args->{trend};
        $graph->is_shared($args->{is_shared}) if exists $args->{is_shared};
        $graph->group_id($args->{group} eq 'default' ? $self->_first_sheet->group->id : $self->_group_id($args->{group}))
            if $args->{group};
        $graph->write;
        $graph_id = $graph->id;
        $self->{graphs}->{$args->{as}} = $graph_id if $args->{as};
    }
    return $obs if $args->{no_data};
    my $view = ($args->{filter} || $args->{view} || $args->{view_ref} || $args->{columns})
        ? $self->_build_view($args, $layout) : undef;
    my $records = GADS::RecordsGraph->new(
        user   => $user,
        layout => $layout,
        schema => $self->schema,
        $args->{rewind} ? (rewind => $self->_parse_dt($args->{rewind})) : (),
        defined $args->{search} ? (search => $args->{search}) : (),
    );
    my $data = GADS::Graph::Data->new(
        id      => $graph_id,
        view    => $view,
        records => $records,
        schema  => $self->schema,
    );
    $obs->{points}  = $data->points;
    $obs->{xlabels} = $data->xlabels;
    # Colours are allocated randomly in Perl except for RAG values
    $obs->{labels}  = [ map $_->{label}, @{$data->labels} ];
    $obs->{options} = $data->options;
    $obs;
}

# A globe (GADS::Globe) of a sheet's records. group, color and label are a
# column reference, a literal number (e.g. -1 for count, or an invalid ID), or
# [parent, child] for a curval sub-field. Countries are a hash in Perl, so
# items are sorted by location and the <br>-separated parts of hover texts
# (after the country) and labels are sorted
sub step_globe
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my $view = ($args->{view_ref} || $args->{view} || $args->{columns})
        ? $self->_build_view($args, $layout) : undef;
    my %options;
    foreach my $opt (qw/group color label/)
    {
        my $v = $args->{$opt};
        next if !defined $v;
        $options{"${opt}_col_id"} = ref $v eq 'ARRAY'
            ? join('_', map $self->_col($layout, $_)->id, @$v)
            : $v =~ /^-?[0-9]+$/ ? $v : $self->_col($layout, $v)->id;
    }
    my $globe = GADS::Globe->new(
        %options,
        records_options => {
            user   => $user,
            layout => $layout,
            schema => $self->schema,
            $view ? (view => $view) : (),
        },
    );
    my $data = $globe->data;
    my $canon = sub {
        my ($s, $keep_first) = @_;
        return undef if !defined $s;
        my @parts = split /<br>/, $s, -1;
        my @first = $keep_first && @parts ? (shift @parts) : ();
        join '<br>', @first, sort @parts;
    };
    my @traces;
    foreach my $trace (@{$data->{data}})
    {
        my @items;
        foreach my $i (0..$#{$trace->{locations}})
        {
            my %item = (location => $trace->{locations}->[$i]);
            $item{text} = $canon->($trace->{text}->[$i], !$trace->{hovertext})
                if $trace->{text};
            $item{hovertext} = $canon->($trace->{hovertext}->[$i], 1)
                if $trace->{hovertext};
            $item{z} = $trace->{z}->[$i] if $trace->{z};
            if ($trace->{countrycolors})
            {
                my $c = $trace->{countrycolors}->[$i];
                $item{countrycolor} = defined $c && $c =~ /^#/ && $c ne '#D3D3D3' ? 'color' : $c;
            }
            push @items, \%item;
        }
        push @traces, {
            type          => $trace->{type},
            hoverinfo     => $trace->{hoverinfo},
            locationmode  => $trace->{locationmode},
            mode          => $trace->{mode},
            showscale     => exists $trace->{showscale} ? (${$trace->{showscale}} ? 1 : 0) : undef,
            marker        => $trace->{marker},
            countrycolors => exists $trace->{countrycolors} ? (defined $trace->{countrycolors} ? 1 : 0) : undef,
            items         => [ sort { $a->{location} cmp $b->{location} } @items ],
        };
    }
    return {
        traces => \@traces,
        params => {
            has_view                    => defined $data->{params}->{view_id} ? 1 : 0,
            layout_identifier           => $data->{params}->{layout_identifier},
            globe_fields                => $data->{params}->{globe_fields},
            default_view_limit_extra_id => $data->{params}->{default_view_limit_extra_id},
        },
        colors => scalar @{$globe->colors},
    };
}

# Import CSV content into a sheet (GADS::Import, without forking). options
# are Import's options, with update_unique/skip_existing_unique a column
# reference (ID for the ID column) and append a list of columns. Observes the
# import status and its rows
sub step_import
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = $self->_user($args->{user});
    my %options = %{$args->{options} || {}};
    foreach my $opt (qw/update_unique skip_existing_unique/)
    {
        next if !$options{$opt};
        $options{$opt} = $options{$opt} eq 'ID' ? $layout->column_id->id : $self->_col($layout, $options{$opt})->id;
    }
    $options{append} = [ map $self->_col($layout, $_)->id, @{$options{append}} ]
        if $options{append};
    my $content = $args->{content};
    # {user:<name>} is replaced by that user's ID
    $content =~ s/\{user:(\w+)\}/$self->_user($1)->id/ge;
    my $import = GADS::Import->new(
        schema => $self->schema,
        layout => $layout,
        user   => $user,
        file   => \$content,
        %options,
    );
    my $status = $import->process;
    $status->discard_changes;
    return {
        row_count     => $status->row_count,
        written_count => $status->written_count,
        error_count   => $status->error_count,
        skipped_count => $status->skipped_count,
        result        => $status->result,
        completed     => $status->completed ? 1 : 0,
        rows          => [ map +{
            status  => $_->status,
            content => $_->content,
            errors  => $_->errors,
            changes => $_->changes,
        }, $self->schema->resultset('ImportRow')->search({ import_id => $status->id }, { order_by => 'id' })->all ],
    };
}

# Clear the cached layouts of all sheets, as tests do with $layout->clear
sub step_clear_layouts
{   my ($self, $args) = @_;
    $_->layout->clear foreach values %{$self->{sheets}};
    return {};
}

# The graphs that a user can see (GADS::Graphs->all)
sub step_graphs
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my $all = GADS::Graphs->new(
        current_user => $user,
        schema       => $self->schema,
        layout       => $layout,
    )->all;
    return {
        count => scalar @$all,
        ids   => [ sort { $a <=> $b } map $_->id, @$all ],
    };
}

# Wait in real time (for values such as Lua's os.time)
# Render a Template Toolkit template with the same engine options that
# Dancer2::Template::TemplateToolkit uses for GADS
sub step_template
{   my ($self, $args) = @_;
    my $dir = File::Temp->newdir;
    my %files = (%{$args->{files} || {}}, '_main.tt' => $args->{template} // '');
    foreach my $name (keys %files)
    {
        my $path = "$dir/$name";
        (my $d = $path) =~ s!/[^/]+$!!;
        File::Path::make_path($d);
        open my $fh, '>:encoding(UTF-8)', $path or die $!;
        print $fh $files{$name};
        close $fh;
    }
    my $tt = Template->new(
        ANYCASE      => 1,
        ABSOLUTE     => 1,
        ENCODING     => 'UTF-8',
        RECURSION    => 1,
        INCLUDE_PATH => ["$dir"],
    );
    my $output = '';
    $tt->process('_main.tt', $args->{vars} || {}, \$output)
        or error $tt->error->as_string =~ s/\Q$dir\E\/?//gr;
    return { output => $output };
}

sub _web
{   my $self = shift;
    $self->{web} ||= do {
        my $web = GADS::Conformance::Web->new(
            schema => $self->schema,
            gads   => $self->{gads_dir},
            config => $self->{scenario}->{config},
        );
        $web->ensure_site;
        $web;
    };
}

# Make an HTTP request to the web application
sub step_http
{   my ($self, $args) = @_;
    my $web = $self->_web;
    my %req = %$args;
    $req{query} = $self->_web_values($args->{query}) if $args->{query};
    $req{form} = $self->_web_values($args->{form}) if $args->{form};
    $req{json} = $self->_web_values($args->{json}) if $args->{json};
    $req{headers} = $self->_web_values($args->{headers}) if $args->{headers};
    $req{path} = $self->_web_path($args->{path});
    $req{csrf} = exists $args->{csrf} ? $args->{csrf} : 1;
    delete $req{capture};
    my $obs = $web->request(%req);
    # Remember values of the JSON response for later steps ({var:<name>})
    foreach my $name (sort keys %{$args->{capture} || {}})
    {
        my $key = $args->{capture}->{$name};
        my $v = ref $obs->{json} eq 'HASH' ? $obs->{json}->{$key} : undef;
        # A missing value leaves the variable unset (using it later fails)
        $self->{web_vars}->{$name} = $v if defined $v && $v ne '';
    }
    return $self->_web_unvar($obs);
}

# Replace the values of captured variables (e.g. random OAuth tokens) in an
# observation by their references
sub _web_unvar
{   my ($self, $v) = @_;
    my $vars = $self->{web_vars} || {};
    if (ref $v eq 'HASH')
    {
        return { map { $_ => $self->_web_unvar($v->{$_}) } keys %$v };
    }
    elsif (ref $v eq 'ARRAY')
    {
        return [ map $self->_web_unvar($_), @$v ];
    }
    return $v if !defined $v || ref $v;
    foreach my $name (sort { length $vars->{$b} <=> length $vars->{$a} || $a cmp $b } keys %$vars)
    {
        my $value = $vars->{$name};
        $v =~ s/\Q$value\E/{var:$name}/g;
    }
    $v;
}

# Log in to the web application as a user (sets the user's password)
sub step_login
{   my ($self, $args) = @_;
    my $web  = $self->_web;
    my $user = $self->_user($args->{user});
    $web->set_password($user);
    my $as = $args->{as} || $args->{user};
    $web->request(as => $as, method => 'GET', path => '/login');
    return $web->request(
        as     => $as,
        method => 'POST',
        path   => $self->_web_path($args->{path} || '/login'),
        csrf   => 1,
        form   => {
            username => $user->username,
            password => $args->{password} // $GADS::Conformance::Web::PASSWORD,
            signin   => 1,
        },
        no_body => 1,
    );
}

# Substitute references in a path: {record:alias} {view:alias} {sheet:alias}
# {user:name} {column:name}
sub _web_path
{   my ($self, $path) = @_;
    $path =~ s/\{(\w+):([^}]+)\}/$self->_web_ref($1, $2)/ge;
    $path;
}

sub _web_ref
{   my ($self, $type, $ref) = @_;
    return $self->_user($ref)->id if $type eq 'user';
    return $self->{records}->{$ref} if $type eq 'record';
    return $self->{views}->{$ref} if $type eq 'view';
    if ($type eq 'var')
    {
        exists $self->{web_vars}->{$ref} or die "Unknown variable $ref";
        return $self->{web_vars}->{$ref};
    }
    if ($type eq 'sheet')
    {
        return $self->_sheet($ref)->layout->identifier;
    }
    if ($type eq 'instance')
    {
        return $self->_sheet($ref)->layout->instance_id;
    }
    if ($type eq 'totp')
    {
        # The current OTP token of an MFA secret
        require Authen::OATH;
        require Convert::Base32;
        return Authen::OATH->new->totp(Convert::Base32::decode_base32($ref));
    }
    if ($type eq 'column')
    {
        # <sheet alias>:<name> is a column of that sheet
        return $self->_col($self->_sheet($1)->layout, $2)->id
            if $ref =~ /^([^:]+):(.+)$/;
        return $self->_col($self->_sheet->layout, $ref)->id;
    }
    die "Unknown reference type $type";
}

sub _web_values
{   my ($self, $v) = @_;
    if (ref $v eq 'HASH')
    {
        # Keys may also hold references (e.g. column IDs as parameter names)
        return { map { $self->_web_values($_) => $self->_web_values($v->{$_}) } keys %$v };
    }
    elsif (ref $v eq 'ARRAY')
    {
        return [ map $self->_web_values($_), @$v ];
    }
    return $v if !defined $v;
    $v =~ s/\{(\w+):([^}]+)\}/$self->_web_ref($1, $2)/ge;
    $v;
}

# Run a script of the bin directory of GADS in-process, against the
# scenario's database (through the Dancer2 DBIC plugin of the web harness).
# Files given are written to a temporary directory, referred to in arguments
# as {file:<name>}; that directory is {tmp} in the observed output.
sub step_cli
{   my ($self, $args) = @_;
    my $script = $args->{script} or error "script required";
    $self->_web; # The Dancer2 configuration and schema of the scripts
    my $path = "$self->{gads_dir}/bin/$script";
    -f $path or error "Unknown script $script";
    require File::Temp;
    # A named directory is kept for the later scripts of the scenario
    my $tmp = $args->{dir}
        ? ($self->{cli_dirs}->{$args->{dir}} ||= File::Temp::tempdir(CLEANUP => 1))
        : File::Temp::tempdir(CLEANUP => 1);
    foreach my $name (sort keys %{$args->{files} || {}})
    {
        require File::Basename;
        require File::Path;
        File::Path::make_path(File::Basename::dirname("$tmp/$name"));
        open my $fh, '>:raw', "$tmp/$name" or error "$tmp/$name: $!";
        print $fh Encode::encode_utf8($args->{files}->{$name});
        close $fh;
    }
    my @argv = map {
        my $v = $_;
        $v =~ s/\{file:([^}]+)\}/$tmp\/$1/g;
        $self->_web_values($v)
    } @{$args->{args} || []};
    my $stdin = $args->{stdin} // '';
    my ($out, $err) = ('', '');
    my ($exit, $died);
    # The site of the schema, which scripts set
    my $site_id = $self->schema->site_id;
    # Scripts write their files (such as exports) in the current directory
    require Cwd;
    my $cwd = Cwd::getcwd();
    chdir $tmp or error "chdir $tmp: $!";
    {
        local @ARGV = @argv;
        local $0 = "bin/$script";
        # Hashes that scripts dump are written in the order of their keys
        require Data::Dumper;
        local $Data::Dumper::Sortkeys = 1;
        local *STDIN;
        open STDIN, '<', \$stdin or die $!;
        local *STDOUT;
        open STDOUT, '>', \$out or die $!;
        local *STDERR;
        open STDERR, '>', \$err or die $!;
        no warnings 'redefine';
        local *CORE::GLOBAL::exit = sub {
            die bless { code => $_[0] // 0 }, 'GADS::Conformance::Exit';
        };
        # The reports of the script are written as the default dispatcher
        # of Log::Report does, when they are made: the reason and message,
        # with the error of a fault
        Log::Report::dispatcher(CALLBACK => 'conformance_cli', mode => 'NORMAL', callback => sub {
            my ($disp, $opts, $reason, $message) = @_;
            my $text = $message->toString;
            if ($reason eq 'FAULT' && $opts->{errno})
            {
                local $! = $opts->{errno};
                $text .= "; $!";
            }
            print STDERR lc($reason).": $text\n";
        });
        # A try of its own, as the try of the scenario hides all reports
        Log::Report::try(sub {
            package main;
            my $r = do $path;
            die $@ if $@;
        }, hide => 'NONE');
        my $e = $@->died;
        Log::Report::dispatcher(close => 'conformance_cli');
        if (defined $e)
        {
            if (ref $e eq 'GADS::Conformance::Exit')
            {
                $exit = $e->{code};
            }
            elsif (blessed $e && $e->isa('Log::Report::Exception'))
            {
                # A fatal report is written, and ends the script with the
                # error of a fault, else 1
                my $errno = $e->reason eq 'FAULT' && $e->report_opts->{errno};
                my $text = $e->message->toString;
                if ($errno)
                {
                    local $! = $errno;
                    $text .= "; $!";
                }
                print STDERR lc($e->reason).": $text\n";
                $exit = $errno || 1;
            }
            else {
                $died = ref $e && $e->can('message') ? $e->message->toString : "$e";
                $exit = 255;
            }
        }
    }
    chdir $cwd;
    $self->schema->site_id($site_id);
    $exit //= 0;
    foreach my $v ($out, $err, $died)
    {
        next if !defined $v;
        $v = Encode::decode_utf8($v) if !utf8::is_utf8($v);
        $v =~ s/\Q$tmp\E/{tmp}/g;
        $v =~ s/ at \S+ line \d+(?:, <\S+> (?:line|chunk) \d+)?\.?\n?//g;
        # Code compiled in a string eval (whose number depends on the
        # evals before it in the process)
        $v =~ s/ at \(eval \d+\) line \d+\.?\n?//g;
        # Redefinitions from loading a script a second time in one process
        $v =~ s/Subroutine \S+ redefined//g;
        # Addresses of references, and the stack traces of croaks
        $v =~ s/\((0x[0-9a-f]+)\)/(0x0)/g;
        $v =~ s/\t\S.* called.*//s;
    }
    my $obs = { exit => $exit, stdout => $out, stderr => $err };
    $obs->{died} = $died if defined $died;
    # The files written by the script: their contents, or (for random
    # contents) their number of lines and first line
    if (my $mode = $args->{observe_files})
    {
        require File::Find;
        my %given = map { $_ => 1 } keys %{$args->{files} || {}};
        my %files;
        File::Find::find({ no_chdir => 1, wanted => sub {
            return if -d $_;
            (my $rel = $_) =~ s/^\Q$tmp\E\///;
            return if $given{$rel};
            open my $fh, '<:raw', $_ or error "$_: $!";
            my $content = do { local $/; <$fh> };
            $content = Encode::decode_utf8($content);
            if ($mode eq 'lines')
            {
                my @lines = split /\n/, $content;
                $files{$rel} = { lines => scalar @lines, first => $lines[0] };
            }
            else {
                $files{$rel} = $content;
            }
        }}, $tmp);
        $obs->{files} = \%files;
    }
    $obs;
}

sub step_sleep
{   my ($self, $args) = @_;
    sleep $args->{seconds};
    return {};
}

sub step_set_time
{   my ($self, $args) = @_;
    $self->_set_time($args->{time});
    return { time => $args->{time} };
}

sub step_create_records
{   my ($self, $args) = @_;
    my $sheet = $self->_sheet($args->{sheet});
    if ($args->{data})
    {
        $sheet->{data} = $args->{data};
    }
    $sheet->create_records;
    return {};
}

sub step_add_autocur
{   my ($self, $args) = @_;
    my $sheet = $self->_sheet($args->{sheet});
    my $refers = $self->_sheet($args->{refers_to});
    my $related = $self->_col($refers->layout, $args->{related_field});
    my %options = (
        refers_to_instance_id => $refers->layout->instance_id,
        related_field_id      => $related->id,
    );
    $options{curval_field_ids} = [ map $self->_col($refers->layout, $_)->id, @{$args->{curval_fields}} ]
        if $args->{curval_fields};
    my $autocur = $sheet->add_autocur(%options);
    $_->layout->clear foreach values %{$self->{sheets}};
    return { id => $autocur->id, name => $autocur->name };
}

my %column_class = (
    string    => 'String',
    intgr     => 'Intgr',
    enum      => 'Enum',
    tree      => 'Tree',
    date      => 'Date',
    daterange => 'Daterange',
    person    => 'Person',
    file      => 'File',
    calc      => 'Calc',
    rag       => 'Rag',
    curval    => 'Curval',
    autocur   => 'Autocur',
    filval    => 'Filval',
);

sub _apply_column_attrs
{   my ($self, $col, $layout, $args) = @_;
    foreach my $attr (qw/name name_short optional isunique remember multivalue
        description helptext width aggregate group_display textbox force_regex
        ordering end_node_only code return_type decimal_places
        value_selector show_add delete_not_used override_permissions limit_rows
        show_in_edit default_today show_datepicker show_calculator
        lookup_endpoint lookup_group notes default_to_login notify_on_selection
        notify_on_selection_subject notify_on_selection_message filesize max_length/)
    {
        next unless exists $args->{$attr};
        $col->$attr($args->{$attr});
    }
    $col->set_can_child($args->{can_child}) if exists $args->{can_child};
    if (exists $args->{enumvals})
    {
        $col->enumvals([ map { ref $_ ? $_ : { value => $_ } } @{$args->{enumvals}} ]);
    }
    if (my $refers = $args->{refers_to})
    {
        my $s = $self->_sheet($refers);
        $col->refers_to_instance_id($s->layout->instance_id);
        my @ids = $args->{curval_fields}
            ? map { $self->_col($s->layout, $_)->id } @{$args->{curval_fields}}
            : map { $_->id } grep { $_->type ne 'autocur' } $s->layout->all(exclude_internal => 1);
        $col->curval_field_ids(\@ids);
    }
    elsif ($args->{curval_fields})
    {
        my $parent = $col->layout_parent;
        $col->curval_field_ids([ map { $self->_col($parent, $_)->id } @{$args->{curval_fields}} ]);
    }
    if (my $rf = $args->{related_field})
    {
        $col->related_field_id($self->_col($layout, $rf)->id);
    }
    if (exists $args->{filter})
    {
        my $parent_layout = $col->type eq 'curval' ? $col->layout_parent : $layout;
        my $filter = $self->_convert_filter($parent_layout, $args->{filter});
        $col->filter(GADS::Filter->new(as_hash => $filter || {}));
    }
    if (exists $args->{display_fields})
    {
        my $df = $self->_convert_filter($layout, $args->{display_fields});
        $col->display_fields(GADS::Filter->new(as_hash => $df || {}, layout => $layout));
    }
    if (exists $args->{link_parent})
    {
        $col->link_parent_id($args->{link_parent} ? $self->_col($layout, $args->{link_parent})->id : undef);
    }
    if (exists $args->{topic})
    {
        $col->topic_id($args->{topic} ? $self->{topics}->{$args->{topic}} : undef);
    }
    if (exists $args->{permissions})
    {
        my $perms = $args->{permissions};
        my $sheet = $args->{permissions_sheet} ? $self->_sheet($args->{permissions_sheet}) : $self->_first_sheet;
        if (!ref $perms && $perms eq 'default')
        {
            $col->set_permissions({ $sheet->group->id => $sheet->default_permissions });
        }
        elsif (!ref $perms && $perms eq 'none')
        {
            $col->set_permissions({});
        }
        else {
            my %p;
            foreach my $g (keys %$perms)
            {
                my $gid = $g eq 'default' ? $sheet->group->id : $self->_group_id($g);
                $p{$gid} = $perms->{$g};
            }
            $col->set_permissions(\%p);
        }
    }
}

sub _group_id
{   my ($self, $ref) = @_;
    return $ref if $ref =~ /^[0-9]+$/;
    return $self->_first_sheet->group->id if $ref eq 'default';
    return $self->{groups}->{$ref} if $self->{groups}->{$ref};
    my $g = $self->schema->resultset('Group')->search({ name => $ref })->next
        or error "Unknown group $ref";
    $g->id;
}

sub step_column
{   my ($self, $args) = @_;
    my $op     = $args->{op} || 'create';
    my $sheet  = $self->_sheet($args->{sheet});
    my $layout = $sheet->layout;
    # A column of a table created with layout create_table
    my $table  = $args->{table} && ($self->{tables}->{$args->{table}} or error "Unknown table $args->{table}");
    $layout = $table if $table;
    if ($op eq 'create')
    {
        my $type  = $args->{type} or error "Column type required";
        my $class = "GADS::Column::".($column_class{$type} or error "Unknown type $type");
        my $col = $class->new(
            schema => $self->schema,
            user   => undef,
            layout => $layout,
        );
        $col->type($type);
        $args->{permissions} = 'default' if !exists $args->{permissions};
        $self->_apply_column_attrs($col, $layout, $args);
        $col->write(%{$args->{write_options} || {}}, force => 1);
        if ($type eq 'tree' && $args->{tree})
        {
            $col->update($self->_tree_input($args->{tree}));
        }
        $_->layout->clear foreach values %{$self->{sheets}};
        $table->clear if $table;
        $sheet->columns->{$col->name} = $layout->column($col->id) if !$table && $layout->column($col->id);
        $self->{cols}->{$args->{as}} = $col->id if $args->{as};
        return { id => $col->id, name => $col->name };
    }
    # held_col: a column object kept with op hold (which, as in tests that
    # reuse a column object, can be written again after deletion)
    my $col = $args->{held_col}
        ? ($self->{held_cols}->{$args->{held_col}} or error "Unknown held column $args->{held_col}")
        : $self->_col($layout, $args->{name_ref} || $args->{column} || $args->{name});
    if ($op eq 'hold')
    {
        $self->{held_cols}->{$args->{as}} = $col;
        return { id => $col->id };
    }
    elsif ($op eq 'clear')
    {
        $col->clear;
        return { id => $col->id };
    }
    elsif ($op eq 'update')
    {
        my %set = %{$args->{set} || {}};
        $self->_apply_column_attrs($col, $layout, \%set);
        $col->write(%{$args->{write_options} || {}}, force => 1);
        if ($col->type eq 'tree' && $set{tree})
        {
            $col->update($self->_tree_input($set{tree}));
        }
        $_->layout->clear foreach values %{$self->{sheets}};
        return { id => $col->id };
    }
    elsif ($op eq 'delete')
    {
        $col->delete;
        $_->layout->clear foreach values %{$self->{sheets}};
        $table->clear if $table;
        return { deleted => $col->id };
    }
    elsif ($op eq 'update_cached')
    {
        # Optionally from a layout for another user (e.g. none, as for
        # overnight updates)
        # (columns hold weak references to their layout, so keep it)
        my $user_layout = $args->{layout_user} && $self->_layout($args->{sheet}, $args->{user});
        $col = $self->_col($user_layout, $args->{column})
            if $user_layout;
        $col->update_cached;
        $_->layout->clear foreach values %{$self->{sheets}};
        return { id => $col->id };
    }
    elsif ($op eq 'get')
    {
        my %obs = (
            id          => $col->id,
            name        => $col->name,
            name_short  => $col->name_short,
            type        => $col->type,
            return_type => $col->return_type,
            multivalue  => $col->multivalue,
            optional    => $col->optional,
            isunique    => $col->isunique,
            internal    => $col->internal,
            userinput   => $col->userinput,
            numeric     => $col->numeric ? 1 : 0,
            value_field => $col->value_field,
            can_child   => $col->can_child ? 1 : 0,
        );
        if ($col->type eq 'enum' || $col->type eq 'tree')
        {
            $obs{enumvals} = [ map { { id => $_->{id}, value => $_->{value}, deleted => $_->{deleted} ? 1 : 0 } } @{$col->enumvals} ];
        }
        $obs{tree} = $col->json if $col->type eq 'tree';
        $obs{depends_on} = [ sort { $a <=> $b } @{$col->depends_on} ] if !$col->userinput;
        if ($args->{validate})
        {
            $obs{validate} = { map { $_ => ($col->validate($_) ? 1 : 0) } @{$args->{validate}} };
        }
        if ($args->{validate_search})
        {
            $obs{validate_search} = { map { $_ => ($col->validate_search($_) ? 1 : 0) } @{$args->{validate_search}} };
        }
        if (defined(my $vbw = $args->{values_beginning_with}))
        {
            $obs{values_beginning_with} = [ map { ref $_ ? { id => $_->{id}, label => $_->{label} } : $_ } $col->values_beginning_with($vbw) ];
        }
        $obs{subvals_input_required} = [ sort map $_->name, @{$col->subvals_input_required} ]
            if $args->{subvals_input_required};
        if ($args->{filtered_values} && $col->can('filtered_values'))
        {
            $obs{filtered_values} = [ map { { id => $_->{id}, value => $_->{value} } } @{$col->filtered_values} ];
        }
        if ($args->{all_values} && $col->can('all_values'))
        {
            $obs{all_values} = [ map { { id => $_->{id}, value => $_->{value} } } @{$col->all_values} ];
        }
        return \%obs;
    }
    error "Unknown column op $op";
}

# Convert a simple tree description into the jstree format accepted by
# GADS::Column::Tree::update
sub _tree_input
{   my ($self, $tree) = @_;
    my $n = 0;
    my $conv;
    $conv = sub {
        my $node = shift;
        if (!ref $node)
        {
            return { text => $node, children => [], data => {}, id => 'j1_'.++$n };
        }
        my ($text, $children) = ref $node eq 'HASH' ? %$node : @$node;
        if (ref $children ne 'ARRAY')
        {
            # {text => ..., id => ..., children => ...}
            return { %$node, data => {}, children => [ map $conv->($_), @{$node->{children} || []} ] }
                if $node->{text};
        }
        return { text => $text, data => {}, id => 'j1_'.++$n, children => [ map $conv->($_), @{$children || []} ] };
    };
    [ map $conv->($_), @$tree ];
}

sub step_layout
{   my ($self, $args) = @_;
    my $op     = $args->{op};
    my $sheet  = $self->_sheet($args->{sheet});
    my $layout = $sheet->layout;
    if ($op eq 'create_table')
    {
        # A new table (GADS::Layout->new(name => ...)->write), as the
        # sheet's user
        my $l = GADS::Layout->new(
            name   => $args->{name},
            user   => $sheet->user,
            schema => $self->schema,
            config => GADS::Config->instance,
        );
        $l->write;
        $self->{tables}->{$args->{as}} = $l;
        return { instance_id => $l->instance_id };
    }
    if ($op eq 'purge_table')
    {
        my $l = $self->{tables}->{$args->{table}} or error "Unknown table $args->{table}";
        $l->purge;
        return {};
    }
    if ($op eq 'delete_table')
    {
        my $l = $self->{tables}->{$args->{table}} or error "Unknown table $args->{table}";
        $l->delete;
        return {};
    }
    if ($op eq 'has_column')
    {
        # Whether a column of another sheet (column: alias:name) can be
        # retrieved from this layout by its id or name_short (by)
        my ($alias, $cname) = split /:/, $args->{column}, 2;
        my $src = $self->_col($self->_sheet($alias)->layout, $cname);
        my $col = ($args->{by} || 'id') eq 'name_short'
            ? $layout->column_by_name_short($src->name_short)
            : $layout->column($src->id);
        return { exists => $col ? 1 : 0 };
    }
    if ($op eq 'set_user')
    {
        # Change the user of the sheet's layout (used by everything sharing
        # it, such as held views)
        $layout->user($self->_user($args->{user}));
        $layout->clear;
        return {};
    }
    if ($op eq 'set_alert_columns')
    {
        $layout->set_alert_columns([ map $self->_col($layout, $_)->id, @{$args->{columns}} ]);
        $layout->write;
        $layout->clear;
        return {};
    }
    if ($op eq 'has_children')
    {
        return { has_children => $layout->has_children ? 1 : 0 };
    }
    if ($op eq 'write_internal')
    {
        # Try writing each internal column, observing the errors (by name,
        # as the internal columns share a position, which PostgreSQL orders
        # arbitrarily)
        return [ sort { $a->{name} cmp $b->{name} } map {
            my $col = $_;
            try { $col->write(force => 1) } hide => 'ALL';
            my $e = $@->wasFatal;
            +{ name => $col->name, error => $e ? _error_text($e) : undef };
        } $layout->all(only_internal => 1) ];
    }
    if ($op eq 'delete_all_columns')
    {
        # Delete every column in reverse dependency order, observing for
        # each whether it existed, any error and whether it has gone
        my @out;
        foreach my $col (reverse $layout->all(order_dependencies => 1))
        {
            my $existed = $self->schema->resultset('Layout')->find($col->id) ? 1 : 0;
            try { $col->delete } hide => 'ALL';
            my $e = $@->wasFatal;
            my $error = $e ? _error_text($e) : undef;
            push @out, {
                name    => $col->name,
                existed => $existed,
                error   => $error,
                exists  => $self->schema->resultset('Layout')->find($col->id) ? 1 : 0,
            };
        }
        $_->layout->clear foreach values %{$self->{sheets}};
        return \@out;
    }
    if ($op eq 'position')
    {
        $layout->position(map { $self->_col($layout, $_)->id } @{$args->{columns}});
        $_->layout->clear foreach values %{$self->{sheets}};
        return {};
    }
    elsif ($op eq 'set_groups')
    {
        # Table permissions: list of [group, permission]
        my @set;
        foreach my $g (@{$args->{permissions}})
        {
            my ($group, $perm) = @$g;
            my $gid = $group eq 'default' ? $self->_first_sheet->group->id : $self->_group_id($group);
            push @set, "${gid}_$perm";
        }
        $layout->set_groups(\@set);
        $layout->write;
        $_->layout->clear foreach values %{$self->{sheets}};
        return {};
    }
    elsif ($op eq 'update')
    {
        foreach my $attr (qw/name name_short sort_type hide_in_selector homepage_text/)
        {
            $layout->$attr($args->{$attr}) if exists $args->{$attr};
        }
        if (exists $args->{sort_layout})
        {
            $layout->sort_layout_id($args->{sort_layout} ? $self->_col($layout, $args->{sort_layout})->id : undef);
        }
        if (exists $args->{forget_history})
        {
            $layout->_rset->update({ forget_history => $args->{forget_history} });
        }
        if (exists $args->{default_view_limit_extra})
        {
            my $v = $args->{default_view_limit_extra};
            $layout->_rset->update({ default_view_limit_extra_id => $v ? $self->{views}->{$v} : undef });
        }
        if (exists $args->{view_limit})
        {
            $layout->view_limit_id($args->{view_limit} ? $self->{views}->{$args->{view_limit}} : undef);
        }
        $layout->write;
        $_->layout->clear foreach values %{$self->{sheets}};
        return {};
    }
    elsif ($op eq 'columns')
    {
        my $l = $self->_layout($args->{sheet}, $args->{user});
        my %opts = %{$args->{options} || {}};
        return [ map { $_->name } $l->all(%opts) ];
    }
    elsif ($op eq 'user_can')
    {
        my $l = $self->_layout($args->{sheet}, $args->{user});
        # Permissions are checked differently once all columns are built
        $l->columns if $args->{with_columns};
        return { map { $_ => ($l->user_can($_) ? 1 : 0) } @{$args->{permissions}} };
    }
    elsif ($op eq 'delete')
    {
        $layout->delete;
        return {};
    }
    error "Unknown layout op $op";
}

# Build a record object
sub _new_record
{   my ($self, $args, $layout) = @_;
    my $user = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    GADS::Record->new(
        user   => $user,
        layout => $layout,
        schema => $self->schema,
        $args->{rewind} ? (rewind => $self->_parse_dt($args->{rewind})) : (),
    );
}

sub _parse_dt
{   my ($self, $v) = @_;
    $v =~ /^(\d{4})-(\d\d)-(\d\d)(?:[ T](\d\d):(\d\d):(\d\d))?/ or error "Invalid datetime $v";
    DateTime->new(year => $1, month => $2, day => $3, hour => $4 || 0, minute => $5 || 0, second => $6 || 0, time_zone => 'UTC');
}

# A current ID, or "\@held:<as>" for that of a held record
sub _held_ref
{   my ($self, $v) = @_;
    return $self->{held}->{$1}->current_id if defined $v && $v =~ /^\@held:(\w+)$/;
    $v;
}

sub _layout_for_step
{   my ($self, $args) = @_;
    $args->{layout_user}
        ? $self->_layout($args->{sheet}, $args->{user})
        : $self->_sheet($args->{sheet})->layout;
}

sub _find_record
{   my ($self, $record, $args) = @_;
    my $sheet = $self->_sheet($args->{sheet});
    if (defined $args->{current_id})
    {
        my $cid = $args->{current_id};
        $cid = $self->{records}->{$1} if $cid =~ /^\@record:(\w+)$/;
        $cid = $self->{held}->{$1}->current_id if $cid =~ /^\@held:(\w+)$/;
        $record->find_current_id($cid, %{$args->{find_options} || {}});
    }
    elsif (defined $args->{record_id})
    {
        my $rid = $args->{record_id};
        $rid = $self->{held}->{$1}->record_id if $rid =~ /^\@held_record:(\w+)$/;
        $record->find_record_id($rid, %{$args->{find_options} || {}});
    }
    elsif (defined $args->{serial})
    {
        $record->find_serial_id($args->{serial});
    }
    elsif (defined $args->{deleted_current_id})
    {
        $record->find_deleted_currentid($args->{deleted_current_id});
    }
    else {
        error "No record identifier specified";
    }
}

sub _set_values
{   my ($self, $record, $layout, $values, $args) = @_;
    my @order = ref $values eq 'ARRAY' ? @$values : map { [$_ => $values->{$_}] } sort keys %$values;
    foreach my $pair (@order)
    {
        my ($name, $value) = ref $pair eq 'ARRAY' ? @$pair : %$pair;
        my $col = $self->_col($layout, $name);
        my $datum = $record->get_field_value($col)
            or error "No datum for column $name";
        # @own_ids: the datum's own current IDs (a no-op edit)
        $value = [ @{$datum->ids} ] if defined $value && !ref $value && $value eq '@own_ids';
        # {own_ids_matching: regex}: the IDs of the datum's own curval values
        # whose text matches
        if (ref $value eq 'HASH' && defined $value->{own_ids_matching})
        {
            my $re = $value->{own_ids_matching};
            $value = [ map $_->{id}, grep $_->{value} =~ /$re/, @{$datum->values} ];
        }
        # @own_queries: the query of each of the datum's own curval values
        # with its current ID (as an unchanged curval edit form)
        $value = [ map $_->{record}->as_query."&current_id=".$_->{record}->current_id, @{$datum->values} ]
            if defined $value && !ref $value && $value eq '@own_queries';
        # In a list, {own_id: regex} is the ID of the datum's own curval
        # value whose text matches, and with query that query plus
        # &current_id= that ID
        if (ref $value eq 'ARRAY')
        {
            $value = [ map {
                if (ref $_ eq 'HASH' && defined $_->{own_id})
                {
                    my $re = $_->{own_id};
                    my ($id) = map $_->{id}, grep $_->{value} =~ /$re/, @{$datum->values};
                    defined $id or error "No own value matching $re";
                    defined $_->{query}
                        ? $self->_convert_value_single($col, $_->{query})."&current_id=$id"
                        : $id;
                }
                else { $_ }
            } @$value ];
        }
        $datum->set_value($self->_convert_value($col, $value), %{$args->{set_options} || {}});
    }
}

sub step_record
{   my ($self, $args) = @_;
    my $op     = $args->{op} || 'create';
    my $layout = $self->_layout_for_step($args);
    my $record = $self->_new_record($args, $layout);
    $record->columns([ map { $self->_col($layout, $_)->id } @{$args->{columns}} ])
        if $args->{columns};
    my %write_options = (no_alerts => 1, %{$args->{options} || {}});
    if ($op eq 'create')
    {
        $record->initialise(instance_id => $layout->instance_id);
        $record->parent_id($self->_held_ref($args->{parent_id})) if $args->{parent_id};
        $record->linked_id($args->{linked_id}) if $args->{linked_id};
        $self->_set_values($record, $layout, $args->{values} || {}, $args);
        $record->write(%write_options);
        $self->{records}->{$args->{as}} = $record->current_id if $args->{as};
        my $obs = {
            current_id => $record->current_id,
            record_id  => $record->record_id,
            serial     => $record->serial,
        };
        $obs->{record} = $self->_observe_after($args, $record->current_id)->{values}
            if $args->{observe};
        return $obs;
    }
    $self->_find_record($record, $args);
    if ($op eq 'update')
    {
        $self->_set_values($record, $layout, $args->{values} || {}, $args);
        if ($args->{check_changed})
        {
            my %changed;
            foreach my $name (@{$args->{check_changed}})
            {
                $changed{$name} = $record->get_field_value($self->_col($layout, $name))->changed ? 1 : 0;
            }
            $self->{_changed} = \%changed;
        }
        $record->write(%write_options);
        my $obs = {
            current_id => $record->current_id,
            record_id  => $record->record_id,
        };
        $obs->{changed} = delete $self->{_changed} if $self->{_changed};
        $obs->{record} = $self->_observe_after($args, $record->current_id)->{values}
            if $args->{observe};
        return $obs;
    }
    elsif ($op eq 'get')
    {
        my $obs = $self->_observe_record($record, $layout, observe => $args->{observe},
            with_record_id => 1, with_parent => 1);
        if ($args->{for_code})
        {
            my %fc;
            foreach my $name (@{$args->{for_code}})
            {
                my $col = $self->_col($layout, $name);
                my $datum = $record->get_field_value($col);
                $fc{$name} = _lua_typed($datum->for_code(fields => {}, already_seen_code => undef));
            }
            $obs->{for_code} = \%fc;
        }
        if ($args->{html_form})
        {
            $obs->{html_form} = { map {
                my $d = $record->get_field_value($self->_col($layout, $_));
                my $hf = $d->html_form;
                $_ => [ map { ref $_ eq 'HASH' ? $_->{id} // $_->{value_id} : $_ } @$hf ]
            } @{$args->{html_form}} };
        }
        if ($args->{blank})
        {
            $obs->{blank} = { map { $_ => ($record->get_field_value($self->_col($layout, $_))->blank ? 1 : 0) } @{$args->{blank}} };
        }
        if ($args->{datum_values})
        {
            $obs->{datum_values} = { map {
                my $d = $record->get_field_value($self->_col($layout, $_));
                $_ => $d->can('ids') ? $d->ids : $d->can('values') ? $d->values : undef
            } @{$args->{datum_values}} };
        }
        if ($args->{curval_records})
        {
            $obs->{curval_records} = { map {
                my $d = $record->get_field_value($self->_col($layout, $_));
                $_ => [ map { $_->{id} } @{$d->values} ]
            } @{$args->{curval_records}} };
        }
        if ($args->{dependent_not_shown})
        {
            $obs->{dependent_not_shown} = { map {
                $_ => ($record->get_field_value($self->_col($layout, $_))->dependent_not_shown ? 1 : 0)
            } @{$args->{dependent_not_shown}} };
        }
        if ($args->{record_props})
        {
            $obs->{props} = {
                is_draft  => $record->is_draft ? 1 : 0,
                deleted   => $record->deleted ? 1 : 0,
                serial    => $record->serial,
                linked_id => $record->linked_id,
                child_record_ids => $record->child_record_ids,
            };
        }
        return $obs;
    }
    elsif ($op eq 'delete')
    {
        $record->delete_current(%{$args->{delete_options} || {}});
        return { deleted => $record->current_id };
    }
    elsif ($op eq 'purge')
    {
        $record->purge_current;
        return { purged => 1 };
    }
    elsif ($op eq 'restore')
    {
        $record->restore;
        return { restored => 1 };
    }
    elsif ($op eq 'versions')
    {
        return [ map { { id => $_->id, created => _plain($_->created) } } $record->versions ];
    }
    elsif ($op eq 'write_linked_id')
    {
        $record->write_linked_id($args->{linked_id});
        return {};
    }
    error "Unknown record op $op";
}

# Operations on a record object that is kept between steps, mirroring
# tests that reuse the same GADS::Record object
sub step_held
{   my ($self, $args) = @_;
    my $op   = $args->{op} or error "op required";
    my $name = $args->{as} or error "as required";
    my $layout = $op =~ /^(new|single|find)$/ ? $self->_layout_for_step($args) : undef;
    if ($op eq 'new')
    {
        my $record = $self->_new_record($args, $layout);
        $record->parent_id($self->_held_ref($args->{parent_id})) if $args->{parent_id};
        if (my $l = $args->{linked_id})
        {
            $l = $self->{held}->{$1}->current_id if $l =~ /^\@held:(\w+)$/;
            $record->linked_id($l);
        }
        $record->initialise(instance_id => $layout->instance_id) unless $args->{no_initialise};
        $self->{held}->{$name} = $record;
        return {};
    }
    if ($op eq 'single')
    {
        # First record of a sheet, retrieved using Records->single. With
        # panic_on_code, error if any code values need to be evaluated
        # during retrieval (i.e. they should have been stored already)
        my $user = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
        local $ENV{GADS_PANIC_ON_ENTERING_CODE} = $args->{panic_on_code} ? 1 : 0;
        my $records = GADS::Records->new(
            user   => $user,
            layout => $layout,
            schema => $self->schema,
            $args->{view_ref} ? (view => $self->_build_view($args, $layout)) : (),
            $args->{is_deleted} ? (is_deleted => 1) : (),
        );
        # nth: the record from calling single that many times
        $records->single foreach 2..($args->{nth} || 1);
        $self->{held}->{$name} = $records->single or error "No records";
        $op = 'get';
    }
    $self->{held}->{$name} ||= $self->_new_record($args, $layout)
        if $op eq 'find';
    my $record = $self->{held}->{$name} or error "Unknown held record $name";
    $record->layout->clear if $args->{clear_layout_first};
    $layout = $record->layout;
    my $obs = {};
    if ($op eq 'find')
    {
        # Resolve any held reference before clearing (which removes the ID)
        my %find = %$args;
        $find{current_id} = $self->{held}->{$1}->current_id
            if defined $find{current_id} && $find{current_id} =~ /^\@held:(\w+)$/;
        $record->clear if $args->{clear};
        if ($args->{chronology})
        {
            # Record::find_chronology_id: observe each version's editor,
            # time and the changed fields (name_short and value)
            $record->find_chronology_id($find{current_id});
            $obs->{chronology} = [ map {
                +{
                    editor   => $_->{editor}->as_string,
                    datetime => $_->{datetime}->as_string,
                    changed  => [ map { { name_short => $_->{name_short}, value => $_->{data}->{value} } } @{$_->{changed}} ],
                }
            } @{$record->chronology} ];
        }
        else {
            $self->_find_record($record, \%find);
        }
        # version: n then loads the nth version (latest first)
        if (defined $args->{version})
        {
            my $version = ($record->versions)[$args->{version}]
                or error "No version $args->{version}";
            $record->clear;
            $record->find_record_id($version->id);
        }
    }
    elsif ($op eq 'clone_oldvalue')
    {
        # Make the datums look as if they have been written to: set each
        # one's oldvalue to a clone of itself
        foreach my $name (@{$args->{columns}})
        {
            my $datum = $record->get_field_value($self->_col($layout, $name));
            $datum->oldvalue($datum->clone);
        }
    }
    elsif ($op eq 'clear')
    {
        $record->clear;
    }
    elsif ($op eq 'initialise')
    {
        $record->initialise;
    }
    elsif ($op eq 'clone')
    {
        # Clone the record into another held record (to)
        $self->{held}->{$args->{to}} = $record->clone;
    }
    elsif ($op eq 'set_from_form')
    {
        # Set values from the html_form of the same columns of another held
        # record (from), as a form submission. as_query uses the as_query of
        # each (curval) value, and replace: [from, to] substitutes text
        my $src = $self->{held}->{$args->{from}} or error "Unknown held record $args->{from}";
        foreach my $c (@{$args->{columns}})
        {
            my $col  = $self->_col($record->layout, $c);
            my $form = $src->fields->{$col->id}->html_form;
            my @vals = $args->{as_query} ? map($_->{as_query}, @$form) : @$form;
            if (my $r = $args->{replace})
            {
                s/\Q$r->[0]\E/$r->[1]/ foreach @vals;
            }
            $record->fields->{$col->id}->set_value([@vals]);
        }
    }
    elsif ($op eq 'load_remembered_values')
    {
        $record->load_remembered_values;
    }
    elsif ($op eq 'set')
    {
        $self->_set_values($record, $layout, $args->{values} || {}, $args);
    }
    elsif ($op eq 'user')
    {
        $record->user($self->_user($args->{user}));
    }
    elsif ($op eq 'write')
    {
        my %opts = %{$args->{options} || {}};
        $opts{version_datetime} = $self->_parse_dt($opts{version_datetime})
            if $opts{version_datetime};
        $opts{submission_token} = $record->submission_token
            if $opts{submission_token};
        # submitted_fields_of: the fields required by that column's filter
        $opts{submitted_fields} = $record->layout->column($self->_col($record->layout, delete $opts{submitted_fields_of})->id)->subvals_input_required
            if $opts{submitted_fields_of};
        $record->write(no_alerts => 1, %opts);
        $obs->{current_id} = $record->current_id;
        $obs->{record_id}  = $record->record_id;
    }
    elsif ($op eq 're_evaluate')
    {
        $record->get_field_value($self->_col($layout, $args->{column}))->re_evaluate;
    }
    elsif ($op eq 'write_value')
    {
        $record->get_field_value($self->_col($layout, $args->{column}))->write_value;
    }
    elsif ($op eq 'delete')
    {
        $record->delete_current;
    }
    elsif ($op eq 'purge')
    {
        $record->purge_current;
    }
    elsif ($op eq 'remove_id')
    {
        $record->remove_id;
    }
    elsif ($op eq 'write_linked_id')
    {
        my $l = $args->{linked};
        $l = $self->{held}->{$1}->current_id if defined $l && $l =~ /^\@held:(\w+)$/;
        $record->write_linked_id($l);
    }
    elsif ($op eq 'purge_version')
    {
        $record->purge;
    }
    elsif ($op eq 'restore')
    {
        $record->restore;
    }
    elsif ($op ne 'get')
    {
        error "Unknown held op $op";
    }
    $record->layout->clear if $args->{clear_layout};
    $obs->{current_id} = $record->current_id;
    if (my $cols = $args->{observe})
    {
        my %values;
        foreach my $c (@$cols)
        {
            my $datum = $record->get_field_value($self->_col($layout, $c));
            $values{$c} = defined $datum ? $datum->as_string : undef;
        }
        $obs->{values} = \%values;
    }
    if (my $cols = $args->{changed})
    {
        $obs->{changed} = { map { $_ => ($record->get_field_value($self->_col($layout, $_))->changed ? 1 : 0) } @$cols };
    }
    if (my $cols = $args->{value_refs})
    {
        $obs->{value_refs} = { map {
            my $d = $record->get_field_value($self->_col($layout, $_));
            $_ => [ map { ref $_ } @{$d->value} ]
        } @$cols };
    }
    $obs->{versions} = [ map { $_->created->ymd } $record->versions ] if $args->{versions};
    $obs->{serial} = $record->serial if $args->{serial};
    $obs->{parent_id} = $record->parent_id if $args->{observe_parent};
    # Whether values are the same as those of another held record (for
    # values that differ between runs, such as the time)
    if (my $same = $args->{same_as})
    {
        my $other = $self->{held}->{$same->{as}} or error "Unknown held record $same->{as}";
        $obs->{same_as} = { map {
            my $col = $self->_col($layout, $_);
            $_ => ($record->fields->{$col->id}->as_string eq $other->fields->{$col->id}->as_string ? 1 : 0)
        } @{$same->{columns}} };
    }
    # Values of a curval field, from the record's layout (which may have
    # been rebuilt)
    # With filtered_full, also value_id, html and selector_id (with the
    # random token of a new record's replaced by *)
    $obs->{filtered_values} = [ map +{ id => $_->{id}, value => $_->{value},
        ($args->{filtered_full} ? (value_id => $_->{value_id}, html => $_->{html},
            selector_id => $_->{selector_id} =~ s/^new_.*/new_*/r) : ()) },
        @{$record->layout->column($self->_col($record->layout, $args->{filtered_values})->id)->filtered_values($args->{submission_token} ? $record->submission_token : undef)} ]
        if $args->{filtered_values};
    $obs->{columns_render} = [ map $_->name, @{$record->columns_render} ]
        if $args->{columns_render};
    if ($args->{record_meta})
    {
        my ($edited, $deletedby, $deleted) = ($record->edited_user, $record->deletedby, $record->deleted);
        $obs->{meta} = {
            edited_user => $edited ? $edited->as_string : undef,
            deletedby   => $deletedby ? $deletedby->as_string : undef,
            deleted     => $deleted ? "$deleted" : undef,
        };
    }
    $obs->{child_record_ids} = $record->child_record_ids if $args->{child_record_ids};
    if (my $cols = $args->{datum})
    {
        $obs->{datum} = { map {
            $_ => _datum_props($record->get_field_value($self->_col($layout, $_)), $args->{datum_props})
        } @$cols };
    }
    if (my $c = $args->{for_code_curval})
    {
        # Values of a curval as passed to code, with all fields of the
        # referred sheet requested
        my $d = $record->get_field_value($self->_col($layout, $c));
        my $fields = { map { $_->name_short => 1 } grep $_->name_short, $d->column->layout_parent->all };
        # for_code_fields: request just those fields, and observe the first
        # value's field_values
        $fields = { map { $_ => 1 } @{$args->{for_code_fields}} }
            if $args->{for_code_fields};
        my $fc = $d->for_code(fields => $fields);
        my @list = ref $fc eq 'ARRAY' ? @$fc : defined $fc ? ($fc) : ();
        $obs->{for_code_curval} = {
            count             => scalar @list,
            field_values_keys => [ sort keys %{(@list && $list[0]->{field_values}) || {}} ],
        };
        $obs->{for_code_curval}->{field_values} = @list ? $list[0]->{field_values} : undef
            if $args->{for_code_fields};
    }
    $obs;
}

my @default_datum_props = qw/as_string blank changed/;

# Properties of a datum
sub _datum_props
{   my ($datum, $props) = @_;
    return undef if !$datum;
    my %obs;
    foreach my $p (@{$props || \@default_datum_props})
    {
        if ($p eq 'as_string' || $p eq 'html' || $p eq 'filter_value')
        {
            $obs{$p} = $datum->$p;
        }
        elsif ($p eq 'blank' || $p eq 'changed')
        {
            $obs{$p} = $datum->$p ? 1 : 0;
        }
        elsif ($p eq 'html_form')
        {
            $obs{$p} = [ map { ref $_ eq 'HASH' ? $_->{id} // $_->{value_id} : $_ } @{$datum->html_form} ];
        }
        elsif ($p eq 'dependent_not_shown')
        {
            $obs{$p} = $datum->dependent_not_shown ? 1 : 0;
        }
        elsif ($p eq 'html_form_query')
        {
            # The as_query of each curval value of the edit form
            $obs{$p} = [ map { ref $_ eq 'HASH' ? $_->{as_query} : undef } @{$datum->html_form} ];
        }
        elsif ($p eq 'text_all')
        {
            $obs{$p} = [ @{$datum->text_all} ];
        }
        elsif ($p eq 'ids')
        {
            $obs{$p} = [ @{$datum->ids} ];
        }
        elsif ($p eq 'deleted_values')
        {
            $obs{$p} = [ map $_->{value}, @{$datum->deleted_values} ];
        }
        elsif ($p eq 'oldvalue')
        {
            my $old = $datum->oldvalue;
            $obs{$p} = $old ? { as_string => $old->as_string, blank => $old->blank ? 1 : 0 } : undef;
        }
        elsif ($p eq 'empty_value')
        {
            # As checked for new records in t/005_setvalues.t
            my $empty = 1;
            if ($datum->column->multivalue)
            {
                $empty = 0 if $datum->can('value') && !(ref $datum->value eq 'ARRAY' && !@{$datum->value});
                $empty = 0 if $datum->can('ids') && !(ref $datum->ids eq 'ARRAY' && !@{$datum->ids});
            }
            else {
                $empty = 0 if $datum->can('value') && defined $datum->value;
                $empty = 0 if $datum->can('id') && $datum->column->type ne 'tree' && defined $datum->id;
            }
            $obs{$p} = $empty;
        }
        elsif ($p eq 'id')
        {
            $obs{$p} = $datum->can('id') ? $datum->id : undef;
        }
        elsif ($p eq 'id_hash')
        {
            $obs{$p} = $datum->can('id_hash') ? (ref $datum->id_hash eq 'HASH' ? 1 : 0) : undef;
        }
        elsif ($p eq 'hours')
        {
            # Hour of each datetime value (in the time zone it is held in)
            $obs{$p} = [ map $_->hour, @{$datum->values} ];
        }
        else {
            error "Unknown datum property $p";
        }
    }
    \%obs;
}

# Datum created directly from a column, as for grouped results or values
# that start as undefined
sub step_datum
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $column = $self->_col($layout, $args->{column});
    my %params = (
        column => $column,
        schema => $self->schema,
        layout => $column->layout,
    );
    if (exists $args->{init_value})
    {
        $params{init_value} = [ map $self->_convert_value($column, $_), @{$args->{init_value}} ];
    }
    if ($args->{init_no_value})
    {
        $params{set_value}       = undef;
        $params{init_no_value}   = 1;
        $params{datetime_parser} = $self->schema->storage->datetime_parser;
    }
    my $datum = $column->class->new(%params);
    $datum->set_value($self->_convert_value($column, $args->{set_value}), %{$args->{set_options} || {}})
        if exists $args->{set_value};
    _datum_props($datum, $args->{datum_props});
}

sub _observe_after
{   my ($self, $args, $current_id) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $record = GADS::Record->new(
        user   => $self->_first_sheet->user,
        layout => $layout,
        schema => $self->schema,
    );
    $record->find_current_id($current_id);
    $self->_observe_record($record, $layout, observe => ref $args->{observe} eq 'ARRAY' ? $args->{observe} : undef);
}

sub _build_view
{   my ($self, $args, $layout) = @_;
    if (my $va = $args->{view_ref})
    {
        my $vid = $self->{views}->{$va} // error "Unknown view $va";
        return GADS::View->new(
            id          => $vid,
            instance_id => $layout->instance_id,
            layout      => $layout,
            schema      => $self->schema,
        );
    }
    my $v = $args->{view} || {};
    my $filter = $self->_convert_filter($layout, $args->{filter} || $v->{filter});
    my $view = GADS::View->new(
        name        => $v->{name} || 'Test view',
        instance_id => $layout->instance_id,
        layout      => $layout,
        schema      => $self->schema,
        $filter ? (filter => GADS::Filter->new(as_hash => $filter, layout => $layout)) : (),
    );
    my $columns = $args->{columns} || $v->{columns};
    $view->columns([ map { $self->_col($layout, $_)->id } @$columns ]) if $columns;
    if (my $sorts = $v->{sorts})
    {
        $view->set_sorts({
            fields => [ map { $self->_filter_id($layout, $_->{name}) } @$sorts ],
            types  => [ map { $_->{type} || 'asc' } @$sorts ],
        });
    }
    if (my $groups = $v->{groups})
    {
        $view->set_groups([ map { $self->_filter_id($layout, $_) } @$groups ]);
    }
    $view;
}

# Retrieve the first record (Records->single) repeat times with a view, as
# for random sorts: observes for each of columns whether the values differed
# between retrievals (many) or not (one, with the value)
sub step_repeat_single
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my %seen;
    foreach (1..($args->{repeat} || 10))
    {
        my $records = GADS::Records->new(
            user   => $user,
            layout => $layout,
            schema => $self->schema,
            view   => $self->_build_view($args, $layout),
        );
        my $record = $records->single;
        $seen{$_}->{$record->get_field_value($self->_col($layout, $_))->as_string} = 1
            foreach @{$args->{columns}};
    }
    return { map {
        my @v = keys %{$seen{$_}};
        $_ => (@v > 1 ? 'many' : "one: $v[0]")
    } @{$args->{columns}} };
}

# Retrieve the file of record current_id with
# ResultSet::Fileval->find_with_permission as user
sub step_file_access
{   my ($self, $args) = @_;
    my $fileval = $self->schema->resultset('Fileval')->search({
        'record.current_id' => $args->{current_id},
    },{
        join     => { files => 'record' },
        order_by => 'me.id',
    })->next or error "No file for record $args->{current_id}";
    my $file = $self->schema->resultset('Fileval')->find_with_permission($fileval->id, $self->_user($args->{user}));
    return { found => $file ? 1 : 0 };
}

sub step_search
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my $view   = ($args->{filter} || $args->{view} || $args->{view_ref} || $args->{columns})
        ? $self->_build_view($args, $layout) : undef;
    my %params = (
        user   => $user,
        layout => $layout,
        schema => $self->schema,
    );
    $params{view}    = $view if $view;
    $params{rows}    = $args->{rows} if $args->{rows};
    $params{page}    = $args->{page} if $args->{page};
    $params{search}  = $args->{search} if defined $args->{search};
    $params{rewind}  = $self->_parse_dt($args->{rewind}) if $args->{rewind};
    $params{from}    = $self->_parse_dt($args->{from}) if $args->{from};
    $params{to}      = $self->_parse_dt($args->{to}) if $args->{to};
    $params{exclusive} = $args->{exclusive} if $args->{exclusive};
    $params{include_children} = $args->{include_children} if exists $args->{include_children};
    $params{is_deleted} = $args->{is_deleted} if $args->{is_deleted};
    $params{include_approval} = $args->{include_approval} if $args->{include_approval};
    $params{view_limit_extra_id} = $self->{views}->{$args->{view_limit_extra}} if $args->{view_limit_extra};
    $params{view_limit_override_id} = $self->{views}->{$args->{view_limit_override}} if $args->{view_limit_override};
    $params{columns} = [ map { $self->_col($layout, $_)->id } @{$args->{retrieve}} ] if $args->{retrieve};
    # retrieve_all: all the layout's non-internal columns
    $params{columns} = [ map $_->id, $layout->all(exclude_internal => 1) ] if $args->{retrieve_all};
    if (my $ds = $args->{default_sort})
    {
        $params{default_sort} = { type => $ds->{type}, id => $ds->{name} ? $self->_col($layout, $ds->{name})->id : $ds->{id} };
    }
    my $records = GADS::Records->new(%params);
    if (my $sort = $args->{sort})
    {
        # sheet: the column is in another sheet; parent: the curval column
        # it is a subfield of
        $records->sort([ map {
            my $l = $_->{sheet} ? $self->_sheet($_->{sheet})->layout : $layout;
            +{
                id   => (defined $_->{id} ? $_->{id} : $self->_filter_id($l, $_->{name})),
                type => $_->{type} || 'asc',
                ($_->{parent} ? (parent_id => $self->_col($layout, $_->{parent})->id) : ()),
            }
        } @$sort ]);
    }
    if (my $af = $args->{additional_filters})
    {
        $records->additional_filters([ map { { id => $self->_col($layout, $_->{name})->id, value => $_->{value}, is_text => $_->{is_text} } } @$af ]);
    }
    # timeline: the records' data_timeline (with label, group and color
    # columns), calendar: data_calendar
    return $self->_observe_timeline($records, $layout, $args->{timeline})
        if $args->{timeline};
    return { items => [ map _timeline_item($_), @{$records->data_calendar} ] }
        if $args->{calendar};
    my $obs = {};
    $obs->{count} = $records->count unless $args->{no_count};
    $obs->{pages} = $records->pages if $args->{rows};
    my @rows;
    my @ids;
    my $observe = $args->{observe};
    my $results = $args->{no_results} ? [] : $records->results;
    foreach my $rec (@$results)
    {
        push @ids, $rec->current_id;
        # record_columns: the columns of each record shown (its
        # columns_render, as the presentation layer)
        push @{$obs->{record_columns}}, [ map $_->name, @{$rec->columns_render} ]
            if $args->{record_columns};
        if ($observe)
        {
            my %v;
            foreach my $name (@$observe)
            {
                my $col = $self->_col($layout, $name);
                my $datum = $rec->get_field_value($col);
                $v{$name} = defined $datum ? $datum->as_string : undef;
            }
            push @rows, \%v;
        }
    }
    $obs->{ids}  = \@ids;
    $obs->{record_columns} ||= [] if $args->{record_columns};
    if (@ids)
    {
        $obs->{first} = $ids[0];
        $obs->{last}  = $ids[-1];
    }
    $obs->{rows} = \@rows if $observe;
    if ($args->{aggregate})
    {
        my $agg = $records->aggregate_results;
        $obs->{aggregate} = $agg ? { map {
            my $col = $self->_col($layout, $_);
            $_ => $agg->get_field_value($col)->as_string
        } @{$args->{aggregate}} } : undef;
    }
    $obs->{columns_render} = [ map $_->name, @{$records->columns_render} ]
        if $args->{columns_render};
    if ($args->{group_counts})
    {
        $obs->{id_counts} = [ map { $_->id_count } @$results ];
    }
    if ($args->{relations})
    {
        $obs->{relations} = [ map +{
            parent_id        => $_->parent_id,
            child_record_ids => $_->child_record_ids,
        }, @$results ];
    }
    if ($args->{single})
    {
        # Iterate using single() as GADS does for large sets
        $records->clear;
        my @single;
        # single_dependent_not_shown: also each record's dependent_not_shown
        # for that column
        my $dns = $args->{single_dependent_not_shown} && $self->_col($layout, $args->{single_dependent_not_shown});
        my @dns;
        while (my $r = $records->single)
        {
            push @single, $r->current_id;
            push @dns, $r->get_field_value($dns)->dependent_not_shown ? 1 : 0 if $dns;
        }
        $obs->{single} = \@single;
        $obs->{single_dependent_not_shown} = \@dns if $dns;
    }
    $obs;
}

# An item of a timeline or calendar, with its style (a random colour for
# most values) only as whether it has one
sub _timeline_item
{   my $item = shift;
    my %i = %$item;
    delete @i{qw/dt dt_to/};
    $i{style} = $i{style} ? 1 : 0;
    $i{color} = $i{color} ? 1 : 0 if exists $i{color};
    $i{has_time} = $i{has_time} ? 1 : 0 if exists $i{has_time};
    $i{values} = [ map "$_->{name}: $_->{value}", @{$i{values}} ] if $i{values};
    \%i;
}

sub _observe_timeline
{   my ($self, $records, $layout, $opts) = @_;
    my %options;
    if (ref $opts eq 'HASH')
    {
        $options{$_} = $self->_col($layout, $opts->{$_})->id
            foreach grep $opts->{$_} && $opts->{$_} !~ /^[0-9]+$/, qw/label group color/;
        $options{$_} = $opts->{$_}
            foreach grep $opts->{$_} && $opts->{$_} =~ /^[0-9]+$/, qw/label group color/;
        $options{all_group_values} = 1 if $opts->{all_group_values};
    }
    my $tl = $records->data_timeline(%options);
    return {
        items  => [ map _timeline_item($_), @{$tl->{items}} ],
        groups => [ map +{ content => $_->{content}, order => $_->{order} },
            sort { $a->{order} <=> $b->{order} } @{$tl->{groups}} ],
        colors => scalar @{$tl->{colors}},
        min    => $tl->{min} ? $tl->{min}->strftime('%Y-%m-%dT%H:%M:%S') : undef,
        max    => $tl->{max} ? $tl->{max}->strftime('%Y-%m-%dT%H:%M:%S') : undef,
    };
}

sub step_view
{   my ($self, $args) = @_;
    my $op     = $args->{op} || 'create';
    my $layout = $self->_layout($args->{sheet}, $args->{user} // 'superadmin');
    if ($op eq 'create' || $op eq 'update')
    {
        my $view;
        if ($op eq 'update')
        {
            my $vid = $self->{views}->{$args->{ref}} // error "Unknown view $args->{ref}";
            $view = GADS::View->new(
                id          => $vid,
                instance_id => $layout->instance_id,
                layout      => $layout,
                schema      => $self->schema,
            );
        }
        else {
            $view = GADS::View->new(
                instance_id => $layout->instance_id,
                layout      => $layout,
                schema      => $self->schema,
            );
        }
        $view->name($args->{name}) if exists $args->{name};
        $view->global($args->{global}) if exists $args->{global};
        $view->is_admin($args->{is_admin}) if exists $args->{is_admin};
        $view->group_id($args->{group} ? $self->_group_id($args->{group}) : undef) if exists $args->{group};
        if (exists $args->{filter})
        {
            my $filter = $self->_convert_filter($layout, $args->{filter});
            $view->filter(GADS::Filter->new(as_hash => $filter || {}, layout => $layout));
        }
        # Set the view's existing filter (so that the change is tracked), as
        # a hash or as JSON
        $view->filter->as_hash($self->_convert_filter($layout, $args->{set_filter}) || {})
            if exists $args->{set_filter};
        $view->filter->as_json(JSON::encode_json($self->_convert_filter($layout, $args->{set_filter_json}) || {}))
            if exists $args->{set_filter_json};
        if ($args->{columns})
        {
            $view->columns($args->{columns} eq '@all'
                ? [ map { $_->id } $layout->all ]
                : [ map { $self->_col($layout, $_)->id } @{$args->{columns}} ]);
        }
        if (my $sorts = $args->{sorts})
        {
            $view->set_sorts({
                fields => [ map { $self->_filter_id($layout, $_->{name}) } @$sorts ],
                types  => [ map { $_->{type} || 'asc' } @$sorts ],
            });
        }
        $view->set_groups([ map { $self->_filter_id($layout, $_) } @{$args->{groups}} ]) if $args->{groups};
        $view->write(%{$args->{write_options} || {}});
        $self->{views}->{$args->{as}} = $view->id if $args->{as};
        return { id => $view->id };
    }
    elsif ($op eq 'delete')
    {
        my $vid = $self->{views}->{$args->{ref}} // error "Unknown view $args->{ref}";
        my $view = GADS::View->new(
            id          => $vid,
            instance_id => $layout->instance_id,
            layout      => $layout,
            schema      => $self->schema,
        );
        $view->delete;
        return {};
    }
    elsif ($op eq 'get')
    {
        my $vid = $self->{views}->{$args->{ref}} // error "Unknown view $args->{ref}";
        my $view = GADS::View->new(
            id          => $vid,
            instance_id => $layout->instance_id,
            layout      => $layout,
            schema      => $self->schema,
        );
        return {
            id       => $view->id,
            name     => $view->name,
            global   => $view->global ? 1 : 0,
            is_admin => $view->is_admin ? 1 : 0,
            filter   => $view->filter->as_hash,
            columns  => $view->columns,
            writable => $view->writable ? 1 : 0,
            exists   => $view->exists,
        };
    }
    elsif ($op eq 'list')
    {
        my $views = GADS::Views->new(
            user        => $self->_user($args->{user} // 'superadmin'),
            schema      => $self->schema,
            layout      => $layout,
            instance_id => $layout->instance_id,
        );
        return {
            user_views   => [ map { $_->name } @{$views->user_views} ],
            global_views => [ map { $_->name } @{$views->global} ],
        };
    }
    error "Unknown view op $op";
}

# A filter object (GADS::Filter) kept between steps. op: new (with as_json
# or as_hash, or neither), set_json, set_hash, get. observe lists what to
# report, in order (each may build lazy values): as_hash, as_json (decoded),
# changed, column_ids (joined with spaces)
sub step_filter_held
{   my ($self, $args) = @_;
    my $op   = $args->{op} || 'get';
    my $name = $args->{as} or error "Held filter alias (as) required";
    if ($op eq 'new')
    {
        $self->{held_filters}->{$name} = GADS::Filter->new(
            exists $args->{as_json} ? (as_json => JSON::encode_json($args->{as_json})) : (),
            exists $args->{as_hash} ? (as_hash => $args->{as_hash}) : (),
        );
    }
    my $filter = $self->{held_filters}->{$name} or error "Unknown held filter $name";
    if ($op eq 'set_json')
    {
        $filter->as_json(JSON::encode_json($args->{value}));
    }
    elsif ($op eq 'set_hash')
    {
        $filter->as_hash($args->{value});
    }
    elsif ($op ne 'new' && $op ne 'get')
    {
        error "Unknown held filter op $op";
    }
    my $obs = {};
    foreach my $o (@{$args->{observe} || []})
    {
        $obs->{$o} = $o eq 'as_hash' ? $filter->as_hash
            : $o eq 'as_json' ? JSON::decode_json($filter->as_json)
            : $o eq 'changed' ? ($filter->changed ? 1 : 0)
            : $o eq 'column_ids' ? "@{$filter->column_ids}"
            : error "Unknown filter observation $o";
    }
    $obs;
}

# Create (op create: name, sheet or table, as) or update (op update: as,
# prevent_edit as a topic alias or null) a topic
sub step_topic
{   my ($self, $args) = @_;
    my $op = $args->{op} || 'create';
    if ($op eq 'create')
    {
        my $instance_id = $args->{table}
            ? $self->{tables}->{$args->{table}}->instance_id
            : $self->_sheet($args->{sheet})->layout->instance_id;
        my $topic = $self->schema->resultset('Topic')->create({
            name        => $args->{name},
            instance_id => $instance_id,
        });
        $self->{topics}->{$args->{as}} = $topic->id;
        return { id => $topic->id };
    }
    elsif ($op eq 'update')
    {
        my $id = $self->{topics}->{$args->{as}} // error "Unknown topic $args->{as}";
        my $topic = $self->schema->resultset('Topic')->find($id);
        $topic->prevent_edit_topic_id($args->{prevent_edit} ? $self->{topics}->{$args->{prevent_edit}} : undef);
        $topic->update;
        return {};
    }
    error "Unknown topic op $op";
}

# Purge the values of columns in all versions of a record
# (Result::Current->historic_purge): sheet, current_id, user, columns
sub step_historic_purge
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $current = $self->schema->resultset('Current')->find($args->{current_id})
        or error "Record $args->{current_id} not found";
    $current->historic_purge($self->_user($args->{user} // 'superadmin'),
        map $self->_col($layout, $_)->id, @{$args->{columns}});
    return {};
}

# A CSV download of records (Records->csv_header and csv_line): sheet,
# user and a view as for search. insert_after: {line: N, values: {...}}
# writes a new record once line N has been produced. Observes the header
# and the lines
sub step_download
{   my ($self, $args) = @_;
    my $layout = $self->_layout_for_step($args);
    my $user   = exists $args->{user} ? $self->_user($args->{user}) : $self->_first_sheet->user;
    my $view   = ($args->{filter} || $args->{view} || $args->{view_ref} || $args->{columns})
        ? $self->_build_view($args, $layout) : undef;
    my $records = GADS::Records->new(
        user   => $user,
        layout => $layout,
        schema => $self->schema,
        $view ? (view => $view) : (),
    );
    # The CSV is the values of the database as they are (Text::CSV::Encoded
    # without an encoding): UTF-8 bytes with SQLite, which are decoded for
    # comparison as text, and text with PostgreSQL (DBD::Pg decodes)
    my $text = sub { utf8::is_utf8($_[0]) ? $_[0] : Encode::decode('UTF-8', $_[0]) };
    my $obs = { header => $text->($records->csv_header), lines => [] };
    my $i = 0;
    while (my $line = $records->csv_line)
    {
        $i++;
        push @{$obs->{lines}}, $text->($line);
        my $ins = $args->{insert_after};
        if ($ins && $ins->{line} == $i)
        {
            my $record = GADS::Record->new(
                user   => $user,
                layout => $layout,
                schema => $self->schema,
            );
            $record->initialise;
            $self->_set_values($record, $layout, $ins->{values}, $ins);
            $record->write(no_alerts => 1);
        }
    }
    $obs;
}

# Set the site of the schema (as $schema->site_id)
sub step_site
{   my ($self, $args) = @_;
    $self->schema->site_id($args->{id});
    return {};
}

# The instances the user can access (GADS::Instances->all)
sub step_instances
{   my ($self, $args) = @_;
    my $instances = GADS::Instances->new(schema => $self->schema, user => $self->_user($args->{user}));
    my $obs = { names => [ map $_->name, @{$instances->all} ] };
    $obs->{is_valid} = [ map { $instances->is_valid($_) ? 1 : 0 } @{$args->{is_valid}} ]
        if $args->{is_valid};
    $obs;
}

# One table-level permission test of t/018_instance.t: test (delete, purge,
# download, layout, view_create, link) as user, with the layout of sheet
# either built anew for the user (layout_from: new, which also makes it the
# sheet's layout) or from GADS::Instances (layout_from: instances).
# allowed: whether the permission is expected (to undo what the test did).
# reset_groups: afterwards remove the table permissions using that layout.
# Observes the errors of each action (null if none).
sub step_table_permission
{   my ($self, $args) = @_;
    my $sheet = $self->_sheet($args->{sheet});
    my $user  = $self->_user($args->{user});
    my $schema = $self->schema;
    my $layout;
    if (($args->{layout_from} || 'new') eq 'new')
    {
        $sheet->user_layout($user);
        $sheet->clear_layout;
        $layout = $sheet->layout;
    }
    else {
        $layout = GADS::Instances->new(schema => $schema, user => $user)->layout($sheet->instance_id);
    }
    my $records = GADS::Records->new(
        user   => $user,
        layout => $layout,
        schema => $schema,
    );
    my @errors;
    my $attempt = sub {
        my $code = shift;
        try { $code->() } hide => 'ALL';
        my $e = $@->wasFatal;
        push @errors, $e ? _error_text($e) : undef;
    };
    my $test    = $args->{test};
    my $allowed = $args->{allowed};
    if ($test eq 'delete')
    {
        my $record = $records->single;
        $attempt->(sub { $record->delete_current });
        $schema->resultset('Current')->find($record->current_id)->update({ deleted => undef })
            if $allowed;
    }
    elsif ($test eq 'purge')
    {
        my $record     = $records->single;
        my $current_id = $record->current_id;
        my $record_id  = $record->record_id;
        $schema->resultset('Current')->find($current_id)->update({ deleted => DateTime->now });
        $attempt->(sub { $record->find_deleted_currentid($current_id, $layout->instance_id) });
        $attempt->(sub { $record->find_deleted_recordid($record_id, $layout->instance_id) });
        $schema->resultset('Current')->find($current_id)->update({ deleted => undef });
        $attempt->(sub { $record->purge_current });
        if ($allowed)
        {
            $schema->resultset('Current')->find($current_id)->update({ deleted => DateTime->now });
            $attempt->(sub { $record->purge_current });
            $sheet->create_records;
        }
        # Purging a single version (which no longer exists if purged)
        $attempt->(sub { $record->purge });
        pop @errors if $allowed;
    }
    elsif ($test eq 'download')
    {
        $attempt->(sub { $records->csv_header });
        $attempt->(sub { $records->csv_line });
    }
    elsif ($test eq 'layout')
    {
        my ($col) = $layout->all(userinput => 1);
        $attempt->(sub { $col->write });
    }
    elsif ($test eq 'view_create')
    {
        my $view = GADS::View->new(
            instance_id => $layout->instance_id,
            layout      => $layout,
            schema      => $schema,
            name        => 'Test',
            global      => 0,
            columns     => [],
        );
        $attempt->(sub { $view->write });
    }
    elsif ($test eq 'link')
    {
        my $record = $records->single;
        $attempt->(sub { $record->write_linked_id($record->current_id) });
    }
    else { error "Invalid test: $test" }
    if ($args->{reset_groups})
    {
        $layout->set_groups([]);
        $layout->write;
    }
    return { errors => \@errors };
}

# The views available to the user of the sheet's layout
# (GADS::Views->user_views_all)
sub step_views
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $views = GADS::Views->new(
        schema      => $self->schema,
        layout      => $layout,
        instance_id => $layout->instance_id,
        $args->{other_user} ? (other_user_id => $self->_user($args->{other_user})->id) : (),
    );
    my $all = $views->user_views_all;
    return { names => [ map $_->name, @$all ], ids => [ map $_->id, @$all ] };
}

# A view object kept between steps, using the sheet's layout (whose user may
# change with layout set_user). op: new (with id: view alias, or view
# attributes), set (attributes), write, filter (access the filter), delete,
# get
sub step_view_held
{   my ($self, $args) = @_;
    my $op     = $args->{op} || 'get';
    my $name   = $args->{as} or error "Held view alias (as) required";
    my $layout = $self->_sheet($args->{sheet})->layout;
    my %attrs  = $self->_view_attrs($layout, $args);
    if ($op eq 'new')
    {
        my $id = $args->{id};
        $id = $self->{held_views}->{$1}->id if defined $id && $id =~ /^\@held:(\w+)$/;
        $id = $self->{views}->{$id} // error("Unknown view $id") if defined $id && $id !~ /^-?[0-9]+$/;
        $self->{held_views}->{$name} = GADS::View->new(
            instance_id => $layout->instance_id,
            layout      => $layout,
            schema      => $self->schema,
            defined $id ? (id => $id) : (),
            %attrs,
        );
        return {};
    }
    my $view = $self->{held_views}->{$name} or error "Unknown held view $name";
    if ($op eq 'set')
    {
        $view->$_($attrs{$_}) foreach sort keys %attrs;
        $view->set_groups([ map $self->_filter_id($layout, $_), @{$args->{groups}} ]) if $args->{groups};
        $view->filter(undef) if exists $args->{filter} && !defined $args->{filter};
    }
    elsif ($op eq 'write')
    {
        $view->write;
        $self->{views}->{$args->{view_as}} = $view->id if $args->{view_as};
        return { id => $view->id, user_id => $self->schema->resultset('View')->find($view->id)->user_id };
    }
    elsif ($op eq 'filter')
    {
        $view->filter;
    }
    elsif ($op eq 'delete')
    {
        $view->delete;
    }
    elsif ($op ne 'get')
    {
        error "Unknown held view op $op";
    }
    my $obs = {};
    if ($args->{observe})
    {
        $obs->{name}    = $view->name;
        $obs->{columns} = $view->columns;
    }
    $obs->{filter_base64} = JSON::decode_json(MIME::Base64::decode_base64($view->filter->base64))
        if $args->{filter_base64};
    $obs;
}

# View attributes from a step: name, global, is_admin, columns, group
# (name, default or null), other_user, filter
sub _view_attrs
{   my ($self, $layout, $args) = @_;
    my %attrs;
    exists $args->{$_} and $attrs{$_} = $args->{$_} foreach qw/name global is_admin/;
    $attrs{columns} = [ map $self->_col($layout, $_)->id, @{$args->{columns}} ] if $args->{columns};
    $attrs{group_id} = defined $args->{group} ? $self->_group_id($args->{group}) : undef
        if exists $args->{group};
    $attrs{other_user_id} = $self->_user($args->{other_user})->id if $args->{other_user};
    $attrs{filter} = GADS::Filter->new(as_hash => $self->_convert_filter($layout, $args->{filter}))
        if $args->{filter};
    %attrs;
}

# An alert on a view (GADS::Alert). frequency null removes the alert
# The emails sent since the last emails step
sub step_emails
{   my ($self, $args) = @_;
    my @emails = @EMAILS;
    @EMAILS = ();
    return \@emails;
}

sub step_alert
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $alert = GADS::Alert->new(
        user      => $self->_user($args->{user}),
        layout    => $layout,
        schema    => $self->schema,
        frequency => $args->{frequency},
        view_id   => $self->{views}->{$args->{view}} // error("Unknown view $args->{view}"),
    );
    $alert->write;
    return { id => defined $args->{frequency} ? $alert->id : undef };
}

# Process alerts for changes to records, as GADS does after updating code
# values
sub step_alert_send
{   my ($self, $args) = @_;
    my $sheet  = $self->_sheet($args->{sheet});
    my $layout = $sheet->layout;
    GADS::AlertSend->new(
        layout      => $layout,
        schema      => $self->schema,
        user        => $sheet->user,
        current_ids => $args->{current_ids},
        columns     => [ map $self->_col($layout, $_)->id, @{$args->{columns}} ],
    )->process;
    return {};
}

# Descriptions and links of records for alert emails, for each user in turn
# (sharing the description cache)
sub step_alert_description
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $ad = GADS::AlertDescription->new(schema => $self->schema);
    my %params = (instance_id => $layout->instance_id, current_ids => $args->{current_ids});
    return [ map {
        my $user = $self->_user($_);
        +{
            description => $ad->description(%params, user => $user),
            link        => $ad->link(%params, user => $user),
        };
    } @{$args->{users}} ];
}

sub step_user
{   my ($self, $args) = @_;
    my $op = $args->{op} || 'create';
    my $sheet = $self->_first_sheet;
    if ($op eq 'create')
    {
        my $user = $sheet->create_user(
            permissions => $args->{permissions} || [],
            user_id     => $args->{user_id},
            team_id     => $args->{team_id},
            no_group    => $args->{no_group},
        );
        $self->{users}->{$args->{as}} = $user if $args->{as};
        return { id => $user->id };
    }
    elsif ($op eq 'view_limits')
    {
        my $user = $self->_user($args->{user});
        $user->set_view_limits([ map { $self->{views}->{$_} // error "Unknown view $_" } @{$args->{views}} ]);
        return {};
    }
    elsif ($op eq 'group')
    {
        # Create a group and give it to users
        my $group = GADS::Group->new(schema => $self->schema);
        $group->from_id;
        $group->name($args->{name});
        $group->write;
        $self->{groups}->{$args->{name}} = $group->id;
        foreach my $u (@{$args->{users} || []})
        {
            $self->schema->resultset('UserGroup')->create({
                user_id  => $self->_user($u)->id,
                group_id => $group->id,
            });
        }
        return { id => $group->id };
    }
    elsif ($op eq 'create_user')
    {
        # ResultSet::User->create_user, as the user admin pages (params as
        # the form, with group names and view aliases; welcome emails are
        # not sent)
        my %params = %{$args->{params} || {}};
        $params{groups} = [ map $self->_group_id($_), @{$params{groups}} ]
            if ref $params{groups} eq 'ARRAY';
        $params{view_limits} = [ map $self->{views}->{$_} // error("Unknown view $_"), @{$params{view_limits}} ]
            if ref $params{view_limits} eq 'ARRAY';
        my $user = $self->schema->resultset('User')->create_user(
            current_user     => $self->_user($args->{current_user} // 'superadmin'),
            no_welcome_email => 1,
            %params,
        );
        $self->{users}->{$args->{as}} = $user if $args->{as};
        my $u = $self->schema->resultset('User')->find($user->id);
        return { id => $u->id, value => $u->value, username => $u->username };
    }
    elsif ($op eq 'update_row')
    {
        # Update the user row directly (with its validation)
        my $u = $self->schema->resultset('User')->find($self->_user($args->{user})->id);
        $u->update($args->{set});
        return {};
    }
    elsif ($op eq 'insert_row')
    {
        # Create a user row directly (with its validation)
        my $u = $self->schema->resultset('User')->create($args->{set});
        $self->{users}->{$args->{as}} = $u if $args->{as};
        return { id => $u->id };
    }
    elsif ($op eq 'set_groups')
    {
        # As $user->groups($logged_in_user, [...])
        # (save: name keeps the user's current groups, which restore: name
        # sets again)
        my $user = $self->_user($args->{user});
        $self->{saved_groups}->{$args->{save}} = [ map $_->id, $user->groups ]
            if $args->{save};
        my $groups = $args->{restore}
            ? $self->{saved_groups}->{$args->{restore}} || error("Unknown saved groups $args->{restore}")
            : [ map $self->_group_id($_), @{$args->{groups}} ];
        $user->groups($self->_user($args->{by} // 'superadmin'), $groups);
        return {};
    }
    elsif ($op eq 'groups_viewable')
    {
        my $user = $self->_user($args->{user});
        my $re = $args->{match};
        return [ sort grep { !defined $re || /$re/ } map $_->name, $user->groups_viewable ];
    }
    elsif ($op eq 'remove_from_group')
    {
        my $gid = $args->{group} eq 'default' ? $sheet->group->id : $self->_group_id($args->{group});
        $self->schema->resultset('UserGroup')->search({
            user_id  => $self->_user($args->{user})->id,
            group_id => $gid,
        })->delete;
        return {};
    }
    error "Unknown user op $op";
}

# Direct database manipulation (as tests do with $schema->resultset)
# Resolve references to runner objects in values for db steps
sub _db_ref
{   my ($self, $v) = @_;
    return $v if !defined $v || ref $v;
    return $self->_user($1)->id if $v =~ /^\@user:(.+)$/;
    return $self->{records}->{$1} // error("Unknown record alias $1") if $v =~ /^\@record:(.+)$/;
    return ($1 eq 'default' ? $self->_first_sheet->group->id : $self->_group_id($1)) if $v =~ /^\@group:(.+)$/;
    return $self->_col(undef, $1)->id if $v =~ /^\@column:(.+)$/;
    return $self->_sheet($1)->layout->instance_id if $v =~ /^\@instance:(.+)$/;
    return $self->{views}->{$1} // error "Unknown view $1" if $v =~ /^\@view:(.+)$/;
    $v;
}

sub _db_refs
{   my ($self, $h) = @_;
    return $h if !$h;
    +{ map { $_ => $self->_db_ref($h->{$_}) } keys %$h };
}

sub step_db
{   my ($self, $args) = @_;
    my $op = $args->{op} || 'update';
    $args = { %$args, where => $self->_db_refs($args->{where}), set => $self->_db_refs($args->{set}) };
    my $rs = $self->schema->resultset(_camel($args->{table}))->search($args->{where} || {});
    if ($op eq 'update')
    {
        my $set = { %{$args->{set}} };
        foreach my $k (keys %$set)
        {
            $set->{$k} = DateTime->now if defined $set->{$k} && $set->{$k} eq '@now';
        }
        $rs->update($set);
        $_->layout->clear foreach $args->{clear_layouts} ? values %{$self->{sheets}} : ();
    }
    elsif ($op eq 'count')
    {
        return { count => $rs->count };
    }
    elsif ($op eq 'max')
    {
        return { max => $rs->get_column($args->{column})->max };
    }
    elsif ($op eq 'delete')
    {
        $rs->delete;
    }
    elsif ($op eq 'insert')
    {
        my $row = $self->schema->resultset(_camel($args->{table}))->create($args->{set});
        return { id => $row->id };
    }
    elsif ($op eq 'select')
    {
        my @cols = @{$args->{columns}};
        # Columns whose value is reported only as whether it matches a regex
        # (for values such as JSON with an unpredictable key order)
        my $match = $args->{columns_match} || {};
        return [ map {
            my $r = $_;
            +{ map {
                my $v = $r->{$_};
                $_ => exists $match->{$_} ? (defined $v && $v =~ /$match->{$_}/ ? 1 : 0) : $v
            } @cols }
        } $rs->search({}, {
            # Tables without an id column name their own order
            order_by     => [ map "me.$_", @{$args->{order_by} || ['id']} ],
            result_class => 'DBIx::Class::ResultClass::HashRefInflator',
        })->all ];
    }
    return {};
}

sub _camel
{   my $t = shift;
    # Tables whose result class name differs from the table name
    my %source = (curval_fields => 'CurvalField');
    return $source{$t} if $source{$t};
    join '', map ucfirst, split /_/, $t;
}

sub step_set_multivalue
{   my ($self, $args) = @_;
    my $sheet = $self->_sheet($args->{sheet});
    $sheet->set_multivalue($args->{value} ? 1 : 0);
    $_->layout->clear foreach values %{$self->{sheets}};
    return {};
}

# Evaluate Lua code directly using the GADS evaluator
sub step_lua
{   my ($self, $args) = @_;
    my $layout = $self->_sheet($args->{sheet})->layout;
    my $col = GADS::Column::Calc->new(
        schema => $self->schema,
        user   => undef,
        layout => $layout,
        return_type => $args->{return_type} || 'string',
        multivalue  => $args->{multivalue} ? 1 : 0,
    );
    my $vars = $args->{vars} || {};
    my $ret = $col->eval($args->{code}, $vars);
    return {
        return => _plain($ret->{return}),
        error  => _normalise_error($ret->{error}),
    };
}

1;
