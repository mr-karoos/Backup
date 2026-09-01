#!/usr/bin/env bash
# ==============================================================================
# Host Storage Preparation Script for Backup Platform (Internal V1)
# ==============================================================================
# Prepares canonical host directory structures, ownership, and strict POSIX
# permissions for the non-root application container (UID:GID 10001:10001) and
# root-owned metadata backups.
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

APP_UID=10001
APP_GID=10001
DEFAULT_STORAGE_ROOT="/srv/backup-platform"
STORAGE_ROOT="${1:-${DEFAULT_STORAGE_ROOT}}"

# 2. Strict Path Validation (Defend against dangerous system directories)
if [[ "${STORAGE_ROOT}" != /* ]]; then
    echo "ERROR: STORAGE_ROOT must be an absolute path starting with '/': '${STORAGE_ROOT}'" >&2
    exit 1
fi

# Clean trailing slashes except for root
STORAGE_ROOT="${STORAGE_ROOT%/}"
if [[ -z "${STORAGE_ROOT}" ]]; then
    STORAGE_ROOT="/"
fi

# Blacklist root and dangerous top-level operating system paths
case "${STORAGE_ROOT}" in
    "/" | "/etc" | "/etc/"* | "/usr" | "/usr/"* | "/var" | "/var/"* | \
    "/home" | "/home/"* | "/root" | "/root/"* | "/opt" | "/tmp" | "/tmp/"* | \
    "/run" | "/run/"* | "/srv" | "/bin" | "/sbin" | "/lib" | "/lib64" | \
    "/boot" | "/dev" | "/proc" | "/sys")
        echo "ERROR: Target path '${STORAGE_ROOT}' is a reserved system directory. Aborting." >&2
        exit 1
        ;;
esac

if [[ "${STORAGE_ROOT}" =~ \.\. ]]; then
    echo "ERROR: Target path '${STORAGE_ROOT}' cannot contain '..' relative elements." >&2
    exit 1
fi

# 3. Prevent Symlink Hijacking on Target Root
if [[ -L "${STORAGE_ROOT}" ]]; then
    echo "ERROR: Target path '${STORAGE_ROOT}' is a symbolic link. Symlinks are not permitted." >&2
    exit 1
fi

echo "==> Preparing Backup Platform host storage directory: ${STORAGE_ROOT}"

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

# 7. Configure platform metadata backup directory (0700, owned by root:root)
# Owned by root:root so application container cannot overwrite or delete database dumps
echo "==> Setting root:root ownership and 0700 permissions for metadata backups"
chown root:root "${STORAGE_ROOT}/metadata-backups"
chmod 0700 "${STORAGE_ROOT}/metadata-backups"

# 8. Verification of created storage paths
echo "==> Verification of storage paths:"
ls -ld "${STORAGE_ROOT}" \
      "${STORAGE_ROOT}/tmp" \
      "${STORAGE_ROOT}/organizations" \
      "${STORAGE_ROOT}/metadata-backups"

echo "==> Host storage preparation completed successfully."
