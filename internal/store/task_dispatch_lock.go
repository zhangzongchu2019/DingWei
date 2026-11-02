package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

var _ td.Repository = (*SQLite)(nil)

func taskDispatchLockArgs(l td.Lock) []any {
	h, _ := json.Marshal(l.Handoff)
	a := taskDispatchKeyArgs(l.Key)
	return append(a, l.State, l.Generation, l.LeaseToken, l.HolderAgentID, l.HolderOwnerLC, l.AssignedByUserLC, l.AuthorizationGroup, l.CommitSHA, l.TasksBlobSHA, l.TaskLine, taskDispatchTime(l.GrantedAt), taskDispatchTime(l.ExpiresAt), taskDispatchTime(l.LastRenewedAt), taskDispatchTime(l.UpdatedAt), l.MonitorRef, l.PullRequestID, l.PRDimension, l.PRHeadSHA, l.PRRemoteState, l.PRBaseRef, l.PRHeadRef, taskDispatchTime(l.PRUpdatedAt), l.ComparisonState, l.ObservedCommitSHA, taskDispatchTime(l.LastAttemptAt), taskDispatchTime(l.LastCheckedAt), l.ComparisonError, string(h), l.SyncRequired)
}
func taskDispatchFingerprint(m td.Mutation) string {
	// Remote observations and bindings are re-read on retry. They are not user
	// request identity. The explicit new execution coordinates remain included.
	m.Bindings = nil
	m.ComparisonState = ""
	m.ObservedCommitSHA = ""
	m.ComparisonError = ""
	m.PRDimension = ""
	m.PRHeadSHA = ""
	m.PRRemoteState = ""
	m.PRBaseRef = ""
	m.PRHeadRef = ""
	m.PRUpdatedAt = time.Time{}
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func taskDispatchOutcome(err error) string {
	if err == nil {
		return "success"
	}
	return err.Error()
}
func taskDispatchOutcomeError(s string) error {
	if s == "success" {
		return nil
	}
	for _, e := range []error{td.ErrInvalid, td.ErrForbidden, td.ErrConflict, td.ErrClock, td.ErrUnavailable, td.ErrUnknown, td.ErrNeedsSync, td.ErrComparison, td.ErrNotFound, td.ErrCompleted, td.ErrPRRequired} {
		if e.Error() == s {
			return e
		}
	}
	return td.ErrUnknown
}
func taskDispatchBindings(ctx context.Context, tx *sql.Tx, m td.Mutation) error {
	if m.ActorKind == "system" {
		return nil
	}
	required := map[string]bool{}
	if m.ActorKind == "user" {
		required[m.ActorUserLC] = false
	} else if m.ActorKind == "agent" {
		required[m.ActorOwnerLC] = false
	} else {
		return td.ErrForbidden
	}
	if m.HolderOwnerLC != "" {
		required[m.HolderOwnerLC] = false
	}
	for _, b := range m.Bindings {
		if b.Principal == "" || b.Provider != m.Key.Provider || b.Instance != m.Key.Instance || b.AccountID == "" || b.Login == "" {
			return td.ErrForbidden
		}
		var n int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM openspec_read_binding b JOIN console_principal p ON p.id=b.principal AND p.active=1 WHERE b.principal=? AND b.provider=? AND b.instance=? AND b.account_id=? AND b.login=?`, b.Principal, b.Provider, b.Instance, b.AccountID, b.Login).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return td.ErrForbidden
		}
		if _, ok := required[b.Principal]; ok {
			required[b.Principal] = true
		}
	}
	for p, found := range required {
		if p == "" || !found {
			return td.ErrForbidden
		}
	}
	return nil
}

// ApplyTaskDispatch samples UTC only after acquiring the SQLite transaction.
// Every completed decision, event and notification is committed together. A
// failed/uncertain commit never returns success, even after the PG fence ran.
func (s *SQLite) ApplyTaskDispatch(ctx context.Context, m td.Mutation, clock func() time.Time, check func() error) (td.Result, error) {
	empty := td.Result{OperationID: m.OperationID}
	if m.Key.Validate() != nil || !td.ValidOperation(m.OperationID) || clock == nil || check == nil {
		return empty, td.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	now := clock().UTC()
	if err = taskDispatchBindings(ctx, tx, m); err != nil {
		return empty, err
	}
	fingerprint := taskDispatchFingerprint(m)
	var oldFingerprint, stored string
	err = tx.QueryRowContext(ctx, `SELECT request_sha256,result_json FROM r10_task_lock_operation WHERE operation_id=?`, m.OperationID).Scan(&oldFingerprint, &stored)
	if err == nil {
		if oldFingerprint != fingerprint {
			return empty, td.ErrConflict
		}
		var r td.Result
		if err = json.Unmarshal([]byte(stored), &r); err != nil {
			return empty, err
		}
		if err = check(); err != nil {
			return empty, err
		}
		if err = tx.Commit(); err != nil {
			return empty, td.ErrUnknown
		}
		r.Replay = true
		return r, taskDispatchOutcomeError(r.Outcome)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	old, err := scanTaskDispatchLock(tx.QueryRowContext(ctx, `SELECT `+taskDispatchColumns+` FROM r10_task_lock WHERE `+taskDispatchWhere, taskDispatchKeyArgs(m.Key)...))
	if err != nil {
		return empty, err
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return empty, err
	}
	next, decision := td.Transition(old, m, now, hex.EncodeToString(random[:]))
	// Comparison failures persist the new unknown/needs_sync observation while
	// refusing the requested action. Other refusals leave the lease untouched.
	write := decision == nil || errors.Is(decision, td.ErrComparison) || errors.Is(decision, td.ErrNeedsSync) || errors.Is(decision, td.ErrPRRequired)
	if old == nil && decision != nil {
		write = false
	}
	if write {
		args := taskDispatchLockArgs(next)
		if old == nil {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
			_, err = tx.ExecContext(ctx, `INSERT INTO r10_task_lock (`+taskDispatchColumns+`) VALUES (`+placeholders+`)`, args...)
		} else {
			columns := strings.Split(taskDispatchColumns, ",")
			assignments := make([]string, len(columns)-5)
			for i, c := range columns[5:] {
				assignments[i] = c + "=?"
			}
			updateArgs := append(args[5:], taskDispatchKeyArgs(m.Key)...)
			updateArgs = append(updateArgs, old.Generation, old.LeaseToken, taskDispatchTime(old.UpdatedAt))
			predicate := ""
			if decision == nil {
				switch m.Kind {
				case "grant":
					predicate = " AND (state!='active' OR expires_at_utc<=?)"
				case "expire":
					predicate = " AND state='active' AND expires_at_utc<=?"
				default:
					predicate = " AND state='active' AND expires_at_utc>?"
				}
				updateArgs = append(updateArgs, taskDispatchTime(now))
			}
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE r10_task_lock SET `+strings.Join(assignments, ",")+` WHERE `+taskDispatchWhere+` AND generation=? AND lease_token=? AND updated_at_utc=?`+predicate, updateArgs...)
			if err == nil {
				var n int64
				n, err = result.RowsAffected()
				if err == nil && n != 1 {
					err = td.ErrConflict
				}
			}
		}
		if err != nil {
			return empty, err
		}
	}
	if !write && old != nil {
		next = *old
	}
	result := td.Result{OperationID: m.OperationID, Outcome: taskDispatchOutcome(decision), Lock: next}
	encoded, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	opArgs := append([]any{m.OperationID}, taskDispatchKeyArgs(m.Key)...)
	opArgs = append(opArgs, m.Kind, fingerprint, m.ActorKind, m.ActorUserLC, m.ActorAgentID, m.ActorOwnerLC, m.ExpectedGeneration, next.Generation, result.Outcome, string(encoded), taskDispatchTime(now), taskDispatchTime(now))
	_, err = tx.ExecContext(ctx, `INSERT INTO r10_task_lock_operation(operation_id,provider,instance,repository_id,change_directory,task_id,operation_kind,request_sha256,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,expected_generation,result_generation,outcome,result_json,created_at_utc,completed_at_utc) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, opArgs...)
	if err != nil {
		return empty, err
	}
	eventArgs := append([]any{m.OperationID}, taskDispatchKeyArgs(m.Key)...)
	eventArgs = append(eventArgs, next.Generation, m.Kind, m.ActorKind, m.ActorUserLC, m.ActorAgentID, m.ActorOwnerLC, taskDispatchTime(now), result.Outcome+";pr="+next.PRDimension)
	_, err = tx.ExecContext(ctx, `INSERT INTO r10_task_lock_event(operation_id,provider,instance,repository_id,change_directory,task_id,generation,event_type,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,at_utc,detail) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, eventArgs...)
	if err != nil {
		return empty, err
	}
	notice := ""
	if decision == nil && (m.Kind == "grant" || m.Kind == "handoff" || m.Kind == "expire" || m.Kind == "release") {
		notice = m.Kind
	}
	if write && old != nil && next.ComparisonState != old.ComparisonState && (next.ComparisonState == "needs_sync" || next.ComparisonState == "comparison_unknown") {
		notice = next.ComparisonState
	}
	if notice != "" {
		if err = taskDispatchEnqueue(ctx, tx, m, next, notice, "agent", next.HolderAgentID, next.HolderOwnerLC, now); err != nil {
			return empty, err
		}
		if notice != "grant" && notice != "handoff" {
			if err = taskDispatchEnqueue(ctx, tx, m, next, notice, "user", next.AssignedByUserLC, next.AssignedByUserLC, now); err != nil {
				return empty, err
			}
		}
	}
	if err = check(); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, td.ErrUnknown
	}
	return result, decision
}
func taskDispatchEnqueue(ctx context.Context, tx *sql.Tx, m td.Mutation, l td.Lock, typ, kind, id, owner string, now time.Time) error {
	if id == "" {
		return td.ErrInvalid
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%s/%s/%s", l.Key.Digest(), l.Generation, typ, kind, id)))
	noticeID := "r10-" + hex.EncodeToString(sum[:])
	args := append([]any{noticeID, m.OperationID}, taskDispatchKeyArgs(m.Key)...)
	args = append(args, l.Generation, kind, id, owner, typ, "pending", noticeID, taskDispatchTime(now))
	_, err := tx.ExecContext(ctx, `INSERT INTO r10_task_lock_outbox(id,operation_id,provider,instance,repository_id,change_directory,task_id,generation,recipient_kind,recipient_id,recipient_owner_lc,event_type,delivery_state,source_envelope_id,created_at_utc) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(provider,instance,repository_id,change_directory,task_id,generation,event_type,recipient_kind,recipient_id) DO NOTHING`, args...)
	return err
}
func (s *SQLite) UpdateTaskDispatchNotice(ctx context.Context, id, expected, state, delivery, reason string, at time.Time) (bool, error) {
	allowed := map[string]map[string]bool{"pending": {"attempting": true, "available": true, "superseded": true, "unknown": true}, "attempting": {"queued": true, "unknown": true, "superseded": true}}
	if !allowed[expected][state] || id == "" || at.IsZero() || len(reason) > 128 {
		return false, td.ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE r10_task_lock_outbox SET delivery_state=?,delivery_id=?,reason=?,completed_at_utc=? WHERE id=? AND delivery_state=?`, state, delivery, reason, taskDispatchTime(at), id, expected)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
