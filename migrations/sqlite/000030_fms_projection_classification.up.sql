ALTER TABLE fms_projection_mirrors
    ADD COLUMN book_classification JSON NOT NULL DEFAULT '{}';
