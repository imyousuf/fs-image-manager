// Package selfupdate implements the `update` subcommand: it discovers the
// latest GitHub Release for fs-image-manager, downloads the asset matching the
// running OS/arch, verifies its SHA256 against the release's SHA256SUMS, and
// atomically replaces the running binary before restarting the owning systemd
// unit.
//
// The package depends only on the standard library so the produced binary stays
// CGO-free and pulls in no extra modules. Network access (the GitHub API) is
// isolated behind the ReleaseSource interface so the checksum-verification and
// atomic-replace logic can be exercised in unit tests against a fake release
// server and temp files. Tests that hit the real GitHub API are tagged
// //go:build integration.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
)

// DefaultOwner/DefaultRepo identify the GitHub repository releases are pulled
// from. They match the module path github.com/imyousuf/fs-image-manager.
const (
	DefaultOwner = "imyousuf"
	DefaultRepo  = "fs-image-manager"
	// BinaryName is the executable name inside each release tarball and the
	// asset-name prefix (fs-image-manager_<version>_<os>_<arch>.tar.gz).
	BinaryName = "fs-image-manager"
)

// ErrUpToDate is returned by Update when the running version already matches the
// latest release.
var ErrUpToDate = errors.New("selfupdate: already at the latest version")

// Release is a resolved GitHub release with the assets relevant to updating.
type Release struct {
	// Version is the release tag, e.g. "v0.2.0".
	Version string
	// Assets maps asset file name to its download URL.
	Assets map[string]string
}

// AssetName returns the tarball asset name for this release and the given
// os/arch, e.g. "fs-image-manager_v0.2.0_linux_amd64.tar.gz".
func (r Release) AssetName(goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, r.Version, goos, goarch)
}

// ReleaseSource resolves the latest release and fetches assets by name. The
// GitHub implementation lives in github.go; tests provide a fake backed by an
// httptest server.
type ReleaseSource interface {
	// Latest returns the most recent (non-draft, non-prerelease) release.
	Latest(ctx context.Context) (Release, error)
	// Fetch returns the bytes of the named asset from the release.
	Fetch(ctx context.Context, rel Release, assetName string) ([]byte, error)
}

// Restarter restarts the running service after the binary is replaced.
// The systemd implementation lives in restart.go; tests use a recording fake.
type Restarter interface {
	Restart(ctx context.Context, unit string) error
}

// Options configures an Updater.
type Options struct {
	// Source resolves releases; defaults to the public GitHub source for
	// DefaultOwner/DefaultRepo.
	Source ReleaseSource
	// Restarter restarts the systemd unit after replacement; defaults to the
	// systemctl-backed implementation.
	Restarter Restarter
	// CurrentVersion is the version of the running binary (main.version). Used
	// to short-circuit when already up to date.
	CurrentVersion string
	// Unit is the systemd unit to restart after a successful replace. If empty,
	// the restart step is skipped (e.g. when run outside systemd).
	Unit string
	// GOOS/GOARCH select the asset to download; default to the build's values.
	GOOS, GOARCH string
	// Logger receives progress; defaults to slog.Default().
	Logger *slog.Logger
}

// Updater performs the update workflow.
type Updater struct {
	source    ReleaseSource
	restarter Restarter
	current   string
	unit      string
	goos      string
	goarch    string
	log       *slog.Logger
}

// New builds an Updater, filling in defaults for any unset option.
func New(opts Options) *Updater {
	u := &Updater{
		source:    opts.Source,
		restarter: opts.Restarter,
		current:   opts.CurrentVersion,
		unit:      opts.Unit,
		goos:      opts.GOOS,
		goarch:    opts.GOARCH,
		log:       opts.Logger,
	}
	if u.source == nil {
		u.source = NewGitHubSource(DefaultOwner, DefaultRepo)
	}
	if u.restarter == nil {
		u.restarter = SystemctlRestarter{}
	}
	if u.goos == "" {
		u.goos = runtime.GOOS
	}
	if u.goarch == "" {
		u.goarch = runtime.GOARCH
	}
	if u.log == nil {
		u.log = slog.Default()
	}
	return u
}

// Check resolves the latest release without applying it. It reports the latest
// version and whether it differs from the running version.
func (u *Updater) Check(ctx context.Context) (latest string, newer bool, err error) {
	rel, err := u.source.Latest(ctx)
	if err != nil {
		return "", false, err
	}
	return rel.Version, !sameVersion(u.current, rel.Version), nil
}

// Update runs the full workflow: resolve latest, download the matching asset and
// its checksum manifest, verify, extract, atomically replace the running binary,
// and restart the unit. The path of the binary to replace is given explicitly
// (callers pass os.Executable()) so the replace logic is testable.
//
// It returns ErrUpToDate (and makes no changes) when CurrentVersion already
// matches the latest release.
func (u *Updater) Update(ctx context.Context, targetPath string) (applied string, err error) {
	rel, err := u.source.Latest(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	if sameVersion(u.current, rel.Version) {
		return rel.Version, ErrUpToDate
	}
	u.log.Info("updating fs-image-manager",
		"from", u.current, "to", rel.Version, "os", u.goos, "arch", u.goarch)

	assetName := rel.AssetName(u.goos, u.goarch)
	asset, err := u.source.Fetch(ctx, rel, assetName)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}

	sums, err := u.source.Fetch(ctx, rel, ChecksumFileName)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", ChecksumFileName, err)
	}
	if err := VerifyChecksum(assetName, asset, sums); err != nil {
		return "", err
	}
	u.log.Info("verified release asset checksum", "asset", assetName)

	binary, err := ExtractBinary(asset, BinaryName)
	if err != nil {
		return "", fmt.Errorf("extract %s from %s: %w", BinaryName, assetName, err)
	}

	if err := ReplaceBinary(targetPath, binary); err != nil {
		return "", fmt.Errorf("replace %s: %w", targetPath, err)
	}
	u.log.Info("replaced running binary", "path", targetPath, "version", rel.Version)

	if u.unit != "" {
		if err := u.restarter.Restart(ctx, u.unit); err != nil {
			// The new binary is in place; surface the restart failure so the
			// operator can restart manually, but do not roll back.
			return rel.Version, fmt.Errorf("binary updated to %s but restarting %s failed: %w",
				rel.Version, u.unit, err)
		}
		u.log.Info("restarted systemd unit", "unit", u.unit)
	}
	return rel.Version, nil
}
