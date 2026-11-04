#!/usr/bin/env perl
# Generates the golden cases of the Go JSON encoders that reproduce
# JSON::PP: gads.EncodeJSON (JSON::PP->new->canonical, used for pages and
# the API) and perljson.Pretty (->canonical->pretty, used by export).
#
# JSON::PP is the encoder of the golden results (no JSON::XS).
#
#   perl gen_json_golden.pl > ../../gads-go/internal/perljson/testdata/golden.json
#
# Each case gives its value in a typed form that both languages build in
# the same way: {"f": "<IEEE 754 bits>"} a float, {"i": "<digits>"} an
# integer, {"s": "..."} a string, {"b": 0|1} a boolean, {"n": null},
# {"a": [...]} an array and {"h": {...}} a hash.
use strict;
use warnings;
use utf8;
use JSON::PP;

sub tf { { f => unpack 'H16', pack 'd>', $_[0] } }
sub ti { { i => "$_[0]" } }
sub ts { { s => $_[0] } }

my $negative_zero = unpack 'd>', pack 'H16', '8000000000000000';

my @floats = (
    0.5, -0.5, 0.1 + 0.2, 1 / 3, -2 / 3, 1 / 7, 33.333333333333336,
    66.66666666666667, 1.1 * 1.1, 0.07 * 100, 255 / 256, 2.5, 1e14 + 0.5,
    1e15, 1e21, 1e100, 1.7976931348623157e308, 1e-4, 1e-5, 1e-7,
    1.5e-7, 5e-324, 123.456, 1e15 / 3, 3.0, 100.0, -0.0, $negative_zero,
    12345678.9, 0.000123456789012345678,
    # Whole floats from 2**53 to 2**64 are left out: JSON::PP 4.06
    # (without B) takes a value as a number when 0 + $value eq $value, and
    # for these the result depends on the values encoded before (the same
    # value is a number after some and a string after others). Go writes a
    # number.
    2**52, 2**53 - 1, -(2**53 - 1),
);

my @ints = (0, 1, -1, 42, 9007199254740993, '9223372036854775807', '-9223372036854775808');

# Escapes: the control characters, quote, backslash and slash, DEL, the
# line and paragraph separators (escaped by Go's encoding/json), HTML
# characters, and characters beyond the BMP
my @strings = (
    '', 'plain', 'quote " backslash \\ slash /', "\b\f\n\r\t", "\x00\x01\x1f",
    "\x7f", "\x{80}\x{9f}", "\x{a0}", "\x{2028}\x{2029}", '</script>&<>',
    "é日本\x{1f600}", "\x{fffd}\x{feff}", '1.5', '0',
);

my @cases = (
    (map { tf($_) } @floats),
    (map { ti($_) } @ints),
    (map { ts($_) } @strings),
    { b => 1 }, { b => 0 }, { n => undef }, { a => [] }, { h => {} },
    { a => [ tf(0.1 + 0.2), ti(1), ts("x\x{2028}"), { n => undef }, { a => [ tf(1 / 3) ] } ] },
    { h => { b => ti(2), a => tf(1 / 3), B => ts('upper'), 'é' => ti(3), 10 => ti(10), 9 => ti(9), '' => ts('empty key'), "k\x{2028}" => tf(1e15) } },
    { h => { nested => { h => { z => { a => [ tf(2 / 3), { h => { y => { n => undef } } } ] } } } } },
);

sub build {
    my $c = shift;
    my ($t) = keys %$c;
    my $v = $c->{$t};
    return unpack 'd>', pack 'H16', $v if $t eq 'f';
    return 0 + $v if $t eq 'i';
    return "$v" if $t eq 's';
    return $v ? JSON::PP::true : JSON::PP::false if $t eq 'b';
    return undef if $t eq 'n';
    return [ map build($_), @$v ] if $t eq 'a';
    return { map { $_ => build($v->{$_}) } keys %$v };
}

my $compact = JSON::PP->new->canonical->allow_nonref;
my $pretty  = JSON::PP->new->canonical->pretty->allow_nonref;

my @out = map { { in => $_, compact => $compact->encode(build($_)), pretty => $pretty->encode(build($_)) } } @cases;

print JSON::PP->new->canonical->pretty->utf8->encode({
    _generator => 'conformance/perl/gen_json_golden.pl',
    json_pp    => $JSON::PP::VERSION,
    cases      => \@out,
});
