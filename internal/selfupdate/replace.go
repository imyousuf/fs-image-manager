package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// maxBinarySize caps the bytes read for a single tar entry, guarding against a
// malformed/hostile archive claiming an enormous file. 512 MiB is far above any
// realistic fs-image-manager binary.
const maxBinarySize = 512 << 20

// ExtractBinary reads a gzip-compressed tar archive and returns the contents of
// the entry whose base name equals binaryName. The release tarballs place the
// binary either at the archive root or under a versioned directory, so the
// match is by base name.
func ExtractBinary(targz []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		buf := make([]byte, 0, hdr.Size)
		b := bytes.NewBuffer(buf)
		if _, err := io.Copy(b, io.LimitReader(tr, maxBinarySize)); err != nil {
			return nil, fmt.Errorf("read %q from tar: %w", binaryName, err)
		}
		if b.Len() == 0 {
			return nil, fmt.Errorf("selfupdate: %q in archive is empty", binaryName)
		}
		return b.Bytes(), nil
	}
	return nil, fmt.Errorf("selfupdate: %q not found in archive", binaryName)
}

// ReplaceBinary atomically replaces the file at path with newBinary, preserving
// the original file's mode (or 0o755 if it cannot be determined).
//
// The new bytes are written to a temp file in the same directory (so the final
// rename is atomic on POSIX — rename within a filesystem is atomic) and fsync'd
// before the rename, so an interrupted update never leaves a half-written or
// missing binary. On success the running file is swapped in a single rename.
func ReplaceBinary(path string, newBinary []byte) (err error) {
	dir := filepath.Dir(path)

	mode := os.FileMode(0o755)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".fsim-update-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename succeeds.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(newBinary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp binary: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}
	if err = os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp binary: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic rename into place: %w", err)
	}
	return nil
}
