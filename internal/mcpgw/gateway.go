// Package mcpgw 提供 r53 外部席位的无状态 MCP、同权 REST 和 OAuth 边界。
package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
)

const ProtocolVersion = "2026-07-28"
const metaPrefix = "io.modelcontextprotocol/"
const toolsListTTLMS = 300000
const toolsListCacheScope = "private"
const serverInfoName = "dingwei-external"
const serverInfoVersion = dwversion.Version

func serverInfoMeta() map[string]any {
	return map[string]any{metaPrefix + "serverInfo": map[string]string{"name": serverInfoName, "version": serverInfoVersion}}
}

func toolCallResult(data any, isError bool) map[string]any {
	b, _ := json.Marshal(data)
	return map[string]any{
		"resultType":        "complete",
		"content":           []any{map[string]any{"type": "text", "text": string(b)}},
		"structuredContent": data,
		"isError":           isError,
		"_meta":             serverInfoMeta(),
	}
}

// Seats 不含签发、执行、文件或管理能力。每个通讯请求独立 Authenticate。
type Seats interface {
	Authenticate(context.Context, string) (extseat.Seat, error)
	ListOnline(context.Context, extseat.Seat) ([]extseat.OnlineAgent, error)
	Send(context.Context, extseat.Seat, string, string, string, ...bool) (extseat.SendReceipt, error)
	Wait(context.Context, extseat.Seat, time.Duration) ([]extseat.Delivery, error)
	Ack(context.Context, extseat.Seat, string, int) error
}

// Origins 集成时接 r52 的受信来源判定；缺省只认可 PublicOrigin。
type Origins interface {
	TrustedOrigin(context.Context, string) bool
}
type Login interface {
	ServeConnectPage(http.ResponseWriter, *http.Request)
	Callback(context.Context, feishulogin.CallbackRequest) (feishulogin.CallbackResult, error)
}

type Config struct {
	PublicOrigin string
	Resource     string
	Logger       *slog.Logger
	Seats        Seats
	Origins      Origins
	Login        Login
	Clients      ClientResolver
	Store        Store
	SendReplyMax time.Duration
	Now          func() time.Time
}
type Gateway struct{ cfg Config }

func New(cfg Config) (*Gateway, error) {
	if cfg.SendReplyMax == 0 {
		cfg.SendReplyMax = extshared.SendReplyMax
	}
	if cfg.SendReplyMax <= 0 || cfg.SendReplyMax > extshared.SendReplyMax {
		return nil, errors.New("invalid send reply maximum")
	}

	u, e := url.Parse(cfg.PublicOrigin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return nil, errors.New("mcpgw: invalid public origin")
	}
	r, e := url.Parse(cfg.Resource)
	if e != nil || r.Scheme != "https" || r.Host != u.Host || r.User != nil || r.RawQuery != "" || r.Fragment != "" || r.Path != "/mcp" {
		return nil, errors.New("mcpgw: resource must be public origin /mcp")
	}
	if cfg.Seats == nil || cfg.Login == nil || cfg.Clients == nil || cfg.Store == nil {
		return nil, errors.New("mcpgw: incomplete dependencies")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Gateway{cfg: cfg}, nil
}

// Register 只登记新路径，不替换 helper 或 Console 的现有处理器。
func (g *Gateway) Register(mux *http.ServeMux) {
	mux.HandleFunc("/mcp", g.guard(g.mcp))
	mux.HandleFunc(codexClientPath, g.guard(g.codexClientMetadata))
	mux.HandleFunc("/api/external/", g.guard(g.rest))
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", g.guard(g.resourceMetadata))
	mux.HandleFunc("/.well-known/oauth-authorization-server", g.guard(g.serverMetadata))
	mux.HandleFunc("/oauth/authorize", g.guard(g.authorize))
	mux.HandleFunc("/oauth/token", g.guard(g.token))
	mux.HandleFunc("/feishu/connect", g.guard(g.connect))
	mux.HandleFunc("/mcp/oauth/feishu/callback", g.guard(g.callback))
}
func (g *Gateway) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// same-origin keeps referrers off cross-site hops (Feishu) while letting
		// same-origin form POSTs carry a real Origin for the guard below.
		w.Header().Set("Referrer-Policy", "same-origin")
		origins := r.Header.Values("Origin")
		if len(origins) > 1 || (len(origins) == 1 && (origins[0] == "" || !g.trusted(r.Context(), origins[0]))) {
			if r.URL.Path == "/mcp/oauth/feishu/callback" {
				g.failCallback(w, r, http.StatusForbidden, "origin_rejected", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)
				return
			}
			writeJSON(w, 403, map[string]string{"error": "origin_rejected"})
			return
		}
		next(w, r)
	}
}
func (g *Gateway) trusted(ctx context.Context, origin string) bool {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if g.cfg.Origins != nil {
		return g.cfg.Origins.TrustedOrigin(ctx, origin)
	}
	return origin == g.cfg.PublicOrigin
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func method(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method == want {
		return true
	}
	w.Header().Set("Allow", want)
	writeJSON(w, 405, map[string]string{"error": "method_not_allowed"})
	return false
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, extshared.MessageJSONMaxBytes)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("extra JSON")
	}
	return nil
}
func (g *Gateway) authenticate(r *http.Request) (extseat.Seat, error) {
	h := r.Header.Values("Authorization")
	if len(h) != 1 {
		return extseat.Seat{}, extshared.ErrUnauthenticated
	}
	p := strings.Fields(h[0])
	if len(p) != 2 || !strings.EqualFold(p[0], "Bearer") {
		return extseat.Seat{}, extshared.ErrUnauthenticated
	}
	return g.cfg.Seats.Authenticate(r.Context(), p[1])
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

var errParams = errors.New("invalid parameters")
var errMethod = errors.New("unknown method")

// errorRules 是 extshared 错误的唯一对外映射；绝不回传底层错误正文。
var errorRules = []struct {
	err          error
	status, code int
	name         string
}{
	{extshared.ErrUnauthenticated, 401, 1001, "unauthenticated"},
	{extshared.ErrTokenRevoked, 401, 1001, "token_revoked"},
	{extshared.ErrTokenExpired, 401, 1001, "token_expired"},
	{extshared.ErrTenantRejected, 403, 1003, "tenant_rejected"},
	{extshared.ErrGroupDissolved, 403, 1003, "group_dissolved"},
	{extshared.ErrTargetNotInAG, 403, 1003, "target_not_in_ag"},
	{extshared.ErrNotPermitted, 403, 1003, "not_permitted"},
	{extshared.ErrIdempotencyKey, 409, 1009, "idempotency_conflict"},
	{extshared.ErrQueueFull, 429, 1029, "queue_full"},
	{errParams, 400, -32602, "invalid_params"},
	{errMethod, 404, -32601, "method_not_found"},
	{context.Canceled, 408, 1008, "request_cancelled"},
	{context.DeadlineExceeded, 408, 1008, "request_timeout"},
}

func mapped(e error) (int, wireError) {
	var large *extshared.BodyTooLargeError
	if errors.As(e, &large) {
		return 413, wireError{Code: 1013, Message: "body_too_large", Data: large}
	}
	for _, r := range errorRules {
		if errors.Is(e, r.err) {
			return r.status, wireError{Code: r.code, Message: r.name}
		}
	}
	return 500, wireError{Code: -32603, Message: "internal_error"}
}
func (g *Gateway) challenge(w http.ResponseWriter, status int) {
	if status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+g.cfg.PublicOrigin+`/.well-known/oauth-protected-resource/mcp", scope="`+extshared.ScopeString()+`"`)
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Meta      map[string]json.RawMessage `json:"_meta"`
		Name      string                     `json:"name,omitempty"`
		Arguments json.RawMessage            `json:"arguments,omitempty"`
	} `json:"params"`
}

func rpcError(w http.ResponseWriter, status int, id json.RawMessage, e wireError) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, status, map[string]any{"jsonrpc": "2.0", "id": id, "error": e})
}
func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	var text string
	if len(id) > 0 && id[0] == '"' && json.Unmarshal(id, &text) == nil {
		return true
	}
	_, e := strconv.ParseInt(string(id), 10, 64)
	return e == nil
}

func singleHeader(r *http.Request, name string) string {
	v := r.Header.Values(name)
	if len(v) != 1 {
		return ""
	}
	return v[0]
}
func nameHeader(s string) string {
	if strings.HasPrefix(s, "=?base64?") && strings.HasSuffix(s, "?=") {
		b, e := base64.StdEncoding.DecodeString(s[9 : len(s)-2])
		if e != nil {
			return ""
		}
		return string(b)
	}
	return s
}
func (g *Gateway) mcp(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		rpcError(w, 415, nil, wireError{Code: -32600, Message: "application/json required"})
		return
	}
	if !accepts(r.Header.Get("Accept"), "application/json") || !accepts(r.Header.Get("Accept"), "text/event-stream") {
		rpcError(w, 406, nil, wireError{Code: -32600, Message: "Accept must include JSON and SSE"})
		return
	}
	if g.route2025(w, r) {
		return
	}
	var q rpcRequest
	if decode(w, r, &q) != nil {
		rpcError(w, 400, nil, wireError{Code: -32700, Message: "invalid JSON request"})
		return
	}
	if q.JSONRPC != "2.0" || !validID(q.ID) || q.Method == "" {
		rpcError(w, 400, nil, wireError{Code: -32600, Message: "invalid request"})
		return
	}
	var version string
	_ = json.Unmarshal(q.Params.Meta[metaPrefix+"protocolVersion"], &version)
	if version == "" || singleHeader(r, "MCP-Protocol-Version") != version || singleHeader(r, "Mcp-Method") != q.Method || (q.Method == "tools/call" && (q.Params.Name == "" || nameHeader(singleHeader(r, "Mcp-Name")) != q.Params.Name)) {
		rpcError(w, 400, q.ID, wireError{Code: -32020, Message: "HeaderMismatch"})
		return
	}
	if version != ProtocolVersion {
		rpcError(w, 400, q.ID, wireError{Code: -32022, Message: "Unsupported protocol version", Data: map[string]any{"supported": []string{ProtocolVersion}, "requested": version}})
		return
	}
	var capabilities map[string]json.RawMessage
	if json.Unmarshal(q.Params.Meta[metaPrefix+"clientCapabilities"], &capabilities) != nil || capabilities == nil {
		rpcError(w, 400, q.ID, wireError{Code: -32602, Message: "clientCapabilities object required"})
		return
	}
	// MVP 不使用任何客户端扩展；声明的已知能力仍须为对象，不能沿用上一请求。
	for _, raw := range capabilities {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			rpcError(w, 400, q.ID, wireError{Code: -32602, Message: "invalid client capability"})
			return
		}
	}
	seat, e := g.authenticate(r)
	if e != nil {
		st, we := mapped(e)
		g.challenge(w, st)
		rpcError(w, st, q.ID, we)
		return
	}
	var result any
	switch q.Method {
	case "server/discover":
		result = map[string]any{"resultType": "complete", "supportedVersions": []string{ProtocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}}, "_meta": serverInfoMeta()}
	case "tools/list":
		result = map[string]any{"resultType": "complete", "tools": toolDefinitions(g.cfg.SendReplyMax), "ttlMs": toolsListTTLMS, "cacheScope": toolsListCacheScope, "_meta": serverInfoMeta()}
	case "tools/call":
		var data any
		data, e = g.call(r.Context(), seat, q.Params.Name, q.Params.Arguments)
		if e == nil {
			result = toolCallResult(data, false)
		}
	default:
		e = errMethod
	}
	if e != nil {
		st, we := mapped(e)
		g.challenge(w, st)
		rpcError(w, st, q.ID, we)
		return
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
}
func accepts(header, want string) bool {
	for _, s := range strings.Split(header, ",") {
		t, p, e := mime.ParseMediaType(strings.TrimSpace(s))
		quality := 1.0
		if raw, ok := p["q"]; ok {
			q, err := strconv.ParseFloat(raw, 64)
			if err != nil || q <= 0 || q > 1 {
				continue
			}
			quality = q
		}
		if e == nil && t == want && quality > 0 {
			return true
		}
	}
	return false
}
func (g *Gateway) rest(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/external/")
	if !knownTool(name) {
		st, we := mapped(errMethod)
		writeJSON(w, st, map[string]any{"error": we})
		return
	}
	seat, e := g.authenticate(r)
	if e != nil {
		st, we := mapped(e)
		g.challenge(w, st)
		writeJSON(w, st, map[string]any{"error": we})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, extshared.MessageJSONMaxBytes)
	raw, e := io.ReadAll(r.Body)
	if e == nil {
		var out any
		out, e = g.call(r.Context(), seat, name, raw)
		if e == nil {
			writeJSON(w, 200, out)
			return
		}
	}
	if e != nil && !errors.Is(e, errParams) {
		var me *http.MaxBytesError
		if errors.As(e, &me) {
			e = errParams
		}
	}
	st, we := mapped(e)
	g.challenge(w, st)
	writeJSON(w, st, map[string]any{"error": we})
}
