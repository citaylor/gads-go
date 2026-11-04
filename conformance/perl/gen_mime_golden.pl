#!/usr/bin/env perl
# Generates the golden cases of the Go File::MimeInfo (gads-go/internal/mimeinfo):
# the type File::MimeInfo->mimetype gives each file, with the test globs of
# internal/mimeinfo/testdata (a system directory and the user's) and
# nothing else.
#
#   perl gen_mime_golden.pl ../../gads-go/internal/mimeinfo/testdata \
#       > ../../gads-go/internal/mimeinfo/testdata/golden.json
#
# A file is given by its name and the hex of its contents (no contents: the
# file does not exist).
use strict;
use warnings;
use utf8;
use Cwd qw(abs_path);
use File::Temp qw(tempdir);
use JSON::PP;

my $data;
BEGIN {
    $data = abs_path(shift // die "usage: $0 TESTDATA\n");
    $ENV{XDG_DATA_HOME} = "$data/home";
    $ENV{XDG_DATA_DIRS} = "$data/system";
}
use File::MimeInfo ();

my $text = unpack 'H*', "hello\n";
my @names = (
    'a.txt', 'A.TXT', 'a.Txt', 'x.csv', 'a.b.pdf', 'a.tar.gz', 'b.gz', 'A.TAR.GZ', 'c.tar.bz2',
    'x.tar', 'Makefile', 'makefile', 'MAKEFILE', 'README', 'README.md', 'readme', 'READMEx',
    'error.log.1', 'error.log.12', 'error.log', 'file~', 'x.bak', 'x.dup', 'x.UPR', 'x.upr',
    'x.a-b', 'x.b', 'file1.q', 'file.q', 'file12.q', 'x.c++', 'x.ünï', 'x.ÜNÏ', 'x.ovr',
    'ov1.x', 'x.home', 'core', 'Core', 'nocolon', '.txt', 'txt', 'x.', 'x..txt', 'x.txt.',
    'x.unknown', 'noext', 'É.TXT', 'x_y.c++', 'x.tar.GZ', 'x.pdf.unknown', 'x.unknown.pdf',
);
my @contents = (
    '', $text, '00', '000102', unpack('H*', "tab\there\r\n\f\x0b"), '7f', '1b5b6d',
    unpack('H*', 'x' x 40), unpack('H*', ('x' x 40) . "\x01"), unpack('H*', ('x' x 31) . "\x01"),
    unpack('H*', "caf\xc3\xa9 \xe6\x97\xa5\xe6\x9c\xac"), 'c3', 'ff', 'e282ac', 'c280',
    unpack('H*', "\xc2\x85 next line"), unpack('H*', "x\xe2\x80\xa8y"), unpack('H*', "\xef\xbb\xbfbom"),
    unpack('H*', "\xf0\x9f\x98\x80 emoji"), unpack('H*', ("\xe6\x97\xa5" x 33) . "\x01"),
    unpack('H*', "latin1 \xe9t\xe9"),
    unpack('H*', "zero\xe2\x80\x8bwidth"), unpack('H*', "private \xee\x80\x80"),
    unpack('H*', "unassigned \xcd\xb8"), unpack('H*', "mark e\xcc\x81"),
);

my $dir = tempdir(CLEANUP => 1);
my @cases;
my $n = 0;
my $case = sub {
    my ($name, $hex) = @_;
    my $sub = "$dir/" . $n++;
    mkdir $sub or die "$sub: $!";
    my $file = "$sub/$name";
    if (defined $hex) {
        open my $fh, '>:raw', $file or die "$file: $!";
        print $fh pack 'H*', $hex;
        close $fh;
    }
    utf8::encode(my $bytes = $file);
    push @cases, { name => $name, (defined $hex ? (contents => $hex) : ()),
        mime => File::MimeInfo::mimetype($bytes) // '' };
};
$case->($_, $text) for @names;
$case->('x.txt', undef);
$case->('nofile', undef);
$case->('data', $_) for @contents;
$case->('x.unknown', $_) for @contents;

print JSON::PP->new->canonical->pretty->utf8->encode({
    _generator => 'conformance/perl/gen_mime_golden.pl',
    file_mimeinfo => $File::MimeInfo::VERSION,
    cases => \@cases,
});
