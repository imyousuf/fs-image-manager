package costest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

const eps = 1e-9

func approx(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s: got %.10f, want %.10f", what, got, want)
	}
}

// --- Rekognition per-image tier math ----------------------------------------

func TestPriceRekognitionImages_Zero(t *testing.T) {
	tiers, total := priceRekognitionImages(0)
	if tiers != nil || total != 0 {
		t.Fatalf("zero images should cost nothing, got tiers=%v total=%v", tiers, total)
	}
	if _, total := priceRekognitionImages(-5); total != 0 {
		t.Fatalf("negative images should cost nothing, got %v", total)
	}
}

func TestPriceRekognitionImages_FirstTier(t *testing.T) {
	// 1,000 images all in the $0.0010 tier = $1.00.
	tiers, total := priceRekognitionImages(1000)
	approx(t, total, 1.00, "1000 images")
	if len(tiers) != 1 || tiers[0].Images != 1000 {
		t.Fatalf("expected single tier of 1000 images, got %+v", tiers)
	}
}

func TestPriceRekognitionImages_ExactFirstTierBoundary(t *testing.T) {
	// Exactly 1,000,000 images, all at $0.0010 => $1,000.
	_, total := priceRekognitionImages(1_000_000)
	approx(t, total, 1000.0, "1,000,000 images")
}

func TestPriceRekognitionImages_CrossSecondTierBoundary(t *testing.T) {
	// 1,000,001 images: first 1,000,000 @ $0.0010 = $1000, the 1 extra @ $0.0008.
	tiers, total := priceRekognitionImages(1_000_001)
	approx(t, total, 1000.0+0.0008, "1,000,001 images")
	if len(tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d: %+v", len(tiers), tiers)
	}
	if tiers[0].Images != 1_000_000 || tiers[1].Images != 1 {
		t.Fatalf("tier split wrong: %+v", tiers)
	}
}

func TestPriceRekognitionImages_AllTiers(t *testing.T) {
	// 40,000,000 images spans all four tiers:
	//   1,000,000 @ 0.0010 = 1000
	//   4,000,000 @ 0.0008 = 3200
	//  30,000,000 @ 0.0006 = 18000
	//   5,000,000 @ 0.0004 = 2000
	//                 total = 24200
	tiers, total := priceRekognitionImages(40_000_000)
	approx(t, total, 24200.0, "40M images")
	if len(tiers) != 4 {
		t.Fatalf("expected 4 tiers, got %d", len(tiers))
	}
	wantImages := []int64{1_000_000, 4_000_000, 30_000_000, 5_000_000}
	for i, w := range wantImages {
		if tiers[i].Images != w {
			t.Errorf("tier %d images: got %d want %d", i, tiers[i].Images, w)
		}
	}
}

// --- storage rates ----------------------------------------------------------

func TestStorageRates(t *testing.T) {
	// 100 GB priced at each hot tier.
	in := Inputs{SizeBytes: 100 * 1_000_000_000, SizeIsActual: true}
	e := Compute(in, Options{})
	approx(t, e.SizeGB, 100.0, "size GB")
	if len(e.Storage) != 2 {
		t.Fatalf("expected 2 hot storage lines, got %d", len(e.Storage))
	}
	// S3 Standard $0.023/GB-mo * 100 = $2.30 ; GCS $0.020 * 100 = $2.00.
	approx(t, e.Storage[0].MonthlyUSD, 2.30, "S3 100GB")
	approx(t, e.Storage[1].MonthlyUSD, 2.00, "GCS 100GB")
	approx(t, e.MonthlyStorageUSD, 2.30, "representative monthly storage")
}

// --- face metadata storage --------------------------------------------------

func TestFaceStorageMonthly(t *testing.T) {
	// 10,000 faces @ $0.00001/face-mo = $0.10/month.
	in := Inputs{Faces: 10_000, FacesAreActual: true}
	e := Compute(in, Options{})
	approx(t, e.FaceStorageUSDMo, 0.10, "10k faces/mo")
	if e.FacesAreEstimated {
		t.Error("actual faces should not be flagged estimated")
	}
}

// --- free tier --------------------------------------------------------------

func TestFreeTierImagesAndFaces(t *testing.T) {
	in := Inputs{Images: 1500, Faces: 1500, ImagesAreActual: true, FacesAreActual: true}

	// Without free tier: all 1500 images & faces priced.
	noFT := Compute(in, Options{})
	approx(t, noFT.IndexOneTimeUSD, 1500*0.0010, "1500 images no free tier")
	approx(t, noFT.FaceStorageUSDMo, 1500*0.00001, "1500 faces no free tier")
	if noFT.FreeTierImages != 0 || noFT.FreeTierFaces != 0 {
		t.Error("free tier should be zero when not applied")
	}

	// With free tier: first 1000 images and 1000 faces are free.
	withFT := Compute(in, Options{ApplyFreeTier: true})
	if withFT.FreeTierImages != 1000 || withFT.IndexImagesPriced != 500 {
		t.Fatalf("image free tier wrong: free=%d priced=%d", withFT.FreeTierImages, withFT.IndexImagesPriced)
	}
	if withFT.FreeTierFaces != 1000 || withFT.FacesPriced != 500 {
		t.Fatalf("face free tier wrong: free=%d priced=%d", withFT.FreeTierFaces, withFT.FacesPriced)
	}
	approx(t, withFT.IndexOneTimeUSD, 500*0.0010, "500 priced images")
	approx(t, withFT.FaceStorageUSDMo, 500*0.00001, "500 priced faces")
}

func TestFreeTierCoversEverything(t *testing.T) {
	// Fewer images/faces than the free allowance => nothing priced.
	in := Inputs{Images: 300, Faces: 200, ImagesAreActual: true, FacesAreActual: true}
	e := Compute(in, Options{ApplyFreeTier: true})
	if e.IndexImagesPriced != 0 || e.IndexOneTimeUSD != 0 {
		t.Errorf("all images should be free: priced=%d cost=%v", e.IndexImagesPriced, e.IndexOneTimeUSD)
	}
	if e.FacesPriced != 0 || e.FaceStorageUSDMo != 0 {
		t.Errorf("all faces should be free: priced=%d cost=%v", e.FacesPriced, e.FaceStorageUSDMo)
	}
	if e.FreeTierImages != 300 || e.FreeTierFaces != 200 {
		t.Errorf("free tier should cap at actual counts: %d/%d", e.FreeTierImages, e.FreeTierFaces)
	}
}

// --- totals + ollama --------------------------------------------------------

func TestTotalsAndOllama(t *testing.T) {
	in := Inputs{
		Images:    5000,
		SizeBytes: 50 * 1_000_000_000, // 50 GB
		Faces:     5000,
	}
	e := Compute(in, Options{})
	approx(t, e.OllamaUSD, 0, "ollama is free")
	approx(t, e.OneTimeUSD, 5000*0.0010, "one-time = indexing")
	wantStorage := 50 * 0.023
	wantFaces := 5000 * 0.00001
	approx(t, e.MonthlyStorageUSD, wantStorage, "monthly storage")
	approx(t, e.MonthlyUSD, wantStorage+wantFaces, "monthly total = storage + faces")
}

// --- faces-from-images estimate ---------------------------------------------

func TestEstimateFacesFromImages(t *testing.T) {
	if got := EstimateFacesFromImages(1000, 1.5); got != 1500 {
		t.Errorf("1000 images * 1.5 = 1500, got %d", got)
	}
	if got := EstimateFacesFromImages(1000, 0); got != 1500 {
		t.Errorf("avg<=0 should use default 1.5 => 1500, got %d", got)
	}
	if got := EstimateFacesFromImages(0, 2); got != 0 {
		t.Errorf("zero images => zero faces, got %d", got)
	}
}

// --- text rendering ---------------------------------------------------------

func TestRenderText_MarksEstimateVsActual(t *testing.T) {
	in := Inputs{
		Images:          1000,
		SizeBytes:       10 * 1_000_000_000,
		Faces:           1500,
		ImagesAreActual: true,
		SizeIsActual:    true,
		FacesAreActual:  false, // estimated
	}
	e := Compute(in, Options{})
	var buf bytes.Buffer
	if err := RenderText(&buf, e); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Cloud cost estimate",
		"Image assets : 1000 (actual)",
		"Stored faces : 1500 (estimate)",
		"AWS S3",
		"GCS",
		"IndexFaces",
		"face-metadata storage",
		"Ollama",
		"$0.00", // ollama free
		"TOTAL",
		"One-time",
		"Est. monthly",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatRate(t *testing.T) {
	cases := map[float64]string{
		0.023:   "$0.023",
		0.020:   "$0.02",
		0.0010:  "$0.001",
		0.00001: "$0.00001",
		1.0:     "$1.00",
		0:       "$0.00",
	}
	for in, want := range cases {
		if got := FormatRate(in); got != want {
			t.Errorf("FormatRate(%v): got %q want %q", in, got, want)
		}
	}
}

func TestFormatUSD_SubCent(t *testing.T) {
	if got := FormatUSD(0.004); got != "$0.0040" {
		t.Errorf("sub-cent format: got %q", got)
	}
	if got := FormatUSD(2.5); got != "$2.50" {
		t.Errorf("normal format: got %q", got)
	}
	if got := FormatUSD(0); got != "$0.00" {
		t.Errorf("zero format: got %q", got)
	}
}

// --- JSON rendering ---------------------------------------------------------

func TestRenderJSON_Shape(t *testing.T) {
	in := Inputs{
		Images:          2000,
		SizeBytes:       20 * 1_000_000_000,
		Faces:           3000,
		ImagesAreActual: true,
		SizeIsActual:    true,
		FacesAreActual:  true,
	}
	e := Compute(in, Options{})
	var buf bytes.Buffer
	if err := RenderJSON(&buf, e); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{
		"pricingAsOf", "inputs", "sizeGB", "storage", "archiveNote",
		"indexOneTimeUSD", "faceStorageUSDMonth", "ollamaUSD",
		"oneTimeUSD", "monthlyUSD", "monthlyStorageUSD",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON missing key %q", key)
		}
	}
	// Nested inputs shape.
	inputs, ok := got["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("inputs not an object: %T", got["inputs"])
	}
	if inputs["images"].(float64) != 2000 {
		t.Errorf("inputs.images: got %v", inputs["images"])
	}
	// Numbers round-trip.
	if got["oneTimeUSD"].(float64) != 2000*0.0010 {
		t.Errorf("oneTimeUSD: got %v", got["oneTimeUSD"])
	}
}

// --- RepoSource (injectable, DB-free) ---------------------------------------

type fakeAssetLister struct {
	byAlias map[string][]catalog.Asset
	err     error
}

func (f fakeAssetLister) ListAllAssets(_ context.Context, alias string) ([]catalog.Asset, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byAlias[alias], nil
}

type fakePeople struct {
	persons []catalog.Person
	counts  map[string]int64
	err     error
}

func (f fakePeople) ListPersons(context.Context) ([]catalog.Person, error) {
	return f.persons, f.err
}

func (f fakePeople) CountFacesForPerson(_ context.Context, personID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[personID], nil
}

func img(alias string, files ...int64) catalog.Asset {
	a := catalog.Asset{Alias: alias, Kind: "image"}
	for _, s := range files {
		a.Files = append(a.Files, catalog.File{Size: s})
	}
	return a
}

func vid(alias string, files ...int64) catalog.Asset {
	a := catalog.Asset{Alias: alias, Kind: "video"}
	for _, s := range files {
		a.Files = append(a.Files, catalog.File{Size: s})
	}
	return a
}

func TestRepoSource_ActualFaces(t *testing.T) {
	assets := fakeAssetLister{byAlias: map[string][]catalog.Asset{
		"pics": {
			img("pics", 100, 50), // image, 150 bytes (RAW+JPG)
			img("pics", 200),     // image, 200 bytes
			vid("pics", 1000),    // video, 1000 bytes, NOT an image
		},
	}}
	ppl := fakePeople{
		persons: []catalog.Person{{ID: "p1"}, {ID: "p2"}},
		counts:  map[string]int64{"p1": 3, "p2": 4},
	}
	src := RepoSource{Aliases: []string{"pics"}, Assets: assets, People: ppl, Faces: ppl}

	in, err := src.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if in.Images != 2 {
		t.Errorf("images: got %d want 2 (videos excluded)", in.Images)
	}
	if in.SizeBytes != 1350 {
		t.Errorf("size: got %d want 1350 (all files incl. video)", in.SizeBytes)
	}
	if in.Faces != 7 || !in.FacesAreActual {
		t.Errorf("faces: got %d actual=%v, want 7 actual", in.Faces, in.FacesAreActual)
	}
	if !in.ImagesAreActual || !in.SizeIsActual {
		t.Error("measured images/size should be actual")
	}
}

func TestRepoSource_EstimatesFacesWhenNoneIndexed(t *testing.T) {
	assets := fakeAssetLister{byAlias: map[string][]catalog.Asset{
		"pics": {img("pics", 10), img("pics", 10), img("pics", 10), img("pics", 10)},
	}}
	// People wired but no faces stored => estimate.
	ppl := fakePeople{persons: []catalog.Person{{ID: "p1"}}, counts: map[string]int64{"p1": 0}}
	src := RepoSource{Aliases: []string{"pics"}, Assets: assets, People: ppl, Faces: ppl, AvgFacesPerImage: 2}

	in, err := src.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if in.FacesAreActual {
		t.Error("faces should be estimated when none indexed")
	}
	if in.Faces != 8 { // 4 images * 2.0
		t.Errorf("estimated faces: got %d want 8", in.Faces)
	}
}

func TestRepoSource_NilPeopleEstimates(t *testing.T) {
	assets := fakeAssetLister{byAlias: map[string][]catalog.Asset{
		"pics": {img("pics", 10), img("pics", 10)},
	}}
	src := RepoSource{Aliases: []string{"pics"}, Assets: assets} // no People/Faces
	in, err := src.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if in.FacesAreActual {
		t.Error("nil people repo => estimate")
	}
	if in.Faces != 3 { // 2 images * default 1.5
		t.Errorf("estimated faces: got %d want 3", in.Faces)
	}
}

func TestRepoSource_PropagatesErrors(t *testing.T) {
	wantErr := errors.New("boom")
	src := RepoSource{Aliases: []string{"pics"}, Assets: fakeAssetLister{err: wantErr}}
	if _, err := src.Collect(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped asset error, got %v", err)
	}
}
