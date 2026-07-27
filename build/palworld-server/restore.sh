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
#
# Safety: the archive is extracted into a staging directory FIRST; the live
# world is only cleared and replaced once extraction has fully succeeded, so a
# corrupt or truncated backup never destroys the existing save.
set -euo pipefail

log() { echo "[restore] $(date -u +%FT%TZ) $*"; }

MODE="${1:-}"
: "${STEAMAPPDIR:=/palworld}"
: "${DATA_DIR:=${STEAMAPPDIR}/Pal/Saved}"

STAGING="${DATA_DIR}.restore-staging"

configure_rclone() {
    export RCLONE_CONFIG_S3_TYPE=s3
    export RCLONE_CONFIG_S3_PROVIDER="${S3_PROVIDER:-Other}"
    export RCLONE_CONFIG_S3_ENV_AUTH=true
    export RCLONE_CONFIG_S3_REGION="${S3_REGION:-}"
    # Kept identical to backup.sh: reads do not probe the bucket today, but the
    # operator never creates one either, so CreateBucket stays out of the
    # required credentials permissions. See backup.sh for the failure mode.
    export RCLONE_CONFIG_S3_NO_CHECK_BUCKET=true
    if [ -n "${S3_ENDPOINT:-}" ]; then
        export RCLONE_CONFIG_S3_ENDPOINT="${S3_ENDPOINT}"
    fi
    if [ "${S3_INSECURE_TLS:-false}" = "true" ]; then
        export RCLONE_CONFIG_S3_NO_CHECK_CERTIFICATE=true
    fi
}

reset_staging() {
    rm -rf "${STAGING}"
    mkdir -p "${STAGING}"
}

# swap_into_place is only reached after a successful extraction into STAGING.
swap_into_place() {
    log "extraction succeeded; swapping restored data into ${DATA_DIR}"
    mkdir -p "${DATA_DIR}"
    find "${DATA_DIR}" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
    find "${STAGING}" -mindepth 1 -maxdepth 1 -exec mv -t "${DATA_DIR}" {} +
    rm -rf "${STAGING}"
}

case "${MODE}" in
    pvc)
        SRC="${2:?source file required}"
        [ -f "${SRC}" ] || { log "ERROR: source ${SRC} not found"; exit 1; }
        reset_staging
        log "extracting ${SRC} -> ${STAGING}"
        tar -xzf "${SRC}" -C "${STAGING}"
        swap_into_place
        ;;
    s3)
        : "${S3_BUCKET:?S3_BUCKET required}"
        : "${S3_KEY:?S3_KEY required}"
        configure_rclone
        REMOTE="s3:${S3_BUCKET}/${S3_PREFIX:+${S3_PREFIX}/}${S3_KEY}"
        reset_staging
        log "streaming ${REMOTE} -> ${STAGING}"
        rclone cat "${REMOTE}" | tar -xzf - -C "${STAGING}"
        swap_into_place
        ;;
    *)
        log "ERROR: unknown mode '${MODE}' (expected pvc|s3)"
        exit 2
        ;;
esac
log "restore complete"
