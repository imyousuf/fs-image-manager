#!/bin/sh
# postinstall (Debian "configure" / nfpm scripts.postinstall)
#
# Runs after the package files are unpacked. Creates the dedicated system
# user/group, the state directory, reloads systemd, and prints next steps.
# It deliberately does NOT enable or start any service: the operator must
# edit the config (libraries, secrets, listener) first.
set -e

FSIM_USER=fsim
FSIM_GROUP=fsim
FSIM_HOME=/var/lib/fs-image-manager

# Create the system group/user if they are not present yet. Idempotent so it
# is safe on upgrades and reinstalls.
if ! getent group "${FSIM_GROUP}" >/dev/null 2>&1; then
    addgroup --system "${FSIM_GROUP}" >/dev/null 2>&1 \
        || groupadd --system "${FSIM_GROUP}" >/dev/null 2>&1 \
        || true
fi

if ! getent passwd "${FSIM_USER}" >/dev/null 2>&1; then
    adduser --system --home "${FSIM_HOME}" --no-create-home \
        --ingroup "${FSIM_GROUP}" --shell /usr/sbin/nologin "${FSIM_USER}" >/dev/null 2>&1 \
        || useradd --system --home-dir "${FSIM_HOME}" --no-create-home \
        --gid "${FSIM_GROUP}" --shell /usr/sbin/nologin "${FSIM_USER}" >/dev/null 2>&1 \
        || true
fi

# State directory for the sqlite DB and derivative cache.
mkdir -p "${FSIM_HOME}"
chown "${FSIM_USER}:${FSIM_GROUP}" "${FSIM_HOME}" || true
chmod 0750 "${FSIM_HOME}" || true

# Make the freshly installed unit files visible to systemd.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

cat <<'EOF'

fs-image-manager installed.

Next steps:
  1. Edit the config:   sudo $EDITOR /etc/fs-image-manager/image-manager.cfg
  2. Start the server:  sudo systemctl enable --now fs-image-manager

The service is intentionally NOT started automatically — it needs your
library paths and settings first. The GPU worker (optional) is
fs-image-manager-worker; see /etc/fs-image-manager and the deploy docs.

EOF

exit 0
