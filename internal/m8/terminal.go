package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	terminalOutputType = "terminal_output"
	terminalInputType  = "terminal_input"
	terminalResizeType = "terminal_resize"
	terminalTokenTTL   = 8 * time.Hour
	terminalBufferCap  = 1 << 20
)

type terminalState struct {
	keyID       string
	sessionName string
	buffer      string
	viewers     map[string]*terminalViewer
	writerPage  string
	writerToken string
	writerUntil time.Time
}

type terminalViewer struct {
	id   string
	code string
	conn *websocket.Conn
	mu   chan struct{}
}

type terminalClientMessage struct {
	Type  string `json:"type"`
	Data  string `json:"data,omitempty"`
	Token string `json:"token,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Cols  int    `json:"cols,omitempty"`
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, terminalPageHTML, html.EscapeString(sessionName), html.EscapeString(sessionName))
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
	viewer := &terminalViewer{id: randomHex(8), code: h.uniqueTerminalCode(), conn: conn, mu: make(chan struct{}, 1)}
	viewer.mu <- struct{}{}
	var st *terminalState
	h.mu.Lock()
	var ok bool
	st, ok = h.terminalBySessionNameLocked(sessionName)
	if ok {
		st.viewers[viewer.id] = viewer
	}
	h.mu.Unlock()
	if !ok || st == nil {
		_ = terminalWrite(r.Context(), viewer, map[string]any{"type": "status", "readonly": true, "message": "会话不在线或会话名不唯一"})
		_ = conn.Close(websocket.StatusNormalClosure, "offline")
		return
	}
	defer func() {
		h.mu.Lock()
		if cur := h.terminals[terminalKey(st.keyID, st.sessionName)]; cur != nil {
			delete(cur.viewers, viewer.id)
			if cur.writerPage == viewer.id {
				cur.writerPage = ""
				cur.writerToken = ""
				cur.writerUntil = time.Time{}
			}
		}
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}()
	if st.buffer != "" {
		_ = terminalWrite(r.Context(), viewer, map[string]any{"type": "output", "data": st.buffer})
	}
	_ = terminalWrite(r.Context(), viewer, map[string]any{
		"type":     "status",
		"readonly": st.writerPage != viewer.id,
		"code":     viewer.code,
		"message":  "飞书发送 #unlock " + viewer.code + " 后可输入",
	})
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
	syncTargets := h.appendTerminalSyncItemLocked(c.keyID, c.sessionName, data, ts)
	h.mu.Unlock()
	for _, v := range viewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "output", "data": data})
	}
	item := terminalSyncItem{TS: ts, Text: data}
	for _, target := range syncTargets {
		_ = h.sendFeishuSyncItem(context.Background(), c.keyID, c.sessionName, target, item)
	}
	return true
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
	h.mu.Lock()
	c := h.sessionClients[keyID][sessionName]
	h.mu.Unlock()
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

func (h *Hub) dispatchTerminalInputCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, pageCode, ok := parseTerminalInputCommand(h.stripLeadingBotMentions(msg.BotChannelID, text))
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	source := sourceAccount(msg)
	var keyID string
	if sessionName == "" {
		if action == "lock" && pageCode == "" {
			n := h.revokeTerminalWritersForAccount(source)
			return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("已锁定你名下 %d 个页面的输入。", n)}, true, nil
		}
		k, s, err := h.findTerminalViewerByCode(source, pageCode)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
		}
		keyID, sessionName = k, s
	} else {
		var found bool
		keyID, found = h.lookupKeyIDForSession(source, sessionName)
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

func (h *Hub) findTerminalViewerByCode(account, code string) (keyID, sessionName string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var mk, ms string
	n := 0
	for _, st := range h.terminals {
		if !h.keyAccounts[st.keyID][account] {
			continue
		}
		for _, v := range st.viewers {
			if strings.EqualFold(v.code, code) {
				mk, ms = st.keyID, st.sessionName
				n++
			}
		}
	}
	if n == 0 {
		return "", "", fmt.Errorf("未找到页面码 %s 对应的可控制页面（先在网页打开会话）", code)
	}
	if n > 1 {
		return "", "", fmt.Errorf("页面码 %s 命中多个页面，请刷新其中一个再试", code)
	}
	return mk, ms, nil
}

func (h *Hub) revokeTerminalWritersForAccount(account string) int {
	h.mu.Lock()
	var toNotify []*terminalViewer
	n := 0
	for _, st := range h.terminals {
		if !h.keyAccounts[st.keyID][account] {
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
	h.mu.Unlock()
	for _, v := range viewers {
		_ = terminalWrite(context.Background(), v, map[string]any{"type": "write_revoked", "readonly": true, "message": "输入已锁定"})
	}
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
    input.value = localStorage.getItem(key) || '5000';
    let fit;
    function newTerm() {
      const t = new Terminal({ convertEol: false, cursorBlink: true, scrollback: parseInt(input.value, 10) || 5000, theme: { background: '#0b0f14' } });
      fit = new FitAddon.FitAddon();
      t.loadAddon(fit);
      t.open(document.getElementById('terminal'));
      try { fit.fit(); } catch (e) {}
      return t;
    }
    let term = newTerm();
    let fitPending = false;
    let lastSize = '';
    function sendResize() {
      try {
        if (!ws || ws.readyState !== 1 || !term) return;
        const s = term.cols + 'x' + term.rows;
        if (s === lastSize) return; lastSize = s;
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      } catch (e) {}
    }
    const doFit = () => { if (fitPending) return; fitPending = true; requestAnimationFrame(() => { fitPending = false; try { fit.fit(); sendResize(); } catch (e) {} }); };
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
    function connectWS() {
      ws = new WebSocket(proto + '//' + location.host + '/ws/view/' + encodeURIComponent(sessionName));
      ws.onopen = () => { reconnectDelay = 1000; try { fit.fit(); } catch (e) {} sendResize(); };
      ws.onmessage = (ev) => {
        const m = JSON.parse(ev.data);
        if (m.type === 'output') term.write(m.data || '');
        if (m.type === 'status') {
          if (m.code) document.getElementById('code').textContent = m.code;
          setStatus(m.readonly !== false, m.message || '');
        }
        if (m.type === 'write_granted') { token = m.token || ''; setStatus(false, m.message || '输入已解锁'); }
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
