#!/usr/bin/env bash
#
# Restore a Palworld world from a backup. Runs inside a Job scheduled by the
# operator while the game is stopped.
#
# Usage:
#   restore.sh pvc <src-file>    extract a tar.gz from a local path (backup PVC)
#   restore.sh s3                stream+extract a tar.gz from an S3 remote
#
# Env: same as backup.sh, plus DATA_DIR as the restore target.
set -euo pipefail

log() { echo "[restore] $(date -u +%FT%TZ) $*"; }

MODE="${1:-}"
: "${STEAMAPPDIR:=/palworld}"
: "${DATA_DIR:=${STEAMAPPDIR}/Pal/Saved}"

configure_rclone() {
    export RCLONE_CONFIG_S3_TYPE=s3
    export RCLONE_CONFIG_S3_PROVIDER="${S3_PROVIDER:-Other}"
    export RCLONE_CONFIG_S3_ENV_AUTH=true
    export RCLONE_CONFIG_S3_REGION="${S3_REGION:-}"
    if [ -n "${S3_ENDPOINT:-}" ]; then
        export RCLONE_CONFIG_S3_ENDPOINT="${S3_ENDPOINT}"
    fi
    if [ "${S3_INSECURE_TLS:-false}" = "true" ]; then
        export RCLONE_CONFIG_S3_NO_CHECK_CERTIFICATE=true
    fi
}

prepare_target() {
    log "clearing restore target ${DATA_DIR}"
    mkdir -p "${DATA_DIR}"
    # Remove existing contents but keep the directory (it may be a mount point).
    find "${DATA_DIR}" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
}

case "${MODE}" in
    pvc)
        SRC="${2:?source file required}"
        [ -f "${SRC}" ] || { log "ERROR: source ${SRC} not found"; exit 1; }
        prepare_target
        log "extracting ${SRC} -> ${DATA_DIR}"
        tar -xzf "${SRC}" -C "${DATA_DIR}"
        ;;
    s3)
        : "${S3_BUCKET:?S3_BUCKET required}"
        : "${S3_KEY:?S3_KEY required}"
        configure_rclone
        REMOTE="s3:${S3_BUCKET}/${S3_PREFIX:+${S3_PREFIX}/}${S3_KEY}"
        prepare_target
        log "streaming ${REMOTE} -> ${DATA_DIR}"
        rclone cat "${REMOTE}" | tar -xzf - -C "${DATA_DIR}"
        ;;
    *)
        log "ERROR: unknown mode '${MODE}' (expected pvc|s3)"
        exit 2
        ;;
esac
log "restore complete"
