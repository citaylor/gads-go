#!/usr/bin/env perl
# Generates the golden cases of the Go YAML writer that reproduces YAML::XS
# Dump (gads-go/internal/yamlxs), as used by GADS's move-instance dump.
#
#   perl gen_yaml_golden.pl > ../../gads-go/internal/yamlxs/testdata/golden.json
#
# Values are given in the typed form of gen_json_golden.pl. Each one is
# dumped alone and in collections, as the indentation and the folding of
# long lines depend on where it is.
use strict;
use warnings;
use utf8;
use Encode ();
use JSON::PP;
use YAML::XS ();

sub tf { { f => unpack 'H16', pack 'd>', $_[0] } }
sub ti { { i => "$_[0]" } }
sub ts { { s => $_[0] } }

my $long = 'Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua';
my $code = "function evaluate (L1integer1)\n    if L1integer1 > 10 then\n        return 'big'\n    end\n    return 'small'\nend";

my @strings = (
    # Plain, or quoted as another type
    '', ' ', '~', 'null', 'Null', 'NULL', 'true', 'True', 'false', 'yes', 'No',
    'on', 'OFF', 'y', 'n', '1', '-1', '+1', ' 1', '1 ', '1.5', '.5', '1e3',
    '0x1F', '0o17', '017', '1_000', 'Inf', '-inf', 'nan', '.inf', '.nan',
    '2026-11-04', '12:30', 'plain text', 'it\'s', 'x', 'a1b2',
    # Indicators
    'a: b', 'a:b', 'a :b', 'x:', ':', ': x', ':x', '?', '? x', '?x', '-', '- x',
    '-x', '--', '---', '--- x', '...', '....', 'a #b', 'a#b', '#c', '@at',
    '`tick', '!tag', '&anchor', '*alias', '|', '>', '%pct', '{a}', '[a]',
    'a,b', 'a[b]', 'a{b}', '"dq"', "'sq'", 'a"b', 'tab	here', 'foo bar: baz',
    '<<', '=', ' lead', 'trail ', '  two  ',
    # Characters: control, DEL, C1, NBSP, BOM, separators, other scripts,
    # beyond the BMP
    "\x00", "\x07bell", "\x08\x0b\x0c\x1b", "\x1f", "x\x7f", "\x{80}\x{85}\x{9f}",
    "a\x{a0}b", "\x{a0}", "\x{feff}bom", "\x{fffe}", "\x{2028}ls", "x\x{2029}",
    'é', 'ÉCOLE', '日本語', "\x{1f600}", "a\x{10000}", "\x{e000}\x{fffd}",
    "x\r", "\ry",
    # Line breaks: short (double quoted) and long (literal blocks)
    "multi\nline", "a\n", "\n", "\nlead", "a\n\n", "a\nb\n", "a \nb", "a\n b",
    "  indented\nlines", $code, "$code\n", "$code\n\n", "\n$code", "trailing space \n$code",
    "line one is here\nline two is here\n\n\nline five", "tab\there\nand a second line of text",
    "x" x 31 . "\n", "emoji \x{1f600}\nand a second line of text",
    # Folding at 80 columns
    $long, "$long $long", "$long\x{a0}x", "'$long'", ": $long", "$long  double  spaces",
    'x' x 100, ('word ' x 30) . 'end', "\x{1f600} $long",
);

my @values = (
    (map { ts($_) } @strings),
    ti(0), ti(42), ti(-7), ti('9223372036854775807'),
    tf(0.5), tf(1 / 3), tf(1e15), tf(1e-7), tf(-2.5), tf(100.0),
    { n => undef }, { a => [] }, { h => {} },
);

# Keys: plain, quoted, long (complex keys beyond 128 characters) and
# multi-line
my @keys = ('key', 'a: b', '', '1', ' sp', "multi\nline key", 'k' x 129, 'é');

sub build {
    my $c = shift;
    my ($t) = keys %$c;
    my $v = $c->{$t};
    return unpack 'd>', pack 'H16', $v if $t eq 'f';
    return 0 + $v if $t eq 'i';
    return "$v" if $t eq 's';
    return undef if $t eq 'n';
    return [ map build($_), @$v ] if $t eq 'a';
    return { map { $_ => build($v->{$_}) } keys %$v };
}

sub dump_yaml { Encode::decode_utf8(YAML::XS::Dump(@_)) }

my @cases;
for my $v (@values) {
    push @cases, map { { in => $_, out => dump_yaml(build($_)) } } (
        $v,
        { a => [ $v, ts('next') ] },
        { h => { key => $v, other => ts('x') } },
        { h => { list => { a => [ $v ] } } },
        { a => [ { h => { key => $v } }, { a => [ $v ] } ] },
    );
}
for my $k (@keys) {
    push @cases, map { { in => $_, out => dump_yaml(build($_)) } } (
        { h => { $k => ts('value') } },
        { h => { $k => { a => [ ti(1), ti(2) ] } } },
        { a => [ { h => { $k => { h => { inner => ts($long) } } } } ] },
    );
}

print JSON::PP->new->canonical->pretty->utf8->encode({
    _generator => 'conformance/perl/gen_yaml_golden.pl',
    yaml_xs    => $YAML::XS::VERSION,
    libyaml    => YAML::XS::LibYAML::libyaml_version(),
    cases      => \@cases,
});
