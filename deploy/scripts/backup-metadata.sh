#!/usr/bin/env bash
# ==============================================================================
# PostgreSQL Metadata Backup Script for Backup Platform (Internal V1)
# ==============================================================================
# Creates an atomic, verified PostgreSQL metadata dump using pg_dump from the
# running Docker PostgreSQL container (backup-platform-postgres).
#
# Production Installation: /usr/local/sbin/backup-platform-metadata-backup
# Execution: sudo /usr/local/sbin/backup-platform-metadata-backup
# ==============================================================================

set -euo pipefail

# 1. Secure file creation from first byte (0600 file permissions under umask 077)
umask 077

# 2. Require root privileges
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: backup-metadata.sh must be run as root (or with sudo)." >&2
    exit 1
fi

# 3. Enforce zero arguments (Scheduled root backup uses fixed canonical configuration)
if [[ $# -gt 0 ]]; then
    echo "ERROR: backup-metadata.sh takes no arguments in canonical deployment." >&2
    exit 1
fi

# 4. Mandatory flock Concurrency Protection (Fail-closed if flock is unavailable)
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
chown root:root "${LOCK_DIR}"
chmod 0700 "${LOCK_DIR}"

LOCK_FILE="${LOCK_DIR}/metadata-backup.lock"
if [[ -L "${LOCK_FILE}" ]]; then
    echo "ERROR: Lock file '${LOCK_FILE}' is a symbolic link. Aborting." >&2
    exit 1
fi
touch "${LOCK_FILE}"
chown root:root "${LOCK_FILE}"
chmod 0600 "${LOCK_FILE}"

exec 200>"${LOCK_FILE}"
if ! flock -n 200; then
    echo "ERROR: Another metadata backup process is currently holding the lock." >&2
    exit 1
fi

# 5. Canonical Directory & Target Validation
BACKUP_DIR="/srv/backup-platform/metadata-backups"
POSTGRES_CONTAINER="backup-platform-postgres"

if [[ -L "${BACKUP_DIR}" ]]; then
    echo "ERROR: Metadata backup directory '${BACKUP_DIR}' is a symbolic link. Aborting." >&2
    exit 1
fi

mkdir -p "${BACKUP_DIR}"
chown root:root "${BACKUP_DIR}"
chmod 0700 "${BACKUP_DIR}"

# 6. Verify PostgreSQL Container is Available and Running
if ! docker inspect --format '{{.State.Running}}' "${POSTGRES_CONTAINER}" 2>/dev/null | grep -q 'true'; then
    echo "ERROR: PostgreSQL container '${POSTGRES_CONTAINER}' is not running." >&2
    exit 1
fi

TIMESTAMP="$(date -u +"%Y%m%d-%H%M%S")"
FINAL_DUMP_FILE="${BACKUP_DIR}/backup-platform-metadata-${TIMESTAMP}.dump"
PARTIAL_DUMP_FILE="${FINAL_DUMP_FILE}.partial"

# 7. Collision Protection (Prevent accidental overwrite of existing dump)
if [[ -e "${FINAL_DUMP_FILE}" ]]; then
    echo "ERROR: Target dump file '${FINAL_DUMP_FILE}' already exists. Aborting to prevent overwrite." >&2
    exit 1
fi

if [[ -e "${PARTIAL_DUMP_FILE}" ]]; then
    echo "ERROR: Target partial dump file '${PARTIAL_DUMP_FILE}' already exists. Aborting to prevent overwrite." >&2
    exit 1
fi

# 8. Cleanup trap for partial files on unexpected failure
cleanup() {
    local exit_code=$?
    if [[ ${exit_code} -ne 0 && -f "${PARTIAL_DUMP_FILE}" ]]; then
        echo "WARN: Metadata backup interrupted or failed; removing partial dump file." >&2
        rm -f "${PARTIAL_DUMP_FILE}"
    fi
}
trap cleanup EXIT

echo "==> Initiating Backup Platform metadata dump at ${TIMESTAMP} UTC..."

# 9. Execute pg_dump inside the isolated PostgreSQL container using in-container env
if ! docker exec -i "${POSTGRES_CONTAINER}" \
    sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "${PARTIAL_DUMP_FILE}"; then
    echo "ERROR: pg_dump failed while exporting database metadata." >&2
    exit 1
fi

# 10. Verify dump file size is non-zero
if [[ ! -s "${PARTIAL_DUMP_FILE}" ]]; then
    echo "ERROR: Generated metadata dump is empty (0 bytes)." >&2
    rm -f "${PARTIAL_DUMP_FILE}"
    exit 1
fi

# 11. Structural validation of generated archive using pg_restore --list
echo "==> Validating metadata dump archive structure..."
if ! cat "${PARTIAL_DUMP_FILE}" | docker exec -i "${POSTGRES_CONTAINER}" \
    sh -c 'pg_restore --list' >/dev/null; then
    echo "ERROR: Metadata dump archive validation (pg_restore --list) failed. Dump is corrupt." >&2
    rm -f "${PARTIAL_DUMP_FILE}"
    exit 1
fi

# 12. Atomic rename to final timestamped .dump file
mv "${PARTIAL_DUMP_FILE}" "${FINAL_DUMP_FILE}"
chmod 0600 "${FINAL_DUMP_FILE}"

DUMP_SIZE="$(wc -c < "${FINAL_DUMP_FILE}" | tr -d ' ')"
echo "==> Metadata backup created and verified successfully:"
echo "    File: ${FINAL_DUMP_FILE}"
echo "    Size: ${DUMP_SIZE} bytes"
