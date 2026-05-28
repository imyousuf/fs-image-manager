package metadata

import (
	"fmt"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// init registers the maker-note parsers so vendor-specific tags (Canon/Nikon
// lens info, etc.) decode. RegisterParsers is idempotent and safe at init.
func init() {
	exif.RegisterParsers(mknote.All...)
}

// exifTimeLayout is the EXIF datetime format ("YYYY:MM:DD HH:MM:SS"). EXIF has
// no timezone, so we interpret it as local time -- consistent with how DSLRs
// stamp the camera's clock.
const exifTimeLayout = "2006:01:02 15:04:05"

// extractEXIF opens mp through the resolver and decodes its EXIF into a Meta. A
// file with no decodable EXIF (e.g. a screenshot PNG) returns a zero Meta and a
// nil error -- "no metadata" is not a failure. A genuine open error is returned.
func extractEXIF(resolver mediapath.Resolver, mp catalog.MediaPath) (Meta, error) {
	f, err := resolver.Open(mp)
	if err != nil {
		return Meta{}, fmt.Errorf("metadata: open %s: %w", mp, err)
	}
	defer func() { _ = f.Close() }()

	x, err := exif.Decode(f)
	if err != nil {
		// No EXIF / unsupported -- not an error worth surfacing; the asset is
		// simply indexed without capture metadata.
		return Meta{}, nil //nolint:nilerr // absent EXIF is a normal, non-fatal outcome
	}

	m := Meta{Source: SourceEXIF}
	m.CameraMake = strings.TrimSpace(exifString(x, exif.Make))
	m.CameraModel = strings.TrimSpace(exifString(x, exif.Model))
	m.Lens = lensModel(x)
	m.Orientation = int(exifInt(x, exif.Orientation))
	m.Width = int(exifInt(x, exif.PixelXDimension))
	m.Height = int(exifInt(x, exif.PixelYDimension))
	if m.Width == 0 {
		m.Width = int(exifInt(x, exif.ImageWidth))
	}
	if m.Height == 0 {
		m.Height = int(exifInt(x, exif.ImageLength))
	}
	if t, ok := exifTime(x); ok {
		m.CapturedAt = &t
	}
	if lat, lng, ok := exifGPS(x); ok {
		m.GPSLat = &lat
		m.GPSLng = &lng
	}
	return m, nil
}

// exifString returns the trimmed string value of a tag, or "" if absent.
func exifString(x *exif.Exif, name exif.FieldName) string {
	tag, err := x.Get(name)
	if err != nil {
		return ""
	}
	s, err := tag.StringVal()
	if err != nil {
		// Fall back to the formatted representation, stripping surrounding quotes.
		return strings.Trim(tag.String(), `"`)
	}
	return s
}

// exifInt returns the first integer component of a tag, or 0 if absent/non-int.
func exifInt(x *exif.Exif, name exif.FieldName) int64 {
	tag, err := x.Get(name)
	if err != nil {
		return 0
	}
	v, err := tag.Int64(0)
	if err != nil {
		return 0
	}
	return v
}

// lensModel reads the lens description, trying the standard LensModel tag first
// and then the maker-note LensModel some bodies use.
func lensModel(x *exif.Exif) string {
	if s := exifString(x, exif.LensModel); s != "" {
		return s
	}
	if tag, err := x.Get("LensModel"); err == nil {
		return strings.Trim(tag.String(), `"`)
	}
	return ""
}

// exifTime returns the capture time, preferring DateTimeOriginal (when the photo
// was taken) over DateTime (last modified). EXIF carries no zone, so the stamp
// is read as local time.
func exifTime(x *exif.Exif) (time.Time, bool) {
	for _, name := range []exif.FieldName{exif.DateTimeOriginal, exif.DateTimeDigitized, exif.DateTime} {
		s := exifString(x, name)
		if s == "" {
			continue
		}
		if t, err := time.ParseInLocation(exifTimeLayout, s, time.Local); err == nil {
			return t, true
		}
	}
	// goexif also exposes a convenience parser that consults the same tags.
	if t, err := x.DateTime(); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// exifGPS returns decimal-degree coordinates when both are present (rare on a
// DSLR library). goexif handles the ref-sign and DMS->decimal conversion.
func exifGPS(x *exif.Exif) (lat, lng float64, ok bool) {
	lat, lng, err := x.LatLong()
	if err != nil {
		return 0, 0, false
	}
	// LatLong returns (0,0) with a nil error only when the tags decode to the
	// null island; treat an exact zero pair as "no GPS" to avoid false pins.
	if lat == 0 && lng == 0 {
		return 0, 0, false
	}
	return lat, lng, true
}
