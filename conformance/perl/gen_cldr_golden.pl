#!/usr/bin/env perl
# Generates the golden cases of the Go CLDR package (gads-go/internal/cldr)
# from the Perl modules it ports: DateTime format_cldr (formatting) and
# DateTime::Format::CLDR (parsing, as GADS::DateTime::parse_datetime does),
# in the default en-US locale.
#
#   perl gen_cldr_golden.pl > ../../gads-go/internal/cldr/testdata/golden.json
use strict;
use warnings;
use DateTime;
use DateTime::Format::CLDR;
use JSON::PP;

# Dates around the edges of weeks, months and years (ISO week years,
# leap days, midnight and noon, the first days of a month on each weekday)
my @dates = (
    [2014, 10, 10, 12, 34, 56, 123456789],
    [2000,  1,  1,  0,  0,  0, 0],
    [2016,  2, 29, 23, 59, 59, 999000000],
    [1999, 12, 31, 12,  0,  0, 0],
    [2021,  1,  3,  0, 30,  5, 5000000],
    [2020, 12, 31, 11, 59,  1, 0],
    [2019, 12, 30, 13,  1,  2, 0],
    [2015,  3,  1,  9,  5,  7, 0],
    [2015,  8,  1, 21, 45,  0, 0],
    [2016,  5,  1, 18, 15, 30, 0],
    [2017,  9, 30,  1,  2,  3, 0],
    [  33,  6, 15,  6,  7,  8, 0],
    [   0,  1,  1,  0,  0,  0, 0],
    [ -44,  3, 15, 12,  0,  0, 0],
);

my @format_patterns = (
    (map { my $c = $_; map { $c x $_ } 1..5 }
        qw/G y Y u Q q M L w W d D F g E e c a h H K k j m s A z Z v V/),
    'S', 'SS', 'SSS', 'SSSSSS', 'SSSSSSSSS', 'SSSSSSSSSSSS',
    'dd', 'DDD', 'yyyy-MM-dd', 'yyyy-MM-dd HH:mm:ss', 'dd/MM/yyyy', 'MM/dd/yy',
    'EEEE dd MMMM yyyy HH:mm:ss', 'EEE, d MMM yyyy', 'h:mm a', "h 'o''clock' a",
    "yyyy-MM-dd'T'HH:mm:ss.SSS", "''", "'quoted text' yyyy", 'yyyy.MM.dd G',
    'QQQQ yyyy', "'week' w 'of' YYYY", 'x X b B', '[d]{M}(yy)', 'yyyy年M月d日',
);

my @format;
foreach my $d (@dates)
{
    my ($y, $mo, $da, $h, $mi, $s, $ns) = @$d;
    my $dt = DateTime->new(year => $y, month => $mo, day => $da, hour => $h,
        minute => $mi, second => $s, nanosecond => $ns, time_zone => 'UTC');
    my %out = map { $_ => $dt->format_cldr($_) } @format_patterns;
    push @format, { time => $d, out => \%out };
}

# [pattern, value]: the value parsed as GADS does, or undef where the parser
# returns undef (or the pattern is broken)
my @parse_cases = (
    ['yyyy-MM-dd', '2014-10-10'],
    ['yyyy-MM-dd', '2014-1-5'],
    ['yyyy-MM-dd', '  2014-10-10'],
    ['yyyy-MM-dd', '2014-10-10 '],
    ['yyyy-MM-dd', '2014-10-10x'],
    ['yyyy-MM-dd', '2014-13-10'],
    ['yyyy-MM-dd', '2014-02-30'],
    ['yyyy-MM-dd', '2016-02-29'],
    ['yyyy-MM-dd', '2015-02-29'],
    ['yyyy-MM-dd', '14-10-10'],
    ['yyyy-MM-dd', ''],
    ['yyyy-MM-dd', 'nonsense'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10 12:34:56'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10 24:00:00'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10 24:00:01'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10 23:60:00'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10 23:59:60'],
    ['yyyy-MM-dd HH:mm:ss', '2014-10-10   1:2:3'],
    ['dd/MM/yyyy', '31/12/1999'],
    ['dd/MM/yyyy', '31/11/1999'],
    ['MM/dd/yy', '12/31/99'],
    ['MM/dd/yy', '01/02/03'],
    ['MM/dd/yy', '01/02/69'],
    ['MM/dd/yy', '01/02/70'],
    ['d MMM yyyy', '5 Mar 2015'],
    ['d MMM yyyy', '5 mar 2015'],
    ['d MMM yyyy', '5 March 2015'],
    ['d MMMM yyyy', '5 March 2015'],
    ['d MMMM yyyy', '5 MARCH 2015'],
    ['d MMMMM yyyy', '5 M 2015'],
    ['d MMMMM yyyy', '5 J 2015'],
    ['EEEE d MMMM yyyy', 'Friday 10 October 2014'],
    ['EEEE d MMMM yyyy', 'Thursday 10 October 2014'],
    ['EEE d MMM yyyy', 'Fri 10 Oct 2014'],
    ['EEE d MMM yyyy', 'Sat 10 Oct 2014'],
    ['yyyy-MM-dd h:mm a', '2014-10-10 12:15 AM'],
    ['yyyy-MM-dd h:mm a', '2014-10-10 12:15 PM'],
    ['yyyy-MM-dd h:mm a', '2014-10-10 1:15 pm'],
    ['yyyy-MM-dd h:mm a', '2014-10-10 13:15 PM'],
    ['yyyy-MM-dd K:mm a', '2014-10-10 0:15 PM'],
    ['yyyy-MM-dd K:mm a', '2014-10-10 11:15 AM'],
    ['yyyy-MM-dd kk:mm', '2014-10-10 24:15'],
    ['yyyy-MM-dd kk:mm', '2014-10-10 01:15'],
    ['yyyy-MM-dd HH:mm', '2014-10-10 00:15'],
    ["yyyy-MM-dd'T'HH:mm:ss.SSS", '2014-10-10T12:34:56.789'],
    ["yyyy-MM-dd'T'HH:mm:ss.S", '2014-10-10T12:34:56.5'],
    ["yyyy-MM-dd'T'HH:mm:ss", '2014-10-10t12:34:56'],
    ["yyyy-MM-dd'T'HH:mm:ss", '2014-10-10T12:34:56'],
    ['h:mm a', '9:30 PM'],
    ['HH:mm', '09:30'],
    ['yyyy', '2014'],
    ['yyyy MM', '2014 06'],
    ['D yyyy', '100 2014'],
    ['yyyy-MM-dd D', '2014-04-10 100'],
    ['yyyy-MM-dd D', '2014-04-11 100'],
    ['yyyy-MM-dd QQQ', '2014-04-10 Q2'],
    ['yyyy-MM-dd QQQ', '2014-04-10 Q3'],
    ['yyyy-MM-dd QQQQ', '2014-04-10 2nd quarter'],
    ['yyyy-MM-dd Q', '2014-11-10 4'],
    ['yyyy-MM-dd w', '2014-10-10 41'],
    ['yyyy-MM-dd w', '2014-10-10 40'],
    ['yyyy-MM-dd W', '2014-10-10 2'],
    ['yyyy-MM-dd F', '2014-10-10 2'],
    ['yyyy-MM-dd e', '2014-10-10 6'],
    ['yyyy-MM-dd e', '2014-10-10 5'],
    ['yyyy-MM-dd c', '2014-10-10 5'],
    ['yyyy-MM-dd eeee', '2014-10-10 Friday'],
    ['yyyy-MM-dd G', '2014-10-10 AD'],
    ['yyyy-MM-dd GGGG', '2014-10-10 Anno Domini'],
    ['yyyy-MM-dd ZZZZZ', '2014-10-10 +01:00'],
    ['yyyy.MM.dd', '2014.10.10'],
    ['yyyy.MM.dd', '2014x10x10'],
    ["yyyy 'at' HH", '2014 at 10'],
    ["yyyy 'at' HH", '2014 AT 10'],
    ['yyyy-MM-dd xx', '2014-10-10 xx'],
    ["h 'o''clock' a", "9 o'clock PM"],
    ["yyyy-MM-dd ''", "2014-10-10 '"],
    ['yyyy-MM-dd j:mm a', '2014-10-10 3:15 PM'],
    ['YYYY-MM-dd', '2014-10-10'],
    ['uuuu-MM-dd', '2014-10-10'],
    ['G yyyy-MM-dd', 'BC 0044-03-15'],
    ['GGGG yyyy-MM-dd', 'Before Christ 0044-03-15'],
    ['yyyy-MM-dd ZZZZZ', '2014-10-10 -05:30'],
    ['yyyy-MM-dd ZZZZZ', '2014-10-10 0530'],
    ['yyyy-MM-dd EEEEE', '2014-10-10 F'],
    ['yyyy-MM-dd EEEEE', '2014-10-10 T'],
    ['yyyy-MM-dd MMM', '2014-10-10 Oct'],
    ['yyyy-MM-dd HH:mm:ss.SSSSSSSSS', '2014-10-10 12:34:56.123456789'],
    ['yyyy-MM-dd', '2014-10-10' . "\t"],
);

my @parse;
foreach my $c (@parse_cases)
{
    my ($pattern, $value) = @$c;
    my $dt = eval {
        DateTime::Format::CLDR->new(pattern => $pattern)->parse_datetime($value);
    };
    push @parse, {
        pattern => $pattern,
        value   => $value,
        result  => $dt ? sprintf('%04d-%02d-%02dT%02d:%02d:%02d.%09d', $dt->year,
            $dt->month, $dt->day, $dt->hour, $dt->minute, $dt->second, $dt->nanosecond) : undef,
    };
}

print JSON::PP->new->canonical->pretty->encode({
    _generator => 'conformance/perl/gen_cldr_golden.pl',
    datetime   => DateTime->VERSION,
    cldr       => DateTime::Format::CLDR->VERSION,
    format     => \@format,
    parse      => \@parse,
});
