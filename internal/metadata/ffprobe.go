package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// ffprobeExtractor runs the host's ffprobe to read video container/stream facts.
// ffprobe is an optional runtime dependency (docs/specs/_contracts.md sec.0): if
// it is not found, path is "" and extraction is a graceful no-op.
type ffprobeExtractor struct {
	path string // resolved ffprobe binary path; "" = unavailable
}

// newFFprobe resolves the ffprobe binary. An explicit override wins (tests); a
// non-empty value that does not exist on PATH disables video extraction rather
// than erroring -- the worker/host may simply lack ffprobe.
func newFFprobe(override string) *ffprobeExtractor {
	if override != "" {
		return &ffprobeExtractor{path: override}
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return &ffprobeExtractor{path: p}
	}
	return &ffprobeExtractor{}
}

// available reports whether ffprobe was found.
func (f *ffprobeExtractor) available() bool { return f != nil && f.path != "" }

// extract runs ffprobe over the video at mp and normalizes its JSON into a Meta.
// With no ffprobe it returns an empty Meta (Source none) and a nil error so
// videos are still cataloged/browsable; a real ffprobe failure is returned.
func (f *ffprobeExtractor) extract(ctx context.Context, resolver mediapath.Resolver, mp catalog.MediaPath) (Meta, error) {
	if !f.available() {
		return Meta{}, nil
	}
	abs, _, _, err := resolver.Resolve(mp)
	if err != nil {
		return Meta{}, fmt.Errorf("metadata: resolve %s: %w", mp, err)
	}

	// -v error keeps stderr quiet; we only consume stdout JSON.
	cmd := exec.CommandContext(ctx, f.path, //nolint:gosec // path is from LookPath or a test override; abs is resolver-validated
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		abs,
	)
	out, err := cmd.Output()
	if err != nil {
		return Meta{}, fmt.Errorf("metadata: ffprobe %s: %w", mp, err)
	}
	return parseFFprobe(out)
}

// ffprobeOutput mirrors the subset of ffprobe's JSON we consume.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecType string `json:"codec_type"` // "video" | "audio" | ...
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  string `json:"duration"` // seconds, as a string
}

type ffprobeFormat struct {
	Duration string            `json:"duration"` // seconds, as a string
	Tags     map[string]string `json:"tags"`
}

// parseFFprobe normalizes ffprobe JSON into a Meta: duration (ms), the first
// video stream's codec/dimensions, the creation time tag (when present) and GPS
// from the QuickTime location tag (sparse). It is split out for direct testing
// against canned ffprobe output without invoking the binary.
func parseFFprobe(data []byte) (Meta, error) {
	var o ffprobeOutput
	if err := json.Unmarshal(data, &o); err != nil {
		return Meta{}, fmt.Errorf("metadata: parse ffprobe json: %w", err)
	}

	m := Meta{Source: SourceFFprobe}
	m.DurationMs = durationMs(o.Format.Duration)

	for _, s := range o.Streams {
		if s.CodecType != "video" {
			continue
		}
		m.Codec = s.CodecName
		m.Width = s.Width
		m.Height = s.Height
		if m.DurationMs == 0 {
			m.DurationMs = durationMs(s.Duration)
		}
		break // first video stream wins
	}

	if t, ok := creationTime(o.Format.Tags); ok {
		m.CapturedAt = &t
	}
	if lat, lng, ok := locationTag(o.Format.Tags); ok {
		m.GPSLat = &lat
		m.GPSLng = &lng
	}
	return m, nil
}

// durationMs converts ffprobe's fractional-seconds string ("12.480000") to whole
// milliseconds. An empty/unparseable value yields 0.
func durationMs(s string) int64 {
	if s == "" {
		return 0
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil || secs < 0 {
		return 0
	}
	return int64(secs*1000 + 0.5)
}

// creationTimeLayouts are the timestamp formats ffmpeg writes into the
// creation_time tag across container types.
var creationTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000000Z",
	"2006-01-02 15:04:05",
}

// creationTime reads the container's creation time tag (case-insensitive key)
// and parses it against the known layouts.
func creationTime(tags map[string]string) (time.Time, bool) {
	v := tagValue(tags, "creation_time")
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range creationTimeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// locationTag parses the QuickTime/MP4 "location" tag (ISO 6709, e.g.
// "+37.7749-122.4194/") into decimal-degree coordinates. Most DSLR clips lack
// it; this is the rare geotagged case.
func locationTag(tags map[string]string) (lat, lng float64, ok bool) {
	v := tagValue(tags, "location")
	if v == "" {
		v = tagValue(tags, "com.apple.quicktime.location.ISO6709")
	}
	if v == "" {
		return 0, 0, false
	}
	return parseISO6709(v)
}

// parseISO6709 parses a leading "+DD.DDDD-DDD.DDDD" signed lat/long pair. It
// stops at the first non-numeric terminator (the trailing altitude or "/").
func parseISO6709(s string) (lat, lng float64, ok bool) {
	s = strings.TrimSpace(s)
	// Find the boundary between the latitude and the (signed) longitude: the
	// second sign character starting from index 1.
	split := -1
	for i := 1; i < len(s); i++ {
		if s[i] == '+' || s[i] == '-' {
			split = i
			break
		}
	}
	if split <= 0 {
		return 0, 0, false
	}
	latStr := s[:split]
	rest := s[split:]
	// The longitude runs until a trailing '/', '+'/'-' (altitude) or end.
	end := len(rest)
	for i := 1; i < len(rest); i++ {
		if rest[i] == '+' || rest[i] == '-' || rest[i] == '/' {
			end = i
			break
		}
	}
	lngStr := rest[:end]
	lat, err1 := strconv.ParseFloat(latStr, 64)
	lng, err2 := strconv.ParseFloat(lngStr, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lat, lng, true
}

// tagValue does a case-insensitive lookup over the (small) tags map.
func tagValue(tags map[string]string, key string) string {
	if tags == nil {
		return ""
	}
	if v, ok := tags[key]; ok {
		return v
	}
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
