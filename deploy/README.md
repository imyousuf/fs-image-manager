# Deploying fs-image-manager

`fs-image-manager` ships as a single static (CGO-free) binary with the web UI
embedded. There are two roles:

- **`serve`** — the HTTP server + library/DB/cache. Runs on the media server.
- **`enrich-worker`** — pulls AI/transform jobs from a `serve` instance's
  internal job API. Runs on the GPU box.

Both roles run from the **same binary**; the systemd unit decides which mode.

---

## 1. Install the binary

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
    /usr/local/bin/fs-image-manager
```

After the first install you can keep it current with the built-in updater
(no need to re-download manually):

```sh
sudo fs-image-manager update
```

`update` queries the latest GitHub Release, verifies the SHA256 checksum,
atomically replaces `/usr/local/bin/fs-image-manager`, and restarts the
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
