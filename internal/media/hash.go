package media

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// hashHeadBytes is how many leading bytes of a file feed the content hash. The
// catalog hash is intentionally cheap (head + size) rather than a full-file
// digest: DSLR files are large, the hash only needs to detect that a file
// changed (for cache invalidation and reconcile), and the size component makes
// truncation/extension collisions vanishingly unlikely. See
// docs/specs/_contracts.md §3 ("content hash (xxhash/sha256 of head+size)").
const hashHeadBytes = 64 << 10 // 64 KiB

// HashReader computes the content hash over the first hashHeadBytes of r plus
// the declared size. It reads at most hashHeadBytes from r. The returned string
// is a hex sha256 digest; callers treat it as opaque.
func HashReader(r io.Reader, size int64) (string, error) {
	h := sha256.New()
	if _, err := io.CopyN(h, r, hashHeadBytes); err != nil && err != io.EOF {
		return "", fmt.Errorf("media: hash read: %w", err)
	}
	var sizeBuf [8]byte
	binary.BigEndian.PutUint64(sizeBuf[:], uint64(size))
	_, _ = h.Write(sizeBuf[:])
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashFile opens path and computes its content hash (head + size). It is a
// convenience over HashReader for callers that already hold an on-disk path
// obtained from the sanctioned mediapath resolver.
func HashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the traversal-proof resolver
	if err != nil {
		return "", fmt.Errorf("media: hash open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("media: hash stat %q: %w", path, err)
	}
	return HashReader(f, info.Size())
}
