#!/usr/bin/env perl
# Generates the golden cases of the Go Text::CSV getline
# (gads-go/internal/textcsv): the rows that Text::CSV->new({ binary => 1 })
# reads with getline from each input until it returns undef, as GADS's CSV
# imports read (GADS::Import, the user upload, bin/import-user.pl), and the
# error code (error_diag) that stopped it, 0 at the end of the input.
#
#   perl gen_csv_golden.pl > ../../gads-go/internal/textcsv/testdata/golden.json
#
# The cases are written ones, then random strings of the characters that
# matter to the parser (srand 1: the same each time).
use strict;
use warnings;
use JSON::PP;
use Text::CSV;

my @cases = (
    "", "a", "a\n", "a,b\n\nc,d\n", "a,b\r\n\r\nc,d\r\n", "a\"b,c\nd,e\n",
    "\"a\"b,c\nd,e\n", "\"a,b\nc,d\n", "a, \"b\" ,c\n", "\"a\" ,b\n", "a,b",
    "a,\"b\nc\",d\n", " a , b \n", "a,,b,\n", "\n\na\n", "\"\"\n", "a\rb,c\n",
    "a,b\n\"x\"\"y\",z\n", "a\tb,\"c\"d\"\n", "\"a\"\r\nb\n", "\"a\"\rb\n",
    "\"a\rb\",c\n", "a\r\n", "a\r", "\r\n", "\r", "\"a\"\"\"\n", "\"\"\"\"\n",
    ",\n", ",,\n", "\"a\",\"b\"\n", "caf\x{e9},na\x{ef}ve\n", "x\x00y,z\n",
    "\"x\x00y\",z\n", "a,b\n,\nc\n", "\"a\nb\nc\"\n", "a\n\"b",
    "sep=;\na;b,c\n", "SEP=|\na|b\n", "sep=;\r\na;b\r\n", "x\nsep=;\na;b\n",
    "\"a\"0b\"\n", "a\x{7f}b\n",
);
srand 1;
my @chars = ('a', 'b', ',', '"', "\n", "\r", ' ', "\t");
for (1 .. 5000) {
    my $len = int rand 14;
    push @cases, join '', map { $chars[int rand @chars] } 1 .. $len;
}
# Longer, with fewer specials
my @longer = (('a') x 6, 'b', ',', ',', '"', '"', "\n", "\r", ' ', "\t", "\x{e9}");
for (1 .. 3000) {
    my $len = int rand 40;
    push @cases, join '', map { $longer[int rand @longer] } 1 .. $len;
}

my @out;
for my $in (@cases) {
    my $csv = Text::CSV->new({ binary => 1 });
    open my $fh, '<', \$in or die;
    my @rows;
    while (my $row = $csv->getline($fh)) {
        push @rows, $row;
        last if @rows > 100;
    }
    my $code = 0 + $csv->error_diag;
    $code = 0 if $code == 2012; # EOF
    push @out, { in => $in, rows => \@rows, error => $code };
}
print JSON::PP->new->canonical->ascii->encode({ backend => Text::CSV->backend . ' ' . Text::CSV->backend->VERSION, cases => \@out }), "\n";
