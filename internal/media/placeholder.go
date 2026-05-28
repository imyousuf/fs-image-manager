package media

import (
	"bytes"
	"image"
	"image/color"
	"sync"

	"github.com/disintegration/imaging"
)

// Placeholder kinds. A placeholder stands in for a real derivative that cannot
// be produced on the host: a video poster when ffmpeg is absent, or a RAW-only
// asset thumbnail until the develop-raw worker job lands.
const (
	PlaceholderVideo = "video"
	PlaceholderRAW   = "raw"
	placeholderOther = "other"
)

// placeholderColors maps a placeholder kind to a flat fill. Muted, distinct
// tones so the grid still reads as "video vs needs-develop vs unknown" without
// any decode.
var placeholderColors = map[string]color.NRGBA{
	PlaceholderVideo: {R: 0x2b, G: 0x2b, B: 0x33, A: 0xff},
	PlaceholderRAW:   {R: 0x33, G: 0x2b, B: 0x2b, A: 0xff},
	placeholderOther: {R: 0x30, G: 0x30, B: 0x30, A: 0xff},
}

// placeholderCache memoises the encoded JPEG per (kind,size); placeholders are
// constant so there is no reason to re-render them per request.
var (
	placeholderMu    sync.Mutex
	placeholderCache = map[string][]byte{}
)

// Placeholder returns a small flat-colour JPEG for the given placeholder kind
// and size, used when a real derivative cannot be produced on the host. The
// result is memoised. It never errors in practice (encoding a solid image), so
// a render failure falls back to a 1x1 transparent-ish pixel.
func Placeholder(kind string, size int) []byte {
	if size <= 0 {
		size = DefaultThumbSize
	}
	col, ok := placeholderColors[kind]
	if !ok {
		col = placeholderColors[placeholderOther]
		kind = placeholderOther
	}
	cacheKey := kind + ":" + itoa(size)

	placeholderMu.Lock()
	defer placeholderMu.Unlock()
	if b, ok := placeholderCache[cacheKey]; ok {
		return b
	}
	img := imaging.New(size, size, col)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(jpegQuality)); err != nil {
		// Degrade to a 1x1 of the same colour; still a valid JPEG.
		tiny := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		tiny.SetNRGBA(0, 0, col)
		buf.Reset()
		_ = imaging.Encode(&buf, tiny, imaging.JPEG)
	}
	out := buf.Bytes()
	placeholderCache[cacheKey] = out
	return out
}

// itoa is a tiny non-allocating-ish int formatter to avoid importing strconv
// just for cache keys.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
