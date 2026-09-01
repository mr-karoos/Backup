#!/usr/bin/env bash
# ==============================================================================
# Host Storage Preparation Script for Backup Platform (Internal V1)
# ==============================================================================
# Prepares host directory structures, ownership, and strict POSIX permissions
# for the non-root application container (UID:GID 10001:10001) and metadata dumps.
#
# Target OS: Ubuntu 22.04 LTS / Debian 12 / Compatible Linux
# Execution: sudo ./prepare-host.sh [OPTIONAL_STORAGE_ROOT]
# ==============================================================================

set -euo pipefail

# 1. Require root privileges
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: prepare-host.sh must be run as root (or with sudo)." >&2
    exit 1
fi

STORAGE_ROOT="${1:-/srv/backup-platform}"
APP_UID=10001
APP_GID=10001

echo "==> Preparing Backup Platform host storage directory: ${STORAGE_ROOT}"

# 2. Create canonical directory structure idempotently
mkdir -p "${STORAGE_ROOT}"
mkdir -p "${STORAGE_ROOT}/tmp"
mkdir -p "${STORAGE_ROOT}/organizations"
mkdir -p "${STORAGE_ROOT}/metadata-backups"

# 3. Configure application runtime directory ownership and strict permissions (0700)
# Owned by non-root container user 10001:10001
echo "==> Setting ownership to ${APP_UID}:${APP_GID} and permissions to 0700 for application storage"
chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}"
chmod 0700 "${STORAGE_ROOT}"

chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}/tmp"
chmod 0700 "${STORAGE_ROOT}/tmp"

chown "${APP_UID}:${APP_GID}" "${STORAGE_ROOT}/organizations"
chmod 0700 "${STORAGE_ROOT}/organizations"

# 4. Configure platform metadata backup directory ownership and permissions (0700)
# Owned by root:root so application container cannot overwrite or delete database dumps
echo "==> Setting root:root ownership and 0700 permissions for metadata backups"
chown root:root "${STORAGE_ROOT}/metadata-backups"
chmod 0700 "${STORAGE_ROOT}/metadata-backups"

# 5. Verify configuration
echo "==> Verification of created storage paths:"
ls -ld "${STORAGE_ROOT}" \
      "${STORAGE_ROOT}/tmp" \
      "${STORAGE_ROOT}/organizations" \
      "${STORAGE_ROOT}/metadata-backups"

echo "==> Host storage preparation completed successfully."
