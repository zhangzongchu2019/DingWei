package m8

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台 spec／任务的 Agent 侧（R-V2-SPEC-TASK-SHIM-IMPL G2、G3；复验方先审一）。
//
// 签发：人在 console 里对【本人】的 Agent 签 spec／任务三类页面 id 的能力令牌（handleNewPlatformCapability）。
// 使用：Agent 以 /ws/agent 同一张专属凭据（Authorization: Bearer dwa_…）＋能力令牌调 /api/agent/console/*
// （newPlatformSpecAgentCapability）。不认旧 service key 与 X-DW-Agent-* 头，不看 consoleAuthEnabled。
// 🔵 095 本身没有把这两类令牌交到 Agent 手里的链路（方案第六节答复①）；本件只做签发与校验，投递登记另议。

// newPlatformCapabilityTTL 与旧签发口同（5 分钟）。
const newPlatformCapabilityTTL = 5 * time.Minute

// handleNewPlatformCapability POST /api/console/agents/{full_name}/capability 的新平台分支。
// 前置：RequireConsoleAgent 新平台分支已按 cookie＋full_name 认过归属用户、校过 Origin。
// 只签 ConsoleAgentCapabilityPageAllowed 的三类页面 id；终端页 id 仍 404（view 写权走飞书 ＃unlock，现状不动）。
func (h *Hub) handleNewPlatformCapability(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PageID string `json:"page_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req) != nil || strings.TrimSpace(req.PageID) == "" {
		http.Error(w, "page_id required", http.StatusBadRequest)
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	if !ConsoleAgentCapabilityPageAllowed(pageID) {
		writeAgentWSError(w, http.StatusNotFound, "capability_not_used")
		return
	}
	repo, repoOK := h.Repo.(consoleAuthRepository)
	ag, _, err := h.Identity.ResolveFullName(r.Context(), strings.TrimSpace(r.PathValue("sessionName")))
	if !repoOK || err != nil {
		writeAgentWSError(w, http.StatusNotFound, "not_found")
		return
	}
	sess, ok := h.newPlatformSessionProjection(r)
	if !ok || sess.PrincipalID != ag.OwnerLC { // D4 只属主（RequireConsoleAgent 已拦，这里再钉一次）
		writeAgentWSError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := h.syncNewPlatformUser(r.Context(), ag.OwnerLC, sess); err != nil {
		log.Printf("newplat shim: sync user %s: %v", ag.OwnerLC, err)
		writeAgentWSError(w, http.StatusServiceUnavailable, "console_unavailable")
		return
	}
	raw, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "capability unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "capability unavailable", http.StatusServiceUnavailable)
		return
	}
	now := h.now()
	capability := model.ConsolePageCapability{ID: id, SessionID: sess.ID, PrincipalID: sess.PrincipalID, AgentID: ag.ID, PageID: pageID,
		TokenHash: hashConsoleToken(raw), ExpiresAt: now.Add(newPlatformCapabilityTTL)}
	// 存储在同一事务里核：投影会话有效、Agent active（暂停 ⇒ 拒）、属主 interact 授权。
	if err := repo.RotateConsolePageCapability(r.Context(), capability, AgentKeyID(ag.OwnerLC), ag.NameLC(), now); err != nil {
		writeAgentWSError(w, http.StatusForbidden, "forbidden")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"page_id": pageID, "capability": raw, "expires_in": int(newPlatformCapabilityTTL / time.Second)})
}

// newPlatformSpecAgentCapability RequireConsoleSpecAgentCapability 的新平台分支。
// 每次现查：凭据有效且 Agent 未吊销未暂停、属主 active（AuthenticateAgent）；能力令牌属该 Agent、该页面、属主本人签发；
// 签发它的新平台会话此刻仍有效（人被停用或口令被重置 ⇒ 立即失效，复验方 H11 → 本件 H14）。
func (h *Hub) newPlatformSpecAgentCapability(pageID func(*http.Request) string, next http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	deny := func() { http.Error(w, "console capability required", http.StatusForbidden) }
	repo, repoOK := h.Repo.(consoleAuthRepository)
	cred := ""
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		cred = strings.TrimSpace(auth[7:])
	}
	raw := strings.TrimSpace(r.Header.Get("X-DW-Console-Capability"))
	expectedPage := pageID(r)
	if !repoOK || cred == "" || raw == "" || expectedPage == "" {
		deny()
		return
	}
	ag, _, err := h.Identity.AuthenticateAgent(r.Context(), cred)
	if err != nil {
		deny()
		return
	}
	if err := h.syncNewPlatformUser(r.Context(), ag.OwnerLC, nil); err != nil {
		log.Printf("newplat shim: sync user %s: %v", ag.OwnerLC, err)
		http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
		return
	}
	capability, err := repo.ResolveConsolePageCapability(r.Context(), hashConsoleToken(raw), ag.ID, expectedPage, h.now())
	if err != nil {
		http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
		return
	}
	if capability == nil || capability.PrincipalID != identity.Lower(ag.OwnerLC) ||
		!h.newPlatformSessionValid(r.Context(), capability.SessionID, capability.PrincipalID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, capability.PrincipalID)
	ctx = context.WithValue(ctx, consoleSpecActorContextKey{}, consoleSpecActor{ID: capability.AgentID, Kind: "agent"})
	next(w, r.WithContext(ctx))
}
