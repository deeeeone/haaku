#!/usr/bin/env bash
# Shared helpers for the omnibus init and service scripts. Sourced, not run.
#
# shellcheck shell=bash

STATE_DIR=/run/tkiller-omnibus
CONTAINER_ENV=/run/s6/container_environment

log() { printf '[omnibus] %s\n' "$*"; }
warn() { printf '[omnibus] warning: %s\n' "$*" >&2; }

# truthy VALUE: accepts the usual spellings of yes.
truthy() {
  case "${1,,}" in
    1 | true | yes | on) return 0 ;;
    *) return 1 ;;
  esac
}

# enabled NAME: whether init-config turned the named bundled service on.
enabled() { [[ -f "$STATE_DIR/$1.enabled" ]]; }

# export_env NAME VALUE: make a variable visible to every later service.
export_env() {
  printf '%s' "$2" >"$CONTAINER_ENV/$1"
  export "$1=$2"
}

# own_tree USER GROUP PATH: hand a tree to a service account, touching only
# entries that are not already owned correctly so large trees stay fast.
own_tree() {
  find "$3" \( ! -user "$1" -o ! -group "$2" \) -exec chown -h "$1:$2" {} + 2>/dev/null || true
}

# write_as USER PATH, append_as USER PATH: write or append stdin to PATH as
# USER. For files in a directory a service account owns: written as root, a
# symlink that account planted at PATH would redirect the write.
write_as() { s6-setuidgid "$1" tee "$2" >/dev/null; }
append_as() { s6-setuidgid "$1" tee -a "$2" >/dev/null; }

# notify_ready: tell s6 a longrun is ready on the fd named in notification-fd.
notify_ready() { printf '\n' >&3; }

# exec_clean PATTERN CMD...: exec CMD with only PATH, HOME, TZ and the variables
# whose names match the extended regex PATTERN. The bundled services run this
# way so they never see TKILLER_SECRET_KEY or the rest of the app's
# environment: the tkiller database role is the cluster's superuser, and a
# superuser can read files and run programs as other accounts, so a secret in
# PostgreSQL's own environment would be readable through the database.
#
# The app itself is *not* started this way — it needs its environment, and it
# runs as its own unprivileged account.
exec_clean() {
  local pattern=$1 name
  shift
  local -a keep=("PATH=$PATH" "HOME=$HOME")
  [[ -n "${TZ:-}" ]] && keep+=("TZ=$TZ")
  if [[ -n "$pattern" ]]; then
    for name in $(compgen -e); do
      [[ "$name" =~ $pattern ]] && keep+=("$name=${!name}")
    done
  fi
  exec env -i "${keep[@]}" "$@"
}

# PG_LOCK is held, as a flock(2) lock, by the PostgreSQL server for its whole
# life. postmaster.pid only guards servers in one PID namespace, so a second
# container on the same /config would see a stale-looking pid file and start a
# second server on the same data directory. The file lives in /config, which
# only root writes, so the postgres account cannot swap it for a new one. It is
# root:postgres 0660 and opened read-write: NFS emulates flock(2) with a POSIX
# lock, and an exclusive one needs a writable descriptor.
# shellcheck disable=SC2034 # used by init-config, init-postgres, svc-postgres
PG_LOCK=/config/.postgres.lock

# read_small PATH: the first 16 bytes of PATH, read without following a link and
# without blocking on a FIFO, for files root reads in a directory a service
# account controls.
read_small() { dd if="$1" bs=16 count=1 iflag=nofollow,nonblock status=none 2>/dev/null; }

# tkiller_port: the port the app listens on. PORT from the container wins, then
# the image default, which is also the binary's own default.
tkiller_port() { printf '%s' "${PORT:-8080}"; }
