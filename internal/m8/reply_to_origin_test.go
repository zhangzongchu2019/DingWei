package m8

// 回程段 2 的判据 R-1 ~ R-5 ＋ C2 阳性对照。
//
// 🔴 判据 9(两层各测各的):本文件只测【结构性不回程】那一层(R-2)。
//    hop/dedup 兜底那一层由 TestApplyReplyOriginTermination 单独测 ——
//    **不要把兜底也顺带覆盖进来**,否则删掉兜底不会红。
//
// 🔴 判据 6(素材要横跨被测维度):
//    C2 必须造【两个不同的 A】。同一个 A 发两次时,「按本轮 DeliveryID 回」与
//    「按最近一次回」结果相同,断言分辨不出来。

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

// replyOriginFixture 造一条「A → B」的 durable 投递记录,返回 turn key。
func replyOriginFixture(t *testing.T, h *Hub, ctx context.Context, deliveryID, sourceMessageID, from string, meta map[string]any) amrTurnKey {
	t.Helper()
	inbound := model.Envelope{ID: sourceMessageID, To: sessionAddress("bee", "k1"), From: from, Body: "请处理", TS: time.Now().Unix(), Meta: meta}
	payload, err := json.Marshal(inbound)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{
		ID: deliveryID, KeyID: "k1", SessionName: "bee",
		Envelope: string(payload), SourceMessageID: sourceMessageID, SourceType: "agent_route",
	}); err != nil {
		t.Fatal(err)
	}
	return amrTurnKey{KeyID: "k1", Session: "bee", DeliveryID: deliveryID, TurnID: "t-" + deliveryID}
}

func replyOriginProcessor(h *Hub) (*amrProcessor, *[]model.Envelope) {
	p := testAMRProcessor()
	p.hub = h
	p.routeTimeout = 5 * time.Second
	routed := &[]model.Envelope{}
	p.routeEnvelope = func(_ context.Context, env model.Envelope) error {
		*routed = append(*routed, env)
		return nil
	}
	return p, routed
}

// R-1:A 显式发 B ⇒ B 的「没有 @提及」的那一轮输出自动回 A。
func TestReplyToOriginR1RoutesTurnOutputBackToSender(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_r1", "msg-r1", sessionAddress("ant", "k1"), nil)
	p, routed := replyOriginProcessor(h)

	p.replyToOrigin(amrRouteJob{key: key, body: "我做完了"})

	if len(*routed) != 1 {
		t.Fatalf("R-1:应恰好回程一条,实测 %d 条", len(*routed))
	}
	got := (*routed)[0]
	if got.To != sessionAddress("ant", "k1") {
		t.Fatalf("R-1:必须回给触发这一轮的那个发送方,to=%q", got.To)
	}
	if got.From != sessionAddress("bee", "k1") {
		t.Fatalf("R-1:发件方必须是产生输出的会话,from=%q", got.From)
	}
	if got.Body != "我做完了" {
		t.Fatalf("R-1:正文必须是本轮输出原文,body=%q", got.Body)
	}
	if kindOf(got) != kindReply {
		t.Fatalf("R-1:回程必须标 kind=reply(R-2 就是靠它结构性成立),kind=%q", kindOf(got))
	}
	if metaString(got.Meta, metaKeyInReplyTo) != "msg-r1" {
		t.Fatalf("R-1:in_reply_to 必须是触发投递的源消息号,实测 %q", metaString(got.Meta, metaKeyInReplyTo))
	}
	if metaString(got.Meta, "route_source") != replyToOriginRouteSource {
		t.Fatalf("R-1:必须标明来路,便于与 @提及 路由区分,实测 %q", metaString(got.Meta, "route_source"))
	}
	if p.stats.replyOriginRouted != 1 {
		t.Fatalf("R-1:计数必须自增(心跳日志靠它可见),实测 %d", p.stats.replyOriginRouted)
	}
}

// Production reliable deliveries carry meta.type="delivery". That value is a
// transport wrapper, not a protocol control message, and must not suppress R1.
func TestReplyToOriginTransportDeliveryTypeStillRoutes(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_transport", "msg-transport", sessionAddress("ant", "k1"),
		map[string]any{"type": "delivery"})
	p, routed := replyOriginProcessor(h)

	p.replyToOrigin(amrRouteJob{key: key, body: "生产同形的普通投递输出"})

	if len(*routed) != 1 {
		t.Fatalf("transport type=delivery was mistaken for control: routed=%d want=1", len(*routed))
	}
	if got := metaString((*routed)[0].Meta, "route_source"); got != replyToOriginRouteSource {
		t.Fatalf("transport delivery reply route_source=%q want=%q", got, replyToOriginRouteSource)
	}
}

func TestReplyToOriginTrueControlTypesStillSkip(t *testing.T) {
	for _, controlType := range []string{"delivery_ack", "provision", "mirror_control"} {
		t.Run(controlType, func(t *testing.T) {
			h, _, ctx := newTestHub(t)
			key := replyOriginFixture(t, h, ctx, "dw-dlv_"+controlType, "msg-"+controlType, sessionAddress("ant", "k1"),
				map[string]any{"type": controlType})
			p, routed := replyOriginProcessor(h)

			p.replyToOrigin(amrRouteJob{key: key, body: "控制消息不应回程"})

			if len(*routed) != 0 || p.stats.replyOriginSkipped != 1 {
				t.Fatalf("control type=%s was not skipped: routed=%d skipped=%d", controlType, len(*routed), p.stats.replyOriginSkipped)
			}
		})
	}
}

// A Hub-authored system notice is injected into the CLI for visibility. The
// admitted lifecycle event carries its semantic marker into the completed turn,
// which must neither reply nor create another failure notice.
func TestReplyToOriginSystemNotificationTurnStopsWithoutFailureNotice(t *testing.T) {
	h, _, _ := newTestHub(t)
	p, routed := replyOriginProcessor(h)
	base := amrSessionEvent{DeliveryID: "opaque-delivery-coordinate", TurnID: "turn-system-notice"}
	admitted := base
	admitted.Kind = "user_input"
	admitted.Data = map[string]any{"lifecycle": "admitted", "system_notification": true}
	p.ingest("k1", "bee", amrEventJSON(t, admitted))
	text := base
	text.Kind, text.Text = "assistant_text", "我看到了系统通知"
	p.ingest("k1", "bee", amrEventJSON(t, text))
	done := base
	done.Kind = "state_change"
	done.Data = map[string]any{"change": "turn_completed", "status": "completed"}
	p.ingest("k1", "bee", amrEventJSON(t, done))

	var job amrRouteJob
	select {
	case job = <-p.queue:
	default:
		t.Fatal("system notification turn did not reach reply-to-origin worker")
	}
	if !job.systemNotification {
		t.Fatal("system notification semantic marker was lost before reply-to-origin")
	}
	p.replyToOrigin(job)
	if len(*routed) != 0 || p.stats.replyOriginUnavailable != 0 || p.stats.notificationsSent != 0 {
		t.Fatalf("system notification self-reproduced: routed=%d unavailable=%d notices=%d",
			len(*routed), p.stats.replyOriginUnavailable, p.stats.notificationsSent)
	}
	if p.stats.replyOriginSkipped != 1 {
		t.Fatalf("system notification skip was not counted: skipped=%d want=1", p.stats.replyOriginSkipped)
	}
}

// R-2:A 收到该回复之后的输出【不再】自动回 B —— 结构性,不靠兜底。
// 🔴 同一条用例里配阳性对照:换成一条【显式】入站,回程必须仍然发生。
//
//	只验「不回」是半条 —— 一个「干脆永不回程」的实现也能让它通过。
func TestReplyToOriginR2StopsOnReplyInboundButExplicitStillFlows(t *testing.T) {
	h, _, ctx := newTestHub(t)
	p, routed := replyOriginProcessor(h)

	// (a) 入站本身是自动回程 ⇒ 不再生成下一跳
	replyKey := replyOriginFixture(t, h, ctx, "dw-dlv_r2a", "msg-r2a", sessionAddress("ant", "k1"),
		map[string]any{metaKeyKind: kindReply})
	p.replyToOrigin(amrRouteJob{key: replyKey, body: "这是我对回复的输出"})
	if len(*routed) != 0 {
		t.Fatalf("R-2:入站为 reply 时不得再自动回程,实测回了 %d 条", len(*routed))
	}
	if p.stats.replyOriginSkipped != 1 {
		t.Fatalf("R-2:跳过必须计数,实测 %d", p.stats.replyOriginSkipped)
	}

	// (b) 阳性对照:A 再次显式发送(无 kind=reply)⇒ 仍能回程
	explicitKey := replyOriginFixture(t, h, ctx, "dw-dlv_r2b", "msg-r2b", sessionAddress("ant", "k1"), nil)
	p.replyToOrigin(amrRouteJob{key: explicitKey, body: "这轮是被显式消息触发的"})
	if len(*routed) != 1 {
		t.Fatalf("R-5 阳性对照:显式入站必须仍能回程,否则「不回」是靠断掉通道实现的,实测 %d 条", len(*routed))
	}
	if (*routed)[0].To != sessionAddress("ant", "k1") {
		t.Fatalf("R-5 阳性对照:回程目标错,to=%q", (*routed)[0].To)
	}
}

// R-3:hop 必须沿链累加 —— 不带的话每跳都从 0 起算,RouteEnvelope 里的 R4 兜底永远够不着。
// 🔴 本条只验「带没带」;兜底本身是否会丢弃由 TestApplyReplyOriginTermination 验(两层各测各的)。
func TestReplyToOriginR3CarriesHopForward(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_r3", "msg-r3", sessionAddress("ant", "k1"),
		map[string]any{metaKeyHopCount: 4, metaKeyMaxHops: 7})
	p, routed := replyOriginProcessor(h)

	p.replyToOrigin(amrRouteJob{key: key, body: "输出"})

	if len(*routed) != 1 {
		t.Fatalf("应回程一条,实测 %d", len(*routed))
	}
	if hopCountOf((*routed)[0]) != 4 {
		t.Fatalf("R-3:hop_count 必须沿链带过来(入站 4),实测 %d", hopCountOf((*routed)[0]))
	}
	if maxHopsOf((*routed)[0]) != 7 {
		t.Fatalf("R-3:max_hops 必须沿链带过来(入站 7),实测 %d", maxHopsOf((*routed)[0]))
	}
}

// R-4:回程必须带 dedup_key,且对同一 (delivery,turn) 稳定 —— RouteEnvelope 的 R5 靠它去重。
func TestReplyToOriginR4CarriesStableDedupKey(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_r4", "msg-r4", sessionAddress("ant", "k1"), nil)
	p, routed := replyOriginProcessor(h)

	p.replyToOrigin(amrRouteJob{key: key, body: "输出"})
	p.replyToOrigin(amrRouteJob{key: key, body: "输出"})

	if len(*routed) != 2 {
		t.Fatalf("本条只看 dedup_key 取值,投递两次,实测 %d", len(*routed))
	}
	first := metaString((*routed)[0].Meta, metaKeyDedupKey)
	second := metaString((*routed)[1].Meta, metaKeyDedupKey)
	if first == "" {
		t.Fatal("R-4:回程必须带 dedup_key,否则 R5 去重无从下手")
	}
	if first != second {
		t.Fatalf("R-4:同一 (delivery,turn) 的 dedup_key 必须稳定,%q != %q", first, second)
	}
}

// C1 / G-R-01:来源记录查不到时,失败通知必须【真的到达】在线的 B —— 不是只有计数。
//
// 🔴 本条是被打回后重写的。第一版的问题(验收人 G-R-01,组长立为判据 10):
//
//	用例名承诺「Visible / 不静默」,断言却只证明「计数 +1」;
//	夹具里根本没有在线的 B ⇒ notifySource 实际写失败(日志 source_offline),用例照样 PASS。
//	**用例名承诺的范围大于断言实际覆盖的范围** —— 而名字不会被变异检查,它永远是绿的。
//	自检方法:把用例名逐词念出来问「这个词我断言了吗」。
//
// 现在:接一条真 websocket,断言失败正文确实抵达。删掉 p.notifySource(...) 本条必须转红。
func TestReplyToOriginMissingDeliveryNotifiesOnlineSession(t *testing.T) {
	h, _, ctx := newTestHub(t)

	accepted := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-release
	}))
	defer server.Close()
	defer close(release)
	clientConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.CloseNow()
	serverConn := <-accepted
	defer serverConn.CloseNow()

	h.mu.Lock()
	h.sessionClients["k1"] = map[string]*sessionClient{"bee": {conn: serverConn, keyID: "k1", sessionName: "bee"}}
	h.mu.Unlock()

	p, routed := replyOriginProcessor(h)
	p.replyToOrigin(amrRouteJob{key: amrTurnKey{KeyID: "k1", Session: "bee", DeliveryID: "dw-dlv_missing", TurnID: "t1"}, body: "这是一轮无法回程但必须保留头部的输出"})

	if len(*routed) != 0 {
		t.Fatalf("查不到来源时不得凭空投递,实测 %d 条", len(*routed))
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := clientConn.Read(readCtx)
	if err != nil {
		t.Fatalf("G-R-01:失败通知必须真的抵达在线会话 —— 删掉 p.notifySource(...) 时本条应在这里转红。err=%v", err)
	}
	var got model.Envelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("抵达的不是可解析的信封:%v raw=%q", err, string(data))
	}
	if got.To != sessionAddress("bee", "k1") {
		t.Fatalf("通知必须发给产生输出的那个会话,to=%q", got.To)
	}
	if !strings.Contains(got.Body, "未自动回程") {
		t.Fatalf("通知正文必须说明「没有自动回程」这件事,实测 %q", got.Body)
	}
	if !strings.Contains(got.Body, "dw-dlv_missing") {
		t.Fatalf("通知正文必须带上是哪一次投递,便于定位,实测 %q", got.Body)
	}
	if !strings.Contains(got.Body, "原文头部：这是一轮无法回程但必须保留头部的输出") {
		t.Fatalf("默认回程失败通知必须带本轮原文头部,便于区分多条失败: %q", got.Body)
	}
	if strings.Contains(got.Body, replyToOriginRouteSource) {
		t.Fatalf("通知正文不得把内部来路名渲染成投递目标,实测 %q", got.Body)
	}
	if p.stats.replyOriginUnavailable != 1 {
		t.Fatalf("C1:查不到必须留下可见计数(心跳日志里看得到),实测 %d", p.stats.replyOriginUnavailable)
	}
}

// C2 阳性对照:两个【不同的】A 交错发给同一个 B ⇒ 各自的回程各回各家。
// 🔴 必须是两个不同的 A:同一个 A 发两次时,「按本轮 DeliveryID」与「按最近一次」结果相同,分辨不出来。
func TestReplyToOriginC2InterleavedSendersEachGetTheirOwnReply(t *testing.T) {
	h, _, ctx := newTestHub(t)
	p, routed := replyOriginProcessor(h)

	k1 := replyOriginFixture(t, h, ctx, "dw-dlv_c2a", "msg-c2a", sessionAddress("ant", "k1"), nil)
	k2 := replyOriginFixture(t, h, ctx, "dw-dlv_c2b", "msg-c2b", sessionAddress("cat", "k1"), nil)

	// 交错:先都进来,再按【第一条】的 turn 产出输出 —— 「最近一次」口径会把它回给 cat。
	p.replyToOrigin(amrRouteJob{key: k1, body: "这是对 ant 那条的回应"})
	p.replyToOrigin(amrRouteJob{key: k2, body: "这是对 cat 那条的回应"})

	if len(*routed) != 2 {
		t.Fatalf("两轮各回一条,实测 %d", len(*routed))
	}
	if (*routed)[0].To != sessionAddress("ant", "k1") {
		t.Fatalf("C2:第一轮由 ant 触发,必须回 ant —— 实测 %q(若为 cat,说明用的是「最近一次」口径)", (*routed)[0].To)
	}
	if (*routed)[1].To != sessionAddress("cat", "k1") {
		t.Fatalf("C2:第二轮由 cat 触发,必须回 cat,实测 %q", (*routed)[1].To)
	}
	if metaString((*routed)[0].Meta, metaKeyInReplyTo) != "msg-c2a" ||
		metaString((*routed)[1].Meta, metaKeyInReplyTo) != "msg-c2b" {
		t.Fatal("C2:in_reply_to 必须各自指向自己那一条源消息")
	}
}

// 🔴 装配级(判据 3):上面的用例都直接调 replyToOrigin —— 它们证明不了「这段被接上了」。
// 本条走真正的入口 amrProcessor.process(),body 里【没有任何 @提及】,
// 因此必须落进 non_candidate 分支并触发回程。
// 判据形态:删掉 amr.go 里那一行 p.replyToOrigin(job) ⇒ 本条必须转红。
func TestReplyToOriginIsWiredIntoProcessNonCandidateBranch(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_wire", "msg-wire", sessionAddress("ant", "k1"), nil)
	p, routed := replyOriginProcessor(h)
	p.mu.Lock()
	p.stats.opened, p.stats.queued = 1, 1
	p.mu.Unlock()

	// 正文里没有任何 @ 目标 ⇒ parseAMRCandidatesDetailed 给 0 候选且 lookedLikeCandidate=false
	p.process(amrRouteJob{key: key, body: "这一轮我只是在控制台里说了点什么，没有提及任何人"})

	if len(*routed) != 1 {
		t.Fatalf("装配:process() 的 non_candidate 分支必须触发回程,实测 %d 条 —— 删掉 amr.go 里那一行调用时本条应转红", len(*routed))
	}
	if (*routed)[0].To != sessionAddress("ant", "k1") || kindOf((*routed)[0]) != kindReply {
		t.Fatalf("装配:回程信封形态错 to=%q kind=%q", (*routed)[0].To, kindOf((*routed)[0]))
	}
}

func TestAMRUnsupportedChineseNameReturnsWholeTurnThroughRealIngress(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dw-dlv_soft", "msg-soft", sessionAddress("ant", "k1"), nil)
	p, routed := replyOriginProcessor(h)
	body := "这段正文提到 @张宗楚 但名称不符合路由语法，所以正文必须完整回程"
	p.ingest(key.KeyID, key.Session, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: body, DeliveryID: key.DeliveryID, TurnID: key.TurnID}))
	p.ingest(key.KeyID, key.Session, amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: key.DeliveryID, TurnID: key.TurnID}))
	select {
	case job := <-p.queue:
		p.process(job)
	default:
		t.Fatal("真实 ingest 入口没有形成回合任务")
	}
	if len(*routed) != 1 || (*routed)[0].Body != body || (*routed)[0].To != sessionAddress("ant", "k1") {
		t.Fatalf("不支持的中文名不得吞掉正文: routed=%+v", *routed)
	}
}

func TestAMRPreviouslyUnlistedSessionUsesDefaultTurnPipeline(t *testing.T) {
	p := testAMRProcessor()
	key := amrTurnKey{KeyID: "k-unlisted", Session: "previously-unlisted", DeliveryID: "d-unlisted", TurnID: "t-unlisted"}
	body := "请 @target 处理"
	p.ingest(key.KeyID, key.Session, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: body, DeliveryID: key.DeliveryID, TurnID: key.TurnID}))
	p.ingest(key.KeyID, key.Session, amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: key.DeliveryID, TurnID: key.TurnID}))
	select {
	case job := <-p.queue:
		if job.key != key || job.body != body {
			t.Fatalf("此前名单外席位没有进入同一条回合路由队列: job=%+v", job)
		}
	default:
		t.Fatal("此前名单外席位的回合正文被静默跳过")
	}
}
