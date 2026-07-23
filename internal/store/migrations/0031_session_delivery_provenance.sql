-- Optional Hub-attested display provenance. Historical rows stay NULL and are
-- rendered as unknown. No backfill is attempted.
ALTER TABLE session_delivery ADD COLUMN provenance TEXT;
