#!/usr/bin/env bash
# ==============================================================================
# PostgreSQL Metadata Backup Script for Backup Platform (Internal V1)
# ==============================================================================
# Creates an atomic, consistent PostgreSQL metadata dump using pg_dump
# from the running Docker Compose PostgreSQL container.
#
# Execution: sudo ./backup-metadata.sh [OPTIONAL_COMPOSE_DIR]
# ==============================================================================

set -euo pipefail

# 1. Configuration and paths
COMPOSE_DIR="${1:-/opt/backup-platform}"
BACKUP_DIR="${BACKUP_METADATA_DIR:-/srv/backup-platform/metadata-backups}"
LOCK_FILE="/tmp/backup-platform-metadata.lock"
TIMESTAMP="$(date -u +"%Y%m%d-%H%M%S")"
FINAL_DUMP_FILE="${BACKUP_DIR}/backup-platform-metadata-${TIMESTAMP}.dump"
PARTIAL_DUMP_FILE="${FINAL_DUMP_FILE}.partial"

# 2. Concurrency Protection (Prevent overlapping backup jobs)
exec 200>"${LOCK_FILE}"
if command -v flock >/dev/null 2>&1; then
    if ! flock -n 200; then
        echo "ERROR: Another metadata backup process is currently running." >&2
        exit 1
    fi
fi

# 3. Ensure target backup directory exists with strict permissions
if [[ ! -d "${BACKUP_DIR}" ]]; then
    mkdir -p "${BACKUP_DIR}"
    chmod 0700 "${BACKUP_DIR}"
fi

# 4. Cleanup trap for partial files on unexpected failure
cleanup() {
    local exit_code=$?
    if [[ ${exit_code} -ne 0 && -f "${PARTIAL_DUMP_FILE}" ]]; then
        echo "WARN: Metadata backup interrupted or failed; removing partial dump file." >&2
        rm -f "${PARTIAL_DUMP_FILE}"
    fi
}
trap cleanup EXIT

# 5. Extract database parameters from deploy/.env if available
ENV_FILE="${COMPOSE_DIR}/deploy/.env"
DB_USER="backup_platform"
DB_NAME="backup_platform"

if [[ -f "${ENV_FILE}" ]]; then
    # Parse non-empty values without evaluating arbitrary code
    PARSED_USER="$(grep -E '^POSTGRES_USER=' "${ENV_FILE}" | cut -d '=' -f2- | tr -d '"' | tr -d "'" || true)"
    PARSED_DB="$(grep -E '^POSTGRES_DB=' "${ENV_FILE}" | cut -d '=' -f2- | tr -d '"' | tr -d "'" || true)"
    if [[ -n "${PARSED_USER}" ]]; then DB_USER="${PARSED_USER}"; fi
    if [[ -n "${PARSED_DB}" ]]; then DB_NAME="${PARSED_DB}"; fi
fi

echo "==> Initiating Backup Platform metadata dump at ${TIMESTAMP} UTC..."

# 6. Execute pg_dump inside the isolated PostgreSQL container
# Uses custom compressed archive format (-Fc)
if ! docker compose -f "${COMPOSE_DIR}/compose.yaml" exec -T postgres \
    pg_dump -U "${DB_USER}" -d "${DB_NAME}" -Fc > "${PARTIAL_DUMP_FILE}"; then
    echo "ERROR: pg_dump failed while exporting PostgreSQL database metadata." >&2
    exit 1
fi

# 7. Set strict POSIX file permissions (0600)
chmod 0600 "${PARTIAL_DUMP_FILE}"

# 8. Verify dump file size is non-zero
if [[ ! -s "${PARTIAL_DUMP_FILE}" ]]; then
    echo "ERROR: Generated metadata dump is empty (0 bytes)." >&2
    rm -f "${PARTIAL_DUMP_FILE}"
    exit 1
fi

# 9. Atomic rename from .partial to final timestamped .dump file
mv "${PARTIAL_DUMP_FILE}" "${FINAL_DUMP_FILE}"

DUMP_SIZE="$(wc -c < "${FINAL_DUMP_FILE}" | tr -d ' ')"
echo "==> Metadata backup created successfully:"
echo "    File: ${FINAL_DUMP_FILE}"
echo "    Size: ${DUMP_SIZE} bytes"
