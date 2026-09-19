#!/bin/sh
set -eu

puid="${PUID:-1000}"
pgid="${PGID:-1000}"

case "${puid}" in
    ''|*[!0-9]*)
        echo "PUID and PGID must be numeric" >&2
        exit 2
        ;;
esac
case "${pgid}" in
    ''|*[!0-9]*)
        echo "PUID and PGID must be numeric" >&2
        exit 2
        ;;
esac

mkdir -p /data/data/current /data/data/proposed /data/backups
chown -R "${puid}:${pgid}" /data/data /data/backups

exec su-exec "${puid}:${pgid}" /usr/local/bin/ha-lightcraft "$@"
