package hosted

// T-G4-30 · 创建编排器（M3 主线；首个打通的组合＝Codex，业主 31405e84）。
//
// 依赖全部接口注入（G3 分支合入前不接真件）：
//  · IdentityIssuer——G3 identity 的托管签发族（签名照 internal/identity/hosted.go，760a7a45＋ab3b4c5b（g3-hosted-r1））；
//  · NodeGateway——Hub 到节点代理的指令通道（WSS 服务端，接线在 T-G4-35 同批的 Hub 网关落地）；
//  · CombosSource——逐家验通制的事实源（B12；T-G4-15 的验通结果回板后填充）。
//
// 校验分工（G3 重审）：名称校验委托 IssueHostedAgent、不重复实现；
// 联动过滤/套餐两档/超额/配额与归属＝本层（我方职责）。
// 失败清理：任何阶段失败一律 RetireHostedAgent——不调则名字一直被占（G3 提醒，板 aa39f642）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------- 外部接口（注入） ----------

type EnrollToken struct {
	ApplicationID string
	PollToken     string
	ExpiresAt     time.Time
}

type IssuedAgent struct {
	ID       string
	FullName string
}

type IdentityIssuer interface {
	IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (IssuedAgent, EnrollToken, error)
	RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	// ListHostedAgentIDsByOwner 配额已用统计需名下席位清单（hosted_seat 不存 owner——G3 重审③）。
	// 🔵 G3 侧若无现成方法，接线时经 console agents 端点服务端侧取——已在给 opus50tc2k 的清单里。
	ListHostedAgentIDsByOwner(ctx context.Context, ownerLC string) ([]string, error)
}

type NodeCommand struct {
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params"`
}

type NodeResult struct {
	OK    bool
	Error string
}

type NodeGateway interface {
	Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error)
}

type Combo struct {
	CLI, LLM, CredKind string
	Verified           bool
}

type CombosSource interface {
	// Lookup 返回该组合的验通状态；ok=false＝不兼容（combo_incompatible）。
	Lookup(cli, llm, credKind string) (verified bool, ok bool)
}

// ImageCatalog 基础镜像版本登记（G1 挂接）：按 CLI 取当前 pinned 镜像 ref（含 digest）。
// 「可升级标记」与「默认最新」的数据源同此——升级时取新 ref。
type ImageCatalog interface {
	Latest(cli string) (ref string, ok bool)
}

// ---------- 错误码（契约 v2 第四节；页面只认机器码） ----------

var (
	ErrComboIncompatible = errors.New("combo_incompatible")
	ErrComboNotVerified  = errors.New("combo_not_verified")
	ErrQuotaExceeded     = errors.New("quota_exceeded")
	ErrPlanExceedsQuota  = errors.New("plan_exceeds_quota")
	ErrNoNodeCapacity    = errors.New("node_no_capacity")
	ErrAccountNotAllowed = errors.New("account_not_allowed")
)

// ---------- 编排器 ----------

type Orchestrator struct {
	st      *Store
	pool    *PoolService
	idp     IdentityIssuer
	gw      NodeGateway
	combos  CombosSource
	images  ImageCatalog
	sweepMu sync.Mutex
}

func NewOrchestrator(st *Store, pool *PoolService, idp IdentityIssuer, gw NodeGateway, combos CombosSource, images ImageCatalog) *Orchestrator {
	return &Orchestrator{st: st, pool: pool, idp: idp, gw: gw, combos: combos, images: images}
}

type CreateRequest struct {
	OwnerLC   string // 归属用户（console 当前用户，N3.Actor）
	By        string // 操作者（审计）
	Name      string
	GroupCode string // 编组可不选（业主令 8582acc4：三键皆缺＝不入组合法；给了才校验）
	NodeID    string // "auto" 或节点 id
	Pool      string // 节点池标签（V8-D1：非空 ⇒ 只在该池落位；空 ⇒ 全池最闲）
	CLI, LLM  string
	CredKind  string // api_key | token_file
	AccountID string
	Plan      string
}

type CreateOutcome struct {
	JobID   string // = agent_id（契约：job 与席位同一标识，jobs/{job_id} 查询）
	AgentID string
	State   string
}

// Create 创建托管 Agent：同步校验（失败即 4xx）→ 签发 → 绑定 → 席位落库（queued）。
// 异步执行链（pull→create→online）由 RunJob 驱动；超时清扫见 SweepTimeouts。
func (o *Orchestrator) Create(ctx context.Context, req CreateRequest) (CreateOutcome, error) {
	// ① 联动过滤＋逐家验通（B12：验通一家放开一家）
	verified, ok := o.combos.Lookup(req.CLI, req.LLM, req.CredKind)
	if !ok {
		return CreateOutcome{}, ErrComboIncompatible
	}
	if !verified {
		return CreateOutcome{}, ErrComboNotVerified
	}
	// ② 套餐
	plan, ok := PlanByID(req.Plan)
	if !ok {
		return CreateOutcome{}, fmt.Errorf("%w: plan %q", ErrParams, req.Plan)
	}
	// ③ 配额与归属（我方职责；规则集中在 QuotaService——轮换不占配额，此处只管新建）
	if err := NewQuotaService(o.st, o.idp).CheckCreate(ctx, req.OwnerLC, plan); err != nil {
		return CreateOutcome{}, err
	}
	// ④ 账号：本人可用（授权）＋未绑定＋CLI/LLM 匹配＋凭据形态匹配
	usable, err := o.st.ListUsableAccounts(ctx, req.OwnerLC, req.CLI, req.LLM)
	if err != nil {
		return CreateOutcome{}, err
	}
	var acct *Account
	for i := range usable {
		if usable[i].ID == req.AccountID && usable[i].Kind == req.CredKind {
			acct = &usable[i]
			break
		}
	}
	if acct == nil {
		return CreateOutcome{}, ErrAccountNotAllowed
	}
	// ⑤ 节点：指定或自动（余量最大）；容量须容得下套餐
	node, err := o.pickNode(ctx, req.NodeID, req.Pool, plan)
	if err != nil {
		return CreateOutcome{}, err
	}
	// ⑤b 镜像：按 CLI 取当前 pinned ref（G1 镜像版本登记）
	imageRef, ok := o.images.Latest(req.CLI)
	if !ok {
		return CreateOutcome{}, fmt.Errorf("%w: 无 %s 的登记镜像", ErrParams, req.CLI)
	}

	// ⑥ 签发（名称校验委托 G3）
	ag, tok, err := o.idp.IssueHostedAgent(ctx, req.OwnerLC, req.Name, req.GroupCode, req.By)
	if err != nil {
		return CreateOutcome{}, err
	}
	_ = tok // 令牌随 profile 注入容器（T-G4-31/32：RunJob 的 create_seat 参数里带上）
	// ⑦ 绑定账号（CAS）；失败 ⇒ 一律 Retire 释放名字
	if err := o.pool.Bind(ctx, req.By, true, req.AccountID, ag.ID); err != nil {
		_, _ = o.idp.RetireHostedAgent(ctx, ag.ID, req.By)
		return CreateOutcome{}, ErrAccountNotAllowed
	}
	// ⑧ 席位落库（queued）＋事件
	err = o.st.CreateSeat(ctx, Seat{
		AgentID: ag.ID, NodeID: node.ID, CLI: req.CLI, LLM: req.LLM, CredKind: req.CredKind,
		AccountRef: sql.NullString{String: req.AccountID, Valid: true}, Plan: req.Plan, ImageRef: imageRef, CreatedBy: req.By,
	})
	if err != nil {
		_, _ = o.idp.RetireHostedAgent(ctx, ag.ID, req.By)
		_ = o.pool.Unbind(ctx, req.By, true, req.AccountID)
		return CreateOutcome{}, err
	}
	return CreateOutcome{JobID: ag.ID, AgentID: ag.ID, State: "queued"}, nil
}

func (o *Orchestrator) pickNode(ctx context.Context, nodeID, pool string, plan Plan) (Node, error) {
	var n Node
	var err error
	if nodeID != "" && nodeID != "auto" {
		n, err = o.st.GetNode(ctx, nodeID)
		if err == nil && n.State != "online" {
			err = ErrNoNodeCapacity
		}
	} else {
		n, err = o.st.PickNodeByPool(ctx, pool)
	}
	if err != nil {
		return n, ErrNoNodeCapacity
	}
	if freeMB(n) < plan.MemMB {
		return n, ErrNoNodeCapacity
	}
	return n, nil
}

func freeMB(n Node) int64 {
	var c struct {
		MemFreeMB int64 `json:"mem_free_mb"`
	}
	_ = json.Unmarshal(n.Capacity, &c)
	return c.MemFreeMB
}

// ---------- 执行链（异步） ----------

// RunJob 驱动一个席位的执行链：pulling → starting → online；失败清理（一律 Retire）。
// 超时由 SweepTimeouts 兜底（排队 10m／拉镜像 30m／启动 5m）。
func (o *Orchestrator) RunJob(ctx context.Context, agentID string) {
	fail := func(stage, reason string) {
		seat, err := o.st.GetSeat(ctx, agentID)
		if err == nil {
			_ = o.st.SetSeatState(ctx, agentID, "error", reason, "fail", json.RawMessage(`{"stage":"`+stage+`"}`))
			if seat.AccountRef.Valid {
				_ = o.pool.Unbind(ctx, "system", true, seat.AccountRef.String)
			}
		}
		_, _ = o.idp.RetireHostedAgent(ctx, agentID, "system")
	}

	if err := o.st.SetSeatState(ctx, agentID, "pulling", "", "pull", nil); err != nil {
		return
	}
	seat, err := o.st.GetSeat(ctx, agentID)
	if err != nil {
		return
	}
	if res, err := o.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "pull_image", Params: json.RawMessage(`{"image":` + jsonStr(seat.ImageRef) + `}`)}); err != nil || !res.OK {
		fail("pulling", "image_pull_failed")
		return
	}
	if err := o.st.SetSeatState(ctx, agentID, "starting", "", "start", nil); err != nil {
		return
	}
	if res, err := o.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "create_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)}); err != nil || !res.OK {
		fail("starting", "start_failed")
		return
	}
	// online 的最终确认＝席位以专属凭据连入（Hub 侧连接事件回调 MarkOnline）
	if err := o.st.SetSeatState(ctx, agentID, "online", "", "online", nil); err != nil {
		return
	}
}

// MarkOnline 席位以专属凭据连入时由 Hub 连接层回调（T-G4-32 同通道）。
func (o *Orchestrator) MarkOnline(ctx context.Context, agentID string) error {
	return o.st.SetSeatState(ctx, agentID, "online", "", "online", nil)
}

// SweepTimeouts 超时清扫（排队 10 分钟不可取消／拉镜像 30m／启动 5m；超时即失败不留半成品）。
func (o *Orchestrator) SweepTimeouts(ctx context.Context, now time.Time) {
	o.sweepMu.Lock()
	defer o.sweepMu.Unlock()
	rows, err := o.st.pool.Query(ctx, htx(), `
SELECT agent_id, state, updated_at FROM g4_hosted_seat
WHERE state IN ('queued','pulling','starting')`)
	if err != nil {
		return
	}
	defer rows.Close()
	type pending struct{ id, state string }
	var timedOut []pending
	for rows.Next() {
		var id, state string
		var updated time.Time
		if err := rows.Scan(&id, &state, &updated); err != nil {
			return
		}
		var limit time.Duration
		var reason string
		switch state {
		case "queued":
			limit, reason = 10*time.Minute, "queue_timeout"
		case "pulling":
			limit, reason = 30*time.Minute, "pull_timeout"
		case "starting":
			limit, reason = 5*time.Minute, "start_timeout"
		}
		if now.Sub(updated) > limit {
			timedOut = append(timedOut, pending{id: id, state: reason})
		}
	}
	for _, p := range timedOut {
		seat, err := o.st.GetSeat(ctx, p.id)
		if err == nil && seat.AccountRef.Valid {
			_ = o.pool.Unbind(ctx, "system", true, seat.AccountRef.String)
		}
		_ = o.st.SetSeatState(ctx, p.id, "error", p.state, "fail", json.RawMessage(`{"reason":"`+p.state+`"}`))
		_, _ = o.idp.RetireHostedAgent(ctx, p.id, "system")
	}
}

func jsonStr(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
