#!/bin/sh
# preremove (Debian "prerm" / nfpm scripts.preremove)
#
# Runs before the package files are removed. Stop and disable the services so
# they do not keep running against files that are about to disappear. We guard
# every call so removal never fails because a unit was absent or never enabled.
set -e

if command -v systemctl >/dev/null 2>&1; then
    for unit in fs-image-manager.service fs-image-manager-worker.service; do
        systemctl stop "${unit}" >/dev/null 2>&1 || true
        systemctl disable "${unit}" >/dev/null 2>&1 || true
    done
fi

exit 0
