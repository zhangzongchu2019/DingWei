-- PG 8 -> 9: preserve the absolute age of new-platform sessions for bounded sliding renewal.
-- Existing rows receive the migration time (now()) as their creation time.
ALTER TABLE np_session
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

COMMENT ON COLUMN np_session.created_at IS
    'Session creation time; rows existing before migration 0104 use the migration time as their creation time.';
