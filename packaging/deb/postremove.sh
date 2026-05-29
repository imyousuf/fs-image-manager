#!/bin/sh
# postremove (Debian "postrm" / nfpm scripts.postremove)
#
# Runs after the package files are removed. dpkg passes the action as $1:
#   remove   -> package removed, config kept (an upgrade also calls this path)
#   purge    -> "apt purge" / "dpkg -P": also drop config + state + service user
# Only on purge do we tear down the system user and the state directory, so a
# plain remove (or upgrade) keeps the operator's DB/cache and config intact.
set -e

FSIM_USER=fsim
FSIM_GROUP=fsim
FSIM_HOME=/var/lib/fs-image-manager

# Reload systemd after the unit files have been removed.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

case "${1:-}" in
    purge)
        # Remove the state directory (sqlite DB + derivative cache). The
        # conffile under /etc is handled by dpkg's conffile machinery.
        rm -rf "${FSIM_HOME}" || true

        # Drop the dedicated system user/group created in postinstall.
        if getent passwd "${FSIM_USER}" >/dev/null 2>&1; then
            deluser --system "${FSIM_USER}" >/dev/null 2>&1 \
                || userdel "${FSIM_USER}" >/dev/null 2>&1 \
                || true
        fi
        if getent group "${FSIM_GROUP}" >/dev/null 2>&1; then
            delgroup --system "${FSIM_GROUP}" >/dev/null 2>&1 \
                || groupdel "${FSIM_GROUP}" >/dev/null 2>&1 \
                || true
        fi
        ;;
    *)
        # remove / upgrade / abort-*: keep user, state, and config.
        ;;
esac

exit 0
