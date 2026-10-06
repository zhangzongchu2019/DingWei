package main

// T-G4-33/51 · console hosted 路由（契约 v2 逐字）：新建/选项/进度/生命周期四操作/管理配额。
//
// 归属与角色：actor（N3.Actor）＋ownsAgent（identity 归属）＋isManager（管理角色）全部注入——
// 本文件不复制身份语义。页面只认机器码：错误一律 {"error": 机器码}，quota_exceeded 带 remaining。

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// hostedDeps 路由依赖（装配处注入；测试用桩）。
type hostedDeps struct {
	orch      *hosted.Orchestrator
	life      *hosted.Lifecycle
	qs        *hosted.QuotaService
	actor     func(r *http.Request) (string, error)
	ownsAgent func(r *http.Request, userLC, agentID string) bool // identity 归属（含管理用户豁免由身份侧定）
	isManager func(r *http.Request, userLC string) bool          // 管理角色（C6）
	listUsers func(r *http.Request) ([]hostedUser, error)        // 管理配额页用户清单（identity 侧）
	// 0.2.1.101 账号包（R-V2-ACCOUNT-PER-USER）
	pool         *hosted.PoolService
	publicOrigin string // 写接口 Origin 守卫（与 N3 同口径：Origin 须＝PublicOrigin，否则 403 origin_mismatch）
}

// hostedUser 管理配额页一行的用户身份：ID 原样展示，IDLC 是配额行的键（g4_quota.user_lc）。
type hostedUser struct {
	ID, IDLC, ShortName string
}

func registerHostedRoutes(mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}, d hostedDeps) {
	mux.HandleFunc("GET /api/console/hosted/options", d.handleOptions)
	mux.HandleFunc("POST /api/console/hosted/agents", d.handleCreate)
	mux.HandleFunc("GET /api/console/hosted/jobs/{jobID}", d.handleJob)
	mux.HandleFunc("POST /api/console/hosted/agents/{agentID}/{op}", d.handleLifecycle)
	mux.HandleFunc("GET /api/console/admin/quotas", d.handleQuotaList)
	mux.HandleFunc("PUT /api/console/admin/quotas/{userID}", d.handleQuotaPut)
	// 我的 LLM 账号（属主本人；他人一律 404）
	mux.HandleFunc("GET /api/console/llm-accounts", d.handleAccountList)
	mux.HandleFunc("POST /api/console/llm-accounts", d.handleAccountRegister)
	mux.HandleFunc("PUT /api/console/llm-accounts/{id}/secret", d.handleAccountSecret)
	mux.HandleFunc("POST /api/console/llm-accounts/{id}/disable", d.handleAccountState(true))
	mux.HandleFunc("POST /api/console/llm-accounts/{id}/enable", d.handleAccountState(false))
	// 管理用户：只看元数据、停用、启用（不见密钥、不能换密——组长 59bf5349）
	mux.HandleFunc("GET /api/console/admin/llm-accounts", d.handleAdminAccountList)
	mux.HandleFunc("POST /api/console/admin/llm-accounts/{id}/disable", d.handleAdminAccountState(true))
	mux.HandleFunc("POST /api/console/admin/llm-accounts/{id}/enable", d.handleAdminAccountState(false))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeHostedError 错误 → 契约第四节机器码（组长裁定 lead-g4-joint-run-1 第 3 条：一律机器码，不出 Go 错误文本）。
// 每个可能到达这里的错误都显式列出；列不到的是内部故障：答 500 internal_error，原文只进日志。
func writeHostedError(w http.ResponseWriter, err error) {
	var qe *hosted.QuotaExceededError
	var pe *hosted.PlanExceedsQuotaError
	var be *hosted.BackupFailedError
	switch {
	case errors.As(err, &qe):
		writeRemaining(w, "quota_exceeded", qe.RemainingJSON())
		return
	case errors.As(err, &pe):
		writeRemaining(w, "plan_exceeds_quota", pe.RemainingJSON())
		return
	case errors.As(err, &be):
		// 原因放 detail：页面不读不显示，供日志与审计（契约第四节）。
		log.Printf("hosted: backup_failed: %s", be.Detail())
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "backup_failed", "detail": be.Detail()})
		return
	}
	for _, m := range hostedErrorCodes {
		if errors.Is(err, m.err) {
			writeJSON(w, m.status, map[string]string{"error": m.code})
			return
		}
	}
	log.Printf("hosted: internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
}

func writeRemaining(w http.ResponseWriter, code string, remaining json.RawMessage) {
	var rem map[string]any
	_ = json.Unmarshal(remaining, &rem)
	writeJSON(w, http.StatusConflict, map[string]any{"error": code, "remaining": rem})
}

// hostedErrorCodes 哨兵 → 机器码与状态。顺序即优先级（errors.Is 先中先答）。
var hostedErrorCodes = []struct {
	err    error
	code   string
	status int
}{
	// 身份侧（经 G3 适配器原样透传上来的 identity 错误）
	{identity.ErrNameInvalid, "name_invalid", http.StatusBadRequest},
	{identity.ErrNameTaken, "name_taken", http.StatusConflict},
	{identity.ErrUnknownOwner, "forbidden_role", http.StatusForbidden},
	// 创建
	{hosted.ErrComboIncompatible, "combo_incompatible", http.StatusBadRequest},
	{hosted.ErrComboNotVerified, "combo_not_verified", http.StatusBadRequest},
	{hosted.ErrAccountNotAllowed, "account_not_allowed", http.StatusBadRequest},
	{hosted.ErrAccountBound, "account_not_allowed", http.StatusConflict},
	{hosted.ErrNoNodeCapacity, "node_no_capacity", http.StatusConflict},
	{hosted.ErrQuotaExceeded, "quota_exceeded", http.StatusConflict},
	{hosted.ErrPlanExceedsQuota, "plan_exceeds_quota", http.StatusConflict},
	{hosted.ErrGroupNotFound, "group_not_found", http.StatusUnprocessableEntity},
	// 包 B2（修订 2：与 invalid_state 同族 422；C3 表单据此显示中文原因）
	{hosted.ErrModelNotAvailable, "model_not_available", http.StatusUnprocessableEntity},
	{hosted.ErrModelNotSupported, "model_not_supported", http.StatusUnprocessableEntity},
	{hosted.ErrGroupClosed, "group_closed", http.StatusConflict},
	{hosted.ErrParams, "params_invalid", http.StatusBadRequest},
	// 操作
	{hosted.ErrNotOwner, "not_owner", http.StatusForbidden},
	{hosted.ErrForbiddenRole, "forbidden_role", http.StatusForbidden},
	{hosted.ErrConfirmRequired, "confirm_required", http.StatusBadRequest},
	{hosted.ErrInvalidState, "invalid_state", http.StatusUnprocessableEntity},
	{hosted.ErrNotFound, "invalid_state", http.StatusUnprocessableEntity}, // 不是托管席位（如自备 Agent）：对它不允许托管操作
	{hosted.ErrNodeOffline, "node_offline", http.StatusServiceUnavailable},
	{hosted.ErrStartFailed, "start_failed", http.StatusBadGateway},
	{hosted.ErrRolledBack, "upgrade_failed_rolled_back", http.StatusConflict},
	// 0.2.1.101 账号包
	{hosted.ErrAccountNameTaken, "account_name_taken", http.StatusConflict},
	{hosted.ErrAccountLimit, "account_limit", http.StatusConflict},
	{hosted.ErrAccountInUse, "account_in_use", http.StatusConflict},
	{hosted.ErrSecretTooLarge, "too_large", http.StatusRequestEntityTooLarge},
	{hosted.ErrNodeUnsupported, "node_unsupported", http.StatusConflict},
	{hosted.ErrCredInjectFailed, "credential_inject_failed", http.StatusBadGateway},
}

// actorOf 认人；失败按新平台 N3 统一口径答：无会话 401 unauthenticated，必须改密 403 must_change_password。
func (d hostedDeps) actorOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, err := d.actor(r)
	if err == nil {
		return actor, true
	}
	if errors.Is(err, identity.ErrMustChange) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "must_change_password"})
	} else {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
	}
	return "", false
}

func (d hostedDeps) handleOptions(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	v, err := d.orch.Options(r.Context(), actor)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d hostedDeps) handleCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	if !d.sameOrigin(w, r) {
		return
	}
	var req struct {
		Name      string `json:"name"`
		GroupID   string `json:"group_id"` // 编组的 32 位 code（与 identity 批准接口 group_code 同值同义；可缺省）
		NodeID    string `json:"node_id"`
		CLI       string `json:"cli"`
		LLM       string `json:"llm"`
		AccountID string `json:"account_id"`
		PlanID    string `json:"plan_id"`
		Model     string `json:"model"` // 包 B2：按席模型（可空）
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_invalid"})
		return
	}
	out, err := d.orch.Create(r.Context(), hosted.CreateRequest{
		OwnerLC: actor, By: actor, Name: req.Name, GroupCode: req.GroupID,
		NodeID: req.NodeID, CLI: req.CLI, LLM: req.LLM, AccountID: req.AccountID, Plan: req.PlanID, Model: req.Model,
	})
	if err != nil {
		writeHostedError(w, err)
		return
	}
	// 异步执行链（蒙版轮询 jobs 端点）；失败清扫由编排器/SweepTimeouts 兜
	go d.orch.RunJob(context.Background(), out.AgentID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": out.JobID})
}

func (d hostedDeps) handleJob(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	// 归属按席位 created_by（含 Agent 已吊销的失败任务），不走身份侧 ownsAgent —— 见 JobProgressFor。
	jobID := r.PathValue("jobID")
	jv, err := d.life.JobProgressFor(r.Context(), jobID, actor)
	if errors.Is(err, hosted.ErrNotFound) {
		jv, err = d.orch.JobProgressFor(r.Context(), jobID, actor)
	}
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jv)
}

func (d hostedDeps) handleLifecycle(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	if !d.sameOrigin(w, r) {
		return
	}
	agentID, op := r.PathValue("agentID"), r.PathValue("op")
	if op != "upgrade" && op != "restart" && op != "disable" && op != "enable" && op != "destroy" && op != "refresh_cred" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invalid_state"})
		return
	}
	if !d.ownsAgent(r, actor, agentID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_owner"})
		return
	}
	var body struct {
		Confirm bool   `json:"confirm"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // 空体合法（upgrade/restart/enable 无参）
	jobID, err := d.life.EnqueueJob(r.Context(), hosted.LifecycleJobRequest{
		AgentID: agentID, Actor: actor, Op: op, Version: body.Version, Confirm: body.Confirm,
	})
	if err != nil {
		writeHostedError(w, err)
		return
	}
	go d.life.RunJob(context.Background(), jobID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func (d hostedDeps) handleQuotaList(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	if !d.isManager(r, actor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden_role"})
		return
	}
	users, err := d.listUsers(r)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	lcs := make([]string, len(users))
	for i, u := range users {
		lcs[i] = u.IDLC
	}
	rows, err := d.qs.ListWithUsage(r.Context(), lcs)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	// 契约第三节形状（组长裁定 lead-g4-joint-run-1 第 5 条）：内存、磁盘以 GB 计。
	type usedLimit struct {
		Used  int64 `json:"used"`
		Limit int64 `json:"limit"`
	}
	type quotaRow struct {
		User struct {
			ID        string `json:"id"`
			ShortName string `json:"short_name"`
		} `json:"user"`
		Agents usedLimit `json:"agents"`
		VCPU   usedLimit `json:"vcpu"`
		MemGB  usedLimit `json:"mem_gb"`
		DiskGB usedLimit `json:"disk_gb"`
	}
	out := make([]quotaRow, 0, len(rows))
	for i, q := range rows {
		var row quotaRow
		row.User.ID, row.User.ShortName = users[i].ID, users[i].ShortName
		row.Agents = usedLimit{int64(q.Usage.Agents), int64(q.Quota.AgentsLimit)}
		row.VCPU = usedLimit{int64(q.Usage.VCPU), int64(q.Quota.VCPULimit)}
		row.MemGB = usedLimit{q.Usage.MemMB / 1024, q.Quota.MemMBLimit / 1024}
		row.DiskGB = usedLimit{q.Usage.DiskMB / 1024, q.Quota.DiskMBLimit / 1024}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (d hostedDeps) handleQuotaPut(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	if !d.sameOrigin(w, r) {
		return
	}
	if !d.isManager(r, actor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden_role"})
		return
	}
	var body struct {
		AgentsLimit *int   `json:"agents_limit"`
		VCPULimit   *int   `json:"vcpu_limit"`
		MemGBLimit  *int64 `json:"mem_gb_limit"`
		DiskGBLimit *int64 `json:"disk_gb_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.AgentsLimit == nil || body.VCPULimit == nil || body.MemGBLimit == nil || body.DiskGBLimit == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_invalid"}) // 四键齐全才接受（契约第三节）
		return
	}
	err := d.qs.Put(r.Context(), actor, true, hosted.Quota{
		UserLC:      strings.ToLower(r.PathValue("userID")), // 配额行按小写用户 ID 存（与身份侧 user_id_lc 一致）
		AgentsLimit: *body.AgentsLimit, VCPULimit: *body.VCPULimit,
		MemMBLimit: *body.MemGBLimit * 1024, DiskMBLimit: *body.DiskGBLimit * 1024,
	})
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "ok"})
}

// sameOrigin 写接口 Origin 守卫（0.2.1.101 补漏：hosted 写路由原先只核会话；与 N3 authed 同口径与同错误码）。
// 不符 ⇒ 403 origin_mismatch，零副作用。publicOrigin 未配（仅测试装配）时一律拒，不放行。
func (d hostedDeps) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if d.publicOrigin != "" && r.Header.Get("Origin") == d.publicOrigin {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_mismatch"})
	return false
}

// ---------- 我的 LLM 账号（0.2.1.101，R-V2-ACCOUNT-PER-USER） ----------

// accountView 账号元数据应答形状（永不含密钥；token_file 无 last4）。
type accountView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CLI       string `json:"cli"`
	LLM       string `json:"llm"`
	Last4     string `json:"last4,omitempty"` // 只给属主本人；管理视图恒空（不序列化）
	State     string `json:"state"`           // available｜disabled（存量 bound 显示为 available）
	InUse     int    `json:"in_use"`
	CreatedAt string `json:"created_at"`
	Owner     string `json:"owner,omitempty"` // 只在管理视图出
}

func toAccountView(a hosted.Account, withOwner bool) accountView {
	st := a.State
	if st == "bound" {
		st = "available" // 历史状态：0.2.1.101 起占用看引用数，不再有独占
	}
	v := accountView{ID: a.ID, Name: a.Name, Kind: a.Kind, CLI: a.CLI, LLM: a.LLM, Last4: a.Last4, State: st,
		InUse: a.InUse, CreatedAt: a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
	if withOwner { // 管理视图：带属主、**不带 last4**——api_key 的后四位也是密钥片段，只给属主本人看（复核 tosonnet55tc2k）
		v.Owner = a.OwnerLC
		v.Last4 = ""
	}
	return v
}

// writeAccountError 账号路由：不存在／非本人统一 404 not_found（不区分，防探测）；其余同托管机器码。
func writeAccountError(w http.ResponseWriter, err error) {
	if errors.Is(err, hosted.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeHostedError(w, err)
}

func (d hostedDeps) handleAccountList(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	as, err := d.pool.ListOwned(r.Context(), actor)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	out := make([]accountView, 0, len(as))
	for _, a := range as {
		out = append(out, toAccountView(a, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// readSecretBody 读请求体（上限＝凭据上限＋余量）；超限 ⇒ too_large。🔴 请求体不进任何日志。
func readSecretBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, hosted.MaxSecretBytes+4096)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "params_invalid"})
		}
		return false
	}
	return true
}

func (d hostedDeps) handleAccountRegister(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok || !d.sameOrigin(w, r) {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		CLI    string `json:"cli"`
		LLM    string `json:"llm"`
		Secret string `json:"secret"`
	}
	if !readSecretBody(w, r, &body) {
		return
	}
	if !d.orch.AccountComboKnown(body.CLI, body.LLM, body.Kind) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "combo_incompatible"})
		return
	}
	a, err := d.pool.RegisterOwned(r.Context(), actor, body.Name, body.Kind, body.CLI, body.LLM, body.Secret)
	body.Secret = ""
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAccountView(a, false))
}

func (d hostedDeps) handleAccountSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok || !d.sameOrigin(w, r) {
		return
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if !readSecretBody(w, r, &body) {
		return
	}
	n, err := d.pool.UpdateSecretOwned(r.Context(), actor, r.PathValue("id"), body.Secret)
	body.Secret = ""
	if err != nil {
		writeAccountError(w, err)
		return
	}
	// in_use：页面据此提示「有 N 个 Agent 在用，可逐个刷新」——换密不触达在跑席位。
	writeJSON(w, http.StatusOK, map[string]int{"in_use": n})
}

func (d hostedDeps) handleAccountState(disable bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.actorOf(w, r)
		if !ok || !d.sameOrigin(w, r) {
			return
		}
		if err := d.pool.SetOwnedState(r.Context(), actor, r.PathValue("id"), disable); err != nil {
			writeAccountError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d hostedDeps) handleAdminAccountList(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.actorOf(w, r)
	if !ok {
		return
	}
	if !d.isManager(r, actor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden_role"})
		return
	}
	as, err := d.pool.ListAdmin(r.Context(), actor, true)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	out := make([]accountView, 0, len(as))
	for _, a := range as {
		out = append(out, toAccountView(a, true))
	}
	writeJSON(w, http.StatusOK, out)
}

func (d hostedDeps) handleAdminAccountState(disable bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.actorOf(w, r)
		if !ok || !d.sameOrigin(w, r) {
			return
		}
		if !d.isManager(r, actor) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden_role"})
			return
		}
		if err := d.pool.SetStateAdmin(r.Context(), actor, true, r.PathValue("id"), disable); err != nil {
			writeAccountError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
