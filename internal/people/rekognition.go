package people

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rekognition"
	rektypes "github.com/aws/aws-sdk-go-v2/service/rekognition/types"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// RekognitionRecognizer is the DEFAULT catalog.FaceRecognizer (docs/TECH_SPEC.md
// section 9.2): AWS Rekognition Face Collections give managed, high-quality face
// re-identification with no GPU and little code. The trade-off -- faces of
// family/friends are uploaded to AWS -- is accepted for quality per the user.
//
// It maps the two-call contract onto Rekognition:
//
//   - DetectAndEmbed indexes the image's faces into the collection
//     (IndexFaces). "Embedding" is server-side: each detected face becomes a
//     persisted FaceId in the collection, returned as catalog.Face.ExtRef. The
//     bounding box and detection confidence come back per face.
//   - Match takes one such indexed face and searches the collection for other
//     faces of the same person (SearchFaces by FaceId). The best match above a
//     threshold yields that face's ExternalImageId -- which the pipeline sets to
//     the person id -- so faces of one person converge on one cluster.
//
// The collection name comes from [ai] rekognition_collection; the AWS config
// (profile imyousuf, region us-east-1) is built by NewRekognitionRecognizer.
type RekognitionRecognizer struct {
	api        rekognitionAPI
	collection string
	// matchThreshold is the minimum similarity (0..100) for SearchFaces to treat
	// two faces as the same person. 90 is AWS's recommended high-precision value
	// for re-identification; below it false-merges climb.
	matchThreshold float32
	// detectThreshold is the minimum face-detection confidence (0..100) IndexFaces
	// must clear to keep a face. Filters tiny/blurry background faces.
	detectThreshold float32
}

// rekognitionAPI is the slice of the Rekognition client this recognizer uses,
// declared as an interface so tests substitute a fake without a real AWS client.
// The concrete *rekognition.Client satisfies it.
type rekognitionAPI interface {
	CreateCollection(ctx context.Context, in *rekognition.CreateCollectionInput, optFns ...func(*rekognition.Options)) (*rekognition.CreateCollectionOutput, error)
	DeleteCollection(ctx context.Context, in *rekognition.DeleteCollectionInput, optFns ...func(*rekognition.Options)) (*rekognition.DeleteCollectionOutput, error)
	IndexFaces(ctx context.Context, in *rekognition.IndexFacesInput, optFns ...func(*rekognition.Options)) (*rekognition.IndexFacesOutput, error)
	SearchFaces(ctx context.Context, in *rekognition.SearchFacesInput, optFns ...func(*rekognition.Options)) (*rekognition.SearchFacesOutput, error)
	SearchFacesByImage(ctx context.Context, in *rekognition.SearchFacesByImageInput, optFns ...func(*rekognition.Options)) (*rekognition.SearchFacesByImageOutput, error)
}

// RekognitionOptions configures a RekognitionRecognizer.
type RekognitionOptions struct {
	// Profile is the AWS shared-config profile (default config falls back to it).
	Profile string
	// Region is the AWS region (e.g. "us-east-1").
	Region string
	// Collection is the Face Collection id ([ai] rekognition_collection).
	Collection string
	// MatchThreshold/DetectThreshold override the defaults (0 = use default).
	MatchThreshold  float32
	DetectThreshold float32
}

const (
	defaultMatchThreshold  = 90.0
	defaultDetectThreshold = 80.0
	// maxFacesPerSearch caps SearchFaces results; we only need the top match.
	maxFacesPerSearch = 5
)

// NewRekognitionRecognizer builds the default recognizer, resolving AWS config
// from the named shared-config profile and region. The collection must be set.
// It does not create the collection; call EnsureCollection (idempotent) once at
// startup. Returns an error if AWS config cannot be loaded or no collection is
// configured.
func NewRekognitionRecognizer(ctx context.Context, opts RekognitionOptions) (*RekognitionRecognizer, error) {
	if opts.Collection == "" {
		return nil, errors.New("people: rekognition collection is required ([ai] rekognition_collection)")
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if opts.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(opts.Region))
	}
	if opts.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(opts.Profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("people: load aws config (profile %q region %q): %w", opts.Profile, opts.Region, err)
	}
	return newRekognitionRecognizer(rekognition.NewFromConfig(cfg), opts), nil
}

// newRekognitionRecognizer is the injectable constructor (tests pass a fake api).
func newRekognitionRecognizer(api rekognitionAPI, opts RekognitionOptions) *RekognitionRecognizer {
	match := opts.MatchThreshold
	if match <= 0 {
		match = defaultMatchThreshold
	}
	detect := opts.DetectThreshold
	if detect <= 0 {
		detect = defaultDetectThreshold
	}
	return &RekognitionRecognizer{
		api:             api,
		collection:      opts.Collection,
		matchThreshold:  match,
		detectThreshold: detect,
	}
}

// EnsureCollection creates the Face Collection if it does not already exist. It
// is idempotent: an existing collection (ResourceAlreadyExistsException) is not
// an error. Call once at startup before indexing.
func (r *RekognitionRecognizer) EnsureCollection(ctx context.Context) error {
	_, err := r.api.CreateCollection(ctx, &rekognition.CreateCollectionInput{
		CollectionId: aws.String(r.collection),
	})
	if err != nil {
		var exists *rektypes.ResourceAlreadyExistsException
		if errors.As(err, &exists) {
			return nil
		}
		return fmt.Errorf("people: create collection %q: %w", r.collection, err)
	}
	return nil
}

// DeleteCollection removes the Face Collection. Used by integration tests to tear
// down a throwaway collection; not part of the normal runtime path.
func (r *RekognitionRecognizer) DeleteCollection(ctx context.Context) error {
	_, err := r.api.DeleteCollection(ctx, &rekognition.DeleteCollectionInput{
		CollectionId: aws.String(r.collection),
	})
	if err != nil {
		return fmt.Errorf("people: delete collection %q: %w", r.collection, err)
	}
	return nil
}

// DetectAndEmbed indexes the image's faces into the collection and returns one
// catalog.Face per indexed face: ExtRef is the Rekognition FaceId, BBox is the
// normalized bounding box, Confidence is the detection confidence. AssetID is
// left empty here -- the pipeline fills it (the recognizer is asset-agnostic).
//
// IndexFaces only persists faces clearing detectThreshold and that Rekognition
// deems indexable; unindexable faces (too small/turned) are dropped, which is
// the correct high-quality behaviour (we do not cluster garbage detections).
func (r *RekognitionRecognizer) DetectAndEmbed(ctx context.Context, rd io.Reader) ([]catalog.Face, error) {
	raw, err := io.ReadAll(rd)
	if err != nil {
		return nil, fmt.Errorf("people: read image: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("people: empty image stream")
	}

	// DSLR JPEGs routinely exceed Rekognition's 5 MiB / 4096 px limits, which it
	// rejects with a 400 ValidationException; downscale to fit before indexing.
	img, err := prepareForRekognition(raw)
	if err != nil {
		return nil, err
	}

	out, err := r.api.IndexFaces(ctx, &rekognition.IndexFacesInput{
		CollectionId:        aws.String(r.collection),
		Image:               &rektypes.Image{Bytes: img},
		DetectionAttributes: []rektypes.Attribute{rektypes.AttributeDefault},
		QualityFilter:       rektypes.QualityFilterAuto,
		MaxFaces:            aws.Int32(20),
	})
	if err != nil {
		return nil, fmt.Errorf("people: index faces: %w", err)
	}

	faces := make([]catalog.Face, 0, len(out.FaceRecords))
	for _, rec := range out.FaceRecords {
		if rec.Face == nil {
			continue
		}
		conf := derefF32(rec.Face.Confidence)
		if conf < r.detectThreshold {
			continue
		}
		faces = append(faces, catalog.Face{
			ExtRef:     aws.ToString(rec.Face.FaceId),
			BBox:       bboxOf(rec.Face.BoundingBox),
			Confidence: float64(conf),
		})
	}
	return faces, nil
}

// Match searches the collection for other faces of the same person as f (by its
// indexed FaceId) and returns the best match's person id. The person id is read
// from the matched face's ExternalImageId, which the pipeline stamps when it
// assigns a face to a person (see AssignExternalID). A face that matches nothing
// above the threshold returns ("", 0, nil) -- the pipeline then opens a new
// "unknown" cluster for it.
func (r *RekognitionRecognizer) Match(ctx context.Context, f catalog.Face) (string, float64, error) {
	if f.ExtRef == "" {
		return "", 0, errors.New("people: match requires an indexed face (empty ExtRef)")
	}
	out, err := r.api.SearchFaces(ctx, &rekognition.SearchFacesInput{
		CollectionId:       aws.String(r.collection),
		FaceId:             aws.String(f.ExtRef),
		FaceMatchThreshold: aws.Float32(r.matchThreshold),
		MaxFaces:           aws.Int32(maxFacesPerSearch),
	})
	if err != nil {
		return "", 0, fmt.Errorf("people: search faces: %w", err)
	}

	// FaceMatches are returned descending by similarity; take the first match
	// that carries a person id (ExternalImageId). A match without one is a face
	// indexed but not yet assigned to a person, so it does not resolve the query.
	for _, m := range out.FaceMatches {
		if m.Face == nil {
			continue
		}
		pid := aws.ToString(m.Face.ExternalImageId)
		if pid == "" {
			continue
		}
		return pid, float64(derefF32(m.Similarity)), nil
	}
	return "", 0, nil
}

// SearchSimilar returns the external face refs in the collection most similar to
// the indexed face faceExtRef (best first), excluding the query face itself. It
// is the pipeline's primitive for clustering: the pipeline resolves each hit's
// ref to one of our person clusters via the repo. An empty result means no face
// in the collection clears the match threshold.
func (r *RekognitionRecognizer) SearchSimilar(ctx context.Context, faceExtRef string) ([]Candidate, error) {
	if faceExtRef == "" {
		return nil, errors.New("people: search requires an indexed face (empty ext ref)")
	}
	out, err := r.api.SearchFaces(ctx, &rekognition.SearchFacesInput{
		CollectionId:       aws.String(r.collection),
		FaceId:             aws.String(faceExtRef),
		FaceMatchThreshold: aws.Float32(r.matchThreshold),
		MaxFaces:           aws.Int32(maxFacesPerSearch),
	})
	if err != nil {
		return nil, fmt.Errorf("people: search faces: %w", err)
	}
	cands := make([]Candidate, 0, len(out.FaceMatches))
	for _, m := range out.FaceMatches {
		if m.Face == nil {
			continue
		}
		ref := aws.ToString(m.Face.FaceId)
		if ref == "" || ref == faceExtRef {
			continue
		}
		cands = append(cands, Candidate{ExtRef: ref, Similarity: float64(derefF32(m.Similarity))})
	}
	return cands, nil
}

// Compile-time checks: the recognizer satisfies both the shared catalog port and
// the richer pipeline Recognizer seam.
var (
	_ catalog.FaceRecognizer = (*RekognitionRecognizer)(nil)
	_ Recognizer             = (*RekognitionRecognizer)(nil)
)

// derefF32 dereferences a *float32, treating nil as 0.
func derefF32(p *float32) float32 {
	if p == nil {
		return 0
	}
	return *p
}

// bboxOf converts a Rekognition BoundingBox (normalized) to catalog.Face's
// [x,y,w,h] array, treating nil fields as 0.
func bboxOf(b *rektypes.BoundingBox) [4]float64 {
	if b == nil {
		return [4]float64{}
	}
	return [4]float64{
		float64(derefF32(b.Left)),
		float64(derefF32(b.Top)),
		float64(derefF32(b.Width)),
		float64(derefF32(b.Height)),
	}
}
