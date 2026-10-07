package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-0103-MENTION-REPLY-MIRROR（要点板 runbooks/PLAN-R-V2-0103-MENTION-REPLY-MIRROR-opus50tc2k.md，以第五、六节为准）。
// 断言面＝A（Kimi.zzc）的 view 事件流（eventRing）、A 的 helper 连接是否收到信封、A 的投递队列；
// 实现承诺＝view_mention_reply.go 的识别（delivery_id→入站 route_source）、一轮累计、回合结束一次性发、同 owner 复核。

// aReplies A 页事件流里的 mention_reply（按到达顺序）。
func (e *vmEnv) aReplies(t *testing.T) []map[string]any {
	t.Helper()
	e.hub.mu.Lock()
	ring := append([]string(nil), e.hub.terminalForSession(AgentKeyID("zzc"), "kimi").eventRing...)
	e.hub.mu.Unlock()
	var out []map[string]any
	for _, raw := range ring {
		var ev map[string]any
		if json.Unmarshal([]byte(raw), &ev) == nil && ev["kind"] == "mention_reply" {
			out = append(out, ev)
		}
	}
	return out
}

func (e *vmEnv) waitReplies(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := e.aReplies(t)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("A 页 3s 内未见第 %d 条 mention_reply（现 %d 条）", n, len(got))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// emit 以 X 的 helper 身份发一条 session_event（与真 helper 同一条 WS 路）。
func emitTurnEvent(t *testing.T, ctx context.Context, conn *websocket.Conn, ev map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(ev)
	if err := writeEnvelope(ctx, conn, model.Envelope{ID: "ev-" + randomHex(6), To: "workpulse#" + AgentKeyID("zzc"), Body: string(raw),
		Meta: map[string]any{"type": terminalSessionEventType}}); err != nil {
		t.Fatal(err)
	}
}

func textEv(dlv, turn, text string, extra map[string]any) map[string]any {
	ev := map[string]any{"kind": "assistant_text", "text": text, "delivery_id": dlv, "turn_id": turn, "data": map[string]any{}}
	for k, v := range extra {
		ev["data"].(map[string]any)[k] = v
	}
	return ev
}

func doneEv(dlv, turn string, data map[string]any) map[string]any {
	d := map[string]any{"change": "turn_completed", "subtype": "success"}
	for k, v := range data {
		d[k] = v
	}
	return map[string]any{"kind": "state_change", "delivery_id": dlv, "turn_id": turn, "data": d}
}

// mention 发一条提及 Beta 的 view 输入，返回 Beta 收到的派生信封的 delivery_id。
func (e *vmEnv) mentionBeta(t *testing.T, rid, text string) string {
	t.Helper()
	if _, _, r := e.post(t, rid, text, []string{"Beta.zzc"}); len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
		t.Fatalf("前提：提及应投给 Beta：%q", vmReceipts(r))
	}
	e.a.next(t, text)
	dlv := metaString(e.x.next(t, text).Meta, "delivery_id")
	if dlv == "" {
		t.Fatal("前提：Beta 应经持久投递收到（有 delivery_id）")
	}
	return dlv
}

func noTurnKeys(t *testing.T, ev map[string]any) {
	t.Helper()
	data, _ := ev["data"].(map[string]any)
	for _, k := range []string{"turn_id", "delivery_id"} {
		if _, has := ev[k]; has {
			t.Fatalf("回流事件顶层不得带 %s（会并进 A 自己的回合正文）：%v", k, ev)
		}
		if _, has := data[k]; has {
			t.Fatalf("回流事件 data 不得带 %s：%v", k, ev)
		}
	}
}

func TestMentionReplyMirror(t *testing.T) {
	e := newVMEnv(t)
	ctx := e.ctx

	// 格 1：Beta 回该轮（快照式两段）⇒ A 页一条 mention_reply；正文不重复；来源标明；不带 turn_id／delivery_id。
	t.Run("the mentioned agent's reply to that turn shows on the sender's page", func(t *testing.T) {
		dlv := e.mentionBeta(t, "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1", "回流一")
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t1", "第一段", nil))
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t1", "第一段第二段", nil)) // 快照
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t1", "第一段第二段", nil)) // 重复
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t1", nil))
		got := e.waitReplies(t, 1)
		ev := got[0]
		data, _ := ev["data"].(map[string]any)
		if ev["text"] != "第一段第二段" || data["from_full_name"] != "Beta.zzc" || data["from_label"] != "Beta" || data["status"] != "ok" || data["source_id"] != dlv {
			t.Fatalf("回流事件=%v", ev)
		}
		noTurnKeys(t, ev)
		if !strings.HasPrefix(fmt.Sprint(ev["message_id"]), "mreply-"+dlv+"-") {
			t.Fatalf("message_id=%v", ev["message_id"])
		}
	})

	// 格 2：追加式喂法同样不重复。
	t.Run("appended fragments are joined once", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2", "回流二")
		for _, s := range []string{"甲", "乙", "丙"} {
			emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t2", s, nil))
		}
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t2", nil))
		if got := e.waitReplies(t, before+1); got[before]["text"] != "甲乙丙" {
			t.Fatalf("追加式正文=%v", got[before]["text"])
		}
	})

	// 格 3：回流不进 A 的投递——A 的 helper 连接收不到任何信封；A 的队列没有新行。
	t.Run("the reply never enters A's delivery path", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3", "回流三")
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t3", "只给人看", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t3", nil))
		e.waitReplies(t, before+1)
		e.a.none(t, "", 400*time.Millisecond) // 任何信封都不得到 A 的 helper
		if pendingDeliveryFor(t, e, AgentKeyID("zzc"), "kimi") {
			t.Fatal("A 的投递队列不得有回流产生的行")
		}
	})

	// 格 4：只认 view_mention 派生信封触发的那一轮——入站带 mentioned_from 却没有 route_source 的投递不回流；
	// X 自己的事件流照常。
	t.Run("only a turn triggered by a view-mention envelope is mirrored", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := seedDeliveryToBeta(t, e, map[string]any{"mentioned_from": "Kimi.zzc"})
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t4", "别处来的一轮", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t4", nil))
		time.Sleep(300 * time.Millisecond)
		if got := len(e.aReplies(t)); got != before {
			t.Fatalf("非 view_mention 那一轮不得回流（%d→%d）", before, got)
		}
		e.hub.mu.Lock()
		xRing := strings.Join(e.hub.terminalForSession(AgentKeyID("zzc"), "beta").eventRing, "\n")
		e.hub.mu.Unlock()
		if !strings.Contains(xRing, "别处来的一轮") {
			t.Fatal("X 自己的事件流应照常")
		}
	})

	// 格 5：回放事件（data.replay=true）不看——重连回放同一轮不出第二张卡。
	t.Run("replayed events do not mirror twice", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5", "回流五")
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t5", "一次", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t5", nil))
		e.waitReplies(t, before+1)
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t5", "一次", map[string]any{"replay": true}))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t5", map[string]any{"replay": true}))
		time.Sleep(300 * time.Millisecond)
		if got := len(e.aReplies(t)); got != before+1 {
			t.Fatalf("回放不得再出卡（%d 条）", got-before)
		}
	})

	// 格 6：delivery_id 为空的回合事件不跟踪（与 AMR ingest 同口径；不报 ack 的对端本就收不到派生信封）。
	t.Run("events without a delivery id are not tracked", func(t *testing.T) {
		before := len(e.aReplies(t))
		emitTurnEvent(t, ctx, e.xConn, textEv("", "t6", "无号", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv("", "t6", nil))
		time.Sleep(300 * time.Millisecond)
		if got := len(e.aReplies(t)); got != before {
			t.Fatal("无 delivery_id 的回合不得回流")
		}
	})

	// 格 7：失败轮 ⇒ 只发一行状态，不带正文。
	t.Run("a failed turn shows one status line", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7", "回流七")
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t7", "半截正文", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t7", map[string]any{"is_error": true}))
		ev := e.waitReplies(t, before+1)[before]
		data, _ := ev["data"].(map[string]any)
		text := fmt.Sprint(ev["text"])
		if data["status"] != "failed" || !strings.HasPrefix(text, "Beta 这一轮未成功：") || strings.Contains(text, "半截正文") {
			t.Fatalf("失败状态行=%v", ev)
		}
	})

	// 格 8：派生信封终未处理（不可重试的 failed ⇒ dead）⇒ A 页一行「未能处理这条转发」。
	t.Run("a dead mention delivery shows a status line", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8", "回流八")
		if err := writeEnvelope(ctx, e.xConn, model.Envelope{ID: "ack-fail-" + dlv, To: "workpulse#" + AgentKeyID("zzc"),
			Meta: map[string]any{"type": "delivery_ack", "delivery_id": dlv, "state": "failed", "reason": "cli_unavailable", "retryable": false}}); err != nil {
			t.Fatal(err)
		}
		ev := e.waitReplies(t, before+1)[before]
		if ev["text"] != "Beta 未能处理这条转发：cli_unavailable" {
			t.Fatalf("dead 状态行=%v", ev["text"])
		}
	})

	// 格 9：owner 不同（入站 mentioned_from 指向他人 Agent）⇒ 不回流。
	t.Run("a different owner is never mirrored to", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := seedDeliveryToBeta(t, e, map[string]any{"route_source": "view_mention", "mentioned_from": "Gamma.lb"})
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t9", "不该出现", nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t9", nil))
		time.Sleep(300 * time.Millisecond)
		e.hub.mu.Lock()
		gammaRing := strings.Join(e.hub.terminalForSession(AgentKeyID("lbl"), "gamma").eventRing, "\n")
		e.hub.mu.Unlock()
		if strings.Contains(gammaRing, "mention_reply") || len(e.aReplies(t)) != before {
			t.Fatal("跨 owner 不得回流")
		}
	})

	// 格 10：超长截断——按 UTF-8 字节、不切坏中文、末尾逐字说明；含非法字节的输入不吞成空卡。
	t.Run("long replies are cut at 16 KiB on a character boundary with the ruled note", func(t *testing.T) {
		before := len(e.aReplies(t))
		dlv := e.mentionBeta(t, "a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0", "回流十")
		emitTurnEvent(t, ctx, e.xConn, textEv(dlv, "t10", "\xff\xfe"+strings.Repeat("中文回复", 2000), nil))
		emitTurnEvent(t, ctx, e.xConn, doneEv(dlv, "t10", nil))
		text := fmt.Sprint(e.waitReplies(t, before+1)[before]["text"])
		note := "（内容较长，完整回复请到 Beta 的页面查看）"
		if !utf8.ValidString(text) || len(text) > mentionReplyMaxBytes || !strings.HasSuffix(text, note) || !strings.Contains(text, "中文回复中文回复") {
			t.Fatalf("截断：valid=%v len=%d suffix=%v", utf8.ValidString(text), len(text), strings.HasSuffix(text, note))
		}
	})

	// 格 11：X 离线时被提及（持久排队），上线后处理那一轮 ⇒ 照样回流。
	t.Run("a reply after the target comes back online is still mirrored", func(t *testing.T) {
		before := len(e.aReplies(t))
		_ = e.qConn.Close(websocket.StatusNormalClosure, "offline")
		waitFor(t, 2*time.Second, "Quill 下线", func() bool { return !e.hub.AgentOnline(e.ids["Quill"]) })
		if _, _, r := e.post(t, "a1b1a1b1a1b1a1b1a1b1a1b1a1b1a1b1", "回流十一", []string{"Quill.zzc"}); len(r.Mentions) != 1 || r.Mentions[0].Status != "queued" {
			t.Fatalf("前提：应排队：%q", vmReceipts(r))
		}
		e.a.next(t, "回流十一")
		conn, st, body := e.dial(e.qCred, "")
		if conn == nil {
			t.Fatalf("redial: %d %s", st, body)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		frames := vmCollect(conn)
		if err := writeEnvelope(ctx, conn, model.Envelope{ID: "caps-q3", To: "workpulse#" + AgentKeyID("zzc"),
			Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
			t.Fatal(err)
		}
		dlv := metaString(frames.next(t, "回流十一").Meta, "delivery_id")
		emitTurnEvent(t, ctx, conn, textEv(dlv, "t11", "迟到的回复", nil))
		emitTurnEvent(t, ctx, conn, doneEv(dlv, "t11", nil))
		ev := e.waitReplies(t, before+1)[before]
		if ev["text"] != "迟到的回复" || ev["data"].(map[string]any)["from_label"] != "Quill" {
			t.Fatalf("离线后回流=%v", ev)
		}
	})
}

// pendingDeliveryFor A 会话名下有无可领取的持久投递（证明回流没进 A 的投递队列）。
func pendingDeliveryFor(t *testing.T, e *vmEnv, keyID, session string) bool {
	t.Helper()
	d, err := e.hub.Repo.ClaimSessionDelivery(e.ctx, keyID, session, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return d != nil
}

// seedDeliveryToBeta 直接落一条投给 Beta 的入站（meta 由用例给），返回 delivery_id——用来造「不是 view_mention」或「跨 owner」的入站。
func seedDeliveryToBeta(t *testing.T, e *vmEnv, meta map[string]any) string {
	t.Helper()
	dlv := deliveryIDPrefix + randomHex(16)
	// 照 routeToSession 持久投递落库的形状带上 type／delivery_id／ack_required：否则 Hub 排空到 Beta 时
	// 测试收信端无从回执，该行占着租约，会把后面的投递堵住（不是被测行为）。
	full := map[string]any{"type": "delivery", "delivery_id": dlv, "delivery_version": 1, "ack_required": true}
	for k, v := range meta {
		full[k] = v
	}
	env := model.Envelope{ID: "seed-" + randomHex(8), To: sessionAddress("beta", AgentKeyID("zzc")), From: sessionAddress("workpulse", AgentKeyID("zzc")), Body: "seed", TS: time.Now().Unix(), Meta: full}
	payload, _ := json.Marshal(env)
	if err := e.hub.Repo.EnqueueSessionDelivery(e.ctx, model.SessionDelivery{ID: dlv, KeyID: AgentKeyID("zzc"), SessionName: "beta",
		Envelope: string(payload), SourceMessageID: env.ID, SourceType: "view_inbound"}); err != nil {
		t.Fatal(err)
	}
	return dlv
}

// 纯函数五态（AMR 与回流共用）：首段／重复／快照／追加／超限截头。
func TestAccumulateTurnText(t *testing.T) {
	type step struct {
		frag      string
		wantText  string
		wantKind  turnFragmentKind
		wantTrunc bool
	}
	cases := []struct {
		name  string
		limit int
		steps []step
	}{
		{"first then duplicate then snapshot then append", 100, []step{
			{"ab", "ab", turnFragmentFirst, false},
			{"ab", "ab", turnFragmentDuplicate, false},
			{"abcd", "abcd", turnFragmentSnapshot, false},
			{"ef", "abcdef", turnFragmentAppend, false},
		}},
		{"head is kept when over the limit", 4, []step{
			{"abc", "abc", turnFragmentFirst, false},
			{"defg", "abcd", turnFragmentAppend, true},
		}},
		{"multibyte head is cut on a rune boundary", 4, []step{
			{"中文", "中", turnFragmentFirst, true},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, observed, trunc := "", 0, false
			for i, s := range c.steps {
				var kind turnFragmentKind
				text, observed, trunc, kind = accumulateTurnText(text, observed, s.frag, c.limit, trunc)
				if text != s.wantText || kind != s.wantKind || trunc != s.wantTrunc {
					t.Fatalf("step %d frag=%q ⇒ text=%q kind=%d trunc=%v，want %q %d %v", i, s.frag, text, kind, trunc, s.wantText, s.wantKind, s.wantTrunc)
				}
			}
			_ = observed
		})
	}
}

// 有界：入站查询 LRU 上限与在途回合 30 分钟过期（注入时钟）。
func TestMentionMirrorBounds(t *testing.T) {
	m := newMentionMirror(nil)
	for i := 0; i < mentionReplyLookupCap+50; i++ {
		m.remember(fmt.Sprintf("d%d", i), mentionReplySource{})
	}
	if len(m.lookups) != mentionReplyLookupCap || m.order.Len() != mentionReplyLookupCap {
		t.Fatalf("LRU 应封顶 %d：map=%d list=%d", mentionReplyLookupCap, len(m.lookups), m.order.Len())
	}
	if _, kept := m.lookups["d0"]; kept {
		t.Fatal("最早的应先被淘汰")
	}
	now := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	m.turns[amrTurnKey{DeliveryID: "old"}] = &mentionReplyTurn{started: now}
	m.turns[amrTurnKey{DeliveryID: "new"}] = &mentionReplyTurn{started: now.Add(20 * time.Minute)}
	m.expireLocked(now.Add(31 * time.Minute))
	if _, old := m.turns[amrTurnKey{DeliveryID: "old"}]; old || len(m.turns) != 1 {
		t.Fatalf("30 分钟未结束的应过期：%v", m.turns)
	}
	for i := 0; i < mentionReplyMaxInflight; i++ {
		m.turns[amrTurnKey{DeliveryID: fmt.Sprintf("x%d", i)}] = &mentionReplyTurn{started: now.Add(time.Duration(i+30) * time.Minute)}
	}
	m.evictOldestLocked()
	if _, gone := m.turns[amrTurnKey{DeliveryID: "new"}]; gone {
		t.Fatal("在途满时应先淘汰最早开始的那一轮")
	}
}
