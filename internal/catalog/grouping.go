package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"time"
)

// This file is media-pipeline-owned (repo behaviour, per
// docs/specs/_contracts.md §1): the pure grouping logic that collapses files
// sharing (alias, dir, basename-without-ext) into a single Asset. It depends
// only on the types in types.go and is deterministic so it is trivially
// unit-testable without a database.
//
// CRITICAL grouping rules (docs/specs/media-pipeline.md, _contracts.md §3):
//   - Files with the same (alias, dir, basename-sans-ext) are ONE asset.
//   - DisplayPath prefers a JPG sibling, then another decodable image, then an
//     "other" image; a RAW-only group has NO host display source and is marked
//     needs-develop (the develop-raw job fills it later) — we NEVER pick the RAW
//     as a display source when a JPG sibling exists (never decode RAW on host).
//   - .xmp/.pp3/.thm (and any non-media) attach as Kind=sidecar and never form
//     a standalone asset; a group of only sidecars is dropped.
//   - A video file forms a video asset; videos and images never share an asset
//     even at the same basename (distinct media kinds).

// fileClassifier abstracts extension classification so the grouping logic does
// not import internal/media (which would be a cycle: media has no need of
// catalog grouping, but keeping catalog dependency-light matters). The repo
// wires media.ClassifyExt in via SetClassifier; tests can inject their own.
type fileClassifier func(name string) FileKind

// AssetKind values for Asset.Kind.
const (
	AssetKindImage = "image"
	AssetKindVideo = "video"
)

// groupKey identifies the bucket a file falls into. Image and video files at
// the same basename are deliberately separated by isVideo so they never merge.
type groupKey struct {
	alias   string
	dir     string
	base    string // basename without extension
	isVideo bool
}

// AssetIDFor returns the stable asset id: a hex digest of
// "<alias>/<dir>/<basename-without-ext>" (docs/specs/_contracts.md §3). It is
// independent of which member files currently exist, so re-scans of the same
// logical photo keep the same id.
func AssetIDFor(alias, dir, base string) string {
	key := alias + "/" + dir + "/" + base
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:32]
}

// baseAndExt splits a filename into its lower-cased basename-without-extension
// and lower-cased extension (no dot). Multi-dot names keep everything before
// the final dot as the base.
func baseAndExt(name string) (base, ext string) {
	e := path.Ext(name)
	base = strings.TrimSuffix(name, e)
	if e != "" {
		ext = strings.ToLower(e[1:])
	}
	return base, ext
}

// FileInput is the public, dependency-light description of one on-disk file fed
// to Group. The ingest layer builds these from a directory listing; the repo
// builds them from DB rows. MediaPath is the alias-prefixed path; Name is its
// base filename (with extension) used for classification and basename grouping.
type FileInput struct {
	Alias     string
	Dir       string
	Name      string
	MediaPath MediaPath
	Size      int64
	ModTime   time.Time
	Hash      string
}

// Group collapses a flat set of files into Assets following the CRITICAL
// grouping rules (see file header). classify maps a filename to its FileKind
// (wire media.ClassifyExt). Output is deterministically ordered by (dir, base).
func Group(inputs []FileInput, classify Classifier) []Asset {
	entries := make([]fileEntry, 0, len(inputs))
	for _, in := range inputs {
		entries = append(entries, fileEntry{
			alias:     in.Alias,
			dir:       in.Dir,
			name:      in.Name,
			mediaPath: in.MediaPath,
			size:      in.Size,
			modTime:   in.ModTime,
			hash:      in.Hash,
		})
	}
	return groupFiles(entries, fileClassifier(classify))
}

// groupFiles collapses a flat set of files (each an alias/dir/name triple plus
// its kind) into Assets following the CRITICAL grouping rules above. classify
// maps a filename to its FileKind. The input order does not affect the result;
// outputs are deterministically ordered by (dir, base).
//
// fileEntry carries everything grouping needs about one on-disk file without
// pulling in the full File (which also needs size/mtime/hash). The repo builds
// these from a directory listing or DB rows.
func groupFiles(entries []fileEntry, classify fileClassifier) []Asset {
	buckets := make(map[groupKey][]fileEntry)
	for _, e := range entries {
		kind := classify(e.name)
		base, _ := baseAndExt(e.name)
		isVideo := kind == FileKindVideo
		// Sidecars must attach to a base group; they never decide media kind.
		// They join whichever (image|video) group shares their basename. We add
		// them to the image bucket by default and reconcile to video below if
		// the only sibling is a video — handled after bucketing.
		k := groupKey{alias: e.alias, dir: e.dir, base: base, isVideo: isVideo}
		buckets[k] = append(buckets[k], fileEntry{
			alias: e.alias, dir: e.dir, name: e.name, kind: kind,
			mediaPath: e.mediaPath, size: e.size, modTime: e.modTime, hash: e.hash,
		})
	}

	// Sidecars bucketed as image (isVideo=false) but whose only real sibling is
	// a video need to migrate to the video bucket. Detect: for each non-video
	// bucket that contains ONLY sidecars, if a video bucket with the same base
	// exists, fold the sidecars in.
	reassignSidecars(buckets)

	assets := make([]Asset, 0, len(buckets))
	for k, files := range buckets {
		a, ok := buildAsset(k, files)
		if ok {
			assets = append(assets, a)
		}
	}
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].Dir != assets[j].Dir {
			return assets[i].Dir < assets[j].Dir
		}
		if assets[i].BaseName != assets[j].BaseName {
			return assets[i].BaseName < assets[j].BaseName
		}
		return assets[i].Kind < assets[j].Kind
	})
	return assets
}

// reassignSidecars moves sidecar-only image buckets into a sibling video bucket
// (same alias/dir/base) so a .thm next to a video attaches to the video asset.
func reassignSidecars(buckets map[groupKey][]fileEntry) {
	for k, files := range buckets {
		if k.isVideo {
			continue
		}
		if !allSidecars(files) {
			continue
		}
		vk := groupKey{alias: k.alias, dir: k.dir, base: k.base, isVideo: true}
		if _, ok := buckets[vk]; ok {
			buckets[vk] = append(buckets[vk], files...)
			delete(buckets, k)
		}
	}
}

func allSidecars(files []fileEntry) bool {
	for _, f := range files {
		if f.kind != FileKindSidecar {
			return false
		}
	}
	return len(files) > 0
}

// buildAsset turns one bucket into an Asset, choosing DisplayPath. It returns
// ok=false for a bucket containing only sidecars (no standalone asset).
func buildAsset(k groupKey, entries []fileEntry) (Asset, bool) {
	// Deterministic member order: by media path.
	sort.Slice(entries, func(i, j int) bool { return entries[i].mediaPath < entries[j].mediaPath })

	files := make([]File, 0, len(entries))
	hasNonSidecar := false
	for _, e := range entries {
		if e.kind != FileKindSidecar {
			hasNonSidecar = true
		}
		files = append(files, File{
			MediaPath: e.mediaPath,
			Kind:      e.kind,
			Size:      e.size,
			ModTime:   e.modTime,
			Hash:      e.hash,
		})
	}
	if !hasNonSidecar {
		return Asset{}, false
	}

	kind := AssetKindImage
	if k.isVideo {
		kind = AssetKindVideo
	}

	return Asset{
		ID:          AssetIDFor(k.alias, k.dir, k.base),
		Alias:       k.alias,
		Dir:         k.dir,
		BaseName:    k.base,
		Kind:        kind,
		Files:       files,
		DisplayPath: chooseDisplay(k.isVideo, files),
	}, true
}

// NeedsDevelop reports whether an asset is RAW-only and awaiting a develop-raw
// job: an image asset with no host display source (DisplayPath==""). Videos and
// images that already have a JPG/PNG/other display source never need develop.
func NeedsDevelop(a Asset) bool {
	return a.Kind == AssetKindImage && a.DisplayPath == ""
}

// chooseDisplay picks the display source for an asset's files. For a video the
// display source is the video file itself (the poster is derived from it). For
// an image we prefer JPG, then PNG, then any other host image, then — RAW-only
// — leave the display empty (NeedsDevelop reports it). We NEVER pick the RAW as
// a display source.
func chooseDisplay(isVideo bool, files []File) (display MediaPath) {
	if isVideo {
		for _, f := range files {
			if f.Kind == FileKindVideo {
				return f.MediaPath
			}
		}
		return ""
	}
	var jpg, png, other MediaPath
	for _, f := range files {
		switch f.Kind {
		case FileKindJPG:
			if jpg == "" {
				jpg = f.MediaPath
			}
		case FileKindPNG:
			if png == "" {
				png = f.MediaPath
			}
		case FileKindOther:
			if other == "" {
				other = f.MediaPath
			}
		}
	}
	switch {
	case jpg != "":
		return jpg
	case png != "":
		return png
	case other != "":
		// A non-decodable image (e.g. heic): set as display so the path is
		// known. Whether the host can actually decode it is the serving layer's
		// concern (it falls back to a placeholder if not).
		return other
	default:
		// RAW-only (or no image members): NEVER decode the RAW on the host. No
		// display source until a develop-raw job produces a derivative;
		// NeedsDevelop reports this.
		return ""
	}
}
