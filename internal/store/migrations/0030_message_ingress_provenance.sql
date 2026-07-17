-- Persist the authentication boundary that produced each inbound message.
-- Existing rows and writers that omit the field remain fail-closed.
ALTER TABLE message ADD COLUMN ingress_provenance TEXT NOT NULL DEFAULT 'untrusted'
  CHECK (ingress_provenance IN ('untrusted', 'webhook_verified', 'feishu_ws_authenticated'));
