# Deploying fs-image-manager

`fs-image-manager` ships as a single static (CGO-free) binary with the web UI
embedded. There are two roles:

- **`serve`** — the HTTP server + library/DB/cache. Runs on the media server.
- **`enrich-worker`** — pulls AI/transform jobs from a `serve` instance's
  internal job API. Runs on the GPU box.

Both roles run from the **same binary**; the systemd unit decides which mode.

---

## Install via .deb (recommended on Debian/Ubuntu)

Each release publishes a `.deb` per architecture as a GitHub Release asset. It
installs the binary, both systemd units, and a default config, and creates the
`fsim` service user for you.

```sh
VERSION=v0.2.0          # the release you want
ARCH=amd64              # or arm64
DEB=fs-image-manager_${VERSION}_${ARCH}.deb

curl -fsSLO https://github.com/imyousuf/fs-image-manager/releases/download/${VERSION}/${DEB}

# apt resolves dependencies and runs the maintainer scripts:
sudo apt install ./${DEB}
# (or, with plain dpkg, then pull in any missing deps:)
#   sudo dpkg -i ./${DEB} && sudo apt-get -f install
```

The package installs:

| Path | Purpose |
| --- | --- |
| `/usr/bin/fs-image-manager` | the binary |
| `/lib/systemd/system/fs-image-manager.service` | serve unit |
| `/lib/systemd/system/fs-image-manager-worker.service` | enrich-worker unit |
| `/etc/fs-image-manager/image-manager.cfg` | config (a dpkg **conffile** — your edits survive upgrades) |
| `/var/lib/fs-image-manager` | state dir (sqlite DB + cache), owned by `fsim` |

> Note: the units run `/usr/bin/fs-image-manager`, matching where the `.deb`
> installs the binary. The tarball/manual install (§1) also uses `/usr/bin` here
> for consistency; if you place the binary elsewhere, point `ExecStart` at it via
> `sudo systemctl edit fs-image-manager` (and the worker unit).

### After installing

The service is **not** started automatically — it needs your config first.

```sh
# 1. Edit libraries / DB / cache / auth / secrets:
sudo $EDITOR /etc/fs-image-manager/image-manager.cfg

# 2. Enable + start the media server:
sudo systemctl enable --now fs-image-manager
systemctl status fs-image-manager
```

For the GPU worker, fill in `/etc/fs-image-manager/worker.env` (see
[§4](#4-install-the-systemd-units)) and `enable --now fs-image-manager-worker`.

Upgrade with the next release's `.deb` (or `fs-image-manager update`, see
[§5](#5-self-update)); remove with `sudo apt remove fs-image-manager` (keeps your
config/DB) or `sudo apt purge fs-image-manager` (also drops the `fsim` user,
`/var/lib/fs-image-manager`, and the config).

### Why not Snap?

A `.deb` is the right fit here, not a Snap. Snap's strict confinement sandboxes
filesystem access, but fs-image-manager's whole job is to read **arbitrary
library paths** anywhere on the host (and accept uploads into them), and the
worker needs **ffmpeg + GPU devices** (`/dev/dri`, `/dev/nvidia*`) plus external
tools (exiftool, darktable, ollama). Those cross the confinement boundary and
would require broad interfaces/classic mode, defeating the point of a Snap. The
`.deb` integrates cleanly with systemd, ships the units, and leaves device and
path access to the (tunable) systemd hardening already in the units.

### Install via apt (Cloudsmith repo)

For the full `apt update && apt install` + `apt upgrade` experience, the release
workflow can also push each `.deb` to a managed [Cloudsmith](https://cloudsmith.io)
apt repository (GitHub Packages does not host Debian repos; Cloudsmith handles the
signing + hosting). packagecloud.io works the same way — swap the publish step.

**Maintainer setup (one time):** create a Cloudsmith repo `imyousuf/fs-image-manager`
and add its API key as the `CLOUDSMITH_API_KEY` GitHub Actions secret. The release
job then pushes the per-arch `.deb`s to the `any-distro/any-version` channel on every
`v*` tag. (Without the secret, releases still publish the `.deb` assets shown above.)

**End-user setup** (Cloudsmith shows the exact one-liner on the repo page):

```sh
curl -1sLf 'https://dl.cloudsmith.io/public/imyousuf/fs-image-manager/setup.deb.sh' \
  | sudo -E bash
sudo apt install fs-image-manager
```

After that, upgrades arrive through normal `apt update && apt upgrade`. If you don't
want a hosted repo, the per-release `.deb` asset above installs fine on its own.

---

## 1. Install the binary (tarball, any Linux)

> Prefer the [`.deb`](#install-via-deb-recommended-on-debianubuntu) on
> Debian/Ubuntu. Use the tarball on non-Debian distros or for a manual install.


Download the release tarball for your OS/arch from
[GitHub Releases](https://github.com/imyousuf/fs-image-manager/releases) and
verify the checksum:

```sh
VERSION=v0.2.0          # the release you want
ARCH=amd64              # or arm64
TARBALL=fs-image-manager_${VERSION}_linux_${ARCH}.tar.gz

curl -fsSLO https://github.com/imyousuf/fs-image-manager/releases/download/${VERSION}/${TARBALL}
curl -fsSLO https://github.com/imyousuf/fs-image-manager/releases/download/${VERSION}/SHA256SUMS

# Verify, extract, install
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf "${TARBALL}"
sudo install -m 0755 "fs-image-manager_${VERSION}_linux_${ARCH}/fs-image-manager" \
    /usr/bin/fs-image-manager
```

After the first install you can keep it current with the built-in updater
(no need to re-download manually):

```sh
sudo fs-image-manager update
```

`update` queries the latest GitHub Release, verifies the SHA256 checksum,
atomically replaces `/usr/bin/fs-image-manager`, and restarts the
relevant systemd unit. See [§5](#5-self-update).

## 2. Create the service user and directories

```sh
sudo useradd --system --home /var/lib/fs-image-manager \
    --shell /usr/sbin/nologin fsim || true
sudo mkdir -p /var/lib/fs-image-manager /etc/fs-image-manager
sudo chown -R fsim:fsim /var/lib/fs-image-manager
```

## 3. Configuration

Copy the template and edit it (libraries, DB path, cache dir, auth token, etc.):

```sh
sudo cp image-manager.cfg.template /etc/fs-image-manager/image-manager.cfg
sudo chown fsim:fsim /etc/fs-image-manager/image-manager.cfg
sudo $EDITOR /etc/fs-image-manager/image-manager.cfg
```

The DB (`[database] path`) and cache (`[cache] dir`) live under
`/var/lib/fs-image-manager` by default. If you point library roots elsewhere or
accept uploads into them, add those paths to `ReadWritePaths=` in the serve unit
(the hardening sandbox is otherwise read-only outside the state dir).

## 4. Install the systemd units

### Media server (`serve`)

```sh
sudo cp deploy/fs-image-manager.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fs-image-manager.service
systemctl status fs-image-manager.service
```

The UI/API is then on `http://<host>:8080` (or whatever `[http] listener` says).

### GPU box (`enrich-worker`)

The worker authenticates to the serve instance with the shared secret from the
serve config's `[worker] shared_secret`. Keep it out of the unit file via an
environment file:

```sh
sudo install -d -m 0750 /etc/fs-image-manager
sudo tee /etc/fs-image-manager/worker.env >/dev/null <<'EOF'
WORKER_SERVER=https://media.example.lan:8080
WORKER_SECRET=replace-with-shared-secret
EOF
sudo chmod 0640 /etc/fs-image-manager/worker.env

sudo cp deploy/fs-image-manager-worker.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fs-image-manager-worker.service
systemctl status fs-image-manager-worker.service
```

The worker unit deliberately does **not** restrict device access, so ffmpeg /
darktable can reach the GPU (`/dev/dri`, `/dev/nvidia*`). Install whatever
external tools you need (ffmpeg, exiftool, dcraw, darktable-cli, ollama) on the
worker host — they are detected at runtime and used when present.

## 5. Self-update

`fs-image-manager update` is safe to run on a live host:

1. Resolves the latest GitHub Release for this repo.
2. Downloads the asset matching the running OS/arch
   (`fs-image-manager_<version>_<os>_<arch>.tar.gz`).
3. Verifies its SHA256 against `SHA256SUMS` from the same release.
4. Atomically replaces the running binary (write-temp-then-rename on the same
   filesystem, so it is never left half-written).
5. Restarts the owning systemd unit if one is detected.

Run it as a user that can write the binary and restart the unit (typically
`root` via `sudo`). Useful flags:

```sh
fs-image-manager update --check      # report the latest version, do not apply
fs-image-manager update --unit fs-image-manager-worker.service  # restart a specific unit
```

If the binary is not managed by systemd (e.g. run manually), `update` still
replaces it; just restart the process yourself.
