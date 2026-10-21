package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"
)

func questionPresentation(q PilotQuestion, c QuestionContext, a BlockerActor, current PilotQuestion) PilotQuestion {
	q.CanAnswer = questionAnswerer(a, c, current)
	q.CanManage = questionManager(a, c)
	q.CanConfigureRecipients = a.UserID != "" && a.UserID == c.Owner
	q.CanAssign = q.CanConfigureRecipients || a.UserID != "" && a.UserID == current.PMUser
	q.CanDecideChange = q.CanConfigureRecipients
	q.AssignmentOptions = []string{"user:" + c.Owner, "user:" + q.PMUser}
	if c.Editor != "" {
		q.AssignmentOptions = append(q.AssignmentOptions, "user:"+c.Editor)
	}
	if c.Task != nil {
		q.AssignmentOptions = append(q.AssignmentOptions, "agent:"+c.Task.Developer, "agent:"+c.Task.Reviewer)
	}
	if !q.CanManage {
		q.LeaderUser = ""
		for i := range q.Notifications {
			if q.Notifications[i].Recipient != a.UserID {
				q.Notifications[i].Recipient = ""
			}
		}
	}
	return q
}
func (s *SQLite) QuestionCandidateIDs(ctx context.Context, a BlockerActor, spec, after string, limit int) ([]string, error) {
	if a.Key() == "" || limit < 1 || limit > 101 {
		return nil, ErrTaskFlowInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT q.id FROM pilot_question q JOIN console_spec s ON s.id=q.spec_id LEFT JOIN console_spec_task t ON t.id=q.task_id WHERE q.id>? AND (?='' OR q.spec_id=?) AND ((?<>'' AND (s.principal_id=? OR s.current_editor_id=? OR q.pm_user=?)) OR q.responsible_principal=? OR (?<>'' AND (t.developer_agent_id=? OR t.reviewer_agent_id=?))) ORDER BY q.id LIMIT ?`, after, spec, spec, a.UserID, a.UserID, a.UserID, a.UserID, a.Key(), a.AgentID, a.AgentID, a.AgentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *SQLite) ReadQuestion(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, before int64, check func() error, changeBefore ...int64) (PilotQuestion, []QuestionAnswer, []QuestionChange, error) {
	cb := int64(0)
	if len(changeBefore) > 0 {
		cb = changeBefore[0]
	}
	return s.readQuestion(ctx, expected, a, qid, before, cb, check, true)
}
func (s *SQLite) ReadQuestionSummary(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, check func() error) (PilotQuestion, error) {
	q, _, _, err := s.readQuestion(ctx, expected, a, qid, 0, 0, check, false)
	return q, err
}
func (s *SQLite) readQuestion(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, before, changeBefore int64, check func() error, includeHistory bool) (PilotQuestion, []QuestionAnswer, []QuestionChange, error) {
	var q PilotQuestion
	if a.Key() == "" || qid == "" || before < 0 || changeBefore < 0 || check == nil {
		return q, nil, nil, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return q, nil, nil, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, "", "", qid)
	if err != nil {
		return q, nil, nil, err
	}
	if !reflect.DeepEqual(c, expected) {
		return q, nil, nil, ErrTaskFlowConflict
	}
	q, err = scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if err != nil {
		return q, nil, nil, err
	}
	if c.Archived || !questionReader(a, c, q) || (a.AgentID != "" && !questionCurrent(q, c)) {
		return q, nil, nil, ErrTaskFlowPermission
	}
	answers := []QuestionAnswer{}
	changes := []QuestionChange{}
	if includeHistory {
		rows, err := tx.QueryContext(ctx, `SELECT rowid,generation,actor_id,event_id,answer_kind,body,evidence_ref,scope_change,created_at FROM pilot_question_answer WHERE question_id=? AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT 101`, qid, before, before)
		if err != nil {
			return q, nil, nil, err
		}
		answers = []QuestionAnswer{}
		for rows.Next() {
			var v QuestionAnswer
			if err = rows.Scan(&v.Sequence, &v.Generation, &v.Actor, &v.EventID, &v.Kind, &v.Body, &v.Evidence, &v.ScopeChange, &v.CreatedAt); err != nil {
				rows.Close()
				return q, nil, nil, err
			}
			answers = append(answers, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return q, nil, nil, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT rowid,id,state,revision,new_revision,new_spec_id,reason FROM pilot_question_change WHERE question_id=? AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT 101`, qid, changeBefore, changeBefore)
		if err != nil {
			return q, nil, nil, err
		}
		changes = []QuestionChange{}
		for rows.Next() {
			var v QuestionChange
			if err = rows.Scan(&v.Sequence, &v.ID, &v.State, &v.Revision, &v.NewRevision, &v.NewSpecID, &v.Reason); err != nil {
				rows.Close()
				return q, nil, nil, err
			}
			changes = append(changes, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return q, nil, nil, err
		}

	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,state,reason,recipient_user,COALESCE(attempted_at,''),COALESCE(finished_at,'') FROM pilot_question_outbox WHERE question_id=? AND generation=? ORDER BY id LIMIT 100`, qid, q.Generation)
	if err != nil {
		return q, nil, nil, err
	}
	for rows.Next() {
		var d QuestionDelivery
		if err = rows.Scan(&d.Kind, &d.State, &d.Reason, &d.Recipient, &d.AttemptedAt, &d.FinishedAt); err != nil {
			rows.Close()
			return q, nil, nil, err
		}
		q.Notifications = append(q.Notifications, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return q, nil, nil, err
	}
	q.BindingCurrent = questionCurrent(q, c)
	if !q.BindingCurrent {
		q.RecheckState = "scope_changed"
	} else if q.BlockerID != "" {
		if err = tx.QueryRowContext(ctx, `SELECT state FROM console_task_blocker WHERE id=? AND task_id=?`, q.BlockerID, q.TaskID).Scan(&q.RecheckState); err != nil {
			return q, nil, nil, err
		}
	} else if q.TaskID == "" {
		q.RecheckState = "spec_question_no_automatic_task"
	} else {
		q.RecheckState = "completed_task_feedback_no_wake"
	}
	if q.TaskID != "" && q.BindingCurrent {
		if err = tx.QueryRowContext(ctx, `SELECT status FROM console_spec_task WHERE id=?`, q.TaskID).Scan(&q.TaskStatus); err != nil {
			return q, nil, nil, err
		}
	}
	if err = check(); err != nil {
		return q, nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return q, nil, nil, err
	}
	return questionPresentation(q, c, a, q), answers, changes, nil
}
func (s *SQLite) OpenQuestionIDs(ctx context.Context, after string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM pilot_question WHERE id>? AND state NOT IN('resolved','cancelled','superseded') AND first_substantive_answer_at IS NULL ORDER BY id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *SQLite) QueueQuestionReminder(ctx context.Context, expected QuestionContext, qid string, check func() error, now time.Time) error {
	if check == nil {
		return ErrTaskFlowPermission
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, "", "", qid)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(c, expected) {
		return ErrTaskFlowConflict
	}
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if err != nil {
		return err
	}
	if !questionCurrent(q, c) || questionTerminal(q.State) || q.FirstSubstantiveAt != "" {
		return nil
	}
	due, e := time.Parse(time.RFC3339Nano, q.DueAt)
	if e != nil {
		return e
	}
	if now.Before(due) {
		return nil
	}
	// The partial unique index is per question/generation, regardless of target.
	if err = questionNotice(ctx, tx, q, "remind_60m", q.LeaderUser, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	return tx.Commit()
}

type QuestionNotice struct {
	ID, QuestionID, Kind, Recipient, State, Reason string
	Generation, PolicyVersion, EventVersion        int
}

func (s *SQLite) PendingQuestionNotices(ctx context.Context, after string) ([]QuestionNotice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,question_id,kind,recipient_user,state,reason,generation,policy_version,event_version FROM pilot_question_outbox WHERE id>? AND state IN('queued','pending_config','dispatching') ORDER BY id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QuestionNotice{}
	for rows.Next() {
		var n QuestionNotice
		if err = rows.Scan(&n.ID, &n.QuestionID, &n.Kind, &n.Recipient, &n.State, &n.Reason, &n.Generation, &n.PolicyVersion, &n.EventVersion); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// QuestionFeishuDisabledReason is persisted only for a not-attempted notice.
const QuestionFeishuDisabledReason = "DW_PILOT_QUESTION_FEISHU_ENABLED_disabled"

func (s *SQLite) PrepareQuestionNotice(ctx context.Context, expected QuestionContext, n QuestionNotice, configured bool, check func() error, now time.Time, unconfiguredReason ...string) (PilotQuestion, bool, error) {
	var q PilotQuestion
	if check == nil {
		return q, false, ErrTaskFlowPermission
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return q, false, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, "", "", n.QuestionID)
	if err != nil {
		return q, false, err
	}
	if !reflect.DeepEqual(c, expected) {
		return q, false, ErrTaskFlowConflict
	}
	q, err = scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, n.QuestionID))
	if err != nil {
		return q, false, err
	}
	var state string
	var recipient string
	if err = tx.QueryRowContext(ctx, `SELECT state,recipient_user FROM pilot_question_outbox WHERE id=? AND question_id=? AND generation=?`, n.ID, n.QuestionID, n.Generation).Scan(&state, &recipient); err != nil {
		return q, false, err
	}
	if recipient != n.Recipient {
		return q, false, ErrTaskFlowConflict
	}
	if state == "sent" || state == "unknown" || state == "cancelled" {
		return q, false, nil
	}
	at := now.UTC().Format(time.RFC3339Nano)
	next, reason := "dispatching", ""
	if state == "dispatching" {
		next = "unknown"
		reason = "dispatch_receipt_unknown_no_retry"
	} else if !questionCurrent(q, c) || questionTerminal(q.State) || q.Generation != n.Generation {
		next = "cancelled"
		reason = "question_superseded"
	} else if n.Kind == "remind_60m" {
		due, e := time.Parse(time.RFC3339Nano, q.DueAt)
		if e != nil {
			return q, false, e
		}
		if q.FirstSubstantiveAt != "" {
			next = "cancelled"
			reason = "answer_committed_before_reminder"
		} else if now.Before(due) {
			return q, false, nil
		} else if recipient != q.LeaderUser {
			next = "cancelled"
			reason = "recipient_reassigned"
		}
	} else if n.Kind == "question" && q.FirstSubstantiveAt != "" {
		next = "cancelled"
		reason = "answered_before_question_notice"
	} else if recipient != q.PMUser && "user:"+recipient != q.Responsible {
		next = "cancelled"
		reason = "recipient_reassigned"
	}
	if next == "dispatching" && (!configured || recipient == "") {
		next = "pending_config"
		reason = "binding_or_transport_unconfigured"
		if len(unconfiguredReason) == 1 && unconfiguredReason[0] == QuestionFeishuDisabledReason {
			reason = QuestionFeishuDisabledReason
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pilot_question_outbox SET state=?,reason=?,attempted_at=CASE WHEN ?='dispatching' THEN ? ELSE attempted_at END,finished_at=CASE WHEN ? IN('unknown','cancelled') THEN ? ELSE finished_at END WHERE id=?`, next, reason, next, at, next, at, n.ID); err != nil {
		return q, false, err
	}
	if err = check(); err != nil {
		return q, false, err
	}
	if err = tx.Commit(); err != nil {
		return q, false, err
	}
	return q, next == "dispatching", nil
}
func (s *SQLite) FinishQuestionNotice(ctx context.Context, id, providerID string, sent bool, now time.Time) error {
	state, reason := "unknown", "provider_outcome_unknown_no_retry"
	if sent && providerID != "" {
		state = "sent"
		reason = "provider_accepted_not_human_read"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE pilot_question_outbox SET state=?,reason=?,finished_at=?,provider_message_id=? WHERE id=? AND state='dispatching'`, state, reason, now.UTC().Format(time.RFC3339Nano), providerID, id)
	return err
}

// A raw reference accessor for the trusted worker, never an HTTP response.
func (s *SQLite) QuestionReferences(ctx context.Context, qid string) (PilotQuestion, error) {
	q, err := scanQuestion(s.db.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrTaskFlowPermission
	}
	return q, err
}

// Only the trusted worker calls this when it knows the provider was NOT called.
// CAS refuses sent/unknown/cancelled rows and old generation/recipient snapshots.
func (s *SQLite) DeferUnsentQuestionNotice(ctx context.Context, n QuestionNotice, state string, preparedHere bool, now time.Time) error {
	if state != "queued" && state != "pending_config" && state != "cancelled" {
		return ErrTaskFlowInvalid
	}
	_, err := s.db.ExecContext(ctx, `UPDATE pilot_question_outbox SET state=CASE WHEN state='dispatching' AND NOT ? THEN 'unknown' ELSE ? END,reason=CASE WHEN state='dispatching' AND NOT ? THEN 'dispatch_receipt_unknown_no_retry' ELSE 'provider_not_attempted_authorization_recheck' END,attempted_at=CASE WHEN state='dispatching' AND NOT ? THEN attempted_at ELSE NULL END,finished_at=CASE WHEN (?='cancelled' OR (state='dispatching' AND NOT ?)) THEN ? ELSE NULL END WHERE id=? AND question_id=? AND generation=? AND recipient_user=? AND state IN('queued','pending_config','dispatching')`, preparedHere, state, preparedHere, preparedHere, state, preparedHere, now.UTC().Format(time.RFC3339Nano), n.ID, n.QuestionID, n.Generation, n.Recipient)
	return err
}
