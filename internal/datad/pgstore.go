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
	"strconv"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
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
CREATE INDEX IF NOT EXISTS dispatch_task_id_idx ON dispatch(task_id);
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
-- M5 A-lite 增量(契约 61e5938 §1):task 列 + assignment 当代行 + 首果账
ALTER TABLE task ADD COLUMN IF NOT EXISTS deps JSONB;
ALTER TABLE task ADD COLUMN IF NOT EXISTS priority TEXT CHECK (priority IS NULL OR priority IN ('P0','P1','P2','P3'));
ALTER TABLE task ADD COLUMN IF NOT EXISTS is_critical BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS assignment (
  task_id TEXT NOT NULL REFERENCES task(id),
  role TEXT NOT NULL CHECK (role IN ('executor','reviewer')),
  assignee TEXT NOT NULL, generation BIGINT NOT NULL,
  active BOOLEAN NOT NULL DEFAULT true, request_id TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (task_id, role, generation));
-- 硬门①:当代唯一 executor/reviewer(partial-unique;历史代不受约束)
CREATE UNIQUE INDEX IF NOT EXISTS assignment_active_uniq ON assignment(task_id, role) WHERE active;
CREATE TABLE IF NOT EXISTS assign_first_result (
  request_id TEXT PRIMARY KEY, outcome TEXT NOT NULL, http_code INT NOT NULL,
  request_hash TEXT NOT NULL, body JSONB NOT NULL, task_id TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now());
-- D4c 部署纵深(prodops 以超级用户执行;datad_user=运行账号):见 deploy/datad-grants.sql
--   · REVOKE ALL ON assignment,assign_first_result FROM PUBLIC(防外部组件直写)
--   · GRANT SELECT,INSERT,UPDATE ON assignment TO datad_user(无 DELETE,历史代永留)
--   · GRANT SELECT,INSERT ON assign_first_result TO datad_user(首果账 append-only,无 UPDATE/DELETE)
--   · REVOKE UPDATE,DELETE ON audit FROM datad_user(audit 仅 INSERT/SELECT,§4)
-- N-A3 §5①"assign 是 assignment 唯一写路径":DB 层=上述 GRANT(挡 datad_user 之外角色);
--   代码层=assignment_grant_test.go TestAssignSoleWritePath(挡 datad_user 进程内新端点绕过)。
`

// PGStore 三池:db=常规应用池;assignDB=assign 角色池(N-A3 唯一 assignment 写路径);
// gcDB=首果 GC 角色池。dev 单池时三者同指。
type PGStore struct {
	db       *repo.Pool
	assignDB *repo.Pool
	gcDB     *repo.Pool
}

// dtx 是 datad 【尚未租户化】的统一占位上下文。
//
// 🔴 不要把它换成 repo.SystemCtx()。两者的区别是本次收编的要点:
//
//	SystemCtx = 这个操作本就与租户无关(建表、迁移、健康检查);
//	dtx       = 这个操作【本该有租户】,只是 datad 的表还没有租户列(领域 schema 归 SP7b)。
//	若把 dtx 写成 SystemCtx,判据一照样达标,却会留下一批「看起来已收编、实则全是系统调用」
//	的调用点 —— 用「与租户无关」冒充「已做隔离」,即本仓已归纳的「安全属性互相冒充」。
//	保留 dtx 是为了让这笔债【可清点、可棘轮】:grep dtx() 即知还欠多少处。
func dtx() repo.TenantCtx {
	return repo.PendingTenantCtx("datad 领域表尚无租户列,待 SP7b 各领域 schema 落地")
}

// OpenPG 连接+建表(幂等)。assign/GC 池未配时回落主池(dev/Mem 同构;生产 N-A3 要求
// 独立角色池,用 OpenPGPools)。
func OpenPG(ctx context.Context, dsn string) (*PGStore, error) {
	return OpenPGPools(ctx, dsn, "", "")
}

// OpenPGPools 三池分权(N-A3):dsn=常规应用池(datad_app 谱系,对 assignment/首果账无写权);
// assignDSN=assign 专用角色池(仅 AssignGeneration 单事务所需最小权限);gcDSN=首果 GC 角色池
// (仅删 assign_first_result,对 assignment 零写权)。assignDSN/gcDSN 空=回落主池(dev;生产
// 部署门要求三池三角色,见 deploy/datad-grants.sql+N-A3 真 PG 证据)。
// 独立池=池内连接固定该角色登录,无 SET ROLE,故事务后无 role 泄漏面(池间凭据隔离)。
func OpenPGPools(ctx context.Context, dsn, assignDSN, gcDSN string) (*PGStore, error) {
	// 建库句柄一律由 repo 包创建(§22.2c #1);repo.OpenPG 内已含 Ping,故此处不再另 ping。
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		return nil, err
	}
	// 建表是 DDL,本就无租户语义 -> 显式 SystemCtx,不用 dtx。
	if _, err := pool.Exec(ctx, repo.SystemCtx(), SchemaDDL); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("schema migrate: %w", err)
	}
	p := &PGStore{db: pool, assignDB: pool, gcDB: pool}
	if assignDSN != "" {
		adb, err := repo.OpenPG(ctx, assignDSN)
		if err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("assign pool: %w", err)
		}
		p.assignDB = adb
	}
	if gcDSN != "" {
		gdb, err := repo.OpenPG(ctx, gcDSN)
		if err != nil {
			_ = p.Close()
			return nil, fmt.Errorf("gc pool: %w", err)
		}
		p.gcDB = gdb
	}
	return p, nil
}

func (p *PGStore) Close() error {
	var first error
	for _, d := range []*repo.Pool{p.assignDB, p.gcDB} {
		if d != nil && d != p.db {
			if err := d.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	if err := p.db.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

// Ping 健康检查:无租户语义,显式 SystemCtx。
func (p *PGStore) Ping(ctx context.Context) error { return p.db.Ping(ctx, repo.SystemCtx()) }

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
	rows, err := p.db.Query(ctx, dtx(), `SELECT `+taskCols+` FROM task
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
	row, err := p.db.QueryRow(ctx, dtx(), `SELECT `+taskCols+` FROM task WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	return scanTask(row)
}

func (p *PGStore) PutTask(ctx context.Context, t Task, ifMatch time.Time, force bool) (*Task, error) {
	// A10 N-A2(dev56 cycle2 B4 修):owner 校验与 task 写在**同一事务**内,并取与 Assign 相同的
	// per-task advisory 锁——消 check-then-write 窗:并发序 "PUT 读旧 executor A→Assign 提交新
	// executor B/owner=B→PUT 写 owner=A" 在共锁串行下不可能(PUT 的读必然见 Assign 提交后终态,
	// 或 PUT 先完成后 Assign 覆写=正常次序)。普通与 force/UPSERT 两路径同盖。
	// 事务改回调式(repo.InTx):*sql.Tx 因此无从逃出仓储包(§22.2c #1 后半句)。
	// 语义不变 —— fn 返回错误即回滚,返回 nil 即提交,与原「defer Rollback + 末尾 Commit」等价。
	var out *Task
	err := p.db.InTx(ctx, dtx(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('datad.assign:'||$1::text))`, t.ID); err != nil {
			return err
		}
		var execA sql.NullString
		if err := tx.QueryRow(ctx,
			`SELECT assignee FROM assignment WHERE task_id=$1 AND role='executor' AND active`, t.ID).Scan(&execA); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if execA.Valid && t.OwnerSession != execA.String {
			return ErrOwnerConflictPut // 拒且不改 task(回调返错=回滚,零写)
		}
		if force {
			if _, err := tx.Exec(ctx, `INSERT INTO task(id,title,owner_session,status,anchor,gate,eta,detail)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8)
  ON CONFLICT(id) DO UPDATE SET title=$2,owner_session=$3,status=$4,anchor=$5,gate=$6,eta=$7,detail=$8,updated_at=now()`,
				t.ID, t.Title, t.OwnerSession, t.Status, t.Anchor, t.Gate, t.ETA, nullJSON(t.Detail)); err != nil {
				return err
			}
		} else {
			res, err := tx.Exec(ctx, `UPDATE task SET title=$2,owner_session=$3,status=$4,anchor=$5,
  gate=$6,eta=$7,detail=$8,updated_at=now() WHERE id=$1 AND updated_at=$9`,
				t.ID, t.Title, t.OwnerSession, t.Status, t.Anchor, t.Gate, t.ETA, nullJSON(t.Detail), ifMatch)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrConflict
			}
		}
		var serr error
		out, serr = scanTask(tx.QueryRow(ctx, `SELECT `+taskCols+` FROM task WHERE id=$1`, t.ID))
		return serr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *PGStore) UpdateProgress(ctx context.Context, id string, pr TaskProgress) (*Task, error) {
	prog := pr.Progress
	if prog == nil {
		prog = json.RawMessage("null")
	}
	res, err := p.db.Exec(ctx, dtx(), `UPDATE task SET
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

func (p *PGStore) OwnerTransition(ctx context.Context, id, owner, to string) (*Task, error) {
	if to != "in_progress" && to != "review" { // 非法 to 在锁/事务前拒(与 Mem 时机统一,CR N4)
		return nil, ErrWrongState
	}
	// 闸依赖 Read Committed:RR 快照取在 advisory 锁等待前,锁释放后 NOT EXISTS
	// 按旧快照看不到并发已提交的 in_progress 行→闸静默穿透;显式钉 RC 不赌库默认。
	var t *Task
	err := p.db.InTx(ctx, dtx(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx repo.Tx) error {
		// 每次一项闸须原子:同 owner 并发拉两件,read-committed 下 NOT EXISTS 各自过检
		// →双 in_progress;per-owner advisory 锁串行化(锁随事务释放)。
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('datad.task.pull:'||$1::text))`, owner); err != nil {
			return err
		}
		// UPDATE..RETURNING 一步读回(CR n1:commit 后另读在事务外,可被并发覆盖)
		var row *sql.Row
		if to == "in_progress" {
			row = tx.QueryRow(ctx, `UPDATE task SET status='in_progress', updated_at=now()
  WHERE id=$1 AND owner_session=$2 AND status='dispatched'
  AND NOT EXISTS (SELECT 1 FROM task WHERE owner_session=$2 AND status='in_progress')
  RETURNING `+taskCols, id, owner)
		} else {
			row = tx.QueryRow(ctx, `UPDATE task SET status='review', updated_at=now()
  WHERE id=$1 AND owner_session=$2 AND status='in_progress'
  RETURNING `+taskCols, id, owner)
		}
		var serr error
		t, serr = scanTask(row)
		if errors.Is(serr, ErrNotFound) { // 0 行消歧:404/403/wrong_state/pull_gate
			var gotOwner, gotStatus string
			qerr := tx.QueryRow(ctx,
				`SELECT owner_session,status FROM task WHERE id=$1`, id).Scan(&gotOwner, &gotStatus)
			switch {
			case errors.Is(qerr, sql.ErrNoRows):
				return ErrNotFound
			case qerr != nil:
				return qerr
			case gotOwner != owner:
				return ErrNotOwner
			case to == "in_progress" && gotStatus == "dispatched":
				return ErrPullGate
			default:
				return ErrWrongState
			}
		}
		return serr
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (p *PGStore) PullLag(ctx context.Context) ([]PullLagRow, error) {
	rows, err := p.db.Query(ctx, dtx(), `SELECT t.id, t.title, t.owner_session,
  COALESCE(MAX(d.created_at), t.updated_at) AS since
  FROM task t LEFT JOIN dispatch d ON d.task_id=t.id AND d.role='executor'
  WHERE t.status='dispatched'
  GROUP BY t.id, t.title, t.owner_session, t.updated_at
  ORDER BY since, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PullLagRow{}
	for rows.Next() {
		var r PullLagRow
		if err := rows.Scan(&r.TaskID, &r.Title, &r.OwnerSession, &r.Since); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PGStore) AppendDispatch(ctx context.Context, d Dispatch) error {
	// id 序=提交序不变量(shenceclaude MC3):BIGSERIAL 分配序≠提交序,增量游标
	// since_id 依赖"id 单调可见";advisory xact 锁串行化 append(coordinator 单写
	// 面且低频,串行代价可忽略),锁随事务释放。
	return p.db.InTx(ctx, dtx(), nil, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('datad.dispatch.append'))`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO dispatch(task_id,assignee,role,round,note)
  VALUES($1,$2,$3,$4,$5)`, d.TaskID, d.Assignee, d.Role, d.Round, d.Note)
		return err
	})
}

func (p *PGStore) ListDispatches(ctx context.Context, taskID, assignee string, sinceID int64, limit int) ([]DispatchRow, error) {
	if limit <= 0 {
		limit = 1000 // 与 Mem 语义归一(N3):非法/缺省=默认上限,不是 0 行
	}
	q := `SELECT id,task_id,assignee,role,round,note,created_at FROM dispatch WHERE id>$1`
	args := []any{sinceID}
	if taskID != "" {
		q += ` AND task_id=$` + strconv.Itoa(len(args)+1)
		args = append(args, taskID)
	}
	if assignee != "" {
		q += ` AND assignee=$` + strconv.Itoa(len(args)+1)
		args = append(args, assignee)
	}
	q += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit)
	rows, err := p.db.Query(ctx, dtx(), q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DispatchRow{}
	for rows.Next() {
		var r DispatchRow
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Assignee, &r.Role, &r.Round, &r.Note, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PGStore) GetConfig(ctx context.Context, key string, generation int64) (*ConfigEntry, error) {
	q := `SELECT key,generation,value,updated_by,updated_at FROM config WHERE key=$1`
	var row *sql.Row
	var qerr error
	if generation > 0 {
		row, qerr = p.db.QueryRow(ctx, dtx(), q+` AND generation=$2`, key, generation)
	} else {
		row, qerr = p.db.QueryRow(ctx, dtx(), q+` ORDER BY generation DESC LIMIT 1`, key)
	}
	if qerr != nil {
		return nil, qerr
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

func (p *PGStore) ListConfig(ctx context.Context) ([]ConfigEntry, error) {
	// 每 key 最新代次:DISTINCT ON(key) ORDER BY key,generation DESC
	rows, err := p.db.Query(ctx, dtx(), `SELECT DISTINCT ON (key) key,generation,value,updated_by,updated_at
  FROM config ORDER BY key, generation DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigEntry
	for rows.Next() {
		e := ConfigEntry{}
		var val []byte
		if err := rows.Scan(&e.Key, &e.Generation, &val, &e.UpdatedBy, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Value = val
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PGStore) PutConfig(ctx context.Context, key string, value json.RawMessage, expectedGen int64, by string) (*ConfigEntry, error) {
	// CAS:INSERT 目标代次=expected+1,主键冲突或前代不符=412
	var e *ConfigEntry
	err := p.db.InTx(ctx, dtx(), nil, func(tx repo.Tx) error {
		var cur int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(generation),0) FROM config WHERE key=$1`, key).Scan(&cur); err != nil {
			return err
		}
		if cur != expectedGen {
			return ErrCASFailed
		}
		e = &ConfigEntry{Key: key, Generation: cur + 1, Value: value, UpdatedBy: by, UpdatedAt: time.Now().UTC()}
		if _, err := tx.Exec(ctx, `INSERT INTO config(key,generation,value,updated_by,updated_at)
  VALUES($1,$2,$3,$4,$5)`, key, e.Generation, string(value), by, e.UpdatedAt); err != nil {
			return ErrCASFailed // 并发同代次插入=主键冲突,归 412
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (p *PGStore) PutSessionState(ctx context.Context, s SessionState) error {
	_, err := p.db.Exec(ctx, dtx(), `INSERT INTO session_state(session,state,watermark_pct,current_task,note,updated_at)
  VALUES($1,$2,$3,$4,$5,now())
  ON CONFLICT(session) DO UPDATE SET state=$2,watermark_pct=$3,current_task=$4,note=$5,updated_at=now()`,
		s.Session, s.State, s.WatermarkPct, s.CurrentTask, s.Note)
	return err
}

func (p *PGStore) ListSessions(ctx context.Context) ([]SessionState, error) {
	rows, err := p.db.Query(ctx, dtx(),
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
	_, err := p.db.Exec(ctx, dtx(), `INSERT INTO heartbeat(session,at,payload) VALUES($1,$2,$3)
  ON CONFLICT DO NOTHING`, session, at, nullJSON(payload))
	return err
}

func (p *PGStore) LatestHeartbeats(ctx context.Context) ([]HeartbeatLatest, error) {
	// GROUP BY+MAX 走 PK(session,at) index-only 扫(N4:DISTINCT ON 混合序不吃索引);
	// 代价仍随 history 线性,真正解=retention(backlog),当前量级(会话数×心跳频)可承受。
	rows, err := p.db.Query(ctx, dtx(),
		`SELECT session, MAX(at) AS at FROM heartbeat GROUP BY session ORDER BY session`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HeartbeatLatest{}
	for rows.Next() {
		var h HeartbeatLatest
		if err := rows.Scan(&h.Session, &h.At); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (p *PGStore) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := p.db.Query(ctx, dtx(),
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
	_, err := p.db.Exec(ctx, dtx(), `INSERT INTO skill(name,version,status,content,sha256,proposed_by)
  VALUES($1,$2,'proposed',$3,$4,$5)`, s.Name, s.Version, s.Content, s.SHA256, s.ProposedBy)
	return err
}

func (p *PGStore) SetSkillStatus(ctx context.Context, name, version, status, by string) error {
	res, err := p.db.Exec(ctx, dtx(),
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
	_, err := p.db.Exec(ctx, dtx(), `INSERT INTO audit(actor,op,target,detail,at)
  VALUES($1,$2,$3,$4,$5)`, e.Actor, e.Op, e.Target, nullJSON(e.Detail), e.At)
	return err
}

func (p *PGStore) ListAudit(ctx context.Context, since time.Time, limit int) ([]AuditEntry, error) {
	rows, err := p.db.Query(ctx, dtx(), `SELECT actor,op,target,COALESCE(detail,'null'),at FROM audit
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
