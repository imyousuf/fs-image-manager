package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestVerifyChecksum(t *testing.T) {
	asset := []byte("the release tarball bytes")
	name := "fs-image-manager_v1.2.3_linux_amd64.tar.gz"
	good := sha256hex(asset)

	tests := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{
			name:     "two-space text mode",
			manifest: fmt.Sprintf("%s  %s\n", good, name),
		},
		{
			name:     "binary mode star marker",
			manifest: fmt.Sprintf("%s *%s\n", good, name),
		},
		{
			name:     "uppercase digest still matches",
			manifest: fmt.Sprintf("%s  %s\n", strings.ToUpper(good), name),
		},
		{
			name: "multiple entries, picks the right one",
			manifest: "deadbeef  other_file.tar.gz\n" +
				fmt.Sprintf("%s  %s\n", good, name),
		},
		{
			name:     "path-prefixed name compared by base",
			manifest: fmt.Sprintf("%s  dist/%s\n", good, name),
		},
		{
			name:     "missing entry",
			manifest: "deadbeef  some_other.tar.gz\n",
			wantErr:  "no checksum",
		},
		{
			name:     "wrong digest",
			manifest: fmt.Sprintf("%s  %s\n", sha256hex([]byte("different")), name),
			wantErr:  "checksum mismatch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyChecksum(name, asset, []byte(tc.manifest))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSameVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "v1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "1.2.3", true},
		{" v1.2.3 ", "v1.2.3", true},
		{"v1.2.3", "v1.2.4", false},
		{"", "v1.2.3", false}, // unknown current is never up to date
		{"v1.2.3", "", false},
	}
	for _, tc := range tests {
		if got := sameVersion(tc.a, tc.b); got != tc.want {
			t.Errorf("sameVersion(%q,%q)=%v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
