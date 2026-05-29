#!/usr/bin/env bash
#
# fs-image-manager installer.
#
# Quick install (latest release, with systemd setup when run as root):
#   curl -fsSL https://github.com/imyousuf/fs-image-manager/releases/latest/download/install.sh | sudo bash
#
# Options (pass after `bash -s --` when piping, or directly when run as a file):
#   --version vX.Y.Z   install a specific release tag (default: latest)
#   --no-service       install only the binary; skip systemd units, config, user
#   -h, --help         show this help
#
# Environment overrides:
#   VERSION=vX.Y.Z     same as --version
#
# Idempotent: re-running installs/upgrades the binary and (re)writes the units,
# but never overwrites an existing /etc/fs-image-manager/image-manager.cfg.
set -euo pipefail

REPO="imyousuf/fs-image-manager"
BIN_NAME="fs-image-manager"
BIN_DEST="/usr/bin/${BIN_NAME}"
UNIT_DIR="/lib/systemd/system"
CONF_DIR="/etc/fs-image-manager"
CONF_DEST="${CONF_DIR}/image-manager.cfg"
STATE_DIR="/var/lib/fs-image-manager"
FSIM_USER="fsim"
FSIM_GROUP="fsim"

VERSION="${VERSION:-}"
WANT_SERVICE=1

# --- helpers ---------------------------------------------------------------

log()  { printf '==> %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
err()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
    # Print the leading comment block (lines 2.. up to the first non-# line),
    # stripping the leading "# ".
    awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"
    exit "${1:-0}"
}

need() {
    command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

# --- args ------------------------------------------------------------------

while [ "$#" -gt 0 ]; do
    case "$1" in
        --version)
            [ "$#" -ge 2 ] || err "--version requires an argument (e.g. v0.2.0)"
            VERSION="$2"
            shift 2
            ;;
        --version=*)
            VERSION="${1#*=}"
            shift
            ;;
        --no-service)
            WANT_SERVICE=0
            shift
            ;;
        -h|--help)
            usage 0
            ;;
        *)
            err "unknown argument: $1 (try --help)"
            ;;
    esac
done

# --- preflight -------------------------------------------------------------

need uname
need curl
need install

# OS must be Linux (systemd units + paths are Linux-only).
os="$(uname -s)"
[ "${os}" = "Linux" ] || err "unsupported OS: ${os} (only Linux is supported)"

# Map machine arch to the release asset arch.
machine="$(uname -m)"
case "${machine}" in
    x86_64|amd64)        ARCH="amd64" ;;
    aarch64|arm64)       ARCH="arm64" ;;
    *) err "unsupported architecture: ${machine} (only amd64/arm64 are supported)" ;;
esac

# A sha256 verifier: prefer sha256sum, fall back to shasum.
if command -v sha256sum >/dev/null 2>&1; then
    SHA_CHECK() { sha256sum --ignore-missing -c "$1"; }
elif command -v shasum >/dev/null 2>&1; then
    SHA_CHECK() { shasum -a 256 --ignore-missing -c "$1"; }
else
    err "need sha256sum or shasum to verify the download"
fi

# Are we root? Needed to install to /usr/bin and to set up the service.
IS_ROOT=0
[ "$(id -u)" -eq 0 ] && IS_ROOT=1

# Is systemd present?
HAVE_SYSTEMD=0
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    HAVE_SYSTEMD=1
fi

# --- resolve version -------------------------------------------------------

if [ -z "${VERSION}" ]; then
    log "Resolving latest release tag for ${REPO}..."
    api="https://api.github.com/repos/${REPO}/releases/latest"
    # Extract "tag_name": "vX.Y.Z" without requiring jq.
    VERSION="$(curl -fsSL "${api}" \
        | grep -m1 '"tag_name"' \
        | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
    [ -n "${VERSION}" ] || err "could not resolve the latest release tag from ${api}"
fi
log "Installing ${BIN_NAME} ${VERSION} (linux/${ARCH})"

BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
TARBALL="${BIN_NAME}_${VERSION}_linux_${ARCH}.tar.gz"

# --- download + verify -----------------------------------------------------

TMP="$(mktemp -d)"
# shellcheck disable=SC2317  # invoked via trap, not reachable linearly
cleanup() { rm -rf "${TMP}"; }
trap cleanup EXIT

dl() {
    # dl <remote-filename> <local-path>
    local url="${BASE_URL}/$1"
    curl -fsSL -o "$2" "${url}" \
        || err "download failed: ${url}"
}

log "Downloading ${TARBALL} + SHA256SUMS"
dl "${TARBALL}" "${TMP}/${TARBALL}"
dl "SHA256SUMS" "${TMP}/SHA256SUMS"

log "Verifying checksum"
( cd "${TMP}" && SHA_CHECK SHA256SUMS ) \
    || err "checksum verification failed for ${TARBALL}"

log "Extracting"
tar -xzf "${TMP}/${TARBALL}" -C "${TMP}"
SRC_BIN="${TMP}/${BIN_NAME}_${VERSION}_linux_${ARCH}/${BIN_NAME}"
[ -f "${SRC_BIN}" ] || err "binary not found in archive: ${SRC_BIN}"

# --- install binary --------------------------------------------------------

# Writing to /usr/bin needs root. Use sudo when available and not already root.
SUDO=""
if [ "${IS_ROOT}" -ne 1 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        warn "not running as root; using sudo for privileged steps"
    else
        err "must be run as root (or with sudo available) to install to ${BIN_DEST}"
    fi
fi

log "Installing binary to ${BIN_DEST}"
${SUDO} install -m 0755 "${SRC_BIN}" "${BIN_DEST}"

# --- service setup ---------------------------------------------------------

if [ "${WANT_SERVICE}" -eq 0 ]; then
    log "Binary installed (--no-service): skipping systemd units, config, and user."
    log "Done. ${BIN_DEST} --version  to confirm."
    exit 0
fi

if [ "${HAVE_SYSTEMD}" -ne 1 ]; then
    warn "systemd not detected; installed the binary only."
    warn "Set up the service manually — see the deploy docs:"
    warn "  https://github.com/${REPO}/blob/master/deploy/README.md"
    exit 0
fi

log "Setting up systemd service"

# Fetch the unit files + config template from the SAME release.
dl "${BIN_NAME}.service"        "${TMP}/${BIN_NAME}.service"
dl "${BIN_NAME}-worker.service" "${TMP}/${BIN_NAME}-worker.service"
dl "image-manager.cfg.template" "${TMP}/image-manager.cfg.template"

# Service user/group (idempotent; addgroup/adduser with useradd/groupadd fallbacks).
if ! getent group "${FSIM_GROUP}" >/dev/null 2>&1; then
    log "Creating system group ${FSIM_GROUP}"
    ${SUDO} addgroup --system "${FSIM_GROUP}" >/dev/null 2>&1 \
        || ${SUDO} groupadd --system "${FSIM_GROUP}" >/dev/null 2>&1 \
        || warn "could not create group ${FSIM_GROUP}"
fi
if ! getent passwd "${FSIM_USER}" >/dev/null 2>&1; then
    log "Creating system user ${FSIM_USER}"
    ${SUDO} adduser --system --home "${STATE_DIR}" --no-create-home \
        --ingroup "${FSIM_GROUP}" --shell /usr/sbin/nologin "${FSIM_USER}" >/dev/null 2>&1 \
        || ${SUDO} useradd --system --home-dir "${STATE_DIR}" --no-create-home \
        --gid "${FSIM_GROUP}" --shell /usr/sbin/nologin "${FSIM_USER}" >/dev/null 2>&1 \
        || warn "could not create user ${FSIM_USER}"
fi

# State directory (sqlite DB + derivative cache).
log "Creating state directory ${STATE_DIR}"
${SUDO} mkdir -p "${STATE_DIR}"
${SUDO} chown "${FSIM_USER}:${FSIM_GROUP}" "${STATE_DIR}" || true
${SUDO} chmod 0750 "${STATE_DIR}" || true

# Install the unit files.
log "Installing systemd units to ${UNIT_DIR}"
${SUDO} install -m 0644 "${TMP}/${BIN_NAME}.service"        "${UNIT_DIR}/${BIN_NAME}.service"
${SUDO} install -m 0644 "${TMP}/${BIN_NAME}-worker.service" "${UNIT_DIR}/${BIN_NAME}-worker.service"

# Install the config template ONLY if no config exists yet — never clobber edits.
${SUDO} mkdir -p "${CONF_DIR}"
if [ -e "${CONF_DEST}" ]; then
    log "Config already present at ${CONF_DEST} — leaving it untouched"
else
    log "Installing default config to ${CONF_DEST}"
    ${SUDO} install -m 0640 "${TMP}/image-manager.cfg.template" "${CONF_DEST}"
    ${SUDO} chown "${FSIM_USER}:${FSIM_GROUP}" "${CONF_DEST}" 2>/dev/null || true
fi

log "Reloading systemd"
${SUDO} systemctl daemon-reload || true

# Deliberately do NOT enable/start: the service needs config first.
cat <<EOF

${BIN_NAME} ${VERSION} installed.

Next steps:
  1. Edit the config:   sudo \$EDITOR ${CONF_DEST}
  2. Start the server:  sudo systemctl enable --now ${BIN_NAME}

The service is intentionally NOT started automatically — set your library
paths and settings first. The GPU worker (optional) is ${BIN_NAME}-worker;
see https://github.com/${REPO}/blob/master/deploy/README.md

EOF

exit 0
