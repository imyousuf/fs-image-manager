-- ai-people queries (sqlc). Domains: persons (named/unknown face clusters),
-- faces (detected faces with bbox + external vector ref), and the enrichment
-- run ledger (content-hash cache for enrich-ai / face-index). See migration
-- 0400_people.sql for the schema and docs/specs/_contracts.md section 6 for
-- conventions. KEEP THIS FILE PURE ASCII: sqlc v1.31 corrupts generated
-- constants on any non-ASCII byte.

-- name: UpsertPerson :exec
INSERT INTO persons (id, name, cover_face_id, ext_collection, created_at, updated_at)
VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))
ON CONFLICT(id) DO UPDATE SET
    name           = excluded.name,
    cover_face_id  = excluded.cover_face_id,
    ext_collection = excluded.ext_collection,
    updated_at     = datetime('now');

-- name: GetPerson :one
SELECT id, name, cover_face_id, ext_collection FROM persons WHERE id = ?;

-- name: ListPersons :many
-- All persons, named clusters first (then unknown), each ordered by name/id, so
-- the People view shows labelled people ahead of pending-unknown clusters.
SELECT id, name, cover_face_id, ext_collection FROM persons
ORDER BY (name = '') ASC, name ASC, id ASC;

-- name: RenamePerson :execrows
UPDATE persons
SET name = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: SetPersonCoverFace :exec
UPDATE persons
SET cover_face_id = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: DeletePerson :exec
DELETE FROM persons WHERE id = ?;

-- name: InsertFace :exec
INSERT INTO faces (
    id, asset_id, person_id, bbox_x, bbox_y, bbox_w, bbox_h,
    confidence, ext_ref, content_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'));

-- name: GetFace :one
SELECT id, asset_id, person_id, bbox_x, bbox_y, bbox_w, bbox_h,
       confidence, ext_ref, content_hash
FROM faces WHERE id = ?;

-- name: AssignFacePerson :exec
UPDATE faces SET person_id = ? WHERE id = ?;

-- name: ListFacesForAsset :many
SELECT id, asset_id, person_id, bbox_x, bbox_y, bbox_w, bbox_h,
       confidence, ext_ref, content_hash
FROM faces WHERE asset_id = ?
ORDER BY id ASC;

-- name: ListFacesForPerson :many
SELECT id, asset_id, person_id, bbox_x, bbox_y, bbox_w, bbox_h,
       confidence, ext_ref, content_hash
FROM faces WHERE person_id = ?
ORDER BY id ASC;

-- name: ListAssetIDsForPerson :many
-- Distinct asset ids that contain a face assigned to this person, for the
-- "all photos of X" People view, newest captures first then stable by id.
SELECT DISTINCT f.asset_id
FROM faces f
JOIN assets a ON a.id = f.asset_id
WHERE f.person_id = ?
ORDER BY a.captured_at DESC, f.asset_id ASC;

-- name: ListPersonAssetCounts :many
-- Per-person face/asset counts for the People view (how many photos each person
-- appears in). Counts distinct assets, not raw faces.
SELECT person_id, COUNT(DISTINCT asset_id) AS asset_count
FROM faces
WHERE person_id IS NOT NULL
GROUP BY person_id;

-- name: PersonForExtRef :one
-- The person a previously-indexed face (identified by its backend ext_ref, e.g.
-- a Rekognition FaceId) is assigned to. Returns the most recent assignment when
-- a ref somehow appears more than once. Used by the pipeline to resolve a
-- collection-search hit to one of our person clusters.
SELECT person_id FROM faces
WHERE ext_ref = ? AND person_id IS NOT NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: DeleteFacesForAsset :exec
DELETE FROM faces WHERE asset_id = ?;

-- name: CountFacesForPerson :one
SELECT COUNT(*) FROM faces WHERE person_id = ?;

-- name: RecordEnrichmentRun :exec
INSERT INTO enrichment_runs (asset_id, kind, content_hash, ran_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(asset_id, kind, content_hash) DO UPDATE SET
    ran_at = datetime('now');

-- name: HasEnrichmentRun :one
-- Reports whether an asset's exact bytes have already had this kind of
-- enrichment run -- the content-hash cache check that lets the enqueue path skip
-- unchanged media (returns 1 when a matching run exists, else 0).
SELECT EXISTS (
    SELECT 1 FROM enrichment_runs
    WHERE asset_id = ? AND kind = ? AND content_hash = ?
) AS present;
