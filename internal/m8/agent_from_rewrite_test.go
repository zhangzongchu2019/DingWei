package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-HUB-FROM-REWRITE（组长裁定·甲）：入网 helper 的出站信封 from 是「会话名#key」，而它的 key 只是占位
// （helper 侧 SH_KEY_ID 缺省 "enrolled"；真身份是 Hub 按凭据查出的 u_属主）。Hub 须把「会话名部分等于本连接会话名」的 from
// 改写成真地址 sessionAddress(会话名, u_属主) 再往下走；会话名不同仍拒（不得冒用同属主下另一会话）。
// 这些用例走真 /ws/agent 握手（专属凭据），信封里的 from 故意写成占位 key。

const placeholderFromKey = "enrolled" // 与 tools/sessionhelper enroll.py 的占位一致

// enrollInGroup 与 enroll 同，但批准时加入【已有的组】（Agent 间发信要求同组，否则 cross_group_denied）。
func (e *agentEnv) enrollInGroup(device, name, groupCode string) (cred string) {
	e.t.Helper()
	digest, _ := identity.PairingDigest("000102030405060708090a0b0c0d0e0f", "123456")
	r, err := e.svc.Apply(e.ctx, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: "000102030405060708090a0b0c0d0e0f",
		PairingDigest: digest, DeviceFingerprint: device, ClientVersion: "0.3.0", SourceIP: "127.0.0.1"})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.svc.Approve(e.ctx, "zzc", r.ApplicationID, "123456", name, groupCode); err != nil {
		e.t.Fatal(err)
	}
	p, err := e.svc.Poll(e.ctx, r.ApplicationID, r.PollToken)
	if err != nil || p.Credential == "" {
		e.t.Fatalf("poll: %+v %v", p, err)
	}
	return p.Credential
}

func (e *agentEnv) sendEnv(conn *websocket.Conn, env model.Envelope) {
	e.t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := conn.Write(e.ctx, websocket.MessageText, b); err != nil {
		e.t.Fatalf("write envelope: %v", err)
	}
}

// readEnvUntil 读到满足条件的信封为止（跳过握手后的技能推送等无关帧），超时返回 nil。
func (e *agentEnv) readEnvUntil(conn *websocket.Conn, within time.Duration, match func(model.Envelope) bool) *model.Envelope {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, within)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return nil
		}
		var env model.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if match(env) {
			return &env
		}
	}
}

// ① 占位 key 的 terminal_output 必须进该会话的终端缓冲。
func TestAgentWSPlaceholderFromTerminalOutputReachesBuffer(t *testing.T) {
	e := newAgentEnv(t)
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	go readSessionControls(conn)
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{
		From: "opus50ts#" + placeholderFromKey, Body: "terminal-line-from-helper",
		Meta: map[string]any{"type": terminalOutputType},
	})
	waitFor(t, envelopeReadTimeout, "占位 key 的 terminal_output 进入会话终端缓冲", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		st := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]
		return st != nil && strings.Contains(st.buffer, "terminal-line-from-helper")
	})
}

// ② Agent 间发信：占位 key 的信封能投到同属主的另一个 Agent，且收方看到的 from 是改写后的真地址。
func TestAgentWSPlaceholderFromAgentToAgentIsDelivered(t *testing.T) {
	e := newAgentEnv(t)
	codes, err := e.svc.Store.ListAgentGroups(e.ctx, e.agID)
	if err != nil || len(codes) == 0 {
		t.Fatalf("opus50ts 应在自己建的组里: %v %v", codes, err)
	}
	credB := e.enrollInGroup("dev-2", "Kimi", codes[0])
	a, _, _ := e.dial(e.cred, "")
	defer a.CloseNow()
	go readSessionControls(a)
	b, _, _ := e.dial(credB, "")
	defer b.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "kimi")
	e.sendEnv(a, model.Envelope{From: "opus50ts#" + placeholderFromKey, To: sessionAddress("kimi", AgentKeyID("zzc")), Body: "ping-from-opus50ts"})
	got := e.readEnvUntil(b, envelopeReadTimeout, func(env model.Envelope) bool { return env.Body == "ping-from-opus50ts" })
	if got == nil {
		t.Fatal("占位 key 的 Agent 间信件没有投到对方（from 被当成地址不符而丢弃）")
	}
	if want := sessionAddress("opus50ts", AgentKeyID("zzc")); got.From != want {
		t.Fatalf("收方看到的 from=%q，要求改写后的真地址 %q", got.From, want)
	}
}

// ③ 回飞书回复：占位 key 的信封能入出站队列（用户在飞书里收到）。
func TestAgentWSPlaceholderFromFeishuReplyIsDelivered(t *testing.T) {
	e := newAgentEnv(t)
	e.hub.Outbound = bus.NewDBQueue(e.hub.Repo, model.DirectionOut)
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	go readSessionControls(conn)
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{
		From: "opus50ts#" + placeholderFromKey, To: feishuAddress("ou_alice", AgentKeyID("zzc"), "unifiedrobot"),
		Body: "reply-from-agent-to-feishu",
	})
	out := waitOutbound(t, e.ctx, e.hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "reply-from-agent-to-feishu") {
		t.Fatalf("占位 key 的回飞书回复没有进出站队列: %+v", out)
	}
}

// ①′ session_event（结构化会话事件）同样：占位 key 的事件进该会话的事件环。
func TestAgentWSPlaceholderFromSessionEventReachesRing(t *testing.T) {
	e := newAgentEnv(t)
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	go readSessionControls(conn)
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{
		From: "opus50ts#" + placeholderFromKey, Body: `{"kind":"probe-event-from-helper"}`,
		Meta: map[string]any{"type": terminalSessionEventType},
	})
	waitFor(t, envelopeReadTimeout, "占位 key 的 session_event 进入会话事件环", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		st := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]
		return st != nil && len(st.eventRing) == 1 && strings.Contains(st.eventRing[0], "probe-event-from-helper")
	})
}

// ④ 保持拒绝：伪造同属主另一会话的名字（或空名、裸名）仍被拒——身份由凭据定，不由信封自报定。
// 每条伪造信封都必须收到「from 地址必须等于当前会话地址」的错误信封，且不进任何终端缓冲。
func TestAgentWSForgedOtherSessionFromStillRejected(t *testing.T) {
	e := newAgentEnv(t)
	e.enroll("dev-2", "Kimi") // 同属主的另一个 Agent：名字存在、在库里
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	for _, forged := range []string{
		"kimi#" + placeholderFromKey,              // 另一会话名＋占位 key
		sessionAddress("kimi", AgentKeyID("zzc")), // 另一会话的真地址
		"#" + placeholderFromKey,                  // 空会话名
		"opus50ts",                                // 裸会话名（无 #）
	} {
		e.sendEnv(conn, model.Envelope{ID: "forged-" + forged, From: forged, Body: "forged-terminal-line", Meta: map[string]any{"type": terminalOutputType}})
		got := e.readEnvUntil(conn, envelopeReadTimeout, func(env model.Envelope) bool {
			return strings.Contains(env.Body, "from 地址必须等于当前会话地址")
		})
		if got == nil {
			t.Fatalf("伪造的 from=%q 没有被拒（没有收到「from 地址必须等于当前会话地址」错误信封）", forged)
		}
	}
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	for _, name := range []string{"opus50ts", "kimi"} {
		if st := e.hub.terminals[terminalKey(AgentKeyID("zzc"), name)]; st != nil && strings.Contains(st.buffer, "forged-terminal-line") {
			t.Fatalf("伪造 from 的 terminal_output 进了 %s 的终端缓冲", name)
		}
	}
}

// ④′ 近似名反例（复验 tosonnet55tc2k 对 b4812876 的建议，非阻断）：会话名只许【逐字相等（不分大小写）】才改写。
// 把比较换成前缀／后缀／包含之类的宽松判断时，下面四个近似名里至少有一个会被放行：
//
//	opus50ts2  ＝「自身名＋多一位」（自身名是它的前缀）；xopus50ts ＝「前面多一位」（自身名是它的后缀）；
//	pus50ts    ＝「自身名去首字母」（它是自身名的后缀）；opus50t ＝「自身名去末字母」（它是自身名的前缀）。
//
// 每个都必须收到「from 地址必须等于当前会话地址」，且不进任何终端缓冲。
func TestAgentWSNearNameFromStillRejected(t *testing.T) {
	e := newAgentEnv(t)
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	for _, forged := range []string{
		"opus50ts2#" + placeholderFromKey,
		"xopus50ts#" + placeholderFromKey,
		"pus50ts#" + placeholderFromKey,
		"opus50t#" + placeholderFromKey,
	} {
		e.sendEnv(conn, model.Envelope{ID: "near-" + forged, From: forged, Body: "near-name-terminal-line", Meta: map[string]any{"type": terminalOutputType}})
		got := e.readEnvUntil(conn, envelopeReadTimeout, func(env model.Envelope) bool {
			return strings.Contains(env.Body, "from 地址必须等于当前会话地址")
		})
		if got == nil {
			t.Fatalf("近似名 from=%q 没有被拒（会话名比较被放宽了：只有逐字相等才该改写）", forged)
		}
	}
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	if st := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]; st != nil && strings.Contains(st.buffer, "near-name-terminal-line") {
		t.Fatal("近似名 from 的 terminal_output 进了终端缓冲")
	}
}

// ⑤ 旧平台连接（账号 key＋自报名字，HandleSessionWS）不改写：from 的 key 部分写错照旧被拒。
// 凭据定身份的改写只属于 /ws/agent；账号 key 连接的身份仍由「自报名字＋key」组成，信封里的 from 必须逐字等于它。
func TestLegacySessionWSWrongKeyFromStillRejected(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
	defer conn.CloseNow()
	waitSessionOnline(t, hub, key.ID, "developer")
	b, _ := json.Marshal(model.Envelope{ID: "legacy-forged", From: "developer#" + placeholderFromKey, Body: "legacy-terminal-line", Meta: map[string]any{"type": terminalOutputType}})
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithTimeout(ctx, envelopeReadTimeout)
	defer cancel()
	for {
		_, data, err := conn.Read(rctx)
		if err != nil {
			t.Fatal("旧平台连接里 key 部分写错的 from 没有被拒（没有收到「from 地址必须等于当前会话地址」）")
		}
		var env model.Envelope
		if json.Unmarshal(data, &env) == nil && strings.Contains(env.Body, "from 地址必须等于当前会话地址") {
			break
		}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if st := hub.terminals[terminalKey(key.ID, "developer")]; st != nil && strings.Contains(st.buffer, "legacy-terminal-line") {
		t.Fatal("被拒的 from 的 terminal_output 进了终端缓冲")
	}
}
