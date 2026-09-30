-- A mirror row represents the current FMS projection item, not a revision
-- history. Keep the newest fetched row for each FMS archive/asset identity and remove its
-- obsolete child records before enforcing the latest-only identity.

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY source_system, archive_record_id, asset_id
               ORDER BY last_synced_at DESC, created_at DESC, id DESC
           ) AS row_number
    FROM fms_projection_mirrors
), stale AS (
    SELECT id FROM ranked WHERE row_number > 1
)
DELETE FROM fms_mirror_artifact_records
WHERE mirror_id IN (SELECT id FROM stale);

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY source_system, archive_record_id, asset_id
               ORDER BY last_synced_at DESC, created_at DESC, id DESC
           ) AS row_number
    FROM fms_projection_mirrors
), stale AS (
    SELECT id FROM ranked WHERE row_number > 1
)
DELETE FROM fms_mirror_retrieval_units
WHERE mirror_id IN (SELECT id FROM stale);

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY source_system, archive_record_id, asset_id
               ORDER BY last_synced_at DESC, created_at DESC, id DESC
           ) AS row_number
    FROM fms_projection_mirrors
)
DELETE FROM fms_projection_mirrors
WHERE id IN (SELECT id FROM ranked WHERE row_number > 1);

ALTER TABLE fms_projection_mirrors
    DROP CONSTRAINT IF EXISTS uq_fms_projection_mirrors_source_revision;

ALTER TABLE fms_projection_mirrors
    ADD CONSTRAINT uq_fms_projection_mirrors_source_asset UNIQUE (source_system, archive_record_id, asset_id);
