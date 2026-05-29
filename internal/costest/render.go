package costest

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// label returns "(actual)" or "(estimate)" for a quantity.
func label(actual bool) string {
	if actual {
		return "(actual)"
	}
	return "(estimate)"
}

// RenderText writes the human-readable cost breakdown to w. The layout groups
// the three cloud touchpoints (storage, Rekognition indexing, Rekognition face
// storage) plus the free Ollama line, then a concise total. Estimated inputs
// are explicitly marked so the reader never mistakes a planning figure for a
// measured one.
func RenderText(w io.Writer, e Estimate) error {
	var b strings.Builder

	fmt.Fprintf(&b, "Cloud cost estimate for fs-image-manager library\n")
	fmt.Fprintf(&b, "Pricing as of ~%s (approximate list prices; verify before relying on absolutes)\n\n", e.PricingAsOf)

	// Inputs.
	fmt.Fprintf(&b, "Library inputs\n")
	fmt.Fprintf(&b, "  Image assets : %d %s\n", e.Inputs.Images, label(e.Inputs.ImagesAreActual))
	fmt.Fprintf(&b, "  Library size : %.2f GB %s (%d bytes; %s)\n",
		e.SizeGB, label(e.Inputs.SizeIsActual), e.Inputs.SizeBytes, e.GiBNote)
	fmt.Fprintf(&b, "  Stored faces : %d %s\n\n", e.Inputs.Faces, label(e.Inputs.FacesAreActual))

	// Storage (monthly).
	fmt.Fprintf(&b, "1) Cloud object storage  [ONGOING / monthly]\n")
	for _, s := range e.Storage {
		fmt.Fprintf(&b, "   %-8s %-9s  %s/GB-mo  ->  %s / month   (%s)\n",
			s.Provider, s.Tier, FormatRate(s.USDPerGBMonth), FormatUSD(s.MonthlyUSD), s.Note)
	}
	fmt.Fprintf(&b, "   note: %s\n\n", e.ArchiveNote)

	// Rekognition indexing (one-time).
	fmt.Fprintf(&b, "2) AWS Rekognition face indexing (IndexFaces, per IMAGE)  [ONE-TIME]\n")
	if e.FreeTierImages > 0 {
		fmt.Fprintf(&b, "   free tier covers %d images (first 12 months); pricing %d images\n",
			e.FreeTierImages, e.IndexImagesPriced)
	} else {
		fmt.Fprintf(&b, "   pricing %d images (free tier not applied)\n", e.IndexImagesPriced)
	}
	if len(e.IndexTiers) == 0 {
		fmt.Fprintf(&b, "   (no images to index)\n")
	}
	for _, t := range e.IndexTiers {
		ceil := "unbounded"
		if t.UpToImages != 0 {
			ceil = fmt.Sprintf("<=%d", t.UpToImages)
		}
		fmt.Fprintf(&b, "   tier %-12s  %d img x %s/img  =  %s\n",
			ceil, t.Images, FormatRate(t.USDPerImage), FormatUSD(t.USD))
	}
	fmt.Fprintf(&b, "   one-time indexing total: %s\n", FormatUSD(e.IndexOneTimeUSD))
	fmt.Fprintf(&b, "   note: per-image price; downscaling DSLR JPEGs first does not change it. "+
		"Free tier = %d images/mo for 12 months (legacy accounts may see %d/mo).\n\n",
		RekognitionFreeTierImagesPerMonth, RekognitionFreeTierImagesLegacy)

	// Rekognition face storage (monthly).
	fmt.Fprintf(&b, "3) AWS Rekognition face-metadata storage  [ONGOING / monthly]\n")
	facesLabel := "estimated"
	if !e.FacesAreEstimated {
		facesLabel = "actual"
	}
	if e.FreeTierFaces > 0 {
		fmt.Fprintf(&b, "   free tier covers %d faces; pricing %d (%s) faces\n",
			e.FreeTierFaces, e.FacesPriced, facesLabel)
	} else {
		fmt.Fprintf(&b, "   pricing %d (%s) faces\n", e.FacesPriced, facesLabel)
	}
	fmt.Fprintf(&b, "   %d faces x %s/face-mo  =  %s / month\n",
		e.FacesPriced, FormatRate(RekognitionFaceStorageUSDPerFaceMonth), FormatUSD(e.FaceStorageUSDMo))
	fmt.Fprintf(&b, "   (= %s per 1,000 faces / month)\n\n", FormatRate(1000*RekognitionFaceStorageUSDPerFaceMonth))

	// Ollama.
	fmt.Fprintf(&b, "4) Ollama enrichment (captions/tags/embeddings)  [LOCAL]\n")
	fmt.Fprintf(&b, "   runs locally -> %s cloud cost\n\n", FormatUSD(e.OllamaUSD))

	// Totals.
	fmt.Fprintf(&b, "TOTAL\n")
	fmt.Fprintf(&b, "  One-time (Rekognition indexing) : %s\n", FormatUSD(e.OneTimeUSD))
	fmt.Fprintf(&b, "  Est. monthly (storage + faces)  : %s\n", FormatUSD(e.MonthlyUSD))
	fmt.Fprintf(&b, "    = %s storage (%s %s) + %s face storage\n",
		FormatUSD(e.MonthlyStorageUSD), storageProvider(e), storageTier(e), FormatUSD(e.FaceStorageUSDMo))

	_, err := io.WriteString(w, b.String())
	return err
}

// storageProvider / storageTier name the representative (first hot) tier used
// in the monthly total, defaulting safely if no storage line exists.
func storageProvider(e Estimate) string {
	if len(e.Storage) > 0 {
		return e.Storage[0].Provider
	}
	return "n/a"
}

func storageTier(e Estimate) string {
	if len(e.Storage) > 0 {
		return e.Storage[0].Tier
	}
	return ""
}

// RenderJSON writes the estimate as indented JSON. The shape mirrors the
// Estimate struct's JSON tags, so the -json output is a direct, stable
// serialization of the same numbers the text view shows.
func RenderJSON(w io.Writer, e Estimate) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}
