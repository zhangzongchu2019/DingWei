-- R6 metadata only. No source text, credentials, repository scopes or grants.
CREATE TABLE coordinate_review (
 id TEXT PRIMARY KEY,
 revision INTEGER NOT NULL,
 body TEXT NOT NULL
);
CREATE TABLE coordinate_review_agent_binding (
 agent_id TEXT PRIMARY KEY,
 account_id TEXT NOT NULL,
 login TEXT NOT NULL,
 updated_by TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE coordinate_review_binding_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 agent_id TEXT NOT NULL,
 actor TEXT NOT NULL,
 operation TEXT NOT NULL,
 created_at TEXT NOT NULL
);
-- Shared immutable semantic ownership for R6/R9 notification candidates.
CREATE TABLE notification_event_owner (
 event_id TEXT PRIMARY KEY,
 semantic_owner TEXT NOT NULL CHECK(semantic_owner IN ('r6','r9')),
 assignment TEXT NOT NULL
);
CREATE TRIGGER notification_event_owner_no_update BEFORE UPDATE ON notification_event_owner
 BEGIN SELECT RAISE(ABORT,'notification event ownership is immutable'); END;
CREATE TRIGGER notification_event_owner_no_delete BEFORE DELETE ON notification_event_owner
 BEGIN SELECT RAISE(ABORT,'notification event ownership is immutable'); END;
CREATE TRIGGER coordinate_review_no_delete BEFORE DELETE ON coordinate_review
 BEGIN SELECT RAISE(ABORT,'coordinate review history is immutable'); END;
CREATE TRIGGER coordinate_review_history BEFORE UPDATE ON coordinate_review
 WHEN json_valid(NEW.body)=0 OR NEW.id<>OLD.id
 OR json_extract(NEW.body,'$.coordinate')<>json_extract(OLD.body,'$.coordinate')
 OR json_extract(NEW.body,'$.agent_id')<>json_extract(OLD.body,'$.agent_id')
 OR json_extract(NEW.body,'$.assigned_by')<>json_extract(OLD.body,'$.assigned_by')
 OR json_extract(NEW.body,'$.event_id')<>json_extract(OLD.body,'$.event_id')
 OR EXISTS(SELECT 1 FROM json_each(OLD.body,'$.results') o WHERE json_extract(NEW.body,'$.results['||o.key||']') IS NOT o.value)
 OR EXISTS(SELECT 1 FROM json_each(OLD.body,'$.comparisons') o WHERE json_extract(NEW.body,'$.comparisons['||o.key||']') IS NOT o.value)
 BEGIN SELECT RAISE(ABORT,'coordinate review facts are append only'); END;
