package main

// T-G4-33/51 · console hosted 路由（契约 v2 逐字）：新建/选项/进度/生命周期四操作/管理配额。
//
// 归属与角色：actor（N3.Actor）＋ownsAgent（identity 归属）＋isManager（管理角色）全部注入——
// 本文件不复制身份语义。页面只认机器码：错误一律 {"error": 机器码}，quota_exceeded 带 remaining。

import (
	"context"
	"encoding/json"
	"errors"
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
	listUsers func(r *http.Request) ([]string, error)            // 管理配额页用户清单（identity 侧）
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

// writeHostedError 错误→机器码（契约第四节表；status 只分四类：400 参数/确认、403 归属与角色、
// 409 冲突（重名/超额/组关闭）、422 语义不成立（组不存在/状态不允许）、503 节点离线）。
func writeHostedError(w http.ResponseWriter, err error) {
	code := err.Error()
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, hosted.ErrQuotaExceeded):
		if qe, ok := err.(*hosted.QuotaExceededError); ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			var rem map[string]any
			_ = json.Unmarshal(qe.RemainingJSON(), &rem)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "quota_exceeded", "remaining": rem})
			return
		}
		code, status = "quota_exceeded", http.StatusConflict
	case errors.Is(err, hosted.ErrGroupNotFound):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, hosted.ErrGroupClosed), errors.Is(err, hosted.ErrPlanExceedsQuota):
		status = http.StatusConflict
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
	case errors.Is(err, hosted.ErrNotFound):
		code, status = "name_invalid", http.StatusNotFound // 席位/job 不存在（页面按「不存在」处理）
	}
	writeJSON(w, status, map[string]string{"error": code})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_invalid"})
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
	if !d.ownsAgent(r, actor, jobID) {
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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_owner"})
		return
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
	rows, err := d.qs.ListWithUsage(r.Context(), users)
	if err != nil {
		writeHostedError(w, err)
		return
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_invalid"}) // 四键齐全才接受（契约第三节）
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
