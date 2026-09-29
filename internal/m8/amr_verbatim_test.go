package m8

// S-MENTION-BESTEFFORT · C「提及原样送达」判据。
// 条目:ITERATION-PLAN-DETAIL-20260915.md 的 S-MENTION-BESTEFFORT(板 tasks/v0.1-B);指标第 ③ 条:
//   「投递给对应方的正文里,提及字样保持原样、不被改写。」
//
// 两处投递点(amr.go 的 IPC 直投与回合 AMR)原先投出 candidate.body —— 即剥掉提及、
// 且连提及前面的文字一起丢掉的那一段。改后投出发送方写下的整段原文。
// 🔵 "原样"的精确含义:原文只经 prepareSessionSelector 既有的整段首尾去空白(所有路由共用,不在本件改);
//    提及字样、提及前后的文字、内部换行与空白都不动。
//
// 🔴 连锁:原样送达后,半角触发符会出现在收件方的入站正文里。
//   · 入站正文本身【不会】在收件方那一轮被再解析 —— turn AMR 只收集收件方自己的 assistant_text。
//   · 但收件方若在自己的输出里【引用】了原文,引用里的提及会被解析成第二跳;
//     多目标时两个收件方互相引用即成来回弹。
//   · AMR 派生信封原先不继承入站的 hop_count ⇒ 每跳都从 0 起算,RouteEnvelope 的 R4 兜底永远够不着 ⇒ 链无上限。
//   ⇒ 处置:派生信封继承触发本轮那封入站的 hop_count / max_hops(照抄 reply_to_origin.go 的写法),
//     链交给既有 R4 兜底。本件只验"带没带、链是否被截住";R4 本身由 TestApplyReplyOriginTermination 守。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type verbatimRouted struct {
	env        model.Envelope
	deliveryID string
}

type verbatimFixture struct {
	hub     *Hub
	keyID   string
	names   map[string]string // 短名 → Hub 登记的规范会话名
	p       *amrProcessor
	routed  []verbatimRouted
	dropped int
}

// 捕获出口与生产顺序一致:先过 applyReplyOriginTermination(RouteEnvelope 的第一行),
// 被兜底的静默丢弃;放行的真正落 session_delivery,下一跳才能像生产一样查回自己的入站。
func newVerbatimFixture(t *testing.T) *verbatimFixture {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-verbatim", Name: "svc-verbatim", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc-verbatim", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "owner", DisplayName: "Owner", FeishuOpenID: "ou_owner", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	// 三席真实连上 websocket:结果通知要写回发送方,假会话没有连接会写空指针。
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	for _, short := range []string{"ant", "bee", "cat"} {
		conn := dialSession(t, ctx, srv.URL, short, key.ID, secret)
		t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "done") })
	}
	// 规范名从 session_endpoints 取、再逐席等上线 —— 照抄 TestAMRRoutesMentionsAnywhereAndHonorsPairedFences;
	// 直接读 sessionClients 在全量并行跑时会撞上"已连上但尚未登记"的竞态(实测第三席偶发缺席)。
	f := &verbatimFixture{hub: hub, keyID: key.ID, names: map[string]string{}}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range endpoints {
		if ep.KeyID == key.ID {
			f.names[shortSessionName(ep.SessionName)] = ep.SessionName
		}
	}
	for _, short := range []string{"ant", "bee", "cat"} {
		if f.names[short] == "" {
			t.Fatalf("前提:%s 未登记;已登记=%v", short, f.names)
		}
		waitSessionOnline(t, hub, key.ID, f.names[short])
	}
	p := testAMRProcessor()
	p.hub = hub
	p.routeTimeout = 5 * time.Second
	p.routeEnvelope = func(ctx context.Context, env model.Envelope) error {
		out, drop := hub.applyReplyOriginTermination(env)
		if drop {
			f.dropped++
			return nil
		}
		dlv := "dw-dlv_" + out.ID
		payload, err := json.Marshal(out)
		if err != nil {
			return err
		}
		name, _, _ := strings.Cut(out.To, "#")
		if err := hub.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{
			ID: dlv, KeyID: key.ID, SessionName: name,
			Envelope: string(payload), SourceMessageID: out.ID, SourceType: "agent_route",
		}); err != nil {
			return err
		}
		f.routed = append(f.routed, verbatimRouted{env: out, deliveryID: dlv})
		return nil
	}
	f.p = p
	return f
}

func (f *verbatimFixture) turn(short, deliveryID, body string) {
	f.p.process(amrRouteJob{key: amrTurnKey{KeyID: f.keyID, Session: f.names[short], DeliveryID: deliveryID, TurnID: "t-" + deliveryID}, body: body})
}

func (f *verbatimFixture) to(name string) []verbatimRouted {
	var out []verbatimRouted
	for _, r := range f.routed {
		if strings.HasPrefix(r.env.To, f.names[name]+"#") {
			out = append(out, r)
		}
	}
	return out
}

// C-1 原样:收件方拿到的是发送方写下的整段原文 —— 提及字样在,提及前面的文字也在。
func TestAMRVerbatimTurnDeliversOriginalText(t *testing.T) {
	f := newVerbatimFixture(t)
	const original = "前言一句,收件方也该看到。\n@bee @cat 请处理这份清单"
	f.turn("ant", "dw-dlv_seed-c1", original)
	for _, name := range []string{"bee", "cat"} {
		got := f.to(name)
		if len(got) != 1 {
			t.Fatalf("C-1:%s 应收到恰好 1 封,实得 %d 封;全部=%+v", name, len(got), f.routed)
		}
		if got[0].env.Body != original {
			t.Errorf("C-1 原样:%s 收到的正文被改写了\n 实得=%q\n 应为=%q", name, got[0].env.Body, original)
		}
	}
}

// C-2 连锁对照:收件方那一轮不引用原文 ⇒ 入站正文本身不会被再解析,不产生第二跳。
func TestAMRVerbatimInboundAloneIsNotReparsed(t *testing.T) {
	f := newVerbatimFixture(t)
	f.turn("ant", "dw-dlv_seed-c2", "@bee @cat 请处理")
	bee := f.to("bee")
	if len(bee) != 1 {
		t.Fatalf("前提:bee 应收到 1 封,实得 %d", len(bee))
	}
	before := len(f.to("cat"))
	f.turn("bee", bee[0].deliveryID, "好的,已处理完毕。")
	if after := len(f.to("cat")); after != before {
		t.Errorf("C-2:bee 这一轮没有引用原文,却给 cat 多投了 %d 封 —— 入站正文被再解析了", after-before)
	}
}

// C-3 连锁:收件方引用原文 ⇒ 第二跳确实会发生(这是实测结论,不是缺陷);
// 但链必须被 R4 截住 —— 派生信封要继承入站的 hop_count,否则每跳从 0 起算、链无上限。
func TestAMRVerbatimQuotedMentionChainIsBoundedByHopBackstop(t *testing.T) {
	f := newVerbatimFixture(t)
	f.turn("ant", "dw-dlv_seed-c3", "@bee @cat 请处理")
	if len(f.to("bee")) != 1 || len(f.to("cat")) != 1 {
		t.Fatalf("前提:首跳应各投 1 封,bee=%d cat=%d", len(f.to("bee")), len(f.to("cat")))
	}
	// 最坏情形:bee、cat 每一轮都把收到的正文原样引用一遍 ⇒ 互相投递。
	const guard = 3 * defaultMaxHops
	last := map[string]verbatimRouted{"bee": f.to("bee")[0], "cat": f.to("cat")[0]}
	speaker := "bee"
	hops := 0
	secondHopSeen := false
	for hops < guard {
		in := last[speaker]
		n := len(f.routed)
		f.turn(speaker, in.deliveryID, "收到,原文如下:"+in.env.Body)
		if len(f.routed) == n {
			break // 被兜底丢弃,或没有新投递
		}
		hops++
		newest := f.routed[len(f.routed)-1]
		other := "cat"
		if speaker == "cat" {
			other = "bee"
		}
		if !strings.HasPrefix(newest.env.To, f.names[other]+"#") {
			// 引用里没有可解析的提及(例如收到的正文已被剥掉提及)⇒ 走回程,不是第二跳
			break
		}
		if hops == 1 {
			secondHopSeen = true
			if got := hopCountOf(newest.env); got < 2 {
				t.Errorf("C-3 继承:第二跳的 hop_count 应沿链累加(≥2),实得 %d —— 派生信封没有继承入站的 hop_count", got)
			}
		}
		last[other] = newest
		speaker = other
	}
	if !secondHopSeen {
		t.Fatalf("前提:引用原文时应出现第二跳(本件的实测结论之一),实际没有;最后一封去向=%q 正文=%q",
			f.routed[len(f.routed)-1].env.To, f.routed[len(f.routed)-1].env.Body)
	}
	if hops >= guard {
		t.Errorf("C-3 连锁无上限:两席互相引用跑满 %d 跳仍未被 R4 兜底截住(dropped=%d)", guard, f.dropped)
	}
	if f.dropped != 1 {
		t.Errorf("C-3:链应恰好被 R4 静默丢弃 1 次后停下,实得 dropped=%d hops=%d", f.dropped, hops)
	}
	// 首跳进 bee/cat 时 hop=1;此后每跳继承入站值,经 RouteEnvelope 再 +1:继承 1 投出 2 …… 继承 15 投出 16;
	// 继承到 16 的那一封满足 hop_count>=max_hops(16)被丢 ⇒ 第二跳起共放行 15 跳。
	if want := defaultMaxHops - 1; hops != want {
		t.Errorf("C-3:互相引用应恰好放行 %d 跳后被截住,实得 %d 跳", want, hops)
	}
}

// C-4 连锁·分叉:收件方引用 "@bee @cat …" 时,原文里的短名 bee 就是它自己 ——
// 解析后指向本席的目标必须跳过,否则引用一次就分叉成"自己＋对方"两封(实测:改前每跳 2 封、hop 恒为 1)。
func TestAMRVerbatimQuotedSelfMentionIsNotRoutedToSelf(t *testing.T) {
	f := newVerbatimFixture(t)
	f.turn("ant", "dw-dlv_seed-c4", "@bee @cat 请处理")
	in := f.to("bee")[0]
	n := len(f.routed)
	f.turn("bee", in.deliveryID, "收到,原文如下:"+in.env.Body)
	var toSelf, toCat int
	for _, r := range f.routed[n:] {
		switch {
		case strings.HasPrefix(r.env.To, f.names["bee"]+"#"):
			toSelf++
		case strings.HasPrefix(r.env.To, f.names["cat"]+"#"):
			toCat++
		}
	}
	if toSelf != 0 {
		t.Errorf("C-4 分叉:bee 引用原文后给自己投了 %d 封(原文里的短名 bee 没被认成本席)", toSelf)
	}
	if toCat != 1 {
		t.Errorf("C-4:bee 引用原文后给 cat 应投恰好 1 封,实得 %d", toCat)
	}
}

// C-5 连锁·不吞:引用里的提及只有本席自己 ⇒ 不是投递指令,这一轮的输出要按普通回程回给来源,不能被当"候选全拒"吞掉。
func TestAMRVerbatimSelfOnlyQuoteStillReturnsToOrigin(t *testing.T) {
	f := newVerbatimFixture(t)
	f.turn("ant", "dw-dlv_seed-c5", "@bee 请处理")
	in := f.to("bee")
	if len(in) != 1 {
		t.Fatalf("前提:bee 应收到 1 封,实得 %d", len(in))
	}
	n := len(f.routed)
	f.turn("bee", in[0].deliveryID, "收到,原文如下:"+in[0].env.Body+"\n已处理完毕。")
	var toSelf, toAnt int
	for _, r := range f.routed[n:] {
		switch {
		case strings.HasPrefix(r.env.To, f.names["bee"]+"#"):
			toSelf++
		case strings.HasPrefix(r.env.To, f.names["ant"]+"#") && kindOf(r.env) == kindReply:
			toAnt++
		}
	}
	if toSelf != 0 {
		t.Errorf("C-5:只提及自己的引用给自己投了 %d 封", toSelf)
	}
	if toAnt != 1 {
		t.Errorf("C-5 不吞:只提及自己的那一轮应回程给 ant 恰好 1 封,实得 %d —— 回复被吞了;本轮全部=%+v", toAnt, f.routed[n:])
	}
}
