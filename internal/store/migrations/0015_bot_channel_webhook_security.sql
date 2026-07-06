-- R19: optional Feishu webhook verification/decryption settings.

ALTER TABLE bot_channel ADD COLUMN verification_token TEXT;
ALTER TABLE bot_channel ADD COLUMN encrypt_key TEXT;
