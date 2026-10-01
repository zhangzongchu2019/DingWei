package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 新平台 G3：Agent 以专属凭据连入（设计件 runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md 2.3）。
//
// 组长裁定（台账 bfc84543）：握手只带专属凭据，地址里不自报名字；Agent 身份由 Hub 按凭据查出。
// 桥接裁定（台账 ruling-2026-10-01T090343Z-lead-ws-agent-bridge）：每用户一个虚拟 key，
// key_id＝owner_key＝"u_"＋用户 ID 小写，会话名＝Agent 名称小写；认完人后交给 serveSession，其后机制不动。

const (
	// StatusCredentialRevoked 在线时被吊销，Hub 以此关闭码断开（客户端据此调 revoke_local）。
	StatusCredentialRevoked websocket.StatusCode = 4401
)

// agentLivenessProbe 同一凭据已在线时，探活旧连接的时限（变量只为测试可缩短）。
var agentLivenessProbe = 5 * time.Second

// AgentKeyID 用户在会话机制里的虚拟 key。
func AgentKeyID(ownerLC string) string { return "u_" + identity.Lower(ownerLC) }

func writeAgentWSError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// HandleAgentWS GET /ws/agent。
func (h *Hub) HandleAgentWS(w http.ResponseWriter, r *http.Request) {
	if h.Identity == nil {
		http.NotFound(w, r)
		return
	}
	timingStart := time.Now()
	auth := r.Header.Get("Authorization")
	cred := ""
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		cred = strings.TrimSpace(auth[7:])
	}
	if cred == "" {
		writeAgentWSError(w, http.StatusUnauthorized, "credential_invalid")
		return
	}
	ag, fullName, err := h.Identity.AuthenticateAgent(r.Context(), cred)
	switch {
	case errors.Is(err, identity.ErrCredentialRevoked):
		writeAgentWSError(w, http.StatusUnauthorized, "credential_revoked")
		return
	case err != nil:
		writeAgentWSError(w, http.StatusUnauthorized, "credential_invalid")
		return
	}
	keyID := AgentKeyID(ag.OwnerLC)
	sessionName := ag.NameLC()

	// 同一凭据已在线（V1 A5：不顶掉在线者）：先探活旧连接；活着就拒新连接，死了才让 serveSession 换掉它。
	h.mu.Lock()
	old := h.sessionClients[keyID][sessionName]
	h.mu.Unlock()
	if old != nil && old.conn != nil {
		ctx, cancel := context.WithTimeout(r.Context(), agentLivenessProbe)
		alive := old.conn.Ping(ctx) == nil
		cancel()
		if alive {
			writeAgentWSError(w, http.StatusConflict, "already_connected")
			return
		}
	}

	q := r.URL.Query() // 只采信能力类参数；名字、key_id 一律不看
	h.serveSession(w, r, sessionAdmission{
		keyID: keyID, ownerKey: keyID, requestedSessionName: sessionName, sessionName: sessionName,
		fullSessionName: fullName, clientIP: clientIPFromRequest(r),
		tool: strings.TrimSpace(q.Get("tool")), clientVersion: strings.TrimSpace(q.Get("version")),
		osName:      strings.TrimSpace(q.Get("os")),
		timingStart: timingStart, timingLast: time.Now(),
	})
}

// DisconnectAgent 吊销后由 identity.Service.OnRevoked 调用：在线则以 4401 断开（V2 A1）。
// 必须在吊销请求返回之前完成，所以是同步的。
func (h *Hub) DisconnectAgent(agentID string) {
	if h.Identity == nil {
		return
	}
	ag, err := h.Identity.Store.GetAgent(context.Background(), agentID)
	if err != nil {
		return
	}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()]
	h.mu.Unlock()
	if c != nil && c.conn != nil {
		_ = c.conn.Close(StatusCredentialRevoked, "credential_revoked")
	}
}

// AgentOnline 供 console 的 Agent 列表显示在线状态。
func (h *Hub) AgentOnline(agentID string) bool {
	if h.Identity == nil {
		return false
	}
	ag, err := h.Identity.Store.GetAgent(context.Background(), agentID)
	if err != nil || ag.State != identity.AgentActive {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()] != nil
}

// isInternalSource 新平台下 /ws/service 只收本机与内网来源（台账 c796e65e）。
// 直连方与代理头里出现的【每一个】地址都必须是回环或私网：X-Forwarded-For 的首项可由客户端伪造，
// 只看首项（clientIPFromRequest 的取法）会被「先塞一个内网地址」绕过。
func isInternalSource(r *http.Request) bool {
	internal := func(s string) bool {
		ip := net.ParseIP(strings.TrimSpace(s))
		return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
	}
	if !internal(hostOnly(r.RemoteAddr)) {
		return false
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		for _, hop := range strings.Split(xff, ",") {
			if !internal(hop) {
				return false
			}
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" && !internal(real) {
		return false
	}
	return true
}
