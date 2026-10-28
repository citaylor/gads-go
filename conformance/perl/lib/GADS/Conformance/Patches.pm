package GADS::Conformance::Patches;

# The patches of the CPAN modules that make GADS deterministic for the
# conformance runner (perl/run.pl) and the database units of
# perl/bench_units.pl: loaded before GADS and DateTime, in a BEGIN block.

use strict;
use warnings;

# Must be loaded before DateTime so that the clock can be frozen
require Test::MockTime;
Test::MockTime->import(qw(set_fixed_time restore_time));
Test::MockTime::set_fixed_time('2014-10-10T01:00:00Z');

# Perl hashes have a random order, so JSON produced by GADS (for
# example the data attributes of web pages) would differ between runs.
# Make all JSON encoding canonical (sorted keys), as the Go port does.
require JSON::PP;
no warnings qw(redefine prototype);
my $new = \&JSON::PP::new;
*JSON::PP::new = sub { $new->(@_)->canonical(1) };
# The same for the XS backends where installed (JSON uses JSON::XS,
# JSON::MaybeXS Cpanel::JSON::XS), including their encode_json, which
# does not go through new (done before JSON loads them, as it takes
# encode_json from its backend)
foreach my $xs (qw/JSON::XS Cpanel::JSON::XS/)
{
    eval "require $xs; 1" or next;
    no strict 'refs';
    my $xs_new = \&{"${xs}::new"};
    *{"${xs}::new"} = sub { $xs_new->(@_)->canonical(1) };
    *{"${xs}::encode_json"} = sub { $xs->new->utf8->encode($_[0]) };
}

# Random tokens (CSRF tokens, widget grid IDs, submission tokens etc)
# are generated as a predictable sequence, as the Go runner does: "T"
# followed by a zero-padded counter, reset for each scenario
require Session::Token;
*Session::Token::new = sub {
    my ($class, %args) = @_;
    bless { length => $args{length} || 22 }, 'GADS::Conformance::Token';
};
*GADS::Conformance::Token::get = sub {
    my $self = shift;
    sprintf "T%0*d", $self->{length} - 1, ++$GADS::Conformance::TOKENS;
};

# SAML request IDs (random bytes) also come from the sequence
require Net::SAML2::Role::ProtocolMessage;
*Net::SAML2::Role::ProtocolMessage::generate_id = sub {
    sprintf "NETSAML2_T%063d", ++$GADS::Conformance::TOKENS;
};

# Random passwords (GADS _random_pw) come from the same sequence: "Pw"
# followed by the counter padded to 4 digits
require CtrlO::Crypt::XkcdPassword;
*CtrlO::Crypt::XkcdPassword::xkcd = sub {
    sprintf "Pw%04d", ++$GADS::Conformance::TOKENS;
};

1;
