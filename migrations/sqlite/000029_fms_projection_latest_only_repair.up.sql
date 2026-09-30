-- Repair SQLite databases where the first latest-only migration was already
-- marked applied before its deduplication and identity constraint took effect.

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

CREATE UNIQUE INDEX IF NOT EXISTS uq_fms_projection_mirrors_source_asset
    ON fms_projection_mirrors (source_system, archive_record_id, asset_id);
