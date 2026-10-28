# The Perl GADS, for running the conformance suite and the benchmarks in a
# container on the same base as the Go builder (Debian trixie), so that the
# two are compared on equal terms. It holds the toolchain and the CPAN
# modules only: the GADS source (/upstream/GADS) and the conformance suite
# (/conformance) are mounted, as docker-compose.bench.yml does.
#
#   docker/perl/gen-cpanfile.sh        # pin the modules of the reference install
#   docker compose -f docker-compose.bench.yml build perl
#
# The modules are the exact versions of cpanfile.pinned. With XS=1 (the
# default) the XS backends that a production install has are added: JSON::XS
# (used by JSON, which otherwise falls back to JSON::PP) and Text::CSV_XS
# (Text::CSV, otherwise Text::CSV_PP).

ARG PERL_VERSION=5.40
ARG DEBIAN_RELEASE=trixie
FROM perl:${PERL_VERSION}-${DEBIAN_RELEASE}

# Lua 5.3 (Inline::Lua, as the Go port's vendored Lua 5.3.6), PostgreSQL
# (DBD::Pg), libmagic (File::LibMagic), libxml2/expat (Net::SAML2),
# shared-mime-info (File::MimeInfo, as the Go image), libqrencode
# (Imager::QRCode, for the MFA of the dev branch)
RUN apt-get update \
    && apt-get install -y --no-install-recommends liblua5.3-dev libpq-dev \
        postgresql-client libmagic-dev libxml2-dev libexpat1-dev libssl-dev libqrencode-dev \
        zlib1g-dev shared-mime-info \
    && rm -rf /var/lib/apt/lists/*

# Inline::Lua: the ctrlo fork (not on CPAN), at a fixed commit
ARG INLINE_LUA_COMMIT=fd9512458cafa13e3ccd36b2b5f85d42cd3845bb
RUN cpanm --notest --quiet Inline@0.87 \
    && git clone https://github.com/ctrlo/Inline-Lua /tmp/inline-lua \
    && cd /tmp/inline-lua && git checkout -q "$INLINE_LUA_COMMIT" \
    && perl Makefile.PL INC=-I/usr/include/lua5.3 LIBS=-llua5.3 \
    && make && make install \
    && cd / && rm -rf /tmp/inline-lua

# The exact distributions, in the order of the reference install (so that
# dependencies come first), from CPAN or, for older releases, BackPAN; then
# cpanfile.pinned checks that every module is at its pinned version
COPY dists.pinned cpanfile.pinned /tmp/
RUN grep -v '^#' /tmp/dists.pinned \
        | xargs cpanm --notest --quiet \
            --mirror https://cpan.metacpan.org --mirror https://backpan.perl.org \
    && cpanm --notest --quiet --installdeps --cpanfile /tmp/cpanfile.pinned /tmp \
    && rm -rf /root/.cpanm

ARG XS=1
RUN if [ "$XS" = 1 ]; then cpanm --notest --quiet JSON::XS Text::CSV_XS && rm -rf /root/.cpanm; fi

# The Inline build directory (Inline::Lua compiles GADS::Column::Code's Lua
# on first use), writable by the user the container runs as
ENV PERL_INLINE_DIRECTORY=/cache/inline \
    GADS_NO_FORK=1 \
    TZ=UTC
RUN mkdir -p /cache/inline && chmod -R a+rwX /cache
WORKDIR /conformance
