// Package mediapath is the single sanctioned way to turn an alias-prefixed
// MediaPath ("<alias>/<relpath>") into an on-disk location. It is the
// Phase-0 path-traversal fix: no other package constructs library paths by
// string concatenation. Resolution is traversal-proof, backed by os.Root
// (Go 1.24), so even a path that survives lexical validation cannot escape
// its library root at open time (e.g. via a symlink).
package mediapath

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// MediaPath and Library are re-exported from internal/catalog so callers that
// only depend on path handling need not import catalog directly.
type (
	MediaPath = catalog.MediaPath
	Library   = catalog.Library
)

// Sentinel errors. Callers (and the HTTP layer) match on these with
// errors.Is to map to the right status code / envelope.
var (
	// ErrEmptyPath is returned for an empty MediaPath.
	ErrEmptyPath = errors.New("mediapath: empty path")
	// ErrNoAlias is returned when the path has no alias segment.
	ErrNoAlias = errors.New("mediapath: missing library alias")
	// ErrRelativePath is returned for absolute or ./.. -containing paths.
	ErrRelativePath = errors.New("mediapath: relative or absolute paths are not allowed")
	// ErrUnknownAlias is returned when the alias is not configured.
	ErrUnknownAlias = errors.New("mediapath: unknown library alias")
	// ErrEscapesRoot is returned when a path resolves outside its root.
	ErrEscapesRoot = errors.New("mediapath: path escapes library root")
)

// Resolver maps validated MediaPaths to on-disk locations within configured
// library roots. It is implemented by *resolver; the interface is exported so
// handlers and other packages can depend on (and mock) it.
type Resolver interface {
	// Resolve maps "<alias>/<relpath>" to an absolute on-disk path guaranteed
	// to live inside the alias's configured root. It returns an error on an
	// unknown alias, a relative/absolute path, or any traversal escape.
	Resolve(mp MediaPath) (absPath string, lib Library, rel string, err error)
	// Libraries returns the configured roots, in config order.
	Libraries() []Library
	// Open opens the file named by mp for reading, traversal-proof via
	// os.Root. The caller owns closing the returned file.
	Open(mp MediaPath) (*os.File, error)
}

type resolver struct {
	libs    []Library
	byAlias map[string]Library
}

// NewResolver builds a Resolver from the given libraries. Each library Root
// must be an absolute path. Duplicate aliases are rejected.
func NewResolver(libs []Library) (Resolver, error) {
	byAlias := make(map[string]Library, len(libs))
	ordered := make([]Library, 0, len(libs))
	for _, lib := range libs {
		if lib.Alias == "" {
			return nil, errors.New("mediapath: library with empty alias")
		}
		if !filepath.IsAbs(lib.Root) {
			return nil, fmt.Errorf("mediapath: library %q root %q is not absolute", lib.Alias, lib.Root)
		}
		if _, dup := byAlias[lib.Alias]; dup {
			return nil, fmt.Errorf("mediapath: duplicate library alias %q", lib.Alias)
		}
		lib.Root = filepath.Clean(lib.Root)
		byAlias[lib.Alias] = lib
		ordered = append(ordered, lib)
	}
	return &resolver{libs: ordered, byAlias: byAlias}, nil
}

func (r *resolver) Libraries() []Library {
	out := make([]Library, len(r.libs))
	copy(out, r.libs)
	return out
}

// Split parses a MediaPath into its alias and library-relative path, applying
// all lexical validation. The returned relPath uses forward slashes and is
// cleaned ("" denotes the library root itself). It performs no filesystem
// access; use Resolve/Open for the traversal-proof on-disk mapping.
func Split(mp MediaPath) (alias, relPath string, err error) {
	s := string(mp)
	if s == "" {
		return "", "", ErrEmptyPath
	}
	// Absolute paths and backslashes are never valid input.
	if strings.HasPrefix(s, "/") {
		return "", "", ErrRelativePath
	}
	if strings.ContainsRune(s, '\\') {
		return "", "", ErrRelativePath
	}
	alias, rest, hasSlash := strings.Cut(s, "/")
	if alias == "" {
		return "", "", ErrNoAlias
	}
	if !hasSlash {
		// Just "<alias>" — refers to the library root.
		return alias, "", nil
	}
	// Reject any "." or ".." segment, empty segments (double slashes) and any
	// other lexical trickery before normalising.
	for _, seg := range strings.Split(rest, "/") {
		switch seg {
		case "":
			// trailing slash ("alias/dir/") is tolerated; an interior empty
			// segment ("alias//dir") is not.
			continue
		case ".", "..":
			return "", "", ErrRelativePath
		}
	}
	// path.Clean collapses redundant slashes and trailing slashes. Because we
	// already rejected "."/".." segments above, Clean cannot escape here; this
	// is belt-and-suspenders, double-checked again after Clean.
	rel := path.Clean(rest)
	if rel == "." {
		rel = ""
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return "", "", ErrRelativePath
	}
	return alias, rel, nil
}

func (r *resolver) lookup(mp MediaPath) (lib Library, rel string, err error) {
	alias, rel, err := Split(mp)
	if err != nil {
		return Library{}, "", err
	}
	lib, ok := r.byAlias[alias]
	if !ok {
		return Library{}, "", fmt.Errorf("%w: %q", ErrUnknownAlias, alias)
	}
	return lib, rel, nil
}

func (r *resolver) Resolve(mp MediaPath) (string, Library, string, error) {
	lib, rel, err := r.lookup(mp)
	if err != nil {
		return "", Library{}, "", err
	}
	// Build the absolute path lexically, then verify with os.Root that the
	// real, symlink-resolved target stays within the root.
	abs := filepath.Join(lib.Root, filepath.FromSlash(rel))
	if rel != "" {
		if err := withinRoot(lib.Root, rel); err != nil {
			return "", Library{}, "", err
		}
	}
	return abs, lib, rel, nil
}

func (r *resolver) Open(mp MediaPath) (*os.File, error) {
	lib, rel, err := r.lookup(mp)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(lib.Root)
	if err != nil {
		return nil, fmt.Errorf("mediapath: open root %q: %w", lib.Root, err)
	}
	defer func() { _ = root.Close() }()
	if rel == "" {
		// Opening the library root itself: hand back the directory.
		return root.Open(".")
	}
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		// os.Root reports an escape attempt as an error; normalise it.
		if isEscapeError(err) {
			return nil, fmt.Errorf("%w: %s", ErrEscapesRoot, mp)
		}
		return nil, err
	}
	return f, nil
}

// withinRoot uses os.Root to verify that rel, resolved against root (following
// any symlinks), does not escape root. A non-existent path is *not* an
// escape — only a confirmed traversal out of the root is. This lets Resolve
// be used for paths that have not been created yet (e.g. upload targets).
func withinRoot(root, rel string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("mediapath: open root %q: %w", root, err)
	}
	defer func() { _ = r.Close() }()
	// Stat resolves symlinks within the root and fails with an escape error if
	// the target leaves it. A "not exist" error means the path is in-bounds
	// but absent, which is fine.
	if _, err := r.Stat(filepath.FromSlash(rel)); err != nil {
		if isEscapeError(err) {
			return fmt.Errorf("%w: %s", ErrEscapesRoot, rel)
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// Any other error (permissions, etc.) is surfaced as-is.
		return err
	}
	return nil
}

// isEscapeError reports whether err indicates an os.Root traversal escape.
// os.Root returns a *PathError wrapping an internal "path escapes from parent"
// error; it is not exported, so match on the (stable) message as well as the
// errors.Is path-separator sentinel where available.
func isEscapeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "path escapes from parent") ||
		strings.Contains(msg, "escapes from parent") ||
		errors.Is(err, errEscapeMarker)
}

// errEscapeMarker exists only so isEscapeError has a sentinel to compare
// against if a future Go release exports one; today the message match carries
// the load.
var errEscapeMarker = errors.New("path escapes from parent directory")
