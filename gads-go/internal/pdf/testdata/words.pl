#!/usr/bin/perl
# The words of pdftotext -bbox output, one per line: page, box and word
use strict;
use warnings;
my $page = 0;
while (<STDIN>)
{
    $page++ if /<page /;
    printf "%d %.2f %.2f %.2f %.2f %s\n", $page, $1, $2, $3, $4, $5
        if /<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*)<\/word>/;
}
