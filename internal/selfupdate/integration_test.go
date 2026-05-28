//go:build integration

// These tests hit the real GitHub Releases API and are excluded from the normal
// `go test ./...` run. Enable with: go test -tags integration ./internal/selfupdate
package selfupdate

import (
	"context"
	"testing"
	"time"
)

// TestRealGitHubLatest verifies that the public GitHub source can resolve the
// latest fs-image-manager release and that it exposes the expected asset naming
// (SHA256SUMS + at least one fs-image-manager_<ver>_<os>_<arch>.tar.gz).
func TestRealGitHubLatest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	src := NewGitHubSource(DefaultOwner, DefaultRepo)
	rel, err := src.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest against real GitHub: %v", err)
	}
	if rel.Version == "" {
		t.Fatal("expected a non-empty release version")
	}
	if _, ok := rel.Assets[ChecksumFileName]; !ok {
		t.Errorf("release %s missing %s asset; assets: %v", rel.Version, ChecksumFileName, rel.Assets)
	}
	want := rel.AssetName("linux", "amd64")
	if _, ok := rel.Assets[want]; !ok {
		t.Errorf("release %s missing expected asset %q; assets: %v", rel.Version, want, rel.Assets)
	}
}
