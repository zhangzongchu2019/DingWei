// Package httpapi 把 identity.Service 暴露为 HTTP 接口（设计件第二、三节）。
//
// 客户端接口无 console 会话；console 接口的「当前用户」由 Actor 注入（N3 会话接上前可接旧会话或测试桩）。
// 错误正文一律 {"error": "<机器码>", ...}，客户端只认机器码。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// API 路由与依赖。
type API struct {
	Svc       *identity.Service
	Claims    *identity.Onboarding
	ClaimBots func(context.Context) ([]ClaimBot, error)
	// Actor 取当前 console 用户的 user_id_lc。未登录返回非 nil 错误；必须改密的会话返回 identity.ErrMustChange
	// （组长裁定 140013Z ②：必须改密访问任何 /api/console/* 一律 403 must_change_password，与 N3 自己的接口同口径）。
	Actor func(r *http.Request) (userIDLC string, err error)
	// PublicOrigin console 写接口要求的 Origin（例如 https://dw.cmjrt.com:55055）。为空则拒绝一切 console 写请求。
	PublicOrigin string
	// TrustProxy 为真且直连方是回环地址时，来源 IP 取 X-Real-IP（Hub 在本机 nginx 之后）。
	TrustProxy bool
	// Online 报告某 Agent 当前是否在线；为空时一律 false。
	Online func(agentID string) bool
	// Hosted 由 G4 注入：给 origin=hosted 的 Agent 提供 hosted 子对象，原样放进列表项。子对象有哪些字段以 G4 契约为准，本包不解析、不改名
	// （例如 token_file 类账号不出 account_last4、改出账号类别与登记时间 —— 组长认可 kimi30kqtuxerh 的提议，字段名由 G4 定）。
	// 返回 false 表示暂无数据，此时不出 hosted 子对象。顶层 state（身份：active／suspended）仍由 G3 给、是「是否停用」的权威来源。
	Hosted func(ctx context.Context, agentID string) (map[string]any, bool)
	// HostedAccess gates hosted-only projections; the ordinary Agent list remains available.
	HostedAccess func(*http.Request) bool
}

const maxBody = 4 << 10

// Mux 挂路由所需的最小接口（*http.ServeMux 即满足）；cmd 的路由全扫描用例借它记下真实注册了哪些路由。
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Register 挂到 mux（Go 1.22 方法＋路径写法，与 cmd/workpulse/main.go 一致）。
func (a *API) Register(mux Mux) {
	mux.HandleFunc("POST /api/enroll/claims", a.claimApply)
	mux.HandleFunc("GET /api/enroll/claims/{id}", a.claimPoll)
	mux.HandleFunc("POST /api/enroll/claims/{id}/ack", a.claimAck)
	mux.HandleFunc("POST /api/enroll/claims/{id}/cancel", a.claimCancel)
	mux.HandleFunc("POST /api/enroll/applications", a.apply)
	mux.HandleFunc("GET /api/enroll/applications/{id}", a.poll)
	mux.HandleFunc("GET /api/console/enroll/applications", a.console(a.listPending))
	mux.HandleFunc("POST /api/console/enroll/applications/{id}/approve", a.console(a.approve))
	mux.HandleFunc("POST /api/console/enroll/applications/{id}/reject", a.console(a.reject))
	mux.HandleFunc("GET /api/console/agents", a.console(a.listAgents))
	mux.HandleFunc("POST /api/console/agents/{id}/revoke", a.console(a.revoke))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string, extra map[string]any) {
	body := map[string]any{"error": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", nil)
		return false
	}
	return true
}

func (a *API) sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a.TrustProxy {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(real) != nil {
				return real
			}
		}
	}
	return host
}

// mapErr 业务错误 → 状态码与机器码（设计件 2.1、2.2、3.1、3.2）。
func mapErr(w http.ResponseWriter, err error) {
	var rl *identity.RateLimitedError
	var pm *identity.PairingMismatchError
	switch {
	case errors.As(err, &rl):
		secs := int(rl.RetryAfter.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeErr(w, http.StatusTooManyRequests, "rate_limited", map[string]any{"retry_after_s": secs})
	case errors.As(err, &pm):
		writeErr(w, http.StatusUnprocessableEntity, "pairing_mismatch", map[string]any{"remaining": pm.Remaining, "rejected": pm.Rejected})
	case errors.Is(err, identity.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict", nil)
	case errors.Is(err, identity.ErrNotAllowed):
		writeErr(w, http.StatusForbidden, "not_allowed", nil)
	case errors.Is(err, identity.ErrBadRequest):
		writeErr(w, http.StatusBadRequest, "bad_request", nil)
	case errors.Is(err, identity.ErrUnknownOwner):
		writeErr(w, http.StatusNotFound, "unknown_owner", nil)
	case errors.Is(err, identity.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, identity.ErrGroupNotFound):
		writeErr(w, http.StatusUnprocessableEntity, "group_not_found", nil)
	case errors.Is(err, identity.ErrGroupClosed):
		writeErr(w, http.StatusConflict, "group_closed", nil)
	case errors.Is(err, identity.ErrGroupNameInvalid):
		writeErr(w, http.StatusUnprocessableEntity, "group_name_invalid", nil)
	case errors.Is(err, identity.ErrGroupNameTaken):
		writeErr(w, http.StatusConflict, "group_name_taken", nil)
	case errors.Is(err, identity.ErrNameInvalid):
		writeErr(w, http.StatusUnprocessableEntity, "name_invalid", nil)
	case errors.Is(err, identity.ErrNameTaken):
		writeErr(w, http.StatusConflict, "name_taken", nil)
	case errors.Is(err, identity.ErrExpired):
		writeErr(w, http.StatusGone, "expired", nil)
	case errors.Is(err, identity.ErrNotPending):
		writeErr(w, http.StatusGone, "not_pending", nil)
	case errors.Is(err, identity.ErrForbidden):
		writeErr(w, http.StatusForbidden, "forbidden", nil)
	case errors.Is(err, identity.ErrHosted):
		writeErr(w, http.StatusConflict, "hosted_agent", nil)
	default:
		writeErr(w, http.StatusInternalServerError, "internal", nil)
	}
}

// ── 客户端接口 ──────────────────────────────────────────────────────────────

func (a *API) apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OwnerUserID       string `json:"owner_user_id"`
		PairingSalt       string `json:"pairing_salt"`
		PairingDigest     string `json:"pairing_digest"`
		DeviceFingerprint string `json:"device_fingerprint"`
		ClientVersion     string `json:"client_version"`
	}
	if !decode(w, r, &req) {
		return
	}
	res, err := a.Svc.Apply(r.Context(), identity.ApplyRequest{
		OwnerUserID: req.OwnerUserID, PairingSalt: req.PairingSalt, PairingDigest: req.PairingDigest,
		DeviceFingerprint: req.DeviceFingerprint, ClientVersion: req.ClientVersion, SourceIP: a.sourceIP(r),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"application_id": res.ApplicationID,
		"poll_token":     res.PollToken,
		"expires_at":     res.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (a *API) poll(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	if token == "" {
		writeErr(w, http.StatusNotFound, "not_found", nil) // 与令牌不符同形，不泄露申请是否存在
		return
	}
	res, err := a.Svc.Poll(r.Context(), r.PathValue("id"), token)
	if err != nil {
		mapErr(w, err)
		return
	}
	body := map[string]any{"state": string(res.State)}
	if res.State == identity.AppApproved {
		body["agent_name"] = res.AgentName
		body["agent_full_name"] = res.AgentFullName
		body["credential"] = res.Credential
	}
	writeJSON(w, http.StatusOK, body)
}

// ── console 接口 ────────────────────────────────────────────────────────────

func (a *API) console(next func(w http.ResponseWriter, r *http.Request, actor string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, err := "", error(identity.ErrNotFound)
		if a.Actor != nil {
			actor, err = a.Actor(r)
		}
		if errors.Is(err, identity.ErrMustChange) {
			writeErr(w, http.StatusForbidden, "must_change_password", nil)
			return
		}
		if err != nil || actor == "" {
			writeErr(w, http.StatusUnauthorized, "unauthenticated", nil)
			return
		}
		if r.Method != http.MethodGet && (a.PublicOrigin == "" || r.Header.Get("Origin") != a.PublicOrigin) {
			writeErr(w, http.StatusForbidden, "origin_mismatch", nil)
			return
		}
		next(w, r, identity.Lower(actor))
	}
}

func (a *API) listPending(w http.ResponseWriter, r *http.Request, actor string) {
	apps, err := a.Svc.ListPending(r.Context(), actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	perDevice := map[string]int{}
	for _, ap := range apps {
		perDevice[ap.DeviceFingerprint]++
	}
	out := make([]map[string]any, 0, len(apps))
	for _, ap := range apps {
		fp := ap.DeviceFingerprint
		if len(fp) > 12 {
			fp = fp[:12]
		}
		out = append(out, map[string]any{
			"application_id":     ap.ID,
			"source_ip":          ap.SourceIP,
			"device_fingerprint": fp,
			"client_version":     ap.ClientVersion,
			"requested_at":       ap.RequestedAt.UTC().Format(time.RFC3339),
			"same_device_count":  perDevice[ap.DeviceFingerprint],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"applications": out})
}

func (a *API) approve(w http.ResponseWriter, r *http.Request, actor string) {
	// 编组二选一：group_code（已有组，须存在且未关闭添加）或 new_group_name（就地新建、新 Agent 当组长）。
	var req struct {
		PairingCode  string `json:"pairing_code"`
		AgentName    string `json:"agent_name"`
		GroupCode    string `json:"group_code"`
		NewGroupName string `json:"new_group_name"`
	}
	if !decode(w, r, &req) {
		return
	}
	// 两个都不给 ⇒ 不入组（业主令 15:34:17Z：编组不是必须的）。给了 new_group_name 却只有空白 ⇒ 名字不合规，不当作「没给」。
	if req.GroupCode != "" && req.NewGroupName != "" {
		writeErr(w, http.StatusUnprocessableEntity, "group_ambiguous", nil)
		return
	}
	approve := a.Svc.Approve
	group := req.GroupCode
	if req.NewGroupName != "" {
		name := identity.NormalizeGroupName(req.NewGroupName) // 去不可见字符与首尾空白（R-V2-0102-GROUP-FIXES B）
		if name == "" {
			writeErr(w, http.StatusUnprocessableEntity, "group_name_invalid", nil)
			return
		}
		approve, group = a.Svc.ApproveWithNewGroup, name
	}
	ag, full, err := approve(r.Context(), actor, r.PathValue("id"), req.PairingCode, req.AgentName, group)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": ag.ID, "agent_full_name": full})
}

func (a *API) reject(w http.ResponseWriter, r *http.Request, actor string) {
	if err := a.Svc.Reject(r.Context(), actor, r.PathValue("id")); err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": string(identity.AppRejected)})
}

func (a *API) listAgents(w http.ResponseWriter, r *http.Request, actor string) {
	agents, err := a.Svc.ListAgents(r.Context(), actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	u, err := a.Svc.Store.GetUser(r.Context(), actor)
	short := ""
	if err == nil {
		short = u.ShortName
	}
	out := make([]map[string]any, 0, len(agents))
	for _, ag := range agents {
		online := false
		if a.Online != nil {
			online = a.Online(ag.ID)
		}
		// session：兼容旧形状 {session, online}（console 左栏仍读它）——值即该 Agent 在 Hub 会话机制里的会话名（名称小写）。
		item := map[string]any{
			"session":  ag.NameLC(),
			"agent_id": ag.ID, "name": ag.Name, "full_name": ag.Name + "." + short,
			"origin": string(ag.Origin), "group_code": ag.GroupID, "state": string(ag.State), "online": online,
		}
		item["agent_type"] = string(ag.Origin)
		if ag.Origin == identity.OriginSelfhost && len(r.Header.Values("Authorization")) == 0 {
			if source, ok := a.Svc.Store.(identity.ConnectionStore); ok {
				if conn, e := source.ReadAgentConnection(r.Context(), actor, ag.ID); e == nil && conn.IP != "" {
					item["host"] = conn.IP
					item["last_source_at"] = conn.At.UTC().Format(time.RFC3339Nano)
				}
			}
		}
		if ag.Origin == identity.OriginHosted && a.Hosted != nil && a.HostedAccess != nil && a.HostedAccess(r) {
			if h, ok := a.Hosted(r.Context(), ag.ID); ok {
				item["hosted"] = h
				item["host"] = h["node_name"]
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (a *API) revoke(w http.ResponseWriter, r *http.Request, actor string) {
	already, err := a.Svc.Revoke(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	if already {
		writeJSON(w, http.StatusOK, map[string]any{"already_revoked": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked_at": a.Svc.Now().UTC().Format(time.RFC3339)})
}
