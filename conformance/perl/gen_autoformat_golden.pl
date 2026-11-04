#!/usr/bin/env perl
# Generates the golden cases of the Go Text::Autoformat
# (gads-go/internal/autoformat): the plain text of an email as
# GADS::Email->send formats it, autoformat($text, {all => 1, break =>
# break_wrap}).
#
#   eval "$(perl -I ~/perl5/lib/perl5 -Mlocal::lib)"
#   perl gen_autoformat_golden.pl > ../../gads-go/internal/autoformat/testdata/golden.json
#
# The cases are the texts below, then random texts made of the parts that
# Text::Autoformat treats apart (quoters, list markers, indents, blank
# lines, separators, block quotes, tabs, long words), from a fixed seed.
use strict;
use warnings;
use utf8;
use JSON::PP;
use Text::Autoformat qw(autoformat break_wrap);

my @cases = (
    "",
    "   \n  ",
    "Hello",
    "Hello\n",
    "Hello\n\n\n",
    "\n\nHello",
    "A message",
    "A message\n\nMessage sent by: User1, User1 (user1\@example.com)\n",
    "0",
    "0\n0",
    ("word " x 40),
    ("x" x 100) . " short",
    "short " . ("x" x 100) . " short",
    "Please click on the following link to reset your password:\n\nhttps://www.example.com/resetpw/" . ("a" x 80) . "\n\nThanks\n",
    "First line\nsecond line\n   indented third\nfourth",
    "Line one\r\nLine two\r\n\r\nParagraph two\r\n",
    "\tTabbed\tline\twith\ttabs",
    "a\tb\tc\n\td",
    "1. First\n2. Second\n3. Third",
    "1. First\n1. Second\n1. Third",
    "01. First\n01. Second\n01: Third\n01: Fourth",
    "007) one\n007) two",
    "a) one\nb) two\nz) three\nz) four",
    "i. one\nii. two\niii. three\niv. four",
    "i. one\ni. two",
    "I. One\nI. Two\nI. Three",
    "(a) one\n(a) two\n(1) three",
    "[1] one\n[1] two",
    "<1> one\n<1> two",
    "1.1. one\n1.1. two\n1.1.1. three\n1.1.1. four\n1.2. five",
    "1. one\n  a. sub\n  a. sub\n2. two",
    "* bullet one\n* bullet two that is long enough to wrap around the right margin of seventy two columns\n- dash\n+ plus\n. dot",
    "PS: one\nPS: two",
    "ps: one\n\nps: two",
    "P.S. one\n\nP.S. two",
    "N.B. note\nNB: note\nNote: note\nnote : note",
    "Mr. Smith went to Washington.\nDr. Who\nU.S.A. is big",
    "8:20 is the time\n1999. was a year\n1000. too",
    "(see below) text\n(x y) text",
    "> quoted line one\n> quoted line two\n>> double quoted\n> back to one",
    "> " . ("quoted " x 20) . "\n> more",
    ": colon quoted\n: again\n| bar quoted\n| again\n# hash\n% percent\n= equals\n; semi\n! bang",
    "abc> quoted with a word\nabc> again",
    "    indented by four\n    and again\n\n        indented by eight",
    "   centred line\n  centred again\n   and once more",
    "----\n____\n====\n####\n****\n++++\n~~~~\n--",
    "Text\n----\nMore text",
    "\"To be or not to be, that is the question, whether 'tis nobler in the mind to suffer\"\n    -- William Shakespeare",
    "'A quote'\n   - Someone\n     Somewhere",
    "``Double backquote''\n  -- Author",
    "Café crème, naïve résumé, Ünïcödé\n日本語のテキスト 日本語のテキスト 日本語のテキスト 日本語のテキスト 日本語のテキスト",
    "e\x{301}\te\x{301}\x{301}\tx",
    "Trailing spaces   \nand tabs\t\t\n",
    (" " x 80) . "deeply indented text that is long",
    "\x{a0}non-breaking\x{a0}space and\x{2003}em space",
    "word-with-hyphens-" x 8,
    "Text with a very-long-hyphenated-word-that-goes-on-and-on-and-on-and-on-forever-and-ever here",
    # Lines indented to the margin: the text of the paragraph is used as a
    # format, with its fields (justified, numeric)
    (" " x 71) . "word word",
    (" " x 72) . "x [[ y",
    (" " x 75) . "text with [[[ fields ]]] and >>>.<< 12.5 and <<<>>> [[[[]]]] end",
    (" " x 80) . "0",
    (" " x 80) . ">>>.<< 3.14159 and ]]].[[ 1e999 x",
    (" " x 70) . "> quoted text here",
    (" " x 60) . "1. a list item that is long enough to wrap",
    "PS: a\n\nPS: b",
    "Ps: a\nPs: b",
    "pS: a\npS: b",
    "P.S. a\nP.P.S. b\nP.S. c",
    "* \"A quotation that is fairly long, long enough to need wrapping onto a second line\"\n    -- Its Author",
    "1. \"quote\"\n     - Person",
    "\te\x{301}\tx\n\x{301}\ty",
    "c. one\nd. two\ne. three",
    "v. one\nw. two",
    "x. one\nx. two\nx. three",
    "MM. a\nMM. b",
    "1. a\n1.1. b\n1.1. c\n2. d\n2.1. e",
    "(i) a\n(ii) b\n(iii) c",
    "[a] one\n[a] two",
    "9. nine\n9. ten\n99. a\n99. b\n999. c\n999. d",
    "z. last\nz. again",
    "A. first\nA. second",
    "a9. x\na9. y",
    "1. one\n\n   continued\n2. two",
    "> > nested\n> > again\n>\n> single",
    "Short line.\nAnother short line that is a bit longer than the first one is.\nShort.",
    ("lorem ipsum dolor sit amet " x 3) . "consectetur",
    ("lorem ipsum dolor sit amet " x 5) . "x",
);

srand(20261104);
my @words = qw(the quick brown fox jumps over lazy dog GADS record table
    field value view alert user email message Please click link sent by
    an and of to in it is that Mr. etc. e.g. 8:20 1999 x);
push @words, 'Café', 'naïve', '日本語', 'é', 'x' x 30, 'y' x 75, 'a-b-c', "don't";
my @quoters = ('', '', '', '', '> ', '>', '>> ', '> > ', ': ', '| ', 'ab> ', '# ');
my @hangs = ('', '', '', '', '', '1. ', '2. ', '1) ', 'a. ', 'b) ', '(a) ', '(1) ',
    'i. ', 'ii. ', 'iv. ', 'I. ', '* ', '- ', '+ ', 'PS: ', 'N.B. ', 'Note: ',
    '1.1. ', '1.2. ', '[1] ', 'Z. ', 'z) ', '10. ', '999. ', 'x. ', 'MM. ');
my @indents = ('', '', '', '', ' ', '  ', '    ', "\t", '        ', ' ' x 20);

sub pick { $_[int rand @_] }

sub random_line {
    my $r = rand();
    return '' if $r < 0.15;
    return pick('----', '====', '****', '~~~~', '--') if $r < 0.18;
    return pick(' ', "\t", '   ') if $r < 0.20;
    my $n = 1 + int rand 18;
    my $words = join pick(' ', ' ', ' ', '  ', "\t"), map { pick(@words) } 1..$n;
    my $line = pick(@indents) . pick(@quoters) . pick(@hangs) . $words;
    $line .= pick('', '', '', ' ', "\t", "\r");
    return $line;
}

sub random_text {
    my $r = rand();
    if ($r < 0.05) {
        my $q = pick('"', "'", '``');
        my $c = $q eq '``' ? "''" : $q;
        my $text = join ' ', map { pick(@words) } 1..(3 + int rand 25);
        return "$q$text$c\n" . (' ' x (1 + int rand 6)) . pick('--', '-') . ' '
            . join(' ', map { pick(@words) } 1..(1 + int rand 4))
            . (rand() < 0.5 ? "\n" . (' ' x 4) . 'Second line' : '');
    }
    my $lines = 1 + int rand 12;
    my $text = join "\n", map { random_line() } 1..$lines;
    $text .= pick('', '', "\n", "\n\n");
    return $text;
}

push @cases, random_text() for 1..800;

# As GADS's decoded parameters
utf8::upgrade($_) for @cases;

my @out;
foreach my $in (@cases) {
    my $out = eval { autoformat($in, {all => 1, break => break_wrap}) };
    push @out, defined $out
        ? { in => $in, out => $out }
        : { in => $in, error => "$@" };
}
print JSON::PP->new->utf8->canonical->pretty->encode(\@out);
