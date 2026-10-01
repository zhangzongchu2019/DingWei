package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Store G4 托管存储（PG）。方法集覆盖 T-G4-01 的六张表＋审计；
// 编排与校验（配额/归属/联动）在上层（T-G4-30/40），本层只做忠实读写与约束内判定。

var (
	ErrNotFound      = errors.New("hosted: not found")
	ErrAccountBound  = errors.New("hosted: account already bound")   // 一席一账号冲突
	ErrInvalidState  = errors.New("hosted: invalid state")
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ApplySchema 幂等建表（可重复 apply）。
func (s *Store) ApplySchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, SchemaDDL)
	return err
}

// DropSchema 仅测试用（rollback 可重复性验证）。
func (s *Store) DropSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, DropDDL)
	return err
}

// ---------- 节点 ----------

type Node struct {
	ID, Name         string
	Labels           json.RawMessage
	State            string
	Capacity         json.RawMessage
	DockerVersion    string
	ProtocolVersion  int
	LastSeenAt       *time.Time
}

func (s *Store) UpsertNode(ctx context.Context, n Node) error {
	_, err := s.db.ExecContext(ctx, `
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
	err := s.db.QueryRowContext(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE id=$1`, id).Scan(
		&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// SetNodeOnline 节点心跳：上线＋容量＋last_seen；离线由 Hub 探测断连后调 SetNodeState。
func (s *Store) SetNodeOnline(ctx context.Context, id string, capacity json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE g4_node SET state='online', capacity=$2, last_seen_at=now(), updated_at=now() WHERE id=$1`, id, capacity)
	return err
}

func (s *Store) SetNodeState(ctx context.Context, id, state string) error {
	switch state {
	case "online", "offline":
	default:
		return ErrInvalidState
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE g4_node SET state=$2, updated_at=now() WHERE id=$1`, id, state)
	return err
}

// PickNodeAuto 「自动」落位：online 节点中剩余内存最大者（组长定 da622016）。
// 容量 JSON 形如 {"mem_free_mb":N}；无 online 节点返回 ErrNotFound（上层译 node_no_capacity）。
func (s *Store) PickNodeAuto(ctx context.Context) (Node, error) {
	var n Node
	err := s.db.QueryRowContext(ctx, `
SELECT id, name, labels, state, capacity, docker_version, node_protocol_version, last_seen_at
FROM g4_node WHERE state='online'
ORDER BY COALESCE((capacity->>'mem_free_mb')::bigint, 0) DESC, id
LIMIT 1`).Scan(
		&n.ID, &n.Name, &n.Labels, &n.State, &n.Capacity, &n.DockerVersion, &n.ProtocolVersion, &n.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
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
	CreatedBy                           string
}

func (s *Store) CreateSeat(ctx context.Context, st Seat) error {
	if _, ok := PlanByID(st.Plan); !ok {
		return fmt.Errorf("%w: plan %q", ErrInvalidState, st.Plan)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO g4_hosted_seat (agent_id, node_id, cli, llm, cred_kind, account_ref, plan, state, image_ref, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9)`,
		st.AgentID, st.NodeID, st.CLI, st.LLM, st.CredKind, st.AccountRef, st.Plan, st.ImageRef, st.CreatedBy)
	return err
}

func (s *Store) GetSeat(ctx context.Context, agentID string) (Seat, error) {
	var st Seat
	err := s.db.QueryRowContext(ctx, `
SELECT agent_id, node_id, cli, llm, cred_kind, account_ref, plan, state, state_detail, image_ref, created_by
FROM g4_hosted_seat WHERE agent_id=$1`, agentID).Scan(
		&st.AgentID, &st.NodeID, &st.CLI, &st.LLM, &st.CredKind, &st.AccountRef,
		&st.Plan, &st.State, &st.StateDetail, &st.ImageRef, &st.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}

// SetSeatState 状态机迁移＋事件一条（同事务）。
func (s *Store) SetSeatState(ctx context.Context, agentID, state, detail, eventKind string, eventDetail json.RawMessage) error {
	if !seatStates[state] {
		return ErrInvalidState
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE g4_hosted_seat SET state=$2, state_detail=$3, updated_at=now() WHERE agent_id=$1`,
		agentID, state, detail)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO g4_seat_event (agent_id, kind, detail) VALUES ($1,$2,$3)`, agentID, eventKind, eventDetail); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- 团队账号池 ----------

type Account struct {
	ID, Name, Kind, CLI, LLM string
	KeyVersion               int
	State                    string
	BoundAgentID             sql.NullString
}

func (s *Store) CreateAccount(ctx context.Context, a Account, secretEnc []byte, by string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO g4_account_pool (id, name, kind, cli, llm, secret_enc, key_version, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.ID, a.Name, a.Kind, a.CLI, a.LLM, secretEnc, a.KeyVersion, by)
	return err
}

func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
SELECT id, name, kind, cli, llm, key_version, state, bound_agent_id FROM g4_account_pool WHERE id=$1`, id).Scan(
		&a.ID, &a.Name, &a.Kind, &a.CLI, &a.LLM, &a.KeyVersion, &a.State, &a.BoundAgentID)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// ListUsableAccounts 新建表单账号下拉：本人被授权（grant）＋未绑定＋CLI/LLM 匹配。
// 永不返回 secret_enc（B7：密钥不出现在任何应答里）。
func (s *Store) ListUsableAccounts(ctx context.Context, userLC, cli, llm string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.name, p.kind, p.cli, p.llm, p.key_version, p.state, p.bound_agent_id
FROM g4_account_pool p
JOIN g4_account_grant g ON g.account_id = p.id AND g.user_lc = $1
WHERE p.state = 'available' AND p.cli = $2 AND p.llm = $3
ORDER BY p.name`, userLC, cli, llm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Kind, &a.CLI, &a.LLM, &a.KeyVersion, &a.State, &a.BoundAgentID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// BindAccount 一席一账号：仅当 available 才绑定成功（并发安全，反向用例的锚）。
func (s *Store) BindAccount(ctx context.Context, accountID, agentID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE g4_account_pool SET state='bound', bound_agent_id=$2, updated_at=now()
WHERE id=$1 AND state='available'`, accountID, agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAccountBound
	}
	return nil
}

// UnbindAccount 销毁/解绑回池（组长定：自动回池）。
func (s *Store) UnbindAccount(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE g4_account_pool SET state='available', bound_agent_id=NULL, updated_at=now()
WHERE id=$1 AND state='bound'`, accountID)
	return err
}

// GrantAccount 按用户授权（组长定 da622016；幂等）。
func (s *Store) GrantAccount(ctx context.Context, accountID, userLC, by string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO g4_account_grant (account_id, user_lc, granted_by) VALUES ($1,$2,$3)
ON CONFLICT (account_id, user_lc) DO NOTHING`, accountID, userLC, by)
	return err
}

// ---------- 配额 ----------

type Quota struct {
	UserLC                                         string
	AgentsLimit, VCPULimit                         int
	MemMBLimit, DiskMBLimit                        int64
}

type Usage struct {
	Agents          int
	VCPU            int
	MemMB, DiskMB   int64
}

func (s *Store) GetQuota(ctx context.Context, userLC string) (Quota, error) {
	var q Quota
	err := s.db.QueryRowContext(ctx, `
SELECT user_lc, agents_limit, mem_mb_limit, disk_mb_limit, vcpu_limit FROM g4_quota WHERE user_lc=$1`, userLC).Scan(
		&q.UserLC, &q.AgentsLimit, &q.MemMBLimit, &q.DiskMBLimit, &q.VCPULimit)
	if errors.Is(err, sql.ErrNoRows) {
		// 未单独设置＝默认零（报业主项 2 的默认口径）：须管理层先配才能建
		return Quota{UserLC: userLC}, nil
	}
	return q, err
}

// PutQuota 设上限。允许低于已用量（G6：只限制之后新建）——本层不做"低于已用拒存"。
func (s *Store) PutQuota(ctx context.Context, q Quota, by string) error {
	_, err := s.db.ExecContext(ctx, `
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
	rows, err := s.db.QueryContext(ctx, `
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

func (s *Store) AddAudit(ctx context.Context, actor, action, target string, detail json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO g4_audit (actor, action, target, detail) VALUES ($1,$2,$3,$4)`,
		actor, action, target, detail)
	return err
}
