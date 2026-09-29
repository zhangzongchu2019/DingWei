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
	conns   map[string]*websocket.Conn
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
	conns := map[string]*websocket.Conn{}
	for _, short := range []string{"ant", "bee", "cat"} {
		conn := dialSession(t, ctx, srv.URL, short, key.ID, secret)
		t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "done") })
		conns[short] = conn
	}
	// 规范名从 session_endpoints 取、再逐席等上线 —— 照抄 TestAMRRoutesMentionsAnywhereAndHonorsPairedFences;
	// 直接读 sessionClients 在全量并行跑时会撞上"已连上但尚未登记"的竞态(实测第三席偶发缺席)。
	f := &verbatimFixture{hub: hub, keyID: key.ID, names: map[string]string{}, conns: conns}
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

// seed 直接落一条"投给 to 的入站"并返回它的 delivery_id —— 用来造【不带】原样送达标记的入站
// (例如发送方显式 send.py 发来的信),以便在标记之外单独测自身跳过与跳数继承。
func (f *verbatimFixture) seed(t *testing.T, to, from, body string, meta map[string]any) string {
	t.Helper()
	env := model.Envelope{ID: "seed-" + randomHex(8), To: sessionAddress(f.names[to], f.keyID), From: sessionAddress(f.names[from], f.keyID), Body: body, TS: time.Now().Unix(), Meta: meta}
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	dlv := deliveryIDPrefix + randomHex(16)
	if err := f.hub.Repo.EnqueueSessionDelivery(context.Background(), model.SessionDelivery{
		ID: dlv, KeyID: f.keyID, SessionName: f.names[to], Envelope: string(payload), SourceMessageID: env.ID, SourceType: "agent_route",
	}); err != nil {
		t.Fatal(err)
	}
	return dlv
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

// C-3 两条上限并存:原样送达的派生信封带 mention_verbatim 标记 ⇒ 收件方引用原文的那一轮不再路由、只回程。
// 这是一跳封顶,比 R4(16 跳)紧;两条都在时,链恰好停在更紧的这一条上。
func TestAMRVerbatimQuotedChainStopsAfterFirstHop(t *testing.T) {
	f := newVerbatimFixture(t)
	f.turn("ant", "dw-dlv_seed-c3", "@bee @cat 请处理")
	bee, cat := f.to("bee"), f.to("cat")
	if len(bee) != 1 || len(cat) != 1 {
		t.Fatalf("前提:首跳应各投 1 封,bee=%d cat=%d", len(bee), len(cat))
	}
	if !metaBool(bee[0].env.Meta, metaKeyMentionVerbatim) {
		t.Fatalf("前提:原样送达的派生信封应带 %s 标记,meta=%v", metaKeyMentionVerbatim, bee[0].env.Meta)
	}
	if strings.Contains(bee[0].env.Body, metaKeyMentionVerbatim) {
		t.Errorf("C-3:标记不得出现在正文里(否则它自己就成了可被引用的东西),正文=%q", bee[0].env.Body)
	}
	n := len(f.routed)
	f.turn("bee", bee[0].deliveryID, "收到,原文如下:"+bee[0].env.Body)
	var second, replies int
	for _, r := range f.routed[n:] {
		switch {
		case kindOf(r.env) == kindReply && strings.HasPrefix(r.env.To, f.names["ant"]+"#"):
			replies++
		default:
			second++
		}
	}
	if second != 0 {
		t.Errorf("C-3 一跳封顶:带标记的入站触发的那一轮仍按提及路由了 %d 封(应为 0);全部=%+v", second, f.routed[n:])
	}
	if replies != 1 {
		t.Errorf("C-3:该轮应回程给 ant 恰好 1 封,实得 %d", replies)
	}
}

// C-3b 跳数继承(不经标记,单独钉):入站不带标记、hop_count=15 ⇒ 派生信封继承 15、经 R4 放行后为 16;
// 入站 hop_count=16 ⇒ 派生信封继承 16 ⇒ R4 静默丢弃。即便标记失效,链也不会无上限。
func TestAMRVerbatimDerivedEnvelopeInheritsInboundHop(t *testing.T) {
	for _, tc := range []struct {
		hop        int
		wantRouted int
		wantDrop   int
	}{{hop: defaultMaxHops - 1, wantRouted: 1, wantDrop: 0}, {hop: defaultMaxHops, wantRouted: 0, wantDrop: 1}} {
		f := newVerbatimFixture(t)
		dlv := f.seed(t, "bee", "ant", "@bee @cat 请处理", map[string]any{"type": "agent_route", metaKeyHopCount: tc.hop})
		f.turn("bee", dlv, "收到,原文如下:@bee @cat 请处理")
		cat := f.to("cat")
		if len(cat) != tc.wantRouted || f.dropped != tc.wantDrop {
			t.Errorf("C-3b 入站 hop=%d:投给 cat %d 封、R4 丢弃 %d 次,应为 %d / %d —— 派生信封没有继承入站 hop_count",
				tc.hop, len(cat), f.dropped, tc.wantRouted, tc.wantDrop)
			continue
		}
		if tc.wantRouted == 1 && hopCountOf(cat[0].env) != tc.hop+1 {
			t.Errorf("C-3b 入站 hop=%d:派生信封经 R4 后 hop 应为 %d,实得 %d", tc.hop, tc.hop+1, hopCountOf(cat[0].env))
		}
	}
}

// C-4 不投自己:入站不带标记(例如对方显式发来)而本席引用 "@bee @cat …" ⇒ 原文里的短名 bee 就是本席,
// 解析后指向本席的目标必须跳过;否则引用一次就分叉成"自己＋对方"两封(实测:改前每跳 2 封、hop 恒为 1)。
func TestAMRVerbatimQuotedSelfMentionIsNotRoutedToSelf(t *testing.T) {
	f := newVerbatimFixture(t)
	dlv := f.seed(t, "bee", "ant", "@bee @cat 请处理", map[string]any{"type": "agent_route"})
	n := len(f.routed)
	f.turn("bee", dlv, "收到,原文如下:@bee @cat 请处理")
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

// C-5 不吞:入站不带标记,引用里的提及只有本席自己 ⇒ 不是投递指令,按普通回程回给来源,不能被当"候选全拒"吞掉。
func TestAMRVerbatimSelfOnlyQuoteStillReturnsToOrigin(t *testing.T) {
	f := newVerbatimFixture(t)
	dlv := f.seed(t, "bee", "ant", "@bee 请处理", map[string]any{"type": "agent_route"})
	n := len(f.routed)
	f.turn("bee", dlv, "收到,原文如下:@bee 请处理\n已处理完毕。")
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

// C-6 等价钉死(一跳封顶与跳数继承共同的前提):走真实 RouteEnvelope 与真实 websocket,
// 收件方自报 delivery_ack_v1 ⇒ 投给它的那一帧 meta.delivery_id == session_delivery 主键,且已带 "dw-" 前缀
// (helper 的 unified_queue._delivery_id 对已带前缀者原样透传,见 delivery_id.go 契约)⇒ 回合事件的 delivery_id
// 能查回这封入站;再用它跑收件方引用原文的那一轮 ⇒ 标记在真实链路上生效(不产生派生投递)。
func TestAMRVerbatimTurnDeliveryIDMatchesSessionDelivery(t *testing.T) {
	f := newVerbatimFixture(t)
	ctx := context.Background()
	if err := writeEnvelope(ctx, f.conns["bee"], model.Envelope{ID: "caps-bee", To: "workpulse#" + f.keyID,
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(envelopeReadTimeout)
	for {
		f.hub.mu.Lock()
		c := f.hub.sessionClients[f.keyID][f.names["bee"]]
		f.hub.mu.Unlock()
		if c != nil && f.hub.sessionSupportsDeliveryACK(c) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("前提:bee 的 delivery_ack_v1 未生效")
		}
		time.Sleep(20 * time.Millisecond)
	}
	real := testAMRProcessor()
	real.hub = f.hub
	real.routeTimeout = 5 * time.Second
	real.routeEnvelope = f.hub.RouteEnvelope
	real.process(amrRouteJob{key: amrTurnKey{KeyID: f.keyID, Session: f.names["ant"], DeliveryID: "dw-dlv_seed-c6", TurnID: "t-c6"}, body: "@bee @cat 请处理"})

	var frame model.Envelope
	for {
		got := readEnvelope(t, ctx, f.conns["bee"])
		if metaString(got.Meta, "type") == "delivery" {
			frame = got
			break
		}
	}
	dlv := metaString(frame.Meta, "delivery_id")
	if !strings.HasPrefix(dlv, "dw-") || metaInt(frame.Meta, "delivery_version") != 1 {
		t.Fatalf("C-6:投给 bee 的帧应带已加 dw- 前缀的 delivery_id 且 delivery_version=1,meta=%v", frame.Meta)
	}
	row, err := f.hub.Repo.GetSessionDelivery(ctx, dlv)
	if err != nil || row == nil {
		t.Fatalf("C-6 等价:帧里的 delivery_id=%q 查不回 session_delivery(err=%v)—— 回合事件将查不到入站,继承与封顶都会静默失效", dlv, err)
	}
	var stored model.Envelope
	if err := json.Unmarshal([]byte(row.Envelope), &stored); err != nil || !metaBool(stored.Meta, metaKeyMentionVerbatim) {
		t.Fatalf("C-6:落库的入站应带 %s 标记,meta=%v err=%v", metaKeyMentionVerbatim, stored.Meta, err)
	}
	if stored.Body != "@bee @cat 请处理" {
		t.Errorf("C-6:真实链路上投给 bee 的正文被改写了,实得=%q", stored.Body)
	}

	// 用 helper 会用的那个 delivery_id(即帧里原样的值)跑 bee 引用原文的一轮
	n := len(f.routed)
	f.turn("bee", dlv, "收到,原文如下:"+stored.Body)
	for _, r := range f.routed[n:] {
		if strings.HasPrefix(r.env.To, f.names["cat"]+"#") {
			t.Errorf("C-6:经真实链路投来的带标记入站,bee 引用后仍给 cat 投了信 —— 一跳封顶在真实链路上没生效")
		}
	}
}
