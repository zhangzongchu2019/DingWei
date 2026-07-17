-- R18: producer session metadata.

ALTER TABLE session_endpoint ADD COLUMN producer INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_endpoint ADD COLUMN target_group TEXT NOT NULL DEFAULT '';
