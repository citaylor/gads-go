#!/usr/bin/perl
# Writes the expected words of each case of cases.json, made by CtrlO::PDF
# with the logic of GADS::PDFGenerator build: perl perl_golden.pl, with
# CtrlO::PDF 0.36, PDF::Table 1.007, PDF::Builder 3.028, the Liberation
# fonts in /usr/share/fonts/truetype/liberation, and pdftotext (poppler)
use strict;
use warnings;
use utf8;
use CtrlO::PDF;
use File::Temp qw/tempfile/;
use JSON::PP;
use MIME::Base64;

my $json  = JSON::PP->new->canonical->utf8;
my $cases = $json->decode(do { local $/; open my $fh, '<', 'cases.json' or die; <$fh> });

foreach my $case (@$cases)
{
    my $logo;
    if ($case->{logo})
    {
        my $fh;
        ($fh, $logo) = tempfile(UNLINK => 1);
        binmode $fh;
        print $fh decode_base64($case->{logo});
        close $fh;
    }
    my $content = eval { build($case, $logo) };
    if (my $err = $@)
    {
        $err =~ s/ at \S+ line \d+\.\n//;
        open my $out, '>:raw', "perl/$case->{name}.txt" or die;
        print $out "error: $err\n";
        close $out;
        next;
    }
    my ($fh, $file) = tempfile(SUFFIX => '.pdf', UNLINK => 1);
    binmode $fh;
    print $fh $content;
    close $fh;
    open my $out, '>:raw', "perl/$case->{name}.txt" or die;
    print $out `pdftotext -bbox $file - | ./words.pl`;
    close $out;
}

sub build
{   my ($case, $logo) = @_;
    my $pdf = CtrlO::PDF->new(
        header => $case->{marking},
        footer => $case->{marking},
        ($logo ? (logo => $logo) : ()),
    );
    $pdf->add_page;
    $pdf->heading($case->{title}, topmargin => $logo ? -30 : 0);
    $pdf->text($case->{subtitle}, size => 14) if $case->{subtitle};
    my $hdr_props = {
        repeat => 0, justify => 'center', font_size => 12,
        bg_color => '#007c88', fg_color => '#ffffff',
    };
    foreach my $topic (@{$case->{topics}})
    {
        my $fields = [ [$topic->{name}] ];
        my $width  = 0;
        foreach my $col (@{$topic->{fields}})
        {
            if ($col->{selected})
            {
                my $first = 1;
                foreach my $values (@{$col->{selected}})
                {
                    $width = $width < (@$values + 1) ? @$values + 1 : $width;
                    push @$fields, [ $first ? $col->{name} : '', @$values ];
                    $first = 0;
                }
            }
            else
            {
                if ($col->{value}) {
                    push @$fields, [ $col->{name}, $col->{value} || "" ];
                }
                else {
                    push @$fields, [ $col->{name}, $col->{grade} ];
                }
                $width = 2 if $width < 2;
            }
        }
        my $cell_props = [];
        foreach my $d (@$fields)
        {
            my $has = @$d;
            my $gap = $width - $has + 1;
            push @$d, undef for (1 .. $gap);
            push @$cell_props, [ (undef) x ($has - 1), { colspan => $gap + 1 } ];
        }
        $pdf->table(
            data => $fields, header_props => $hdr_props, border_c => '#007C88',
            h_border_w => 1, cell_props => $cell_props, size => '4cm *',
        );
    }
    $pdf->text($case->{last_edited}, size => 10);
    $pdf->text($case->{generated_by}, size => 10);

    return $pdf->content;
}
