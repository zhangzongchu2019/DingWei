-- R15: SessionHelper tool/model metadata.
ALTER TABLE session_endpoint ADD COLUMN tool TEXT NOT NULL DEFAULT '';
ALTER TABLE session_endpoint ADD COLUMN model TEXT NOT NULL DEFAULT '';
