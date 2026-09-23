PRAGMA defer_foreign_keys=ON;

CREATE TABLE console_spec_v2 (
  id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  title TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('editing','review','final')),
  current_revision INTEGER NOT NULL CHECK(current_revision > 0),
  legacy_state TEXT CHECK(legacy_state IS NULL OR legacy_state IN ('draft','needs_improvement','hard_gate','human_review','claimable','in_progress','archived')),
  current_editor_id TEXT REFERENCES console_principal(id),
  archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0,1)),
  task_state TEXT NOT NULL DEFAULT 'splitting' CHECK(task_state IN ('splitting','closed')),
  edit_capability_hash TEXT NOT NULL DEFAULT '',
  editing_closed INTEGER NOT NULL DEFAULT 0 CHECK(editing_closed IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK((state='final' AND current_editor_id IS NULL) OR (state<>'final' AND current_editor_id IS NOT NULL))
);

INSERT INTO console_spec_v2(id,principal_id,title,state,current_revision,legacy_state,current_editor_id,archived,task_state,edit_capability_hash,editing_closed,created_at,updated_at)
SELECT id,principal_id,title,
  CASE
    WHEN state IN ('draft','needs_improvement') THEN 'editing'
    WHEN state IN ('hard_gate','human_review') THEN 'review'
    WHEN state IN ('claimable','in_progress','archived') THEN 'final'
  END,
  current_revision,
  state,
  CASE WHEN state IN ('claimable','in_progress','archived') THEN NULL ELSE principal_id END,
  CASE WHEN state='archived' THEN 1 ELSE 0 END,
  'splitting',edit_capability_hash,editing_closed,created_at,updated_at
FROM console_spec;

DROP TABLE console_spec;
ALTER TABLE console_spec_v2 RENAME TO console_spec;
CREATE INDEX idx_console_spec_owner_state ON console_spec(principal_id,state,updated_at,id);
CREATE UNIQUE INDEX ux_console_principal_display_name ON console_principal(display_name);
