#!/usr/bin/env perl
# Generates the golden results of the Go Template Toolkit engine
# (gads-go/internal/tt) from Template Toolkit itself: each line of
# testdata/cases.txt is a template, rendered with Template's defaults (as
# the Go engine's) and no variables.
#
#   perl gen_tt_golden.pl ../../gads-go/internal/tt/testdata/cases.txt \
#       > ../../gads-go/internal/tt/testdata/golden.json
#
# An error is given by its type: its message has Perl's file and line.
use strict;
use warnings;
use Template;
use JSON::PP;

open my $fh, '<:encoding(UTF-8)', $ARGV[0] or die "$ARGV[0]: $!";
my @cases;
while (my $template = <$fh>) {
    chomp $template;
    my $tt = Template->new({ ENCODING => 'utf8' });
    my $out = '';
    if ($tt->process(\$template, {}, \$out)) {
        push @cases, { template => $template, out => $out };
    }
    else {
        push @cases, { template => $template, error => $tt->error->type };
    }
}

print JSON::PP->new->canonical->pretty->utf8->encode({
    _generator => 'conformance/perl/gen_tt_golden.pl',
    template   => $Template::VERSION,
    cases      => \@cases,
});
