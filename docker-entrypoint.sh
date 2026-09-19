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

runtime_user="lightcraft"
if ! awk -F: -v uid="${puid}" '$3 == uid { found=1 } END { exit !found }' /etc/passwd; then
    if awk -F: -v name="${runtime_user}" '$1 == name { found=1 } END { exit !found }' /etc/passwd /etc/group; then
        runtime_user="lightcraft-${puid}"
    fi
    printf '%s:x:%s:%s:HA Lightcraft:/data:/sbin/nologin\n' "${runtime_user}" "${puid}" "${pgid}" >> /etc/passwd
fi
if ! awk -F: -v gid="${pgid}" '$3 == gid { found=1 } END { exit !found }' /etc/group; then
    printf '%s:x:%s:\n' "${runtime_user}" "${pgid}" >> /etc/group
fi

mkdir -p /data/data/current /data/data/proposed /data/backups
chown -R "${puid}:${pgid}" /data/data /data/backups

exec su-exec "${puid}:${pgid}" /usr/local/bin/ha-lightcraft "$@"
