-- sh-fix: SessionHelper reports both short routing name and full tmux session name.
ALTER TABLE session_endpoint ADD COLUMN full_session_name TEXT NOT NULL DEFAULT '';
