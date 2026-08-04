package datad

// PGStore 生产存储(设计冻结版 §2 DDL 原样)。集成测试:DATAD_TEST_PG_DSN 门控
// (无 PG 环境跑 MemStore 同套逻辑用例)。audit 纵深(§4):建议 datad 连接用户
// 对 audit 仅 INSERT——由 D4c 部署 SQL 落(GRANT 语句在 SchemaDDL 尾注释)。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const SchemaDDL = `
CREATE TABLE IF NOT EXISTS task (
  id TEXT PRIMARY KEY, title TEXT NOT NULL, owner_session TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN
    ('proposed','dispatched','in_progress','review','blocked','waiting_gate','done','archived')),
  anchor TEXT NOT NULL DEFAULT '', gate TEXT NOT NULL DEFAULT '',
  eta TEXT NOT NULL DEFAULT '', detail JSONB,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS dispatch (
  id BIGSERIAL PRIMARY KEY, task_id TEXT NOT NULL REFERENCES task(id),
  assignee TEXT NOT NULL, role TEXT NOT NULL CHECK (role IN ('executor','reviewer','advisor')),
  round INT NOT NULL DEFAULT 1, note TEXT NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS config (
  key TEXT NOT NULL, generation BIGINT NOT NULL, value JSONB NOT NULL,
  updated_by TEXT NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (key, generation));
CREATE TABLE IF NOT EXISTS session_state (
  session TEXT PRIMARY KEY,
  state TEXT NOT NULL CHECK (state IN ('active','idle','silent_notice','restarting','dead')),
  watermark_pct REAL, current_task TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS heartbeat (
  session TEXT NOT NULL, at timestamptz NOT NULL, payload JSONB,
  PRIMARY KEY (session, at));
CREATE TABLE IF NOT EXISTS skill (
  name TEXT NOT NULL, version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('proposed','approved','deprecated')),
  content TEXT NOT NULL, sha256 TEXT NOT NULL,
  proposed_by TEXT NOT NULL, approved_by TEXT NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (name, version));
CREATE TABLE IF NOT EXISTS audit (
  id BIGSERIAL PRIMARY KEY, actor TEXT NOT NULL, op TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '', detail JSONB,
  at timestamptz NOT NULL DEFAULT now());
-- D4c 部署纵深(prodops 以超级用户执行;datad_user=运行账号):
-- REVOKE UPDATE, DELETE ON audit FROM datad_user;  -- audit 仅 INSERT/SELECT(§4)
`

type PGStore struct{ db *sql.DB }

// OpenPG 连接+建表(幂等)。
func OpenPG(ctx context.Context, dsn string) (*PGStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, SchemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migrate: %w", err)
	}
	return &PGStore{db: db}, nil
}

func (p *PGStore) Close() error                   { return p.db.Close() }
func (p *PGStore) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	t := &Task{}
	var detail []byte
	err := row.Scan(&t.ID, &t.Title, &t.OwnerSession, &t.Status, &t.Anchor, &t.Gate,
		&t.ETA, &detail, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Detail = detail
	return t, nil
}

const taskCols = `id, title, owner_session, status, anchor, gate, eta, detail, created_at, updated_at`

func (p *PGStore) ListTasks(ctx context.Context, status, owner string) ([]Task, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+taskCols+` FROM task
  WHERE ($1='' OR status=$1) AND ($2='' OR owner_session=$2) ORDER BY id`, status, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (p *PGStore) GetTask(ctx context.Context, id string) (*Task, error) {
	return scanTask(p.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM task WHERE id=$1`, id))
}

func (p *PGStore) PutTask(ctx context.Context, t Task, ifMatch time.Time, force bool) (*Task, error) {
	if force {
		_, err := p.db.ExecContext(ctx, `INSERT INTO task(id,title,owner_session,status,anchor,gate,eta,detail)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8)
  ON CONFLICT(id) DO UPDATE SET title=$2,owner_session=$3,status=$4,anchor=$5,gate=$6,eta=$7,detail=$8,updated_at=now()`,
			t.ID, t.Title, t.OwnerSession, t.Status, t.Anchor, t.Gate, t.ETA, nullJSON(t.Detail))
		if err != nil {
			return nil, err
		}
		return p.GetTask(ctx, t.ID)
	}
	res, err := p.db.ExecContext(ctx, `UPDATE task SET title=$2,owner_session=$3,status=$4,anchor=$5,
  gate=$6,eta=$7,detail=$8,updated_at=now() WHERE id=$1 AND updated_at=$9`,
		t.ID, t.Title, t.OwnerSession, t.Status, t.Anchor, t.Gate, t.ETA, nullJSON(t.Detail), ifMatch)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	return p.GetTask(ctx, t.ID)
}

func (p *PGStore) UpdateProgress(ctx context.Context, id string, pr TaskProgress) (*Task, error) {
	prog := pr.Progress
	if prog == nil {
		prog = json.RawMessage("null")
	}
	res, err := p.db.ExecContext(ctx, `UPDATE task SET
  anchor=CASE WHEN $2<>'' THEN $2 ELSE anchor END,
  eta=CASE WHEN $3<>'' THEN $3 ELSE eta END,
  detail=CASE WHEN $4::jsonb IS NOT NULL AND $4::text<>'null'
         THEN jsonb_set(COALESCE(detail,'{}'::jsonb),'{progress}',$4::jsonb) ELSE detail END,
  updated_at=now() WHERE id=$1`, id, pr.Anchor, pr.ETA, string(prog))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return p.GetTask(ctx, id)
}

func (p *PGStore) AppendDispatch(ctx context.Context, d Dispatch) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO dispatch(task_id,assignee,role,round,note)
  VALUES($1,$2,$3,$4,$5)`, d.TaskID, d.Assignee, d.Role, d.Round, d.Note)
	return err
}

func (p *PGStore) GetConfig(ctx context.Context, key string, generation int64) (*ConfigEntry, error) {
	q := `SELECT key,generation,value,updated_by,updated_at FROM config WHERE key=$1`
	var row *sql.Row
	if generation > 0 {
		row = p.db.QueryRowContext(ctx, q+` AND generation=$2`, key, generation)
	} else {
		row = p.db.QueryRowContext(ctx, q+` ORDER BY generation DESC LIMIT 1`, key)
	}
	e := &ConfigEntry{}
	var val []byte
	err := row.Scan(&e.Key, &e.Generation, &val, &e.UpdatedBy, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Value = val
	return e, nil
}

func (p *PGStore) PutConfig(ctx context.Context, key string, value json.RawMessage, expectedGen int64, by string) (*ConfigEntry, error) {
	// CAS:INSERT 目标代次=expected+1,主键冲突或前代不符=412
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var cur int64
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(generation),0) FROM config WHERE key=$1`, key).Scan(&cur)
	if err != nil {
		return nil, err
	}
	if cur != expectedGen {
		return nil, ErrCASFailed
	}
	e := &ConfigEntry{Key: key, Generation: cur + 1, Value: value, UpdatedBy: by, UpdatedAt: time.Now().UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO config(key,generation,value,updated_by,updated_at)
  VALUES($1,$2,$3,$4,$5)`, key, e.Generation, string(value), by, e.UpdatedAt); err != nil {
		return nil, ErrCASFailed // 并发同代次插入=主键冲突,归 412
	}
	return e, tx.Commit()
}

func (p *PGStore) PutSessionState(ctx context.Context, s SessionState) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO session_state(session,state,watermark_pct,current_task,note,updated_at)
  VALUES($1,$2,$3,$4,$5,now())
  ON CONFLICT(session) DO UPDATE SET state=$2,watermark_pct=$3,current_task=$4,note=$5,updated_at=now()`,
		s.Session, s.State, s.WatermarkPct, s.CurrentTask, s.Note)
	return err
}

func (p *PGStore) ListSessions(ctx context.Context) ([]SessionState, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT session,state,COALESCE(watermark_pct,0),current_task,note,updated_at FROM session_state ORDER BY session`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionState
	for rows.Next() {
		var s SessionState
		if err := rows.Scan(&s.Session, &s.State, &s.WatermarkPct, &s.CurrentTask, &s.Note, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *PGStore) AppendHeartbeat(ctx context.Context, session string, payload json.RawMessage, at time.Time) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO heartbeat(session,at,payload) VALUES($1,$2,$3)
  ON CONFLICT DO NOTHING`, session, at, nullJSON(payload))
	return err
}

func (p *PGStore) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT name,version,status,content,sha256,proposed_by,approved_by FROM skill ORDER BY name,version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.Name, &s.Version, &s.Status, &s.Content, &s.SHA256, &s.ProposedBy, &s.ApprovedBy); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *PGStore) ProposeSkill(ctx context.Context, s Skill) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO skill(name,version,status,content,sha256,proposed_by)
  VALUES($1,$2,'proposed',$3,$4,$5)`, s.Name, s.Version, s.Content, s.SHA256, s.ProposedBy)
	return err
}

func (p *PGStore) SetSkillStatus(ctx context.Context, name, version, status, by string) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE skill SET status=$3, approved_by=$4 WHERE name=$1 AND version=$2`, name, version, status, by)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PGStore) AppendAudit(ctx context.Context, e AuditEntry) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO audit(actor,op,target,detail,at)
  VALUES($1,$2,$3,$4,$5)`, e.Actor, e.Op, e.Target, nullJSON(e.Detail), e.At)
	return err
}

func (p *PGStore) ListAudit(ctx context.Context, since time.Time, limit int) ([]AuditEntry, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT actor,op,target,COALESCE(detail,'null'),at FROM audit
  WHERE at>$1 ORDER BY at LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var d []byte
		if err := rows.Scan(&e.Actor, &e.Op, &e.Target, &d, &e.At); err != nil {
			return nil, err
		}
		e.Detail = d
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
