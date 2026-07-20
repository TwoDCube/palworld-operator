#!/usr/bin/env bash
#
# Health/readiness probe for the Palworld dedicated server.
#
# Modes (first arg):
#   startup   - succeed as soon as the server binary has been installed
#   liveness  - succeed while the server process/port is alive
#   readiness - succeed once the server accepts admin API / query traffic
#
set -uo pipefail

MODE="${1:-readiness}"
: "${REST_ENABLED:=true}"
: "${REST_PORT:=8212}"
: "${GAME_PORT:=8211}"
: "${ADMIN_PASSWORD:=}"
: "${STEAMAPPDIR:=/palworld}"

rest_info() {
    curl -fsS --max-time 5 --user "admin:${ADMIN_PASSWORD}" \
        "http://127.0.0.1:${REST_PORT}/v1/api/info" >/dev/null 2>&1
}

port_open() {
    # /proc based check avoids extra tooling; look for the UDP game port bound.
    local hexport
    hexport=$(printf '%04X' "${GAME_PORT}")
    grep -qi ":${hexport} " /proc/net/udp /proc/net/udp6 2>/dev/null
}

case "${MODE}" in
    startup)
        [ -f "${STEAMAPPDIR}/PalServer.sh" ] && port_open
        ;;
    liveness)
        port_open
        ;;
    readiness)
        if [ "${REST_ENABLED}" = "true" ] && [ -n "${ADMIN_PASSWORD}" ]; then
            rest_info
        else
            port_open
        fi
        ;;
    *)
        echo "unknown mode: ${MODE}" >&2
        exit 2
        ;;
esac
