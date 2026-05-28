package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ChecksumFileName is the manifest published alongside the release tarballs by
// the release workflow (sha256sum format: "<hex>␠␠<name>" per line).
const ChecksumFileName = "SHA256SUMS"

// VerifyChecksum confirms that the SHA256 of data matches the entry for
// assetName in a SHA256SUMS manifest. The manifest follows GNU coreutils
// `sha256sum` output: each line is "<64-hex-digest>  <filename>" (two spaces for
// text mode, " *" for binary mode — both accepted). The filename may be bare or
// path-prefixed; only the base name is compared.
func VerifyChecksum(assetName string, data, sums []byte) error {
	want, ok := lookupChecksum(string(sums), assetName)
	if !ok {
		return fmt.Errorf("selfupdate: no checksum for %q in %s", assetName, ChecksumFileName)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("selfupdate: checksum mismatch for %q: want %s, got %s", assetName, want, got)
	}
	return nil
}

// lookupChecksum returns the expected hex digest for assetName from a SHA256SUMS
// manifest, comparing by base file name.
func lookupChecksum(manifest, assetName string) (digest string, ok bool) {
	wantBase := baseName(assetName)
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// fields[0] = digest; the remainder is the filename (may contain a
		// leading '*' binary-mode marker). Rejoin in case of spaces in names.
		name := strings.TrimPrefix(strings.Join(fields[1:], " "), "*")
		if baseName(name) == wantBase {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// baseName returns the final path element, handling both / and \ separators so
// manifests generated on any platform compare correctly.
func baseName(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// sameVersion compares two release version strings tolerant of a missing "v"
// prefix and surrounding whitespace ("0.2.0" == "v0.2.0").
func sameVersion(a, b string) bool {
	norm := func(s string) string {
		return strings.TrimPrefix(strings.TrimSpace(s), "v")
	}
	a, b = norm(a), norm(b)
	if a == "" || b == "" {
		return false // unknown current version is never "up to date"
	}
	return a == b
}
