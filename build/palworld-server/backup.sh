#!/usr/bin/env bash
#
# Back up a Palworld world. Runs inside a Job scheduled by the operator.
#
# Usage:
#   backup.sh pvc  <dest-file>        tar.gz the save dir to a local path (backup PVC)
#   backup.sh s3                       stream tar.gz to an S3 remote (via rclone)
#
# Env:
#   DATA_DIR          source dir to back up (default ${STEAMAPPDIR}/Pal/Saved)
#   S3_BUCKET,S3_PREFIX,S3_KEY         S3 destination (s3 mode)
#   S3_ENDPOINT,S3_REGION              S3 endpoint/region (s3 mode)
#   AWS_ACCESS_KEY_ID,AWS_SECRET_ACCESS_KEY  S3 credentials (s3 mode)
#   S3_INSECURE_TLS   "true" to skip TLS verification (test only)
#
set -euo pipefail

log() { echo "[backup] $(date -u +%FT%TZ) $*"; }

MODE="${1:-}"
: "${STEAMAPPDIR:=/palworld}"
: "${DATA_DIR:=${STEAMAPPDIR}/Pal/Saved}"

if [ ! -d "${DATA_DIR}" ]; then
    log "ERROR: data dir ${DATA_DIR} does not exist"
    exit 1
fi

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

case "${MODE}" in
    pvc)
        DEST="${2:?destination file required}"
        mkdir -p "$(dirname "${DEST}")"
        log "archiving ${DATA_DIR} -> ${DEST}"
        tar -czf "${DEST}" -C "${DATA_DIR}" .
        SIZE=$(stat -c '%s' "${DEST}")
        log "backup complete: ${SIZE} bytes"
        echo "${SIZE}" > /tmp/backup-size
        ;;
    s3)
        : "${S3_BUCKET:?S3_BUCKET required}"
        : "${S3_KEY:?S3_KEY required}"
        configure_rclone
        REMOTE="s3:${S3_BUCKET}/${S3_PREFIX:+${S3_PREFIX}/}${S3_KEY}"
        log "streaming ${DATA_DIR} -> ${REMOTE}"
        tar -czf - -C "${DATA_DIR}" . | rclone rcat "${REMOTE}"
        SIZE=$(rclone size --json "${REMOTE}" | jq -r '.bytes' 2>/dev/null || echo 0)
        log "backup complete: ${SIZE} bytes at ${REMOTE}"
        echo "${SIZE}" > /tmp/backup-size
        ;;
    *)
        log "ERROR: unknown mode '${MODE}' (expected pvc|s3)"
        exit 2
        ;;
esac
