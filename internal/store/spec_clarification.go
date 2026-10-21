package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// clarificationAnchors uses only structured criterion/case/template IDs. Legacy
// documents without any IDs use immutable SHA + line positions, never a rewrite
// of final Markdown. Duplicate IDs are deliberately ambiguous and rejected.
func clarificationAnchors(ctx context.Context, tx *sql.Tx, q PilotQuestion, persist bool) (map[string]int, error) {
	var body, raw, sha string
	if err := tx.QueryRowContext(ctx, `SELECT content_markdown,acceptance_json,content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=?`, q.SpecID, q.Revision).Scan(&body, &raw, &sha); err != nil {
		return nil, err
	}
	if sha != q.SHA {
		return nil, ErrTaskFlowConflict
	}
	refs := map[string]int{}
	for _, c := range decodeConsoleTaskCriteria(raw) {
		if c.ID != "" {
			refs[c.ID]++
		}
		for _, name := range c.Cases {
			if c.ID != "" && strings.TrimSpace(name) != "" {
				refs[c.ID+"/"+name]++
			}
		}
	}
	var doc string
	err := tx.QueryRowContext(ctx, `SELECT r.document_json FROM pilot_template_binding b JOIN pilot_template_revision r ON r.draft_id=b.draft_id AND r.version=b.draft_version WHERE b.spec_id=? AND b.revision=? AND b.content_sha256=?`, q.SpecID, q.Revision, q.SHA).Scan(&doc)
	if err == nil {
		var d TemplateDocument
		if err = json.Unmarshal([]byte(doc), &d); err != nil {
			return nil, err
		}
		for _, e := range d.Entries {
			if e.ID != "" { // The template and acceptance may name the same entry.
				if refs[e.ID] == 0 {
					refs[e.ID] = 1
				}
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if len(refs) == 0 {
		for i, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			id := fmt.Sprintf("snapshot:%s:line:%d", q.SHA, i+1)
			refs[id] = 1
			if persist {
				if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_anchor(spec_id,revision,content_sha256,anchor_id,line_number) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, q.SpecID, q.Revision, q.SHA, id, i+1); err != nil {
					return nil, err
				}
			}
		}
	}
	return refs, nil
}

func clarificationRole(a BlockerActor, p ClarificationPolicy) string {
	if a.AgentID != "" || a.UserID == "" {
		return "technical"
	}
	if p.PMUser != "" && a.UserID == p.PMUser {
		return "pm"
	}
	if p.DeputyUser != "" && a.UserID == p.DeputyUser {
		return "deputy"
	}
	return "technical"
}

const clarificationPostCols = `p.spec_id,p.floor,p.revision,p.content_sha256,p.question_id,p.answer_actor,p.answer_event,p.role,p.kind,p.entry_refs_json,a.body,a.evidence_ref,p.corrects_floor,p.confirms_floor,p.reason,p.source,p.previous_floor,p.policy_version,p.created_at`

func readClarificationPost(ctx context.Context, db taskQuery, spec string, floor int64) (ClarificationPost, error) {
	var p ClarificationPost
	var refs string
	err := db.QueryRowContext(ctx, `SELECT `+clarificationPostCols+` FROM spec_clarification_post p JOIN pilot_question_answer a ON a.question_id=p.question_id AND a.actor_id=p.answer_actor AND a.event_id=p.answer_event WHERE p.spec_id=? AND p.floor=?`, spec, floor).Scan(&p.SpecID, &p.Floor, &p.Revision, &p.SHA, &p.QuestionID, &p.Actor, &p.EventID, &p.Role, &p.Kind, &refs, &p.Body, &p.Evidence, &p.Corrects, &p.Confirms, &p.Reason, &p.Source, &p.PreviousFloor, &p.PolicyVersion, &p.CreatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(refs), &p.EntryRefs)
	}
	if err == nil {
		p.Images, err = clarificationPostImages(ctx, db, spec, floor)
	}
	return p, err
}

// AppendClarification is the Console/Agent entry. Transport provenance is never
// a field of u; the authenticated Feishu adapter uses a separate internal path.
// The caller holds PG identity/member locks until this SQLite commit completes.
func (s *SQLite) AppendClarification(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, u ClarificationUpdate, enabled bool, check func() error, now time.Time) (ClarificationResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClarificationResult{}, err
	}
	defer tx.Rollback()
	out, err := s.appendClarificationTx(ctx, tx, expected, a, qid, u, enabled, check, now, clarificationOrigin{})
	if err != nil {
		return ClarificationResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ClarificationResult{}, err
	}
	return out, nil
}

type clarificationOrigin struct{ Source, Parent string }

func (s *SQLite) appendClarificationTx(ctx context.Context, tx *sql.Tx, expected QuestionContext, a BlockerActor, qid string, u ClarificationUpdate, enabled bool, check func() error, now time.Time, origin clarificationOrigin) (ClarificationResult, error) {
	var out ClarificationResult
	if !enabled {
		return out, ErrTaskFlowPermission
	}
	if (a.UserID == "") == (a.AgentID == "") || check == nil || !questionNonempty(u.RequestID, 128) || !questionNonempty(u.Body, 8192) || len(u.Evidence) > 2048 || len(u.Reason) > 2048 || len(u.EntryRefs) < 1 || len(u.EntryRefs) > 32 || u.Corrects < 0 || u.Confirms < 0 || len(u.Files) > 4 {
		return out, ErrTaskFlowInvalid
	}
	u.EntryRefs = append([]string{}, u.EntryRefs...)
	sort.Strings(u.EntryRefs)
	for i, r := range u.EntryRefs {
		if !questionNonempty(r, 512) || i > 0 && r == u.EntryRefs[i-1] {
			return out, ErrTaskFlowInvalid
		}
	}
	if u.Evidence == "" {
		u.Evidence = "未提供证据"
	}
	c, err := questionContext(ctx, tx, "", "", qid)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowConflict
	}
	if c.Archived {
		return out, ErrTaskFlowPermission
	}
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if err != nil {
		return out, err
	}
	p, err := readClarificationPolicy(ctx, tx, q.SpecID)
	if err != nil {
		return out, err
	}
	role := clarificationRole(a, p)
	// PM/deputy receive only this thread's grant. Technical contributors must
	// already be current readers; an Agent always remains non-authoritative.
	if role == "technical" && !clarificationReader(a, c, q, p) {
		return out, ErrTaskFlowPermission
	}
	if a.AgentID != "" && (c.Task == nil || !questionCurrent(q, c)) {
		return out, ErrTaskFlowPermission
	}
	if role == "technical" && (u.Kind != "technical" || u.Corrects != 0 || u.Confirms != 0) {
		return out, ErrTaskFlowPermission
	}
	// A human PM may also contribute technical material. That explicit choice
	// never turns into an authoritative answer merely because of their account.
	if u.Kind == "technical" {
		role = "technical"
	}
	switch u.Kind {
	case "technical", "explanation", "scope_change":
		if u.Corrects != 0 || u.Confirms != 0 {
			return out, ErrTaskFlowInvalid
		}
	case "correction":
		if role == "technical" || u.Corrects == 0 || u.Confirms != 0 || !questionNonempty(u.Reason, 2048) {
			return out, ErrTaskFlowPermission
		}
	case "confirmation":
		if role != "pm" || u.Confirms == 0 || u.Corrects != 0 {
			return out, ErrTaskFlowPermission
		}
	default:
		return out, ErrTaskFlowInvalid
	}
	// Replay remains subject to current authorization and payload equality. It
	// precedes rate limits; a new request ID with identical text is a NEW post.
	hash := blockerHash([]any{qid, u, origin})
	var previousHash, spec string
	var floor int64
	err = tx.QueryRowContext(ctx, `SELECT payload_sha,spec_id,floor FROM spec_clarification_request WHERE actor_id=? AND request_id=?`, a.Key(), u.RequestID).Scan(&previousHash, &spec, &floor)
	if err == nil {
		if hash != previousHash {
			return out, ErrTaskFlowConflict
		}
		out.Post, err = readClarificationPost(ctx, tx, spec, floor)
		if err != nil {
			return out, err
		}
		if err = check(); err != nil {
			return out, err
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	files := map[string]bool{}
	for _, id := range u.Files {
		if !clarificationDigest(id) || files[id] {
			return out, ErrTaskFlowInvalid
		}
		files[id] = true
		var owner, question string
		var linked bool
		if err = tx.QueryRowContext(ctx, `SELECT actor_id,question_id,EXISTS(SELECT 1 FROM spec_clarification_post_image p WHERE p.file_id=i.file_id) FROM spec_clarification_image i WHERE file_id=?`, id).Scan(&owner, &question, &linked); err != nil {
			return out, ErrTaskFlowInvalid
		}
		if owner != a.Key() || question != qid || linked {
			return out, ErrTaskFlowPermission
		}
	}
	started, e := time.Parse(time.RFC3339Nano, q.GenerationStartedAt)
	if e != nil || now.Before(started) {
		return out, ErrTaskFlowConflict
	}
	anchors, err := clarificationAnchors(ctx, tx, q, true)
	if err != nil {
		return out, err
	}
	for _, r := range u.EntryRefs {
		if anchors[r] != 1 {
			return out, ErrTaskFlowInvalid
		}
	}
	for _, f := range []int64{u.Corrects, u.Confirms} {
		if f == 0 {
			continue
		}
		old, e := readClarificationPost(ctx, tx, q.SpecID, f)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return out, ErrTaskFlowInvalid
			}
			return out, e
		}
		if old.Revision != q.Revision || old.SHA != q.SHA || u.Confirms != 0 && old.Role != "deputy" {
			return out, ErrTaskFlowConflict
		}
	}
	at := now.UTC().Format(time.RFC3339Nano)
	var total, recent int
	if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(CASE WHEN julianday(created_at)>=julianday(?)-1.0/1440 THEN 1 ELSE 0 END),0) FROM spec_clarification_post WHERE question_id=? AND answer_actor=?`, at, qid, a.Key()).Scan(&total, &recent); err != nil {
		return out, err
	}
	if total >= blockerReplyTotal || recent >= blockerReplyPerMinute {
		return out, ErrBlockerRateLimited
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_floor(spec_id,next_floor) VALUES(?,1) ON CONFLICT DO NOTHING`, q.SpecID); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE spec_clarification_floor SET next_floor=next_floor+1 WHERE spec_id=? RETURNING next_floor-1`, q.SpecID).Scan(&floor); err != nil {
		return out, err
	}
	source := "console"
	if a.AgentID != "" {
		source = "agent"
	}
	if origin.Source != "" {
		if origin.Source != "feishu" || role != "pm" {
			return out, ErrTaskFlowPermission
		}
		source = "feishu"
	}
	var previousFloor int64
	if origin.Parent != "" {
		if source != "feishu" {
			return out, ErrTaskFlowPermission
		}
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(floor),0) FROM spec_clarification_post WHERE spec_id=? AND parent_key=? AND answer_actor=?`, q.SpecID, origin.Parent, a.Key()).Scan(&previousFloor); err != nil {
			return out, err
		}
	}
	event := "clarification:" + blockerHash([]string{a.Key(), u.RequestID})
	answerKind := "substantive"
	if role == "technical" {
		answerKind = "technical_input"
	}
	// Include the logical event in new-answer hashes so the legacy content
	// uniqueness index remains intact yet equal-text supplements are not lost.
	answerHash := blockerHash([]any{hash, event})
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_answer(question_id,generation,actor_id,event_id,payload_sha,answer_kind,body,evidence_ref,scope_change,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, qid, q.Generation, a.Key(), event, answerHash, answerKind, u.Body, u.Evidence, u.Kind == "scope_change", at); err != nil {
		return out, err
	}
	refs, _ := json.Marshal(u.EntryRefs)
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_post(spec_id,floor,revision,content_sha256,question_id,answer_actor,answer_event,role,kind,entry_refs_json,corrects_floor,confirms_floor,reason,source,parent_key,previous_floor,policy_version,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, q.SpecID, floor, q.Revision, q.SHA, qid, a.Key(), event, role, u.Kind, string(refs), u.Corrects, u.Confirms, u.Reason, source, origin.Parent, previousFloor, p.Version, at); err != nil {
		return out, err
	}
	for _, id := range u.Files {
		if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_post_image(spec_id,floor,file_id) VALUES(?,?,?)`, q.SpecID, floor, id); err != nil {
			return out, err
		}
	}
	if u.Kind == "scope_change" {
		id := "qchange:" + blockerHash([]string{qid, a.Key(), event})
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_question_change(id,question_id,generation,answer_actor,answer_event,spec_id,revision,content_sha256,state,created_at) VALUES(?,?,?,?,?,?,?,?,'pending',?)`, id, qid, q.Generation, a.Key(), event, q.SpecID, q.Revision, q.SHA, at); err != nil {
			return out, err
		}
	}
	// Reuse exactly the D2 append/wake path, never create a parallel wake or
	// reopen a completed/resolved task just because its discussion continued.
	if !questionAcknowledgment(u.Body) && q.BlockerID != "" && c.Task != nil && questionCurrent(q, c) {
		var bstate, tstate string
		if err = tx.QueryRowContext(ctx, `SELECT b.state,t.status FROM console_task_blocker b JOIN console_spec_task t ON t.id=b.task_id WHERE b.id=?`, q.BlockerID).Scan(&bstate, &tstate); err != nil {
			return out, err
		}
		if !blockerTerminal(bstate) && tstate != "done" {
			if err = questionBlockerInput(ctx, tx, c, a, q, event, u.Body, u.Evidence, now); err != nil {
				return out, err
			}
		}
	}
	if (u.Kind == "correction" || u.Kind == "scope_change") && c.Task != nil && questionCurrent(q, c) {
		event := fmt.Sprintf("clarification_%s/%s/%d", u.Kind, q.ID, floor)
		for _, r := range []struct{ agent, role string }{{c.Task.Developer, "developer"}, {c.Task.Reviewer, "reviewer"}, {"owner:" + c.Owner, "owner"}} {
			if err = flowOutbox(ctx, tx, *c.Task, r.agent, r.role, event, now); err != nil {
				return out, err
			}
		}
	}
	if role != "technical" && !questionAcknowledgment(u.Body) && q.FirstSubstantiveAt == "" {
		q.FirstSubstantiveAt = at
	}
	q.AnsweredAt = at
	q.Version++
	if !questionAcknowledgment(u.Body) && !questionTerminal(q.State) {
		q.State = "answered_pending_recheck"
	}
	if err = saveQuestion(ctx, tx, q); err != nil {
		return out, err
	}
	if err = questionAudit(ctx, tx, qid, a.Key(), "clarification_append", q.Version, map[string]any{"floor": floor, "role": role, "kind": u.Kind, "policy_version": p.Version}, at); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_request(actor_id,request_id,payload_sha,spec_id,floor,created_at) VALUES(?,?,?,?,?,?)`, a.Key(), u.RequestID, hash, q.SpecID, floor, at); err != nil {
		return out, err
	}
	out.Post, err = readClarificationPost(ctx, tx, q.SpecID, floor)
	if err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	return out, nil
}
