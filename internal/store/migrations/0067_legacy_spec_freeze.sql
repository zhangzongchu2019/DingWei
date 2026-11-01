-- R11: one database-wide, monotonic cutover; no legacy objects are rewritten.
CREATE TABLE IF NOT EXISTS legacy_spec_freeze (
 id INTEGER PRIMARY KEY CHECK(id=1),
 frozen INTEGER NOT NULL DEFAULT 0 CHECK(frozen IN (0,1)),
 frozen_at TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO legacy_spec_freeze(id,frozen,frozen_at) VALUES(1,0,'');
CREATE TRIGGER IF NOT EXISTS legacy_spec_freeze_no_delete BEFORE DELETE ON legacy_spec_freeze
 BEGIN SELECT RAISE(ABORT,'legacy_spec_freeze_immutable'); END;
CREATE TRIGGER IF NOT EXISTS legacy_spec_freeze_no_reopen BEFORE UPDATE ON legacy_spec_freeze
 WHEN OLD.frozen=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_freeze_immutable'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_insert BEFORE INSERT ON console_spec
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_update BEFORE UPDATE ON console_spec
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_delete BEFORE DELETE ON console_spec
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_revision_insert BEFORE INSERT ON console_spec_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_revision_update BEFORE UPDATE ON console_spec_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_revision_delete BEFORE DELETE ON console_spec_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_review_insert BEFORE INSERT ON console_spec_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_review_update BEFORE UPDATE ON console_spec_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_review_delete BEFORE DELETE ON console_spec_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_event_insert BEFORE INSERT ON console_spec_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_event_update BEFORE UPDATE ON console_spec_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_event_delete BEFORE DELETE ON console_spec_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_gate_review_insert BEFORE INSERT ON console_spec_gate_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_gate_review_update BEFORE UPDATE ON console_spec_gate_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_gate_review_delete BEFORE DELETE ON console_spec_gate_review
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_presplit_request_insert BEFORE INSERT ON console_spec_presplit_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_presplit_request_update BEFORE UPDATE ON console_spec_presplit_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_presplit_request_delete BEFORE DELETE ON console_spec_presplit_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_request_insert BEFORE INSERT ON console_spec_agent_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_request_update BEFORE UPDATE ON console_spec_agent_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_request_delete BEFORE DELETE ON console_spec_agent_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_gate_insert BEFORE INSERT ON console_spec_agent_gate
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_gate_update BEFORE UPDATE ON console_spec_agent_gate
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_gate_delete BEFORE DELETE ON console_spec_agent_gate
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_fresh_insert BEFORE INSERT ON console_spec_agent_fresh
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_fresh_update BEFORE UPDATE ON console_spec_agent_fresh
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_fresh_delete BEFORE DELETE ON console_spec_agent_fresh
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_outbox_insert BEFORE INSERT ON console_spec_agent_outbox
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_outbox_update BEFORE UPDATE ON console_spec_agent_outbox
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_agent_outbox_delete BEFORE DELETE ON console_spec_agent_outbox
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_insert BEFORE INSERT ON console_spec_working_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_update BEFORE UPDATE ON console_spec_working_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_delete BEFORE DELETE ON console_spec_working_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_conflict_insert BEFORE INSERT ON console_spec_working_draft_conflict
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_conflict_update BEFORE UPDATE ON console_spec_working_draft_conflict
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_working_draft_conflict_delete BEFORE DELETE ON console_spec_working_draft_conflict
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_insert BEFORE INSERT ON console_spec_flow
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_update BEFORE UPDATE ON console_spec_flow
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_delete BEFORE DELETE ON console_spec_flow
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_event_insert BEFORE INSERT ON console_spec_flow_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_event_update BEFORE UPDATE ON console_spec_flow_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_event_delete BEFORE DELETE ON console_spec_flow_event
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_assist_insert BEFORE INSERT ON console_spec_flow_assist
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_assist_update BEFORE UPDATE ON console_spec_flow_assist
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_assist_delete BEFORE DELETE ON console_spec_flow_assist
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_decision_insert BEFORE INSERT ON console_spec_flow_decision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_decision_update BEFORE UPDATE ON console_spec_flow_decision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_console_spec_flow_decision_delete BEFORE DELETE ON console_spec_flow_decision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_spec_source_insert BEFORE INSERT ON pilot_spec_source
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_spec_source_update BEFORE UPDATE ON pilot_spec_source
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_spec_source_delete BEFORE DELETE ON pilot_spec_source
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_draft_insert BEFORE INSERT ON pilot_template_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_draft_update BEFORE UPDATE ON pilot_template_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_draft_delete BEFORE DELETE ON pilot_template_draft
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_revision_insert BEFORE INSERT ON pilot_template_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_revision_update BEFORE UPDATE ON pilot_template_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_revision_delete BEFORE DELETE ON pilot_template_revision
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_role_insert BEFORE INSERT ON pilot_template_role
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_role_update BEFORE UPDATE ON pilot_template_role
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_role_delete BEFORE DELETE ON pilot_template_role
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_receipt_insert BEFORE INSERT ON pilot_template_receipt
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_receipt_update BEFORE UPDATE ON pilot_template_receipt
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_receipt_delete BEFORE DELETE ON pilot_template_receipt
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_enrollment_insert BEFORE INSERT ON pilot_template_enrollment
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_enrollment_update BEFORE UPDATE ON pilot_template_enrollment
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_enrollment_delete BEFORE DELETE ON pilot_template_enrollment
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_binding_insert BEFORE INSERT ON pilot_template_binding
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_binding_update BEFORE UPDATE ON pilot_template_binding
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_binding_delete BEFORE DELETE ON pilot_template_binding
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_impact_insert BEFORE INSERT ON pilot_template_impact
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_impact_update BEFORE UPDATE ON pilot_template_impact
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_impact_delete BEFORE DELETE ON pilot_template_impact
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_request_insert BEFORE INSERT ON pilot_template_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_request_update BEFORE UPDATE ON pilot_template_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_request_delete BEFORE DELETE ON pilot_template_request
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_audit_insert BEFORE INSERT ON pilot_template_audit
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_audit_update BEFORE UPDATE ON pilot_template_audit
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
CREATE TRIGGER IF NOT EXISTS r11_freeze_pilot_template_audit_delete BEFORE DELETE ON pilot_template_audit
 WHEN (SELECT frozen FROM legacy_spec_freeze WHERE id=1)=1
 BEGIN SELECT RAISE(ABORT,'legacy_spec_frozen'); END;
