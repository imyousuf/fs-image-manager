package metadata

import (
	"testing"
	"time"
)

// TestParseFFprobe checks the JSON normalization against a canned ffprobe output
// (no binary needed): codec/dimensions from the first video stream, duration in
// ms from the format, the creation_time tag, and the ISO 6709 location tag.
func TestParseFFprobe(t *testing.T) {
	const sample = `{
	  "streams": [
	    {"codec_type": "audio", "codec_name": "aac"},
	    {"codec_type": "video", "codec_name": "h264", "width": 3840, "height": 2160, "duration": "12.480000"}
	  ],
	  "format": {
	    "duration": "12.520000",
	    "tags": {
	      "creation_time": "2021-03-05T16:13:31.000000Z",
	      "location": "+37.7749-122.4194/"
	    }
	  }
	}`

	m, err := parseFFprobe([]byte(sample))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if m.Source != SourceFFprobe {
		t.Errorf("source = %q, want ffprobe", m.Source)
	}
	if m.Codec != "h264" {
		t.Errorf("codec = %q, want h264", m.Codec)
	}
	if m.Width != 3840 || m.Height != 2160 {
		t.Errorf("dimensions = %dx%d, want 3840x2160", m.Width, m.Height)
	}
	if m.DurationMs != 12520 {
		t.Errorf("durationMs = %d, want 12520", m.DurationMs)
	}
	if m.CapturedAt == nil || !m.CapturedAt.Equal(time.Date(2021, 3, 5, 16, 13, 31, 0, time.UTC)) {
		t.Errorf("capturedAt = %v, want 2021-03-05T16:13:31Z", m.CapturedAt)
	}
	if m.GPSLat == nil || m.GPSLng == nil {
		t.Fatalf("expected GPS, got lat=%v lng=%v", m.GPSLat, m.GPSLng)
	}
	if *m.GPSLat != 37.7749 || *m.GPSLng != -122.4194 {
		t.Errorf("gps = %v,%v want 37.7749,-122.4194", *m.GPSLat, *m.GPSLng)
	}
}

// TestParseFFprobeMinimal: a clip with only a video stream and no tags still
// yields codec/dimensions and a nil capture time / GPS.
func TestParseFFprobeMinimal(t *testing.T) {
	const sample = `{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080}],"format":{"duration":"5.0"}}`
	m, err := parseFFprobe([]byte(sample))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if m.Codec != "hevc" || m.DurationMs != 5000 {
		t.Errorf("got codec=%q durationMs=%d", m.Codec, m.DurationMs)
	}
	if m.CapturedAt != nil || m.GPSLat != nil {
		t.Errorf("expected no time/gps, got %+v", m)
	}
}

func TestDurationMs(t *testing.T) {
	cases := map[string]int64{
		"":          0,
		"0":         0,
		"1":         1000,
		"12.480000": 12480,
		"0.5":       500,
		"bogus":     0,
	}
	for in, want := range cases {
		if got := durationMs(in); got != want {
			t.Errorf("durationMs(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseISO6709(t *testing.T) {
	cases := []struct {
		in       string
		lat, lng float64
		ok       bool
	}{
		{"+37.7749-122.4194/", 37.7749, -122.4194, true},
		{"-33.8688+151.2093/", -33.8688, 151.2093, true},
		{"+12.34+56.78+010.5/", 12.34, 56.78, true}, // with altitude
		{"garbage", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		lat, lng, ok := parseISO6709(c.in)
		if ok != c.ok || (ok && (lat != c.lat || lng != c.lng)) {
			t.Errorf("parseISO6709(%q) = %v,%v,%v want %v,%v,%v", c.in, lat, lng, ok, c.lat, c.lng, c.ok)
		}
	}
}

func TestJoinCamera(t *testing.T) {
	cases := []struct {
		make, model, want string
	}{
		{"Canon", "Canon EOS R5", "Canon EOS R5"},
		{"NIKON CORPORATION", "NIKON D850", "NIKON D850"},
		{"Canon", "", "Canon"},
		{"", "EOS R5", "EOS R5"},
		{"Sony", "ILCE-7M3", "Sony ILCE-7M3"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := joinCamera(c.make, c.model); got != c.want {
			t.Errorf("joinCamera(%q,%q) = %q, want %q", c.make, c.model, got, c.want)
		}
	}
}
