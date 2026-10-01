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

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

// hostedDeps 路由依赖（装配处注入；测试用桩）。
type hostedDeps struct {
	orch      *hosted.Orchestrator
	life      *hosted.Lifecycle
	qs        *hosted.QuotaService
	actor     func(r *http.Request) (string, error)
	ownsAgent func(r *http.Request, userLC, agentID string) bool // identity 归属（含管理用户豁免由身份侧定）
	isManager func(r *http.Request, userLC string) bool          // 管理角色（C6）
	listUsers func(r *http.Request) ([]quotaUser, error)         // 管理配额页用户清单（identity 侧：id＋短名）
	// seatOwnerOf 席位级归属（created_by）——创建失败 Retire 后 identity 名下已不含该 Agent，
	// 但失败蒙版还要能读原因（联调第 2 条）：jobs/生命周期归属先按席位 created_by 判。
	seatOwnerOf func(r *http.Request, agentID string) (string, bool)
}

// quotaUser 配额页用户行（identity 侧取：id＝登录名小写、short_name＝显示短名）。
type quotaUser struct {
	ID        string
	ShortName string
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
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeHostedError 错误→机器码＋HTTP 状态（契约第四节状态表逐字）：
// 401 unauthenticated · 403 not_owner/forbidden_role · 400 name_invalid/params_invalid/combo_*/
// account_not_allowed/confirm_required · 409 name_taken/quota_exceeded/plan_exceeds_quota/
// node_no_capacity/group_closed/upgrade_failed_rolled_back · 422 group_not_found/invalid_state ·
// 502 start_failed · 503 node_offline/backup_failed · 500 internal_error（未识别的错误一律
// internal_error，真实原因只进日志——错误字段永不拼 Go 文本）。
func writeHostedError(w http.ResponseWriter, err error) {
	code := ""
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, hosted.ErrQuotaExceeded):
		if qe, ok := err.(*hosted.QuotaExceededError); ok {
			writeHostedRemaining(w, "quota_exceeded", qe.RemainingJSON())
			return
		}
		code, status = "quota_exceeded", http.StatusConflict
	case errors.Is(err, hosted.ErrPlanExceedsQuota):
		if pe, ok := err.(*hosted.PlanExceededError); ok {
			writeHostedRemaining(w, "plan_exceeds_quota", pe.RemainingJSON())
			return
		}
		code, status = "plan_exceeds_quota", http.StatusConflict
	case errors.Is(err, hosted.ErrBackupFailed):
		code, status = "backup_failed", http.StatusServiceUnavailable
		v := map[string]any{"error": code}
		if be, ok := err.(*hosted.BackupFailedError); ok && be.Detail != "" {
			v["detail"] = be.Detail // 契约：detail 只供日志与审计，页面不读不显示
		}
		writeJSON(w, status, v)
		return
	case errors.Is(err, hosted.ErrGroupNotFound):
		code, status = "group_not_found", http.StatusUnprocessableEntity
	case errors.Is(err, hosted.ErrGroupClosed):
		code, status = "group_closed", http.StatusConflict
	case errors.Is(err, hosted.ErrNotOwner):
		code, status = "not_owner", http.StatusForbidden
	case errors.Is(err, hosted.ErrForbiddenRole):
		code, status = "forbidden_role", http.StatusForbidden
	case errors.Is(err, hosted.ErrInvalidState):
		code, status = "invalid_state", http.StatusUnprocessableEntity
	case errors.Is(err, hosted.ErrConfirmRequired):
		code, status = "confirm_required", http.StatusBadRequest
	case errors.Is(err, hosted.ErrNodeOffline):
		code, status = "node_offline", http.StatusServiceUnavailable
	case errors.Is(err, hosted.ErrRolledBack):
		code, status = "upgrade_failed_rolled_back", http.StatusConflict
	case errors.Is(err, hosted.ErrStartFailed):
		code, status = "start_failed", http.StatusBadGateway
	case errors.Is(err, hosted.ErrNameTaken):
		code, status = "name_taken", http.StatusConflict
	case errors.Is(err, hosted.ErrNameInvalid):
		code, status = "name_invalid", http.StatusBadRequest
	case errors.Is(err, hosted.ErrParamsInvalid), errors.Is(err, hosted.ErrParams):
		code, status = "params_invalid", http.StatusBadRequest
	case errors.Is(err, hosted.ErrComboIncompatible), errors.Is(err, hosted.ErrComboNotVerified),
		errors.Is(err, hosted.ErrAccountNotAllowed):
		status = http.StatusBadRequest
		code = err.Error()
	case errors.Is(err, hosted.ErrNoNodeCapacity):
		code, status = "node_no_capacity", http.StatusConflict
	case errors.Is(err, hosted.ErrNotFound):
		code, status = "name_invalid", http.StatusNotFound // 席位/job 不存在（页面按「不存在」处理）
	}
	if code == "" && isMachineCode(err.Error()) {
		// 适配器侧以纯文本机器码返回的（newHostedIdentity 不 import hosted 的纪律）——放行，400
		code, status = err.Error(), http.StatusBadRequest
	}
	if code == "" || (code != err.Error() && !isMachineCode(code)) {
		// 未识别错误：不外泄 Go 文本
		log.Printf("hosted route unmapped error: %v", err)
		code = "internal_error"
	}
	writeJSON(w, status, map[string]string{"error": code})
}

// isMachineCode 机器码形状（小写蛇形）——防 Go 错误文本漏进 error 字段。
func isMachineCode(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r == '_') {
			return false
		}
	}
	return true
}

func writeHostedRemaining(w http.ResponseWriter, code string, remaining json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	var rem map[string]any
	_ = json.Unmarshal(remaining, &rem)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "remaining": rem})
}

func (d hostedDeps) handleOptions(w http.ResponseWriter, r *http.Request) {
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "params_invalid"})
		return
	}
	out, err := d.orch.Create(r.Context(), hosted.CreateRequest{
		OwnerLC: actor, By: actor, Name: req.Name, GroupCode: req.GroupID,
		NodeID: req.NodeID, CLI: req.CLI, LLM: req.LLM, AccountID: req.AccountID, Plan: req.PlanID,
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
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	jobID := r.PathValue("jobID")
	// 🔴 联调第 2 条：失败的创建会 Retire（名下清单不再含它），但失败蒙版必须仍能读
	//    到最后阶段与原因——归属按席位 created_by 判（席位行在 Retire 后仍在）
	owner, ok := d.seatOwnerOf(r, jobID)
	if !ok || owner != actor {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_owner"})
		return
	}
	jv, err := d.orch.JobProgress(r.Context(), jobID)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jv)
}

func (d hostedDeps) handleLifecycle(w http.ResponseWriter, r *http.Request) {
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	agentID, op := r.PathValue("agentID"), r.PathValue("op")
	if !d.ownsAgent(r, actor, agentID) {
		if owner, ok := d.seatOwnerOf(r, agentID); !ok || owner != actor {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_owner"})
			return
		}
	}
	var body struct {
		Confirm bool   `json:"confirm"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // 空体合法（upgrade/restart/enable 无参）
	var lerr error
	switch op {
	case "upgrade":
		lerr = d.life.Upgrade(r.Context(), agentID, actor, body.Version, false)
	case "restart":
		lerr = d.life.Restart(r.Context(), agentID, actor, false)
	case "disable":
		lerr = d.life.Disable(r.Context(), agentID, actor, false, body.Confirm)
	case "enable":
		lerr = d.life.Enable(r.Context(), agentID, actor, false)
	case "destroy":
		lerr = d.life.Destroy(r.Context(), agentID, actor, false, body.Confirm)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "name_invalid"})
		return
	}
	if lerr != nil {
		writeHostedError(w, lerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "ok"})
}

func (d hostedDeps) handleQuotaList(w http.ResponseWriter, r *http.Request) {
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
	// 契约第三节形状：[{user:{id,short_name}, agents:{used,limit}, vcpu:{…}, mem_gb:{…}(GB), disk_gb:{…}}]
	type dim struct {
		Used  int64 `json:"used"`
		Limit int64 `json:"limit"`
	}
	rows := []map[string]any{}
	for _, u := range users {
		wu, err := d.qs.GetWithUsage(r.Context(), u.ID)
		if err != nil {
			writeHostedError(w, err)
			return
		}
		rows = append(rows, map[string]any{
			"user":    map[string]string{"id": u.ID, "short_name": u.ShortName},
			"agents":  dim{Used: int64(wu.Usage.Agents), Limit: int64(wu.Quota.AgentsLimit)},
			"vcpu":    dim{Used: int64(wu.Usage.VCPU), Limit: int64(wu.Quota.VCPULimit)},
			"mem_gb":  dim{Used: wu.Usage.MemMB / 1024, Limit: wu.Quota.MemMBLimit / 1024},
			"disk_gb": dim{Used: wu.Usage.DiskMB / 1024, Limit: wu.Quota.DiskMBLimit / 1024},
		})
	}
	writeJSON(w, http.StatusOK, rows)
}

func (d hostedDeps) handleQuotaPut(w http.ResponseWriter, r *http.Request) {
	actor, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "params_invalid"}) // 四键齐全才接受（契约第三节）
		return
	}
	err = d.qs.Put(r.Context(), actor, true, hosted.Quota{
		UserLC:      r.PathValue("userID"),
		AgentsLimit: *body.AgentsLimit, VCPULimit: *body.VCPULimit,
		MemMBLimit: *body.MemGBLimit * 1024, DiskMBLimit: *body.DiskGBLimit * 1024,
	})
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "ok"})
}
