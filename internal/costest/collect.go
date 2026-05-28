package costest

import (
	"context"
	"fmt"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// This file gathers the live library facts (image count, total bytes, stored
// faces) from the catalog and people repositories, behind small interfaces so
// the costest math stays DB- and network-free in tests. The CLI passes the real
// *catalog.Repo and *people.Repo (which satisfy these); a fake satisfies them in
// tests.

// AssetLister is the slice of the catalog repo costest reads: every asset in a
// library, with its files. *catalog.Repo satisfies it via ListAllAssets.
type AssetLister interface {
	ListAllAssets(ctx context.Context, alias string) ([]catalog.Asset, error)
}

// PersonLister + FaceCounter are the slice of the people repo costest reads to
// total the stored face vectors. The face-index pipeline assigns EVERY detected
// face to a person cluster (a known person or a fresh "unknown" cluster), so the
// total stored-face count is the sum of per-person face counts over every
// cluster — there are no person-less face rows to miss. *people.Repo satisfies
// both (ListPersons, CountFacesForPerson).
type PersonLister interface {
	ListPersons(ctx context.Context) ([]catalog.Person, error)
}

// FaceCounter counts faces assigned to a person cluster id.
type FaceCounter interface {
	CountFacesForPerson(ctx context.Context, personID string) (int64, error)
}

// RepoSource is the production InputSource: it counts IMAGE assets and total
// library bytes from the catalog, and reads the actual stored face count from
// the people repo when faces have been indexed. When no faces are indexed it
// falls back to estimating faces = images * AvgFacesPerImage and marks the
// result as an estimate.
type RepoSource struct {
	// Aliases are the library aliases to walk (cfg.Libraries() aliases).
	Aliases []string
	// Assets lists assets per library (required).
	Assets AssetLister
	// People + Faces read the stored face count. Either may be nil (e.g. no AWS
	// configured), in which case faces are estimated from the image count.
	People PersonLister
	Faces  FaceCounter
	// AvgFacesPerImage is used only for the estimate fallback. <= 0 uses the
	// package default.
	AvgFacesPerImage float64
}

// Collect walks the libraries to total image assets + bytes, then resolves the
// face count (actual if indexed, else estimated). It satisfies InputSource.
func (s RepoSource) Collect(ctx context.Context) (Inputs, error) {
	var in Inputs
	in.ImagesAreActual = true
	in.SizeIsActual = true

	for _, alias := range s.Aliases {
		assets, err := s.Assets.ListAllAssets(ctx, alias)
		if err != nil {
			return Inputs{}, fmt.Errorf("costest: list assets for %q: %w", alias, err)
		}
		for _, a := range assets {
			if a.Kind == "image" {
				in.Images++
			}
			// Total bytes spans every file (images + videos + sidecars): the whole
			// tree is what gets synced to the cloud bucket.
			for _, f := range a.Files {
				in.SizeBytes += f.Size
			}
		}
	}

	faces, actual, err := s.collectFaces(ctx)
	if err != nil {
		return Inputs{}, err
	}
	if actual {
		in.Faces = faces
		in.FacesAreActual = true
	} else {
		in.Faces = EstimateFacesFromImages(in.Images, s.AvgFacesPerImage)
		in.FacesAreActual = false
	}
	return in, nil
}

// collectFaces totals the stored face vectors across all person clusters. It
// returns actual=false when the people repo is not wired or no faces are stored
// yet, so the caller estimates instead.
func (s RepoSource) collectFaces(ctx context.Context) (count int64, actual bool, err error) {
	if s.People == nil || s.Faces == nil {
		return 0, false, nil
	}
	persons, err := s.People.ListPersons(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("costest: list persons: %w", err)
	}
	var total int64
	for _, p := range persons {
		n, cerr := s.Faces.CountFacesForPerson(ctx, p.ID)
		if cerr != nil {
			return 0, false, fmt.Errorf("costest: count faces for %q: %w", p.ID, cerr)
		}
		total += n
	}
	if total == 0 {
		// No faces indexed yet -> estimate from images instead of pricing zero.
		return 0, false, nil
	}
	return total, true, nil
}
