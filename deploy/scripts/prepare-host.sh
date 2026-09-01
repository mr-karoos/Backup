#!/usr/bin/env bash
# ==============================================================================
# Host Storage Preparation Script for Backup Platform (Internal V1)
# ==============================================================================
# Prepares the frozen canonical host storage directory (/srv/backup-platform),
# ownership, and strict POSIX permissions for the non-root application
# container (UID:GID 10001:10001) and root-owned metadata backups.
#
# Target OS: Ubuntu 22.04 LTS / Debian 12 / Compatible Linux
# Execution: sudo ./prepare-host.sh
# ==============================================================================

set -euo pipefail

# 1. Require root privileges
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: prepare-host.sh must be run as root (or with sudo)." >&2
    exit 1
fi

# 2. Enforce zero arguments (Internal V1 uses frozen canonical path only)
if [[ $# -gt 0 ]]; then
    echo "ERROR: prepare-host.sh takes no arguments in canonical deployment." >&2
    exit 1
fi

APP_UID=10001
APP_GID=10001
STORAGE_ROOT="/srv/backup-platform"

# 3. Prevent Symlink Hijacking on Target Root
if [[ -L "${STORAGE_ROOT}" ]]; then
    echo "ERROR: Target path '${STORAGE_ROOT}' is a symbolic link. Symlinks are forbidden." >&2
    exit 1
fi

echo "==> Preparing canonical Backup Platform host storage directory: ${STORAGE_ROOT}"

# 4. Create canonical directory structure idempotently
mkdir -p "${STORAGE_ROOT}"
mkdir -p "${STORAGE_ROOT}/tmp"
mkdir -p "${STORAGE_ROOT}/organizations"
mkdir -p "${STORAGE_ROOT}/metadata-backups"

# 5. Verify no subdirectory is a symlink
for subdir in "${STORAGE_ROOT}/tmp" "${STORAGE_ROOT}/organizations" "${STORAGE_ROOT}/metadata-backups"; do
    if [[ -L "${subdir}" ]]; then
        echo "ERROR: Storage subdirectory '${subdir}' is a symbolic link. Aborting." >&2
        exit 1
    fi
done

# 6. Configure non-root application storage permissions (0700, non-recursive)
# Owned by non-root container user 10001:10001
echo "==> Setting ownership to ${APP_UID}:${APP_GID} and permissions to 0700 for application storage"
chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}"
chmod 0700 "${STORAGE_ROOT}"

chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}/tmp"
chmod 0700 "${STORAGE_ROOT}/tmp"

chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}/organizations"
chmod 0700 "${STORAGE_ROOT}/organizations"

# 7. Configure platform metadata backup directory (0700, non-recursive, owned by root:root)
# Owned by root:root so application container cannot overwrite or delete database dumps
echo "==> Setting root:root ownership and 0700 permissions for metadata backups"
chown root:root "${STORAGE_ROOT}/metadata-backups"
chmod 0700 "${STORAGE_ROOT}/metadata-backups"

# 8. Verification of storage paths
echo "==> Verification of storage paths:"
ls -ld "${STORAGE_ROOT}" \
      "${STORAGE_ROOT}/tmp" \
      "${STORAGE_ROOT}/organizations" \
      "${STORAGE_ROOT}/metadata-backups"

echo "==> Host storage preparation completed successfully."
