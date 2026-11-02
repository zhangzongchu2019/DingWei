package m8

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	case errors.Is(err, identity.ErrCredentialSuspended):
		// 停用中（组长裁定）：客户端据此停止重连，但【不删】本地凭据，等启用。
		writeAgentWSError(w, http.StatusUnauthorized, "credential_suspended")
		return
	case err != nil:
		writeAgentWSError(w, http.StatusUnauthorized, "credential_invalid")
		return
	}
	keyID := AgentKeyID(ag.OwnerLC)
	sessionName := ag.NameLC()
	// S-HELPER-SELF-UPDATE-DW123: these are capability/discovery headers, not
	// authentication.  The bearer credential above remains the only identity
	// proof.  In particular the helper must use the exact source advertised by
	// this authenticated Hub for update-only provision validation; it must not
	// derive an owner key from a display name or accept a broad workpulse suffix.
	w.Header().Set("X-DW-Helper-Update", "helper-update/v1")
	w.Header().Set("X-DW-Helper-Update-Source", sessionAddress("workpulse", keyID))
	// Bind helper-side wake checks to the authenticated Agent, never a message's
	// claimed identity or a request header/query supplied by the client.
	w.Header().Set("X-DW-Agent-ID", ag.ID)

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
	adm := sessionAdmission{
		keyID: keyID, ownerKey: keyID, requestedSessionName: sessionName, sessionName: sessionName,
		fullSessionName: fullName, clientIP: "", credentialIdentity: true,
		metricsAgentID: ag.ID,
		tool:           strings.TrimSpace(q.Get("tool")), clientVersion: strings.TrimSpace(q.Get("version")),
		osName:      strings.TrimSpace(q.Get("os")),
		timingStart: timingStart, timingLast: time.Now(),
	}
	if ag.Origin == identity.OriginSelfhost {
		if store, ok := h.Identity.Store.(identity.ConnectionStore); ok {
			ip, at := h.agentSourceIP(r), time.Now().UTC()
			adm.onRegistered = func() {
				if err := store.RecordAgentConnection(context.Background(), ag.ID, ip, at); err != nil {
					log.Printf("agent source persistence failed agent=%s: %v", ag.ID, err)
				}
			}
		}
	}
	if ag.Origin == identity.OriginHosted && h.OnHostedAgentOnline != nil {
		agentID, cb := ag.ID, h.OnHostedAgentOnline
		adm.onRegistered = func() { cb(context.Background(), agentID) }
	}
	// Optional control stream capability: old Hubs do not consume this subtype.
	if ag.Origin == identity.OriginSelfhost {
		w.Header().Set("X-DW-Agent-Metrics", "agent-disk/v1")
		w.Header().Set("X-DW-Inbox-Progress", "inbox-progress/v1")
	}
	h.serveSession(w, r, adm)
}

// DisconnectAgent 吊销或暂停后由 identity.Service.OnRevoked／OnSuspended 调用：在线则以 4401 断开（V2 A1；V8 停用）。
// 关闭帧在吊销请求返回之前发出；但不陪对端走完关闭握手（组长 lead-m8-flake-findings ②）：
// 对端不读时库会等满 5 秒，这里最多等 agentDisconnectWait，余下的握手与拆连接在后台完成。
// 关闭帧发出后该连接上的一切写都失败，所以提前返回不会再有消息投给它。
func (h *Hub) DisconnectAgent(agentID string) {
	if h.Identity == nil {
		return
	}
	ag, err := h.Identity.Store.GetAgent(context.Background(), agentID)
	if err != nil {
		return
	}
	// 关闭码同为 4401，原因按当前状态区分：吊销 ⇒ 客户端删凭据；暂停 ⇒ 不删，等启用。
	reason := "credential_revoked"
	if ag.State == identity.AgentSuspended {
		reason = "credential_suspended"
	}
	h.disconnectAgentSession(AgentKeyID(ag.OwnerLC), ag.NameLC(), reason)
}

// DisconnectAgentRotated 凭据轮换（新凭据被取走、旧凭据随之作废）后由 identity.Service.OnCredentialIssued 调用：
// 在线则以 4401＋credential_suspended 断开旧连接——客户端只退出、不删本地凭据。
// 🔴 不能用 DisconnectAgent：它对 active 的 Agent 给 credential_revoked，客户端会 revoke_local 删凭据文件；
// 托管升级时新旧容器共用同一个 /work（状态目录同一路径），旧 helper 删掉的正是新 helper 刚写下的新凭据，
// 新容器下次重启即读不到凭据、exit 2（R-V2-SEATBATCH-W1-DOWN-CAUSE）。
// 真吊销（OnRevoked）与停用（OnSuspended）仍走 DisconnectAgent，语义不变。
func (h *Hub) DisconnectAgentRotated(agentID string) {
	if h.Identity == nil {
		return
	}
	ag, err := h.Identity.Store.GetAgent(context.Background(), agentID)
	if err != nil {
		return
	}
	h.disconnectAgentSession(AgentKeyID(ag.OwnerLC), ag.NameLC(), "credential_suspended")
}

// disconnectAgentSession 以 4401＋reason 断开一个在线 Agent 会话（不在线即空操作）；最多等 agentDisconnectWait。
// 用户停用（A11）也走这里，reason 用 credential_suspended：停用可恢复，客户端不得删凭据。
func (h *Hub) disconnectAgentSession(keyID, sessionName, reason string) {
	h.mu.Lock()
	c := h.sessionClients[keyID][sessionName]
	h.mu.Unlock()
	if c == nil || c.conn == nil {
		return
	}
	done := make(chan struct{})
	h.backgroundWork("owned", func() {
		func() {
			defer close(done)
			c.obsConn.SetLocalReason("revoked")
			_ = c.conn.Close(StatusCredentialRevoked, reason)
		}()
	})
	t := time.NewTimer(agentDisconnectWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}

// AgentOnline 供 console 的 Agent 列表显示在线状态。
func (h *Hub) AgentOnline(agentID string) bool {
	if h.Identity == nil {
		return false
	}
	ag, err := h.Identity.Store.GetAgent(context.Background(), agentID)
	if err != nil || !ag.Current() {
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

// R31 source IPs are private identity metadata, never session-directory fields.
func (h *Hub) agentSourceIP(r *http.Request) string {
	peer := net.ParseIP(hostOnly(r.RemoteAddr))
	if peer == nil {
		return ""
	}
	if h.TrustProxy && peer.IsLoopback() && len(r.Header.Values("X-Real-IP")) == 1 {
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}
	return peer.String()
}
