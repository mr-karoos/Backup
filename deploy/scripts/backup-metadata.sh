#!/usr/bin/env bash
# ==============================================================================
# PostgreSQL Metadata Backup Script for Backup Platform (Internal V1)
# ==============================================================================
# Creates an atomic, verified PostgreSQL metadata dump using pg_dump from the
# isolated Docker Compose PostgreSQL container.
#
# Execution: sudo ./backup-metadata.sh [OPTIONAL_COMPOSE_DIR]
# ==============================================================================

set -euo pipefail

# 1. Require root privileges
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: backup-metadata.sh must be run as root (or with sudo)." >&2
    exit 1
fi

# 2. Mandatory flock Concurrency Protection (Fail-closed if flock is unavailable)
if ! command -v flock >/dev/null 2>&1; then
    echo "ERROR: 'flock' command is missing on this host. Concurrency lock cannot be acquired." >&2
    exit 1
fi

LOCK_DIR="/run/backup-platform"
if [[ -L "${LOCK_DIR}" ]]; then
    echo "ERROR: Lock directory '${LOCK_DIR}' is a symbolic link. Aborting." >&2
    exit 1
fi
mkdir -p "${LOCK_DIR}"
chmod 0700 "${LOCK_DIR}"

LOCK_FILE="${LOCK_DIR}/metadata-backup.lock"
if [[ -L "${LOCK_FILE}" ]]; then
    echo "ERROR: Lock file '${LOCK_FILE}' is a symbolic link. Aborting." >&2
    exit 1
fi

exec 200>"${LOCK_FILE}"
if ! flock -n 200; then
    echo "ERROR: Another metadata backup process is currently holding the lock." >&2
    exit 1
fi

# 3. Path & Environment Validation
COMPOSE_DIR="${1:-/opt/backup-platform}"
COMPOSE_FILE="${COMPOSE_DIR}/compose.yaml"
ENV_FILE="${COMPOSE_DIR}/deploy/.env"
BACKUP_DIR="${BACKUP_METADATA_DIR:-/srv/backup-platform/metadata-backups}"

if [[ ! -f "${COMPOSE_FILE}" ]]; then
    echo "ERROR: compose.yaml not found at '${COMPOSE_FILE}'." >&2
    exit 1
fi

if [[ ! -f "${ENV_FILE}" ]]; then
    echo "ERROR: Environment file '${ENV_FILE}' does not exist. Please prepare deploy/.env first." >&2
    exit 1
fi

# Validate target backup directory path
if [[ "${BACKUP_DIR}" != /* ]]; then
    echo "ERROR: BACKUP_METADATA_DIR must be an absolute path: '${BACKUP_DIR}'" >&2
    exit 1
fi

case "${BACKUP_DIR}" in
    "/" | "/etc" | "/etc/"* | "/usr" | "/usr/"* | "/var" | "/var/"* | \
    "/home" | "/home/"* | "/root" | "/root/"* | "/opt" | "/tmp" | "/tmp/"* | \
    "/run" | "/run/"* | "/srv" | "/bin" | "/sbin" | "/lib" | "/lib64")
        echo "ERROR: BACKUP_METADATA_DIR cannot be a root or system directory: '${BACKUP_DIR}'" >&2
        exit 1
        ;;
esac

if [[ -L "${BACKUP_DIR}" ]]; then
    echo "ERROR: BACKUP_METADATA_DIR '${BACKUP_DIR}' is a symbolic link. Aborting." >&2
    exit 1
fi

mkdir -p "${BACKUP_DIR}"
chmod 0700 "${BACKUP_DIR}"

TIMESTAMP="$(date -u +"%Y%m%d-%H%M%S")"
FINAL_DUMP_FILE="${BACKUP_DIR}/backup-platform-metadata-${TIMESTAMP}.dump"
PARTIAL_DUMP_FILE="${FINAL_DUMP_FILE}.partial"

# 4. Cleanup trap for partial files on unexpected failure
cleanup() {
    local exit_code=$?
    if [[ ${exit_code} -ne 0 && -f "${PARTIAL_DUMP_FILE}" ]]; then
        echo "WARN: Metadata backup interrupted or failed; removing partial dump file." >&2
        rm -f "${PARTIAL_DUMP_FILE}"
    fi
}
trap cleanup EXIT

echo "==> Initiating Backup Platform metadata dump at ${TIMESTAMP} UTC..."

# 5. Execute pg_dump inside the isolated PostgreSQL container using in-container env
if ! docker compose \
    --env-file "${ENV_FILE}" \
    -f "${COMPOSE_FILE}" \
    exec -T postgres \
    sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "${PARTIAL_DUMP_FILE}"; then
    echo "ERROR: pg_dump failed while exporting database metadata." >&2
    exit 1
fi

# 6. Set strict POSIX file permissions (0600)
chmod 0600 "${PARTIAL_DUMP_FILE}"

# 7. Verify dump file size is non-zero
if [[ ! -s "${PARTIAL_DUMP_FILE}" ]]; then
    echo "ERROR: Generated metadata dump is empty (0 bytes)." >&2
    rm -f "${PARTIAL_DUMP_FILE}"
    exit 1
fi

# 8. Structural validation of generated archive using pg_restore --list
echo "==> Validating metadata dump archive structure..."
if ! cat "${PARTIAL_DUMP_FILE}" | docker compose \
    --env-file "${ENV_FILE}" \
    -f "${COMPOSE_FILE}" \
    exec -T postgres \
    sh -c 'pg_restore --list' >/dev/null; then
    echo "ERROR: Metadata dump archive validation (pg_restore --list) failed. Dump is corrupt." >&2
    rm -f "${PARTIAL_DUMP_FILE}"
    exit 1
fi

# 9. Atomic rename to final timestamped .dump file
mv "${PARTIAL_DUMP_FILE}" "${FINAL_DUMP_FILE}"
chmod 0600 "${FINAL_DUMP_FILE}"

DUMP_SIZE="$(wc -c < "${FINAL_DUMP_FILE}" | tr -d ' ')"
echo "==> Metadata backup created and verified successfully:"
echo "    File: ${FINAL_DUMP_FILE}"
echo "    Size: ${DUMP_SIZE} bytes"
