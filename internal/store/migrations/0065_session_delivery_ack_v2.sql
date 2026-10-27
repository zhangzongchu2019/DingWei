-- Historical deliveries deliberately retain NULL protocol/mode/generation.
-- Reconnecting with a newer helper must not invent their admission semantics.
ALTER TABLE session_delivery ADD COLUMN ack_protocol INTEGER CHECK(ack_protocol IN (1,2));
ALTER TABLE session_delivery ADD COLUMN processing_mode TEXT;
ALTER TABLE session_delivery ADD COLUMN ack_generation INTEGER CHECK(ack_generation > 0);
ALTER TABLE session_delivery ADD COLUMN injected_at TEXT;
ALTER TABLE session_delivery ADD COLUMN turn_id TEXT;
ALTER TABLE session_delivery ADD COLUMN turn_outcome TEXT CHECK(turn_outcome IN ('completed','interrupted','failed'));
