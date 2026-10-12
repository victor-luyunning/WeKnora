ALTER TABLE fms_projection_mirrors
    ADD COLUMN IF NOT EXISTS book_classification JSONB NOT NULL DEFAULT '{}'::jsonb;
