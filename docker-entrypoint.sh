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

exec su-exec "${puid}:${pgid}" /usr/local/bin/ha-lightcraft "$@"
