package hosted

import (
	"context"
	"database/sql"

	"encoding/json"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"strings"
	"time"
)

// Store G4 托管存储（PG）。方法集覆盖 T-G4-01 的六张表＋审计；
// 编排与校验（配额/归属/联动）在上层（T-G4-30/40），本层只做忠实读写与约束内判定。

var (
	ErrNotFound     = errors.New("hosted: not found")
	ErrAccountBound = errors.New("hosted: account already bound") // 一席一账号冲突
	ErrInvalidState = errors.New("hosted: invalid state")
	ErrParams       = errors.New("hosted: 参数不合要求")
)

// htx hosted 域占位（与 datad 的 dtx／fanout 的 ftx 同型：PendingCtx 调用点单位，
// 供 pending-debt 按效果计数；SP7b 双轴化收口时本域归零）。
// 迁移（ApplySchema/DropSchema）用系统上下文——建表本就无租户语义（评审面，两处已登记入基线）。
func htx() repo.AccessCtx {
	return repo.PendingCtx("hosted", "SP7b", "hosted 域双轴化占位——user_lc 是否算租户列待 SP7b 定（已问 qwen38maxkhovvob4）")
}

type Store struct {
	pool *repo.Pool
	// beforeDisableCount 只供测试：停用事务已持 FOR UPDATE、尚未数引用时调用（确定性造「建席／停用」交错）。
	beforeDisableCount func()
	// afterSeatAccountLock 只供测试：建席事务已持账号行 FOR SHARE、尚未插席位行时调用。
	afterSeatAccountLock func()
}

func NewStore(pool *repo.Pool) *Store { return &Store{pool: pool} }

// ApplySchema 幂等建表（可重复 apply）。迁移＝建表无租户语义 ⇒ SystemCtx（评审面）。
func (s *Store) ApplySchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, repo.SystemCtx(), SchemaDDL)
	return err
}

// DropSchema 仅测试用（rollback 可重复性验证）。
func (s *Store) DropSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, repo.SystemCtx(), DropDDL)
	return err
}

// qrow 统一取行并如实上交错误（M1 返修：不得丢 QueryRow 的 error——
// htx 调用点若哪天构造失效，拒绝必须当场可见，不能推迟到 Scan 变成"扫不出行"）。
func (s *Store) qrow(ctx context.Context, q string, args ...any) (*sql.Row, error) {
	return s.pool.QueryRow(ctx, htx(), q, args...)
}

func (s *Store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.pool.Exec(ctx, htx(), q, args...)
}

func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.pool.Query(ctx, htx(), q, args...)
}

// ---------- 节点 ----------

type Node struct {
	ID, Name        string
	Labels          json.RawMessage
	State           string
	Capacity        json.RawMessage
	DockerVersion   string
	ProtocolVersion int
	LastSeenAt      *time.Time
}

func (s *Store) UpsertNode(ctx context.Context, n Node) error {
	_, err := s.exec(ctx, `
INSERT INTO g4_node (id, name, labels, state, docker_version, node_protocol_version)
VALUES ($1,$2,$3,'offline',$4,$5)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, labels=EXCLUDED.labels,
  docker_version=EXCLUDED.docker_version, node_protocol_version=EXCLUDED.node_protocol_version,
  updated_at=now()`,
		n.ID, n.Name, n.Labels, n.DockerVersion, n.ProtocolVersion)
	return err
}

func (s *Store) GetNode(ctx context.Context, id string) (Node, error) {
	var n Node
	err := func() error {

		row, err := s.qrow(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE id=$1`, id)

		if err != nil {

			return err

		}

		return row.Scan(
			&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)

	}()
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// SetNodeOnline 节点心跳：上线＋容量＋last_seen；离线由 Hub 探测断连后调 SetNodeState。
func (s *Store) SetNodeOnline(ctx context.Context, id string, capacity json.RawMessage) error {
	_, err := s.exec(ctx, `
UPDATE g4_node SET state='online', capacity=$2, last_seen_at=now(), updated_at=now() WHERE id=$1`, id, capacity)
	return err
}

func (s *Store) SetNodeState(ctx context.Context, id, state string) error {
	switch state {
	case "online", "offline":
	default:
		return ErrInvalidState
	}
	_, err := s.pool.Exec(ctx, htx(),
		`UPDATE g4_node SET state=$2, updated_at=now() WHERE id=$1`, id, state)
	return err
}

// PickNodeAuto 「自动」落位：online 节点中剩余内存最大者（组长定 da622016）。
// 容量 JSON 形如 {"mem_free_mb":N}；无 online 节点返回 ErrNotFound（上层译 node_no_capacity）。
func (s *Store) PickNodeAuto(ctx context.Context) (Node, error) {
	return s.PickNodeByPool(ctx, "")
}

// PickNodeByPoolModel 包 B2：只在上报清单含 model 的在线节点里选（余量最大）；
// 帧龄超 maxAge 视同下线（修订 4）。无候选 ⇒ ErrNotFound（编排层译 model_not_available）。
func (s *Store) PickNodeByPoolModel(ctx context.Context, pool, model string, maxAge time.Duration) (Node, error) {
	var n Node
	sqlStr := `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE state='online'
  AND capacity->'models' ? $1
  AND last_seen_at > now() - $2::interval`
	args := []any{model, maxAge.String()}
	if pool != "" {
		sqlStr += ` AND labels->>'pool' = $3`
		args = append(args, pool)
	}
	sqlStr += `
ORDER BY COALESCE((capacity->>'mem_free_mb')::bigint, 0) DESC, id
LIMIT 1`
	row, err := s.qrow(ctx, sqlStr, args...)
	if err != nil {
		return n, err
	}
	err = row.Scan(&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// PickNodeByPool 多节点池调度（T-G4-52，V8-D1）：pool 非空 ⇒ 只在该标签池内按剩余内存最大落位；
// pool 为空 ⇒ 全池最闲。池内无 online 节点 ⇒ ErrNotFound（上层译 node_no_capacity）。
func (s *Store) PickNodeByPool(ctx context.Context, pool string) (Node, error) {
	var n Node
	var err error
	if pool == "" {
		err = func() error {

			row, err := s.qrow(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE state='online'
ORDER BY COALESCE((capacity->>'mem_free_mb')::bigint, 0) DESC, id
LIMIT 1`)

			if err != nil {

				return err

			}

			return row.Scan(
				&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)

		}()
	} else {
		err = func() error {

			row, err := s.qrow(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE state='online' AND labels->>'pool' = $1
ORDER BY COALESCE((capacity->>'mem_free_mb')::bigint, 0) DESC, id
LIMIT 1`, pool)

			if err != nil {

				return err

			}

			return row.Scan(
				&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)

		}()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// ListNodes 节点清单（options 读模型用；本期节点个位数级）。
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.query(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at FROM g4_node ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NodeUsedVCPU 节点上在席（未销毁）套餐的 vCPU 占用合计（options 的 free_vcpu＝cpu_total−本值）。
func (s *Store) NodeUsedVCPU(ctx context.Context, nodeID string) (int, error) {
	rows, err := s.pool.Query(ctx, htx(), `
SELECT plan FROM g4_hosted_seat WHERE node_id=$1 AND state NOT IN ('destroyed')`, nodeID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	used := 0
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			return 0, err
		}
		if p, ok := PlanByID(pid); ok {
			used += p.VCPU
		}
	}
	return used, rows.Err()
}

// ---------- 托管席位 ----------

var seatStates = map[string]bool{
	"queued": true, "pulling": true, "starting": true, "online": true, "busy": true,
	"disabled": true, "stopped": true, "error": true, "destroyed": true,
}

type Seat struct {
	AgentID, NodeID, CLI, LLM, CredKind string
	AccountRef                          sql.NullString
	Plan, State, StateDetail, ImageRef  string
	Model                               string // 生效模型（包 B2；建席时定死落库，空＝未指定）
	CreatedBy                           string
}

// SeatsPendingReconcile 节点重连对账（组长 7fabddb0 ⑤）：该节点上「离线销毁」留下
// reconcile_pending 标记且尚未 reconcile_done 的席位。
func (s *Store) SeatsPendingReconcile(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, htx(), `
SELECT s.agent_id FROM g4_hosted_seat s
WHERE s.node_id=$1 AND s.state='destroyed'
  AND EXISTS (SELECT 1 FROM g4_seat_event e WHERE e.agent_id=s.agent_id AND e.kind='destroy'
              AND e.detail @> '{"reconcile_pending":true}'::jsonb)
  AND NOT EXISTS (SELECT 1 FROM g4_seat_event e2 WHERE e2.agent_id=s.agent_id AND e2.kind='reconcile_done')`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SeatBackupPending 离线销毁的席位是否还欠一次备份：销毁事件带 backup_pending，且之后没有 reconcile_backup_done。
func (s *Store) SeatBackupPending(ctx context.Context, agentID string) (bool, error) {
	var pending bool
	row, err := s.qrow(ctx, `
SELECT EXISTS (SELECT 1 FROM g4_seat_event e WHERE e.agent_id=$1 AND e.kind='destroy'
               AND e.detail @> '{"backup_pending":true}'::jsonb)
   AND NOT EXISTS (SELECT 1 FROM g4_seat_event e2 WHERE e2.agent_id=$1 AND e2.kind='reconcile_backup_done')`, agentID)
	if err != nil {
		return false, err
	}
	err = row.Scan(&pending)
	return pending, err
}

func (s *Store) CreateSeat(ctx context.Context, st Seat) error {
	if _, ok := PlanByID(st.Plan); !ok {
		return fmt.Errorf("%w: plan %q", ErrInvalidState, st.Plan)
	}
	const ins = `
INSERT INTO g4_hosted_seat (agent_id, node_id, cli, llm, cred_kind, account_ref, plan, state, image_ref, model, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,$10)`
	if !st.AccountRef.Valid {
		_, err := s.pool.Exec(ctx, htx(), ins,
			st.AgentID, st.NodeID, st.CLI, st.LLM, st.CredKind, st.AccountRef, st.Plan, st.ImageRef, st.Model, st.CreatedBy)
		return err
	}
	// 带账号：同一事务内先 FOR SHARE 锁账号行并校验「未停用、属主＝建席人」，再插席位行——
	// 与停用（SetAccountState 的 FOR UPDATE＋另句数引用）串行化，不会出现「已停用账号被新席引用」。
	// 0 行／不合 ⇒ ErrAccountNotAllowed；席位主键冲突等存储错误原样返回（不映射成账号错误）。
	return s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		var state, owner string
		err := tx.QueryRow(ctx, `SELECT state, owner_lc FROM g4_account_pool WHERE id=$1 FOR SHARE`, st.AccountRef.String).Scan(&state, &owner)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountNotAllowed
		}
		if err != nil {
			return err
		}
		if state == "disabled" || owner == "" || owner != st.CreatedBy {
			return ErrAccountNotAllowed
		}
		if s.afterSeatAccountLock != nil {
			s.afterSeatAccountLock()
		}
		_, err = tx.Exec(ctx, ins,
			st.AgentID, st.NodeID, st.CLI, st.LLM, st.CredKind, st.AccountRef, st.Plan, st.ImageRef, st.Model, st.CreatedBy)
		return err
	})
}

func (s *Store) GetSeat(ctx context.Context, agentID string) (Seat, error) {
	var st Seat
	err := func() error {

		row, err := s.qrow(ctx, `
SELECT agent_id, node_id, cli, llm, cred_kind, account_ref, plan, state, state_detail, image_ref, model, created_by
FROM g4_hosted_seat WHERE agent_id=$1`, agentID)

		if err != nil {

			return err

		}

		return row.Scan(
			&st.AgentID, &st.NodeID, &st.CLI, &st.LLM, &st.CredKind, &st.AccountRef,
			&st.Plan, &st.State, &st.StateDetail, &st.ImageRef, &st.Model, &st.CreatedBy)

	}()
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}

// SetSeatModel 写席位生效模型（包 B2；建席时 CreateSeat 已带，本方法供升级口径调整与测试）。
func (s *Store) SetSeatModel(ctx context.Context, agentID, model string) error {
	_, err := s.exec(ctx, `UPDATE g4_hosted_seat SET model=$2, updated_at=now() WHERE agent_id=$1`, agentID, model)
	return err
}

// SetSeatState 状态机迁移＋事件一条（同事务，InTx 回调——句柄不出 repo 包）。
func (s *Store) SetSeatState(ctx context.Context, agentID, state, detail, eventKind string, eventDetail json.RawMessage) error {
	if !seatStates[state] {
		return ErrInvalidState
	}
	return s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `
UPDATE g4_hosted_seat SET state=$2, state_detail=$3, updated_at=now() WHERE agent_id=$1`,
			agentID, state, detail)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `
INSERT INTO g4_seat_event (agent_id, kind, detail) VALUES ($1,$2,$3)`, agentID, eventKind, eventDetail)
		return err
	})
}

// TransitionSeatState 条件迁移：席位当前是 from 才改成 to（同事务记事件），返回是否迁移了。
// 当前不是 from（含席位不存在）⇒ false、不记事件、不报错 —— 供「只在某一态下才生效」的回调用（如 MarkOnline）。
func (s *Store) TransitionSeatState(ctx context.Context, agentID, from, to, detail, eventKind string, eventDetail json.RawMessage) (bool, error) {
	if !seatStates[to] {
		return false, ErrInvalidState
	}
	moved := false
	err := s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `
UPDATE g4_hosted_seat SET state=$2, state_detail=$3, updated_at=now() WHERE agent_id=$1 AND state=$4`,
			agentID, to, detail, from)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		moved = true
		_, err = tx.Exec(ctx, `
INSERT INTO g4_seat_event (agent_id, kind, detail) VALUES ($1,$2,$3)`, agentID, eventKind, eventDetail)
		return err
	})
	return moved, err
}

// OnlineAfterExistingTimeout 既有席位（重启／启用）启动超时后 Agent 才连入：error/start_timeout ⇒ online（lead-enable-restart-race ②）。
// 只认「最近一条事件是既有席位超时（detail 含 existing:true）」——新建超时的那条不带此标记（且已吊销回收），
// 其后任何操作（重启、销毁…）都会再记事件，届时不再命中。
func (s *Store) OnlineAfterExistingTimeout(ctx context.Context, agentID string) (bool, error) {
	moved := false
	err := s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `
UPDATE g4_hosted_seat s SET state='online', state_detail='', updated_at=now()
WHERE s.agent_id=$1 AND s.state='error' AND s.state_detail='start_timeout'
  AND (SELECT e.detail FROM g4_seat_event e WHERE e.agent_id=s.agent_id ORDER BY e.id DESC LIMIT 1)
      @> '{"existing":true}'::jsonb`, agentID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		moved = true
		_, err = tx.Exec(ctx, `
INSERT INTO g4_seat_event (agent_id, kind, detail) VALUES ($1,'online','{"after":"start_timeout"}'::jsonb)`, agentID)
		return err
	})
	return moved, err
}

// ---------- LLM 账号池（0.2.1.101：按人隔离＋本人共用，R-V2-ACCOUNT-PER-USER） ----------
//
// 归属：owner_lc（＝identity.User.UserIDLC()）。可见、可选、可改、可停用一律按 owner_lc 判；
// 管理用户只能看元数据、停用、启用（不见密钥、不能换密）。
// 占用：以 g4_hosted_seat.account_ref 为准（同一账号可被本人多个席位共用，不拦——业主 10-06 裁定）；
// g4_account_pool.state 只剩 available／disabled 两义（存量 'bound' 视同可用），bound_agent_id 不再写。

// MaxAccountsPerOwner 每人账号数上限（R-V2-ACCOUNT-PLAN-FINAL-ASKS：20；停用的也计入）。
const MaxAccountsPerOwner = 20

var (
	ErrAccountNameTaken = errors.New("hosted: account name taken") // 本人内同名
	ErrAccountLimit     = errors.New("hosted: account limit reached")
	ErrAccountInUse     = errors.New("hosted: account in use") // 停用守卫：仍有未销毁席位引用
)

type Account struct {
	ID, Name, Kind, CLI, LLM string
	KeyVersion               int
	Last4                    string
	State                    string
	BoundAgentID             sql.NullString // 历史列，只读
	OwnerLC                  string
	InUse                    int       // 未销毁席位对本号的引用数（列表类查询填；GetAccount 不填）
	CreatedAt                time.Time // 登记时间（token_file 类账号的 console 显示用——无后四位）
}

// accountInUseSQL 引用计数口径：account_ref 指向本号且席位未销毁（销毁不删行，置 destroyed；
// 新代码释放时另把 account_ref 置 NULL——两道并用，历史 destroyed 行靠状态过滤）。
const accountInUseSQL = `(SELECT count(*) FROM g4_hosted_seat s WHERE s.account_ref = p.id AND s.state <> 'destroyed')`

const accountCols = `p.id, p.name, p.kind, p.cli, p.llm, p.key_version, p.last4, p.state, p.bound_agent_id, p.owner_lc, p.created_at`

func scanAccount(sc interface{ Scan(...any) error }, a *Account, withInUse bool) error {
	if withInUse {
		return sc.Scan(&a.ID, &a.Name, &a.Kind, &a.CLI, &a.LLM, &a.KeyVersion, &a.Last4, &a.State, &a.BoundAgentID, &a.OwnerLC, &a.CreatedAt, &a.InUse)
	}
	return sc.Scan(&a.ID, &a.Name, &a.Kind, &a.CLI, &a.LLM, &a.KeyVersion, &a.Last4, &a.State, &a.BoundAgentID, &a.OwnerLC, &a.CreatedAt)
}

// CreateAccount 登记一行（归属 a.OwnerLC）。同一属主内同名 ⇒ ErrAccountNameTaken（唯一索引 g4_account_pool_owner_name）。
func (s *Store) CreateAccount(ctx context.Context, a Account, secretEnc []byte, by string) error {
	_, err := s.pool.Exec(ctx, htx(), `
INSERT INTO g4_account_pool (id, name, kind, cli, llm, secret_enc, key_version, last4, created_by, owner_lc)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, a.Name, a.Kind, a.CLI, a.LLM, secretEnc, a.KeyVersion, a.Last4, by, a.OwnerLC)
	if err != nil && isUniqueViolation(err, "g4_account_pool_owner_name") {
		return ErrAccountNameTaken
	}
	return err
}

// isUniqueViolation 按约束名识别唯一冲突（驱动无关：错误文本含约束名）。
func isUniqueViolation(err error, constraint string) bool {
	msg := err.Error()
	return containsAll(msg, "duplicate key", constraint) || containsAll(msg, "23505", constraint)
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !strings.Contains(s, x) {
			return false
		}
	}
	return true
}

// CountOwnedAccounts 属主名下账号数（含停用；上限判定用）。
func (s *Store) CountOwnedAccounts(ctx context.Context, ownerLC string) (int, error) {
	var n int
	row, err := s.qrow(ctx, `SELECT count(*) FROM g4_account_pool WHERE owner_lc=$1`, ownerLC)
	if err != nil {
		return 0, err
	}
	err = row.Scan(&n)
	return n, err
}

func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	row, err := s.qrow(ctx, `SELECT `+accountCols+` FROM g4_account_pool p WHERE p.id=$1`, id)
	if err == nil {
		err = scanAccount(row, &a, false)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// GetOwnedAccount 只取属主本人的账号；他人或不存在一律 ErrNotFound（不区分，防探测）。
func (s *Store) GetOwnedAccount(ctx context.Context, ownerLC, id string) (Account, error) {
	a, err := s.GetAccount(ctx, id)
	if err != nil {
		return Account{}, err
	}
	if ownerLC == "" || a.OwnerLC != ownerLC {
		return Account{}, ErrNotFound
	}
	return a, nil
}

// UpdateAccountSecret 换密：新密文与后四位一并更新（key_version 取当前 active——调用方给）。
func (s *Store) UpdateAccountSecret(ctx context.Context, accountID string, secretEnc []byte, keyVersion int, last4 string) error {
	res, err := s.pool.Exec(ctx, htx(), `
UPDATE g4_account_pool SET secret_enc=$2, key_version=$3, last4=$4, updated_at=now() WHERE id=$1`,
		accountID, secretEnc, keyVersion, last4)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AccountInUse 本号的存活引用数。
func (s *Store) AccountInUse(ctx context.Context, accountID string) (int, error) {
	var n int
	row, err := s.qrow(ctx, `SELECT count(*) FROM g4_hosted_seat WHERE account_ref=$1 AND state <> 'destroyed'`, accountID)
	if err != nil {
		return 0, err
	}
	err = row.Scan(&n)
	return n, err
}

// SetAccountState 停用（disabled）／启用（available）。
// 停用守卫＝无存活引用；与建席（CreateSeatWithAccount 的 FOR SHARE）串行化：先 FOR UPDATE 锁账号行，
// 再**另起一条语句**数引用（READ COMMITTED 下新语句取新快照——不能合成一句 UPDATE … WHERE NOT EXISTS，
// 等锁后的重检不会以新快照重算子查询；复核 tosonnet55tc2k）。
func (s *Store) SetAccountState(ctx context.Context, accountID, state string) error {
	switch state {
	case "available", "disabled":
	default:
		return ErrInvalidState
	}
	return s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		var cur string
		if err := tx.QueryRow(ctx, `SELECT state FROM g4_account_pool WHERE id=$1 FOR UPDATE`, accountID).Scan(&cur); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if state == "disabled" {
			if s.beforeDisableCount != nil {
				s.beforeDisableCount()
			}
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM g4_hosted_seat WHERE account_ref=$1 AND state <> 'destroyed'`, accountID).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return ErrAccountInUse
			}
		}
		_, err := tx.Exec(ctx, `UPDATE g4_account_pool SET state=$2, updated_at=now() WHERE id=$1`, accountID, state)
		return err
	})
}

// listAccounts 公共列表（带 in_use），where 只用于本文件内固定片段。
func (s *Store) listAccounts(ctx context.Context, where string, args ...any) ([]Account, error) {
	rows, err := s.pool.Query(ctx, htx(), `SELECT `+accountCols+`, `+accountInUseSQL+` FROM g4_account_pool p `+where+` ORDER BY p.name, p.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err := scanAccount(rows, &a, true); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListUsableAccounts 新建表单账号下拉：**属主本人**＋未停用（存量 'bound' 视同可用）＋CLI/LLM 匹配，带 in_use。
// 永不返回 secret_enc（B7：密钥不出现在任何应答里）。
func (s *Store) ListUsableAccounts(ctx context.Context, userLC, cli, llm string) ([]Account, error) {
	if userLC == "" {
		return []Account{}, nil
	}
	return s.listAccounts(ctx, `WHERE p.owner_lc = $1 AND p.state <> 'disabled' AND ($2 = '' OR p.cli = $2) AND ($3 = '' OR p.llm = $3)`, userLC, cli, llm)
}

// ListOwnedAccounts 「我的 LLM 账号」：属主本人全部（含停用）。
func (s *Store) ListOwnedAccounts(ctx context.Context, ownerLC string) ([]Account, error) {
	if ownerLC == "" {
		return []Account{}, nil
	}
	return s.listAccounts(ctx, `WHERE p.owner_lc = $1`, ownerLC)
}

// ListAllAccounts 管理视图：全部账号元数据（含属主、在用数；无密钥）。
func (s *Store) ListAllAccounts(ctx context.Context) ([]Account, error) {
	return s.listAccounts(ctx, ``)
}

// AssignAccountOwner 只给无主存量行指派属主（宿主命令行 assign-owner）；已有主 ⇒ ErrInvalidState。
func (s *Store) AssignAccountOwner(ctx context.Context, accountID, ownerLC string) error {
	if ownerLC == "" {
		return fmt.Errorf("%w: owner 必填", ErrParams)
	}
	res, err := s.pool.Exec(ctx, htx(), `UPDATE g4_account_pool SET owner_lc=$2, updated_at=now() WHERE id=$1 AND owner_lc=''`, accountID, ownerLC)
	if err != nil {
		if isUniqueViolation(err, "g4_account_pool_owner_name") {
			return ErrAccountNameTaken
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInvalidState
	}
	return nil
}

// ReleaseSeatAccount 释放本席对账号的引用（销毁／建席失败回滚／RunJob 失败／新建超时）：
// 只把本席 account_ref 置 NULL；账号行与其他席位不动（共用）。返回被释放的账号 ID（无则空）。
func (s *Store) ReleaseSeatAccount(ctx context.Context, agentID string) (string, error) {
	var prev sql.NullString
	err := s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT account_ref FROM g4_hosted_seat WHERE agent_id=$1 FOR UPDATE`, agentID).Scan(&prev); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if !prev.Valid {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE g4_hosted_seat SET account_ref=NULL, updated_at=now() WHERE agent_id=$1`, agentID)
		return err
	})
	if err != nil {
		return "", err
	}
	return prev.String, nil
}

// SeatAccountOf 席位所引用的账号（无则 ErrNotFound）。
func (s *Store) SeatAccountOf(ctx context.Context, agentID string) (string, error) {
	var ref sql.NullString
	row, err := s.qrow(ctx, `SELECT account_ref FROM g4_hosted_seat WHERE agent_id=$1`, agentID)
	if err == nil {
		err = row.Scan(&ref)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !ref.Valid) {
		return "", ErrNotFound
	}
	return ref.String, err
}

// SeatMayUseAccount 解密下发的边界：席位行引用本号，且账号属主＝席位属主（纵深：防凭 account_id 构造取他人凭据）。
func (s *Store) SeatMayUseAccount(ctx context.Context, agentID, accountID string) (bool, error) {
	var n int
	row, err := s.qrow(ctx, `
SELECT count(*) FROM g4_hosted_seat s JOIN g4_account_pool p ON p.id = s.account_ref
WHERE s.agent_id=$1 AND s.account_ref=$2 AND p.owner_lc <> '' AND p.owner_lc = s.created_by`, agentID, accountID)
	if err != nil {
		return false, err
	}
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}

// ---------- 配额 ----------

type Quota struct {
	UserLC                  string
	AgentsLimit, VCPULimit  int
	MemMBLimit, DiskMBLimit int64
}

// DefaultQuota 业主裁定的新用户默认配额（8971bddf）：未单独设置时按此计。
func DefaultQuota(userLC string) Quota {
	return Quota{UserLC: userLC, AgentsLimit: 4, VCPULimit: 4, MemMBLimit: 16384, DiskMBLimit: 65536}
}

type Usage struct {
	Agents        int
	VCPU          int
	MemMB, DiskMB int64
}

func (s *Store) GetQuota(ctx context.Context, userLC string) (Quota, error) {
	var q Quota
	err := func() error {

		row, err := s.qrow(ctx, `
SELECT user_lc, agents_limit, mem_mb_limit, disk_mb_limit, vcpu_limit FROM g4_quota WHERE user_lc=$1`, userLC)

		if err != nil {

			return err

		}

		return row.Scan(
			&q.UserLC, &q.AgentsLimit, &q.MemMBLimit, &q.DiskMBLimit, &q.VCPULimit)

	}()
	if errors.Is(err, sql.ErrNoRows) {
		// 未单独设置＝业主裁定默认值（8971bddf）：Agent 数 4／内存 16GB／磁盘 64GB／vCPU 4
		return DefaultQuota(userLC), nil
	}
	return q, err
}

// PutQuota 设上限。允许低于已用量（G6：只限制之后新建）——本层不做"低于已用拒存"。
func (s *Store) PutQuota(ctx context.Context, q Quota, by string) error {
	_, err := s.pool.Exec(ctx, htx(), `
INSERT INTO g4_quota (user_lc, agents_limit, mem_mb_limit, disk_mb_limit, vcpu_limit, updated_by)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (user_lc) DO UPDATE SET agents_limit=EXCLUDED.agents_limit,
  mem_mb_limit=EXCLUDED.mem_mb_limit, disk_mb_limit=EXCLUDED.disk_mb_limit,
  vcpu_limit=EXCLUDED.vcpu_limit, updated_by=EXCLUDED.updated_by, updated_at=now()`,
		q.UserLC, q.AgentsLimit, q.MemMBLimit, q.DiskMBLimit, q.VCPULimit, by)
	return err
}

// UsageFor 按 agent_id 集合聚合已用（disabled 计入、destroyed 不计；套餐映射在 plans.go）。
// 说明：hosted_seat 不存 owner——owner 在 identity 侧；上层（T-G4-40）先从 identity
// 取该用户名下 agent 列表再调本函数（G3 重审③：席位身份归 identity，本包不复制）。
func (s *Store) UsageFor(ctx context.Context, agentIDs []string) (Usage, error) {
	var u Usage
	if len(agentIDs) == 0 {
		return u, nil
	}
	rows, err := s.pool.Query(ctx, htx(), `
SELECT plan, COUNT(*) FROM g4_hosted_seat
WHERE agent_id = ANY($1) AND state <> 'destroyed'
GROUP BY plan`, agentIDs)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var planID string
		var n int
		if err := rows.Scan(&planID, &n); err != nil {
			return u, err
		}
		p, ok := PlanByID(planID)
		if !ok {
			continue
		}
		u.Agents += n
		u.VCPU += p.VCPU * n
		u.MemMB += p.MemMB * int64(n)
		u.DiskMB += p.DiskMB * int64(n)
	}
	return u, rows.Err()
}

// ---------- 审计 ----------

// CountAudit 审计计数（B8「恰好一条」与集成套件断言用）。
func (s *Store) CountAudit(ctx context.Context, action, target string) (int, error) {
	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action=$1 AND target=$2`, action, target)
	if err != nil {
		return 0, err
	}
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// SecretPlaintextHits B7 反向闸：全库扫明文命中数（应恒 0）。
func (s *Store) SecretPlaintextHits(ctx context.Context, plaintext string) (int, error) {
	// bytea 子串匹配（密文不是合法 UTF-8，convert_from 会报错——那本身即密文的旁证，但探针不能靠报错）
	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_account_pool WHERE position($1::bytea in secret_enc) > 0`, []byte(plaintext))
	if err != nil {
		return 0, err
	}
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) AddAudit(ctx context.Context, actor, action, target string, detail json.RawMessage) error {
	_, err := s.pool.Exec(ctx, htx(), `
INSERT INTO g4_audit (actor, action, target, detail) VALUES ($1,$2,$3,$4)`,
		actor, action, target, detail)
	return err
}
