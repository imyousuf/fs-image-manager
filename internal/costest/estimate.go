package costest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// DefaultAvgFacesPerImage is the assumed number of faces per image when faces
// have NOT yet been indexed in the DB. Personal libraries skew low (many
// landscapes/objects, some portraits); 1.5 is a deliberately rough planning
// figure. Override with the -faces flag for a what-if, or populate the DB and
// the actual stored face count is used instead.
const DefaultAvgFacesPerImage = 1.5

// Inputs are the measured (or supplied) facts about the library that the cost
// model prices. They are produced by an InputSource so the estimator can be
// driven from a real catalog/people DB OR from pure what-if flags, and so tests
// need neither a DB nor the network.
type Inputs struct {
	// Images is the number of IMAGE assets (face-index candidates). Videos are
	// excluded: Rekognition IndexFaces here runs over still images.
	Images int64 `json:"images"`
	// SizeBytes is the total size of the library's media on disk, used to price
	// cloud object storage. This is the full library (images + videos + sidecars),
	// because the whole tree is what gets synced to the bucket.
	SizeBytes int64 `json:"sizeBytes"`
	// Faces is the number of stored face vectors to price for ongoing metadata
	// storage. When FacesAreActual is true it is a real count read from the DB;
	// otherwise it is an estimate (Images * avg-faces-per-image).
	Faces int64 `json:"faces"`
	// FacesAreActual marks whether Faces came from the indexed DB (true) or was
	// estimated (false). The report labels the line accordingly.
	FacesAreActual bool `json:"facesAreActual"`
	// ImagesAreActual / SizeIsActual likewise mark whether Images / SizeBytes
	// were measured from the catalog (true) or supplied via what-if flags (false).
	ImagesAreActual bool `json:"imagesAreActual"`
	SizeIsActual    bool `json:"sizeIsActual"`
}

// InputSource yields the Inputs to price. The CLI implements one over the
// catalog + people repos; tests use a trivial fake. It is an interface so no
// DB/network is required to exercise the cost math.
type InputSource interface {
	Collect(ctx context.Context) (Inputs, error)
}

// Options tune the estimate. Zero values are sensible: AvgFacesPerImage falls
// back to DefaultAvgFacesPerImage, and IncludeFreeTier defaults via its own
// caller (the CLI sets it explicitly).
type Options struct {
	// AvgFacesPerImage is the multiplier used to ESTIMATE faces when the DB has
	// none indexed. Ignored when the InputSource reports actual faces. <= 0 uses
	// DefaultAvgFacesPerImage.
	AvgFacesPerImage float64
	// ApplyFreeTier subtracts the Rekognition 12-month free-tier allowances
	// (images analyzed, faces stored) from the priced quantities. The user opts
	// in because the free tier only applies in the first 12 months of an account.
	ApplyFreeTier bool
}

// StorageLine is the monthly storage cost for one provider/tier at the library
// size.
type StorageLine struct {
	Provider      string  `json:"provider"`
	Tier          string  `json:"tier"`
	USDPerGBMonth float64 `json:"usdPerGBMonth"`
	Note          string  `json:"note"`
	MonthlyUSD    float64 `json:"monthlyUSD"`
}

// RekognitionTierCharge is the indexing charge contributed by one volume tier.
type RekognitionTierCharge struct {
	UpToImages  int64   `json:"upToImages"` // 0 => unbounded final tier
	USDPerImage float64 `json:"usdPerImage"`
	Images      int64   `json:"images"` // images priced in this tier
	USD         float64 `json:"usd"`    // tier subtotal
}

// Estimate is the full priced breakdown. Money fields are USD. The JSON tags
// drive the -json output so the wire shape is stable and testable.
type Estimate struct {
	PricingAsOf string `json:"pricingAsOf"`

	Inputs Inputs `json:"inputs"`

	// Storage: the library size priced at each hot tier (monthly). ArchiveNote
	// summarises the cheaper cold tiers without pricing them.
	GiBNote     string        `json:"gibNote"`
	SizeGB      float64       `json:"sizeGB"`
	Storage     []StorageLine `json:"storage"`
	ArchiveNote string        `json:"archiveNote"`

	// Rekognition one-time indexing.
	IndexImagesPriced int64                   `json:"indexImagesPriced"` // after free tier
	FreeTierImages    int64                   `json:"freeTierImages"`    // images covered free
	IndexTiers        []RekognitionTierCharge `json:"indexTiers"`
	IndexOneTimeUSD   float64                 `json:"indexOneTimeUSD"`

	// Rekognition ongoing face-metadata storage (monthly).
	FacesPriced       int64   `json:"facesPriced"`   // after free tier
	FreeTierFaces     int64   `json:"freeTierFaces"` // faces covered free
	FaceStorageUSDMo  float64 `json:"faceStorageUSDMonth"`
	FacesAreEstimated bool    `json:"facesAreEstimated"`

	// Ollama is local — always $0 cloud cost. Kept explicit for completeness.
	OllamaUSD float64 `json:"ollamaUSD"`

	// Totals. OneTimeUSD is the up-front indexing cost. MonthlyUSD is the
	// recurring cost using the FIRST hot tier (S3 Standard) plus face storage —
	// a single representative monthly figure. Per-provider monthly storage is in
	// Storage[] for comparison.
	OneTimeUSD        float64 `json:"oneTimeUSD"`
	MonthlyUSD        float64 `json:"monthlyUSD"`
	MonthlyStorageUSD float64 `json:"monthlyStorageUSD"` // representative (first hot tier)
}

// Compute returns the priced breakdown for in using the model in pricing.go.
// It is pure (no IO): the InputSource has already gathered the facts. This is
// the function the tests exercise for tier boundaries, free tier and rates.
func Compute(in Inputs, opts Options) Estimate {
	e := Estimate{
		PricingAsOf:       PricingAsOf.Format("2006-01-02"),
		Inputs:            in,
		GiBNote:           "cloud GB = 1000^3 bytes (decimal), matching vendor billing",
		ArchiveNote:       ArchiveStorageRateNote,
		FacesAreEstimated: !in.FacesAreActual,
		OllamaUSD:         0,
	}

	// --- storage (monthly) ---
	e.SizeGB = float64(in.SizeBytes) / bytesPerGB
	for _, r := range HotStorageRates {
		e.Storage = append(e.Storage, StorageLine{
			Provider:      r.Provider,
			Tier:          r.Tier,
			USDPerGBMonth: r.USDPerGBMonth,
			Note:          r.Note,
			MonthlyUSD:    e.SizeGB * r.USDPerGBMonth,
		})
	}
	if len(e.Storage) > 0 {
		e.MonthlyStorageUSD = e.Storage[0].MonthlyUSD // first hot tier = representative
	}

	// --- Rekognition one-time indexing (per image, tiered) ---
	indexImages := in.Images
	if opts.ApplyFreeTier {
		e.FreeTierImages = minInt64(indexImages, RekognitionFreeTierImagesPerMonth)
		indexImages -= e.FreeTierImages
	}
	e.IndexImagesPriced = indexImages
	e.IndexTiers, e.IndexOneTimeUSD = priceRekognitionImages(indexImages)

	// --- Rekognition ongoing face-metadata storage (monthly) ---
	faces := in.Faces
	if opts.ApplyFreeTier {
		e.FreeTierFaces = minInt64(faces, RekognitionFreeTierFacesStored)
		faces -= e.FreeTierFaces
	}
	e.FacesPriced = faces
	e.FaceStorageUSDMo = float64(faces) * RekognitionFaceStorageUSDPerFaceMonth

	// --- totals ---
	e.OneTimeUSD = e.IndexOneTimeUSD
	e.MonthlyUSD = e.MonthlyStorageUSD + e.FaceStorageUSDMo

	return e
}

// priceRekognitionImages applies the tiered per-image price to n images and
// returns the per-tier breakdown plus the total. Tiers are cumulative: the
// UpToImages field is the inclusive running ceiling, and the final tier
// (UpToImages == 0) is unbounded.
func priceRekognitionImages(n int64) ([]RekognitionTierCharge, float64) {
	if n <= 0 {
		return nil, 0
	}
	var (
		charges   []RekognitionTierCharge
		total     float64
		priorCeil int64 // images already consumed by lower tiers
	)
	for _, t := range RekognitionImageTiers {
		// Width of this tier: ceiling minus what lower tiers already covered.
		var width int64
		if t.UpToImages == 0 {
			width = n - priorCeil // unbounded final tier soaks up the rest
		} else {
			width = t.UpToImages - priorCeil
		}
		if width <= 0 {
			continue
		}
		remaining := n - priorCeil
		if remaining <= 0 {
			break
		}
		inThisTier := remaining
		if inThisTier > width {
			inThisTier = width
		}
		sub := float64(inThisTier) * t.USDPerImage
		charges = append(charges, RekognitionTierCharge{
			UpToImages:  t.UpToImages,
			USDPerImage: t.USDPerImage,
			Images:      inThisTier,
			USD:         sub,
		})
		total += sub
		priorCeil += inThisTier
		if priorCeil >= n {
			break
		}
	}
	return charges, total
}

// EstimateFacesFromImages returns the estimated stored-face count for a library
// of n images at avg faces/image. avg <= 0 uses DefaultAvgFacesPerImage. The
// result is rounded down to a whole face count.
func EstimateFacesFromImages(images int64, avg float64) int64 {
	if avg <= 0 {
		avg = DefaultAvgFacesPerImage
	}
	if images <= 0 {
		return 0
	}
	return int64(float64(images) * avg)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// FormatUSD renders a USD dollar TOTAL with cents, e.g. "$1.23" or "$0.0040"
// for sub-cent figures so tiny monthly costs are not shown as "$0.00".
func FormatUSD(v float64) string {
	if v != 0 && v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// FormatRate renders a per-unit RATE (per GB, per image, per face) without
// losing the small fractional digits that matter for rates, e.g. "$0.023",
// "$0.0010", "$0.00001". It trims trailing zeros beyond the minimum 2 decimals
// while keeping at least two so dollar-and-cents reads naturally.
func FormatRate(v float64) string {
	// Use enough precision to show the rate, then trim trailing zeros but keep
	// at least two decimals.
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if i := strings.IndexByte(s, '.'); i < 0 {
		s += ".00"
	} else if decimals := len(s) - i - 1; decimals < 2 {
		s += strings.Repeat("0", 2-decimals)
	}
	return "$" + s
}
