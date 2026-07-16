package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	terminalOutputType       = "terminal_output"
	terminalInputType        = "terminal_input"
	terminalInterruptType    = "terminal_interrupt"
	terminalResizeType       = "terminal_resize"
	terminalRefreshType      = "terminal_refresh"
	terminalSessionEventType = "session_event"
	terminalTokenTTL         = 8 * time.Hour
	terminalBufferCap        = 1 << 20
	terminalEventRingCap     = 1000
	terminalDisconnectGrace  = 5 * time.Minute // 网页断连后保留 viewer+码 的宽限期(修解锁码失效)
)

type terminalState struct {
	keyID       string
	sessionName string
	buffer      string
	viewers     map[string]*terminalViewer
	writerPage  string
	writerToken string
	writerUntil time.Time
	cookieToken string
	cookieUntil time.Time
	eventRing   []string               // 结构化会话事件(session_event)环形缓冲,供晚订阅回放
	eventSubs   map[string]chan string // 内网事件订阅者(View Service),subID → channel
}

type terminalViewer struct {
	id             string
	code           string
	conn           *websocket.Conn // nil ⇒ 无活连接(View Service 虚拟 viewer;或网页断连宽限中)
	mu             chan struct{}
	lastSeen       time.Time // 仅虚拟 viewer 用:authorize 轮询/输入时刷新,供清理陈旧代理
	pageID         string    // 网页 localStorage 稳定页标识:重连据此认回原 viewer(码/写权不丢)
	disconnectedAt time.Time // 网页断连时刻;非零=宽限保留中,超 terminalDisconnectGrace 才清
}

type terminalClientMessage struct {
	Type  string `json:"type"`
	Data  string `json:"data,omitempty"`
	Token string `json:"token,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Cols  int    `json:"cols,omitempty"`
}

type terminalCookieRequest struct {
	Token string `json:"token"`
}

type viewInterruptRequest struct {
	ViewerID   string `json:"viewer_id"`
	Token      string `json:"token"`
	DeliveryID string `json:"delivery_id"`
	TurnID     string `json:"turn_id"`
}

func terminalKey(keyID, sessionName string) string {
	return keyID + "\x00" + sessionName
}

func (h *Hub) terminalForSession(keyID, sessionName string) *terminalState {
	k := terminalKey(keyID, sessionName)
	st := h.terminals[k]
	if st == nil {
		st = &terminalState{keyID: keyID, sessionName: sessionName, viewers: map[string]*terminalViewer{}}
		h.terminals[k] = st
	}
	return st
}

func (h *Hub) terminalBySessionNameLocked(sessionName string) (*terminalState, bool) {
	var found *terminalState
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] == nil {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = h.terminalForSession(keyID, sessionName)
	}
	return found, found != nil
}

func (h *Hub) closeTerminalLocked(keyID, sessionName string) []*terminalViewer {
	k := terminalKey(keyID, sessionName)
	st := h.terminals[k]
	if st == nil {
		return nil
	}
	viewers := make([]*terminalViewer, 0, len(st.viewers))
	for _, v := range st.viewers {
		viewers = append(viewers, v)
	}
	delete(h.terminals, k)
	return viewers
}

func (h *Hub) HandleTerminalViewPage(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	// C4:按三级路由策略选目标。命中 View 服务 ⇒ 反代;否则(含任何异常)走老 xterm。
	name, tgt := loadViewRouting().resolve(sessionName, ownerFromSessionName(sessionName))
	if !tgt.Builtin && tgt.URL != "" {
		if h.proxyToViewService(w, r, sessionName, tgt.URL) {
			return
		}
		log.Printf("[view-routing] target %s (%s) unreachable, fallback to legacy for %s", name, tgt.URL, sessionName)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, terminalPageHTML, html.EscapeString(sessionName), html.EscapeString(sessionName))
}

// HandleViewSubPath 处理 /view/<名>/{events,input,status,...} 子路径。
// 新 view 的页面(SSE/输入/状态)不在 hub 根路径,而是挂在会话自己的 /view/<名>/ 下,
// 命中 View 服务就反代过去(剥前缀 + 带 X-DW-Session);未路由到 View 服务的会话没有这些
// 子路径(老 xterm 走 /ws/view),故 404。
func (h *Hub) HandleViewSubPath(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	name, tgt := loadViewRouting().resolve(sessionName, ownerFromSessionName(sessionName))
	if !tgt.Builtin && tgt.URL != "" {
		if h.proxyToViewService(w, r, sessionName, tgt.URL) {
			return
		}
		log.Printf("[view-routing] subpath target %s (%s) unreachable for %s", name, tgt.URL, sessionName)
	}
	http.NotFound(w, r)
}

func (h *Hub) HandleTerminalViewCookie(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	var req terminalCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}
	if !h.validTerminalCookieToken(sessionName, token) {
		http.Error(w, "unlock token expired", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     terminalViewCookieName(sessionName),
		Value:    token,
		Path:     "/",
		MaxAge:   int(terminalTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   terminalCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (h *Hub) HandleTerminalViewWS(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	pageID := strings.TrimSpace(r.URL.Query().Get("page"))
	mu := make(chan struct{}, 1)
	mu <- struct{}{}
	var viewer *terminalViewer
	var st *terminalState
	h.mu.Lock()
	var ok bool
	st, ok = h.terminalBySessionNameLocked(sessionName)
	// Capture values under h.mu to avoid racing with grantTerminalInput
	// (which writes st.writerPage) and other terminal state mutators.
	var isWriter bool
	var hasBuffer bool
	var buf string
	var needsRefresh bool = true
	var writerToken string
	if ok {
		h.sweepDisconnectedViewersLocked(st)
		// 认回:同会话、同 pageID、断连宽限中的 viewer ⇒ 继承其 id/code(码与写权不丢)。
		var reclaim *terminalViewer
		if pageID != "" {
			for _, v := range st.viewers {
				if v.conn == nil && v.pageID == pageID && !v.disconnectedAt.IsZero() {
					reclaim = v
					break
				}
			}
		}
		if reclaim != nil {
			// 新对象继承旧 id/code,替换 map 里旧对象(旧连接的 defer 据 v!=viewer 不误删)
			viewer = &terminalViewer{id: reclaim.id, code: reclaim.code, conn: conn, mu: mu, pageID: pageID}
		} else {
			viewer = &terminalViewer{id: randomHex(8), code: h.uniqueTerminalCodeLocked(), conn: conn, mu: mu, pageID: pageID}
		}
		st.viewers[viewer.id] = viewer
		isWriter = st.writerPage == viewer.id
		if isWriter {
			writerToken = st.writerToken
		}
		hasBuffer = st.buffer != ""
		buf = st.buffer
		needsRefresh = st.buffer == ""
	}
	h.mu.Unlock()
	if !ok || st == nil {
		_ = terminalWrite(r.Context(), &terminalViewer{conn: conn, mu: mu}, map[string]any{"type": "status", "readonly": true, "message": "会话不在线或会话名不唯一"})
		_ = conn.Close(websocket.StatusNormalClosure, "offline")
		return
	}
	defer func() {
		h.mu.Lock()
		if cur := h.terminals[terminalKey(st.keyID, st.sessionName)]; cur != nil {
			if v := cur.viewers[viewer.id]; v == viewer { // 仍是本连接(未被重连认领替换)
				if viewer.pageID != "" {
					// 有 pageID:断连保留宽限——码在窗口内仍可 #unlock,重连据 pageID 认回写权
					v.conn = nil
					v.disconnectedAt = time.Now()
				} else {
					delete(cur.viewers, viewer.id)
					if cur.writerPage == viewer.id {
						cur.writerPage = ""
						cur.writerUntil = time.Time{}
					}
				}
			}
		}
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}()
	if hasBuffer {
		_ = terminalWrite(r.Context(), viewer, map[string]any{"type": "output", "data": buf})
	}
	_ = terminalWrite(r.Context(), viewer, map[string]any{
		"type":     "status",
		"readonly": !isWriter,
		"code":     viewer.code,
		"message":  "飞书发送 #unlock " + viewer.code + " 后可输入",
	})
	if isWriter && writerToken != "" {
		// 重连认回:该页在断连期间已被 #unlock 授权 ⇒ 直接补发 write_granted,页面立即可输入
		_ = terminalWrite(r.Context(), viewer, map[string]any{
			"type":      "write_granted",
			"readonly":  false,
			"token":     writerToken,
			"expiresIn": int(terminalTokenTTL.Seconds()),
			"message":   "输入已解锁",
		})
	}
	if token, ok := terminalViewCookie(r, sessionName); ok {
		h.restoreTerminalInputFromCookie(viewer, st.keyID, st.sessionName, token)
	}
	if needsRefresh {
		go h.requestTerminalRefreshAfterInitialResize(r.Context(), st.keyID, st.sessionName, viewer.id)
	}
	// 保活:每 30s 发一次 WS ping,防止 nginx/浏览器在空闲(无终端输出)时断连,并及时发现死连接
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				// 通过串行化的写路径(terminalWrite 用 viewer.mu)发心跳消息,
				// 不用 conn.Ping —— 否则会与输出转发的 conn.Write 并发写、coder/websocket 崩连接
				if err := terminalWrite(r.Context(), viewer, map[string]any{"type": "ping"}); err != nil {
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var msg terminalClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if err := h.routeTerminalInput(r.Context(), st.keyID, st.sessionName, viewer.id, msg.Token, msg.Data); err != nil {
				_ = terminalWrite(r.Context(), viewer, map[string]any{"type": "status", "readonly": true, "message": err.Error()})
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = h.routeTerminalResize(r.Context(), st.keyID, st.sessionName, msg.Cols, msg.Rows)
			}
		}
	}
}

func terminalWrite(ctx context.Context, v *terminalViewer, payload map[string]any) error {
	// 虚拟 viewer(View Service 的代理身份,conn==nil)不走 WS 写:
	// 它通过 /internal/view/authorize 轮询获知授权,不需要推送。
	if v == nil || v.conn == nil {
		return nil
	}
	data, _ := json.Marshal(payload)
	<-v.mu
	defer func() { v.mu <- struct{}{} }()
	return v.conn.Write(ctx, websocket.MessageText, data)
}

func (h *Hub) handleTerminalOutputEnvelope(c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != terminalOutputType {
		return false
	}
	data := env.Body
	clean := sanitizeTerminalSyncText(data)
	ts := time.Now().UTC()
	h.mu.Lock()
	st := h.terminalForSession(c.keyID, c.sessionName)
	st.buffer += data
	if len(st.buffer) > terminalBufferCap {
		st.buffer = st.buffer[len(st.buffer)-terminalBufferCap:]
	}
	viewers := make([]*terminalViewer, 0, len(st.viewers))
	for _, v := range st.viewers {
		viewers = append(viewers, v)
	}
	var syncTargets []feishuSyncTarget
	if clean != "" {
		syncTargets = h.appendTerminalSyncItemLocked(c.keyID, c.sessionName, clean, ts)
	}
	h.mu.Unlock()
	for _, v := range viewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "output", "data": data})
	}
	item := terminalSyncItem{TS: ts, Text: clean}
	for _, target := range syncTargets {
		h.queueFeishuSyncItem(c.keyID, c.sessionName, target, item)
	}
	return true
}

// handleSessionEventEnvelope 接收 helper 发来的结构化会话事件(session_event):
// 存入 eventRing(供晚订阅的 View Service 回放),并扇出给内网订阅者。
// 刻意【不进 buffer、不碰老 xterm viewers】—— 新页走这条,老页照旧走 terminal_output。
func (h *Hub) handleSessionEventEnvelope(c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != terminalSessionEventType {
		return false
	}
	data := env.Body
	h.mu.Lock()
	st := h.terminalForSession(c.keyID, c.sessionName)
	st.eventRing = append(st.eventRing, data)
	if len(st.eventRing) > terminalEventRingCap {
		st.eventRing = st.eventRing[len(st.eventRing)-terminalEventRingCap:]
	}
	subs := make([]chan string, 0, len(st.eventSubs))
	for _, ch := range st.eventSubs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- data:
		default: // 慢消费者不阻塞事件主路(与 output 扇出同纪律)
		}
	}
	return true
}

// subscribeSessionEvents 注册一个内网事件订阅者(C2 的 SSE 端点使用):
// 返回当前 eventRing 回放快照 + 实时 channel + 注销函数。
func (h *Hub) subscribeSessionEvents(keyID, sessionName, subID string) (replay []string, ch chan string, unsub func()) {
	ch = make(chan string, 256)
	h.mu.Lock()
	st := h.terminalForSession(keyID, sessionName)
	if st.eventSubs == nil {
		st.eventSubs = map[string]chan string{}
	}
	st.eventSubs[subID] = ch
	replay = append([]string(nil), st.eventRing...)
	h.mu.Unlock()
	unsub = func() {
		h.mu.Lock()
		if cur := h.terminals[terminalKey(keyID, sessionName)]; cur != nil {
			delete(cur.eventSubs, subID)
		}
		h.mu.Unlock()
	}
	return replay, ch, unsub
}

// HandleInternalViewEvents(C2):内网 SSE 口,View Service 订阅某会话的结构化事件流。
// 先回放 eventRing,再挂实时流。**仅内网监听**(部署时绑 127.0.0.1,不上公网 mux)。
func (h *Hub) HandleInternalViewEvents(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("session"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// 解析 keyID:会话须在线且会话名唯一(与 view WS 同判据)
	h.mu.Lock()
	st, found := h.terminalBySessionNameLocked(sessionName)
	var keyID string
	if found {
		keyID = st.keyID
	}
	h.mu.Unlock()
	if !found {
		http.Error(w, "session offline or ambiguous", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	subID := randomHex(8)
	replay, ch, unsub := h.subscribeSessionEvents(keyID, sessionName, subID)
	defer unsub()

	writeSSE := func(data string) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, data := range replay {
		if !writeSSE(data) {
			return
		}
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(data) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ---- C4:入口路由器(按用户三级灰度路由到不同 View 服务 / 老 xterm)----

type ViewTarget struct {
	URL               string `json:"url,omitempty"`                   // View 服务地址(反代)
	Builtin           bool   `json:"builtin,omitempty"`               // true ⇒ hub 内置老 xterm 页
	ViewUnlockV2      bool   `json:"view_unlock_v2,omitempty"`        // 显式 capability gate
	HubToVSSecretFile string `json:"hub_to_vs_secret_file,omitempty"` // 每 target 独立密钥文件
	VSToHubSecretFile string `json:"vs_to_hub_secret_file,omitempty"` // 反向输入独立密钥文件
}

// ViewRouting 路由策略。解析顺序:会话级 > 用户级 > 默认(命中即停)。
type ViewRouting struct {
	Sessions map[string]string     `json:"sessions"` // 会话全名 → target 名
	Users    map[string]string     `json:"users"`    // owner → target 名
	Default  string                `json:"default"`  // 兜底 target 名
	Targets  map[string]ViewTarget `json:"targets"`
}

var (
	viewRoutingMu    sync.Mutex
	viewRoutingCache *ViewRouting
	viewRoutingMTime time.Time
)

func viewRoutingFallback() *ViewRouting {
	return &ViewRouting{Default: "legacy-xterm", Targets: map[string]ViewTarget{"legacy-xterm": {Builtin: true}}}
}

// loadViewRouting 读路由配置(JSON,路径来自 VIEW_ROUTING_CONFIG)。
// 按文件 mtime 热加载。**任何缺失/出错一律回退老 xterm(fail-safe)。**
func loadViewRouting() *ViewRouting {
	path := strings.TrimSpace(os.Getenv("VIEW_ROUTING_CONFIG"))
	if path == "" {
		return viewRoutingFallback()
	}
	viewRoutingMu.Lock()
	defer viewRoutingMu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return viewRoutingFallback()
	}
	if viewRoutingCache != nil && fi.ModTime().Equal(viewRoutingMTime) {
		return viewRoutingCache
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if viewRoutingCache != nil {
			return viewRoutingCache
		}
		return viewRoutingFallback()
	}
	var vr ViewRouting
	if err := json.Unmarshal(data, &vr); err != nil {
		log.Printf("[view-routing] parse error, keeping previous/fallback: %v", err)
		if viewRoutingCache != nil {
			return viewRoutingCache
		}
		return viewRoutingFallback()
	}
	if vr.Default == "" {
		vr.Default = "legacy-xterm"
	}
	if vr.Targets == nil {
		vr.Targets = map[string]ViewTarget{}
	}
	if _, ok := vr.Targets["legacy-xterm"]; !ok {
		vr.Targets["legacy-xterm"] = ViewTarget{Builtin: true}
	}
	viewRoutingCache = &vr
	viewRoutingMTime = fi.ModTime()
	return &vr
}

// resolve 三级:会话级 > 用户级 > 默认。目标名未在 Targets 定义 ⇒ 兜底老页。
func (vr *ViewRouting) resolve(sessionName, owner string) (string, ViewTarget) {
	name := vr.Default
	if t, ok := vr.Sessions[sessionName]; ok {
		name = t
	} else if owner != "" {
		if t, ok := vr.Users[owner]; ok {
			name = t
		}
	}
	if t, ok := vr.Targets[name]; ok {
		return name, t
	}
	// 配置引用了未定义的 target ⇒ 兜底老页,但打日志提示配置错误(否则静默走 legacy 难排查)
	log.Printf("[view-routing] target %q not defined in targets (session=%s owner=%s), falling back to legacy-xterm", name, sessionName, owner)
	return "legacy-xterm", ViewTarget{Builtin: true}
}

// ownerFromSessionName 取合规会话名(<owner>-<短名>-<key末4位>)的 owner 段。
func ownerFromSessionName(sessionName string) string {
	sessionName = strings.TrimSpace(sessionName)
	if !sessionNamePattern.MatchString(sessionName) {
		return ""
	}
	parts := strings.Split(sessionName, "-")
	if len(parts) == 3 {
		return parts[0]
	}
	return ""
}

// proxyToViewService 反代到 View 服务;剥掉 /view/<名> 前缀 + 带 X-DW-Session 头。
// 后端不可达时(连接失败)返回 false,调用方兜底老页(不让用户白屏)。
func (h *Hub) proxyToViewService(w http.ResponseWriter, r *http.Request, sessionName, target string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	prefix := "/view/" + sessionName
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			p := strings.TrimPrefix(req.URL.Path, prefix)
			if p == "" {
				p = "/"
			}
			req.URL.Path = strings.TrimRight(u.Path, "/") + p
			req.Host = u.Host
			req.Header.Set("X-DW-Session", sessionName)
		},
	}
	reached := true
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
		reached = false // 连接失败:不写响应,交给调用方兜底
	}
	rp.ServeHTTP(w, r)
	return reached
}

// ---- C3:内网鉴权/输入口(View Service 用;复用解锁/写令牌机制,不 fork 鉴权)----

type viewAuthorizeRequest struct {
	Session  string `json:"session"`
	ViewerID string `json:"viewer_id,omitempty"` // 空=注册新虚拟 viewer;有=查询该 viewer 写权
}

type viewAuthorizeResponse struct {
	ViewerID    string `json:"viewer_id"`
	Code        string `json:"code"`         // 页面展示,用户飞书发 #unlock <code> 解锁
	CanRead     bool   `json:"can_read"`     // 会话在线即可读
	CanWrite    bool   `json:"can_write"`    // 已解锁且令牌未过期
	WriterToken string `json:"writer_token"` // can_write 时给出,用于 /input
	ExpiresUnix int64  `json:"expires,omitempty"`
	UnlockHint  string `json:"unlock_hint"`
}

type viewInputRequest struct {
	ViewerID string `json:"viewer_id"`
	Token    string `json:"token"`
	Text     string `json:"text"`
}

// registerViewViewer 为 View Service 注册一个虚拟 viewer(conn=nil),
// 分配唯一 code(与真 WS viewer 同码池,故 #unlock <code> 经 grantTerminalInput 原样命中)。
// 顺带清理陈旧虚拟 viewer。返回 keyID/viewerID/code。
func (h *Hub) registerViewViewer(sessionName string) (keyID, viewerID, code string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.terminalBySessionNameLocked(sessionName)
	if !ok {
		return "", "", "", fmt.Errorf("session offline or ambiguous")
	}
	h.sweepStaleViewViewersLocked(st)
	code = h.uniqueTerminalCodeLocked() // 与真 WS viewer 同一份码生成,避免逻辑漂移
	viewerID = randomHex(8)
	st.viewers[viewerID] = &terminalViewer{id: viewerID, code: code, conn: nil, mu: make(chan struct{}, 1), lastSeen: time.Now()}
	return st.keyID, viewerID, code, nil
}

// sweepStaleViewViewersLocked 移除超过 TTL 未活动的虚拟 viewer(conn==nil)。
// 只碰虚拟 viewer,绝不动真 WS viewer。调用方须持 h.mu。
func (h *Hub) sweepStaleViewViewersLocked(st *terminalState) {
	now := time.Now()
	for id, v := range st.viewers {
		if v.conn == nil && !v.lastSeen.IsZero() && now.Sub(v.lastSeen) > terminalTokenTTL {
			delete(st.viewers, id)
			if st.writerPage == id {
				st.writerPage = ""
				st.writerToken = ""
				st.writerUntil = time.Time{}
			}
		}
	}
}

// sweepDisconnectedViewersLocked 清理断连超过宽限期的网页 viewer(conn==nil 且 disconnectedAt 非零)。
// 只碰"断连宽限"的真网页 viewer,不碰 View Service 虚拟 viewer(那些 disconnectedAt 恒为零、用 lastSeen)。调用方须持 h.mu。
func (h *Hub) sweepDisconnectedViewersLocked(st *terminalState) {
	now := time.Now()
	for id, v := range st.viewers {
		if v.conn == nil && !v.disconnectedAt.IsZero() && now.Sub(v.disconnectedAt) > terminalDisconnectGrace {
			delete(st.viewers, id)
			if st.writerPage == id {
				st.writerPage = ""
				st.writerToken = ""
				st.writerUntil = time.Time{}
			}
		}
	}
}

// viewWriteState 查某虚拟 viewer 当前写权(刷新 lastSeen);复用 writerPage/writerToken/writerUntil。
func (h *Hub) viewWriteState(sessionName, viewerID string) (canWrite bool, token string, expiresUnix int64, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, found := h.terminalBySessionNameLocked(sessionName)
	if !found {
		return false, "", 0, false
	}
	v := st.viewers[viewerID]
	if v == nil {
		return false, "", 0, false
	}
	v.lastSeen = time.Now()
	if st.writerPage == viewerID && st.writerToken != "" && time.Now().Before(st.writerUntil) {
		return true, st.writerToken, st.writerUntil.Unix(), true
	}
	return false, "", 0, true
}

// HandleInternalViewAuthorize(C3):View Service 注册虚拟 viewer / 查询写权。仅内网。
func (h *Hub) HandleInternalViewAuthorize(w http.ResponseWriter, r *http.Request) {
	var req viewAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	sessionName := strings.TrimSpace(req.Session)
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	resp := viewAuthorizeResponse{CanRead: true}
	if req.ViewerID == "" {
		_, viewerID, code, err := h.registerViewViewer(sessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		resp.ViewerID, resp.Code = viewerID, code
	} else {
		canWrite, token, exp, ok := h.viewWriteState(sessionName, req.ViewerID)
		if !ok {
			http.Error(w, "viewer not found (re-register)", http.StatusNotFound)
			return
		}
		resp.ViewerID = req.ViewerID
		resp.CanWrite, resp.WriterToken, resp.ExpiresUnix = canWrite, token, exp
	}
	if resp.Code != "" { // 查询已有 viewer 时无新 code,不拼半截提示(View Service 已握有初始 code)
		resp.UnlockHint = "飞书发送 #unlock " + resp.Code + " 后可输入"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleInternalViewInput(C3):View Service 回投人类输入。复用 routeTerminalInput,
// 解锁判定一字不改(viewerID=writerPage,token=writerToken)。仅内网。
func (h *Hub) HandleInternalViewInput(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("session"))
	if sessionName == "" || strings.Contains(sessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	var req viewInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	st, found := h.terminalBySessionNameLocked(sessionName)
	var keyID string
	if found {
		keyID = st.keyID
		if v := st.viewers[req.ViewerID]; v != nil {
			v.lastSeen = time.Now()
		}
	}
	h.mu.Unlock()
	if !found {
		http.Error(w, "session offline or ambiguous", http.StatusNotFound)
		return
	}
	if err := h.routeTerminalInput(r.Context(), keyID, sessionName, req.ViewerID, req.Token, req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// HandleInternalViewInterrupt validates only the view write grant, then
// forwards the requested identity. The helper is the sole stale-turn CAS authority.
func (h *Hub) HandleInternalViewInterrupt(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimSpace(r.PathValue("session"))
	var req viewInterruptRequest
	if sessionName == "" || strings.Contains(sessionName, "#") || json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.DeliveryID) == "" || strings.TrimSpace(req.TurnID) == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	st, found := h.terminalBySessionNameLocked(sessionName)
	var c *sessionClient
	if found && st.writerPage == req.ViewerID && st.writerToken != "" && st.writerToken == req.Token && time.Now().Before(st.writerUntil) {
		c = h.sessionClients[st.keyID][st.sessionName]
		st.writerUntil = time.Now().Add(terminalTokenTTL)
	}
	h.mu.Unlock()
	if c == nil {
		http.Error(w, "interrupt not authorized or session offline", http.StatusForbidden)
		return
	}
	env := model.Envelope{
		ID: randomHex(16), To: sessionAddress(sessionName, c.keyID), From: sessionAddress("workpulse", c.keyID), TS: time.Now().Unix(),
		Meta: map[string]any{"type": terminalInterruptType, "system": true, "no_mirror": true, "delivery_id": req.DeliveryID, "turn_id": req.TurnID},
	}
	if err := c.write(r.Context(), env); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"state":"forwarded"}`))
}

func (h *Hub) routeTerminalInput(ctx context.Context, keyID, sessionName, pageID, token, data string) error {
	if data == "" {
		return nil
	}
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil || st.writerPage != pageID || st.writerToken == "" || st.writerToken != token || time.Now().After(st.writerUntil) {
		h.mu.Unlock()
		return fmt.Errorf("输入未解锁或令牌已过期")
	}
	st.writerUntil = time.Now().Add(terminalTokenTTL)
	if st.cookieToken == token {
		st.cookieUntil = st.writerUntil
	}
	c := h.sessionClients[keyID][sessionName]
	h.mu.Unlock()
	if c == nil {
		return fmt.Errorf("会话离线")
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: data,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": terminalInputType, "system": true, "no_mirror": true},
	}
	return c.write(ctx, env)
}

// routeTerminalResize 把浏览器 xterm 的实际尺寸(列/行)转发给 sessionHelper，
// 让它对 PTY setwinsize，CLI 才会按整块显示区渲染（否则停在默认 24×80）。
func (h *Hub) routeTerminalResize(ctx context.Context, keyID, sessionName string, cols, rows int) error {
	var c *sessionClient
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		c = h.sessionClients[keyID][sessionName]
		h.terminalLastSize[terminalKey(keyID, sessionName)] = [2]int{cols, rows}
	}()
	if c == nil {
		return nil
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": terminalResizeType, "system": true, "no_mirror": true, "cols": cols, "rows": rows},
	}
	return c.write(ctx, env)
}

func (h *Hub) requestTerminalRefresh(ctx context.Context, keyID, sessionName string) error {
	var c *sessionClient
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		c = h.sessionClients[keyID][sessionName]
	}()
	if c == nil {
		return nil
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": terminalRefreshType, "system": true, "no_mirror": true},
	}
	return c.write(ctx, env)
}

func (h *Hub) requestTerminalRefreshAfterInitialResize(ctx context.Context, keyID, sessionName, viewerID string) {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	ok := st != nil && st.viewers[viewerID] != nil && st.buffer == ""
	h.mu.Unlock()
	if ok {
		_ = h.requestTerminalRefresh(context.Background(), keyID, sessionName)
		// 5 s timeout: if session client didn't respond with terminal output,
		// notify the viewer so the page doesn't stay black indefinitely.
		go func() {
			time.Sleep(5 * time.Second)
			h.mu.Lock()
			st := h.terminals[terminalKey(keyID, sessionName)]
			var viewer *terminalViewer
			stillEmpty := false
			if st != nil {
				viewer = st.viewers[viewerID]
				stillEmpty = st.buffer == ""
			}
			h.mu.Unlock()
			if viewer != nil && stillEmpty {
				_ = terminalWrite(context.Background(), viewer, map[string]any{
					"type":     "status",
					"readonly": true,
					"message":  "等待会话活动...",
				})
			}
		}()
	}
}

func (h *Hub) dispatchTerminalInputCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	commandText := h.stripLeadingBotMentions(msg.BotChannelID, text)
	if fullSessionName, pageCode, ok := parseViewUnlockV2Command(commandText); ok {
		return h.dispatchViewUnlockV2(ctx, msg, fullSessionName, pageCode)
	}
	action, sessionName, pageCode, ok := parseTerminalInputCommand(commandText)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	source := sourceAccount(msg)
	var keyID string
	if sessionName == "" {
		if action == "lock" && pageCode == "" {
			n := h.revokeTerminalWritersForAccount(ctx, source)
			return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("已锁定你名下 %d 个页面的输入。", n)}, true, nil
		}
		k, s, err := h.findTerminalViewerByCode(ctx, source, pageCode)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
		}
		keyID, sessionName = k, s
	} else {
		var found bool
		keyID, found = h.lookupKeyIDForTerminalSession(ctx, source, sessionName)
		if !found {
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到可控制的在线会话：" + sessionName}, true, nil
		}
	}
	switch action {
	case "unlock":
		token, err := h.grantTerminalInput(keyID, sessionName, pageCode)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
		}
		_ = token
		return model.PrefixDispatchResult{Matched: true, Reply: "已解锁输入：" + sessionName + "，8小时无交互自动回收。"}, true, nil
	case "lock":
		h.revokeTerminalInput(keyID, sessionName)
		return model.PrefixDispatchResult{Matched: true, Reply: "已锁定输入：" + sessionName}, true, nil
	default:
		return model.PrefixDispatchResult{}, false, nil
	}
}

var viewUnlockV2CodePattern = regexp.MustCompile(`^([a-z0-9]+-[a-z0-9]+-[0-9a-f]{4})-([A-Za-z0-9]{8})$`)

func parseViewUnlockV2Command(text string) (fullSessionName, pageCode string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 || fields[0] != "#unlock" {
		return "", "", false
	}
	matches := viewUnlockV2CodePattern.FindStringSubmatch(fields[1])
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], fields[1], true
}

func trustedViewUnlockIngress(provenance model.IngressProvenance) bool {
	provenance = model.NormalizeIngressProvenance(provenance)
	return provenance == model.IngressWebhookVerified || provenance == model.IngressFeishuWSAuthenticated
}

func (h *Hub) dispatchViewUnlockV2(ctx context.Context, msg model.Message, fullSessionName, pageCode string) (model.PrefixDispatchResult, bool, error) {
	routeTarget, _ := loadViewRouting().resolve(fullSessionName, ownerFromSessionName(fullSessionName))
	if !trustedViewUnlockIngress(msg.IngressProvenance) {
		auditViewUnlockV2(msg, "", "", "", fullSessionName, routeTarget, pageCode, "rejected", "untrusted_ingress")
		return model.PrefixDispatchResult{Matched: true, Reply: "拒绝解锁：消息来源未通过可信入站认证。"}, true, nil
	}
	senderOwner, targetOwner, senderOpenID, err := h.authorizeViewUnlockOwner(ctx, msg, fullSessionName)
	if err != nil {
		auditViewUnlockV2(msg, senderOpenID, senderOwner, targetOwner, fullSessionName, routeTarget, pageCode, "rejected", err.Error())
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	unlocker := h.ViewPageUnlocker
	if unlocker == nil {
		unlocker = newHMACViewPageUnlocker()
	}
	err = unlocker.UnlockViewPage(ctx, ViewPageUnlockRequest{
		Code: pageCode, FullSessionName: fullSessionName, OwnerKey: targetOwner,
		SenderOpenID: senderOpenID, MessageID: msg.ID,
	})
	if err != nil {
		auditViewUnlockV2(msg, senderOpenID, senderOwner, targetOwner, fullSessionName, routeTarget, pageCode, "failed", err.Error())
		return model.PrefixDispatchResult{Matched: true, Reply: "解锁失败：" + err.Error()}, true, nil
	}
	auditViewUnlockV2(msg, senderOpenID, senderOwner, targetOwner, fullSessionName, routeTarget, pageCode, "granted", "")
	return model.PrefixDispatchResult{Matched: true, Reply: "已解锁页面输入：" + fullSessionName}, true, nil
}

func auditViewUnlockV2(msg model.Message, senderOpenID, senderOwner, targetOwner, targetSession, routeTarget, pageCode, result, reason string) {
	digest := sha256.Sum256([]byte(pageCode))
	log.Printf("view_unlock_v2_audit message_id=%q sender_open_id=%q sender_owner=%q target_session=%q target_owner=%q route_target=%q code_sha256=%s result=%q reason=%q",
		msg.ID, senderOpenID, senderOwner, targetSession, targetOwner, routeTarget,
		hex.EncodeToString(digest[:8]), result, reason)
}

func (h *Hub) authorizeViewUnlockOwner(ctx context.Context, msg model.Message, fullSessionName string) (senderOwner, targetOwner, senderOpenID string, err error) {
	senderOpenID = strings.TrimSpace(msg.SenderOpenID)
	if msg.ChatType == model.ChatGroup && senderOpenID == "" {
		return "", "", "", fmt.Errorf("拒绝解锁：群消息缺少可信发信人")
	}
	if senderOpenID == "" {
		botID, entityID, ok := accountEntityParts(msg.ChatEntityID)
		if !ok || botID != msg.BotChannelID || entityID == "" || msg.ChatType != model.ChatPersonal {
			return "", "", "", fmt.Errorf("拒绝解锁：缺少可信发信人")
		}
		senderOpenID = entityID
	}
	senderOwner = h.ownerKeyForAccount(ctx, msg.BotChannelID+":personal:"+senderOpenID)
	if senderOwner == "" {
		return "", "", senderOpenID, fmt.Errorf("拒绝解锁：发信人未绑定会话 owner")
	}

	endpoints, listErr := h.Repo.ListSessionEndpoints(ctx)
	if listErr != nil {
		return senderOwner, "", senderOpenID, fmt.Errorf("解锁失败：查询会话失败")
	}
	parts := strings.Split(fullSessionName, "-")
	if len(parts) < 3 {
		return senderOwner, "", senderOpenID, fmt.Errorf("拒绝解锁：页面会话名无效")
	}
	shortName, keySuffix := strings.Join(parts[1:len(parts)-1], "-"), parts[len(parts)-1]
	var matches []model.SessionEndpoint
	for _, endpoint := range endpoints {
		if keyTail(endpoint.KeyID) != keySuffix {
			continue
		}
		if endpoint.FullSessionName == fullSessionName || endpoint.SessionName == fullSessionName ||
			(endpoint.FullSessionName == "" && endpoint.SessionName == shortName) {
			matches = append(matches, endpoint)
		}
	}
	if len(matches) != 1 {
		return senderOwner, "", senderOpenID, fmt.Errorf("拒绝解锁：页面会话不存在或不可用")
	}
	targetOwner = strings.TrimSpace(matches[0].OwnerKey)
	if targetOwner == "" {
		targetOwner = h.ownerKeyForKey(ctx, matches[0].KeyID)
	}
	if targetOwner == "" || parts[0] != targetOwner || targetOwner != senderOwner {
		return senderOwner, targetOwner, senderOpenID, fmt.Errorf("拒绝解锁：发信人不是该会话 owner")
	}
	return senderOwner, targetOwner, senderOpenID, nil
}

func parseTerminalInputCommand(text string) (action, sessionName, pageCode string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 1 {
		return "", "", "", false
	}
	switch fields[0] {
	case "#unlock":
		if len(fields) != 2 {
			return "", "", "", false
		}
		return "unlock", "", strings.ToUpper(fields[1]), true
	case "#lock":
		if len(fields) == 2 {
			return "lock", "", strings.ToUpper(fields[1]), true
		}
		if len(fields) == 1 {
			return "lock", "", "", true
		}
		return "", "", "", false
	case "#解锁输入":
		if len(fields) != 3 {
			return "", "", "", false
		}
		return "unlock", fields[1], strings.ToUpper(fields[2]), true
	case "#锁定输入":
		if len(fields) != 2 {
			return "", "", "", false
		}
		return "lock", fields[1], "", true
	default:
		return "", "", "", false
	}
}

func (h *Hub) uniqueTerminalCode() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.uniqueTerminalCodeLocked()
}

// uniqueTerminalCodeLocked 生成全局唯一的 4 位页面码(真 WS viewer 与虚拟 viewer 同码池)。
// 调用方须持 h.mu。真/虚拟 viewer 共用这一份,避免两处各自实现而逻辑漂移。
func (h *Hub) uniqueTerminalCodeLocked() string {
	used := map[string]bool{}
	for _, st := range h.terminals {
		for _, v := range st.viewers {
			used[v.code] = true
		}
	}
	for i := 0; i < 64; i++ {
		c := randomDigits(4)
		if !used[c] {
			return c
		}
	}
	return randomDigits(4)
}

type terminalViewerLookupItem struct {
	keyID        string
	sessionName  string
	code         string
	exactAccount bool
}

func (h *Hub) findTerminalViewerByCode(ctx context.Context, account, code string) (keyID, sessionName string, err error) {
	h.mu.Lock()
	var candidates []terminalViewerLookupItem
	totalViewers := 0
	exactAccountViewers := 0
	for _, st := range h.terminals {
		exact := h.keyAccounts[st.keyID][account]
		for _, v := range st.viewers {
			totalViewers++
			if exact {
				exactAccountViewers++
			}
			candidates = append(candidates, terminalViewerLookupItem{keyID: st.keyID, sessionName: st.sessionName, code: v.code, exactAccount: exact})
		}
	}
	h.mu.Unlock()
	var mk, ms string
	n := 0
	accountOwner := ""
	ownerMatchedViewers := 0
	for _, item := range candidates {
		if !item.exactAccount {
			if accountOwner == "" {
				accountOwner = h.ownerKeyForAccount(ctx, account)
			}
			if accountOwner == "" || h.ownerKeyForKey(ctx, item.keyID) != accountOwner {
				continue
			}
			ownerMatchedViewers++
		}
		if strings.EqualFold(item.code, code) {
			mk, ms = item.keyID, item.sessionName
			n++
		}
	}
	if n == 0 {
		log.Printf("terminal unlock code not found account=%s account_owner=%s code=%s viewers=%d exact_account_viewers=%d owner_matched_viewers=%d", account, accountOwner, code, totalViewers, exactAccountViewers, ownerMatchedViewers)
		return "", "", fmt.Errorf("未找到页面码 %s 对应的可控制页面（先在网页打开会话）", code)
	}
	if n > 1 {
		return "", "", fmt.Errorf("页面码 %s 命中多个页面，请刷新其中一个再试", code)
	}
	return mk, ms, nil
}

func (h *Hub) lookupKeyIDForTerminalSession(ctx context.Context, account, sessionName string) (string, bool) {
	h.mu.Lock()
	var candidates []terminalViewerLookupItem
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] == nil {
			continue
		}
		candidates = append(candidates, terminalViewerLookupItem{keyID: keyID, sessionName: sessionName, exactAccount: h.keyAccounts[keyID][account]})
	}
	h.mu.Unlock()
	accountOwner := ""
	for _, item := range candidates {
		if item.exactAccount {
			return item.keyID, true
		}
		if accountOwner == "" {
			accountOwner = h.ownerKeyForAccount(ctx, account)
		}
		if accountOwner != "" && h.ownerKeyForKey(ctx, item.keyID) == accountOwner {
			return item.keyID, true
		}
	}
	return "", false
}

func (h *Hub) ownerKeyForAccount(ctx context.Context, account string) string {
	if strings.TrimSpace(account) == "" || h.Repo == nil {
		return ""
	}
	members, err := h.Repo.ListMembers(ctx)
	if err == nil {
		for _, member := range members {
			if member.Active && accountBelongsToMember(account, member) {
				return member.OwnerKey
			}
		}
	}
	if botChannelID, feishuID, ok := accountEntityParts(account); ok {
		entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID)
		if err == nil && entity != nil && entity.Active && strings.TrimSpace(entity.BoundOwner) != "" {
			return entity.BoundOwner
		}
	}
	return ""
}

func (h *Hub) revokeTerminalWritersForAccount(ctx context.Context, account string) int {
	h.mu.Lock()
	var candidates []terminalViewerLookupItem
	for _, st := range h.terminals {
		exact := h.keyAccounts[st.keyID][account]
		for _, v := range st.viewers {
			candidates = append(candidates, terminalViewerLookupItem{keyID: st.keyID, sessionName: st.sessionName, code: v.id, exactAccount: exact})
		}
	}
	h.mu.Unlock()
	accountOwner := h.ownerKeyForAccount(ctx, account)
	allowed := map[string]bool{}
	for _, item := range candidates {
		if item.exactAccount || (accountOwner != "" && h.ownerKeyForKey(ctx, item.keyID) == accountOwner) {
			allowed[terminalKey(item.keyID, item.sessionName)] = true
		}
	}
	h.mu.Lock()
	var toNotify []*terminalViewer
	n := 0
	for _, st := range h.terminals {
		if !allowed[terminalKey(st.keyID, st.sessionName)] {
			continue
		}
		if st.writerPage != "" {
			for _, v := range st.viewers {
				if v.id == st.writerPage {
					toNotify = append(toNotify, v)
				}
			}
			st.writerPage = ""
			st.writerToken = ""
			st.writerUntil = time.Time{}
			st.cookieToken = ""
			st.cookieUntil = time.Time{}
			n++
		}
	}
	h.mu.Unlock()
	for _, v := range toNotify {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "write_revoked", "readonly": true, "message": "输入已锁定"})
	}
	return n
}

func (h *Hub) grantTerminalInput(keyID, sessionName, pageCode string) (string, error) {
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil {
		h.mu.Unlock()
		return "", fmt.Errorf("终端未连接：" + sessionName)
	}
	var target *terminalViewer
	for _, v := range st.viewers {
		if strings.EqualFold(v.code, pageCode) {
			target = v
			break
		}
	}
	if target == nil {
		h.mu.Unlock()
		return "", fmt.Errorf("页面码无效或页面已断开")
	}
	oldViewers := make([]*terminalViewer, 0, len(st.viewers))
	for _, v := range st.viewers {
		if v.id != target.id {
			oldViewers = append(oldViewers, v)
		}
	}
	token := randomHex(32)
	st.writerPage = target.id
	st.writerToken = token
	st.writerUntil = time.Now().Add(terminalTokenTTL)
	st.cookieToken = token
	st.cookieUntil = st.writerUntil
	h.mu.Unlock()
	for _, v := range oldViewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "write_revoked", "readonly": true, "message": "输入权已转移到其它页面"})
	}
	_ = terminalWrite(context.Background(), target, map[string]any{
		"type":      "write_granted",
		"readonly":  false,
		"token":     token,
		"expiresIn": int(terminalTokenTTL.Seconds()),
		"message":   "输入已解锁",
	})
	return token, nil
}

func (h *Hub) revokeTerminalInput(keyID, sessionName string) {
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil {
		h.mu.Unlock()
		return
	}
	viewers := make([]*terminalViewer, 0, len(st.viewers))
	for _, v := range st.viewers {
		viewers = append(viewers, v)
	}
	st.writerPage = ""
	st.writerToken = ""
	st.writerUntil = time.Time{}
	st.cookieToken = ""
	st.cookieUntil = time.Time{}
	h.mu.Unlock()
	for _, v := range viewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "write_revoked", "readonly": true, "message": "输入已锁定"})
	}
}

func (h *Hub) validTerminalCookieToken(sessionName, token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.terminalBySessionNameLocked(sessionName)
	if !ok || st == nil || st.cookieToken == "" || st.cookieToken != token || time.Now().After(st.cookieUntil) {
		return false
	}
	return true
}

func (h *Hub) restoreTerminalInputFromCookie(viewer *terminalViewer, keyID, sessionName, token string) bool {
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil || st.cookieToken == "" || st.cookieToken != token || time.Now().After(st.cookieUntil) {
		h.mu.Unlock()
		return false
	}
	oldViewers := make([]*terminalViewer, 0, len(st.viewers))
	for _, v := range st.viewers {
		if v.id != viewer.id && st.writerPage == v.id {
			oldViewers = append(oldViewers, v)
		}
	}
	st.writerPage = viewer.id
	st.writerToken = token
	st.writerUntil = time.Now().Add(terminalTokenTTL)
	h.mu.Unlock()
	for _, v := range oldViewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "write_revoked", "readonly": true, "message": "输入权已转移到其它页面"})
	}
	_ = terminalWrite(context.Background(), viewer, map[string]any{
		"type":      "write_granted",
		"readonly":  false,
		"token":     token,
		"expiresIn": int(terminalTokenTTL.Seconds()),
		"message":   "输入已解锁",
	})
	return true
}

func terminalViewCookie(r *http.Request, sessionName string) (string, bool) {
	c, err := r.Cookie(terminalViewCookieName(sessionName))
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return "", false
	}
	return strings.TrimSpace(c.Value), true
}

func terminalViewCookieName(sessionName string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionName)))
	return "dw_view_" + hex.EncodeToString(sum[:8])
}

func terminalCookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// terminalMockUnlockEnabled returns true when WP_TERMINAL_MOCK_UNLOCK is set to
// "true". This gate is checked at both route registration time (production builds
// never register the handler) and at request time (defence in depth).
func terminalMockUnlockEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WP_TERMINAL_MOCK_UNLOCK")), "true")
}

// HandleTerminalMockUnlock is a test-only endpoint that validates a terminal
// unlock code without going through the real Feishu bot flow. It is gated behind
// WP_TERMINAL_MOCK_UNLOCK=true and must be disabled in production builds.
//
// POST /api/terminal/mock-unlock
// Body: {"code": "1234", "session_name": "alice-home-abc1"}
// Response: {"valid": true, "token": "<32-char-hex>"} or {"valid": false}
//
// The fixed test code "0000" always passes for any session that has an active
// viewer page — this lets integration tests unlock any session without a real
// Feishu round-trip.
func (h *Hub) HandleTerminalMockUnlock(w http.ResponseWriter, r *http.Request) {
	if !terminalMockUnlockEnabled() {
		http.Error(w, "not available", http.StatusNotFound)
		return
	}
	var req struct {
		Code        string `json:"code"`
		SessionName string `json:"session_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(req.Code)
	sessionName := strings.TrimSpace(req.SessionName)
	if code == "" || sessionName == "" {
		http.Error(w, "code and session_name required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Fixed test code "0000" bypasses Feishu and unlocks any session.
	if code != "0000" {
		_, _ = w.Write([]byte(`{"valid":false}`))
		return
	}
	h.mu.Lock()
	st, ok := h.terminalBySessionNameLocked(sessionName)
	if !ok || st == nil {
		h.mu.Unlock()
		_, _ = w.Write([]byte(`{"valid":false,"error":"terminal not connected"}`))
		return
	}
	if len(st.viewers) == 0 {
		h.mu.Unlock()
		_, _ = w.Write([]byte(`{"valid":false,"error":"no viewer page open"}`))
		return
	}
	// Grant input to the first viewer page.
	token := randomHex(32)
	until := time.Now().Add(terminalTokenTTL)
	var target *terminalViewer
	for _, v := range st.viewers {
		target = v
		st.writerPage = v.id
		break
	}
	st.writerToken = token
	st.writerUntil = until
	st.cookieToken = token
	st.cookieUntil = until
	h.mu.Unlock()
	_ = terminalWrite(context.Background(), target, map[string]any{
		"type":      "write_granted",
		"readonly":  false,
		"token":     token,
		"expiresIn": int(terminalTokenTTL.Seconds()),
		"message":   "输入已解锁 (mock)",
	})
	_, _ = w.Write([]byte(`{"valid":true,"token":"` + token + `"}`))
}

const terminalPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DingWei Terminal - %s</title>
  <link rel="stylesheet" href="/xterm/xterm.min.css">
  <style>
    html, body { height: 100%%; margin: 0; background: #0b0f14; color: #d7dde8; font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { display: flex; flex-direction: column; }
    .bar { display: flex; align-items: center; gap: 12px; padding: 8px 12px; background: #111823; border-bottom: 1px solid #273244; font-size: 14px; }
    .bar strong { color: #ffffff; }
    .bar input { width: 90px; background: #0b0f14; color: #e8edf5; border: 1px solid #3a4658; border-radius: 4px; padding: 4px 6px; }
    .pill { padding: 2px 7px; border-radius: 999px; background: #263244; color: #dbe6f7; }
    .locked { color: #ffcf66; }
    .unlocked { color: #7ee787; }
    #terminal { flex: 1; min-height: 0; padding: 8px; overflow: hidden; box-sizing: border-box; }
  </style>
</head>
<body>
  <div class="bar">
    <strong>#%s</strong>
    <span>页面码 <span id="code" class="pill">------</span></span>
    <span id="state" class="locked">只读</span>
    <label>最多显示行数 <input id="scrollback" type="number" min="500" max="100000" step="500"></label>
    <span id="msg"></span>
  </div>
  <div id="terminal"></div>
  <script src="/xterm/xterm.min.js"></script>
  <script src="/xterm/addon-fit.min.js"></script>
  <script>
    const sessionName = location.pathname.split('/').pop();
    const key = 'dingwei.terminal.scrollback.' + sessionName;
    const input = document.getElementById('scrollback');
    input.value = localStorage.getItem(key) || '2000';
    let fit;
    function newTerm() {
      const t = new Terminal({ convertEol: false, cursorBlink: true, scrollback: parseInt(input.value, 10) || 2000, theme: { background: '#0b0f14' } });
      fit = new FitAddon.FitAddon();
      t.loadAddon(fit);
      t.open(document.getElementById('terminal'));
      try { fit.fit(); } catch (e) {}
      return t;
    }
    let term = newTerm();
    let fitPending = false;
    let lastSize = '';
    function sendResize(force) {
      try {
        if (!ws || ws.readyState !== 1 || !term) return;
        if (!term.cols || !term.rows) return;
        const s = term.cols + 'x' + term.rows;
        if (!force && s === lastSize) return; lastSize = s;
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      } catch (e) {}
    }
    function fitAndResize(force) {
      try { fit.fit(); sendResize(force); } catch (e) {}
    }
    const doFit = () => { if (fitPending) return; fitPending = true; requestAnimationFrame(() => { fitPending = false; fitAndResize(false); }); };
    function primeResize() {
      lastSize = '';
      fitAndResize(true);
      // If terminal already has dimensions after first fit, we are done.
      if (term.cols && term.rows) return;
      // Otherwise watch for non-zero dimensions via ResizeObserver
      // (handles slow xterm.js initialization beyond fixed-delay retries).
      let attempts = 0;
      const maxAttempts = 12;
      const termEl = document.getElementById('terminal');
      const ro = new ResizeObserver(() => {
        fitAndResize(true);
        attempts++;
        if ((term.cols && term.rows) || attempts >= maxAttempts) {
          ro.disconnect();
        }
      });
      ro.observe(termEl);
      // Hard fallback after 3 s (covers edge case where ResizeObserver
      // fires but xterm.js cols/rows stay zero due to CSS/layout issues).
      setTimeout(() => { ro.disconnect(); fitAndResize(true); }, 3000);
    }
    new ResizeObserver(doFit).observe(document.getElementById('terminal'));
    window.addEventListener('resize', doFit);
    function recreate() {
      localStorage.setItem(key, input.value);
      const old = term;
      document.getElementById('terminal').innerHTML = '';
      term = newTerm();
      old.dispose();
      term.write('\r\n[scrollback changed; reconnect to replay buffer]\r\n');
      bindTerm();
    }
    input.addEventListener('change', recreate);
    let token = '';
    let readonly = true;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    let ws;
    let reconnectDelay = 1000;
    function setStatus(ro, msg) {
      readonly = ro;
      document.getElementById('state').textContent = ro ? '只读' : '可输入';
      document.getElementById('state').className = ro ? 'locked' : 'unlocked';
      document.getElementById('msg').textContent = msg || '';
    }
	    function persistUnlockCookie(unlockToken) {
	      if (!unlockToken) return;
	      fetch('/view/' + encodeURIComponent(sessionName) + '/unlock-cookie', {
	        method: 'POST',
	        credentials: 'same-origin',
	        headers: { 'Content-Type': 'application/json' },
	        body: JSON.stringify({ token: unlockToken })
	      }).catch(() => {});
	    }
    var pageId = (function(){
      var k = 'dwViewPage:' + sessionName;
      try { var p = localStorage.getItem(k); if(!p){ p = 'p'+Math.random().toString(36).slice(2,10)+Date.now().toString(36); localStorage.setItem(k,p);} return p; }
      catch(e){ return 'p'+Math.random().toString(36).slice(2,10); }
    })();
    function connectWS() {
      ws = new WebSocket(proto + '//' + location.host + '/ws/view/' + encodeURIComponent(sessionName) + '?page=' + encodeURIComponent(pageId));
      ws.onopen = () => { reconnectDelay = 1000; primeResize(); };
      ws.onmessage = (ev) => {
        const m = JSON.parse(ev.data);
        if (m.type === 'output') term.write(m.data || '');
        if (m.type === 'status') {
          if (m.code) document.getElementById('code').textContent = m.code;
          setStatus(m.readonly !== false, m.message || '');
        }
        if (m.type === 'write_granted') { token = m.token || ''; persistUnlockCookie(token); setStatus(false, m.message || '输入已解锁'); }
        if (m.type === 'write_revoked') { token = ''; setStatus(true, m.message || '输入已锁定'); }
      };
      ws.onclose = () => {
        token = '';
        setStatus(true, '连接断开，自动重连中…');
        setTimeout(connectWS, reconnectDelay);
        reconnectDelay = Math.min(Math.floor(reconnectDelay * 1.5), 10000);
      };
      ws.onerror = () => { try { ws.close(); } catch (e) {} };
    }
    connectWS();
    function bindTerm() {
      term.onData((data) => {
        if (readonly || !token || !ws || ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify({ type: 'input', token, data }));
      });
    }
    bindTerm();
  </script>
</body>
</html>`
