package metadata_test

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRaw writes data to root/rel, creating parent dirs. Used to plant small
// fixture files under a library root.
func writeRaw(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
