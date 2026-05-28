// Package internaltest provides shared test fixtures for the whole codebase:
// temp directories, a temp config with two library roots, a sample-media
// copier that pulls a few real files from the user's ~/Pictures and ~/Videos,
// and in-memory fakes for the Cache, Queue, Enricher and FaceRecognizer
// seams (see docs/specs/_contracts.md §4 and §10).
//
// It is imported only from tests. Helpers take *testing.T and clean up after
// themselves via t.TempDir / t.Cleanup.
package internaltest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/config"
)

// Roots holds the two library roots created by NewTempRoots, addressable by
// their aliases.
type Roots struct {
	Dir      string // the temp parent directory
	Pictures string // absolute path of the "pictures" root
	Videos   string // absolute path of the "videos" root
}

// NewTempRoots creates a temp parent with two library subdirectories
// ("pictures", "videos") and returns their absolute paths.
func NewTempRoots(t *testing.T) Roots {
	t.Helper()
	dir := t.TempDir()
	pics := filepath.Join(dir, "pictures")
	vids := filepath.Join(dir, "videos")
	for _, d := range []string{pics, vids} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("internaltest: mkdir %q: %v", d, err)
		}
	}
	return Roots{Dir: dir, Pictures: pics, Videos: vids}
}

// WriteConfig writes a valid INI config file (per _contracts.md §7) into a
// temp dir, pointing the "pictures"/"videos" aliases at the given roots and
// placing the sqlite db and cache dir under the same temp area. The ingest
// debounce is set low (1s) so tests don't wait minutes. It returns the path to
// the written config file.
func WriteConfig(t *testing.T, roots Roots) string {
	t.Helper()
	cfgDir := t.TempDir()
	dbPath := filepath.Join(cfgDir, "fsim.db")
	cacheDir := filepath.Join(cfgDir, "cache")
	contents := "[libraries]\n" +
		"pictures = " + roots.Pictures + "\n" +
		"videos   = " + roots.Videos + "\n\n" +
		"[http]\n" +
		"listener = :0\n\n" +
		"[auth]\n" +
		"token =\n\n" +
		"[database]\n" +
		"path = " + dbPath + "\n\n" +
		"[cache]\n" +
		"dir = " + cacheDir + "\n\n" +
		"[ingest]\n" +
		"debounce_seconds = 1\n\n" +
		"[worker]\n" +
		"shared_secret = test-secret\n\n" +
		"[ai]\n" +
		"ollama_url =\n" +
		"rekognition_profile = imyousuf\n" +
		"rekognition_region  = us-east-1\n" +
		"rekognition_collection =\n"
	path := filepath.Join(cfgDir, "image-manager.cfg")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("internaltest: write config: %v", err)
	}
	return path
}

// NewConfig is a convenience that creates two temp roots, writes a config and
// loads it, returning both the loaded config and the roots.
func NewConfig(t *testing.T) (*config.Config, Roots) {
	t.Helper()
	roots := NewTempRoots(t)
	path := WriteConfig(t, roots)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("internaltest: load config: %v", err)
	}
	return cfg, roots
}
