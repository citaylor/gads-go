package GADS::Conformance::Web;

# Drives the GADS Dancer2 web application in-process (Plack::Test) against
# the conformance runner's schema, for the `http` and `login` steps.

use strict;
use warnings;

use Encode ();
use File::Basename qw(dirname);
use HTTP::Cookies;
use HTTP::Request;
use HTTP::Request::Common ();
use MIME::Base64 ();
use JSON ();
use Plack::Test;
use URI;
use DateTime;
use URI::Escape qw(uri_escape_utf8);

use GADS::Conformance::WebSchema;

my $loaded;

# The password set for users that log in (see SPEC.md): "password", hashed
# with a fixed salt so that both runners write identical database rows
our $PASSWORD      = 'password';
our $PASSWORD_HASH = '{SSHA512}+mohhbPgqahe9B/7Z+88H7b3SYD46/lw5OcuNT7ZU31ZMIPCAd/W5D4cinqsK8jbsRnH37fUuPExEROVvXDpf3NhbHQ=';

sub new
{   my ($class, %args) = @_;
    my $self = bless {
        schema  => $args{schema},
        gads    => $args{gads},
        config  => $args{config} || {},
        clients => {},
    }, $class;
    $self->_load;
    $self;
}

sub _load
{   my $self = shift;
    $GADS::Conformance::WebSchema::SCHEMA = $self->{schema};
    return if $loaded;
    my $here = dirname(__FILE__);
    $ENV{DANCER_CONFDIR}     = "$here/../../../webconf";
    $ENV{DANCER_ENVIRONMENT} = 'conformance';
    $ENV{DANCER_VIEWS}       = "$self->{gads}/views";
    $ENV{DANCER_PUBLIC}      = "$self->{gads}/public";
    # Sites are created by the scenario; the app needs at least one
    require GADS;
    my $gads = Dancer2->runner->apps->[0]->config->{gads};
    $gads->{$_} = $self->{config}->{$_} foreach keys %{$self->{config}};
    $loaded = GADS->to_app;
}

sub app { $loaded }

# The web application needs a site: make sure one exists with the host
# used for requests
sub ensure_site
{   my $self = shift;
    my $rs = $self->{schema}->resultset('Site');
    if (!$rs->count)
    {
        $rs->create({ host => 'localhost' });
    }
    else {
        $_->update({ host => 'localhost' }) foreach $rs->search({ host => undef })->all;
    }
}

# Give a user the conformance password, so that it can log in
sub set_password
{   my ($self, $user) = @_;
    $user->update({
        password  => $PASSWORD_HASH,
        pwchanged => DateTime->now,
    });
}

sub _client
{   my ($self, $name) = @_;
    $self->{clients}->{$name} ||= {
        jar  => HTTP::Cookies->new,
        csrf => undef,
    };
}

# Perform a request. Returns the observation.
sub request
{   my ($self, %args) = @_;
    my $client = $self->_client($args{as} || 'anon');
    my $method = uc($args{method} || 'GET');
    my $uri    = URI->new('http://'.($args{host} || 'localhost').$args{path});
    my $q = { %{$args{query} || {}} };
    # A JSON list cannot hold the CSRF token, which is then sent in the query
    # (as the dashboard JavaScript does)
    $q->{'csrf-token'} = $client->{csrf}
        if (ref $args{json} eq 'ARRAY' || defined $args{body}) && $args{csrf} && !exists $q->{'csrf-token'};
    if (%$q)
    {
        $uri->query_form(map { $_ => $q->{$_} } sort keys %$q);
    }
    my $req = HTTP::Request->new($method => $uri);
    if (my $uploads = $args{upload})
    {
        # A multipart form: the form values, the CSRF token and the files
        my @form = map { my $k = $_; my $v = $args{form}->{$k};
            ref $v eq 'ARRAY' ? (map { ($k, $_) } @$v) : ($k, $v)
        } sort keys %{$args{form} || {}};
        push @form, csrf_token => $client->{csrf}
            if $args{csrf} && !grep { $_ eq 'csrf_token' } @form;
        foreach my $name (sort keys %$uploads)
        {
            my $u = $uploads->{$name};
            my $content = defined $u->{content_b64}
                ? MIME::Base64::decode_base64($u->{content_b64})
                : Encode::encode_utf8($u->{content} // '');
            push @form, $name => [undef, $u->{filename},
                'Content-Type' => $u->{content_type} || 'application/octet-stream',
                Content        => $content,
            ];
        }
        my $r = HTTP::Request::Common::POST($uri, Content_Type => 'form-data', Content => \@form);
        $req->header('Content-Type' => $r->header('Content-Type'));
        $req->content($r->content);
    }
    elsif (defined $args{body})
    {
        $req->content(Encode::encode_utf8($args{body}));
    }
    elsif (my $json = $args{json})
    {
        $json->{csrf_token} = $client->{csrf}
            if ref $json eq 'HASH' && $args{csrf} && !exists $json->{csrf_token};
        $req->header('Content-Type' => 'application/json');
        $req->content(JSON->new->canonical->utf8->encode($json));
    }
    elsif ($method eq 'POST' || $method eq 'PUT' || $args{form})
    {
        my @form = map { my $k = $_; my $v = $args{form}->{$k};
            ref $v eq 'ARRAY' ? (map { ($k, $_) } @$v) : ($k, $v)
        } sort keys %{$args{form} || {}};
        push @form, csrf_token => $client->{csrf}
            if $args{csrf} && !grep { $_ eq 'csrf_token' } @form;
        my @pairs;
        while (my ($k, $v) = splice @form, 0, 2)
        {
            push @pairs, uri_escape_utf8($k).'='.uri_escape_utf8($v // '');
        }
        $req->header('Content-Type' => 'application/x-www-form-urlencoded');
        $req->content(join '&', @pairs);
    }
    $req->header($_ => $args{headers}->{$_}) foreach keys %{$args{headers} || {}};
    $client->{jar}->add_cookie_header($req);
    my $schema  = $self->{schema};
    my $site_id = $schema->site_id;
    my $res = Plack::Test->create($loaded)->request($req);
    $schema->site_id($site_id);
    $res->request($req);
    $client->{jar}->extract_cookies($res);
    return $self->_observe($client, $res, %args);
}

sub _observe
{   my ($self, $client, $res, %args) = @_;
    my $ct      = $res->header('Content-Type') // '';
    my $content = $res->content;
    $content = Encode::decode('UTF-8', $content) if $ct =~ /charset=utf-8/i || $ct =~ /json/;
    # Remember the CSRF token of the session for later requests
    if ($content =~ /data-csrf="([^"]+)"/ || $content =~ /name="csrf_token"\s+value="([^"]+)"/)
    {
        $client->{csrf} = $1;
    }
    my $obs = {
        status       => $res->code,
        content_type => $ct,
    };
    my $saml_encoded;
    if (my $loc = $res->header('Location'))
    {
        $loc =~ s!^https?://localhost!!;
        # A SAML request of a redirect is deflated XML whose attributes are
        # in the order of a Perl hash: it is observed decoded, with the
        # attributes of each element sorted
        if ($loc =~ /[?&]SAMLRequest=([^&]+)/)
        {
            $saml_encoded = $1;
            require IO::Uncompress::RawInflate;
            my $raw = MIME::Base64::decode_base64(URI::Escape::uri_unescape($1));
            my $xml;
            IO::Uncompress::RawInflate::rawinflate(\$raw => \$xml);
            $xml =~ s{<([\w:]+)((?:\s+[\w:]+="[^"]*")+)(\s*/?)>}{
                my ($name, $attrs, $end) = ($1, $2, $3);
                "<$name" . join('', map " $_", sort $attrs =~ /([\w:]+="[^"]*")/g) . "$end>"
            }ge;
            $obs->{saml_request} = $xml;
            $loc =~ s/(SAMLRequest=)[^&]+/$1\{saml_request\}/;
        }
        $obs->{location} = $loc;
    }
    foreach my $h (qw/X-Frame-Options Content-Security-Policy Content-Disposition/)
    {
        my $v = $res->header($h);
        $obs->{headers}->{$h} = $v if defined $v;
    }
    my @cookies;
    $client->{jar}->scan(sub { push @cookies, $_[1] });
    $obs->{cookies} = [ sort @cookies ];
    if ($ct =~ /json/ && length $content)
    {
        my $data = eval { JSON->new->decode($content) };
        $obs->{json} = defined $data ? _normalise_json($data, $client) : $content;
    }
    elsif (!$args{no_body})
    {
        $content =~ s/\Q$client->{csrf}\E/{csrf}/g if $client->{csrf};
        $content =~ s/\Q$saml_encoded\E/{saml_request}/g if $saml_encoded;
        # Remove the source locations of Perl error messages
        $content =~ s/ at \S+ line \d+\.\n//g;
        # Stringified references (a template printing a hash) are addresses
        $content =~ s/\b(HASH|ARRAY|CODE)\(0x[0-9a-f]+\)/$1(0x)/g;
        $obs->{body} = $content;
    }
    $obs;
}

sub _normalise_json
{   my ($data, $client) = @_;
    if (ref $data eq 'HASH')
    {
        return { map { $_ => _normalise_json($data->{$_}, $client) } keys %$data };
    }
    elsif (ref $data eq 'ARRAY')
    {
        return [ map _normalise_json($_, $client), @$data ];
    }
    elsif (JSON::is_bool($data))
    {
        return $data ? 'true' : 'false';
    }
    return undef if !defined $data;
    my $s = "$data";
    $s =~ s/\Q$client->{csrf}\E/{csrf}/g if $client->{csrf};
    return $s;
}

1;
