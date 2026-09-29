-- FMS remains the authority for publications, permission, source assets and
-- evidence locators. These tables hold a deletable/rebuildable WeKnora mirror
-- of one fully fetched published source revision; no FMS object key, local
-- path, original EPUB or credential is persisted here.

CREATE TABLE IF NOT EXISTS fms_projection_mirrors (
    id VARCHAR(36) PRIMARY KEY,
    source_system VARCHAR(32) NOT NULL DEFAULT 'fms',
    source_ref TEXT NOT NULL,
    revision_key VARCHAR(64) NOT NULL,
    archive_record_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    book_no TEXT NOT NULL DEFAULT '',
    book_title TEXT NOT NULL DEFAULT '',
    book_subject TEXT NOT NULL DEFAULT '',
    source_checksum_sha256 VARCHAR(64) NOT NULL,
    parser_version TEXT NOT NULL DEFAULT '',
    segment_policy_version TEXT NOT NULL DEFAULT '',
    embedding_policy TEXT NOT NULL DEFAULT '',
    projection_state VARCHAR(32) NOT NULL,
    readiness_status VARCHAR(32) NOT NULL,
    handoff_eligible BOOLEAN NOT NULL,
    artifacts JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_fms_projection_mirrors_source_revision UNIQUE (source_system, source_ref, revision_key)
);

CREATE INDEX IF NOT EXISTS idx_fms_projection_mirrors_source_ref
    ON fms_projection_mirrors (source_system, source_ref, last_synced_at DESC);

CREATE TABLE IF NOT EXISTS fms_mirror_artifact_records (
    id VARCHAR(36) PRIMARY KEY,
    mirror_id VARCHAR(36) NOT NULL REFERENCES fms_projection_mirrors(id) ON DELETE CASCADE,
    kind VARCHAR(64) NOT NULL,
    record_index INTEGER NOT NULL,
    payload JSONB NOT NULL,
    CONSTRAINT uq_fms_mirror_artifact_record UNIQUE (mirror_id, kind, record_index)
);

CREATE INDEX IF NOT EXISTS idx_fms_mirror_artifact_records_mirror_kind
    ON fms_mirror_artifact_records (mirror_id, kind, record_index);

CREATE TABLE IF NOT EXISTS fms_mirror_retrieval_units (
    id VARCHAR(36) PRIMARY KEY,
    mirror_id VARCHAR(36) NOT NULL REFERENCES fms_projection_mirrors(id) ON DELETE CASCADE,
    unit_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    structural_node_id TEXT NOT NULL,
    content_type VARCHAR(64) NOT NULL,
    content_text TEXT NOT NULL,
    locator JSONB NOT NULL,
    payload JSONB NOT NULL,
    CONSTRAINT uq_fms_mirror_retrieval_unit UNIQUE (mirror_id, unit_id)
);

CREATE INDEX IF NOT EXISTS idx_fms_mirror_retrieval_units_structure
    ON fms_mirror_retrieval_units (mirror_id, structural_node_id, ordinal);

CREATE TABLE IF NOT EXISTS fms_sync_runs (
    id VARCHAR(36) PRIMARY KEY,
    status VARCHAR(32) NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP WITH TIME ZONE NULL,
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_fms_sync_runs_started_at ON fms_sync_runs (started_at DESC);
