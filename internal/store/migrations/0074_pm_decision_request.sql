-- R9 PM decision request state is isolated from legacy clarification and delivery tables.
CREATE TABLE r9_pm_responsibility (
 provider TEXT NOT NULL, instance TEXT NOT NULL, repository_id TEXT NOT NULL,
 change_directory TEXT NOT NULL, point_id TEXT NOT NULL,
 pm_user_id_lc TEXT, mapping_generation INTEGER NOT NULL CHECK(mapping_generation>=0),
 set_by_user_id_lc TEXT NOT NULL DEFAULT '', updated_at_utc TEXT NOT NULL DEFAULT '', deleted_at_utc TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(provider,instance,repository_id,change_directory,point_id)
);
CREATE TABLE r9_pm_request (
 request_id TEXT PRIMARY KEY,
 provider TEXT NOT NULL, instance TEXT NOT NULL, repository_id TEXT NOT NULL, change_directory TEXT NOT NULL,
 commit_sha TEXT NOT NULL, point_id TEXT NOT NULL, content_sha256 TEXT NOT NULL,
 pm_user_id_lc TEXT NOT NULL, mapping_generation INTEGER NOT NULL, generation INTEGER NOT NULL CHECK(generation>0),
 status TEXT NOT NULL CHECK(status IN ('open','answered','timed_out','superseded','cancelled','unknown')),
 created_at_utc TEXT NOT NULL, deadline_at_utc TEXT NOT NULL, next_reminder_at_utc TEXT NOT NULL DEFAULT '',
 missed_periods INTEGER NOT NULL DEFAULT 0 CHECK(missed_periods>=0),
 answered_at_utc TEXT NOT NULL DEFAULT '', answer TEXT NOT NULL DEFAULT '', created_by_user_id_lc TEXT NOT NULL DEFAULT '', mr_writeback_status TEXT NOT NULL DEFAULT 'postponed' CHECK(mr_writeback_status IN ('postponed','pending','written','unknown','failed')), summary_json TEXT NOT NULL DEFAULT '{}',
 UNIQUE(provider,instance,repository_id,change_directory,commit_sha,point_id,content_sha256,pm_user_id_lc,generation)
);
CREATE INDEX r9_pm_request_due ON r9_pm_request(status,next_reminder_at_utc);
CREATE INDEX r9_pm_request_coordinate ON r9_pm_request(provider,instance,repository_id,change_directory,point_id,generation);
CREATE TABLE r9_pm_response (
 response_id TEXT PRIMARY KEY, request_id TEXT NOT NULL, generation INTEGER NOT NULL,
 principal_user_id_lc TEXT NOT NULL, channel TEXT NOT NULL CHECK(channel IN ('console','feishu')),
 decision TEXT NOT NULL CHECK(decision IN ('agree','disagree','revise')), comment TEXT NOT NULL DEFAULT '',
 issuer TEXT NOT NULL DEFAULT '', tenant TEXT NOT NULL DEFAULT '', message_id TEXT NOT NULL DEFAULT '',
 operation_id TEXT NOT NULL DEFAULT '', received_seq INTEGER NOT NULL, received_at_utc TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('accepted','late','rejected')),
 accepted_slot INTEGER NOT NULL DEFAULT 0 CHECK(accepted_slot IN (0,1))
);
CREATE UNIQUE INDEX r9_pm_response_message_unique ON r9_pm_response(issuer,tenant,message_id) WHERE message_id<>'';
CREATE UNIQUE INDEX r9_pm_response_accept_once ON r9_pm_response(request_id,generation,accepted_slot) WHERE accepted_slot=1;
CREATE TABLE r9_pm_delivery (
 operation_id TEXT PRIMARY KEY, request_id TEXT NOT NULL, generation INTEGER NOT NULL,
 channel TEXT NOT NULL CHECK(channel IN ('console','feishu')), kind TEXT NOT NULL CHECK(kind IN ('initial','reminder')),
 period_index INTEGER NOT NULL, recipient_id TEXT NOT NULL, issuer TEXT NOT NULL DEFAULT '', state NOT NULL CHECK(state IN ('pending','sent','failed','unknown','closed')),
 provider_message_id TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0,
 created_at_utc TEXT NOT NULL, attempted_at_utc TEXT NOT NULL DEFAULT '', receipt_at_utc TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
 UNIQUE(request_id,generation,channel,kind,period_index)
);
CREATE INDEX r9_pm_delivery_pending ON r9_pm_delivery(state,created_at_utc);
CREATE TABLE r9_pm_inbound_key (
 issuer TEXT NOT NULL, tenant TEXT NOT NULL, key_kind TEXT NOT NULL, event_or_message_id TEXT NOT NULL,
 payload_sha256 TEXT NOT NULL, request_id TEXT NOT NULL, generation INTEGER NOT NULL,
 received_at_utc TEXT NOT NULL, state TEXT NOT NULL,
 PRIMARY KEY(issuer,tenant,key_kind,event_or_message_id)
);
CREATE TABLE r9_pm_audit (
 audit_id INTEGER PRIMARY KEY AUTOINCREMENT, actor_user_id_lc TEXT NOT NULL,
 provider TEXT NOT NULL, instance TEXT NOT NULL, repository_id TEXT NOT NULL, change_directory TEXT NOT NULL, point_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('set','delete','request','response','timeout','supersede')), pm_user_id_lc TEXT NOT NULL DEFAULT '',
 mapping_generation INTEGER NOT NULL, at_utc TEXT NOT NULL, result TEXT NOT NULL DEFAULT 'ok'
);
CREATE INDEX r9_pm_audit_coordinate ON r9_pm_audit(provider,instance,repository_id,change_directory,point_id,audit_id);
