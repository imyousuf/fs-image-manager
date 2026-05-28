package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeSource is an in-memory ReleaseSource backed by a byte map.
type fakeSource struct {
	release Release
	blobs   map[string][]byte
	fetched []string
}

func (f *fakeSource) Latest(context.Context) (Release, error) { return f.release, nil }

func (f *fakeSource) Fetch(_ context.Context, _ Release, name string) ([]byte, error) {
	f.fetched = append(f.fetched, name)
	b, ok := f.blobs[name]
	if !ok {
		return nil, fmt.Errorf("no such asset %q", name)
	}
	return b, nil
}

// recordingRestarter records the units it was asked to restart.
type recordingRestarter struct {
	units   []string
	failErr error
}

func (r *recordingRestarter) Restart(_ context.Context, unit string) error {
	r.units = append(r.units, unit)
	return r.failErr
}

// newFakeRelease builds a fake release for v1.5.0/linux/amd64 with a tarball
// containing the given binary payload and a matching SHA256SUMS.
func newFakeRelease(t *testing.T, payload []byte) (*fakeSource, string) {
	t.Helper()
	rel := Release{Version: "v1.5.0"}
	assetName := rel.AssetName("linux", "amd64")
	tgz := makeTarGz(t, map[string][]byte{
		fmt.Sprintf("%s_%s_linux_amd64/%s", BinaryName, rel.Version, BinaryName): payload,
	})
	sums := fmt.Sprintf("%s  %s\n%s  %s\n",
		sha256hex(tgz), assetName,
		sha256hex([]byte("noise")), "unrelated.txt")
	src := &fakeSource{
		release: rel,
		blobs: map[string][]byte{
			assetName:        tgz,
			ChecksumFileName: []byte(sums),
		},
	}
	return src, assetName
}

func TestUpdateHappyPath(t *testing.T) {
	payload := []byte("\x7fELF the new binary")
	src, assetName := newFakeRelease(t, payload)
	restarter := &recordingRestarter{}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      restarter,
		CurrentVersion: "v1.0.0",
		Unit:           "fs-image-manager.service",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	applied, err := u.Update(context.Background(), target)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if applied != "v1.5.0" {
		t.Fatalf("applied = %q, want v1.5.0", applied)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("binary not replaced with new payload")
	}

	// Both the asset and the checksum manifest must have been fetched.
	if len(src.fetched) != 2 {
		t.Fatalf("expected 2 fetches (asset + sums), got %v", src.fetched)
	}
	if src.fetched[0] != assetName || src.fetched[1] != ChecksumFileName {
		t.Fatalf("unexpected fetch order: %v", src.fetched)
	}

	if len(restarter.units) != 1 || restarter.units[0] != "fs-image-manager.service" {
		t.Fatalf("expected one restart of the unit, got %v", restarter.units)
	}
}

func TestUpdateUpToDateMakesNoChanges(t *testing.T) {
	payload := []byte("payload")
	src, _ := newFakeRelease(t, payload)
	restarter := &recordingRestarter{}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	original := []byte("running binary, must not change")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      restarter,
		CurrentVersion: "v1.5.0", // same as release
		Unit:           "fs-image-manager.service",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	_, err := u.Update(context.Background(), target)
	if !errors.Is(err, ErrUpToDate) {
		t.Fatalf("expected ErrUpToDate, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(original) {
		t.Fatalf("binary should be untouched when up to date")
	}
	if len(restarter.units) != 0 {
		t.Fatalf("should not restart when up to date")
	}
}

func TestUpdateChecksumMismatchAborts(t *testing.T) {
	src, assetName := newFakeRelease(t, []byte("payload"))
	// Corrupt the manifest so verification fails.
	src.blobs[ChecksumFileName] = []byte("0000000000000000000000000000000000000000000000000000000000000000  " + assetName + "\n")
	restarter := &recordingRestarter{}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	original := []byte("running binary, must not change")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      restarter,
		CurrentVersion: "v1.0.0",
		Unit:           "fs-image-manager.service",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	if _, err := u.Update(context.Background(), target); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(original) {
		t.Fatalf("binary must NOT be replaced on checksum failure")
	}
	if len(restarter.units) != 0 {
		t.Fatalf("must not restart on checksum failure")
	}
}

func TestUpdateNoUnitSkipsRestart(t *testing.T) {
	payload := []byte("payload")
	src, _ := newFakeRelease(t, payload)
	restarter := &recordingRestarter{}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      restarter,
		CurrentVersion: "v1.0.0",
		Unit:           "", // no unit -> no restart
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	if _, err := u.Update(context.Background(), target); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(restarter.units) != 0 {
		t.Fatalf("expected no restart when unit empty, got %v", restarter.units)
	}
}

func TestUpdateRestartFailureSurfacesButBinaryReplaced(t *testing.T) {
	payload := []byte("\x7fELF new")
	src, _ := newFakeRelease(t, payload)
	restarter := &recordingRestarter{failErr: errors.New("systemctl boom")}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      restarter,
		CurrentVersion: "v1.0.0",
		Unit:           "fs-image-manager.service",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	applied, err := u.Update(context.Background(), target)
	if err == nil {
		t.Fatal("expected restart failure to surface")
	}
	if applied != "v1.5.0" {
		t.Fatalf("applied version should still be reported, got %q", applied)
	}
	// Binary must already be the new one despite restart failure.
	got, _ := os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatalf("binary should be replaced even if restart fails")
	}
}

// TestGitHubSourceAgainstFakeServer exercises the real GitHubSource HTTP/JSON
// path against an httptest server (no real network), covering Latest + Fetch.
func TestGitHubSourceAgainstFakeServer(t *testing.T) {
	payload := []byte("\x7fELF binary from fake gh")
	tgz := makeTarGz(t, map[string][]byte{BinaryName: payload})
	assetName := "fs-image-manager_v2.0.0_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256hex(tgz), assetName)

	mux := http.NewServeMux()
	var assetURL, sumsURL string

	mux.HandleFunc("/repos/imyousuf/fs-image-manager/releases/latest",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v2.0.0",
				"draft": false,
				"prerelease": false,
				"assets": [
					{"name": %q, "browser_download_url": %q},
					{"name": %q, "browser_download_url": %q}
				]
			}`, assetName, assetURL, ChecksumFileName, sumsURL)
		})
	mux.HandleFunc("/dl/asset", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tgz)
	})
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	assetURL = srv.URL + "/dl/asset"
	sumsURL = srv.URL + "/dl/sums"

	src := &GitHubSource{
		Owner:   "imyousuf",
		Repo:    "fs-image-manager",
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "fs-image-manager")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := New(Options{
		Source:         src,
		Restarter:      &recordingRestarter{},
		CurrentVersion: "v1.0.0",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	applied, err := u.Update(context.Background(), target)
	if err != nil {
		t.Fatalf("Update via GitHubSource: %v", err)
	}
	if applied != "v2.0.0" {
		t.Fatalf("applied = %q, want v2.0.0", applied)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatalf("binary not replaced from fake GitHub asset")
	}
}

func TestCheck(t *testing.T) {
	src, _ := newFakeRelease(t, []byte("p"))

	u := New(Options{Source: src, Restarter: &recordingRestarter{}, CurrentVersion: "v1.0.0",
		GOOS: "linux", GOARCH: "amd64"})
	latest, newer, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest != "v1.5.0" || !newer {
		t.Fatalf("Check = (%q,%v), want (v1.5.0,true)", latest, newer)
	}

	u2 := New(Options{Source: src, Restarter: &recordingRestarter{}, CurrentVersion: "v1.5.0",
		GOOS: "linux", GOARCH: "amd64"})
	latest2, newer2, err := u2.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest2 != "v1.5.0" || newer2 {
		t.Fatalf("Check = (%q,%v), want (v1.5.0,false)", latest2, newer2)
	}
}
