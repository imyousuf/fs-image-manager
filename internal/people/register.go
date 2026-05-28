package people

import (
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// This file is the turnkey wiring the serve and worker assemblies call so the
// platform-owned main.go never has to know the shape of the face-index pipeline.
// The face-index job is split: the worker recognizes faces (AWS/GPU), the serve
// host assigns them to clusters (DB).

// RegisterResultHandler installs the face-index result-handler on the serve-side
// jobs registry. servePipeline must be built with the people repo and (usually)
// the search index, so assignment persists faces/clusters and pushes person
// names into search. Call at serve startup:
//
//	people.RegisterResultHandler(reg, servePipeline)
func RegisterResultHandler(reg *jobs.Registry, servePipeline *Pipeline) {
	reg.Register(catalog.JobKindFaceIndex, ResultHandler(servePipeline))
}

// RegisterWorkerHandler installs the face-index worker handler on the worker.
// workerPipeline only needs a Recognizer (its repo/search are unused on the
// worker, which has no DB). Call on the worker box before w.Run, only when a
// FaceRecognizer is configured:
//
//	rec, _ := people.NewRekognitionRecognizer(ctx, opts)
//	people.RegisterWorkerHandler(w, people.NewPipeline(rec, nil, nil, opts.Collection))
func RegisterWorkerHandler(w *worker.Worker, workerPipeline *Pipeline) {
	w.RegisterHandler(catalog.JobKindFaceIndex, WorkerHandler(workerPipeline))
}
