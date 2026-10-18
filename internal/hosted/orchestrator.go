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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var claudeSeatModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

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
	// IssueHostedAgent 签发。编组可缺省（groupCode 空＝不入组）；实现方（接线适配器）
	// 必须把 identity.ErrGroupNotFound/ErrGroupClosed 译为本包 ErrGroupNotFound/ErrGroupClosed
	// 哨兵（契约机器码 group_not_found/group_closed——页面只认机器码）。
	IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (IssuedAgent, EnrollToken, error)
	// IssueHostedAgentWithNewGroup 同 IssueHostedAgent，但就地新建编组、新 Agent 为组长（R-V2-0102-GROUP-FIXES A）。
	// 组名不合规／被占用原样返回 identity 的错误（路由层错误表译为 group_name_invalid／group_name_taken）。
	IssueHostedAgentWithNewGroup(ctx context.Context, ownerLC, name, newGroupName, by string) (IssuedAgent, EnrollToken, error)
	RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	// AbortHostedAgent 没建成的席位回收：吊销 Agent，并删掉随本次签发新建、无其他成员的编组（既有编组不动）。
	// 新建链（落库失败、RunJob 失败、SweepTimeouts 新建超时）一律用它；销毁既有席位仍用 RetireHostedAgent。
	AbortHostedAgent(ctx context.Context, agentID, by string) (bool, error)
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
	// Payload 指令成功的回执载荷（R-G4-BACKUP-CHECKSUM-CTRL：export_seat 经此补报 sha256）。
	Payload json.RawMessage
}

type NodeGateway interface {
	Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error)
}

type Combo struct {
	CLI      string `json:"cli"`
	LLM      string `json:"llm"`
	CredKind string `json:"cred_kind"`
	Verified bool   `json:"verified"`
}

type CombosSource interface {
	// Lookup 返回该组合的验通状态；ok=false＝不兼容（combo_incompatible）。
	Lookup(cli, llm, credKind string) (verified bool, ok bool)
	// List 全量组合表（options 读模型；唯一事实源——不在表＝不兼容）。
	List() []Combo
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
	// 编组两码（契约 v2 第一节；G3 终版 800f24d8 的 identity.ErrGroupNotFound/ErrGroupClosed
	// 由接线适配器译成这两个哨兵——编组可缺省，给了才校验）。
	ErrGroupNotFound = errors.New("group_not_found")
	ErrGroupClosed   = errors.New("group_closed")
	// 包 B2（建席可选模型；HTTP 422 同族，契约写明）
	ErrModelNotAvailable = errors.New("model_not_available") // 清单外／无在线节点支持
	ErrModelNotSupported = errors.New("model_not_supported") // 不支持自选模型的 CLI 带 model
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
	// pendingTok 一次性入网令牌的内存暂存（T-G4-32）：Create 签发后暂存，RunJob 的
	// create_seat 随参数下发后清除。🔴 不落库不落盘——Hub 重启即丢，滞留席位在启动阶段
	// 以 credential_inject_failed 失败清理（Retire＋解绑，不占名），fail-closed 不硬撑。
	tokMu      sync.Mutex
	pendingTok map[string]EnrollToken
	// backups 离线销毁的席位在重连对账时先备份（SetBackuper 注入；为空 ⇒ 待备份的席位一律留待下次，不删数据）。
	backups VolumeBackuper
}

func NewOrchestrator(st *Store, pool *PoolService, idp IdentityIssuer, gw NodeGateway, combos CombosSource, images ImageCatalog) *Orchestrator {
	return &Orchestrator{st: st, pool: pool, idp: idp, gw: gw, combos: combos, images: images, pendingTok: map[string]EnrollToken{}}
}

func (o *Orchestrator) stashToken(agentID string, tok EnrollToken) {
	o.tokMu.Lock()
	defer o.tokMu.Unlock()
	o.pendingTok[agentID] = tok
}

func (o *Orchestrator) takeToken(agentID string) (EnrollToken, bool) {
	o.tokMu.Lock()
	defer o.tokMu.Unlock()
	t, ok := o.pendingTok[agentID]
	return t, ok
}

func (o *Orchestrator) dropToken(agentID string) {
	o.tokMu.Lock()
	defer o.tokMu.Unlock()
	delete(o.pendingTok, agentID)
}

// cpuShares vCPU 软上限换算：clamp(1024×N, 2, 262144)（组长裁定）。
func cpuShares(vcpu int) int {
	shares := vcpu * 1024
	if shares < 2 {
		return 2
	}
	if shares > 262144 {
		return 262144
	}
	return shares
}

type CreateRequest struct {
	AllowSoft bool   // explicit user consent; node admin must also allow its soft pool
	OwnerLC   string // 归属用户（console 当前用户，N3.Actor）
	By        string // 操作者（审计）
	Name      string
	GroupCode string // 编组可不选（业主令 8582acc4：三键皆缺＝不入组合法；给了才校验）
	// NewGroupName 就地新建编组（与 GroupCode 二选一，路由层已判）；非空 ⇒ IssueHostedAgentWithNewGroup。
	NewGroupName string
	NodeID       string // "auto" 或节点 id
	Pool         string // 节点池标签（V8-D1：非空 ⇒ 只在该池落位；空 ⇒ 全池最闲）
	CLI, LLM     string
	CredKind     string // api_key | token_file
	AccountID    string
	Plan         string
	Model        string // 按席模型：Codex/Claude 可选，必须在节点批准清单；空保留各 CLI 旧行为。
}

type CreateOutcome struct {
	JobID   string // = agent_id（契约：job 与席位同一标识，jobs/{job_id} 查询）
	AgentID string
	State   string
}

// Create 创建托管 Agent：同步校验（失败即 4xx）→ 签发 → 绑定 → 席位落库（queued）。
// 异步执行链（pull→create→online）由 RunJob 驱动；超时清扫见 SweepTimeouts。
func (o *Orchestrator) Create(ctx context.Context, req CreateRequest) (CreateOutcome, error) {
	// ⓪ 凭据形态由账号决定（契约：创建请求体只有 account_id，没有 cred_kind——
	//    账号的 kind 即凭据形态；账号不存在 ⇒ account_not_allowed）
	if req.CredKind == "" {
		a, err := o.st.GetAccount(ctx, req.AccountID)
		if err != nil {
			return CreateOutcome{}, ErrAccountNotAllowed
		}
		req.CredKind = a.Kind
	}
	// ⓪b 模型形态闸：只有 Codex/Claude 支持显式模型；其他 CLI 保持拒绝。
	// Claude 只接受完整安全 ID，不能将选项形字符串作为 CLI 参数。
	// 先于联动过滤（形态错比组合错更根本，机器码据此分流页面文案）。
	if req.Model != "" && req.CLI != "codex" && req.CLI != "claude" {
		return CreateOutcome{}, ErrModelNotSupported
	}
	if req.CLI == "claude" && req.Model != "" && !claudeSeatModelID.MatchString(req.Model) {
		return CreateOutcome{}, ErrModelNotAvailable
	}
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
	// ④ 账号：属主本人＋未停用＋CLI/LLM 匹配＋凭据形态匹配（0.2.1.101：按人隔离，共用不拦）
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
	// ⑤ 节点：指定或自动（余量最大）；容量须容得下套餐；带 model ⇒ 只在上报清单含它的在线节点里选
	// （修订 1：有节点支持但被选中节点不支持 ⇒ 自动选别的，不报错；全部不支持才拒）
	node, err := o.pickNode(ctx, req.NodeID, req.Pool, plan, req.Model, req.AllowSoft)
	if err != nil {
		return CreateOutcome{}, err
	}
	// ⑤b 镜像：按 CLI 取当前 pinned ref（G1 镜像版本登记）
	imageRef, ok := o.images.Latest(req.CLI)
	if !ok {
		return CreateOutcome{}, fmt.Errorf("%w: 无 %s 的登记镜像", ErrParams, req.CLI)
	}

	// ⑥ 签发（名称校验委托 G3）
	var ag IssuedAgent
	var tok EnrollToken
	if req.NewGroupName != "" {
		ag, tok, err = o.idp.IssueHostedAgentWithNewGroup(ctx, req.OwnerLC, req.Name, req.NewGroupName, req.By)
	} else {
		ag, tok, err = o.idp.IssueHostedAgent(ctx, req.OwnerLC, req.Name, req.GroupCode, req.By)
	}
	if err != nil {
		return CreateOutcome{}, err
	}
	o.stashToken(ag.ID, tok) // T-G4-32：RunJob 的 create_seat 随参数下发后清除
	// ⑦⑧ 席位落库（queued）＝引用账号：0.2.1.101 起不再独占绑定（本人多席共用，不拦）。
	// CreateSeat 在同一事务内 FOR SHARE 锁账号行并复核「未停用、属主＝建席人」，与停用串行化；
	// 失败 ⇒ 一律 Retire 释放名字（席位行未落，无引用要释放）。账号错误与存储错误分开返回。
	mode, revision := registeredQuota(node)
	var consent json.RawMessage
	if mode == "soft" {
		consent, _ = json.Marshal(map[string]any{"node_id": node.ID, "quota_mode": mode, "quota_policy_revision": revision, "allow_soft": req.AllowSoft, "notice": "无硬磁盘配额"})
	}
	err = o.st.CreateSeat(ctx, Seat{
		QuotaConsent: consent,
		AgentID:      ag.ID, NodeID: node.ID, CLI: req.CLI, LLM: req.LLM, CredKind: req.CredKind,
		AccountRef: sql.NullString{String: req.AccountID, Valid: true}, Plan: req.Plan, ImageRef: imageRef,
		Model: req.Model, CreatedBy: req.By, // 生效值即请求值（建席时定死；空＝未指定）
	})
	if err != nil {
		o.dropToken(ag.ID)
		_, _ = o.idp.AbortHostedAgent(ctx, ag.ID, req.By)
		return CreateOutcome{}, err
	}
	_ = o.st.AddAudit(ctx, req.By, "account.bind", req.AccountID, jsonRaw(`{"agent":"`+ag.ID+`"}`))
	return CreateOutcome{JobID: ag.ID, AgentID: ag.ID, State: "queued"}, nil
}

// AccountComboKnown 登记账号时的联动校验：(cli,llm,kind) 须在联动表里（验通与否不论——验通卡在建席）。
func (o *Orchestrator) AccountComboKnown(cli, llm, kind string) bool {
	_, ok := o.combos.Lookup(cli, llm, kind)
	return ok
}

func (o *Orchestrator) pickNode(ctx context.Context, nodeID, pool string, plan Plan, model string, allowSoft ...bool) (Node, error) {
	nodes, err := o.st.ListNodes(ctx)
	if err != nil {
		return Node{}, ErrNoNodeCapacity
	}
	consent := len(allowSoft) > 0 && allowSoft[0]
	var candidates []Node
	modelSeen := false
	var blockedReasons []string
	for _, n := range nodes {
		if nodeID != "" && nodeID != "auto" && n.ID != nodeID {
			continue
		}
		if n.State != "online" || !nodeQuotaEligible(n) {
			continue
		}
		var labels map[string]string
		_ = json.Unmarshal(n.Labels, &labels)
		if pool != "" && labels["pool"] != pool {
			continue
		}
		mode, _ := registeredQuota(n)
		if mode == "soft" && (!consent || labels["allow_soft"] != "true") {
			continue
		}
		if model != "" {
			if !nodeHasModel(n, model) || n.LastSeenAt == nil || time.Since(*n.LastSeenAt) > 180*time.Second {
				continue
			}
			modelSeen = true
		}
		if mode == "soft" {
			v, err := o.st.nodeSeatCapacity(ctx, n)
			if err != nil || v == nil {
				blockedReasons = append(blockedReasons, "load_unknown")
				continue
			}
			if reasons := v.CanAdd(v.Policy(), capacityPlan(plan)); len(reasons) != 0 {
				blockedReasons = append(blockedReasons, reasons...)
				continue
			}
		}
		if freeMB(n) < plan.MemMB {
			continue
		}
		candidates = append(candidates, n)
	}
	if len(candidates) == 0 {
		if model != "" && !modelSeen {
			return Node{}, ErrModelNotAvailable
		}
		// Union only over visible, consented, model-compatible soft candidates.
		// Each reason means at least one such candidate failed that resource; it
		// does not claim all nodes failed the same dimension or reveal node IDs.
		if len(blockedReasons) != 0 {
			return Node{}, capacityFailure(blockedReasons)
		}
		return Node{}, ErrNoNodeCapacity
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, _ := registeredQuota(candidates[i])
		b, _ := registeredQuota(candidates[j])
		if a != b {
			return a == "hard"
		}
		if freeMB(candidates[i]) != freeMB(candidates[j]) {
			return freeMB(candidates[i]) > freeMB(candidates[j])
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates[0], nil
}

// nodeHasModel 节点上报清单含该 model（capacity JSONB 的 models 数组）。
func nodeHasModel(n Node, model string) bool {
	var c struct {
		Models []string `json:"models"`
	}
	_ = json.Unmarshal(n.Capacity, &c)
	for _, m := range c.Models {
		if m == model {
			return true
		}
	}
	return false
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
		o.dropToken(agentID)
		seat, err := o.st.GetSeat(ctx, agentID)
		if err == nil {
			_ = o.st.SetSeatState(ctx, agentID, "error", reason, "fail", json.RawMessage(`{"stage":"`+stage+`"}`))
			if seat.AccountRef.Valid {
				_ = o.pool.Release(ctx, "system", agentID)
			}
		}
		_, _ = o.idp.AbortHostedAgent(ctx, agentID, "system")
	}

	if err := o.st.SetSeatState(ctx, agentID, "pulling", "", "pull", nil); err != nil {
		return
	}
	seat, err := o.st.GetSeat(ctx, agentID)
	if err != nil {
		return
	}
	if res, err := o.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "pull_image", Params: json.RawMessage(`{"image":` + jsonStr(seat.ImageRef) + `}`)}); err != nil || !res.OK {
		if err == nil {
			if capacityErr := o.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
				fail("pulling", lifecycleJobReason(capacityErr))
				return
			}
		}
		fail("pulling", "image_pull_failed")
		return
	}
	if err := o.st.SetSeatState(ctx, agentID, "starting", "", "start", nil); err != nil {
		return
	}
	// T-G4-32：create_seat 携带资源（套餐换算）、一次性入网令牌（内存暂存）、
	// 账号凭据件（DecryptForSeat 解密一次、审计一条——最小暴露：只下发给本席）。
	tok, ok := o.takeToken(agentID)
	if !ok {
		fail("starting", "credential_inject_failed") // Hub 重启丢了暂存令牌：fail-closed 清理
		return
	}
	var cred string
	if seat.AccountRef.Valid {
		c, derr := o.pool.DecryptForSeat(ctx, "system", seat.AccountRef.String, agentID)
		if derr != nil {
			fail("starting", "credential_inject_failed")
			return
		}
		cred = c
	}
	plan, _ := PlanByID(seat.Plan)
	params, _ := json.Marshal(map[string]any{
		"seat_id":   agentID,
		"image":     seat.ImageRef,
		"resources": o.st.resourceParams(ctx, seat.NodeID, plan),
		"token_file": map[string]string{
			"application_id": tok.ApplicationID,
			"poll_token":     tok.PollToken,
			"expires_at":     tok.ExpiresAt.UTC().Format(time.RFC3339),
		},
		"credential_file": map[string]string{"content": cred},
		"cred_kind":       seat.CredKind, // 容器启动契约（R-G1-HOSTED-IMAGE-CONTRACT）：账号形态随参数下发
		"cli":             seat.CLI,      // 节点代理据此注入 SH_MODE=cli、SH_CLI（组长 R-B12-SEATMODE 第 2 条）
		"model":           seat.Model,    // 包 B2 修订 3：落库记录值（不随节点默认漂移）
	})
	if res, err := o.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "create_seat", Params: params}); err != nil || !res.OK {
		if err == nil {
			if capacityErr := o.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
				fail("starting", lifecycleJobReason(capacityErr))
				return
			}
		}
		n, ne := o.st.GetNode(ctx, seat.NodeID)
		mode, _ := registeredQuota(n)
		if (ne == nil && mode == "soft") || (!res.OK && strings.Contains(res.Error, "soft_quota_blocked")) {
			_ = o.st.SetSeatState(ctx, agentID, "error", "soft_quota_held", "soft_quota", nil)
			return
		}
		fail("starting", "start_failed")
		return
	}
	o.dropToken(agentID)
	// 🔴 容器起来 ≠ online（组长裁定，板 ac33b1cb）：席位停在 starting，直到 Agent 以专属凭据真正连入、
	//    Hub 连接层回调 MarkOnline 才算 online。永不连入 ⇒ SweepTimeouts 按 starting 5 分钟清成 start_timeout。
	//    原先这里直接置 online：容器起来但 Agent 连不上时蒙版仍显示 online，超时清扫也管不到它。
}

// MarkOnline 席位以专属凭据连入时由 Hub 连接层回调（Hub.OnHostedAgentOnline，wireHosted 接线）。
// 只认 starting → online：已 online 的重连、升级中（busy）新容器连入、已超时清掉的席位等一律不改（返回 nil）。
func (o *Orchestrator) MarkOnline(ctx context.Context, agentID string) error {
	if seat, err := o.st.GetSeat(ctx, agentID); err == nil {
		if seat.StateDetail == "soft_quota_held" {
			return nil
		}
		if n, e := o.st.GetNode(ctx, seat.NodeID); e == nil && softSeatHeld(n, agentID) {
			return nil
		}
	}
	moved, err := o.st.TransitionSeatState(ctx, agentID, "starting", "online", "", "online", nil)
	if err != nil || moved {
		return err
	}
	// 既有席位重启／启用超时记了 error/start_timeout 之后 Agent 才连入（lead-enable-restart-race ②）：照样置 online。
	// 新建超时已吊销回收，不涉及（OnlineAfterExistingTimeout 只认既有席位那条超时事件）。
	_, err = o.st.OnlineAfterExistingTimeout(ctx, agentID)
	return err
}

// starting 态的 state_detail：区分「新建席位起来」（空）与「既有席位重启／启用」。
// 超时清扫对两者的处置不同：新建的超时整个回收（吊销、解绑）；既有席位只记 start_timeout，Agent 与账号原样保留，可再重启。
const (
	seatStartingRestart = "restart"
	seatStartingEnable  = "enable"
	seatStartingRefresh = "refresh" // 0.2.1.101 刷新凭据：既有席位，超时只记错、不回收
)

// existingSeatStarting 既有席位（重启／启用／刷新凭据）的 starting：超时只记 error/start_timeout，不吊销不解绑。
func existingSeatStarting(detail string) bool {
	return detail == seatStartingRestart || detail == seatStartingEnable || detail == seatStartingRefresh
}

// SweepTimeouts 超时清扫（排队 10 分钟不可取消／拉镜像 30m／启动 5m；超时即失败不留半成品）。
func (o *Orchestrator) SweepTimeouts(ctx context.Context, now time.Time) {
	o.sweepMu.Lock()
	defer o.sweepMu.Unlock()
	rows, err := o.st.pool.Query(ctx, htx(), `
SELECT agent_id, state, state_detail, updated_at FROM g4_hosted_seat
WHERE state IN ('queued','pulling','starting')`)
	if err != nil {
		return
	}
	defer rows.Close()
	type pending struct {
		id, state string
		existing  bool // 既有席位的重启／启用（不是新建）
	}
	var timedOut []pending
	for rows.Next() {
		var id, state, detail string
		var updated time.Time
		if err := rows.Scan(&id, &state, &detail, &updated); err != nil {
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
			existing := state == "starting" && existingSeatStarting(detail)
			timedOut = append(timedOut, pending{id: id, state: reason, existing: existing})
		}
	}
	for _, p := range timedOut {
		if p.existing {
			// 🔴 既有席位重启／启用后 Agent 没连上：只记错，不吊销、不解绑 —— 那是用户在用的 Agent，
			//    把新建失败的回收套在它身上会把它的凭据吊销掉。记 error 后 Restart 仍可再试（guard 放行 error）。
			//    条件迁移：选出之后、写之前 Agent 恰好连入置 online 的，不被盖回 error。
			_, _ = o.st.TransitionSeatState(ctx, p.id, "starting", "error", p.state, "fail", json.RawMessage(`{"reason":"`+p.state+`","existing":true}`))
			continue
		}
		o.dropToken(p.id)
		seat, err := o.st.GetSeat(ctx, p.id)
		if err == nil && seat.AccountRef.Valid {
			_ = o.pool.Release(ctx, "system", p.id)
		}
		_ = o.st.SetSeatState(ctx, p.id, "error", p.state, "fail", json.RawMessage(`{"reason":"`+p.state+`"}`))
		_, _ = o.idp.AbortHostedAgent(ctx, p.id, "system")
	}
}

// ReconcileNode 节点重连对账（组长 7fabddb0 ⑤：离线销毁的席位，节点重连后补清容器与配额）。
// 接入点：nodegate 在节点接入成功时回调。每个待对账席位补发 destroy_seat（节点侧幂等）；
// 成功即写 reconcile_done 事件（SeatsPendingReconcile 不再命中）。失败留待下次重连再试。
func (o *Orchestrator) ReconcileNode(ctx context.Context, nodeID string) {
	ids, err := o.st.SeatsPendingReconcile(ctx, nodeID)
	if err != nil {
		return
	}
	for _, id := range ids {
		o.reconcileSeat(ctx, nodeID, id)
	}
}

// ReconcileSeatTimeout 对账时单个席位等节点回执的上限（复验建议：别让一个席位卡死整批）。
var ReconcileSeatTimeout = 30 * time.Second

// SetBackuper 注入备份器（装配处与 Lifecycle 用同一个 HubBackuper）。
func (o *Orchestrator) SetBackuper(b VolumeBackuper) { o.backups = b }

func (o *Orchestrator) reconcileSeat(ctx context.Context, nodeID, id string) {
	// 离线销毁的席位：先备份、成功才删（组长裁定 lead-g4-t53-review 第 4 条，业主 8971bddf「先备份再删数据」）。
	// 备份失败或没有备份器 ⇒ 本次不删、留待下次重连。备份走它自己的长时限（大卷），不受下面单席超时约束。
	pending, err := o.st.SeatBackupPending(ctx, id)
	if err != nil {
		return
	}
	if pending {
		if o.backups == nil {
			return
		}
		ref, err := o.backups.BackupSeatVolume(ctx, nodeID, id)
		if err != nil {
			return
		}
		if err := o.st.SetSeatState(ctx, id, "destroyed", "", "reconcile_backup_done",
			json.RawMessage(`{"backup":`+jsonStr(ref)+`,"ttl_days":14}`)); err != nil {
			return
		}
	}
	sctx, cancel := context.WithTimeout(ctx, ReconcileSeatTimeout)
	defer cancel()
	res, err := o.gw.Send(sctx, nodeID, NodeCommand{Type: "destroy_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(id) + `}`)})
	if err == nil && res.OK {
		// 写回执用外层 ctx：节点已执行，不能因单席超时恰在此刻到点而丢掉 reconcile_done。
		_ = o.st.SetSeatState(ctx, id, "destroyed", "", "reconcile_done", nil)
	}
}

func jsonStr(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
