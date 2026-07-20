#!/usr/bin/env bash
#
# Palworld dedicated server entrypoint.
#
# Responsibilities:
#   1. Make an arbitrary (OpenShift-assigned) UID usable (/etc/passwd entry).
#   2. Install / update the server binaries via SteamCMD onto the data volume.
#   3. Render PalWorldSettings.ini from the operator-provided config, injecting
#      secret values (passwords) at runtime rather than baking them into a
#      ConfigMap.
#   4. Launch the server with the correct ports and performance flags.
#   5. Perform a graceful, save-flushing shutdown on SIGTERM/SIGINT.
#
set -euo pipefail

log() { echo "[entrypoint] $(date -u +%FT%TZ) $*"; }

: "${STEAM_APP_ID:=2394010}"
: "${STEAMAPPDIR:=/palworld}"
: "${STEAM_BRANCH:=public}"
: "${STEAM_BETA_PASSWORD:=}"
: "${VALIDATE_ON_START:=false}"
: "${SKIP_UPDATE:=false}"
: "${GAME_PORT:=8211}"
: "${QUERY_PORT:=27015}"
: "${RCON_ENABLED:=true}"
: "${RCON_PORT:=25575}"
: "${REST_ENABLED:=true}"
: "${REST_PORT:=8212}"
: "${MAX_PLAYERS:=32}"
: "${MULTITHREAD_ENABLED:=true}"
: "${PUBLIC_IP:=}"
: "${PUBLIC_PORT:=}"
: "${ADMIN_PASSWORD:=}"
: "${SERVER_PASSWORD:=}"
: "${RCON_PASSWORD:=${ADMIN_PASSWORD}}"
: "${EXTRA_ARGS:=}"
: "${SETTINGS_SOURCE:=/config/PalWorldSettings.ini}"
: "${ENGINE_SOURCE:=/config/Engine.ini}"

STEAMCMD_DIR=/steamcmd
CONFIG_DIR="${STEAMAPPDIR}/Pal/Saved/Config/LinuxServer"
SERVER_BIN="${STEAMAPPDIR}/PalServer.sh"

# --- 1. Arbitrary UID support (OpenShift restricted-v2) -----------------------
# When running as a UID with no /etc/passwd entry, append one so SteamCMD (which
# calls getpwuid) works and $HOME resolves. /etc/passwd is made group-writable
# (GID 0) in the image, which is the standard OpenShift arbitrary-UID pattern.
current_uid="$(id -u)"
current_gid="$(id -g)"
export HOME="${HOME:-/home/steam}"
if ! whoami >/dev/null 2>&1; then
    log "running as unmapped uid ${current_uid}; registering a passwd entry"
    if [ ! -w "${HOME}" ]; then
        export HOME=/tmp
    fi
    if [ -w /etc/passwd ]; then
        printf 'steam:x:%s:%s:steam:%s:/bin/bash\n' "${current_uid}" "${current_gid}" "${HOME}" >> /etc/passwd
    else
        log "WARNING: /etc/passwd is not writable; SteamCMD may warn about the missing user"
    fi
fi

mkdir -p "${STEAMAPPDIR}" "${CONFIG_DIR}" "${HOME}"

# --- 2. Install / update via SteamCMD -----------------------------------------
install_server() {
    if [ "${SKIP_UPDATE}" = "true" ] && [ -f "${SERVER_BIN}" ]; then
        log "SKIP_UPDATE=true and server already installed; skipping SteamCMD"
        return 0
    fi
    local beta_args=""
    if [ "${STEAM_BRANCH}" != "public" ]; then
        beta_args="-beta ${STEAM_BRANCH}"
        if [ -n "${STEAM_BETA_PASSWORD}" ]; then
            beta_args="${beta_args} -betapassword ${STEAM_BETA_PASSWORD}"
        fi
    fi
    local validate=""
    if [ "${VALIDATE_ON_START}" = "true" ]; then
        validate="validate"
    fi
    log "installing/updating app ${STEAM_APP_ID} (branch=${STEAM_BRANCH}) into ${STEAMAPPDIR}"
    # Retry a few times: SteamCMD occasionally fails with transient CDN errors.
    local attempt=0
    until "${STEAMCMD_DIR}/steamcmd.sh" \
            +@sSteamCmdForcePlatformType linux \
            +force_install_dir "${STEAMAPPDIR}" \
            +login anonymous \
            +app_update "${STEAM_APP_ID}" ${beta_args} ${validate} \
            +quit; do
        attempt=$((attempt + 1))
        if [ "${attempt}" -ge 5 ]; then
            log "SteamCMD failed after ${attempt} attempts"
            return 1
        fi
        log "SteamCMD attempt ${attempt} failed; retrying in $((attempt * 5))s"
        sleep "$((attempt * 5))"
    done
    # Record the installed build id for the operator to surface as the version.
    local manifest="${STEAMAPPDIR}/steamapps/appmanifest_${STEAM_APP_ID}.acf"
    if [ -f "${manifest}" ]; then
        grep -oP '"buildid"\s+"\K[0-9]+' "${manifest}" | head -n1 > "${STEAMAPPDIR}/.buildid" || true
        log "installed build id: $(cat "${STEAMAPPDIR}/.buildid" 2>/dev/null || echo unknown)"
    fi
}
install_server

# Palworld also needs the steamclient shared library discoverable.
mkdir -p "${HOME}/.steam/sdk64"
if [ -f "${STEAMCMD_DIR}/linux64/steamclient.so" ]; then
    cp -f "${STEAMCMD_DIR}/linux64/steamclient.so" "${HOME}/.steam/sdk64/steamclient.so" || true
fi

# --- 3. Render PalWorldSettings.ini -------------------------------------------
render_settings() {
    if [ ! -f "${SETTINGS_SOURCE}" ]; then
        log "no settings provided at ${SETTINGS_SOURCE}; using server defaults"
        return 0
    fi
    log "rendering ${CONFIG_DIR}/PalWorldSettings.ini"
    umask 0002
    # Literal token substitution via awk + ENVIRON. Passing the secrets through
    # the environment (not awk -v) avoids awk's backslash-escape processing, and
    # index()/substr() do a purely literal replace, so passwords containing any
    # character (& \ / " etc.) are inserted verbatim regardless of the bash or
    # awk version.
    ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
    SERVER_PASSWORD="${SERVER_PASSWORD}" \
    RCON_PASSWORD="${RCON_PASSWORD}" \
    PUBLIC_IP="${PUBLIC_IP}" \
    awk '
    BEGIN {
        n = split("__PALWORLD_ADMIN_PASSWORD__ __PALWORLD_SERVER_PASSWORD__ __PALWORLD_RCON_PASSWORD__ __PALWORLD_PUBLIC_IP__", toks, " ")
        vals[1] = ENVIRON["ADMIN_PASSWORD"]; vals[2] = ENVIRON["SERVER_PASSWORD"]
        vals[3] = ENVIRON["RCON_PASSWORD"];  vals[4] = ENVIRON["PUBLIC_IP"]
    }
    {
        line = $0
        for (i = 1; i <= n; i++) {
            t = toks[i]; v = vals[i]; tl = length(t); out = ""
            while ((p = index(line, t)) > 0) {
                out = out substr(line, 1, p - 1) v
                line = substr(line, p + tl)
            }
            line = out line
        }
        print line
    }' "${SETTINGS_SOURCE}" > "${CONFIG_DIR}/PalWorldSettings.ini"
    if [ -f "${ENGINE_SOURCE}" ]; then
        cp -f "${ENGINE_SOURCE}" "${CONFIG_DIR}/Engine.ini"
    fi
}
render_settings

# --- 4. Launch the server -----------------------------------------------------
# Base args. The settings file drives the server name by default; a -servername
# override is appended only when PALWORLD_SERVER_NAME is set (as a single array
# element, so embedded spaces are preserved without shell-quoting hacks).
ARGS=(
    "-port=${GAME_PORT}"
    "-queryport=${QUERY_PORT}"
    "-players=${MAX_PLAYERS}"
    "-log"
)
if [ -n "${PALWORLD_SERVER_NAME:-}" ]; then
    ARGS+=("-servername=${PALWORLD_SERVER_NAME}")
fi
if [ "${RCON_ENABLED}" = "true" ]; then
    ARGS+=("-RCONEnabled=True" "-RCONPort=${RCON_PORT}")
fi
if [ "${REST_ENABLED}" = "true" ]; then
    ARGS+=("-RESTAPIEnabled=True" "-RESTAPIPort=${REST_PORT}")
fi
if [ -n "${PUBLIC_IP}" ]; then
    ARGS+=("-publicip=${PUBLIC_IP}")
fi
if [ -n "${PUBLIC_PORT}" ]; then
    ARGS+=("-publicport=${PUBLIC_PORT}")
fi
if [ "${MULTITHREAD_ENABLED}" = "true" ]; then
    ARGS+=("-useperfthreads" "-NoAsyncLoadingThread" "-UseMultithreadForDS=True")
fi
if [ -n "${EXTRA_ARGS}" ]; then
    # shellcheck disable=SC2206
    ARGS+=(${EXTRA_ARGS})
fi

export RCON_PORT ADMIN_PASSWORD RCON_PASSWORD REST_PORT REST_ENABLED RCON_ENABLED

# Graceful shutdown trap: on SIGTERM/SIGINT flush the save and stop cleanly.
SERVER_PID=""
term_handler() {
    log "received termination signal; starting graceful shutdown"
    /usr/local/bin/graceful-shutdown.sh "${SERVER_PID}" || true
    wait "${SERVER_PID}" 2>/dev/null || true
    exit 0
}
trap term_handler SIGTERM SIGINT

cd "${STEAMAPPDIR}"
log "starting PalServer: ${ARGS[*]}"
# PalServer.sh execs the real binary; run it in the background so we can trap.
"${SERVER_BIN}" "${ARGS[@]}" &
SERVER_PID=$!
log "server started with pid ${SERVER_PID}"
wait "${SERVER_PID}"
