#!/usr/bin/env perl
# Generates the golden cases of the Go Lua bridge (gads-go/internal/lua)
# from GADS's own lua_run (GADS::Column::Code, Inline::Lua): each case is
# Lua code and its vars, and the result is what lua_run returns to Perl,
# with the type of each value (an integer, a float or a string).
#
#   perl gen_lua_golden.pl --gads ../../upstream/GADS-dev \
#       > ../../gads-go/internal/lua/testdata/golden.json
#
# Values use the typed form of gen_json_golden.pl: {"f": bits}, {"i": ...},
# {"s": ...}, {"n": null}, {"a": [...]} and {"h": {...}}.
use strict;
use warnings;
use utf8;

BEGIN {
    require FindBin;
    unshift @INC, "$FindBin::Bin/lib";
    require GADS::Conformance::Patches;
}

use B ();
use Getopt::Long;
use JSON::PP;

my $gads;
GetOptions('gads=s' => \$gads) && $gads or die "usage: $0 --gads DIR\n";
unshift @INC, "$gads/lib";
require GADS::Layout; # (loads the columns, GADS::Column::Code among them, in order)
require GADS::Column::Code;

sub tf { { f => unpack 'H16', pack 'd>', $_[0] } }
sub ti { { i => "$_[0]" } }
sub ts { { s => $_[0] } }

# Lua code, as GADS::Column::Code::eval gives it: the vars, then the body
my @cases = (
    # Numbers: integers and floats of Lua 5.3, and their conversions
    [ 'return 1' ], [ 'return -7' ], [ 'return 1.5' ], [ 'return 1 / 3' ],
    [ 'return 2^53' ], [ 'return 2^63' ], [ 'return 10 // 3' ], [ 'return 7 / 7' ],
    [ 'return 3.0' ], [ 'return -0.0' ], [ 'return math.huge' ], [ 'return -math.huge' ],
    [ 'return 0/0' ], [ 'return math.maxinteger' ], [ 'return math.mininteger' ],
    [ 'return 1e15' ], [ 'return 1e16' ], [ 'return 123456789012345678' ],
    [ 'return 0.1 + 0.2' ], [ 'return 100 * 1.1' ], [ 'return math.floor(2.7)' ],
    [ 'return math.pi' ], [ 'return tostring(1e15)' ], [ 'return tostring(0.1)' ],
    [ 'return tostring(3) .. "|" .. tostring(3.0) .. "|" .. tostring(-0.0)' ],
    [ 'return 1 .. ""' ], [ 'return 1.5 .. ""' ], [ 'return 2^63 .. ""' ],
    [ 'return string.format("%d|%5.2f|%g|%s", 3, 1.005, 1e20, 0.1)' ],
    [ 'return "10" + 5' ], [ 'return "3.5" * 2' ], [ 'return "0x10" + 0' ], [ 'return math.tointeger(3.0)' ],
    # Strings and booleans
    [ 'return "text"' ], [ 'return ""' ], [ 'return "é日本"' ], [ "return \"a\\0b\"" ],
    [ 'return true' ], [ 'return false' ], [ 'return nil' ], [ 'return' ],
    [ 'return string.upper("abc") .. string.rep("x", 3)' ],
    # Tables: arrays, holes, hashes, mixed keys, number keys, nesting
    [ 'return {}' ], [ 'return {1, 2, 3}' ], [ 'return {"a", "b"}' ], [ 'return {[1] = "a", [3] = "c"}' ],
    [ 'return {[2] = "b", [1] = "a"}' ], [ 'return {[0] = "z", [1] = "a"}' ], [ 'return {a = 1, b = "x"}' ],
    # (Inline::Lua makes an array up to the largest number key: a key of
    # 2^53 runs Perl out of memory)
    [ 'return {1, 2, a = 3}' ], [ 'return {[1.5] = "x"}' ], [ 'return {[0.1] = "y", [2.5] = "z"}' ],
    [ 'return {[1000] = "m"}' ], [ 'return {[-1] = "neg", [1] = "a"}' ], [ 'return {[1/3] = "third"}' ],
    [ 'return {[true] = "t"}' ],
    # (A table that mixes number keys with others depends on the order of
    # its keys, which Lua 5.3 randomises with the seed of its string hashes:
    # number keys before the first other key are made array indices.)
    [ 'return {a = {b = {c = 1}}, l = {1, {2, 3}}}' ], [ 'return {nil, 2}' ], [ 'return {1, nil, 3}' ],
    [ 'return {x = 1.5, y = 2^63, z = -0.0}' ], [ 'return {f = function() end}' ],
    [ 'local t = {} for i = 1, 5 do t[i] = i * i end return t' ],
    # Functions and other values
    [ 'return print' ], [ 'return function() return 1 end' ],
    # Errors
    [ 'return nil + 1' ], [ 'return (' ], [ 'error("custom")' ], [ 'error({code = 1})' ],
    [ 'return f(1)' ], [ 'local x = {} return x.y.z' ], [ 'return #nil' ], [ 'error("with level", 0)' ],
    [ 'return string.rep("x", -1)' ], [ 'return math.floor("a")' ],
    # Vars from Perl: integers, floats, strings, tables
    [ 'v = vars["v"] return v', { v => ti(42) } ],
    [ 'v = vars["v"] return math.type(v)', { v => ti(42) } ],
    [ 'v = vars["v"] return math.type(v)', { v => tf(42) } ],
    [ 'v = vars["v"] return v * 2', { v => tf(1.25) } ],
    [ 'v = vars["v"] return v // 1', { v => ti(7) } ],
    [ 'v = vars["v"] return v .. "!"', { v => ts('text') } ],
    [ 'v = vars["v"] return type(v)', { v => ts('12') } ],
    [ 'v = vars["v"] return v + 1', { v => ts('12') } ],
    [ 'v = vars["v"] return v', { v => { n => undef } } ],
    [ 'v = vars["v"] return type(v)', { v => { n => undef } } ],
    [ 'v = vars["v"] return #v', { v => { a => [ ti(1), ti(2), ti(3) ] } } ],
    [ 'v = vars["v"] return v[1] .. v[2]', { v => { a => [ ts('a'), ts('b') ] } } ],
    [ 'v = vars["v"] return v.year .. "-" .. v.month', { v => { h => { year => ti(2014), month => ti(10) } } } ],
    [ 'v = vars["v"] return v', { v => { h => { a => { a => [ tf(0.5), ts('x') ] } } } } ],
    [ 'v = vars["v"] local n = 0 for k, x in pairs(v) do n = n + 1 end return n', { v => { h => { a => ti(1), b => ti(2) } } } ],
    [ 'v = vars["v"] return v', { v => tf(1e100) } ],
    [ 'v = vars["v"] return math.type(v)', { v => ti('9223372036854775807') } ],
);

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

# The typed form of a value returned to Perl
sub typed {
    my $v = shift;
    return { n => undef } if !defined $v;
    my $ref = ref $v;
    return { h => { map { $_ => typed($v->{$_}) } keys %$v } } if $ref eq 'HASH';
    return { a => [ map typed($_), @$v ] } if $ref eq 'ARRAY';
    return { bool => $$v ? 1 : 0 } if $ref eq 'Inline::Lua::Boolean';
    return { ref => $ref } if $ref;
    my $flags = B::svref_2object(\$v)->FLAGS;
    # Lua strings are bytes (no UTF-8 flag), given as Latin-1 characters
    return { s => "$v", (utf8::is_utf8($v) ? (utf8 => 1) : ()) } if $flags & B::SVf_POK;
    return { f => unpack 'H16', pack 'd>', $v } if $flags & B::SVf_NOK;
    return { i => "$v" } if $flags & B::SVf_IOK;
    return { s => "$v" };
}

# Each case runs in a child process with its own copy of the Lua state:
# after some errors (a boolean table key) Inline::Lua's state is broken, and
# every later call fails with the same error
my $json = JSON::PP->new->canonical->utf8;
my @out;
for my $case (@cases) {
    my ($code, $vars) = @$case;
    pipe my $r, my $w or die "pipe: $!";
    my $pid = fork // die "fork: $!";
    if (!$pid) {
        close $r;
        my %vars = map { $_ => build($vars->{$_}) } keys %{ $vars || {} };
        my $noop = sub { 0 };
        # An error outside the code's pcall dies (GADS catches it with try)
        my $ret = eval { GADS::Column::Code::lua_run($code, \%vars, $noop, $noop) };
        print $w $json->encode($@ ? { die => "$@" } : { result => typed($ret) });
        close $w;
        require POSIX;
        POSIX::_exit(0);
    }
    close $w;
    my $res = do { local $/; <$r> };
    waitpid $pid, 0;
    push @out, { code => $code, vars => $vars || {}, %{ $json->decode($res) } };
}

print JSON::PP->new->canonical->pretty->utf8->encode({
    _generator => 'conformance/perl/gen_lua_golden.pl',
    cases      => \@out,
});
