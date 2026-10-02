package identity

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// PGStore 是 Store 的 PG 实现（组长台账 43342da4：新领域在自己包内给 PG 实现）。
// 约束：只经 internal/repo 取连接（不出现库句柄）；表结构见 pgschema.sql，由 G2 的迁移账本登记；
// 语义与 MemStore 逐项相同，由同一套 identitytest.Run 契约钉住。
type PGStore struct {
	pool *repo.Pool
	t    repo.AccessCtx
}

var _ Store = (*PGStore)(nil)

// PGSchemaSQL 本包的建表语句（幂等）。迁移账本据此登记；测试也用它在临时库里建表。
//
//go:embed pgschema.sql
var PGSchemaSQL string

// NewPGStore 基于 G2 提供的连接池建存储。
// 身份数据是平台级、不属任何租户，故用系统上下文；全包只在这一处取（边界闸「显式系统上下文调用点」+1，须评审）。
func NewPGStore(pool *repo.Pool) *PGStore {
	return &PGStore{pool: pool, t: repo.SystemCtx()}
}

// pgRequiredTables 身份存储要用的表：本包 pgschema.sql 的八张，加复用的 audit_log（由 G2 的迁移账本建）。
var pgRequiredTables = []string{"np_user", "np_identity", "np_registration_code", "np_session", "np_agent", "np_enroll_application", "np_group", "np_group_member", "audit_log"}

// CheckSchema 表都在才返回 nil；缺表时报出缺哪几张。本包不自行建表 —— 建表归迁移账本，
// Hub 与初始化脚本启动时据此拒绝在没建好表的库上运行，不在半套表上写入。
func (s *PGStore) CheckSchema(ctx context.Context) error {
	var missing []string
	for _, tbl := range pgRequiredTables {
		row, err := s.queryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, tbl)
		if err != nil {
			return err
		}
		var ok bool
		if err := row.Scan(&ok); err != nil {
			return err
		}
		if !ok {
			missing = append(missing, tbl)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("identity: PG schema not applied, missing tables: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ── 小工具 ────────────────────────────────────────────────────────────────

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func fromNull(nt sql.NullTime) time.Time {
	if !nt.Valid {
		return time.Time{}
	}
	return nt.Time.UTC()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// pgCode 取 PG 错误码与约束名（非 PG 错误返回空串）。
func pgCode(err error) (code, constraint string) {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code, pe.ConstraintName
	}
	return "", ""
}

const (
	pgUniqueViolation = "23505"
	pgFKViolation     = "23503"
)

func noRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// rowScanner 同时适配 *sql.Row 与 *sql.Rows。
type rowScanner interface{ Scan(dest ...any) error }

const userCols = `user_id, short_name, display_name, type, state, password_hash, must_change_password, created_at`

func scanUser(r rowScanner) (*User, error) {
	var u User
	var typ, st string
	if err := r.Scan(&u.UserID, &u.ShortName, &u.DisplayName, &typ, &st, &u.PasswordHash, &u.MustChangePassword, &u.CreatedAt); err != nil {
		return nil, err
	}
	u.Type, u.State, u.CreatedAt = UserType(typ), UserState(st), u.CreatedAt.UTC()
	return &u, nil
}

const agentCols = `id, owner_lc, name, group_code, origin, state, coalesce(credential_hash, ''), created_at, revoked_at, revoked_by, suspended_at, suspended_by`

func scanAgent(r rowScanner) (*Agent, error) {
	var a Agent
	var origin, st string
	var revokedAt, suspendedAt sql.NullTime
	if err := r.Scan(&a.ID, &a.OwnerLC, &a.Name, &a.GroupID, &origin, &st, &a.CredentialHash, &a.CreatedAt, &revokedAt, &a.RevokedBy, &suspendedAt, &a.SuspendedBy); err != nil {
		return nil, err
	}
	a.Origin, a.State, a.CreatedAt, a.RevokedAt = AgentOrigin(origin), AgentState(st), a.CreatedAt.UTC(), fromNull(revokedAt)
	a.SuspendedAt = fromNull(suspendedAt)
	return &a, nil
}

const appCols = `id, owner_lc, pairing_salt, pairing_digest, poll_token_hash, device_fingerprint, client_version, source_ip,
	requested_at, expires_at, state, pairing_failures, coalesce(agent_id, ''), credential_taken_at`

func scanApp(r rowScanner) (*Application, error) {
	var a Application
	var st string
	var taken sql.NullTime
	if err := r.Scan(&a.ID, &a.OwnerLC, &a.PairingSalt, &a.PairingDigest, &a.PollTokenHash, &a.DeviceFingerprint, &a.ClientVersion, &a.SourceIP,
		&a.RequestedAt, &a.ExpiresAt, &st, &a.PairingFailures, &a.AgentID, &taken); err != nil {
		return nil, err
	}
	a.State, a.RequestedAt, a.ExpiresAt, a.CredentialTakenAt = AppState(st), a.RequestedAt.UTC(), a.ExpiresAt.UTC(), fromNull(taken)
	return &a, nil
}

func (s *PGStore) queryRow(ctx context.Context, q string, args ...any) (*sql.Row, error) {
	return s.pool.QueryRow(ctx, s.t, q, args...)
}

func (s *PGStore) inTx(ctx context.Context, fn func(repo.Tx) error) error {
	return s.pool.InTx(ctx, s.t, nil, fn)
}

// ── IdentityStore ──────────────────────────────────────────────────────────

func (s *PGStore) CountUsers(ctx context.Context) (int, error) {
	row, err := s.queryRow(ctx, `SELECT count(*) FROM np_user`)
	if err != nil {
		return 0, err
	}
	var n int
	return n, row.Scan(&n)
}

const insertUserSQL = `INSERT INTO np_user (user_id_lc, user_id, short_name, short_name_lc, display_name, type, state,
		password_hash, must_change_password, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

func insertUserArgs(u User) []any {
	return []any{u.UserIDLC(), u.UserID, u.ShortName, u.ShortNameLC(), u.DisplayName, string(u.Type), string(u.State),
		u.PasswordHash, u.MustChangePassword, u.CreatedAt.UTC()}
}

func (s *PGStore) CreateUser(ctx context.Context, u User) error {
	_, err := s.pool.Exec(ctx, s.t, insertUserSQL, insertUserArgs(u)...)
	if code, _ := pgCode(err); code == pgUniqueViolation {
		return ErrConflict
	}
	return err
}

// CreateFirstAdmin 表锁（SHARE ROW EXCLUSIVE 与自身互斥）把「查有无管理用户 ⇒ 插入」串成一步：
// 两个初始化脚本同时执行，后到的等前一个提交后再查，必然看到已有管理用户。
func (s *PGStore) CreateFirstAdmin(ctx context.Context, u User) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE np_user IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM np_user WHERE type IN ('platform_admin', 'manager'))`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrAdminExists
		}
		_, err := tx.Exec(ctx, insertUserSQL, insertUserArgs(u)...)
		if code, _ := pgCode(err); code == pgUniqueViolation {
			return ErrConflict
		}
		return err
	})
}

func (s *PGStore) getUserBy(ctx context.Context, col, val string) (*User, error) {
	row, err := s.queryRow(ctx, `SELECT `+userCols+` FROM np_user WHERE `+col+` = $1`, Lower(val))
	if err != nil {
		return nil, err
	}
	u, err := scanUser(row)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PGStore) GetUser(ctx context.Context, userIDLC string) (*User, error) {
	return s.getUserBy(ctx, "user_id_lc", userIDLC)
}

func (s *PGStore) GetUserByShortName(ctx context.Context, shortNameLC string) (*User, error) {
	return s.getUserBy(ctx, "short_name_lc", shortNameLC)
}

func (s *PGStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT `+userCols+` FROM np_user ORDER BY user_id_lc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *PGStore) SetUserState(ctx context.Context, userIDLC string, state UserState, now time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `UPDATE np_user SET state = $2 WHERE user_id_lc = $1`, Lower(userIDLC), string(state))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if state == UserDisabled {
			_, err = tx.Exec(ctx, `UPDATE np_session SET revoked_at = $2 WHERE user_id_lc = $1 AND revoked_at IS NULL`, Lower(userIDLC), now.UTC())
		}
		return err
	})
}

func (s *PGStore) SetPassword(ctx context.Context, userIDLC, hash string, mustChange bool) error {
	res, err := s.pool.Exec(ctx, s.t, `UPDATE np_user SET password_hash = $2, must_change_password = $3 WHERE user_id_lc = $1`,
		Lower(userIDLC), hash, mustChange)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGStore) ResetPassword(ctx context.Context, userIDLC, hash string, now time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `UPDATE np_user SET password_hash = $2, must_change_password = true WHERE user_id_lc = $1`, Lower(userIDLC), hash)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `UPDATE np_session SET revoked_at = $2 WHERE user_id_lc = $1 AND revoked_at IS NULL`, Lower(userIDLC), now.UTC())
		return err
	})
}

func (s *PGStore) ResolveIdentity(ctx context.Context, ident Identity) (*User, error) {
	row, err := s.queryRow(ctx, `SELECT u.user_id, u.short_name, u.display_name, u.type, u.state, u.password_hash, u.must_change_password, u.created_at
		FROM np_identity i JOIN np_user u ON u.user_id_lc = i.user_id_lc
		WHERE i.provider = $1 AND i.issuer = $2 AND i.subject = $3`, ident.Provider, ident.Issuer, ident.Subject)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(row)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PGStore) CreateRegistrationCode(ctx context.Context, userIDLC, codeDigest string, expiresAt time.Time) error {
	lc := Lower(userIDLC)
	return s.inTx(ctx, func(tx repo.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM np_user WHERE user_id_lc = $1`, lc).Scan(&one); err != nil {
			if noRows(err) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM np_registration_code WHERE user_id_lc = $1`, lc); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO np_registration_code (code_digest, user_id_lc, expires_at) VALUES ($1,$2,$3)`,
			codeDigest, lc, expiresAt.UTC())
		if code, _ := pgCode(err); code == pgUniqueViolation {
			return ErrConflict
		}
		return err
	})
}

func (s *PGStore) ListActiveRegistrationCodes(ctx context.Context, now time.Time) ([]RegistrationCodeRow, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT user_id_lc, code_digest, expires_at FROM np_registration_code
		WHERE redeemed_at IS NULL AND expires_at > $1 ORDER BY user_id_lc`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegistrationCodeRow
	for rows.Next() {
		var r RegistrationCodeRow
		if err := rows.Scan(&r.UserIDLC, &r.CodeDigest, &r.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PGStore) RedeemRegistrationCode(ctx context.Context, codeDigest string, ident Identity, now time.Time) (*User, error) {
	var out *User
	err := s.inTx(ctx, func(tx repo.Tx) error {
		var lc string
		var exp time.Time
		var redeemed sql.NullTime
		err := tx.QueryRow(ctx, `SELECT user_id_lc, expires_at, redeemed_at FROM np_registration_code WHERE code_digest = $1 FOR UPDATE`,
			codeDigest).Scan(&lc, &exp, &redeemed)
		if noRows(err) {
			return ErrCodeInvalid
		}
		if err != nil {
			return err
		}
		if redeemed.Valid || !now.Before(exp) {
			return ErrCodeInvalid
		}
		var one int
		err = tx.QueryRow(ctx, `SELECT 1 FROM np_identity WHERE provider = $1 AND issuer = $2 AND subject = $3`,
			ident.Provider, ident.Issuer, ident.Subject).Scan(&one)
		if err == nil {
			return ErrIdentityTaken
		}
		if !noRows(err) {
			return err
		}
		u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM np_user WHERE user_id_lc = $1 FOR UPDATE`, lc))
		if noRows(err) {
			return ErrCodeInvalid
		}
		if err != nil {
			return err
		}
		if u.State != UserPendingBind {
			return ErrNotPending
		}
		if _, err := tx.Exec(ctx, `UPDATE np_registration_code SET redeemed_at = $2 WHERE code_digest = $1`, codeDigest, now.UTC()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO np_identity (provider, issuer, subject, user_id_lc) VALUES ($1,$2,$3,$4)`,
			ident.Provider, ident.Issuer, ident.Subject, lc); err != nil {
			if code, _ := pgCode(err); code == pgUniqueViolation {
				return ErrIdentityTaken
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_user SET state = 'active' WHERE user_id_lc = $1`, lc); err != nil {
			return err
		}
		u.State = UserActive
		out = u
		return nil
	})
	return out, err
}

func (s *PGStore) CreateSession(ctx context.Context, se Session) error {
	_, err := s.pool.Exec(ctx, s.t, `INSERT INTO np_session (token_hash, user_id_lc, expires_at, revoked_at) VALUES ($1,$2,$3,$4)`,
		se.TokenHash, Lower(se.UserIDLC), se.ExpiresAt.UTC(), nullTime(se.RevokedAt))
	switch code, _ := pgCode(err); code {
	case pgUniqueViolation:
		return ErrConflict
	case pgFKViolation:
		return ErrNotFound
	}
	return err
}

func (s *PGStore) ResolveSession(ctx context.Context, tokenHash string, now time.Time) (*Session, *User, error) {
	row, err := s.queryRow(ctx, `SELECT s.token_hash, s.user_id_lc, s.expires_at,
		u.user_id, u.short_name, u.display_name, u.type, u.state, u.password_hash, u.must_change_password, u.created_at
		FROM np_session s JOIN np_user u ON u.user_id_lc = s.user_id_lc
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > $2 AND u.state = 'active'`, tokenHash, now.UTC())
	if err != nil {
		return nil, nil, err
	}
	var se Session
	var u User
	var typ, st string
	err = row.Scan(&se.TokenHash, &se.UserIDLC, &se.ExpiresAt, &u.UserID, &u.ShortName, &u.DisplayName, &typ, &st,
		&u.PasswordHash, &u.MustChangePassword, &u.CreatedAt)
	if noRows(err) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	se.ExpiresAt, u.Type, u.State, u.CreatedAt = se.ExpiresAt.UTC(), UserType(typ), UserState(st), u.CreatedAt.UTC()
	return &se, &u, nil
}

// AppendAudit 写入既有 audit_log（复用，组长认可的建表建议）；Detail 放进 after_json。
func (s *PGStore) AppendAudit(ctx context.Context, e AuditEvent) error {
	detail, _ := json.Marshal(map[string]string{"detail": e.Detail})
	_, err := s.pool.Exec(ctx, s.t, `INSERT INTO audit_log (actor, action, target, after_json, ts) VALUES ($1,$2,$3,$4,$5)`,
		e.Actor, e.Action, e.Target, string(detail), e.At.UTC())
	return err
}

// ── EnrollStore ────────────────────────────────────────────────────────────

func (s *PGStore) CreateApplication(ctx context.Context, a Application) error {
	st := a.State
	if st == "" {
		st = AppPending
	}
	_, err := s.pool.Exec(ctx, s.t, `INSERT INTO np_enroll_application (id, owner_lc, pairing_salt, pairing_digest, poll_token_hash,
		device_fingerprint, client_version, source_ip, requested_at, expires_at, state, pairing_failures, agent_id, credential_taken_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		a.ID, Lower(a.OwnerLC), a.PairingSalt, a.PairingDigest, a.PollTokenHash, a.DeviceFingerprint, a.ClientVersion, a.SourceIP,
		a.RequestedAt.UTC(), a.ExpiresAt.UTC(), string(st), a.PairingFailures, nullStr(a.AgentID), nullTime(a.CredentialTakenAt))
	if code, _ := pgCode(err); code == pgUniqueViolation {
		return ErrConflict
	}
	return err
}

func (s *PGStore) GetApplication(ctx context.Context, appID string) (*Application, error) {
	row, err := s.queryRow(ctx, `SELECT `+appCols+` FROM np_enroll_application WHERE id = $1`, appID)
	if err != nil {
		return nil, err
	}
	a, err := scanApp(row)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *PGStore) ListPendingApplications(ctx context.Context, ownerLC string, now time.Time) ([]Application, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT `+appCols+` FROM np_enroll_application
		WHERE owner_lc = $1 AND state = 'pending' AND expires_at > $2 ORDER BY requested_at, id`, Lower(ownerLC), now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// lockPendingApp 在事务里锁住属于 owner、当前仍 pending 的申请；不属于 owner 一律按不存在处理。
func lockPendingApp(ctx context.Context, tx repo.Tx, appID, ownerLC string, now time.Time) (*Application, error) {
	a, err := scanApp(tx.QueryRow(ctx, `SELECT `+appCols+` FROM np_enroll_application WHERE id = $1 FOR UPDATE`, appID))
	if noRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.OwnerLC != Lower(ownerLC) {
		return nil, ErrNotFound
	}
	switch a.EffectiveState(now) {
	case AppPending:
		return a, nil
	case AppExpired:
		return nil, ErrExpired
	default:
		return nil, ErrNotPending
	}
}

func (s *PGStore) RecordPairingFailure(ctx context.Context, appID, ownerLC string, max int, now time.Time) (int, bool, error) {
	var remaining int
	var rejected bool
	err := s.inTx(ctx, func(tx repo.Tx) error {
		a, err := lockPendingApp(ctx, tx, appID, ownerLC, now)
		if err != nil {
			return err
		}
		failures := a.PairingFailures + 1
		remaining = max - failures
		st := AppPending
		if remaining <= 0 {
			remaining, rejected, st = 0, true, AppRejected
		}
		_, err = tx.Exec(ctx, `UPDATE np_enroll_application SET pairing_failures = $2, state = $3 WHERE id = $1`, appID, failures, string(st))
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return remaining, rejected, nil
}

// insertAgent 插一行 Agent；主键冲突 ⇒ ErrConflict，同 owner 未吊销同名 ⇒ ErrNameTaken。
func insertAgent(ctx context.Context, tx repo.Tx, ag Agent) error {
	_, err := tx.Exec(ctx, `INSERT INTO np_agent (id, owner_lc, name, name_lc, group_code, origin, state, credential_hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ag.ID, Lower(ag.OwnerLC), ag.Name, ag.NameLC(), ag.GroupID, string(ag.Origin), string(ag.State), nullStr(ag.CredentialHash), ag.CreatedAt.UTC())
	if code, constraint := pgCode(err); code == pgUniqueViolation {
		if constraint == "np_agent_owner_name_current" {
			return ErrNameTaken
		}
		return ErrConflict
	}
	return err
}

func (s *PGStore) Approve(ctx context.Context, in ApproveInput, now time.Time) (*Agent, error) {
	var out *Agent
	err := s.inTx(ctx, func(tx repo.Tx) error {
		a, err := lockPendingApp(ctx, tx, in.AppID, in.OwnerLC, now)
		if err != nil {
			return err
		}
		ag := Agent{ID: in.AgentID, OwnerLC: a.OwnerLC, Name: in.Name, GroupID: in.GroupID, Origin: OriginSelfhost, State: AgentActive, CreatedAt: now.UTC()}
		if err := insertAgent(ctx, tx, ag); err != nil {
			return err
		}
		if err := groupChoiceTx(ctx, tx, ag.ID, a.OwnerLC, in.GroupID, in.NewGroup, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_enroll_application SET state = 'approved', agent_id = $2 WHERE id = $1`, a.ID, ag.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_enroll_application SET state = 'superseded'
			WHERE owner_lc = $1 AND device_fingerprint = $2 AND id <> $3 AND state = 'pending' AND expires_at > $4`,
			a.OwnerLC, a.DeviceFingerprint, a.ID, now.UTC()); err != nil {
			return err
		}
		out = &ag
		return nil
	})
	return out, err
}

func (s *PGStore) Reject(ctx context.Context, appID, ownerLC string, now time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		if _, err := lockPendingApp(ctx, tx, appID, ownerLC, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE np_enroll_application SET state = 'rejected' WHERE id = $1`, appID)
		return err
	})
}

func (s *PGStore) TakeCredential(ctx context.Context, appID, credentialHash string, now time.Time) (*Agent, bool, error) {
	var out *Agent
	var took bool
	err := s.inTx(ctx, func(tx repo.Tx) error {
		a, err := scanApp(tx.QueryRow(ctx, `SELECT `+appCols+` FROM np_enroll_application WHERE id = $1 FOR UPDATE`, appID))
		if noRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if a.State != AppApproved || !a.CredentialTakenAt.IsZero() || !now.Before(a.ExpiresAt) || a.AgentID == "" {
			return nil
		}
		ag, err := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id = $1 FOR UPDATE`, a.AgentID))
		if noRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if ag.State != AgentActive {
			return nil
		}
		// 覆盖写入 ⇒ 旧凭据哈希随之作废（凭据轮换）。
		if _, err := tx.Exec(ctx, `UPDATE np_agent SET credential_hash = $2 WHERE id = $1`, ag.ID, credentialHash); err != nil {
			if code, _ := pgCode(err); code == pgUniqueViolation {
				return ErrConflict
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_enroll_application SET credential_taken_at = $2 WHERE id = $1`, appID, now.UTC()); err != nil {
			return err
		}
		ag.CredentialHash = credentialHash
		out, took = ag, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, took, nil
}

func (s *PGStore) agentBy(ctx context.Context, where string, args ...any) (*Agent, error) {
	row, err := s.queryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	ag, err := scanAgent(row)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return ag, err
}

func (s *PGStore) ResolveAgentByCredential(ctx context.Context, credentialHash string) (*Agent, error) {
	return s.agentBy(ctx, `credential_hash = $1`, credentialHash)
}

func (s *PGStore) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	return s.agentBy(ctx, `id = $1`, agentID)
}

func (s *PGStore) FindAgentByName(ctx context.Context, ownerLC, nameLC string) (*Agent, error) {
	return s.agentBy(ctx, `owner_lc = $1 AND name_lc = $2
		ORDER BY (state IN ('active', 'suspended')) DESC, revoked_at DESC NULLS LAST, id DESC LIMIT 1`, Lower(ownerLC), Lower(nameLC))
}

func (s *PGStore) ListAgents(ctx context.Context, ownerLC string) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT `+agentCols+` FROM np_agent
		WHERE owner_lc = $1 AND state IN ('active', 'suspended') ORDER BY name_lc, id`, Lower(ownerLC))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		ag, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ag)
	}
	return out, rows.Err()
}

func lockAgent(ctx context.Context, tx repo.Tx, agentID string) (*Agent, error) {
	ag, err := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id = $1 FOR UPDATE`, agentID))
	if noRows(err) {
		return nil, ErrNotFound
	}
	return ag, err
}

func revokeLocked(ctx context.Context, tx repo.Tx, agentID, by string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE np_agent SET state = 'revoked', revoked_at = $2, revoked_by = $3 WHERE id = $1`, agentID, now.UTC(), by)
	return err
}

func (s *PGStore) Revoke(ctx context.Context, agentID, ownerLC, by string, now time.Time) (bool, error) {
	var already bool
	err := s.inTx(ctx, func(tx repo.Tx) error {
		ag, err := lockAgent(ctx, tx, agentID)
		if err != nil {
			return err
		}
		switch {
		case ag.OwnerLC != Lower(ownerLC):
			return ErrForbidden
		case ag.Origin == OriginHosted:
			return ErrHosted
		case ag.State == AgentRevoked:
			already = true
			return nil
		}
		return revokeLocked(ctx, tx, agentID, by, now)
	})
	return already, err
}

func (s *PGStore) CreateHostedAgent(ctx context.Context, ag Agent, app Application, newGroup *Group) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		a := ag
		a.Origin, a.State, a.CredentialHash = OriginHosted, AgentActive, ""
		if err := insertAgent(ctx, tx, a); err != nil {
			return err
		}
		if err := groupChoiceTx(ctx, tx, a.ID, a.OwnerLC, a.GroupID, newGroup, a.CreatedAt); err != nil {
			return err
		}
		return insertApprovedApp(ctx, tx, app, Lower(ag.OwnerLC), ag.ID)
	})
}

func insertApprovedApp(ctx context.Context, tx repo.Tx, app Application, ownerLC, agentID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO np_enroll_application (id, owner_lc, pairing_salt, pairing_digest, poll_token_hash,
		device_fingerprint, client_version, source_ip, requested_at, expires_at, state, agent_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'approved',$11)`,
		app.ID, ownerLC, app.PairingSalt, app.PairingDigest, app.PollTokenHash, app.DeviceFingerprint, app.ClientVersion, app.SourceIP,
		app.RequestedAt.UTC(), app.ExpiresAt.UTC(), agentID)
	if code, _ := pgCode(err); code == pgUniqueViolation {
		return ErrConflict
	}
	return err
}

func (s *PGStore) AddHostedToken(ctx context.Context, agentID string, app Application) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		ag, err := lockAgent(ctx, tx, agentID)
		if err != nil {
			return err
		}
		if ag.Origin != OriginHosted {
			return ErrNotHosted
		}
		if ag.State != AgentActive {
			return ErrNotPending
		}
		return insertApprovedApp(ctx, tx, app, ag.OwnerLC, ag.ID)
	})
}

func (s *PGStore) RetireAgent(ctx context.Context, agentID, by string, now time.Time) (bool, error) {
	var already bool
	err := s.inTx(ctx, func(tx repo.Tx) error {
		ag, err := lockAgent(ctx, tx, agentID)
		if err != nil {
			return err
		}
		if ag.State == AgentRevoked {
			already = true
			return nil
		}
		return revokeLocked(ctx, tx, agentID, by, now)
	})
	return already, err
}

func (s *PGStore) SetAgentSuspended(ctx context.Context, agentID string, suspend bool, by string, now time.Time) (bool, error) {
	var already bool
	err := s.inTx(ctx, func(tx repo.Tx) error {
		ag, err := lockAgent(ctx, tx, agentID)
		if err != nil {
			return err
		}
		switch {
		case ag.Origin != OriginHosted:
			return ErrNotHosted
		case ag.State == AgentRevoked:
			return ErrNotPending
		case suspend && ag.State == AgentSuspended, !suspend && ag.State == AgentActive:
			already = true
			return nil
		}
		if suspend {
			_, err = tx.Exec(ctx, `UPDATE np_agent SET state = 'suspended', suspended_at = $2, suspended_by = $3 WHERE id = $1`, agentID, now.UTC(), by)
		} else {
			_, err = tx.Exec(ctx, `UPDATE np_agent SET state = 'active', suspended_at = NULL, suspended_by = '' WHERE id = $1`, agentID)
		}
		return err
	})
	return already, err
}

// ── GroupStore ─────────────────────────────────────────────────────────────

const groupCols = `code, short_name, creator_lc, leader_agent_id, additions_closed, created_at, updated_at`

func scanGroup(r rowScanner) (*Group, error) {
	var g Group
	if err := r.Scan(&g.Code, &g.ShortName, &g.CreatorLC, &g.LeaderAgentID, &g.AdditionsClosed, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	g.CreatedAt, g.UpdatedAt = g.CreatedAt.UTC(), g.UpdatedAt.UTC()
	return &g, nil
}

// lockGroup 取组并加行锁（share ⇒ FOR SHARE：与关闭添加、删组的写互斥，但多个入组可并行）。
func lockGroup(ctx context.Context, tx repo.Tx, code string, share bool) (*Group, error) {
	mode := "FOR UPDATE"
	if share {
		mode = "FOR SHARE"
	}
	g, err := scanGroup(tx.QueryRow(ctx, `SELECT `+groupCols+` FROM np_group WHERE code = $1 `+mode, code))
	if noRows(err) {
		return nil, ErrNotFound
	}
	return g, err
}

// ownedCurrentAgentShared Agent 属 ownerLC 且未吊销；FOR SHARE 挡住同时进行的吊销（吊销取 FOR UPDATE）。
func ownedCurrentAgentShared(ctx context.Context, tx repo.Tx, agentID, ownerLC string) (bool, error) {
	ag, err := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id = $1 FOR SHARE`, agentID))
	if noRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ag.OwnerLC == Lower(ownerLC) && ag.Current(), nil
}

func (s *PGStore) CreateGroup(ctx context.Context, g Group) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		ok, err := ownedCurrentAgentShared(ctx, tx, g.LeaderAgentID, g.CreatorLC)
		if err != nil {
			return err
		}
		if !ok {
			return ErrGroupAgentUnavailable
		}
		_, err = tx.Exec(ctx, `INSERT INTO np_group (`+groupCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			g.Code, g.ShortName, Lower(g.CreatorLC), g.LeaderAgentID, g.AdditionsClosed, g.CreatedAt.UTC(), g.UpdatedAt.UTC())
		if code, constraint := pgCode(err); code == pgUniqueViolation {
			if constraint == "np_group_pkey" {
				return ErrConflict
			}
			return ErrGroupNameTaken
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO np_group_member (code, agent_id, created_at) VALUES ($1,$2,$3)`, g.Code, g.LeaderAgentID, g.CreatedAt.UTC())
		return err
	})
}

func (s *PGStore) GetGroup(ctx context.Context, code string) (*Group, error) {
	row, err := s.queryRow(ctx, `SELECT `+groupCols+` FROM np_group WHERE code = $1`, code)
	if err != nil {
		return nil, err
	}
	g, err := scanGroup(row)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *PGStore) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT `+groupCols+` FROM np_group ORDER BY created_at, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Group, 0)
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (s *PGStore) ListGroupMembers(ctx context.Context, code string) ([]GroupMember, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT code, agent_id, created_at FROM np_group_member WHERE code = $1 ORDER BY created_at, agent_id`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GroupMember, 0)
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.Code, &m.AgentID, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.CreatedAt = m.CreatedAt.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PGStore) ListAgentGroups(ctx context.Context, agentID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, s.t, `SELECT code FROM np_group_member WHERE agent_id = $1 ORDER BY code`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PGStore) JoinGroup(ctx context.Context, code, agentID, ownerLC string, now time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		g, err := lockGroup(ctx, tx, code, true)
		if err != nil {
			return err
		}
		if g.AdditionsClosed {
			return ErrGroupClosed
		}
		ok, err := ownedCurrentAgentShared(ctx, tx, agentID, ownerLC)
		if err != nil {
			return err
		}
		if !ok {
			return ErrGroupAgentUnavailable
		}
		res, err := tx.Exec(ctx, `INSERT INTO np_group_member (code, agent_id, created_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, code, agentID, now.UTC())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrGroupMemberExists
		}
		return nil
	})
}

func (s *PGStore) LeaveGroup(ctx context.Context, code, agentID, actorLC string) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		g, err := lockGroup(ctx, tx, code, false)
		if err != nil {
			return err
		}
		var owner string
		err = tx.QueryRow(ctx, `SELECT a.owner_lc FROM np_group_member m JOIN np_agent a ON a.id = m.agent_id
			WHERE m.code = $1 AND m.agent_id = $2`, code, agentID).Scan(&owner)
		if noRows(err) {
			return ErrGroupMemberNotFound
		}
		if err != nil {
			return err
		}
		if agentID == g.LeaderAgentID {
			return ErrGroupLeaderProtected
		}
		if actor := Lower(actorLC); g.CreatorLC != actor && owner != actor {
			return ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM np_group_member WHERE code = $1 AND agent_id = $2`, code, agentID)
		return err
	})
}

// creatorOnly 对「建组人才可」的单条改写：改到 1 行即成；0 行时再分辨不存在与无权。
func (s *PGStore) creatorOnly(ctx context.Context, code string, res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetGroup(ctx, code); err != nil {
		return err
	}
	return ErrForbidden
}

func (s *PGStore) SetGroupAdditionsClosed(ctx context.Context, code, actorLC string, closed bool, now time.Time) error {
	res, err := s.pool.Exec(ctx, s.t, `UPDATE np_group SET additions_closed = $3, updated_at = $4 WHERE code = $1 AND creator_lc = $2`,
		code, Lower(actorLC), closed, now.UTC())
	return s.creatorOnly(ctx, code, res, err)
}

func (s *PGStore) DeleteGroup(ctx context.Context, code, actorLC string) error {
	res, err := s.pool.Exec(ctx, s.t, `DELETE FROM np_group WHERE code = $1 AND creator_lc = $2`, code, Lower(actorLC))
	return s.creatorOnly(ctx, code, res, err)
}

// groupChoiceTx 批准／托管新建事务内落地编组选择：新组 ⇒ 建组（该 Agent 为组长）＋组长入组；
// 已有组 ⇒ 行锁（FOR SHARE，与关闭添加、删组互斥）后判存在与未关闭 ⇒ 入组。
func groupChoiceTx(ctx context.Context, tx repo.Tx, agentID, ownerLC, code string, newGroup *Group, now time.Time) error {
	if code == "" && newGroup == nil {
		return nil // 不入组（业主令：编组不是必须的）
	}
	if newGroup != nil {
		_, err := tx.Exec(ctx, `INSERT INTO np_group (`+groupCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			newGroup.Code, newGroup.ShortName, Lower(newGroup.CreatorLC), agentID, false, now.UTC(), now.UTC())
		if c, constraint := pgCode(err); c == pgUniqueViolation {
			if constraint == "np_group_pkey" {
				return ErrConflict
			}
			return ErrGroupNameTaken
		}
		if err != nil {
			return err
		}
		code = newGroup.Code
	} else {
		g, err := lockGroup(ctx, tx, code, true)
		if errors.Is(err, ErrNotFound) {
			return ErrGroupNotFound
		}
		if err != nil {
			return err
		}
		usable, err := groupUsableByTx(ctx, tx, g, ownerLC)
		if err != nil {
			return err
		}
		if !usable {
			return ErrGroupNotFound // 别人的组与不存在同一答复，不暴露组存在
		}
		if g.AdditionsClosed {
			return ErrGroupClosed
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO np_group_member (code, agent_id, created_at) VALUES ($1,$2,$3)`, code, agentID, now.UTC())
	return err
}

// groupUsableByTx「本人可用」（组长令 2026-10-01，收紧 GroupAllowed）：本人建的组，或本人已有未吊销成员在内的组。
func groupUsableByTx(ctx context.Context, tx repo.Tx, g *Group, ownerLC string) (bool, error) {
	owner := Lower(ownerLC)
	if g.CreatorLC == owner {
		return true, nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM np_group_member m JOIN np_agent a ON a.id = m.agent_id
		WHERE m.code = $1 AND a.owner_lc = $2 AND a.state IN ('active', 'suspended'))`, g.Code, owner).Scan(&ok)
	return ok, err
}
