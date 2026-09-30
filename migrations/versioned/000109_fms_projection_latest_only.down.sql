ALTER TABLE fms_projection_mirrors
    DROP CONSTRAINT IF EXISTS uq_fms_projection_mirrors_source_asset;

ALTER TABLE fms_projection_mirrors
    ADD CONSTRAINT uq_fms_projection_mirrors_source_revision UNIQUE (source_system, source_ref, revision_key);
