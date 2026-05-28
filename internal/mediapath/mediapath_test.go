package mediapath_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// newResolver builds a resolver over two temp roots ("pictures", "videos")
// and returns it alongside the picture root for filesystem assertions.
func newResolver(t *testing.T) (mediapath.Resolver, string, string) {
	t.Helper()
	base := t.TempDir()
	pics := filepath.Join(base, "pics")
	vids := filepath.Join(base, "vids")
	for _, d := range []string{pics, vids} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", d, err)
		}
	}
	r, err := mediapath.NewResolver([]catalog.Library{
		{Alias: "pictures", Name: "Pictures", Root: pics},
		{Alias: "videos", Name: "Videos", Root: vids},
	})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return r, pics, vids
}

func TestNewResolverValidation(t *testing.T) {
	abs := t.TempDir()
	t.Run("duplicate alias rejected", func(t *testing.T) {
		_, err := mediapath.NewResolver([]catalog.Library{
			{Alias: "a", Root: abs},
			{Alias: "a", Root: abs},
		})
		if err == nil {
			t.Fatal("expected error for duplicate alias")
		}
	})
	t.Run("empty alias rejected", func(t *testing.T) {
		if _, err := mediapath.NewResolver([]catalog.Library{{Alias: "", Root: abs}}); err == nil {
			t.Fatal("expected error for empty alias")
		}
	})
	t.Run("relative root rejected", func(t *testing.T) {
		if _, err := mediapath.NewResolver([]catalog.Library{{Alias: "a", Root: "relative/path"}}); err == nil {
			t.Fatal("expected error for non-absolute root")
		}
	})
}

// TestResolveRejection is the highest-value test set in the repo: every shape
// of invalid MediaPath must be rejected with the right sentinel error.
func TestResolveRejection(t *testing.T) {
	r, _, _ := newResolver(t)

	cases := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", mediapath.ErrEmptyPath},
		{"alias only is valid root", "pictures", nil}, // sanity: not a rejection
		{"unknown alias", "unknown/file.jpg", mediapath.ErrUnknownAlias},
		{"unknown alias root only", "nope", mediapath.ErrUnknownAlias},
		{"leading slash absolute", "/etc/passwd", mediapath.ErrRelativePath},
		{"leading slash with alias-like", "/pictures/a.jpg", mediapath.ErrRelativePath},
		{"dotdot escape", "pictures/../../etc/passwd", mediapath.ErrRelativePath},
		{"dotdot single", "pictures/..", mediapath.ErrRelativePath},
		{"dotdot mid", "pictures/a/../../b", mediapath.ErrRelativePath},
		{"dot segment", "pictures/./a.jpg", mediapath.ErrRelativePath},
		{"dot only", "pictures/.", mediapath.ErrRelativePath},
		{"double slash interior", "pictures//a.jpg", mediapath.ErrRelativePath},
		{"backslash traversal", `pictures\..\..\etc`, mediapath.ErrRelativePath},
		{"backslash sep", `pictures\a.jpg`, mediapath.ErrRelativePath},
		{"empty alias double-leading", "//file", mediapath.ErrRelativePath},
		{"trailing dotdot encoded as path", "pictures/sub/..", mediapath.ErrRelativePath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := r.Resolve(catalog.MediaPath(tc.in))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Resolve(%q): unexpected error %v", tc.in, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Resolve(%q): got error %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestResolveValid(t *testing.T) {
	r, pics, vids := newResolver(t)

	cases := []struct {
		in        string
		wantRoot  string
		wantRel   string
		wantAlias string
	}{
		{"pictures", pics, "", "pictures"},
		{"pictures/2021/IMG_1234.CR3", pics, "2021/IMG_1234.CR3", "pictures"},
		{"videos/clip.mp4", vids, "clip.mp4", "videos"},
		{"pictures/a/b/c.jpg", pics, "a/b/c.jpg", "pictures"},
		{"pictures/trailing/", pics, "trailing", "pictures"}, // trailing slash tolerated
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			abs, lib, rel, err := r.Resolve(catalog.MediaPath(tc.in))
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.in, err)
			}
			wantAbs := tc.wantRoot
			if tc.wantRel != "" {
				wantAbs = filepath.Join(tc.wantRoot, filepath.FromSlash(tc.wantRel))
			}
			if abs != wantAbs {
				t.Errorf("abs = %q, want %q", abs, wantAbs)
			}
			if rel != tc.wantRel {
				t.Errorf("rel = %q, want %q", rel, tc.wantRel)
			}
			if lib.Alias != tc.wantAlias {
				t.Errorf("alias = %q, want %q", lib.Alias, tc.wantAlias)
			}
		})
	}
}

func TestOpenValid(t *testing.T) {
	r, pics, _ := newResolver(t)
	want := []byte("hello-photo")
	if err := os.WriteFile(filepath.Join(pics, "a.jpg"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := r.Open("pictures/a.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOpenRejectsTraversal(t *testing.T) {
	r, _, _ := newResolver(t)
	for _, in := range []string{
		"pictures/../../etc/passwd",
		"/etc/passwd",
		"unknown/x",
		"pictures/..",
	} {
		if _, err := r.Open(catalog.MediaPath(in)); err == nil {
			t.Errorf("Open(%q): expected error, got nil", in)
		}
	}
}

// TestOpenRejectsSymlinkEscape is the os.Root payoff: a symlink that points
// outside the root must not be followable. Even though the MediaPath itself is
// lexically clean, os.Root refuses to traverse the escaping link.
func TestOpenRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	r, pics, _ := newResolver(t)

	// Create a secret file outside any library root.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a symlink inside the pictures root that points at the secret.
	link := filepath.Join(pics, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// A direct symlink-to-file open should be refused as an escape.
	_, err := r.Open("pictures/escape")
	if err == nil {
		t.Fatal("Open via escaping symlink: expected error, got nil")
	}
	if !errors.Is(err, mediapath.ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}

	// A symlinked *directory* used as a path prefix must also be refused.
	dirOutside := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirOutside, "inner.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(pics, "dirlink")
	if err := os.Symlink(dirOutside, dirLink); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	if _, err := r.Open("pictures/dirlink/inner.txt"); err == nil {
		t.Error("Open through escaping dir symlink: expected error, got nil")
	}
}

// TestResolveSymlinkEscape verifies the lexical Resolve (used for paths that
// may not exist yet, e.g. upload targets) also rejects an existing symlink
// that escapes the root.
func TestResolveSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	r, pics, _ := newResolver(t)
	outsideDir := t.TempDir()
	link := filepath.Join(pics, "linkdir")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.Resolve("pictures/linkdir/f.txt"); !errors.Is(err, mediapath.ErrEscapesRoot) {
		t.Errorf("Resolve through escaping symlink: got %v, want ErrEscapesRoot", err)
	}
}

// TestResolveNonexistentInBounds: a path that does not exist yet but stays
// within the root must resolve (so uploads can target new files).
func TestResolveNonexistentInBounds(t *testing.T) {
	r, pics, _ := newResolver(t)
	abs, _, rel, err := r.Resolve("pictures/new/upload.jpg")
	if err != nil {
		t.Fatalf("Resolve nonexistent in-bounds: %v", err)
	}
	if rel != "new/upload.jpg" {
		t.Errorf("rel = %q", rel)
	}
	if abs != filepath.Join(pics, "new", "upload.jpg") {
		t.Errorf("abs = %q", abs)
	}
}

func TestLibrariesOrderAndCopy(t *testing.T) {
	r, _, _ := newResolver(t)
	libs := r.Libraries()
	if len(libs) != 2 || libs[0].Alias != "pictures" || libs[1].Alias != "videos" {
		t.Fatalf("Libraries order wrong: %+v", libs)
	}
	// Mutating the returned slice must not affect the resolver.
	libs[0].Alias = "mutated"
	if r.Libraries()[0].Alias != "pictures" {
		t.Error("Libraries returned a non-copy slice")
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		in        string
		alias     string
		rel       string
		expectErr bool
	}{
		{"pictures/a.jpg", "pictures", "a.jpg", false},
		{"pictures", "pictures", "", false},
		{"pictures/a/b/", "pictures", "a/b", false},
		{"", "", "", true},
		{"/abs", "", "", true},
		{"a/../b", "", "", true},
	}
	for _, tc := range cases {
		alias, rel, err := mediapath.Split(catalog.MediaPath(tc.in))
		if tc.expectErr {
			if err == nil {
				t.Errorf("Split(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Split(%q): %v", tc.in, err)
			continue
		}
		if alias != tc.alias || rel != tc.rel {
			t.Errorf("Split(%q) = (%q,%q), want (%q,%q)", tc.in, alias, rel, tc.alias, tc.rel)
		}
	}
}
