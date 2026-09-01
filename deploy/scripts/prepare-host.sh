#!/usr/bin/env bash
# ==============================================================================
# Host Storage Preparation Script for Backup Platform (Internal V1)
# ==============================================================================
# Prepares canonical application storage (/srv/backup-platform) owned by the
# non-root application container (UID:GID 10001:10001) and isolated metadata
# backup storage (/var/backups/backup-platform) owned by root:root.
#
# Target OS: Ubuntu 22.04 LTS / Debian 12 / Compatible Linux
# Execution: sudo ./prepare-host.sh (Must be run only while app container is stopped)
# ==============================================================================

set -euo pipefail

# 1. Require root privileges
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: prepare-host.sh must be run as root (or with sudo)." >&2
    exit 1
fi

# 2. Enforce zero arguments (Internal V1 uses frozen canonical paths only)
if [[ $# -gt 0 ]]; then
    echo "ERROR: prepare-host.sh takes no arguments in canonical deployment." >&2
    exit 1
fi

# 3. Stopped-App Guard: prepare-host must NOT run while application container is active
APP_CONTAINER="backup-platform-app"
if command -v docker >/dev/null 2>&1; then
    if docker inspect --format '{{.State.Running}}' "${APP_CONTAINER}" 2>/dev/null | grep -q 'true'; then
        echo "ERROR: Application container '${APP_CONTAINER}' is currently running." >&2
        echo "       Please stop the container before preparing host storage (e.g. docker compose stop app)." >&2
        exit 1
    fi
fi

APP_UID=10001
APP_GID=10001
APP_STORAGE_ROOT="/srv/backup-platform"
METADATA_BACKUP_ROOT="/var/backups/backup-platform"

echo "==> Preparing Backup Platform canonical storage paths..."

# 4. Prevent Symlink Hijacking on Application Storage
if [[ -L "${APP_STORAGE_ROOT}" ]]; then
    echo "ERROR: Target path '${APP_STORAGE_ROOT}' is a symbolic link. Symlinks are forbidden." >&2
    exit 1
fi

# 5. Create canonical application directory structure idempotently
mkdir -p "${APP_STORAGE_ROOT}"
mkdir -p "${APP_STORAGE_ROOT}/tmp"
mkdir -p "${APP_STORAGE_ROOT}/organizations"

# 6. Verify no application storage subdirectory is a symlink
for subdir in "${APP_STORAGE_ROOT}/tmp" "${APP_STORAGE_ROOT}/organizations"; do
    if [[ -L "${subdir}" ]]; then
        echo "ERROR: Storage subdirectory '${subdir}' is a symbolic link. Aborting." >&2
        exit 1
    fi
done

# 7. Configure non-root application storage permissions (0700, non-recursive)
# Owned by non-root container user 10001:10001
echo "==> Setting ownership to ${APP_UID}:${APP_GID} and permissions to 0700 for application storage"
chown "${APP_UID}:${APP_GID}" "${APP_STORAGE_ROOT}"
chmod 0700 "${APP_STORAGE_ROOT}"

chown "${APP_UID}:${APP_GID}" "${APP_STORAGE_ROOT}/tmp"
chmod 0700 "${APP_STORAGE_ROOT}/tmp"

chown "${APP_UID}:${APP_GID}" "${APP_STORAGE_ROOT}/organizations"
chmod 0700 "${APP_STORAGE_ROOT}/organizations"

# 8. Prepare isolated root metadata backup directory (outside application storage mount)
if [[ -L "/var/backups" ]]; then
    echo "ERROR: System directory '/var/backups' is a symbolic link. Aborting." >&2
    exit 1
fi

if [[ -L "${METADATA_BACKUP_ROOT}" ]]; then
    echo "ERROR: Metadata backup path '${METADATA_BACKUP_ROOT}' is a symbolic link. Aborting." >&2
    exit 1
fi

mkdir -p "${METADATA_BACKUP_ROOT}"
echo "==> Setting root:root ownership and 0700 permissions for metadata backups"
chown root:root "${METADATA_BACKUP_ROOT}"
chmod 0700 "${METADATA_BACKUP_ROOT}"

# 9. Verification of storage paths
echo "==> Verification of storage paths:"
ls -ld "${APP_STORAGE_ROOT}" \
      "${APP_STORAGE_ROOT}/tmp" \
      "${APP_STORAGE_ROOT}/organizations" \
      "${METADATA_BACKUP_ROOT}"

echo "==> Host storage preparation completed successfully."
