#!/usr/bin/env bash
#
# Graceful shutdown for the Palworld dedicated server.
#
# Order of preference:
#   1. REST API: announce -> save -> shutdown (flushes the world to disk).
#   2. Fall back to SIGINT to the server process (server saves on clean exit).
#
# Arg $1: server PID (used for the fallback path).
set -uo pipefail

log() { echo "[shutdown] $(date -u +%FT%TZ) $*"; }

SERVER_PID="${1:-}"
: "${REST_ENABLED:=true}"
: "${REST_PORT:=8212}"
: "${ADMIN_PASSWORD:=}"
: "${SHUTDOWN_WAIT_SECONDS:=5}"

rest_call() {
    local method="$1" path="$2" body="${3:-}"
    curl -fsS --max-time 15 \
        --user "admin:${ADMIN_PASSWORD}" \
        -H 'Content-Type: application/json' \
        -X "${method}" \
        ${body:+-d "${body}"} \
        "http://127.0.0.1:${REST_PORT}${path}"
}

if [ "${REST_ENABLED}" = "true" ] && [ -n "${ADMIN_PASSWORD}" ]; then
    log "announcing shutdown via REST"
    rest_call POST /v1/api/announce '{"message":"Server is shutting down for maintenance"}' || true
    log "flushing world save via REST"
    if rest_call POST /v1/api/save '' ; then
        log "save request accepted"
    else
        log "save request failed; continuing"
    fi
    sleep 2
    log "requesting clean shutdown via REST (wait ${SHUTDOWN_WAIT_SECONDS}s)"
    if rest_call POST /v1/api/shutdown "{\"waittime\":${SHUTDOWN_WAIT_SECONDS},\"message\":\"Server shutting down\"}" ; then
        log "shutdown request accepted; waiting for process to exit"
        exit 0
    fi
    log "REST shutdown failed; falling back to signal"
fi

# PalServer.sh does not exec the game binary, so the wrapper PID passed in is
# not the process that must receive SIGINT to save. Signal the real shipping
# binary directly, falling back to the wrapper PID.
log "sending SIGINT to the server process for a clean save-on-exit"
if command -v pkill >/dev/null 2>&1 && pkill -INT -f 'PalServer-Linux-Shipping' 2>/dev/null; then
    :
elif [ -n "${SERVER_PID}" ] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill -INT "${SERVER_PID}" 2>/dev/null || true
fi
exit 0
