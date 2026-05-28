package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// makeTarGz builds a gzip-compressed tar archive from name->content entries.
func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	want := []byte("\x7fELF fake binary payload")

	t.Run("at archive root", func(t *testing.T) {
		tgz := makeTarGz(t, map[string][]byte{
			"fs-image-manager": want,
			"README.md":        []byte("docs"),
		})
		got, err := ExtractBinary(tgz, "fs-image-manager")
		if err != nil {
			t.Fatalf("ExtractBinary: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("payload mismatch")
		}
	})

	t.Run("under versioned dir, matched by base name", func(t *testing.T) {
		tgz := makeTarGz(t, map[string][]byte{
			"fs-image-manager_v1.0.0_linux_amd64/fs-image-manager": want,
			"fs-image-manager_v1.0.0_linux_amd64/LICENSE":          []byte("MIT"),
		})
		got, err := ExtractBinary(tgz, "fs-image-manager")
		if err != nil {
			t.Fatalf("ExtractBinary: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("payload mismatch")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tgz := makeTarGz(t, map[string][]byte{"other": want})
		if _, err := ExtractBinary(tgz, "fs-image-manager"); err == nil {
			t.Fatal("expected error for missing binary")
		}
	})

	t.Run("not gzip", func(t *testing.T) {
		if _, err := ExtractBinary([]byte("plain text"), "fs-image-manager"); err == nil {
			t.Fatal("expected error for non-gzip data")
		}
	})
}

func TestReplaceBinaryAtomicAndModePreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fs-image-manager")

	// Original binary with a distinctive mode.
	if err := os.WriteFile(path, []byte("OLD VERSION"), 0o750); err != nil {
		t.Fatalf("seed: %v", err)
	}

	newContent := []byte("NEW VERSION bytes")
	if err := ReplaceBinary(path, newContent); err != nil {
		t.Fatalf("ReplaceBinary: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced: %v", err)
	}
	if !bytes.Equal(got, newContent) {
		t.Fatalf("content not replaced: %q", got)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if fi.Mode().Perm() != 0o750 {
			t.Fatalf("mode not preserved: got %v want 0750", fi.Mode().Perm())
		}
	}

	// No temp files should be left behind in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "fs-image-manager" {
			t.Fatalf("leftover file in dir: %q", e.Name())
		}
	}
}

func TestReplaceBinaryNewFileGetsDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fs-image-manager") // does not exist yet

	if err := ReplaceBinary(path, []byte("brand new")); err != nil {
		t.Fatalf("ReplaceBinary: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Fatalf("expected default 0755, got %v", fi.Mode().Perm())
		}
	}
}
