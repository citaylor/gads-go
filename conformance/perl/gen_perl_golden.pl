#!/usr/bin/env perl
# Generates the golden cases of the Go package that reproduces Perl's
# scalar semantics (gads-go/internal/perl): number stringification and
# numification, looks_like_number, truthiness, lc, substr, int,
# sprintf("%.Nf") and Math::Round round.
#
#   perl gen_perl_golden.pl > ../../gads-go/internal/perl/testdata/golden.json
#
# Floating point inputs are given by their IEEE 754 bits (hex), so that
# both languages start from exactly the same number.
use strict;
use warnings;
no warnings qw(numeric);
use utf8;
use B ();
use JSON::PP;
use Math::Round ();
use Scalar::Util qw(looks_like_number);

sub bits { unpack 'H16', pack 'd>', $_[0] }

# A float (NV) for each value: whole-number literals are integers (IV) in
# Perl, and -0.0 is folded to the integer 0
sub nvs { map { unpack 'd>', pack 'd>', $_ } @_ }
my $negative_zero = unpack 'd>', pack 'H16', '8000000000000000';

# Floats around the edges of %.15g: exact and inexact decimals, the
# switch to exponent notation, rounding at the 15th digit, signed zero,
# subnormals and the largest values
my @floats = nvs(
    0, $negative_zero, 1, -1, 0.1, 0.2, 0.1 + 0.2, 0.3, 1 / 3, 2 / 3, -1 / 3, 1 / 7,
    0.5, 1.5, 2.5, -2.5, 0.125, 0.375, 1.005, 2.675, 1.0000000000000002,
    0.49999999999999994, 123.456, 3.14159265358979, 2.718281828459045,
    1e14, 1e15 - 1, 1e15, 1e15 + 0.3, 1e16, 1e17, 123456789012345678,
    999999999999999.4, 999999999999999.5, 9999999999999998, 1e21, 1e100,
    1.7976931348623157e308, 1e-4, 1e-5, 0.0001234, 0.00001234, 1e-7,
    1.5e-7, 5e-324, 2.2250738585072014e-308, 1e300 * 10, -1e300 * 10,
    33.333333333333336, 66.66666666666667, 100 / 3 * 3, 0.07 * 100,
    1.1 * 1.1, 255 / 256, 1 - 1e-16, 4.35, 4.345, 1e15 / 3, -1e-300,
);

# Strings in numeric context: leading and trailing spaces, signs, partial
# numbers, exponents, hex and binary forms (not numbers in Perl), infinity
# and NaN spellings, Unicode digits and Perl's "0 but true"
my @strings = (
    '', ' ', '0', '1', '-1', '+1', ' 1', '1 ', "1\n", "\t1\t", "1\n\n",
    '01', '007', '1.', '.5', '5.', '-.5', '+.5e-3', '1e5', '1E5', '1e+5',
    '1e-5', '1e', '1e+', '1.5e', '1.2.3', '12abc', ' 3.5x', 'abc', '.',
    '+', '-', '--1', '+-1', '0x10', '0X1A', '0b101', '0o17', '1_000',
    '1,000', 'Inf', 'inf', '-inf', '+Inf', 'Infinity', '-Infinity',
    'infinity', 'infx', 'Info', 'in', 'NaN', 'nan', '-nan', 'nanx', 'nan1',
    '0 but true', '0 but false', '0.0', '00', '-0', '  -0', '-0.0', '+0',
    '1e400', '-1e400', '1e-400', '9223372036854775807',
    '9223372036854775808', '-9223372036854775808', '-9223372036854775809',
    '18446744073709551615', '18446744073709551616', '12345678901234567890',
    '0.1', '0.30000000000000004', '3.0', '2.50', '1e15', '1e16',
    '١٢', '٣', "\x{ff11}", "\x{a0}1", "1\x{a0}", '१', ' +3 ', '- 3',
);

# Strings for lc and substr: ASCII, accented and other scripts, letters
# whose lower case is longer (İ), title case, final sigma, astral
# characters and combining marks
my @texts = (
    '', 'ABC', 'aBc', 'Hello World', 'ÉCOLE', 'café', 'ÀÉÎÕÜ', 'ẞ', 'ß',
    "\x{130}stanbul", 'İ', 'ǅ', 'ǈ', 'ΣΑΣ', 'ΌΣΟΣ', 'Привет', 'ÆØÅ',
    'ＡＢＣ', '𝐀𝐁', '𐐀', 'Ⅻ', 'Ⓐ', "e\x{301}", 'Ǆ', 'ﬃ', 'K', 'Ω', 'Å',
    'x' x 130, ('é' x 129) . 'X', '日本語のテキスト',
);

my @round = nvs(
    0, 0.5, 1.5, 2.5, -0.5, -1.5, -2.5, 0.49999999999999994, 1.4999999999999998,
    2.4999999999999996, 0.49999999999992, 0.4999999999999, 2.49999999999993,
    -0.49999999999992, -2.49999999999993, 1e15 + 0.5, -1e15 - 0.5, 123.456, -123.456, 0.1, 1e300,
);

my @fixed = (0, 1, 2, 3, 5);

sub is_int_scalar {
    my $f = B::svref_2object(\$_[0])->FLAGS;
    return ($f & B::SVf_IOK) && !($f & B::SVf_NOK) ? JSON::PP::true : JSON::PP::false;
}

# An unsigned integer (UV): beyond the range of a signed 64-bit integer
sub is_uv {
    my $f = B::svref_2object(\$_[0])->FLAGS;
    return ($f & B::SVf_IOK) && ($f & B::SVf_IVisUV) ? JSON::PP::true : JSON::PP::false;
}

my %out = (
    _generator => 'conformance/perl/gen_perl_golden.pl',
    perl       => sprintf('%vd', $^V),
);

# Stringification of a float ("$nv")
$out{num_string} = [ map { { bits => bits($_), out => "$_" } } @floats ];

# Numeric context (0 + $s) and looks_like_number
for my $s (@strings) {
    # A copy: numeric context caches the number in the scalar, which
    # JSON::PP would then encode as a number
    my $copy = $s;
    my $n = 0 + $copy;
    push @{ $out{numify} }, {
        in     => $s,
        out    => "$n",
        is_int => is_int_scalar($n),
        is_uv  => is_uv($n),
        lln    => looks_like_number($s) ? JSON::PP::true : JSON::PP::false,
        true   => $s ? JSON::PP::true : JSON::PP::false,
    };
}

# lc, and substr of the first 128 characters (the value_index of GADS)
$out{text} = [ map { { in => $_, lc => lc($_), substr128 => substr($_, 0, 128), substr3 => substr($_, 0, 3) } } @texts ];

# Math::Round round, int(), and sprintf with fixed decimal places
$out{round} = [ map { { bits => bits($_), round => '' . Math::Round::round($_), int => '' . int($_) } } @round ];
for my $f (@floats, @round) {
    next if $f != $f || abs($f) == 9**9**9;
    push @{ $out{fixed} }, { bits => bits($f), out => { map { $_ => sprintf("%.${_}f", $f) } @fixed } };
}

print JSON::PP->new->canonical->pretty->utf8->encode(\%out);
