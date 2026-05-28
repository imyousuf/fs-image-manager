package media_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/media"
)

// TestHashDeterministicAndSizeSensitive: identical content+size hash equal;
// differing size (with identical head) changes the hash.
func TestHashDeterministicAndSizeSensitive(t *testing.T) {
	head := bytes.Repeat([]byte("abc"), 100)

	h1, err := media.HashReader(bytes.NewReader(head), int64(len(head)))
	if err != nil {
		t.Fatalf("HashReader: %v", err)
	}
	h2, _ := media.HashReader(bytes.NewReader(head), int64(len(head)))
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s vs %s", h1, h2)
	}
	// Same head, different declared size -> different hash.
	h3, _ := media.HashReader(bytes.NewReader(head), int64(len(head))+1)
	if h1 == h3 {
		t.Error("size must affect the hash")
	}
}

func TestHashFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.bin")
	if err := os.WriteFile(p, []byte("payload-bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, err := media.HashFile(p)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if h == "" {
		t.Error("empty hash")
	}
	// Stable across calls.
	h2, _ := media.HashFile(p)
	if h != h2 {
		t.Errorf("HashFile not stable: %s vs %s", h, h2)
	}
}

func TestClassifyExt(t *testing.T) {
	cases := map[string]string{
		"IMG.JPG":   "jpg",
		"a.jpeg":    "jpg",
		"b.PNG":     "png",
		"c.CR3":     "raw",
		"d.cr2":     "raw",
		"e.nef":     "raw",
		"clip.MOV":  "video",
		"v.webm":    "video",
		"side.xmp":  "sidecar",
		"side.pp3":  "sidecar",
		"side.THM":  "sidecar",
		"notes.txt": "other",
		"noext":     "other",
	}
	for name, want := range cases {
		if got := string(media.ClassifyExt(name)); got != want {
			t.Errorf("ClassifyExt(%q) = %q, want %q", name, got, want)
		}
	}
}
