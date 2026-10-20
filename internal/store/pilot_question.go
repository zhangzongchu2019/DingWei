package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

func questionContext(ctx context.Context, db taskQuery, specID, taskID, qid string) (QuestionContext, error) {
	c := QuestionContext{ID: qid, SpecID: specID, TaskID: taskID}
	if qid != "" {
		if err := db.QueryRowContext(ctx, `SELECT spec_id,task_id FROM pilot_question WHERE id=?`, qid).Scan(&c.SpecID, &c.TaskID); err != nil {
			return c, ErrTaskFlowPermission
		}
	}
	if c.TaskID != "" {
		t, err := taskFlowState(ctx, db, c.TaskID)
		if err != nil {
			return c, err
		}
		c.Task = &t
		var actual string
		if err = db.QueryRowContext(ctx, `SELECT spec_id,revision,content_sha256 FROM console_spec_task WHERE id=?`, c.TaskID).Scan(&actual, &c.Revision, &c.SHA); err != nil {
			return c, err
		}
		if c.SpecID != "" && c.SpecID != actual {
			return c, ErrTaskFlowInvalid
		}
		c.SpecID = actual
	}
	var current int
	err := db.QueryRowContext(ctx, `SELECT principal_id,COALESCE(current_editor_id,''),current_revision,archived FROM console_spec WHERE id=?`, c.SpecID).Scan(&c.Owner, &c.Editor, &current, &c.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrTaskFlowPermission
	}
	if err != nil {
		return c, err
	}
	if c.Task == nil {
		c.Revision = current
		if err = db.QueryRowContext(ctx, `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=?`, c.SpecID, current).Scan(&c.SHA); err != nil {
			return c, err
		}
	}
	return c, nil
}
func (s *SQLite) QuestionAuthorizationContext(ctx context.Context, spec, task, qid string) (QuestionContext, error) {
	return questionContext(ctx, s.db, spec, task, qid)
}
func readQuestionPolicy(ctx context.Context, db taskQuery, spec string) (QuestionPolicy, error) {
	p := QuestionPolicy{SpecID: spec}
	err := db.QueryRowContext(ctx, `SELECT pm_user,leader_user,version,updated_by,updated_at FROM pilot_question_policy WHERE spec_id=?`, spec).Scan(&p.PMUser, &p.LeaderUser, &p.Version, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}
func (s *SQLite) QuestionPolicy(ctx context.Context, spec string) (QuestionPolicy, error) {
	return readQuestionPolicy(ctx, s.db, spec)
}
func (s *SQLite) SetQuestionPolicy(ctx context.Context, expected QuestionContext, a BlockerActor, u QuestionUpdate, check func() error, now time.Time) (QuestionPolicy, error) {
	u = normalizeQuestionRecipients(u)
	var out QuestionPolicy
	if a.UserID == "" || check == nil || u.RequestID == "" || !validQuestionUser(u.PMUser) || !validQuestionUser(u.LeaderUser) {
		return out, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, expected.SpecID, "", "")
	if err != nil {
		return out, err
	}
	// Policies create limited grants: only the actual spec owner may configure.
	if c.Owner != a.UserID || c.Archived || !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowPermission
	}
	payload := blockerHash([]any{"policy", c.SpecID, u})
	var oldHash, raw string
	e := tx.QueryRowContext(ctx, `SELECT payload_sha,result_json FROM pilot_question_request WHERE actor_id=? AND request_id=?`, a.Key(), u.RequestID).Scan(&oldHash, &raw)
	if e == nil {
		if oldHash != payload {
			return out, ErrTaskFlowConflict
		}
		if err = check(); err != nil {
			return out, err
		}
		err = json.Unmarshal([]byte(raw), &out)
		return out, err
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	old, err := readQuestionPolicy(ctx, tx, c.SpecID)
	if err != nil {
		return out, err
	}
	if old.Version != u.ExpectedVersion {
		return out, ErrTaskFlowConflict
	}
	at := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_policy(spec_id,pm_user,leader_user,version,updated_by,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(spec_id) DO UPDATE SET pm_user=excluded.pm_user,leader_user=excluded.leader_user,version=excluded.version,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, c.SpecID, u.PMUser, u.LeaderUser, old.Version+1, a.Key(), at); err != nil {
		return out, err
	}
	if err = questionAudit(ctx, tx, "", a.Key(), "configure_policy", old.Version+1, map[string]any{"spec_id": c.SpecID, "pm_user": u.PMUser, "leader_user": u.LeaderUser, "applies": "new_questions_only"}, at); err != nil {
		return out, err
	}
	out = QuestionPolicy{SpecID: c.SpecID, PMUser: u.PMUser, LeaderUser: u.LeaderUser, Version: old.Version + 1, UpdatedBy: a.Key(), UpdatedAt: at}
	data, _ := json.Marshal(out)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_request(actor_id,request_id,payload_sha,question_id,result_json,created_at) VALUES(?,?,?,?,?,?)`, a.Key(), u.RequestID, payload, "", string(data), at); err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func questionAudit(ctx context.Context, tx *sql.Tx, id, actor, op string, version int, details any, at string) error {
	b, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, `INSERT INTO pilot_question_audit(question_id,actor_id,operation,version,details_json,created_at) VALUES(?,?,?,?,?,?)`, id, actor, op, version, string(b), at)
	return err
}
func saveQuestion(ctx context.Context, tx *sql.Tx, q PilotQuestion) error {
	_, err := tx.ExecContext(ctx, `UPDATE pilot_question SET generation=?,version=?,pm_user=?,responsible_principal=?,leader_user=?,mode=?,state=?,generation_started_at=?,due_at=?,first_substantive_answer_at=NULLIF(?,''),answered_at=NULLIF(?,''),resolved_at=NULLIF(?,''),policy_version=? WHERE id=?`, q.Generation, q.Version, q.PMUser, q.Responsible, q.LeaderUser, q.Mode, q.State, q.GenerationStartedAt, q.DueAt, q.FirstSubstantiveAt, q.AnsweredAt, q.ResolvedAt, q.PolicyVersion, q.ID)
	return err
}
func questionNotice(ctx context.Context, tx *sql.Tx, q PilotQuestion, kind, user, at string) error {
	eventVersion := 0
	if kind == "assignment" {
		eventVersion = q.Version
	}
	id := "question:" + blockerHash([]any{q.ID, q.Generation, kind, user, eventVersion})
	state, reason := "queued", ""
	if user == "" {
		state = "pending_config"
		reason = "recipient_unconfigured"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO pilot_question_outbox(id,question_id,generation,kind,recipient_user,policy_version,event_version,state,created_at,reason) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, id, q.ID, q.Generation, kind, user, q.PolicyVersion, eventVersion, state, at, reason)
	return err
}
func questionCurrent(q PilotQuestion, c QuestionContext) bool {
	if q.SpecID != c.SpecID || q.Revision != c.Revision || q.SHA != c.SHA || c.Archived {
		return false
	}
	return c.Task == nil || (q.TaskID == c.Task.ID && !c.Task.NeedsReassignment && !c.Task.Archived && q.AssignmentVersion == c.Task.AssignmentVersion)
}
func (s *SQLite) UpdateQuestion(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, u QuestionUpdate, check func() error, now time.Time) (QuestionResult, error) {
	u = normalizeQuestionRecipients(u)
	var out QuestionResult
	if a.Key() == "" || check == nil || !questionNonempty(u.RequestID, 128) || len(u.Title) > 512 || len(u.ObservedFact) > 4096 || len(u.Question) > 4096 || len(u.ExpectedAnswer) > 4096 || len(u.InputAnchors) > 4096 || len(u.Body) > 8192 || len(u.Evidence) > 2048 || len(u.Reason) > 2048 || !validBlockerPrincipal(u.Responsible) {
		return out, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, expected.SpecID, expected.TaskID, qid)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowConflict
	}
	if c.Archived {
		return out, ErrTaskFlowPermission
	}
	var q PilotQuestion
	if qid != "" {
		q, err = scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
		if err != nil {
			return out, err
		}
		if !questionReader(a, c, q) {
			return out, ErrTaskFlowPermission
		}
	}
	if qid == "" {
		if c.Task == nil {
			if !questionManager(a, c) {
				return out, ErrTaskFlowPermission
			}
		} else if !a.taskReader(*c.Task) {
			return out, ErrTaskFlowPermission
		}
	}
	payload := blockerHash([]any{c.SpecID, qid, u})
	var oldHash, raw string
	err = tx.QueryRowContext(ctx, `SELECT payload_sha,result_json FROM pilot_question_request WHERE actor_id=? AND request_id=?`, a.Key(), u.RequestID).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != payload {
			return out, ErrTaskFlowConflict
		}
		if err = check(); err != nil {
			return out, err
		}
		err = json.Unmarshal([]byte(raw), &out)
		out.Replayed = true
		out.Question = questionPresentation(out.Question, c, a, q)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if qid != "" && q.Version != u.ExpectedVersion {
		return out, ErrTaskFlowConflict
	}
	ownerMaintenance := a.UserID == c.Owner && (u.Operation == "cancel" || u.Operation == "reject_change" || u.Operation == "confirm_change")
	if qid != "" && !ownerMaintenance && !questionCurrent(q, c) {
		return out, ErrTaskFlowConflict
	}
	at := now.UTC().Format(time.RFC3339Nano)
	out.Outcome = "updated"
	if qid != "" {
		start, e := time.Parse(time.RFC3339Nano, q.GenerationStartedAt)
		if e != nil || now.Before(start) {
			return out, ErrTaskFlowConflict
		}
	}
	switch u.Operation {
	case "create":
		if qid != "" || !validQuestionKind(u.Kind) || !questionNonempty(u.Title, 512) || !questionNonempty(u.ObservedFact, 4096) || !questionNonempty(u.Question, 4096) || !questionNonempty(u.ExpectedAnswer, 4096) {
			return out, ErrTaskFlowInvalid
		}
		if c.Task == nil {
			if !questionManager(a, c) || u.ExpectedVersion != c.Revision {
				return out, ErrTaskFlowPermission
			}
		} else {
			if !a.taskReader(*c.Task) || u.ExpectedVersion != c.Task.Version || c.Task.NeedsReassignment {
				return out, ErrTaskFlowPermission
			}
		}
		// A developer cannot add an external grant by choosing a recipient. The
		// owner policy supplies PM; only the human owner sets an extra responsible.
		if u.Responsible != "" && a.UserID != c.Owner {
			return out, ErrTaskFlowPermission
		}
		policy, e := readQuestionPolicy(ctx, tx, c.SpecID)
		if e != nil {
			return out, e
		}
		q = PilotQuestion{ID: "pq:" + blockerHash([]string{a.Key(), u.RequestID}), SpecID: c.SpecID, Revision: c.Revision, SHA: c.SHA, TaskID: c.TaskID, Generation: 1, Version: 1, Kind: u.Kind, Title: strings.TrimSpace(u.Title), ObservedFact: strings.TrimSpace(u.ObservedFact), Question: strings.TrimSpace(u.Question), ExpectedAnswer: strings.TrimSpace(u.ExpectedAnswer), InputAnchors: u.InputAnchors, CreatedBy: a.Key(), PMUser: policy.PMUser, Responsible: u.Responsible, LeaderUser: policy.LeaderUser, Mode: "light", State: "awaiting_answer", CreatedAt: at, GenerationStartedAt: at, DueAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano), PolicyVersion: policy.Version}
		if q.Responsible == "" && (q.Kind == "requirement_unclear" || q.Kind == "change") && q.PMUser != "" {
			q.Responsible = "user:" + q.PMUser
		}
		if q.Responsible == "" {
			q.State = "needs_owner"
		}
		if u.ProductionOrSecurity {
			q.Mode = "full"
		}
		if c.Task != nil {
			q.AssignmentVersion = c.Task.AssignmentVersion
			b, e := questionCreateBlocker(ctx, tx, c, a, q, now)
			if e != nil {
				return out, e
			}
			q.BlockerID = b.ID
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pilot_question(`+questionCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, q.ID, q.SpecID, q.Revision, q.SHA, q.TaskID, q.BlockerID, q.AssignmentVersion, q.Generation, q.Version, q.Kind, q.Title, q.ObservedFact, q.Question, q.ExpectedAnswer, q.InputAnchors, q.CreatedBy, q.PMUser, q.Responsible, q.LeaderUser, q.Mode, q.State, q.CreatedAt, q.GenerationStartedAt, q.DueAt, nil, nil, nil, q.PolicyVersion)
		if err != nil {
			return out, err
		}
		if err = questionNotice(ctx, tx, q, "question", q.PMUser, at); err != nil {
			return out, err
		}
		if strings.HasPrefix(q.Responsible, "user:") && q.Responsible != "user:"+q.PMUser {
			if err = questionNotice(ctx, tx, q, "assignment", strings.TrimPrefix(q.Responsible, "user:"), at); err != nil {
				return out, err
			}
		}
	case "answer", "input":
		if questionTerminal(q.State) || !questionNonempty(u.EventID, 128) || !questionNonempty(u.Body, 8192) || !questionNonempty(u.Evidence, 2048) {
			return out, ErrTaskFlowInvalid
		}
		kind := u.AnswerKind
		if u.Operation == "input" {
			if a.AgentID == "" || c.Task == nil || !(a.taskReader(*c.Task) || q.Responsible == a.Key()) {
				return out, ErrTaskFlowPermission
			}
			kind = "technical_input"
			if u.ScopeChange {
				return out, ErrTaskFlowPermission
			}
		} else if !questionAnswerer(a, c, q) || (kind != "ack" && kind != "substantive") {
			return out, ErrTaskFlowPermission
		}
		if kind == "substantive" && questionAcknowledgment(u.Body) {
			kind = "ack"
		}
		if kind == "ack" && u.ScopeChange {
			return out, ErrTaskFlowInvalid
		}
		hash := blockerHash([]any{q.Generation, a.Key(), kind, u.Body, u.Evidence, u.ScopeChange})
		var previous string
		e := tx.QueryRowContext(ctx, `SELECT payload_sha FROM pilot_question_event WHERE question_id=? AND actor_id=? AND event_id=?`, q.ID, a.Key(), u.EventID).Scan(&previous)
		if e == nil {
			if previous != hash {
				return out, ErrTaskFlowConflict
			}
			out.Outcome = "unchanged"
			break
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		canonical := u.EventID
		e = tx.QueryRowContext(ctx, `SELECT event_id FROM pilot_question_answer WHERE question_id=? AND generation=? AND actor_id=? AND payload_sha=?`, q.ID, q.Generation, a.Key(), hash).Scan(&canonical)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_event(question_id,actor_id,event_id,generation,payload_sha,canonical_event_id) VALUES(?,?,?,?,?,?)`, q.ID, a.Key(), u.EventID, q.Generation, hash, canonical); err != nil {
			return out, err
		}
		if e == nil {
			out.Outcome = "unchanged"
			break
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_answer(question_id,generation,actor_id,event_id,payload_sha,answer_kind,body,evidence_ref,scope_change,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, q.ID, q.Generation, a.Key(), u.EventID, hash, kind, u.Body, u.Evidence, u.ScopeChange, at); err != nil {
			return out, err
		}
		if kind != "ack" {
			substantive := kind == "substantive" || (kind == "technical_input" && (q.Kind == "technical" || q.Kind == "resource") && q.Responsible == a.Key() && !questionAcknowledgment(u.Body))
			if substantive && q.FirstSubstantiveAt == "" {
				q.FirstSubstantiveAt = at
			}
			q.AnsweredAt = at
			q.State = "answered_pending_recheck"
			if q.Responsible == "" {
				q.State = "needs_owner"
			}
			if q.BlockerID != "" {
				if err = questionBlockerInput(ctx, tx, c, a, q, u.EventID, u.Body, u.Evidence, now); err != nil {
					return out, err
				}
			}
			if q.TaskID != "" && q.BlockerID == "" {
				if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, q.TaskID); err != nil {
					return out, err
				}
				if err = flowAudit(ctx, tx, q.TaskID, a.Key(), "question_answer_recorded_no_wake", c.Task.Version+1, now); err != nil {
					return out, err
				}
			}
			detail, _ := json.Marshal(map[string]any{"question_id": q.ID, "event_id": u.EventID, "answer_kind": kind, "evidence_ref": u.Evidence})
			if err = appendConsoleSpecEvent(ctx, tx, q.SpecID, q.Revision, a.Key(), "pilot_question_answer", string(detail), now); err != nil {
				return out, err
			}
			if kind == "substantive" && (q.Kind == "change" || u.ScopeChange) {
				id := "qchange:" + blockerHash([]string{q.ID, a.Key(), u.EventID})
				if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_change(id,question_id,generation,answer_actor,answer_event,spec_id,revision,content_sha256,state,created_at) VALUES(?,?,?,?,?,?,?,?,'pending',?)`, id, q.ID, q.Generation, a.Key(), u.EventID, q.SpecID, q.Revision, q.SHA, at); err != nil {
					return out, err
				}
			}
		}
		q.Version++
	case "assign":
		owner := a.UserID != "" && a.UserID == c.Owner
		delegated := a.UserID != "" && a.UserID == q.PMUser && (u.PMUser == "" || u.PMUser == q.PMUser) && (u.LeaderUser == "" || u.LeaderUser == q.LeaderUser) &&
			(u.Responsible == "user:"+c.Owner || c.Editor != "" && u.Responsible == "user:"+c.Editor || u.Responsible == "user:"+q.PMUser || c.Task != nil && (u.Responsible == "agent:"+c.Task.Developer || u.Responsible == "agent:"+c.Task.Reviewer))
		if (!owner && !delegated) || questionTerminal(q.State) || !validQuestionUser(u.PMUser) || !validQuestionUser(u.LeaderUser) {
			return out, ErrTaskFlowPermission
		}
		if !owner {
			u.PMUser = q.PMUser
			u.LeaderUser = q.LeaderUser
		}
		oldPM, oldResponsible := q.PMUser, q.Responsible
		q.PMUser = u.PMUser
		q.Responsible = u.Responsible
		q.LeaderUser = u.LeaderUser
		q.Version++
		if q.BlockerID != "" {
			if err = questionReassignBlocker(ctx, tx, c, a, q, now); err != nil {
				return out, err
			}
		}
		if q.Responsible == "" {
			q.State = "needs_owner"
		} else if q.State == "needs_owner" {
			q.State = "awaiting_answer"
		}
		// Revoke old object grants immediately, and cancel unsent notices to them.
		if _, err = tx.ExecContext(ctx, `UPDATE pilot_question_outbox SET state='cancelled',reason='recipient_reassigned',finished_at=? WHERE question_id=? AND state IN('queued','pending_config') AND kind<>'remind_60m'`, at, q.ID); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pilot_question_outbox SET recipient_user=?,state=CASE WHEN ?='' THEN 'pending_config' ELSE 'queued' END,reason=CASE WHEN ?='' THEN 'recipient_unconfigured' ELSE '' END WHERE question_id=? AND generation=? AND kind='remind_60m' AND state IN('queued','pending_config') AND attempted_at IS NULL`, q.LeaderUser, q.LeaderUser, q.LeaderUser, q.ID, q.Generation); err != nil {
			return out, err
		}
		// Explicit reassignment is a new policy event, not a reset of the SLA clock.
		if oldPM != q.PMUser && q.PMUser != "" {
			if err = questionNotice(ctx, tx, q, "assignment", q.PMUser, at); err != nil {
				return out, err
			}
		}
		if oldResponsible != q.Responsible && strings.HasPrefix(q.Responsible, "user:") && q.Responsible != "user:"+q.PMUser {
			if err = questionNotice(ctx, tx, q, "assignment", strings.TrimPrefix(q.Responsible, "user:"), at); err != nil {
				return out, err
			}
		}
	case "reject_change", "confirm_change":
		if a.UserID != c.Owner || u.ChangeID == "" || !questionNonempty(u.Reason, 2048) {
			return out, ErrTaskFlowPermission
		}
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM pilot_question_change WHERE id=? AND question_id=?`, u.ChangeID, q.ID).Scan(&state); err != nil {
			return out, err
		}
		if state != "pending" {
			return out, ErrTaskFlowConflict
		}
		if u.Operation == "reject_change" {
			if _, err = tx.ExecContext(ctx, `UPDATE pilot_question_change SET state='rejected',decided_by=?,reason=? WHERE id=?`, a.Key(), u.Reason, u.ChangeID); err != nil {
				return out, err
			}
			if q.BlockerID != "" && questionCurrent(q, c) {
				if err = questionBlockerInput(ctx, tx, c, a, q, "reject:"+u.RequestID, "所有者拒绝变更，原范围仍须重核", u.Reason, now); err != nil {
					return out, err
				}
			}
		} else {
			var sha string
			var current int
			if err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=?`, q.SpecID).Scan(&current); err != nil {
				return out, err
			}
			if u.NewRevision <= q.Revision || u.NewRevision != current {
				return out, ErrTaskFlowConflict
			}
			if err = tx.QueryRowContext(ctx, `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=?`, q.SpecID, u.NewRevision).Scan(&sha); err != nil {
				return out, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE pilot_question_change SET state='confirmed_new_revision',decided_by=?,reason=?,new_revision=?,new_content_sha256=? WHERE id=?`, a.Key(), u.Reason, u.NewRevision, sha, u.ChangeID); err != nil {
				return out, err
			}
			q.State = "superseded"
		}
		q.Version++
	case "resolve", "cancel":
		if !questionManager(a, c) || !questionNonempty(u.Reason, 2048) {
			return out, ErrTaskFlowPermission
		}
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pilot_question_change WHERE question_id=? AND state='pending'`, q.ID).Scan(&pending); err != nil {
			return out, err
		}
		if pending != 0 && u.Operation == "resolve" {
			return out, ErrTaskFlowConflict
		}
		if u.Operation == "resolve" {
			if q.FirstSubstantiveAt == "" || q.Responsible == "" {
				return out, ErrTaskFlowConflict
			}
			if q.BlockerID != "" {
				var state string
				if err = tx.QueryRowContext(ctx, `SELECT state FROM console_task_blocker WHERE id=?`, q.BlockerID).Scan(&state); err != nil {
					return out, err
				}
				if state != "resolved" {
					return out, ErrTaskFlowConflict
				}
			}
			q.State = "resolved"
			q.ResolvedAt = at
		} else {
			q.State = "cancelled"
			if q.BlockerID != "" {
				if err = questionCancelBlocker(ctx, tx, c, a, q, u.Reason, now); err != nil {
					return out, err
				}
			}
		}
		q.Version++
	case "reopen":
		if a.UserID != c.Owner || !questionTerminal(q.State) || !questionNonempty(u.Reason, 2048) || q.State == "superseded" {
			return out, ErrTaskFlowPermission
		}
		q.Generation++
		q.Version++
		q.GenerationStartedAt = at
		q.DueAt = now.Add(time.Hour).UTC().Format(time.RFC3339Nano)
		q.FirstSubstantiveAt = ""
		q.AnsweredAt = ""
		q.ResolvedAt = ""
		q.State = "awaiting_answer"
		if q.Responsible == "" {
			q.State = "needs_owner"
		}
		if c.Task != nil {
			b, e := questionCreateBlocker(ctx, tx, c, a, q, now)
			if e != nil {
				return out, e
			}
			q.BlockerID = b.ID
			if _, err = tx.ExecContext(ctx, `UPDATE pilot_question SET blocker_id=? WHERE id=?`, q.BlockerID, q.ID); err != nil {
				return out, err
			}
		}
		if err = questionNotice(ctx, tx, q, "question", q.PMUser, at); err != nil {
			return out, err
		}
	default:
		return out, ErrTaskFlowInvalid
	}
	if err = saveQuestion(ctx, tx, q); err != nil {
		return out, err
	}
	if out.Outcome != "unchanged" {
		if err = questionAudit(ctx, tx, q.ID, a.Key(), u.Operation, q.Version, map[string]any{"event_id": u.EventID, "answer_kind": u.AnswerKind, "scope_change": u.ScopeChange, "reason": u.Reason, "evidence_ref": u.Evidence}, at); err != nil {
			return out, err
		}
	}
	out.Question = questionPresentation(q, c, a, q)
	response, _ := json.Marshal(out)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_request(actor_id,request_id,payload_sha,question_id,result_json,created_at) VALUES(?,?,?,?,?,?)`, a.Key(), u.RequestID, payload, q.ID, string(response), at); err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func questionAcknowledgment(body string) bool {
	s := strings.Trim(strings.TrimSpace(strings.ToLower(body)), "。.!！,， ")
	switch s {
	case "收到", "已收到", "收到谢谢", "知道了", "好的", "ok", "ack", "received", "noted", "thanks", "谢谢":
		return true
	}
	return false
}

func normalizeQuestionRecipients(u QuestionUpdate) QuestionUpdate {
	u.PMUser = strings.ToLower(strings.TrimSpace(u.PMUser))
	u.LeaderUser = strings.ToLower(strings.TrimSpace(u.LeaderUser))
	return u
}
