# syntax=docker/dockerfile:1
#
# The base image: the tkiller binary with its frontend embedded, plus a
# postgresql-client. The omnibus image (omnibus/Dockerfile) is built FROM this
# one, so anything the service needs at runtime belongs here, and omnibus/
# adds only the bundled services and s6. Both images then run the same binary.
#
# CI publishes this under one tag, `<sha>-base`, which no other tag names; the
# omnibus image carries latest / build-N / <sha>. See
# docs/architecture/omnibus-image.md.

# --- Frontend ---
#
# web/embed.go embeds web/dist with `//go:embed all:dist`, so the directory has
# to hold the real bundle *before* the Go build. It is built here rather than
# copied from the host: .dockerignore excludes web/dist (a host bundle must not
# leak into an image), and a bundle built outside would not match the lockfile
# this stage installs from.
FROM node:20-slim AS frontend
ENV CI=true
RUN corepack enable
WORKDIR /app/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

# A named context lets CI pass a prebuilt bundle (`--build-context
# frontend_dist=<dir>`) instead of building it again for the other
# architecture. Local builds use the stage above.
FROM scratch AS frontend_dist
COPY --from=frontend /app/web/dist/. /

# --- Backend ---
#
# The Go tag is pinned to the toolchain go.mod requires, so a build cannot
# silently use a different Go than a developer does.
FROM golang:1.26.9-trixie AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend_dist / web/dist
RUN go build -o /out/api ./cmd/api

# --- Runtime ---
#
# Debian, not alpine, because the pre-migration safety backup runs `pg_dump`
# from the app and that has to be at least the server's major version: the
# PGDG apt repository (Debian only) is where a matching client comes from. The
# same requirement is why PG_MAJOR is declared here and inherited by omnibus/.
FROM debian:trixie-slim AS runtime
ARG TARGETARCH
ARG PG_MAJOR=17
ARG IMAGE_VERSION=dev
ARG BUILD_REVISION=unknown
ARG BUILD_DATE=unknown
# The PGDG key and the s6 tarballs (omnibus/) are the two fetches the build
# depends on; --retry-all-errors is there because a transient TLS reset failed a
# whole CI job once.
RUN apt-get update && \
    # Pick up security updates for whatever the debian tag was when it was
    # last pushed, not only for the packages named below.
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg tzdata && \
    install -d /etc/apt/keyrings && \
    curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
      -o /etc/apt/keyrings/pgdg.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc && \
    echo "deb [signed-by=/etc/apt/keyrings/pgdg.asc arch=${TARGETARCH}] https://apt.postgresql.org/pub/repos/apt trixie-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends "postgresql-client-${PG_MAJOR}" && \
    apt-get purge -y gnupg && apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/*
LABEL org.opencontainers.image.title="trakt-killer" \
      org.opencontainers.image.description="Open, self-hostable watch-tracking server (Trakt replacement)" \
      org.opencontainers.image.source="https://github.com/trakt-killer/tkiller-api" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}"
WORKDIR /app
COPY --from=build /out/api /app/api
# Unprivileged by default here. The omnibus image starts as root instead,
# because its init has to map accounts and hand directories to them; it
# documents /app/api as the way to run it without root.
RUN groupadd -g 911 tkiller && \
    useradd -u 911 -g tkiller -d /home/tkiller -s /usr/sbin/nologin -m tkiller && \
    install -d -o tkiller -g tkiller /home/tkiller
ENV TKILLER_BACKUP_DIR=/home/tkiller/backups \
    TKILLER_MIGRATION_BACKUP_DIR=/home/tkiller/pre-migrate-backups
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD curl -fsS "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1
USER tkiller
CMD ["/app/api"]
