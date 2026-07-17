-- sh-fix: active session names are unique inside one owner account.
ALTER TABLE session_endpoint ADD COLUMN owner_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS ux_session_endpoint_owner_active_name
ON session_endpoint(owner_key, session_name)
WHERE active=1 AND owner_key <> '';
