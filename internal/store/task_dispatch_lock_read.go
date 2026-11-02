package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

const taskDispatchWhere = `provider=? AND instance=? AND repository_id=? AND change_directory=? AND task_id=?`
const taskDispatchColumns = `provider,instance,repository_id,change_directory,task_id,state,generation,lease_token,holder_agent_id,holder_owner_lc,assigned_by_user_id_lc,authorization_group_code,commit_sha,tasks_blob_sha,task_line,granted_at_utc,expires_at_utc,last_renewed_at_utc,updated_at_utc,monitor_ref,pull_request_id,pr_dimension,pr_head_sha,pr_remote_state,pr_base_ref,pr_head_ref,pr_updated_at_utc,comparison_state,observed_commit_sha,last_attempt_at_utc,last_checked_at_utc,comparison_error_class,handoff_json`
const taskDispatchNoticeColumns = `id,operation_id,provider,instance,repository_id,change_directory,task_id,generation,recipient_kind,recipient_id,recipient_owner_lc,event_type,delivery_state,source_envelope_id,delivery_id,created_at_utc,completed_at_utc,reason`
const taskDispatchTimeLayout = "2006-01-02T15:04:05.000000000Z"

func taskDispatchKeyArgs(k td.Key) []any {
	return []any{k.Provider, k.Instance, k.Repository, k.ChangeDir, k.TaskID}
}
func taskDispatchTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(taskDispatchTimeLayout)
}
func taskDispatchParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(taskDispatchTimeLayout, s)
}

type taskDispatchScanner interface{ Scan(...any) error }

func scanTaskDispatchLock(row taskDispatchScanner) (*td.Lock, error) {
	var l td.Lock
	var granted, expires, renewed, updated, attempted, checked, prUpdated, handoff string
	err := row.Scan(&l.Provider, &l.Instance, &l.Repository, &l.ChangeDir, &l.TaskID,
		&l.State, &l.Generation, &l.LeaseToken, &l.HolderAgentID, &l.HolderOwnerLC, &l.AssignedByUserLC,
		&l.AuthorizationGroup, &l.CommitSHA, &l.TasksBlobSHA, &l.TaskLine, &granted, &expires, &renewed, &updated,
		&l.MonitorRef, &l.PullRequestID, &l.PRDimension, &l.PRHeadSHA, &l.PRRemoteState, &l.PRBaseRef, &l.PRHeadRef, &prUpdated, &l.ComparisonState, &l.ObservedCommitSHA, &attempted, &checked, &l.ComparisonError, &handoff)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	texts := []string{granted, expires, renewed, updated, attempted, checked, prUpdated}
	targets := []*time.Time{&l.GrantedAt, &l.ExpiresAt, &l.LastRenewedAt, &l.UpdatedAt, &l.LastAttemptAt, &l.LastCheckedAt, &l.PRUpdatedAt}
	for i, text := range texts {
		at, e := taskDispatchParseTime(text)
		if e != nil {
			return nil, fmt.Errorf("r10 persisted time: %w", e)
		}
		*targets[i] = at
	}
	if err = json.Unmarshal([]byte(handoff), &l.Handoff); err != nil {
		return nil, fmt.Errorf("r10 persisted handoff: %w", err)
	}
	return &l, nil
}

func (s *SQLite) TaskDispatchLock(ctx context.Context, k td.Key) (*td.Lock, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return scanTaskDispatchLock(s.db.QueryRowContext(ctx, `SELECT `+taskDispatchColumns+` FROM r10_task_lock WHERE `+taskDispatchWhere, taskDispatchKeyArgs(k)...))
}

func (s *SQLite) TaskDispatchActive(ctx context.Context) ([]td.Lock, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskDispatchColumns+` FROM r10_task_lock WHERE state='active' ORDER BY provider,instance,repository_id,change_directory,task_id LIMIT 10001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locks := []td.Lock{}
	for rows.Next() {
		if len(locks) == 10000 {
			return nil, td.ErrUnavailable
		}
		lock, e := scanTaskDispatchLock(rows)
		if e != nil {
			return nil, e
		}
		locks = append(locks, *lock)
	}
	return locks, rows.Err()
}

func (s *SQLite) TaskDispatchEvents(ctx context.Context, k td.Key, after int64) ([]td.Event, error) {
	if k.Validate() != nil || after < 0 {
		return nil, td.ErrInvalid
	}
	args := append(taskDispatchKeyArgs(k), after)
	rows, err := s.db.QueryContext(ctx, `SELECT id,operation_id,generation,event_type,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,at_utc,detail FROM r10_task_lock_event WHERE `+taskDispatchWhere+` AND id>? ORDER BY id LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []td.Event{}
	for rows.Next() {
		event := td.Event{Key: k}
		var at string
		if err = rows.Scan(&event.ID, &event.OperationID, &event.Generation, &event.Type, &event.ActorKind, &event.ActorUserLC, &event.ActorAgentID, &event.ActorOwnerLC, &at, &event.Detail); err != nil {
			return nil, err
		}
		event.At, err = taskDispatchParseTime(at)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func scanTaskDispatchNotice(row taskDispatchScanner) (*td.Notice, error) {
	var n td.Notice
	var created, completed string
	err := row.Scan(&n.ID, &n.OperationID, &n.Key.Provider, &n.Key.Instance, &n.Key.Repository, &n.Key.ChangeDir, &n.Key.TaskID,
		&n.Generation, &n.RecipientKind, &n.RecipientID, &n.RecipientOwnerLC, &n.Type, &n.State, &n.SourceEnvelopeID, &n.DeliveryID, &created, &completed, &n.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if n.CreatedAt, err = taskDispatchParseTime(created); err != nil {
		return nil, err
	}
	n.CompletedAt, err = taskDispatchParseTime(completed)
	return &n, err
}

func (s *SQLite) TaskDispatchNotices(ctx context.Context, state string, limit int) ([]td.Notice, error) {
	if limit < 1 || limit > 1000 {
		return nil, td.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskDispatchNoticeColumns+` FROM r10_task_lock_outbox WHERE delivery_state=? ORDER BY created_at_utc,id LIMIT ?`, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []td.Notice{}
	for rows.Next() {
		n, e := scanTaskDispatchNotice(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *SQLite) TaskDispatchNotice(ctx context.Context, id string) (*td.Notice, error) {
	if id == "" || len(id) > 128 {
		return nil, td.ErrInvalid
	}
	return scanTaskDispatchNotice(s.db.QueryRowContext(ctx, `SELECT `+taskDispatchNoticeColumns+` FROM r10_task_lock_outbox WHERE id=?`, id))
}

func (s *SQLite) TaskDispatchOperation(ctx context.Context, k td.Key, id string) (*td.Operation, error) {
	if k.Validate() != nil || !td.ValidOperation(id) {
		return nil, td.ErrInvalid
	}
	op := td.Operation{ID: id, Key: k}
	var result, created, completed string
	args := append([]any{id}, taskDispatchKeyArgs(k)...)
	err := s.db.QueryRowContext(ctx, `SELECT operation_kind,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,result_json,created_at_utc,completed_at_utc FROM r10_task_lock_operation WHERE operation_id=? AND `+taskDispatchWhere, args...).Scan(&op.Kind, &op.ActorKind, &op.ActorUserLC, &op.ActorAgentID, &op.ActorOwnerLC, &result, &created, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(result), &op.Result); err != nil {
		return nil, err
	}
	if op.CreatedAt, err = taskDispatchParseTime(created); err != nil {
		return nil, err
	}
	op.CompletedAt, err = taskDispatchParseTime(completed)
	return &op, err
}
