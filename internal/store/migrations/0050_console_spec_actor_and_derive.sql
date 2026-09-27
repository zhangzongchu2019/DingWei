CREATE TABLE console_spec_revision_v3 (
  spec_id TEXT NOT NULL REFERENCES console_spec(id),
  revision INTEGER NOT NULL CHECK(revision > 0),
  content_markdown TEXT NOT NULL,
  acceptance_json TEXT NOT NULL,
  input_anchors_json TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_by_kind TEXT NOT NULL DEFAULT 'unknown' CHECK(created_by_kind IN ('unknown','principal','agent')),
  created_via TEXT NOT NULL DEFAULT 'principal' CHECK(created_via IN ('principal','shared_link')),
  supersedes_revision INTEGER,
  created_at TEXT NOT NULL,
  PRIMARY KEY(spec_id, revision)
);

INSERT INTO console_spec_revision_v3(
  spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,
  created_by,created_by_kind,created_via,supersedes_revision,created_at
)
SELECT spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,
       created_by,'unknown',created_via,supersedes_revision,created_at
FROM console_spec_revision;

DROP TABLE console_spec_revision;
ALTER TABLE console_spec_revision_v3 RENAME TO console_spec_revision;

ALTER TABLE console_spec ADD COLUMN derived_from TEXT REFERENCES console_spec(id);
CREATE INDEX idx_console_spec_derived_from ON console_spec(derived_from);
