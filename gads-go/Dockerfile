# GADS (Go) application image.
#
# Build with docker compose (see docker-compose.yml), or directly:
#
#   docker build -t gads-go --build-context gads-upstream=../upstream/GADS-dev .
#
# The views (Template Toolkit) and public files (built CSS/JS) are those of
# the Perl application, taken from the named build context "gads-upstream".

ARG GO_VERSION=1.27
ARG DEBIAN_RELEASE=trixie

# builder: everything needed to build and test the Go code, without the
# source (mount it at /src):
#
#   docker build --target builder -t gads-go-builder .
#   docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/src" gads-go-builder go test ./...
#
# or the builder service of docker-compose.yml. The Go toolchain, gcc and
# the C library headers (cgo for the vendored Lua of calculated fields) and
# the modules of go.mod, downloaded and verified, so no downloads are
# needed to build. The caches are writable by any user.
FROM golang:${GO_VERSION}-${DEBIAN_RELEASE} AS builder
# (shared-mime-info: MIME types of files, as on the host; postgresql-client
# and openssl: running the service from the source, as
# the dev service of docker-compose.yml does)
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev make git \
        postgresql-client openssl shared-mime-info \
    && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=1 \
    GOTOOLCHAIN=local \
    GOFLAGS=-mod=readonly \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/go/pkg/mod
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify \
    && mkdir -p /cache/go-build \
    && chmod -R a+rwX /go /cache

# build: the binary of the application image
FROM builder AS build
COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -ldflags "-s -w" -o /out/gads ./cmd/gads

FROM debian:${DEBIAN_RELEASE}-slim
# postgresql-client: waiting for and seeding the database; shared-mime-info:
# the MIME types of uploaded files (as File::MimeInfo); openssl: hashing
# the initial admin password
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata postgresql-client openssl shared-mime-info \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /var/lib/gads --create-home gads
COPY --from=build /out/gads /usr/local/bin/gads
COPY --from=gads-upstream views /usr/share/gads/views
COPY --from=gads-upstream public /usr/share/gads/public
COPY docker/config.yml /etc/gads/config.yml
COPY docker/entrypoint.sh /usr/local/bin/gads-entrypoint
# The entrypoint writes the database settings to environments/docker.yml
# Uploaded files are stored in /var/lib/gads/uploads (gads.uploads)
RUN mkdir -p /etc/gads/environments && chown gads /etc/gads/environments \
    && mkdir -p /var/lib/gads/uploads && chown gads:gads /var/lib/gads/uploads \
    && chmod 755 /usr/local/bin/gads-entrypoint

ENV GADS_CONFIG=/etc/gads \
    DANCER_ENVIRONMENT=docker \
    TZ=UTC
USER gads
WORKDIR /var/lib/gads
EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/gads-entrypoint"]
CMD ["gads", "serve", "-listen", ":3000", "-views", "/usr/share/gads/views", "-public", "/usr/share/gads/public"]
