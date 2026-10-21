package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"time"
)

func readClarificationPolicy(ctx context.Context, db taskQuery, spec string) (ClarificationPolicy, error) {
	p := ClarificationPolicy{SpecID: spec}
	err := db.QueryRowContext(ctx, `SELECT pm_user,deputy_user,version,updated_by,updated_at FROM spec_clarification_policy WHERE spec_id=?`, spec).Scan(&p.PMUser, &p.DeputyUser, &p.Version, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}

// Internal caller must authorize before returning this snapshot to a client.
func (s *SQLite) ClarificationPolicy(ctx context.Context, spec string) (ClarificationPolicy, error) {
	return readClarificationPolicy(ctx, s.db, spec)
}

// SetClarificationPolicy is called under the current PG user/binding locks.
// Owner may select the PM; only that current PM may appoint/revoke a deputy.
// Changing PM atomically revokes the former PM and their deputy for new writes.
func (s *SQLite) SetClarificationPolicy(ctx context.Context, expected QuestionContext, a BlockerActor, u ClarificationPolicyUpdate, check func() error, now time.Time) (ClarificationPolicy, error) {
	var out ClarificationPolicy
	u.PMUser = strings.ToLower(strings.TrimSpace(u.PMUser))
	u.DeputyUser = strings.ToLower(strings.TrimSpace(u.DeputyUser))
	if a.UserID == "" || a.AgentID != "" || check == nil || !validQuestionUser(u.PMUser) || !validQuestionUser(u.DeputyUser) || u.PMUser == u.DeputyUser && u.PMUser != "" || u.PMUser == "" && u.DeputyUser != "" {
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
	if !reflect.DeepEqual(c, expected) || c.Archived {
		return out, ErrTaskFlowConflict
	}
	old, err := readClarificationPolicy(ctx, tx, c.SpecID)
	if err != nil {
		return out, err
	}
	if old.Version != u.ExpectedVersion {
		return out, ErrTaskFlowConflict
	}
	if a.UserID != c.Owner && a.UserID != old.PMUser {
		return out, ErrTaskFlowPermission
	}
	if u.PMUser != old.PMUser {
		if a.UserID != c.Owner || u.DeputyUser != "" {
			return out, ErrTaskFlowPermission
		}
	} else if u.DeputyUser != old.DeputyUser && a.UserID != old.PMUser {
		return out, ErrTaskFlowPermission
	}
	out = ClarificationPolicy{SpecID: c.SpecID, PMUser: u.PMUser, DeputyUser: u.DeputyUser, Version: old.Version + 1, UpdatedBy: a.Key(), UpdatedAt: now.UTC().Format(time.RFC3339Nano)}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_policy(spec_id,pm_user,deputy_user,version,updated_by,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(spec_id) DO UPDATE SET pm_user=excluded.pm_user,deputy_user=excluded.deputy_user,version=excluded.version,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, out.SpecID, out.PMUser, out.DeputyUser, out.Version, out.UpdatedBy, out.UpdatedAt); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_policy_history(spec_id,version,pm_user,deputy_user,actor_id,created_at) VALUES(?,?,?,?,?,?)`, out.SpecID, out.Version, out.PMUser, out.DeputyUser, out.UpdatedBy, out.UpdatedAt); err != nil {
		return out, err
	}
	if err = questionAudit(ctx, tx, "", a.Key(), "clarification_policy", out.Version, out, out.UpdatedAt); err != nil {
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
