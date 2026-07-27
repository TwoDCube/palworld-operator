#!/usr/bin/env bash
#
# Graceful shutdown for the Palworld dedicated server.
#
# Players get a countdown before the world is flushed and the server stops, so
# nobody is dropped mid-session by an unannounced restart. This script is the
# single chokepoint for that: it runs from the pod's preStop hook, which fires on
# *every* termination (settings change rolling the StatefulSet, version update,
# node drain, manual `oc delete pod`, scale to zero).
#
# Order of operations:
#   1. Single-flight guard -- see the SHUTDOWN_MARKER note below.
#   2. Skip the countdown when nobody is connected.
#   3. Broadcast the countdown every SHUTDOWN_WARN_INTERVAL_SECONDS.
#   4. REST announce -> save -> shutdown (flushes the world to disk).
#   5. Fall back to SIGINT to the server process (it saves on clean exit).
#
# Arg $1: server PID (used for the fallback path).
set -uo pipefail

log() { echo "[shutdown] $(date -u +%FT%TZ) $*"; }

SERVER_PID="${1:-}"
: "${REST_ENABLED:=true}"
: "${REST_PORT:=8212}"
: "${ADMIN_PASSWORD:=}"
: "${SHUTDOWN_WAIT_SECONDS:=5}"
: "${SHUTDOWN_WARN_SECONDS:=300}"
: "${SHUTDOWN_WARN_INTERVAL_SECONDS:=60}"
: "${SHUTDOWN_WARN_MESSAGE:=Server is shutting down for maintenance in %s}"
# Effective terminationGracePeriodSeconds, so the countdown can be clamped to the
# budget the kubelet actually allows. 0 disables clamping (standalone `docker run`).
: "${SHUTDOWN_GRACE_SECONDS:=0}"
: "${SHUTDOWN_MARKER:=/tmp/.palworld-shutdown-in-progress}"

# Time kept back for the save + clean shutdown after the countdown ends. Keep in
# step with ShutdownReserveSeconds in api/v1alpha1/palworldgame_types.go.
SHUTDOWN_RESERVE_SECONDS=30

# Fall back to the default when a value is not a non-negative integer: a typo in
# a hand-set extraEnv must not break arithmetic half-way through a termination.
numeric_or() {
    case "$1" in
        ''|*[!0-9]*) log "ignoring non-numeric value '$1'; using $2"; printf '%s' "$2" ;;
        *)           printf '%s' "$1" ;;
    esac
}

SHUTDOWN_WAIT_SECONDS="$(numeric_or "${SHUTDOWN_WAIT_SECONDS}" 5)"
SHUTDOWN_WARN_SECONDS="$(numeric_or "${SHUTDOWN_WARN_SECONDS}" 300)"
SHUTDOWN_WARN_INTERVAL_SECONDS="$(numeric_or "${SHUTDOWN_WARN_INTERVAL_SECONDS}" 60)"
SHUTDOWN_GRACE_SECONDS="$(numeric_or "${SHUTDOWN_GRACE_SECONDS}" 0)"

# --- single-flight guard ----------------------------------------------------
#
# This script is invoked twice per termination: once by the preStop hook and
# again by the entrypoint's SIGTERM trap. A second countdown would run the clock
# past terminationGracePeriodSeconds and get the pod SIGKILLed mid-save, so the
# first invocation claims the marker and later ones return immediately.
#
# `set -o noclobber` makes the claim atomic, so two invocations racing at the
# same instant cannot both win. Returning early is safe: SIGTERM is delivered to
# PID 1 only, never to the game binary, so the trap exiting leaves the first
# countdown running undisturbed.
if ! (set -o noclobber; : > "${SHUTDOWN_MARKER}") 2>/dev/null; then
    log "shutdown already in progress; not starting a second countdown"
    exit 0
fi

rest_call() {
    local method="$1" path="$2" body="${3:-}"
    curl -fsS --max-time 15 \
        --user "admin:${ADMIN_PASSWORD}" \
        -H 'Content-Type: application/json' \
        -X "${method}" \
        ${body:+-d "${body}"} \
        "http://127.0.0.1:${REST_PORT}${path}"
}

# Build the announce body with jq so quotes or backslashes in an operator's
# message cannot produce invalid JSON (a broken body means no warning at all).
json_message() {
    if command -v jq >/dev/null 2>&1; then
        jq -cn --arg m "$1" '{message:$m}'
        return
    fi
    printf '{"message":"%s"}' "${1//\"/}"
}

announce() {
    rest_call POST /v1/api/announce "$(json_message "$1")" >/dev/null 2>&1
}

# Remaining time in words: "5 minutes", "1 minute", "30 seconds". Whole minutes
# only, which is why the default warn interval is a minute.
humanize() {
    local secs="$1"
    if [ "${secs}" -ge 120 ]; then
        printf '%d minutes' "$(( secs / 60 ))"
    elif [ "${secs}" -ge 60 ]; then
        printf '1 minute'
    elif [ "${secs}" -eq 1 ]; then
        printf '1 second'
    else
        printf '%d seconds' "${secs}"
    fi
}

# Substitute placeholders by hand rather than via printf: an operator's message
# may contain a stray '%' that would make printf emit garbage or nothing.
render_message() {
    local template="$1" secs="$2" out
    out="${template//%s/$(humanize "${secs}")}"
    out="${out//%d/${secs}}"
    printf '%s' "${out}"
}

# Number of connected players, or empty when it cannot be determined.
player_count() {
    local body count
    body="$(rest_call GET /v1/api/players '' 2>/dev/null)" || return 1
    command -v jq >/dev/null 2>&1 || return 1
    count="$(printf '%s' "${body}" | jq -r '.players | length' 2>/dev/null)" || return 1
    case "${count}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    printf '%s' "${count}"
}

warn_players() {
    local remaining="$1" interval="$2" step
    while [ "${remaining}" -gt 0 ]; do
        log "warning players: ${remaining}s remaining"
        announce "$(render_message "${SHUTDOWN_WARN_MESSAGE}" "${remaining}")"
        step="${interval}"
        [ "${step}" -gt "${remaining}" ] && step="${remaining}"
        sleep "${step}"
        remaining=$(( remaining - step ))
    done
}

if [ "${REST_ENABLED}" = "true" ] && [ -n "${ADMIN_PASSWORD}" ]; then
    warn_seconds="${SHUTDOWN_WARN_SECONDS}"
    interval="${SHUTDOWN_WARN_INTERVAL_SECONDS}"
    # A 0 interval would spin the announce loop without advancing the countdown.
    [ "${interval}" -lt 1 ] && interval=1

    # Never let the countdown outlast the pod's real budget.
    if [ "${SHUTDOWN_GRACE_SECONDS}" -gt 0 ]; then
        budget=$(( SHUTDOWN_GRACE_SECONDS - SHUTDOWN_RESERVE_SECONDS ))
        [ "${budget}" -lt 0 ] && budget=0
        if [ "${warn_seconds}" -gt "${budget}" ]; then
            log "clamping warning ${warn_seconds}s -> ${budget}s to fit terminationGracePeriodSeconds=${SHUTDOWN_GRACE_SECONDS}s"
            warn_seconds="${budget}"
        fi
    fi

    if [ "${warn_seconds}" -gt 0 ]; then
        # An idle server should not stall a roll for minutes. On an unreadable
        # player list, assume someone may be on and warn anyway.
        players="$(player_count)" || players=""
        if [ "${players}" = "0" ]; then
            log "no players connected; skipping the ${warn_seconds}s warning"
        else
            log "warning ${players:-unknown} player(s) for ${warn_seconds}s every ${interval}s"
            warn_players "${warn_seconds}" "${interval}"
        fi
    fi

    log "announcing shutdown via REST"
    announce "Server is shutting down for maintenance"
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
else
    # No REST means no way to broadcast, so a countdown would only delay the stop
    # without warning anyone.
    log "REST unavailable; skipping the player warning"
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
