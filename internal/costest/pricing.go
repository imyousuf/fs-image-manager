// Package costest estimates the recurring and one-time CLOUD costs of keeping a
// media library managed by fs-image-manager. It models the three real cloud
// touchpoints of the app:
//
//   - Cloud OBJECT STORAGE of the media (ongoing, monthly). The user syncs the
//     library off to a cloud bucket (e.g. Google Cloud Storage), so the bytes on
//     disk translate into a real monthly storage bill. We price the two common
//     "hot" tiers (AWS S3 Standard and GCS Standard) and note the cold/archive
//     tiers for backup-only data.
//   - AWS Rekognition PROCESSING for people recognition. IndexFaces bills
//     per-IMAGE (NOT per byte, and NOT per face): indexing N images is a
//     one-time cost. DSLR JPEGs are downscaled before upload, but that does not
//     change the per-image price — it only reduces request payload.
//   - AWS Rekognition face-metadata STORAGE (ongoing, monthly): each stored face
//     vector costs a tiny monthly fee.
//
// Ollama enrichment runs locally and is FREE in cloud terms; we surface it as a
// $0 line so the report is complete.
//
// The pricing model lives in this one file and is deliberately data-driven and
// heavily commented so it is trivial to update when AWS/GCP change their rates.
// Update the constants below and bump PricingAsOf.
//
// PRICING SOURCES (rates confirmed approximately as of PricingAsOf; verify
// before relying on the absolute numbers — cloud list prices drift):
//
//   - AWS Rekognition pricing (Image Group 1 APIs incl. IndexFaces; face
//     metadata storage; 12-month free tier):
//     https://aws.amazon.com/rekognition/pricing/
//   - AWS S3 pricing (S3 Standard first-50TB tier; Glacier archive tiers):
//     https://aws.amazon.com/s3/pricing/
//   - Google Cloud Storage pricing (Standard regional; Archive):
//     https://cloud.google.com/storage/pricing
package costest

import "time"

// PricingAsOf is the approximate date the rates below were last confirmed
// against the vendor pricing pages. It is printed in the report so a reader
// knows how stale the numbers might be. Update it whenever you touch a rate.
var PricingAsOf = time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC)

// bytesPerGB is the GB used by cloud storage billing. Cloud vendors bill in
// "GB" defined as 1000^3 bytes (decimal GB), NOT 1024^3 (GiB). Using the
// decimal definition keeps our estimate aligned with the vendor invoice.
const bytesPerGB = 1_000_000_000.0

// --- Cloud object storage (monthly, per GB) ---------------------------------

// StorageRate is one cloud object-storage tier's monthly price per GB.
type StorageRate struct {
	// Provider/Tier name as it appears on the vendor pricing page.
	Provider string
	Tier     string
	// USDPerGBMonth is the list price per GB stored per month, in USD.
	USDPerGBMonth float64
	// Note is a short human hint (e.g. region/assumption) shown in the report.
	Note string
}

// HotStorageRates are the "standard"/hot tiers we estimate by default: these
// support frequent reads, matching a browsable photo library mirrored to the
// cloud. Rates are us-east-1 / US-region list prices.
//
//   - AWS S3 Standard: $0.023 per GB-month for the first 50 TB (us-east-1).
//     Source: https://aws.amazon.com/s3/pricing/
//   - GCS Standard: $0.020 per GB-month, regional US (e.g. us-east1).
//     Source: https://cloud.google.com/storage/pricing
var HotStorageRates = []StorageRate{
	{Provider: "AWS S3", Tier: "Standard", USDPerGBMonth: 0.023, Note: "us-east-1, first 50 TB"},
	{Provider: "GCS", Tier: "Standard", USDPerGBMonth: 0.020, Note: "regional US (e.g. us-east1)"},
}

// ArchiveStorageRateNote is a single-line summary of the cold/archive tiers,
// for users who keep the cloud copy purely as a cold backup. We do not compute
// these by default (the app's sync is a browsable mirror, so hot pricing is the
// honest default), but we surface the range so the cheaper option is visible.
//
//   - AWS S3 Glacier Deep Archive: ~$0.00099 per GB-month.
//   - AWS S3 Glacier Flexible Retrieval: ~$0.0036 per GB-month.
//     Source: https://aws.amazon.com/s3/pricing/
//   - GCS Archive: ~$0.0012 per GB-month (regional) / ~$0.0024 (multi-region).
//     Source: https://cloud.google.com/storage/pricing
//
// Archive tiers add per-GB RETRIEVAL fees and minimum storage durations, which
// we do not model — they are noted for awareness only.
const ArchiveStorageRateNote = "Cold-backup archive tiers (not browsable, retrieval fees apply) run far cheaper: " +
	"S3 Glacier Deep Archive ~$0.001/GB-mo, S3 Glacier Flexible ~$0.0036/GB-mo, GCS Archive ~$0.0012/GB-mo."

// --- AWS Rekognition: per-IMAGE processing (IndexFaces, Group 1 APIs) -------

// RekognitionTier is one volume tier of the Group 1 Image API per-image price.
// Tiers are evaluated cumulatively per calendar month (e.g. the first
// FirstNImages images cost USDPerImage each, then the next tier kicks in).
type RekognitionTier struct {
	// UpToImages is the inclusive upper bound (images processed that month) for
	// this tier. The final tier uses a sentinel of 0 meaning "unbounded".
	UpToImages int64
	// USDPerImage is the per-image price within this tier, in USD.
	USDPerImage float64
}

// RekognitionImageTiers is the tiered per-image price for the Rekognition Image
// Group 1 APIs (which include IndexFaces), us-east-1. Indexing is a ONE-TIME
// cost: each library image is indexed once. Prices are per IMAGE regardless of
// resolution or byte size (downscaling DSLR JPEGs first does not change them).
//
// Source: https://aws.amazon.com/rekognition/pricing/ (US East, N. Virginia):
//
//	First       1,000,000 images / mo : $0.0010 per image  ($1.00 / 1,000)
//	Next        4,000,000 images / mo : $0.0008 per image  ($0.80 / 1,000)
//	Next       30,000,000 images / mo : $0.0006 per image  ($0.60 / 1,000)
//	Over       35,000,000 images / mo : $0.0004 per image  ($0.40 / 1,000)
var RekognitionImageTiers = []RekognitionTier{
	{UpToImages: 1_000_000, USDPerImage: 0.0010},
	{UpToImages: 5_000_000, USDPerImage: 0.0008},  // cumulative 1M + next 4M
	{UpToImages: 35_000_000, USDPerImage: 0.0006}, // cumulative 5M + next 30M
	{UpToImages: 0, USDPerImage: 0.0004},          // 0 => unbounded final tier
}

// RekognitionFreeTierImagesPerMonth is the number of Group 1 images analyzable
// per month at no charge during the first 12 months after account creation.
// Source: https://aws.amazon.com/rekognition/pricing/
const RekognitionFreeTierImagesPerMonth int64 = 1000

// RekognitionFreeTierFacesStored is the number of stored face-vector objects
// per month that are free during the same 12-month period.
// Source: https://aws.amazon.com/rekognition/pricing/
const RekognitionFreeTierFacesStored int64 = 1000

// Historically AWS advertised a more generous "5,000 images/month" analysis
// free tier; the current page lists 1,000 images/mo for Group 1. We use the
// current page value (1,000) as the conservative default and note the prior
// figure in the report so the estimate is not surprised by an old account.
const RekognitionFreeTierImagesLegacy int64 = 5000

// --- AWS Rekognition: face-metadata STORAGE (monthly) -----------------------

// RekognitionFaceStorageUSDPerFaceMonth is the ongoing monthly price to store
// one face-vector object (the metadata IndexFaces produces). At $0.00001/face
// that is $0.01 per 1,000 faces per month.
// Source: https://aws.amazon.com/rekognition/pricing/
const RekognitionFaceStorageUSDPerFaceMonth = 0.00001
