package internaltest

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// imageExts and videoExts are the extensions the sample copier recognises when
// pulling real files from the user's libraries.
var (
	imageExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true,
		".cr2": true, ".cr3": true, ".nef": true, ".arw": true,
		".raf": true, ".orf": true, ".dng": true, ".heic": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".avi": true,
	}
)

// CopySampleMedia copies up to n media files found under srcDir (recursively)
// into dstDir, preserving base filenames. It returns the base names copied. If
// srcDir does not exist or holds no media, it returns nil — callers that need
// real media should t.Skip in that case. This lets tests opt into using the
// real ~/Pictures and ~/Videos libraries without hard-failing on machines that
// lack them.
func CopySampleMedia(t *testing.T, srcDir, dstDir string, n int) []string {
	t.Helper()
	if n <= 0 {
		return nil
	}
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var found []string
	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries during sampling
		}
		ext := strings.ToLower(filepath.Ext(path))
		if imageExts[ext] || videoExts[ext] {
			found = append(found, path)
		}
		if len(found) >= n {
			return filepath.SkipAll
		}
		return nil
	})
	if len(found) == 0 {
		return nil
	}
	sort.Strings(found)

	var copied []string
	for _, src := range found {
		base := filepath.Base(src)
		dst := filepath.Join(dstDir, base)
		if err := copyFile(src, dst); err != nil {
			t.Fatalf("internaltest: copy sample %q: %v", src, err)
		}
		copied = append(copied, base)
	}
	return copied
}

// SampleHome returns the user's home dir; used to locate ~/Pictures, ~/Videos.
func SampleHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("internaltest: home dir: %v", err)
	}
	return home
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // test fixture; src comes from a trusted walk
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst) //nolint:gosec // dst is under a t.TempDir
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
