package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// appendBlockerInput is shared by D2 and D3. Both callers have already checked
// current principal/object rights and execute this inside their own transaction.
// It creates one logical input/wake, never a task start or a second scheduler.
func appendBlockerInput(ctx context.Context, tx *sql.Tx, b *TaskBlocker, actor, source, event, body, evidence string, production bool, at string) (bool, error) {
	if actor == "" || blockerTerminal(b.State) || source == "" || len(source) > 256 || event == "" || len(event) > 128 || strings.TrimSpace(body) == "" || len(body) > 8192 || strings.TrimSpace(evidence) == "" || len(evidence) > 2048 {
		return false, ErrTaskFlowInvalid
	}
	hash := blockerHash([]any{actor, body, evidence, production})
	var previous string
	err := tx.QueryRowContext(ctx, `SELECT payload_sha FROM console_task_blocker_input WHERE blocker_id=? AND source=? AND event_id=?`, b.ID, source, event).Scan(&previous)
	if err == nil {
		if previous != hash {
			return false, ErrTaskFlowConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if production && b.Mode != "full" {
		b.Mode = "full"
		b.FullApprovedBy = ""
		b.FullApprovalRef = ""
	}
	b.InputVersion++
	b.Version++
	b.UpdatedAt = at
	if b.State != "rechecking" {
		b.State = "ready_to_recheck"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_input(blocker_id,source,event_id,payload_sha,input_version,actor_id,body,evidence_ref,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, b.ID, source, event, hash, b.InputVersion, actor, body, evidence, at); err != nil {
		return false, err
	}
	if err = blockerWake(ctx, tx, *b, at); err != nil {
		return false, err
	}
	return true, nil
}
func questionCreateBlocker(ctx context.Context, tx *sql.Tx, c QuestionContext, a BlockerActor, q PilotQuestion, now time.Time) (TaskBlocker, error) {
	var b TaskBlocker
	if c.Task == nil {
		return b, ErrTaskFlowInvalid
	}
	var status, old string
	if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(blocker,'') FROM console_spec_task WHERE id=?`, c.TaskID).Scan(&status, &old); err != nil {
		return b, err
	}
	// Post-completion feedback is recorded without reopening or waking a task.
	if status == "done" {
		return b, nil
	}
	conditions, err := blockerConditions(nil)
	if err != nil {
		return b, err
	}
	at := now.UTC().Format(time.RFC3339Nano)
	b = TaskBlocker{ID: "qblk:" + blockerHash([]any{q.ID, q.Generation}), TaskID: c.TaskID, SpecID: c.SpecID, Revision: c.Revision, SHA: c.SHA, AssignmentVersion: c.Task.AssignmentVersion, Developer: c.Task.Developer, Reviewer: c.Task.Reviewer, Generation: 1, Version: 1, Kind: q.Kind, Where: q.Title, WaitingPrincipal: q.Responsible, ExpectedInput: q.ExpectedAnswer, Conditions: conditions, Mode: q.Mode, State: "waiting", CreatedBy: a.Key(), CreatedAt: at, UpdatedAt: at}
	if b.WaitingPrincipal == "" {
		b.State = "needs_owner"
	}
	b.Fingerprint = blockerFingerprint(b)
	if err = insertBlocker(ctx, tx, b); err != nil {
		return b, err
	}
	// Preserve a pre-D2 blocked reason. Never silently consume another cause.
	if status == "blocked" && strings.TrimSpace(old) != "" && strings.TrimSpace(old) != b.Where {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_task_blocker WHERE task_id=? AND id<>?`, c.TaskID, b.ID).Scan(&n); err != nil {
			return b, err
		}
		if n == 0 {
			legacy := b
			legacy.ID = "legacy:" + blockerHash([]string{c.TaskID, old})
			legacy.Where = old
			legacy.Kind = "technical"
			legacy.WaitingPrincipal = ""
			legacy.ExpectedInput = "旧阻塞仍未解除：请所有者补入所等人及真实材料"
			legacy.State = "needs_owner"
			legacy.Fingerprint = blockerHash([]string{c.TaskID, "legacy", old})
			if err = insertBlocker(ctx, tx, legacy); err != nil {
				return b, err
			}
		}
	}
	if status == "in_progress" {
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET status='blocked',blocker=? WHERE id=?`, b.Where, c.TaskID); err != nil {
			return b, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, c.TaskID); err != nil {
		return b, err
	}
	if err = blockerAudit(ctx, tx, b, a.Key(), "question_create", map[string]any{"question_id": q.ID}, at); err != nil {
		return b, err
	}
	if err = flowAudit(ctx, tx, c.TaskID, a.Key(), "question_blocker_create", c.Task.Version+1, now); err != nil {
		return b, err
	}
	return b, nil
}
func questionBlockerInput(ctx context.Context, tx *sql.Tx, c QuestionContext, a BlockerActor, q PilotQuestion, event, body, evidence string, now time.Time) error {
	b, err := scanBlocker(tx.QueryRowContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE id=? AND task_id=?`, q.BlockerID, c.TaskID))
	if err != nil {
		return err
	}
	if c.Task == nil || !blockerCurrent(b, *c.Task) || b.SpecID != c.SpecID || b.Revision != c.Revision || b.SHA != c.SHA {
		return ErrTaskFlowConflict
	}
	at := now.UTC().Format(time.RFC3339Nano)
	changed, err := appendBlockerInput(ctx, tx, &b, a.Key(), "question:"+q.ID, blockerHash([]string{a.Key(), event}), body, evidence, q.Mode == "full", at)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err = saveBlocker(ctx, tx, b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, c.TaskID); err != nil {
		return err
	}
	if err = blockerAudit(ctx, tx, b, a.Key(), "question_answer_input", map[string]any{"question_id": q.ID, "event_id": event, "evidence_ref": evidence}, at); err != nil {
		return err
	}
	return flowAudit(ctx, tx, c.TaskID, a.Key(), "question_answer_input", c.Task.Version+1, now)
}
func questionCancelBlocker(ctx context.Context, tx *sql.Tx, c QuestionContext, a BlockerActor, q PilotQuestion, reason string, now time.Time) error {
	b, err := scanBlocker(tx.QueryRowContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE id=? AND task_id=?`, q.BlockerID, c.TaskID))
	if err != nil {
		return err
	}
	if b.State == "resolved" || b.State == "cancelled" {
		return nil
	}
	b.State = "cancelled"
	b.Version++
	b.LeaseID = ""
	b.LeaseUntil = ""
	b.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	if err = saveBlocker(ctx, tx, b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, c.TaskID); err != nil {
		return err
	}
	return blockerAudit(ctx, tx, b, a.Key(), "question_cancel", map[string]any{"question_id": q.ID, "reason": reason}, b.UpdatedAt)
}
