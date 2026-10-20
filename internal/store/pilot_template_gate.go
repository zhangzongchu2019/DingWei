package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Enrollment is sticky: a new normal/R8 revision without a template binding
// cannot silently fall back to legacy gates. Legacy unenrolled specs are unchanged.
func templateSpecGate(ctx context.Context, q taskQuery, spec string, revision int, now time.Time) error {
	var enrolled int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pilot_template_enrollment WHERE spec_id=?`, spec).Scan(&enrolled); err != nil {
		return err
	}
	if enrolled == 0 {
		return nil
	}
	var draft, sha, actual string
	var version int
	err := q.QueryRowContext(ctx, `SELECT b.draft_id,b.draft_version,b.content_sha256,r.content_sha256 FROM pilot_template_binding b JOIN console_spec_revision r ON r.spec_id=b.spec_id AND r.revision=b.revision WHERE b.spec_id=? AND b.revision=?`, spec, revision).Scan(&draft, &version, &sha, &actual)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecGate
	}
	if err != nil {
		return err
	}
	if sha != actual {
		return ErrConsoleSpecGate
	}
	var raw, body, storedSHA string
	if err = q.QueryRowContext(ctx, `SELECT document_json,content_markdown,content_sha256 FROM pilot_template_revision WHERE draft_id=? AND version=?`, draft, version).Scan(&raw, &body, &storedSHA); err != nil {
		return err
	}
	if templateContentSHA(body) != sha || storedSHA != sha {
		return ErrConsoleSpecGate
	}
	var d TemplateDocument
	if json.Unmarshal([]byte(raw), &d) != nil {
		return ErrConsoleSpecGate
	}
	source, err := scanTemplateSource(q.QueryRowContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE id=?`, d.SourceID))
	if err != nil {
		return err
	}
	if source.State != "ready" || templateContentSHA(source.Content) != source.SHA || len(templateValidate(d, source, now)) != 0 {
		return ErrConsoleSpecGate
	}
	// Target CAS constraints apply only while preparing that revision, not after
	// it was atomically created. Parent references remain immutable live guards.
	parentOnly := d
	parentOnly.TargetSpecID = ""
	parentOnly.PredecessorSpecID = ""
	parentOnly.ExpectedRevision = 0
	if len(templateCrossReferences(ctx, q, source.Owner, parentOnly)) != 0 {
		return ErrConsoleSpecGate
	}
	for _, role := range templateRoles(d) {
		var decision string
		if err = q.QueryRowContext(ctx, `SELECT decision FROM pilot_template_receipt WHERE draft_id=? AND draft_version=? AND entry_id=? AND role=? AND principal_id=? ORDER BY id DESC LIMIT 1`, draft, version, role.EntryID, role.Role, role.Principal).Scan(&decision); err != nil || decision != "accepted" {
			return ErrConsoleSpecGate
		}
	}
	return nil
}
func templateTaskGate(ctx context.Context, q taskQuery, task string, now time.Time, pair ...string) error {
	var spec, group string
	var revision, current, impacts int
	// The legacy planning API also addresses console_task_proposal rows.
	// Preserve unenrolled proposals and fail closed on cross-table ID ambiguity.
	const refs = `SELECT id,spec_id,revision FROM console_spec_task UNION ALL SELECT id,spec_id,revision FROM console_task_proposal`
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM (`+refs+`) WHERE id=?`, task).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrTaskFlowConflict
	}
	if err := q.QueryRowContext(ctx, `SELECT t.spec_id,t.revision,s.current_revision,COALESCE(f.group_code,'') FROM (`+refs+`) t JOIN console_spec s ON s.id=t.spec_id LEFT JOIN console_spec_task f ON f.id=t.id WHERE t.id=?`, task).Scan(&spec, &revision, &current, &group); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pilot_template_impact WHERE predecessor_spec_id=? AND predecessor_revision=?`, spec, revision).Scan(&impacts); err != nil {
		return err
	}
	if impacts > 0 {
		return ErrTaskFlowConflict
	}
	var enrolled int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pilot_template_enrollment WHERE spec_id=?`, spec).Scan(&enrolled); err != nil {
		return err
	}
	if enrolled == 0 {
		return nil
	}
	if len(pair) != 2 && group == "" {
		return ErrTaskFlowConflict
	}
	if current != revision {
		return ErrTaskFlowConflict
	}
	if err := templateSpecGate(ctx, q, spec, revision, now); err != nil {
		return err
	}
	return templateTaskRolesMatch(ctx, q, task, spec, revision, pair...)
}

// Existing derive routes retain mandatory enrollment even before the successor
// has a newly reviewed template. They do not inherit another revision's receipts.
func inheritTemplateEnrollment(ctx context.Context, tx *sql.Tx, source, target, owner string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO pilot_template_enrollment(spec_id,owner_principal,created_at) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM pilot_template_enrollment WHERE spec_id=?) ON CONFLICT(spec_id) DO NOTHING`, target, owner, templateNow(now), source)
	return err
}

func templateFinalReviewGate(ctx context.Context, q taskQuery, spec string, revision int) error {
	var enrolled int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pilot_template_enrollment WHERE spec_id=?`, spec).Scan(&enrolled); err != nil {
		return err
	}
	if enrolled == 0 {
		return nil
	}
	var agent, raw string
	err := q.QueryRowContext(ctx, `SELECT g.agent_id,r.document_json FROM console_spec_agent_gate g JOIN console_spec_agent_request a ON a.id=g.request_id JOIN pilot_template_binding b ON b.spec_id=g.spec_id AND b.revision=g.revision JOIN pilot_template_revision r ON r.draft_id=b.draft_id AND r.version=b.draft_version WHERE g.spec_id=? AND g.revision=? AND g.human_confirmed=1 AND a.status='completed' AND a.kind='review' AND a.agent_id=g.agent_id AND a.snapshot_sha=b.content_sha256`, spec, revision).Scan(&agent, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecGate
	}
	if err != nil {
		return err
	}
	var d TemplateDocument
	if json.Unmarshal([]byte(raw), &d) != nil {
		return ErrConsoleSpecGate
	}
	for _, entry := range d.Entries {
		if entry.Fields.Executor == "agent:"+agent {
			return ErrConsoleSpecGate
		}
		if d.TeamMode == "single" && entry.Fields.Reviewer != "agent:"+agent {
			return ErrConsoleSpecGate
		}
	}
	return nil
}

func templateTaskRolesMatch(ctx context.Context, q taskQuery, task, spec string, revision int, pair ...string) error {
	var dev, review string
	if len(pair) == 2 {
		dev, review = pair[0], pair[1]
	} else if err := q.QueryRowContext(ctx, `SELECT COALESCE(developer_agent_id,''),COALESCE(reviewer_agent_id,'') FROM console_spec_task WHERE id=?`, task).Scan(&dev, &review); err != nil {
		return err
	}
	if dev == "" || review == "" || dev == review {
		return ErrTaskFlowConflict
	}
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT r.document_json FROM pilot_template_binding b JOIN pilot_template_revision r ON r.draft_id=b.draft_id AND r.version=b.draft_version WHERE b.spec_id=? AND b.revision=?`, spec, revision).Scan(&raw); err != nil {
		return err
	}
	var d TemplateDocument
	if json.Unmarshal([]byte(raw), &d) != nil {
		return ErrConsoleSpecGate
	}
	rows, err := q.QueryContext(ctx, `SELECT acceptance_id FROM console_spec_task_acceptance WHERE task_id=?`, task)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		selected[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	matches := func(principal, agent string) bool {
		if principal == "agent:"+agent {
			return true
		}
		if !strings.HasPrefix(principal, "user:") {
			return false
		}
		var owner string
		return q.QueryRowContext(ctx, `SELECT owner_principal FROM console_agent WHERE id=?`, agent).Scan(&owner) == nil && principal == "user:"+owner
	}
	found := 0
	for _, entry := range d.Entries {
		if len(selected) > 0 && !selected[entry.ID] {
			continue
		}
		found++
		if !matches(entry.Fields.Executor, dev) || !matches(entry.Fields.Reviewer, review) {
			return ErrTaskFlowConflict
		}
	}
	if found == 0 {
		return ErrTaskFlowConflict
	}
	return nil
}
