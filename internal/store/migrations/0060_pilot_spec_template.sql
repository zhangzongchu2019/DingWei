-- Pilot-only immutable source/preview/consent records. Existing specs are not
-- silently enrolled. Spec revisions and task gates remain separate from signoff.
CREATE TABLE pilot_spec_source (
 id TEXT PRIMARY KEY, owner_principal TEXT NOT NULL, request_id TEXT NOT NULL,
 request_sha TEXT NOT NULL, source_url TEXT NOT NULL, document_kind TEXT NOT NULL,
 document_id TEXT NOT NULL, source_revision TEXT NOT NULL DEFAULT '',
 source_kind TEXT NOT NULL CHECK(source_kind IN('manual_export','trusted_connector','pending_connector')),
 state TEXT NOT NULL CHECK(state IN('ready','pending_config')),
 content_markdown TEXT NOT NULL DEFAULT '', content_sha256 TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, UNIQUE(owner_principal,request_id)
);
CREATE UNIQUE INDEX idx_pilot_spec_source_content ON pilot_spec_source(owner_principal,source_url,source_revision,content_sha256,source_kind) WHERE state='ready';
CREATE TABLE pilot_template_draft (
 id TEXT PRIMARY KEY, owner_principal TEXT NOT NULL, current_version INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN('pending','confirmed','cancelled')),
 confirmed_spec_id TEXT NOT NULL DEFAULT '', confirmed_revision INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX idx_pilot_template_owner ON pilot_template_draft(owner_principal,id);
CREATE TABLE pilot_template_revision (
 draft_id TEXT NOT NULL REFERENCES pilot_template_draft(id), version INTEGER NOT NULL,
 source_id TEXT NOT NULL REFERENCES pilot_spec_source(id), document_json TEXT NOT NULL,
 plan_sha256 TEXT NOT NULL, content_markdown TEXT NOT NULL, acceptance_json TEXT NOT NULL,
 content_sha256 TEXT NOT NULL, mode TEXT NOT NULL CHECK(mode IN('light','full')),
 created_at TEXT NOT NULL, PRIMARY KEY(draft_id,version)
);
CREATE TABLE pilot_template_role (
 draft_id TEXT NOT NULL, draft_version INTEGER NOT NULL, entry_id TEXT NOT NULL,
 role TEXT NOT NULL, principal_id TEXT NOT NULL,
 PRIMARY KEY(draft_id,draft_version,entry_id,role),
 FOREIGN KEY(draft_id,draft_version) REFERENCES pilot_template_revision(draft_id,version)
);
CREATE INDEX idx_pilot_template_role_principal ON pilot_template_role(principal_id,draft_id,draft_version);
CREATE TABLE pilot_template_receipt (
 id INTEGER PRIMARY KEY, draft_id TEXT NOT NULL, draft_version INTEGER NOT NULL,
 entry_id TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN('executor','reviewer','signoff','full_review')),
 principal_id TEXT NOT NULL, decision TEXT NOT NULL CHECK(decision IN('accepted','withdrawn')),
 reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
 FOREIGN KEY(draft_id,draft_version) REFERENCES pilot_template_revision(draft_id,version)
);
CREATE INDEX idx_pilot_template_receipt_latest ON pilot_template_receipt(draft_id,draft_version,entry_id,role,principal_id,id);
CREATE TABLE pilot_template_enrollment (
 spec_id TEXT PRIMARY KEY REFERENCES console_spec(id), owner_principal TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE pilot_template_binding (
 spec_id TEXT NOT NULL REFERENCES pilot_template_enrollment(spec_id), revision INTEGER NOT NULL,
 draft_id TEXT NOT NULL, draft_version INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 family_revision INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL,
 PRIMARY KEY(spec_id,revision), FOREIGN KEY(draft_id,draft_version) REFERENCES pilot_template_revision(draft_id,version)
);
CREATE TABLE pilot_template_impact (
 predecessor_spec_id TEXT NOT NULL REFERENCES console_spec(id), predecessor_revision INTEGER NOT NULL,
 successor_spec_id TEXT NOT NULL REFERENCES console_spec(id), reason TEXT NOT NULL,
 confirmed_by TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(predecessor_spec_id,predecessor_revision,successor_spec_id)
);
CREATE TABLE pilot_template_request (
 actor_id TEXT NOT NULL, request_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 result_json TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(actor_id,request_id)
);
CREATE TABLE pilot_template_audit (
 id INTEGER PRIMARY KEY, draft_id TEXT NOT NULL DEFAULT '', spec_id TEXT NOT NULL DEFAULT '',
 actor_id TEXT NOT NULL, operation TEXT NOT NULL, details_json TEXT NOT NULL, created_at TEXT NOT NULL
);
-- D1 retains final-spec immutability. A D3 proposal may explicitly reference a
-- same-owner derived successor, rather than pretending final can change in place.
ALTER TABLE pilot_question_change ADD COLUMN new_spec_id TEXT NOT NULL DEFAULT '';
