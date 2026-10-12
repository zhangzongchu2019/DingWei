-- Temporary unowned device claims, credentials and raw pairing codes are never stored.
CREATE TABLE IF NOT EXISTS np_device_claim (
    id text PRIMARY KEY,
    code_digest text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    body jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS np_device_claim_expiry ON np_device_claim(expires_at);
