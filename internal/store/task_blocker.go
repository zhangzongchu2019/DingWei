package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// The principal prefix is set by the authenticated transport, never JSON input.
type BlockerActor struct{ AgentID, UserID string }

func (a BlockerActor) Key() string {
	if a.AgentID != "" && a.UserID == "" {
		return "agent:" + a.AgentID
	}
	if a.UserID != "" && a.AgentID == "" {
		return "user:" + a.UserID
	}
	return ""
}
func (a BlockerActor) owner(t TaskFlowState) bool { return a.UserID != "" && a.UserID == t.Owner }
func (a BlockerActor) developer(t TaskFlowState) bool {
	return a.AgentID != "" && a.AgentID == t.Developer
}
func (a BlockerActor) taskReader(t TaskFlowState) bool {
	return a.owner(t) || a.developer(t) || (a.AgentID != "" && a.AgentID == t.Reviewer)
}

type BlockerCondition struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type BlockerCheck struct {
	ID       string `json:"id"`
	Result   string `json:"result"`
	Evidence string `json:"evidence_ref"`
}
type TaskBlocker struct {
	ID                  string             `json:"id"`
	TaskID              string             `json:"task_id"`
	SpecID              string             `json:"spec_id"`
	Revision            int                `json:"revision"`
	SHA                 string             `json:"content_sha256"`
	AssignmentVersion   int                `json:"assignment_version"`
	Developer           string             `json:"developer_agent_id"`
	Reviewer            string             `json:"reviewer_agent_id"`
	Generation          int                `json:"generation"`
	Version             int                `json:"version"`
	Kind                string             `json:"kind"`
	Where               string             `json:"where"`
	WaitingPrincipal    string             `json:"waiting_principal"`
	ExpectedInput       string             `json:"expected_input"`
	Conditions          []BlockerCondition `json:"resume_recheck"`
	Mode                string             `json:"mode"`
	FullApprovedBy      string             `json:"full_approved_by,omitempty"`
	FullApprovalRef     string             `json:"full_approval_ref,omitempty"`
	State               string             `json:"state"`
	InputVersion        int                `json:"input_version"`
	CheckedInputVersion int                `json:"checked_input_version"`
	LeaseID             string             `json:"lease_id,omitempty"`
	LeaseUntil          string             `json:"lease_until,omitempty"`
	CreatedBy           string             `json:"created_by"`
	CreatedAt           string             `json:"created_at"`
	UpdatedAt           string             `json:"updated_at"`
	Fingerprint         string             `json:"-"`
	WakeState           string             `json:"wake_state,omitempty"`
}
type BlockerInput struct {
	Source       string `json:"source"`
	EventID      string `json:"event_id"`
	InputVersion int    `json:"input_version"`
	Actor        string `json:"actor"`
	Body         string `json:"body"`
	Evidence     string `json:"evidence_ref"`
	CreatedAt    string `json:"created_at"`
}
type BlockerUpdate struct {
	RequestID            string             `json:"request_id"`
	ExpectedVersion      int                `json:"expected_version"`
	Operation            string             `json:"operation"`
	Kind                 string             `json:"kind,omitempty"`
	Where                string             `json:"where,omitempty"`
	WaitingPrincipal     string             `json:"waiting_principal,omitempty"`
	ExpectedInput        string             `json:"expected_input,omitempty"`
	Conditions           []BlockerCondition `json:"resume_recheck,omitempty"`
	ProductionOrSecurity bool               `json:"production_or_security,omitempty"`
	FullApprovalRef      string             `json:"full_approval_ref,omitempty"`
	Source               string             `json:"source,omitempty"`
	EventID              string             `json:"event_id,omitempty"`
	Body                 string             `json:"body,omitempty"`
	Evidence             string             `json:"evidence_ref,omitempty"`
	LeaseID              string             `json:"lease_id,omitempty"`
	InputVersion         int                `json:"input_version,omitempty"`
	Checks               []BlockerCheck     `json:"checks,omitempty"`
}
type BlockerResult struct {
	Blocker             TaskBlocker `json:"blocker"`
	TaskVersion         int         `json:"task_version"`
	Replayed            bool        `json:"replayed"`
	Outcome             string      `json:"outcome"`
	ExecutionAuthorized bool        `json:"execution_authorized"`
}

const blockerCols = `id,task_id,spec_id,revision,content_sha256,assignment_version,developer_agent_id,reviewer_agent_id,generation,version,kind,where_text,waiting_principal,expected_input,conditions_json,mode,full_approved_by,full_approval_ref,state,input_version,checked_input_version,lease_id,lease_until,created_by,created_at,updated_at,fingerprint`

func scanBlocker(row rowScanner) (TaskBlocker, error) {
	var b TaskBlocker
	var raw string
	err := row.Scan(&b.ID, &b.TaskID, &b.SpecID, &b.Revision, &b.SHA, &b.AssignmentVersion, &b.Developer, &b.Reviewer, &b.Generation, &b.Version, &b.Kind, &b.Where, &b.WaitingPrincipal, &b.ExpectedInput, &raw, &b.Mode, &b.FullApprovedBy, &b.FullApprovalRef, &b.State, &b.InputVersion, &b.CheckedInputVersion, &b.LeaseID, &b.LeaseUntil, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.Fingerprint)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &b.Conditions)
	}
	return b, err
}
func blockerCurrent(b TaskBlocker, t TaskFlowState) bool {
	return b.AssignmentVersion == t.AssignmentVersion && b.Developer == t.Developer && b.Reviewer == t.Reviewer && !t.NeedsReassignment && !t.Archived
}
func blockerHash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func blockerLeaseExpired(raw string, now time.Time) bool {
	deadline, err := time.Parse(time.RFC3339Nano, raw)
	return err != nil || !now.Before(deadline)
}
func blockerTerminal(state string) bool { return state == "resolved" || state == "cancelled" }
func validBlockerPrincipal(s string) bool {
	if s == "" {
		return true
	}
	for _, prefix := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, prefix) {
			id := strings.TrimPrefix(s, prefix)
			return id != "" && len(id) <= 128 && !strings.ContainsAny(id, " \t\r\n/\\")
		}
	}
	return false
}
func blockerConditions(input []BlockerCondition) ([]BlockerCondition, error) {
	if len(input) > 3 {
		return nil, ErrTaskFlowInvalid
	}
	byID := map[string]string{}
	for _, c := range input {
		if (c.ID != "materials" && c.ID != "environment" && c.ID != "authorization") || strings.TrimSpace(c.Text) == "" || len(c.Text) > 1024 || byID[c.ID] != "" {
			return nil, ErrTaskFlowInvalid
		}
		byID[c.ID] = strings.TrimSpace(c.Text)
	}
	out := []BlockerCondition{{"identity", "当前身份、编组和指派有效"}, {"revision", "原任务确切修订和范围未变"}, {"dependencies", "依赖仍满足"}}
	for _, c := range []BlockerCondition{{"materials", "核实答复与材料真实且满足原任务条件"}, {"environment", "重新核实原任务环境与隔离对象"}, {"authorization", "重新核实当前有效授权与预算；唤醒不是执行授权"}} {
		if byID[c.ID] != "" {
			c.Text = byID[c.ID]
		}
		out = append(out, c)
	}
	return out, nil
}
func taskBlockersClear(ctx context.Context, q taskQuery, id string) error {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM console_task_blocker WHERE task_id=? AND state NOT IN('resolved','cancelled')`, id).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrTaskFlowConflict
	}
	return nil
}
func blockerAudit(ctx context.Context, tx *sql.Tx, b TaskBlocker, actor, op string, details any, at string) error {
	raw, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, `INSERT INTO console_task_blocker_audit(blocker_id,task_id,actor_id,operation,version,details_json,created_at) VALUES(?,?,?,?,?,?,?)`, b.ID, b.TaskID, actor, op, b.Version, string(raw), at)
	return err
}
func blockerWake(ctx context.Context, tx *sql.Tx, b TaskBlocker, at string) error {
	if b.InputVersion < 1 || b.Developer == "" || blockerTerminal(b.State) || b.State == "needs_reassignment" {
		return nil
	}
	id := "blocker:" + blockerHash([]any{b.ID, b.Generation, b.InputVersion, b.AssignmentVersion, b.Developer})
	_, err := tx.ExecContext(ctx, `INSERT INTO console_task_blocker_outbox(id,blocker_id,task_id,generation,input_version,assignment_version,recipient_agent_id,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(blocker_id,generation,input_version,assignment_version,recipient_agent_id) DO NOTHING`, id, b.ID, b.TaskID, b.Generation, b.InputVersion, b.AssignmentVersion, b.Developer, at)
	return err
}
func saveBlocker(ctx context.Context, tx *sql.Tx, b TaskBlocker) error {
	conditions, _ := json.Marshal(b.Conditions)
	_, err := tx.ExecContext(ctx, `UPDATE console_task_blocker SET assignment_version=?,developer_agent_id=?,reviewer_agent_id=?,generation=?,version=?,waiting_principal=?,conditions_json=?,mode=?,full_approved_by=?,full_approval_ref=?,state=?,input_version=?,checked_input_version=?,lease_id=?,lease_until=?,updated_at=? WHERE id=?`, b.AssignmentVersion, b.Developer, b.Reviewer, b.Generation, b.Version, b.WaitingPrincipal, string(conditions), b.Mode, b.FullApprovedBy, b.FullApprovalRef, b.State, b.InputVersion, b.CheckedInputVersion, b.LeaseID, b.LeaseUntil, b.UpdatedAt, b.ID)
	return err
}
func insertBlocker(ctx context.Context, tx *sql.Tx, b TaskBlocker) error {
	conditions, _ := json.Marshal(b.Conditions)
	_, err := tx.ExecContext(ctx, `INSERT INTO console_task_blocker(`+blockerCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, b.ID, b.TaskID, b.SpecID, b.Revision, b.SHA, b.AssignmentVersion, b.Developer, b.Reviewer, b.Generation, b.Version, b.Kind, b.Where, b.WaitingPrincipal, b.ExpectedInput, string(conditions), b.Mode, b.FullApprovedBy, b.FullApprovalRef, b.State, b.InputVersion, b.CheckedInputVersion, b.LeaseID, b.LeaseUntil, b.CreatedBy, b.CreatedAt, b.UpdatedAt, b.Fingerprint)
	if err != nil {
		return err
	}
	fingerprint, satisfied, _, e := blockerDependencySnapshot(ctx, tx, b.TaskID)
	if e != nil {
		return e
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_condition(blocker_id,adapter,last_sha,last_satisfied) VALUES(?,'dependency_gate',?,?)`, b.ID, fingerprint, satisfied)
	return err
}

func blockerDependencySnapshot(ctx context.Context, q taskQuery, task string) (string, bool, string, error) {
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT depends_on_json FROM console_spec_task WHERE id=?`, task).Scan(&raw); err != nil {
		return "", false, "", err
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return "", false, "", ErrTaskFlowInvalid
	}
	states := map[string]string{}
	satisfied := true
	for _, id := range ids {
		var status string
		err := q.QueryRowContext(ctx, `SELECT status FROM (`+taskUnion+`) WHERE id=?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			status = "missing"
		} else if err != nil {
			return "", false, "", err
		}
		states[id] = status
		if status != "done" {
			satisfied = false
		}
	}
	details, _ := json.Marshal(states)
	return blockerHash([]any{raw, states}), satisfied, string(details), nil
}

// Only a tested, finite adapter may create automatic input; no expressions,
// network probes, commands or user-provided URLs are evaluated here.
func blockerDependencyInput(ctx context.Context, tx *sql.Tx, b *TaskBlocker, at string) (bool, error) {
	fingerprint, satisfied, details, err := blockerDependencySnapshot(ctx, tx, b.TaskID)
	if err != nil {
		return false, err
	}
	var old string
	var was bool
	var sequence int
	err = tx.QueryRowContext(ctx, `SELECT last_sha,last_satisfied,event_sequence FROM console_task_blocker_condition WHERE blocker_id=? AND adapter='dependency_gate'`, b.ID).Scan(&old, &was, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_condition(blocker_id,adapter,last_sha,last_satisfied) VALUES(?,'dependency_gate',?,?)`, b.ID, fingerprint, satisfied)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if old == fingerprint {
		return false, nil
	}
	changed := !was && satisfied
	if changed {
		sequence++
		b.InputVersion++
		b.Version++
		b.UpdatedAt = at
		if b.State != "rechecking" {
			b.State = "ready_to_recheck"
		}
		event := fmt.Sprintf("dependency-gate:%d", sequence)
		_, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_input(blocker_id,source,event_id,payload_sha,input_version,actor_id,body,evidence_ref,created_at) VALUES(?,'condition:dependency_gate',?,?,?,'system',?,?,?)`, b.ID, event, fingerprint, b.InputVersion, "指定依赖gate由未满足变为满足；仅请求重核，其他条件仍须逐项核实", "task-dependencies:"+fingerprint, at)
		if err != nil {
			return false, err
		}
		if err = blockerAudit(ctx, tx, *b, "system", "dependency_condition_input", map[string]any{"snapshot": details, "source_sha": fingerprint}, at); err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE console_task_blocker_condition SET last_sha=?,last_satisfied=?,event_sequence=? WHERE blocker_id=? AND adapter='dependency_gate'`, fingerprint, satisfied, sequence, b.ID)
	return changed, err
}

// The transport holds current PG authorization locks. The first SQLite read
// revalidates the exact state used to acquire them; no initial lookup grants rights.
func (s *SQLite) UpdateTaskBlocker(ctx context.Context, expected TaskFlowState, actor BlockerActor, bid string, u BlockerUpdate, recheck func() error, now time.Time) (BlockerResult, error) {
	var out BlockerResult
	key := actor.Key()
	at := now.UTC().Format(time.RFC3339Nano)
	if key == "" || recheck == nil || u.RequestID == "" || len(u.RequestID) > 128 || len(u.Where) > 4096 || len(u.ExpectedInput) > 4096 || len(u.Body) > 8192 || len(u.Evidence) > 2048 || len(u.FullApprovalRef) > 2048 || !validBlockerPrincipal(u.WaitingPrincipal) {
		return out, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	t, err := taskFlowState(ctx, tx, expected.ID)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(t, expected) {
		return out, ErrTaskFlowConflict
	}
	if t.Archived || t.NeedsReassignment || t.GroupCode == "" {
		return out, ErrTaskFlowPermission
	}
	var b TaskBlocker
	if u.Operation != "create" {
		b, err = scanBlocker(tx.QueryRowContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE id=? AND task_id=?`, bid, t.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrTaskFlowPermission
		}
		if err != nil {
			return out, err
		}
	}
	canRead := actor.taskReader(t)
	canInput := canRead || b.WaitingPrincipal == key
	if !canRead && (u.Operation != "input" || !canInput) {
		return out, ErrTaskFlowPermission
	}
	if u.Operation == "input" && !(actor.owner(t) || b.WaitingPrincipal == key) {
		return out, ErrTaskFlowPermission
	}
	payload := blockerHash([]any{t.ID, bid, u})
	var oldHash, raw string
	err = tx.QueryRowContext(ctx, `SELECT payload_sha,result_json FROM console_task_blocker_request WHERE actor_id=? AND request_id=?`, key, u.RequestID).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != payload {
			return out, ErrTaskFlowConflict
		}
		if err = recheck(); err != nil {
			return out, err
		}
		err = json.Unmarshal([]byte(raw), &out)
		out.Replayed = true
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var status, deps, specID, sha string
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT status,depends_on_json,spec_id,revision,content_sha256 FROM console_spec_task WHERE id=?`, t.ID).Scan(&status, &deps, &specID, &revision, &sha); err != nil {
		return out, err
	}
	if status == "done" {
		return out, ErrTaskFlowConflict
	}
	if u.Operation != "create" && u.Operation != "adopt" && (!blockerCurrent(b, t) || b.SpecID != specID || b.Revision != revision || b.SHA != sha) {
		return out, ErrTaskFlowConflict
	}
	if u.Operation != "create" && b.Version != u.ExpectedVersion {
		return out, ErrTaskFlowConflict
	}
	out.Outcome = "updated"
	switch u.Operation {
	case "create":
		if !(actor.developer(t) || actor.owner(t)) {
			return out, ErrTaskFlowPermission
		}
		if u.ExpectedVersion != t.Version {
			return out, ErrTaskFlowConflict
		}
		if strings.TrimSpace(u.Where) == "" || strings.TrimSpace(u.ExpectedInput) == "" {
			return out, ErrTaskFlowInvalid
		}
		if u.Kind != "requirement_unclear" && u.Kind != "change" && u.Kind != "technical" && u.Kind != "resource" {
			return out, ErrTaskFlowInvalid
		}
		conditions, e := blockerConditions(u.Conditions)
		if e != nil {
			return out, e
		}
		mode := "light"
		if u.ProductionOrSecurity {
			mode = "full"
		}
		fingerprint := blockerHash([]any{t.ID, t.AssignmentVersion, u.Kind, strings.TrimSpace(u.Where), u.WaitingPrincipal, strings.TrimSpace(u.ExpectedInput), conditions, mode})
		existing, e := scanBlocker(tx.QueryRowContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE task_id=? AND fingerprint=? AND state NOT IN('resolved','cancelled') ORDER BY id LIMIT 1`, t.ID, fingerprint))
		if e == nil {
			b = existing
			out.Outcome = "unchanged"
		} else if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		} else {
			b = TaskBlocker{ID: "blk:" + blockerHash([]string{key, u.RequestID}), TaskID: t.ID, SpecID: specID, Revision: revision, SHA: sha, AssignmentVersion: t.AssignmentVersion, Developer: t.Developer, Reviewer: t.Reviewer, Generation: 1, Version: 1, Kind: u.Kind, Where: strings.TrimSpace(u.Where), WaitingPrincipal: u.WaitingPrincipal, ExpectedInput: strings.TrimSpace(u.ExpectedInput), Conditions: conditions, Mode: mode, State: "waiting", CreatedBy: key, CreatedAt: at, UpdatedAt: at, Fingerprint: fingerprint}
			if b.WaitingPrincipal == "" {
				b.State = "needs_owner"
			}
			if err = insertBlocker(ctx, tx, b); err != nil {
				return out, err
			}
			// Preserve an older blocked reason as a separate unsatisfied card unless the
			// current executor explicitly restates that exact reason in the new card.
			var old string
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(blocker,'') FROM console_spec_task WHERE id=?`, t.ID).Scan(&old); err != nil {
				return out, err
			}
			if status == "blocked" && strings.TrimSpace(old) != "" && strings.TrimSpace(old) != b.Where {
				var n int
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_task_blocker WHERE task_id=? AND id<>?`, t.ID, b.ID).Scan(&n); err != nil {
					return out, err
				}
				if n == 0 {
					legacy := b
					legacy.ID = "legacy:" + blockerHash([]string{t.ID, old})
					legacy.Where = old
					legacy.Kind = "technical"
					legacy.WaitingPrincipal = ""
					legacy.ExpectedInput = "旧阻塞仍未解除：请所有者明确所等人、补入新材料后重核"
					legacy.State = "needs_owner"
					legacy.Fingerprint = blockerHash([]string{t.ID, "legacy", old})
					if err = insertBlocker(ctx, tx, legacy); err != nil {
						return out, err
					}
				}
			}
			if status == "in_progress" {
				if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET status='blocked',blocker=? WHERE id=?`, b.Where, t.ID); err != nil {
					return out, err
				}
			}
		}
	case "input":
		if blockerTerminal(b.State) || u.Source == "" || len(u.Source) > 64 || u.EventID == "" || len(u.EventID) > 128 || strings.TrimSpace(u.Body) == "" || strings.TrimSpace(u.Evidence) == "" {
			return out, ErrTaskFlowInvalid
		}
		inputHash := blockerHash([]any{key, u.Body, u.Evidence, u.ProductionOrSecurity})
		var previous string
		e := tx.QueryRowContext(ctx, `SELECT payload_sha FROM console_task_blocker_input WHERE blocker_id=? AND source=? AND event_id=?`, b.ID, u.Source, u.EventID).Scan(&previous)
		if e == nil {
			if previous != inputHash {
				return out, ErrTaskFlowConflict
			}
			out.Outcome = "unchanged"
		} else if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		} else {
			if u.ProductionOrSecurity && b.Mode != "full" {
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
			if _, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_input(blocker_id,source,event_id,payload_sha,input_version,actor_id,body,evidence_ref,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, b.ID, u.Source, u.EventID, inputHash, b.InputVersion, key, u.Body, u.Evidence, at); err != nil {
				return out, err
			}
			if err = blockerWake(ctx, tx, b, at); err != nil {
				return out, err
			}
		}
	case "assign_waiter":
		if !actor.owner(t) || blockerTerminal(b.State) || u.WaitingPrincipal == "" {
			return out, ErrTaskFlowPermission
		}
		b.WaitingPrincipal = u.WaitingPrincipal
		b.Version++
		b.UpdatedAt = at
		if b.State == "needs_owner" {
			b.State = "waiting"
		}
	case "approve_full":
		if !actor.owner(t) || b.Mode != "full" || strings.TrimSpace(u.FullApprovalRef) == "" {
			return out, ErrTaskFlowPermission
		}
		if b.FullApprovedBy == key && b.FullApprovalRef == u.FullApprovalRef {
			out.Outcome = "unchanged"
			break
		}
		b.LeaseID = ""
		b.LeaseUntil = ""
		b.FullApprovedBy = key
		b.FullApprovalRef = u.FullApprovalRef
		b.InputVersion++
		b.State = "ready_to_recheck"
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_input(blocker_id,source,event_id,payload_sha,input_version,actor_id,body,evidence_ref,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, b.ID, key, "approve:"+u.RequestID, blockerHash([]string{key, u.FullApprovalRef}), b.InputVersion, key, "所有者补入完整档核对记录；仍仅请求重核", u.FullApprovalRef, at); err != nil {
			return out, err
		}
		if err = blockerWake(ctx, tx, b, at); err != nil {
			return out, err
		}
		b.Version++
		b.UpdatedAt = at
	case "begin_recheck":
		if !actor.developer(t) || b.InputVersion < 1 || b.InputVersion != u.InputVersion || b.State != "ready_to_recheck" || (b.Mode == "full" && b.FullApprovalRef == "") {
			return out, ErrTaskFlowConflict
		}
		b.State = "rechecking"
		b.LeaseID = "lease:" + blockerHash([]any{b.ID, b.Generation, b.InputVersion, key, u.RequestID})
		b.LeaseUntil = now.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
		b.Version++
		b.UpdatedAt = at
	case "finish_recheck":
		if b.Mode == "full" && b.FullApprovalRef == "" {
			return out, ErrTaskFlowConflict
		}
		if !actor.developer(t) || b.State != "rechecking" || b.LeaseID == "" || u.LeaseID != b.LeaseID || blockerLeaseExpired(b.LeaseUntil, now) {
			return out, ErrTaskFlowConflict
		}
		if u.InputVersion != b.InputVersion {
			b.State = "ready_to_recheck"
			out.Outcome = "new_input_requires_recheck"
		} else {
			if len(u.Checks) != 3 {
				return out, ErrTaskFlowInvalid
			}
			seen := map[string]bool{}
			pass := true
			for _, c := range u.Checks {
				if (c.ID != "materials" && c.ID != "environment" && c.ID != "authorization") || seen[c.ID] || len(c.Evidence) > 2048 || strings.TrimSpace(c.Evidence) == "" || (c.Result != "pass" && c.Result != "fail" && c.Result != "unknown") {
					return out, ErrTaskFlowInvalid
				}
				seen[c.ID] = true
				if c.Result != "pass" {
					pass = false
				}
			}
			if e := taskDependenciesDone(ctx, tx, deps); e != nil {
				if errors.Is(e, ErrTaskFlowConflict) {
					pass = false
				} else {
					return out, e
				}
			}
			b.CheckedInputVersion = b.InputVersion
			b.State = "waiting"
			if pass {
				b.State = "resolved"
				out.Outcome = "rechecked_not_resumed"
			}
		}
		b.LeaseID = ""
		b.LeaseUntil = ""
		b.Version++
		b.UpdatedAt = at
	case "cancel":
		if !actor.owner(t) || strings.TrimSpace(u.Evidence) == "" {
			return out, ErrTaskFlowPermission
		}
		b.State = "cancelled"
		b.LeaseID = ""
		b.LeaseUntil = ""
		b.Version++
		b.UpdatedAt = at
	case "adopt":
		if !actor.owner(t) || blockerTerminal(b.State) {
			return out, ErrTaskFlowPermission
		}
		b.AssignmentVersion = t.AssignmentVersion
		b.Developer = t.Developer
		b.Reviewer = t.Reviewer
		b.SpecID = specID
		b.Revision = revision
		b.SHA = sha
		b.Generation++
		b.Version++
		b.LeaseID = ""
		b.LeaseUntil = ""
		b.UpdatedAt = at
		b.State = "waiting"
		b.FullApprovedBy = ""
		b.FullApprovalRef = ""
		if b.WaitingPrincipal == "" {
			b.State = "needs_owner"
		}
		if b.InputVersion > 0 {
			b.State = "ready_to_recheck"
			if err = blockerWake(ctx, tx, b, at); err != nil {
				return out, err
			}
		}
	default:
		return out, ErrTaskFlowInvalid
	}
	if err = saveBlocker(ctx, tx, b); err != nil {
		return out, err
	}
	if out.Outcome != "unchanged" {
		t.Version++
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=? WHERE id=?`, t.Version, t.ID); err != nil {
			return out, err
		}
		if err = blockerAudit(ctx, tx, b, key, u.Operation, map[string]any{"input_version": b.InputVersion, "outcome": out.Outcome, "checks": u.Checks, "evidence_ref": u.Evidence}, at); err != nil {
			return out, err
		}
		if err = flowAudit(ctx, tx, t.ID, key, "blocker_"+u.Operation, t.Version, now); err != nil {
			return out, err
		}
	}
	out.Blocker = b
	if !canRead {
		out.Blocker = TaskBlocker{ID: b.ID, TaskID: b.TaskID, Version: b.Version, State: b.State, InputVersion: b.InputVersion}
	}
	out.TaskVersion = t.Version
	response, _ := json.Marshal(out)
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_task_blocker_request(actor_id,request_id,task_id,payload_sha,result_json,created_at) VALUES(?,?,?,?,?,?)`, key, u.RequestID, t.ID, payload, string(response), at); err != nil {
		return out, err
	}
	if err = recheck(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

func (s *SQLite) ReadTaskBlockers(ctx context.Context, expected TaskFlowState, actor BlockerActor, bid, after string, limit int, recheck func() error, inputBefore ...int) ([]TaskBlocker, []BlockerInput, error) {
	if actor.Key() == "" || recheck == nil || limit < 1 || limit > 101 {
		return nil, nil, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	t, err := taskFlowState(ctx, tx, expected.ID)
	if err != nil {
		return nil, nil, err
	}
	if !reflect.DeepEqual(t, expected) {
		return nil, nil, ErrTaskFlowConflict
	}
	if t.Archived || t.NeedsReassignment {
		return nil, nil, ErrTaskFlowPermission
	}
	if !actor.taskReader(t) {
		return nil, nil, ErrTaskFlowPermission
	}
	var specID, sha string
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT spec_id,revision,content_sha256 FROM console_spec_task WHERE id=?`, t.ID).Scan(&specID, &revision, &sha); err != nil {
		return nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE task_id=? AND id>? AND (?='' OR id=?) ORDER BY id LIMIT ?`, t.ID, after, bid, bid, limit)
	if err != nil {
		return nil, nil, err
	}
	cards := []TaskBlocker{}
	for rows.Next() {
		b, e := scanBlocker(rows)
		if e != nil {
			rows.Close()
			return nil, nil, e
		}
		if !blockerTerminal(b.State) && (!blockerCurrent(b, t) || b.SpecID != specID || b.Revision != revision || b.SHA != sha) {
			b.State = "needs_reassignment"
			b.LeaseID = ""
			b.LeaseUntil = ""
		}
		if b.InputVersion > 0 && !blockerTerminal(b.State) && b.State != "needs_reassignment" {
			var dispatched sql.NullString
			var reason string
			e := tx.QueryRowContext(ctx, `SELECT dispatched_at,terminal_reason FROM console_task_blocker_outbox WHERE blocker_id=? AND generation=? AND input_version=? AND assignment_version=? AND recipient_agent_id=?`, b.ID, b.Generation, b.InputVersion, b.AssignmentVersion, b.Developer).Scan(&dispatched, &reason)
			if errors.Is(e, sql.ErrNoRows) {
				b.WakeState = "pending_reconciliation"
			} else if e != nil {
				rows.Close()
				return nil, nil, e
			} else if !dispatched.Valid {
				b.WakeState = "queued_offline_or_upgrade"
			} else if reason == "reliable_delivery_enqueued" {
				b.WakeState = "durable_delivery_queued"
			} else {
				b.WakeState = reason
			}
		}
		cards = append(cards, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	inputs := []BlockerInput{}
	if bid != "" && len(cards) == 1 {
		before := int(^uint(0) >> 1)
		if len(inputBefore) > 0 && inputBefore[0] > 0 {
			before = inputBefore[0]
		}
		rs, e := tx.QueryContext(ctx, `SELECT source,event_id,input_version,actor_id,body,evidence_ref,created_at FROM console_task_blocker_input WHERE blocker_id=? AND input_version<? ORDER BY input_version DESC LIMIT 101`, bid, before)
		if e != nil {
			return nil, nil, e
		}
		for rs.Next() {
			var v BlockerInput
			if e = rs.Scan(&v.Source, &v.EventID, &v.InputVersion, &v.Actor, &v.Body, &v.Evidence, &v.CreatedAt); e != nil {
				rs.Close()
				return nil, nil, e
			}
			inputs = append(inputs, v)
		}
		err = rs.Err()
		rs.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	if err = recheck(); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return cards, inputs, nil
}

type BlockerWake struct {
	ID, BlockerID, TaskID, Recipient            string
	Generation, InputVersion, AssignmentVersion int
}

func (s *SQLite) PendingBlockerWakes(ctx context.Context, after string) ([]BlockerWake, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,blocker_id,task_id,recipient_agent_id,generation,input_version,assignment_version FROM console_task_blocker_outbox WHERE dispatched_at IS NULL AND id>? ORDER BY id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlockerWake{}
	for rows.Next() {
		var w BlockerWake
		if err = rows.Scan(&w.ID, &w.BlockerID, &w.TaskID, &w.Recipient, &w.Generation, &w.InputVersion, &w.AssignmentVersion); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *SQLite) FinishBlockerWake(ctx context.Context, id, reason string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE console_task_blocker_outbox SET dispatched_at=?,terminal_reason=? WHERE id=? AND dispatched_at IS NULL`, at.UTC().Format(time.RFC3339Nano), reason, id)
	return err
}

// Check/repair and outbox insertion share the same transaction as the card.
func (s *SQLite) ReconcileTaskBlockers(ctx context.Context, t TaskFlowState, recheck func() error, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := taskFlowState(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, t) {
		return ErrTaskFlowConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+blockerCols+` FROM console_task_blocker WHERE task_id=? AND state NOT IN('resolved','cancelled')`, t.ID)
	if err != nil {
		return err
	}
	cards := []TaskBlocker{}
	for rows.Next() {
		b, e := scanBlocker(rows)
		if e != nil {
			rows.Close()
			return e
		}
		cards = append(cards, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var specID, sha string
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT spec_id,revision,content_sha256 FROM console_spec_task WHERE id=?`, t.ID).Scan(&specID, &revision, &sha); err != nil {
		return err
	}
	at := now.UTC().Format(time.RFC3339Nano)
	changed := false
	for _, b := range cards {
		if !blockerCurrent(b, t) || b.SpecID != specID || b.Revision != revision || b.SHA != sha {
			if b.State != "needs_reassignment" {
				changed = true
				b.State = "needs_reassignment"
				b.Version++
				b.LeaseID = ""
				b.LeaseUntil = ""
				b.UpdatedAt = at
				if err = saveBlocker(ctx, tx, b); err != nil {
					return err
				}
				if err = blockerAudit(ctx, tx, b, "system", "assignment_invalidated", nil, at); err != nil {
					return err
				}
			}
			continue
		}
		if b.State == "resolved" {
			continue
		}
		inputChanged, e := blockerDependencyInput(ctx, tx, &b, at)
		if e != nil {
			return e
		}
		if inputChanged {
			changed = true
			if err = saveBlocker(ctx, tx, b); err != nil {
				return err
			}
		}
		if b.State == "rechecking" && blockerLeaseExpired(b.LeaseUntil, now) {
			changed = true
			b.State = "ready_to_recheck"
			b.LeaseID = ""
			b.LeaseUntil = ""
			b.Version++
			b.UpdatedAt = at
			if err = saveBlocker(ctx, tx, b); err != nil {
				return err
			}
		}
		if b.State == "ready_to_recheck" || b.State == "rechecking" {
			if err = blockerWake(ctx, tx, b, at); err != nil {
				return err
			}
		}
	}
	if changed {
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, t.ID); err != nil {
			return err
		}
		if err = flowAudit(ctx, tx, t.ID, "system", "blocker_reconcile", t.Version+1, now); err != nil {
			return err
		}
	}
	if recheck == nil {
		return ErrTaskFlowPermission
	}
	if err = recheck(); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLite) BlockerWakeCurrent(ctx context.Context, w BlockerWake) (bool, error) {
	var state, dev string
	var gen, iv, av int
	err := s.db.QueryRowContext(ctx, `SELECT b.state,b.developer_agent_id,b.generation,b.input_version,b.assignment_version FROM console_task_blocker b JOIN console_spec_task t ON t.id=b.task_id WHERE b.id=? AND b.task_id=? AND b.spec_id=t.spec_id AND b.revision=t.revision AND b.content_sha256=t.content_sha256`, w.BlockerID, w.TaskID).Scan(&state, &dev, &gen, &iv, &av)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && !blockerTerminal(state) && state != "needs_reassignment" && dev == w.Recipient && gen == w.Generation && iv >= w.InputVersion && av == w.AssignmentVersion, err
}

// Only explicitly enrolled tasks are scanned; keyset pages cannot be blocked by
// an offline first page and no global legacy task migration runs in background.
func (s *SQLite) BlockerTasksAfter(ctx context.Context, after string) ([]TaskFlowState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,s.principal_id,t.group_code,t.version,t.assignment_version,COALESCE(t.developer_agent_id,''),COALESCE(t.reviewer_agent_id,''),t.needs_reassignment,s.archived FROM console_spec_task t JOIN console_spec s ON s.id=t.spec_id WHERE t.id>? AND EXISTS(SELECT 1 FROM console_task_blocker b WHERE b.task_id=t.id AND b.state NOT IN('resolved','cancelled')) ORDER BY t.id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskFlowState{}
	for rows.Next() {
		var t TaskFlowState
		if err = rows.Scan(&t.ID, &t.Owner, &t.GroupCode, &t.Version, &t.AssignmentVersion, &t.Developer, &t.Reviewer, &t.NeedsReassignment, &t.Archived); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
