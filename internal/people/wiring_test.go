package people_test

import (
	"github.com/imyousuf/fs-image-manager/internal/people"
	"github.com/imyousuf/fs-image-manager/internal/search"
)

// Compile-time contract checks for the serve wiring:
//   - *people.Repo must satisfy the pipeline's PersonStore and the handlers'
//     read/write needs (PutFace/AssignFace/... and ListPersons/AssetCounts/...).
//   - *search.Index must satisfy people's SearchPersonSink, so the face-index
//     pipeline can push person names into the FTS "persons" column.
//
// Kept in the test binary so production people does not import search. If search
// changes UpsertPersons, this fails here rather than only at the main.go call site.
var (
	_ people.PersonStore      = (*people.Repo)(nil)
	_ people.SearchPersonSink = (*search.Index)(nil)
)
