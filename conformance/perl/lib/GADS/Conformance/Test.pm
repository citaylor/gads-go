package GADS::Conformance::Test;

# Runs one conformance scenario as a standalone TAP test inside a GADS
# checkout (the t/conformance bundle made by export_perl_tests.sh):
#
#   prove -r t/conformance            (from the GADS root)
#
# Each step is a test: its own expectations must pass, and its observation
# must equal the one recorded from the reference run (golden/NAME.json).
# Set GADS_CONFORMANCE_NO_GOLDEN=1 to check only the expectations.

use strict;
use warnings;

use File::Basename qw(dirname);
use Cwd qw(abs_path);

my ($here, $root);
BEGIN {
    # lib/GADS/Conformance/Test.pm → t/conformance → GADS root
    $here = abs_path(dirname(__FILE__) . '/../../..');
    $root = abs_path("$here/../..");
    unshift @INC, "$root/lib", "$root/t/lib", "$here/lib";
    $ENV{GADS_NO_FORK} = 1;
}

# Modules the runner needs beyond those of GADS itself
my @missing;
BEGIN {
    @missing = grep { !eval "require $_; 1" } qw(YAML::XS Test::MockTime);
}

use Test::More;

BEGIN {
    if (@missing)
    {
        plan skip_all => "Conformance tests need @missing";
    }
    else {
        # Before GADS and DateTime are loaded (frozen clock, canonical JSON,
        # predictable tokens)
        require GADS::Conformance::Patches;
        require GADS::Conformance::Runner;
    }
}

use IO::Handle;
use JSON ();
use POSIX ();

sub run
{   my $name = shift;
    my $file = "$here/scenarios/$name.yaml";
    my $golden_file = "$here/golden/$name.json";
    my $json = JSON->new->canonical->utf8;

    my $runner = GADS::Conformance::Runner->new(file => $file, gads => $root);
    my $result = eval { $runner->run };
    if (!$result)
    {
        fail "$name ran";
        diag "Fatal: $@";
        done_testing;
        _exit();
    }
    # The observation as recorded (plain JSON strings, arrays and hashes)
    $result = $json->decode(_no_addresses($json->encode($result)));

    my $golden;
    unless ($ENV{GADS_CONFORMANCE_NO_GOLDEN})
    {
        open my $fh, '<:raw', $golden_file or BAIL_OUT "Unable to read $golden_file: $!";
        $golden = $json->decode(_no_addresses(do { local $/; <$fh> }));
    }

    my @steps = @{$result->{steps}};
    foreach my $i (0..$#steps)
    {
        my $step = $steps[$i];
        my $id   = $step->{id};
        # (Test::More warns about names that are only numbers)
        subtest $id =~ /^\d+$/ ? "step $id" : $id => sub {
            ok $step->{pass}, "expectations"
                or diag join "\n", @{$step->{failures} || []};
            if ($golden)
            {
                my $want = $golden->{steps}[$i];
                is_deeply $step, $want, "same as recorded"
                    or diag "Observed: " . substr($json->encode($step->{observed} // $step->{error}), 0, 2000);
            }
        };
    }
    is scalar @steps, scalar @{$golden->{steps}}, "number of steps"
        if $golden;

    done_testing;
    _exit();
}

# Stringified references (a HASH(0x...) in a value) differ between runs
sub _no_addresses
{   my $text = shift;
    $text =~ s/\b(HASH|ARRAY|CODE|SCALAR|GLOB|REF)\(0x[0-9a-f]+\)/$1(0x)/g;
    $text;
}

# Exit with Test::More's status, skipping global destruction (where some
# objects of the web scenarios die), as the suite's runner does
sub _exit
{   my $tb = Test::More->builder;
    my $failed = grep !$_, $tb->summary;
    $failed = 1 if !$tb->is_passing && !$failed;
    GADS::Conformance::Runner::pg_cleanup();
    STDOUT->flush; STDERR->flush;
    POSIX::_exit($failed > 254 ? 254 : $failed);
}

1;
