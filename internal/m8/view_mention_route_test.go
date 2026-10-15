package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-0102-VIEW-MENTION-ROUTE（要点板 runbooks/PLAN-R-V2-0102-VIEW-MENTION-ROUTE-opus50tc2k.md 第四节＋第七至九节）：
// view 页提及同组 Agent ⇒ 当前会话照收原文，被提及方另收一封；只认结构化 mentions。

type vmFrames struct{ ch chan model.Envelope }

// collect 后台持续读，避免带超时的 Read 在超时时把连接关掉（coder/websocket 语义）。
// 持久投递（带 delivery_id）照真 helper 回 processed 回执，免得租约到期重投、干扰「不得再收到」类断言。
func vmCollect(conn *websocket.Conn) *vmFrames {
	f := &vmFrames{ch: make(chan model.Envelope, 64)}
	go func() {
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				close(f.ch)
				return
			}
			var env model.Envelope
			if json.Unmarshal(data, &env) != nil || metaString(env.Meta, "type") == agentNetworkSkillPushType {
				continue
			}
			if dlv := metaString(env.Meta, "delivery_id"); dlv != "" {
				_ = writeEnvelope(context.Background(), conn, model.Envelope{ID: "ack-" + dlv, To: env.From,
					Meta: map[string]any{"type": "delivery_ack", "delivery_id": dlv, "state": "processed"}})
			}
			f.ch <- env
		}
	}()
	return f
}

// next 等一封正文以 body 开头的信封。
func (f *vmFrames) next(t *testing.T, body string) model.Envelope {
	t.Helper()
	deadline := time.After(3 * time.Second)
	var skipped []string
	for {
		select {
		case env, ok := <-f.ch:
			if !ok {
				t.Fatalf("连接已断，未等到 %q", body)
			}
			if strings.HasPrefix(env.Body, body) {
				return env
			}
			skipped = append(skipped, fmt.Sprintf("%s|%.40q", metaString(env.Meta, "type"), env.Body))
		case <-deadline:
			t.Fatalf("3s 内未收到 %q；其间收到并跳过：%v", body, skipped)
		}
	}
}

// none 在 d 内不得出现正文以 body 开头的信封。
func (f *vmFrames) none(t *testing.T, body string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case env, ok := <-f.ch:
			if !ok {
				return
			}
			if strings.HasPrefix(env.Body, body) {
				t.Fatalf("不应收到 %q，实得 %+v", body, env)
			}
		case <-deadline:
			return
		}
	}
}

type vmEnv struct {
	*viewEnv
	secret        []byte
	ids           map[string]string
	a, x, q, z, y *vmFrames
	n             *vmFrames // Nack：在线但不报 delivery_ack_v1（组长 NOACK 050053）
	qConn         *websocket.Conn
	yConn         *websocket.Conn
	groupCode     string
	xConn         *websocket.Conn // Beta 的连接（R-V2-0103 回流用例从这里发回合事件）
	qCred         string
	yCred         string
}

type vmResp struct {
	OK       bool                `json:"ok"`
	Mentions []viewMentionResult `json:"mentions"`
}

func newVMEnv(t *testing.T) *vmEnv {
	t.Helper()
	v := newViewEnv(t) // zzc：Opus50TS、Kimi；lbl：kimi
	e := &vmEnv{viewEnv: v, ids: map[string]string{}}
	ctx := v.ctx
	kimi, err := v.svc.Store.FindAgentByName(ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	e.ids["Kimi"], e.ids["Opus50TS"] = kimi.ID, v.agID
	xCred, xID := v.enrollFor("zzc", "dev-x", "Beta")
	e.qCred, e.ids["Quill"] = v.enrollFor("zzc", "dev-q", "Quill")
	zCred, zID := v.enrollFor("zzc", "dev-z", "Outsider")
	yCred, yID := v.enrollFor("lbl", "dev-y", "Gamma")
	e.yCred = yCred
	e.ids["Beta"], e.ids["Outsider"], e.ids["Gamma"] = xID, zID, yID
	nCred, nID := v.enrollFor("zzc", "dev-n", "Nack")
	e.ids["Nack"] = nID
	for _, n := range []string{"W1", "W2", "W3"} {
		_, e.ids[n] = v.enrollFor("zzc", "dev-"+n, n)
	}
	g, err := v.svc.CreateGroup(ctx, "zzc", kimi.ID, "testa01")
	if err != nil {
		t.Fatal(err)
	}
	e.groupCode = g.Code
	for _, n := range []string{"Beta", "Quill", "Opus50TS", "W1", "W2", "W3", "Nack"} {
		if err := v.svc.JoinGroup(ctx, "zzc", g.Code, e.ids[n]); err != nil {
			t.Fatalf("join %s: %v", n, err)
		}
	}
	if err := v.svc.JoinGroup(ctx, "lbl", g.Code, yID); err != nil { // 同组、他人 owner
		t.Fatal(err)
	}
	dial := func(cred, owner, nameLC string) (*websocket.Conn, *vmFrames) {
		conn, st, body := v.dial(cred, "")
		if conn == nil {
			t.Fatalf("dial %s: %d %s", nameLC, st, body)
		}
		t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "done") })
		waitSessionOnline(t, v.hub, AgentKeyID(owner), nameLC)
		return conn, vmCollect(conn)
	}
	_, e.a = dial(v.zKimiCred, "zzc", "kimi")
	xConn, xFrames := dial(xCred, "zzc", "beta")
	e.xConn = xConn
	e.x = xFrames
	_, e.n = dial(nCred, "zzc", "nack") // 不发 session_capabilities ⇒ 不报 ack
	_, e.z = dial(zCred, "zzc", "outsider")
	e.yConn, e.y = dial(yCred, "lbl", "gamma")
	if err := writeEnvelope(ctx, e.yConn, model.Envelope{ID: "caps-y", To: "workpulse#" + AgentKeyID("lbl"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "gamma supports ACK", func() bool { ack, _ := v.hub.viewMentionPeerACK(ctx, AgentKeyID("lbl"), "gamma"); return ack })
	// Quill 自报 delivery_ack_v1 ⇒ 走持久投递（有 delivery_id，可查回入站）
	e.qConn, e.q = dial(e.qCred, "zzc", "quill")
	if err := writeEnvelope(ctx, e.qConn, model.Envelope{ID: "caps-q", To: "workpulse#" + AgentKeyID("zzc"),
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	// Beta 同样自报 ack（现行 helper 无条件报，app.py:475）；只有 Nack 不报
	if err := writeEnvelope(ctx, xConn, model.Envelope{ID: "caps-x", To: "workpulse#" + AgentKeyID("zzc"),
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quill", "beta"} {
		waitFor(t, time.Second, name+" 支持 delivery_ack", func() bool {
			v.hub.mu.Lock()
			c := v.hub.sessionClients[AgentKeyID("zzc")][name]
			v.hub.mu.Unlock()
			return c != nil && v.hub.sessionSupportsDeliveryACK(c)
		})
	}

	e.secret = []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, e.secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	rawConfig, _ := json.Marshal(map[string]any{
		"sessions": map[string]string{"kimi.zzc": "canary"},
		"targets": map[string]any{"canary": map[string]any{
			"url": "http://127.0.0.1:19321", "view_unlock_v2": true, "vs_to_hub_secret_file": secretFile,
		}},
	})
	if err := os.WriteFile(configFile, rawConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	return e
}

// post 经签名的 view-service → Hub 入口（与生产同一处理链）。
func (e *vmEnv) post(t *testing.T, requestID, text string, mentions []string) (int, string, vmResp) {
	t.Helper()
	body, _ := json.Marshal(viewV2InputRequest{Session: "kimi.zzc", PageID: "page-vm", RequestID: requestID, Text: text, PrincipalID: "zzc", Mentions: mentions})
	path := "/internal/view-v2/kimi.zzc/input"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomHex(16)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.SetPathValue("session", "kimi.zzc")
	req.Header.Set("X-DW-VS-Target", "canary")
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(e.secret, vsToHubDirection, http.MethodPost, path, ts, nonce, body))
	rr := httptest.NewRecorder()
	e.hub.HandleInternalViewV2Input(rr, req)
	var r vmResp
	_ = json.Unmarshal(rr.Body.Bytes(), &r)
	return rr.Code, strings.TrimSpace(rr.Body.String()), r
}

func vmReceipts(r vmResp) []string {
	var out []string
	for _, m := range r.Mentions {
		out = append(out, m.FullName+"|"+m.Status+"|"+m.Reason+"|"+m.Message)
	}
	return out
}

func TestViewMentionRoute(t *testing.T) {
	e := newVMEnv(t)
	ctx := e.ctx

	// 格 1：同组同 owner ⇒ 当前会话收原文、被提及方另收一封，Meta 标来源；回执逐字。
	t.Run("same group same owner delivers and labels the source", func(t *testing.T) {
		st, _, r := e.post(t, "11111111111111111111111111111111", "@Beta 格一你好", []string{"Beta.zzc"})
		if st != 200 || !r.OK {
			t.Fatalf("status=%d resp=%+v", st, r)
		}
		if a := e.a.next(t, "@Beta 格一你好"); a.Body != "@Beta 格一你好\r" {
			t.Fatalf("A body=%q", a.Body)
		}
		x := e.x.next(t, "@Beta 格一你好")
		if x.Body != "@Beta 格一你好\r" {
			t.Fatalf("X body=%q（正文须原样）", x.Body)
		}
		for k, want := range map[string]any{"route_source": "view_mention", "mentioned_from": "Kimi.zzc", "source_delivery_id": "11111111111111111111111111111111",
			"reply_policy": "no_reply", "source_kind": "user"} {
			if got := x.Meta[k]; got != want {
				t.Errorf("X meta %s=%v want %v（meta=%v）", k, got, want, x.Meta)
			}
		}
		if !metaBool(x.Meta, metaKeyMentionVerbatim) || !metaBool(x.Meta, "no_mirror") {
			t.Errorf("X meta 缺一跳封顶或 no_mirror：%v", x.Meta)
		}
		want := []string{"Beta.zzc|delivered||已转给 Beta，回复请到 Beta 的页面查看"}
		if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=%q want %q", got, want)
		}
	})

	// 格 2（组长 NOFALLBACK）：正文有与候选 insert 相同的＠文字、mentions 空 ⇒ 只投当前会话，响应逐字节同旧。全角＠同。
	t.Run("at-text without structured mentions only reaches the current session", func(t *testing.T) {
		for i, text := range []string{"@Beta 格二半角", "＠Beta 格二全角"} {
			st, raw, _ := e.post(t, strings.Repeat(strconv.Itoa(2+i), 32), text, nil)
			if st != 200 || raw != `{"ok":true}` {
				t.Fatalf("%q: %d %s", text, st, raw)
			}
			e.a.next(t, text)
		}
		e.x.none(t, "@Beta 格二", 400*time.Millisecond)
		e.x.none(t, "＠Beta 格二", 100*time.Millisecond)
	})

	// 格 3：不在本组（同 owner）⇒ 不投、回执 not_in_group。
	t.Run("agent outside the group is rejected", func(t *testing.T) {
		_, _, r := e.post(t, "44444444444444444444444444444444", "格三", []string{"Outsider.zzc"})
		want := []string{"Outsider.zzc|rejected|not_in_group|未转给 Outsider.zzc：不在本组或已不可用"}
		if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=%q", got)
		}
		e.a.next(t, "格三")
		e.z.none(t, "格三", 400*time.Millisecond)
	})

	// 同组跨属主：明确 Agent 来源，不继承接收者业主的权限。
	t.Run("same group other owner delivers as an attributed Agent message", func(t *testing.T) {
		_, _, r := e.post(t, "55555555555555555555555555555555", "格四", []string{"Gamma.lb"})
		want := []string{"Gamma.lb|delivered||已转给 Gamma.lb，回复请到 Gamma.lb 的页面查看"}
		if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=%q", got)
		}
		e.a.next(t, "格四")
		y := e.y.next(t, "格四")
		assertCrossOwnerView(t, y)
	})

	// 格 6：mentions 内重复（含大小写）只投一次；同 request_id 重放不再投。
	t.Run("duplicates and replays deliver once", func(t *testing.T) {
		const rid = "66666666666666666666666666666666"
		_, _, r := e.post(t, rid, "格六", []string{"Beta.zzc", "beta.ZZC", " Beta.zzc "})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
			t.Fatalf("回执=%q", vmReceipts(r))
		}
		e.x.next(t, "格六")
		st, raw, _ := e.post(t, rid, "格六", []string{"Beta.zzc", "beta.ZZC", " Beta.zzc "})
		if st != 200 || raw != `{"ok":true,"duplicate":true}` {
			t.Fatalf("replay: %d %s", st, raw)
		}
		e.x.none(t, "格六", 400*time.Millisecond)
	})

	// 格 7：提及自己 ⇒ 当前会话只收一封、无回执。
	t.Run("mentioning itself is a no-op", func(t *testing.T) {
		st, raw, _ := e.post(t, "77777777777777777777777777777777", "格七", []string{"Kimi.zzc", "KIMI.zzc"})
		if st != 200 || raw != `{"ok":true}` {
			t.Fatalf("%d %s", st, raw)
		}
		e.a.next(t, "格七")
		e.a.none(t, "格七", 400*time.Millisecond)
	})

	// 格 8：超过 5 个同组同 owner 目标 ⇒ 前 5 个处理、其余 over_limit；从未连过的目标 ⇒ failed offline。
	t.Run("over the limit and never-connected targets", func(t *testing.T) {
		_, _, r := e.post(t, "88888888888888888888888888888888", "格八", []string{"Beta.zzc", "Opus50TS.zzc", "W1.zzc", "W2.zzc", "W3.zzc", "Quill.zzc"})
		got := vmReceipts(r)
		want := []string{
			"Beta.zzc|delivered||已转给 Beta，回复请到 Beta 的页面查看",
			"Opus50TS.zzc|failed|offline|Opus50TS 不在线，暂无法转达",
			"W1.zzc|failed|offline|W1 不在线，暂无法转达",
			"W2.zzc|failed|offline|W2 不在线，暂无法转达",
			"W3.zzc|failed|offline|W3 不在线，暂无法转达",
			"Quill.zzc|rejected|over_limit|未转给 Quill：每条最多转给 5 个 Agent",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		e.x.next(t, "格八")
		e.q.none(t, "格八", 400*time.Millisecond)
	})

	// 格 9：被提及方那一轮输出里再写提及 ⇒ AMR 一跳封顶，不再按提及路由（amr.go process 的 mention_verbatim）。
	t.Run("the mentioned agent's own mention is not routed again", func(t *testing.T) {
		_, _, r := e.post(t, "99999999999999999999999999999999", "格九", []string{"Quill.zzc"})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
			t.Fatalf("回执=%q", vmReceipts(r))
		}
		q := e.q.next(t, "格九")
		dlv := metaString(q.Meta, "delivery_id")
		if dlv == "" {
			t.Fatalf("Quill 应经持久投递收到（有 delivery_id），meta=%v", q.Meta)
		}
		p := testAMRProcessor()
		p.hub = e.hub
		var routed []model.Envelope
		p.routeEnvelope = func(_ context.Context, env model.Envelope) error { routed = append(routed, env); return nil }
		p.process(amrRouteJob{key: amrTurnKey{KeyID: AgentKeyID("zzc"), Session: "quill", DeliveryID: dlv, TurnID: "t-9"}, body: "收到，转给 @Beta 看看"})
		stats, _ := p.snapshotStats()
		if stats.closedNonCandidate != 1 || stats.closedDispatched != 0 || stats.closedCandidateRejected != 0 {
			t.Fatalf("一跳封顶失效：closed non_candidate=%d dispatched=%d rejected=%d", stats.closedNonCandidate, stats.closedDispatched, stats.closedCandidateRejected)
		}
		for _, env := range routed {
			if strings.HasPrefix(env.To, "beta#") {
				t.Fatalf("不应转投 Beta：%+v", env)
			}
		}
	})

	// 格 5：被提及方离线（曾以 delivery_ack_v1 连过）⇒ 持久排队、回执 queued；重连后收到。
	t.Run("offline target is queued and delivered on reconnect", func(t *testing.T) {
		_ = e.qConn.Close(websocket.StatusNormalClosure, "offline")
		waitFor(t, 2*time.Second, "Quill 下线", func() bool { return !e.hub.AgentOnline(e.ids["Quill"]) })
		_, _, r := e.post(t, "55555555555555555555555555555550", "格五", []string{"Quill.zzc"})
		want := []string{"Quill.zzc|queued||Quill 不在线，已排队；回复请到 Quill 的页面查看"}
		if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=%q", got)
		}
		e.a.next(t, "格五")
		conn, st, body := e.dial(e.qCred, "")
		if conn == nil {
			t.Fatalf("redial: %d %s", st, body)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		frames := vmCollect(conn)
		if err := writeEnvelope(ctx, conn, model.Envelope{ID: "caps-q2", To: "workpulse#" + AgentKeyID("zzc"),
			Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
			t.Fatal(err)
		}
		if got := frames.next(t, "格五"); metaString(got.Meta, "mentioned_from") != "Kimi.zzc" {
			t.Fatalf("重连后收到的信封 meta=%v", got.Meta)
		}
	})

	// 格 12（组长 NOACK 050053）：对端在线但不报 delivery_ack_v1 ⇒ 不投（直写会拿不到 delivery_id，一跳封顶失效），
	// 回执 failed 逐字；当前会话照收原文。反向去掉该判断 ⇒ 本格红（即 tosonnet55tc2k 的 TestRevNoAckPeerOneHopCap 情形）。
	t.Run("a peer without delivery_ack_v1 is not sent to", func(t *testing.T) {
		_, _, r := e.post(t, "12121212121212121212121212121212", "格十二", []string{"Nack.zzc"})
		want := []string{"Nack.zzc|failed|no_ack|Nack 暂不支持可靠转发，未转"}
		if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("回执=%q", got)
		}
		e.a.next(t, "格十二")
		e.n.none(t, "格十二", 400*time.Millisecond)
	})

	// 提示 a：request_id 只收 32 位 hex 或 UUID；超长 ⇒ 400、谁都不投。派生号长度有界。
	t.Run("overlong request id is refused before any delivery", func(t *testing.T) {
		st, _, _ := e.post(t, strings.Repeat("a", 64), "格a", []string{"Beta.zzc"})
		if st != http.StatusBadRequest {
			t.Fatalf("status=%d", st)
		}
		e.x.none(t, "格a", 300*time.Millisecond)
		id := viewMentionEnvelopeID("01234567-89ab-cdef-0123-456789abcdef", e.ids["Beta"])
		if len(id) != 36+3+12 || id != viewMentionEnvelopeID("01234567-89ab-cdef-0123-456789abcdef", e.ids["Beta"]) || id == viewMentionEnvelopeID("01234567-89ab-cdef-0123-456789abcdef", e.ids["W1"]) {
			t.Fatalf("派生号=%q", id)
		}
	})
}

// 格 10：他人访问带 mentions 的 /input ⇒ 认人层照拒（mentions 不改变授权）。
func TestViewMentionRouteNonOwnerRefused(t *testing.T) {
	v := newViewEnv(t)
	body, _ := json.Marshal(map[string]any{"text": "x", "mentions": []string{"Opus50TS.zzc"}})
	req, _ := http.NewRequest("POST", v.srv.URL+"/view/Kimi.zzc/input", bytes.NewReader(body))
	req.Header.Set("Origin", npOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokLBL})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.StatusCode != 404 || strings.TrimSpace(string(out)) != `{"error":"not_found"}` {
		t.Fatalf("other user: %d %s", res.StatusCode, out)
	}
}

// 格 11：旧口径入口（VIEW_UNLOCK_V2 关）与 V2 同语义。
func TestViewMentionRouteLegacyInput(t *testing.T) {
	e := newVMEnv(t)
	_, viewerID, _, err := e.hub.registerViewViewer("kimi.zzc")
	if err != nil {
		t.Fatal(err)
	}
	if !e.hub.grantNewPlatformViewServiceInput(context.Background(), "kimi.zzc") {
		t.Fatal("grant failed")
	}
	e.hub.mu.Lock()
	token := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "kimi")].writerToken
	e.hub.mu.Unlock()
	body, _ := json.Marshal(viewInputRequest{ViewerID: viewerID, Token: token, Text: "格十一\r", Mentions: []string{"Beta.zzc", "Gamma.lb"}})
	req := httptest.NewRequest(http.MethodPost, "/internal/view/kimi.zzc/input", bytes.NewReader(body))
	req.SetPathValue("session", "kimi.zzc")
	rr := httptest.NewRecorder()
	e.hub.HandleInternalViewInput(rr, req)
	var r vmResp
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &r) != nil {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	want := []string{
		"Beta.zzc|delivered||已转给 Beta，回复请到 Beta 的页面查看",
		"Gamma.lb|delivered||已转给 Gamma.lb，回复请到 Gamma.lb 的页面查看",
	}
	if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("回执=%q", got)
	}
	e.a.next(t, "格十一")
	if x := e.x.next(t, "格十一"); x.Body != "格十一\r" || metaString(x.Meta, "mentioned_from") != "Kimi.zzc" {
		t.Fatalf("X=%+v", x)
	}
	assertCrossOwnerView(t, e.y.next(t, "格十一"))
}

// 格 13（tosonnet55tc2k 复核必补 1）：对端在线但本次连接尚未发 session_capabilities，而端点登记曾报 ack ⇒
// 与 routeToSession 同判据走持久投递，应投递、不得误判 no_ack。反向 MN2「只看在线会话自报、不查端点登记」⇒ 本格红。
// 断言面＝回执与对端是否收到带 delivery_id 的信封；实现承诺＝viewMentionPeerACK 回落 Repo.SessionEndpointDeliveryACK。
func TestViewMentionRouteFallsBackToRegisteredACK(t *testing.T) {
	e := newVMEnv(t)
	rCred, rID := e.enrollFor("zzc", "dev-r", "Relink")
	g, err := e.svc.Store.ListAgentGroups(e.ctx, e.ids["Kimi"])
	if err != nil || len(g) == 0 {
		t.Fatalf("Kimi 的编组：%v %v", g, err)
	}
	var code string
	for _, c := range g {
		if gr, _ := e.svc.Store.GetGroup(e.ctx, c); gr != nil && gr.ShortName == "testa01" {
			code = c
		}
	}
	if err := e.svc.JoinGroup(e.ctx, "zzc", code, rID); err != nil {
		t.Fatal(err)
	}
	// 第一次连接：报 ack ⇒ 端点登记 delivery_ack_v1=1
	first, st, body := e.dial(rCred, "")
	if first == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "relink")
	if err := writeEnvelope(e.ctx, first, model.Envelope{ID: "caps-r", To: "workpulse#" + AgentKeyID("zzc"),
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "端点登记 ack", func() bool {
		ack, known, err := e.hub.Repo.SessionEndpointDeliveryACK(e.ctx, AgentKeyID("zzc"), "relink")
		return err == nil && known && ack
	})
	_ = first.Close(websocket.StatusNormalClosure, "reconnect")
	waitFor(t, 2*time.Second, "Relink 下线", func() bool { return !e.hub.AgentOnline(rID) })
	// 第二次连接：不发 capabilities（模拟 capabilities 帧还没到）
	second, st, body := e.dial(rCred, "")
	if second == nil {
		t.Fatalf("redial: %d %s", st, body)
	}
	defer second.Close(websocket.StatusNormalClosure, "done")
	frames := vmCollect(second)
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "relink")
	e.hub.mu.Lock()
	c := e.hub.sessionClients[AgentKeyID("zzc")]["relink"]
	e.hub.mu.Unlock()
	if c == nil || e.hub.sessionSupportsDeliveryACK(c) {
		t.Fatal("前提：本次连接在线且尚未自报 ack")
	}
	_, _, r := e.post(t, "13131313131313131313131313131313", "格十三", []string{"Relink.zzc"})
	want := []string{"Relink.zzc|delivered||已转给 Relink，回复请到 Relink 的页面查看"}
	if got := vmReceipts(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("回执=%q", got)
	}
	// 已进持久投递；真 helper 连上后随即发 capabilities，Hub 据此排空队列——照做后应收到带 delivery_id 的那封。
	if err := writeEnvelope(e.ctx, second, model.Envelope{ID: "caps-r2", To: "workpulse#" + AgentKeyID("zzc"),
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	if env := frames.next(t, "格十三"); metaString(env.Meta, "delivery_id") == "" {
		t.Fatalf("应走持久投递（有 delivery_id）：meta=%v", env.Meta)
	}
}
