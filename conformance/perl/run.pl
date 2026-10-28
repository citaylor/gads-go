#!/usr/bin/perl
# GADS conformance runner (reference implementation).
#
# Executes language-neutral YAML scenarios against the original Perl GADS
# code and writes JSON observation files. See ../SPEC.md.
#
# Usage: run.pl --gads /path/to/GADS --out results/perl scenarios/*.yaml

use strict;
use warnings;

BEGIN {
    # The patches making GADS deterministic (frozen clock, canonical JSON,
    # predictable tokens), before DateTime and GADS are loaded
    require FindBin;
    unshift @INC, "$FindBin::Bin/lib";
    require GADS::Conformance::Patches;
}

use Getopt::Long;
use FindBin;
use File::Basename qw(basename);
use File::Path qw(make_path);
use POSIX ();

my ($gads_dir, $out_dir, $verbose);
BEGIN {
    # Parse --gads early so that lib paths can be set before loading GADS
    for my $i (0..$#ARGV) {
        $gads_dir = $ARGV[$i+1] if $ARGV[$i] eq '--gads';
    }
    $gads_dir ||= "$FindBin::Bin/../../upstream/GADS";
    unshift @INC, "$gads_dir/lib", "$gads_dir/t/lib", "$FindBin::Bin/lib";
}

use GADS::Conformance::Runner;

GetOptions(
    'gads=s'   => \$gads_dir,
    'out=s'    => \$out_dir,
    'verbose!' => \$verbose,
) or die "Bad options";

$out_dir ||= "$FindBin::Bin/../results/perl";
make_path($out_dir);

my @files = @ARGV or die "No scenario files specified";
my ($total, $failed) = (0, 0);
foreach my $file (@files)
{
    # Run each scenario in its own process: GADS registers DBIC
    # relationships per column at class level, which must not leak between
    # scenarios with different databases
    pipe(my $r, my $w) or die "pipe: $!";
    my $pid = fork;
    die "fork: $!" unless defined $pid;
    if (!$pid)
    {
        close $r;
        my $runner = GADS::Conformance::Runner->new(
            file    => $file,
            verbose => $verbose,
            gads    => $gads_dir,
        );
        my $result = eval { $runner->run } || {
            scenario => basename($file) =~ s/\.ya?ml$//r,
            runner   => 'perl',
            fatal    => "$@",
            steps    => [],
        };
        my $name   = $result->{scenario};
        $runner->write_json("$out_dir/$name.json", $result);
        my @fails = grep { !$_->{pass} } @{$result->{steps}};
        my $out = '';
        if (@fails || $result->{fatal})
        {
            $out .= sprintf "FAIL %s (%d/%d steps failed)%s\n", $name, scalar @fails,
                scalar @{$result->{steps}}, $result->{fatal} ? " FATAL: $result->{fatal}" : "";
            foreach my $f (@fails)
            {
                $out .= "    $f->{id}: $_\n" foreach @{$f->{failures}};
            }
            print $w "1\n$out";
        }
        else {
            print $w sprintf "0\nok   %s (%d steps)\n", $name, scalar @{$result->{steps}};
        }
        close $w;
        # POSIX::_exit skips END blocks: drop the PostgreSQL databases
        GADS::Conformance::Runner::pg_cleanup();
        POSIX::_exit(0);
    }
    close $w;
    my $status = <$r>;
    my $text = do { local $/; <$r> } // '';
    close $r;
    waitpid($pid, 0);
    $total++;
    if (!defined $status)
    {
        $failed++;
        print "FAIL $file (runner crashed)\n";
        next;
    }
    chomp $status;
    $failed++ if $status;
    print $text;
}
printf "\n%d scenarios, %d failed\n", $total, $failed;
exit($failed ? 1 : 0);
