-- SQLite equivalent of the PostgreSQL FMS projection mirror migration.
-- These rows are a rebuildable, read-only cache of FMS's published API, never
-- source EPUBs, object keys, local paths, or FMS credentials.

CREATE TABLE IF NOT EXISTS fms_projection_mirrors (
    id TEXT PRIMARY KEY,
    source_system TEXT NOT NULL DEFAULT 'fms',
    source_ref TEXT NOT NULL,
    revision_key TEXT NOT NULL,
    archive_record_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    book_no TEXT NOT NULL DEFAULT '',
    book_title TEXT NOT NULL DEFAULT '',
    book_subject TEXT NOT NULL DEFAULT '',
    source_checksum_sha256 TEXT NOT NULL,
    parser_version TEXT NOT NULL DEFAULT '',
    segment_policy_version TEXT NOT NULL DEFAULT '',
    embedding_policy TEXT NOT NULL DEFAULT '',
    projection_state TEXT NOT NULL,
    readiness_status TEXT NOT NULL,
    handoff_eligible BOOLEAN NOT NULL,
    artifacts JSON NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_synced_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (source_system, source_ref, revision_key)
);

CREATE INDEX IF NOT EXISTS idx_fms_projection_mirrors_source_ref
    ON fms_projection_mirrors (source_system, source_ref, last_synced_at DESC);

CREATE TABLE IF NOT EXISTS fms_mirror_artifact_records (
    id TEXT PRIMARY KEY,
    mirror_id TEXT NOT NULL REFERENCES fms_projection_mirrors(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    record_index INTEGER NOT NULL,
    payload JSON NOT NULL,
    UNIQUE (mirror_id, kind, record_index)
);

CREATE INDEX IF NOT EXISTS idx_fms_mirror_artifact_records_mirror_kind
    ON fms_mirror_artifact_records (mirror_id, kind, record_index);

CREATE TABLE IF NOT EXISTS fms_mirror_retrieval_units (
    id TEXT PRIMARY KEY,
    mirror_id TEXT NOT NULL REFERENCES fms_projection_mirrors(id) ON DELETE CASCADE,
    unit_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    structural_node_id TEXT NOT NULL,
    content_type TEXT NOT NULL,
    content_text TEXT NOT NULL,
    locator JSON NOT NULL,
    payload JSON NOT NULL,
    UNIQUE (mirror_id, unit_id)
);

CREATE INDEX IF NOT EXISTS idx_fms_mirror_retrieval_units_structure
    ON fms_mirror_retrieval_units (mirror_id, structural_node_id, ordinal);

CREATE TABLE IF NOT EXISTS fms_sync_runs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME NULL,
    summary JSON NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_fms_sync_runs_started_at ON fms_sync_runs (started_at DESC);
