# syntax=docker/dockerfile:1
#
# The omnibus image: the base tkiller image plus PostgreSQL and Redis in one
# container, supervised by s6-overlay, so `docker compose up -d` is the whole
# install. It is the image users are meant to run, and the only one that gets
# `latest` / `build-N` / `<sha>` tags.
#
# It is not the only supported layout. Setting DATABASE_URL or REDIS_URL at run
# time points the app at a service of its own and leaves the matching bundled
# one off, so the same image can be one container or one of several. See
# docs/architecture/omnibus-image.md.
#
# CI builds this FROM the base image of the same commit, so both run the same
# binary and the same frontend bundle. That is why this file adds only the
# bundled services and the supervisor: a runtime dependency the app needs
# belongs in the root Dockerfile, and here it would drift.

# The base image of one commit. CI passes the `<sha>-base` tag it pushed; a
# local build names one the same way. No tag follows the newest base, so the
# default is a placeholder on a reserved domain that never resolves.
ARG TKILLER_IMAGE=set-haaku-image.invalid/haaku:base
FROM ${TKILLER_IMAGE}

ARG TARGETARCH
# Pinned with per-architecture checksums: this is the process supervisor, so a
# tag that moved would silently change how every service starts.
ARG S6_OVERLAY_VERSION=3.2.3.2
ARG S6_OVERLAY_NOARCH_SHA256=5379750ed30a84bbd2e2dd74847ba6b5bd29cd0b2e3ea2ec58049b57eb2eda12
ARG S6_OVERLAY_X86_64_SHA256=e6befcc96a437a3831386ecfc51808c5d3e939dc5fe3c02ae9284599e8aa2408
ARG S6_OVERLAY_AARCH64_SHA256=b17f17a82e7a515c682a91edaf2ffdabb73f891981b6c1fd712115693a2f8b4c
# The bundled PostgreSQL major version. PostgreSQL refuses a data directory
# written by another major, so a bump makes every existing install dump and
# restore: treat it as a breaking change in the release notes. The base image's
# postgresql-client uses the same major, because the pre-migration safety
# backup runs `pg_dump` from the app.
ARG PG_MAJOR=17

USER root

# Service accounts with fixed ids, created before the packages so the packages
# reuse them and /config/<service> keeps a valid owner across image updates
# even if a later package takes the next free system id. The tkiller account
# already exists in the base image (uid 911) and is the only one remapped at
# start, by PUID and PGID.
RUN groupadd -r -g 999 postgres && \
    useradd -r -u 999 -g postgres -d /var/lib/postgresql -s /bin/bash -M postgres && \
    groupadd -r -g 998 redis && \
    useradd -r -u 998 -g redis -d /var/lib/redis -s /usr/sbin/nologin -M redis

# PostgreSQL and Redis from Debian and the PostgreSQL apt repository.
# create_main_cluster is turned off before postgresql-${PG_MAJOR} installs, so
# the package does not create a cluster under /var/lib/postgresql; the data
# directory lives under /config/postgres instead.
RUN apt-get update && \
    apt-get install -y --no-install-recommends locales xz-utils && \
    sed -i 's/^# *\(en_US.UTF-8 UTF-8\)/\1/' /etc/locale.gen && \
    locale-gen && \
    install -d /etc/apt/keyrings && \
    curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
      -o /etc/apt/keyrings/pgdg.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc && \
    echo "deb [signed-by=/etc/apt/keyrings/pgdg.asc arch=${TARGETARCH}] https://apt.postgresql.org/pub/repos/apt trixie-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends postgresql-common && \
    sed -ri 's/^#?(create_main_cluster)\s*=.*/\1 = false/' /etc/postgresql-common/createcluster.conf && \
    apt-get install -y --no-install-recommends \
      "postgresql-${PG_MAJOR}" redis-server && \
    rm -rf /var/lib/apt/lists/* /var/lib/postgresql /etc/postgresql

# s6-overlay, verified against the pinned checksums before it is unpacked.
RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) s6_arch=x86_64; s6_sha="${S6_OVERLAY_X86_64_SHA256}" ;; \
      arm64) s6_arch=aarch64; s6_sha="${S6_OVERLAY_AARCH64_SHA256}" ;; \
      *) echo "unsupported TARGETARCH ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    base="https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}"; \
    curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o /tmp/s6-noarch.tar.xz "${base}/s6-overlay-noarch.tar.xz"; \
    curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o /tmp/s6-arch.tar.xz "${base}/s6-overlay-${s6_arch}.tar.xz"; \
    echo "${S6_OVERLAY_NOARCH_SHA256}  /tmp/s6-noarch.tar.xz" | sha256sum -c -; \
    echo "${s6_sha}  /tmp/s6-arch.tar.xz" | sha256sum -c -; \
    tar -C / -Jxpf /tmp/s6-noarch.tar.xz; \
    tar -C / -Jxpf /tmp/s6-arch.tar.xz; \
    rm -f /tmp/s6-noarch.tar.xz /tmp/s6-arch.tar.xz

# The service tree and the init scripts. Everything under /config is created at
# run time by init-config, not here.
COPY rootfs/ /
RUN find /etc/s6-overlay/scripts -type f -exec chmod 0755 {} + && \
    find /etc/s6-overlay/s6-rc.d -type f \
      \( -name run -o -name up -o -name finish -o -name check \) -exec chmod 0755 {} + && \
    chmod 0644 /etc/tkiller-omnibus/lib.sh /etc/tkiller-omnibus/postgresql.omnibus.conf

ENV PATH="/usr/lib/postgresql/${PG_MAJOR}/bin:${PATH}" \
    PG_MAJOR="${PG_MAJOR}" \
    PUID=911 \
    PGID=911 \
    TKILLER_BACKUP_DIR=/config/tkiller/backups \
    TKILLER_MIGRATION_BACKUP_DIR=/config/tkiller/pre-migrate-backups \
    S6_CMD_WAIT_FOR_SERVICES_MAXTIME=0 \
    S6_BEHAVIOUR_IF_STAGE2_FAILS=2 \
    S6_VERBOSITY=1

VOLUME /config

# The base image's health check probes 8080; here PORT may be overridden, and
# the first start applies every migration, which can take minutes on a large
# database. Not healthy before that is correct — the API is not serving yet.
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5m --retries=3 \
  CMD curl -fsS "http://127.0.0.1:$(printf '%s' "${PORT:-8080}")/healthz" || exit 1

# /init needs root to map accounts and hand directories to them. To run this
# image without root, set the entrypoint to /app/api with an external
# DATABASE_URL — see "Starting without root" in docs/architecture/omnibus-image.md.
USER root
ENTRYPOINT ["/init"]
