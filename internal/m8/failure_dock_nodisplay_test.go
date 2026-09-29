package m8

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 板件 runbooks/DESIGN-S-MENTION-ERRORDOCK-NODISPLAY-20260929-qwen38maxkhovvob4.md 的 G1~G7。
//
// 业主 2026-09-29 逐字:「如果匹配不上:a、当错误信息提示,传到 view 页错误提示区;
// b、当异常信息送达到 sessionHelper 发送方。但此信息不用于展示,仅告诉发送方有异常信息。
// 这一点在客户端没有升级支持的情况下可以不发。」
//
// 🔴 观测量是 **Hub 的 eventRing**(view 回放与实时扇出读的就是它),不是"我调了那个函数"。
// 探针自身带阳性对照(G7):它必须能看见**非** error 事件,否则"零个 error"可能只是探针没接上(盲量)。

// failureDockProbe 读 eventRing,返回(环内总条数, 其中 kind=="error" 的那些)。
// 🔴 环里出现一条读不懂的 JSON 就 Fatalf —— 静默跳过会让"零个 error"变成假绿。
func failureDockProbe(t *testing.T, h *Hub, keyID, session string) (int, []map[string]any) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.terminalForSession(keyID, session)
	total := len(st.eventRing)
	var dock []map[string]any
	for _, raw := range st.eventRing {
		var ev map[string]any
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("🔴 探针前提坏了:eventRing 里有一条不是合法 JSON,读不懂 ⇒ 「零个 error」不成立: %v raw=%q", err, raw)
		}
		if ev["kind"] == "error" {
			dock = append(dock, ev)
		}
	}
	return total, dock
}

// tryReadNotice 在 d 之内尝试读一封通知。**读不到不算错**,由调用方决定它是"应该没有"还是"丢了"。
func tryReadNotice(ctx context.Context, conn *websocket.Conn, d time.Duration) (model.Envelope, bool) {
	readCtx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		return model.Envelope{}, false
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return model.Envelope{}, false
	}
	return env, true
}

func setClientVersion(t *testing.T, h *Hub, keyID, session, version string) *sessionClient {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.sessionClients[keyID][session]
	if c == nil {
		t.Fatalf("夹具前提坏了:%s/%s 没有挂上客户端", keyID, session)
	}
	c.version = version
	return c
}

// assertDockEventShape 断 G1 的实质:kind=error、**三个键都不存在**、text/code 有内容。
// 🔴 断"键不存在",不是断"值为空" —— turnKeyFor 用 `||`,空串确实也会落到看板,
// 但**下一个人把它改成空串就能绕过"值为空"那种断言**,而分流行为已经变了。
func assertDockEventShape(t *testing.T, where string, ev map[string]any, wantTarget, wantReason string) {
	t.Helper()
	// 🔴 只禁 turn_id 与 delivery_id 这两个**分流键**(turnKeyFor = turn_id || delivery_id)。
	// 🔵 message_id **不在禁列**,而且是**必需**的:v2 契约缺它就置 conflict=v2_missing_message_id,
	//    渲染器会另产一张「消息格式异常」卡。这一条是实测纠正的(第一版把它一起禁了,
	//    连红五格;而 renderer-failure-dock-regression.js 腿二证明带着它照样进看板)。
	for _, banned := range []string{"turn_id", "delivery_id"} {
		if _, ok := ev[banned]; ok {
			t.Fatalf("🔴 %s:看板事件**带了 %s 键**(值=%v)⇒ 渲染器 :3114 会把它早退进【回合正文】,"+
				"永远进不了错误看板(业主 09-28 要的正是进看板)。这两个键一个都不能带。", where, banned, ev[banned])
		}
	}
	if ev["kind"] != "error" {
		t.Errorf("%s:kind=%v,应为 error(否则走不到 renderError)", where, ev["kind"])
	}
	text, _ := ev["text"].(string)
	if strings.TrimSpace(text) == "" {
		t.Errorf("🔴 %s:text 为空 ⇒ 业主会看到 DEFECT 件记的那个形状「只有一个数字、内容为空」", where)
	}
	if wantTarget != "" && !strings.Contains(text, wantTarget) {
		t.Errorf("%s:text=%q 不含目标写法 %q", where, text, wantTarget)
	}
	if wantReason != "" && !strings.Contains(text, wantReason) {
		t.Errorf("%s:text=%q 不含原因 %q", where, text, wantReason)
	}
	data, _ := ev["data"].(map[string]any)
	if data == nil || data["code"] != failureDockEventCode {
		t.Errorf("%s:data.code=%v,应为 %q", where, data["code"], failureDockEventCode)
	}
	// 🔴 data 里也**不许**出现 delivery_id / turn_id:渲染器 normalizeEvent 连 data.delivery_id、
	// data.turn_id 一起读(:2065、:2067),放这里同样会被早退进回合正文。
	if data != nil {
		for k := range data {
			if k != "code" && k != "source_id" {
				t.Errorf("🔴 %s:data 多了一个键 %q —— 若是 delivery_id/turn_id,它会和顶层那两个键一样把卡片踢出看板;"+
					"若是别的键,请先想清谁读它", where, k)
			}
		}
	}
	if s, _ := ev["message_id"].(string); strings.TrimSpace(s) == "" {
		t.Errorf("🔴 %s:message_id 缺失 ⇒ 声明了 schema_version=delivery.v2 却没有它,"+
			"渲染器 :2103 会置 conflict=v2_missing_message_id 并**另产一张**「消息格式异常」卡(:2328)", where)
	}
	if ev["schema_version"] != "delivery.v2" {
		t.Errorf("%s:schema_version=%v,与其余 Hub 自发事件不一致", where, ev["schema_version"])
	}
	if s, _ := ev["updated_at"].(string); s == "" {
		t.Errorf("%s:updated_at 缺失", where)
	}
}

// G1 ＋ G7 —— 咽喉①(writeDeliveryFailure):看板事件形状 ＋ 探针阳性对照。
func TestFailureDockEventFromWriteDeliveryFailure(t *testing.T) {
	h, _, ctx := newTestHub(t)
	conn := attachAMRNoticeClient(t, h, ctx, "dock-key", "dock-source")
	c := setClientVersion(t, h, "dock-key", "dock-source", "9.9.9.9")
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")

	// G7 探针阳性对照(一):一开始环是空的。
	total, dock := failureDockProbe(t, h, "dock-key", "dock-source")
	if total != 0 || len(dock) != 0 {
		t.Fatalf("G7 前提坏了:还没发生任何失败,环里就有 %d 条(其中 error %d 条)", total, len(dock))
	}
	// G7 探针阳性对照(二):探针必须能看见**非** error 事件 —— 否则"零个 error"可能只是探针没接上。
	h.publishSessionEvent("dock-key", "dock-source", `{"schema_version":"delivery.v2","kind":"assistant_text","text":"probe"}`)
	total, dock = failureDockProbe(t, h, "dock-key", "dock-source")
	if total != 1 || len(dock) != 0 {
		t.Fatalf("🔴 G7 阳性对照不成立:喂了一条 assistant_text,探针应看见 total=1/error=0,实为 total=%d/error=%d "+
			"⇒ 探针读的不是同一个环,下面所有「零个 error」都不采信", total, len(dock))
	}

	original := model.Envelope{ID: "dlv-dock-1", To: "@nobody", From: "dock-source#dock-key", Body: "正文"}
	h.writeDeliveryFailure(ctx, c, original, "@nobody", errors.New("目标会话无法补全为在线 canonical 地址"))

	total, dock = failureDockProbe(t, h, "dock-key", "dock-source")
	if len(dock) != 1 {
		t.Fatalf("🔴 G1:一次失败应恰好产出 1 张看板卡,实为 %d 张(环内共 %d 条)", len(dock), total)
	}
	// 🔴 反回灌:环内总数必须**恰好 2**(阳性对照那条 ＋ 这张卡)。
	// publishSessionEvent 只入环＋扇出给 view 订阅者,**不调** h.amr.ingest
	// (对照:handleSessionEventEnvelopeContext 是 publish ＋ ingest 两件事分开做的)
	// ⇒ 看板这张卡不会被当成新一轮正文再解析一次,不会接出新的通知环。
	// 多于 2 条就说明有人把它回灌了 —— 那是本仓 today 花了一整天在断的那个环。
	if total != 2 {
		t.Errorf("🔴 环内应恰好 2 条(对照 1 ＋ 看板卡 1),实为 %d 条 ⇒ 看板事件疑似被回灌/重复产出", total)
	}
	assertDockEventShape(t, "G1 咽喉①", dock[0], "@nobody", "目标会话无法补全为在线 canonical 地址")

	// G3(顺带):版本受支持 ⇒ 通知照发,且带 display=none。
	env, ok := tryReadNotice(ctx, conn, 2*time.Second)
	if !ok {
		t.Fatalf("G3:版本受支持的客户端应**收到**通知(只是不展示),实际没收到")
	}
	if got := metaString(env.Meta, "display"); got != "none" {
		t.Errorf("🔴 G3:display=%q,应为 \"none\"(业主:「此信息不用于展示」)", got)
	}
	if got := metaString(env.Meta, "type"); got != "delivery_failure" {
		t.Errorf("G3:type=%q,应为 delivery_failure(helper 那半 HNI 认的就是这个值)", got)
	}
}

// G2 —— 阴格:没有失败的一轮,看板**零张**卡(防"到处发卡")。
func TestFailureDockEventAbsentWithoutFailure(t *testing.T) {
	h, _, ctx := newTestHub(t)
	conn := attachAMRNoticeClient(t, h, ctx, "clean-key", "clean-source")
	setClientVersion(t, h, "clean-key", "clean-source", "9.9.9.9")
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	p := testAMRProcessor()
	p.hub = h
	h.amr = p

	key := amrTurnKey{KeyID: "clean-key", Session: "clean-source", DeliveryID: "dlv-clean", TurnID: "turn-1"}
	// 全成功的一轮:与业主那条 b 无关,行为必须与今天逐字相同。
	p.notifyRouteResults(key, []amrTargetResult{{selector: "@alpha", resolved: "alpha#clean-key", ok: true}})

	total, dock := failureDockProbe(t, h, "clean-key", "clean-source")
	if len(dock) != 0 {
		t.Fatalf("🔴 G2:全成功的一轮不得产出看板卡,实为 %d 张:%+v", len(dock), dock)
	}
	if total != 0 {
		t.Errorf("G2:全成功的一轮不该往环里塞任何东西,实为 %d 条", total)
	}
	env, ok := tryReadNotice(ctx, conn, 2*time.Second)
	if !ok {
		t.Fatalf("G2:全成功的【投递结果】通知应照发(不在业主这条 b 的范围里),实际没收到")
	}
	if _, has := env.Meta["display"]; has {
		t.Errorf("🔴 G2:全成功那条**不该**被打 display 标记(它不是异常信号),实为 %+v", env.Meta)
	}
	if !strings.Contains(env.Body, "成功 1") {
		t.Errorf("G2:【投递结果】正文形状变了:%q", env.Body)
	}
}

// G3 ＋ G4 ＋ 三值第三行 —— 版本门。**同一个 Hub、三个客户端**,同一套读法 ⇒
// 🔴 阴性那一腿的"读不到"有同装置阳性对照兜着(不是靠"读超时"当作通过)。
func TestNoDisplayNoticeVersionGate(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	h, _, ctx := newTestHub(t)
	newConn := attachAMRNoticeClient(t, h, ctx, "gate-key", "gate-new")
	oldConn := attachAMRNoticeClient(t, h, ctx, "gate-key", "gate-old")
	unknownConn := attachAMRNoticeClient(t, h, ctx, "gate-key", "gate-unknown")
	cNew := setClientVersion(t, h, "gate-key", "gate-new", "0.2.0.0")
	cOld := setClientVersion(t, h, "gate-key", "gate-old", "0.1.9.9")
	cUnknown := setClientVersion(t, h, "gate-key", "gate-unknown", "")

	original := model.Envelope{ID: "dlv-gate", To: "@x", From: "s#gate-key", Body: "正文"}
	reason := errors.New("目标会话无法补全为在线 canonical 地址")

	// 腿一(阳性对照,先跑):版本 ≥ 门槛 ⇒ 发,且带 display=none。
	h.writeDeliveryFailure(ctx, cNew, original, "@new-target", reason)
	env, ok := tryReadNotice(ctx, newConn, 2*time.Second)
	if !ok {
		t.Fatalf("🔴 G3 阳性对照不成立:版本 0.2.0.0 ≥ 门槛 0.2.0.0 应收到通知 ⇒ 下面那条「旧客户端收不到」不采信")
	}
	if got := metaString(env.Meta, "display"); got != "none" {
		t.Errorf("🔴 G3:display=%q,应为 none", got)
	}

	// 腿二:版本 < 门槛 ⇒ **不发**(业主:「可以不发」),但看板那张卡**照样要有**。
	h.writeDeliveryFailure(ctx, cOld, original, "@old-target", reason)
	if env, ok := tryReadNotice(ctx, oldConn, 400*time.Millisecond); ok {
		t.Errorf("🔴 G4:版本 0.1.9.9 < 门槛 0.2.0.0,**不该**收到通知,实际收到了:%+v", env)
	}
	_, dock := failureDockProbe(t, h, "gate-key", "gate-old")
	if len(dock) != 1 {
		t.Fatalf("🔴 G4:通知可以对旧客户端不发,但**看板卡必须有**(Hub 侧,不依赖 helper 版本)—— "+
			"少了它就是组长 09-29 说的「把可见故障换成不可见故障」。实为 %d 张", len(dock))
	}
	assertDockEventShape(t, "G4 咽喉①", dock[0], "@old-target", "")

	// 腿三(三值第三行):版本**未知** ⇒ 照发、**不打**标记(= 与今天逐字相同)。
	h.writeDeliveryFailure(ctx, cUnknown, original, "@unknown-target", reason)
	env, ok = tryReadNotice(ctx, unknownConn, 2*time.Second)
	if !ok {
		t.Fatalf("🔴 三值第三行:版本未知时应**照发**(减法能力在未知情形下保留既有可见性),实际没收到")
	}
	if v, has := env.Meta["display"]; has {
		t.Errorf("🔴 三值第三行:版本未知时**不该**声称 display=%v(对端可能会展示它,那是协议上说谎)", v)
	}
}

// G5 的第二处发卡点 —— 咽喉②(notifySourceVisible,failure=true)。
func TestFailureDockEventFromNotifySourceVisible(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	h, _, ctx := newTestHub(t)
	conn := attachAMRNoticeClient(t, h, ctx, "nsv-key", "nsv-source")
	setClientVersion(t, h, "nsv-key", "nsv-source", "9.9.9.9")
	p := testAMRProcessor()
	p.hub = h
	h.amr = p

	key := amrTurnKey{KeyID: "nsv-key", Session: "nsv-source", DeliveryID: "dlv-nsv", TurnID: "turn-1"}
	p.notifySourceVisible(key, "@offender", "提及格式正确但目标无权限、不可达或超过数量限制", true, "正文 @offender")

	_, dock := failureDockProbe(t, h, "nsv-key", "nsv-source")
	if len(dock) != 1 {
		t.Fatalf("🔴 G5-②:notifySourceVisible(failure=true) 应产出 1 张看板卡,实为 %d 张", len(dock))
	}
	assertDockEventShape(t, "G5 咽喉②", dock[0], "@offender", "提及格式正确但目标无权限")
	if _, ok := tryReadNotice(ctx, conn, 2*time.Second); !ok {
		t.Errorf("版本受支持 ⇒ 通知仍应照发")
	}
}

// G5 的第二处发卡点 · 阴性腿:发送方**离线**时,通知发不出去,但看板卡**照样要留**
// (eventRing 供晚订阅的 view 回放)—— 否则"离线"就等于"这件事没人知道"。
func TestFailureDockEventWhenSourceOffline(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	h, _, ctx := newTestHub(t)
	p := testAMRProcessor()
	p.hub = h
	h.amr = p

	key := amrTurnKey{KeyID: "off-key", Session: "off-source", DeliveryID: "dlv-off", TurnID: "turn-1"}
	p.notifySourceVisible(key, "@gone", "本轮没有可路由的完整文本", true, "")

	_, dock := failureDockProbe(t, h, "off-key", "off-source")
	if len(dock) != 1 {
		t.Fatalf("🔴 发送方离线时看板卡也必须有(否则这件事没人知道),实为 %d 张", len(dock))
	}
	assertDockEventShape(t, "离线腿", dock[0], "@gone", "本轮没有可路由的完整文本")
	_ = ctx
}

// G5 的第三处发卡点 —— 🔴 **业主实际撞到的那个形状**:「本轮 @1:成功 0,失败 1」。
// 它走 notifyRouteResults(信封是 delivery_result、failure=false),**不经** writeDeliveryFailure
// ⇒ 不覆盖它,㈡ 修的就不是业主看见的那一件事(闸会绿,问题还在)。
func TestRouteResultFailuresReachDockPerTarget(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	h, _, ctx := newTestHub(t)
	conn := attachAMRNoticeClient(t, h, ctx, "rr-key", "rr-source")
	setClientVersion(t, h, "rr-key", "rr-source", "9.9.9.9")
	p := testAMRProcessor()
	p.hub = h
	h.amr = p

	key := amrTurnKey{KeyID: "rr-key", Session: "rr-source", DeliveryID: "dlv-rr", TurnID: "turn-1"}
	p.notifyRouteResults(key, []amrTargetResult{
		{selector: "@alpha", resolved: "alpha#rr-key", ok: true},
		{selector: "#beta", err: errors.New("目标会话无法补全为在线 canonical 地址")},
		{selector: "#gamma", err: errors.New("组内找不到该Agent")},
	})

	_, dock := failureDockProbe(t, h, "rr-key", "rr-source")
	if len(dock) != 2 {
		t.Fatalf("🔴 第三处发卡点:两个失败目标应是**两张**卡(一张一个目标一个原因),实为 %d 张:%+v", len(dock), dock)
	}
	assertDockEventShape(t, "notifyRouteResults #beta", dock[0], "#beta", "目标会话无法补全为在线 canonical 地址")
	assertDockEventShape(t, "notifyRouteResults #gamma", dock[1], "#gamma", "组内找不到该Agent")

	// 有失败 ⇒ 这条【投递结果】就是业主说的异常信号 ⇒ 受版本门约束;受支持 ⇒ 照发且带标记。
	env, ok := tryReadNotice(ctx, conn, 2*time.Second)
	if !ok {
		t.Fatalf("版本受支持 ⇒ 【投递结果】通知应照发")
	}
	if got := metaString(env.Meta, "display"); got != "none" {
		t.Errorf("🔴 有失败的【投递结果】应带 display=none,实为 %q(meta=%+v)", got, env.Meta)
	}
	if !strings.Contains(env.Body, "成功 1，失败 2") {
		t.Errorf("【投递结果】正文形状变了:%q", env.Body)
	}
}

// G4 的第二处咽喉:amr 侧的版本门(与 hub 侧那道是**两处**,分别掏空必须分别红)。
func TestNoDisplayGateOnAMRChokePoint(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.0.0")
	h, _, ctx := newTestHub(t)
	conn := attachAMRNoticeClient(t, h, ctx, "amrgate-key", "amrgate-old")
	c := setClientVersion(t, h, "amrgate-key", "amrgate-old", "0.1.0.0")
	p := testAMRProcessor()
	p.hub = h
	h.amr = p

	key := amrTurnKey{KeyID: "amrgate-key", Session: "amrgate-old", DeliveryID: "dlv-amrgate", TurnID: "turn-1"}
	p.notifySourceVisible(key, "@old", "Hub 路由队列已满", true, "")

	if env, ok := tryReadNotice(ctx, conn, 400*time.Millisecond); ok {
		t.Errorf("🔴 G4-咽喉②:0.1.0.0 < 门槛 ⇒ 不该收到通知,实际收到:%+v", env)
	}
	if p.stats.notificationsSuppressed != 1 {
		t.Errorf("🔴 G4-咽喉②:抑制必须**留痕**(计数),实为 %d —— 静默跳过就是把 T-071 换个方向重演", p.stats.notificationsSuppressed)
	}
	_, dock := failureDockProbe(t, h, "amrgate-key", "amrgate-old")
	if len(dock) != 1 {
		t.Fatalf("🔴 G4-咽喉②:看板卡仍必须有,实为 %d 张", len(dock))
	}
	_ = c
}

// G9 的上半 —— 🔴 **一份形状,两个消费方,不许各写一遍。**
//
// view 侧的 view-service/renderer-failure-dock-regression.js 要喂一个"与 Hub 产出**逐字节同形**"
// 的事件,才能证明它真的落进错误看板。若 JS 里自己再抄一份形状,两份必漂移
// (本仓已在"两份清单各说各话"上吃过亏)⇒ 这里把 Hub 产出的那一串钉成**夹具真源**:
//
//	view-service/fixtures-failure-dock/hub-failure-dock-event.json
//
// 本格逐字节比对"代码此刻产出的" 与 "夹具里躺着的"。
// 🔴 改了 publishFailureDockEvent 的形状而没重生成夹具 ⇒ **本格红**,红文案里带 got/want 两串,
//
//	照 got 覆写夹具即可(JS 侧不用改,它读文件)。
//
// 🔵 时间戳是**传进去的**(fixed),不是 time.Now() ⇒ 输出确定;Go 的 json.Marshal 对 map 键排序 ⇒ 逐字节可比。
func TestFailureDockEventMatchesRendererFixture(t *testing.T) {
	h, _, _ := newTestHub(t)
	const (
		keyID   = "golden-key"
		session = "golden-source"
	)
	fixed := time.Date(2026, 9, 29, 15, 4, 5, 0, time.UTC)
	// 🔵 noticeID / sourceID 都传**固定值**:noticeID 为空时函数会自铸随机 id ⇒ 那就不可比了。
	h.publishFailureDockEvent(keyID, session, "failure-dock-golden", "dlv-golden", "#beta", "失败（目标会话无法补全为在线 canonical 地址）", fixed)

	h.mu.Lock()
	st := h.terminalForSession(keyID, session)
	var raw string
	if len(st.eventRing) == 1 {
		raw = st.eventRing[0]
	}
	h.mu.Unlock()
	if raw == "" {
		t.Fatalf("装置前提坏了:环里应恰好 1 条,实为 %d 条", len(st.eventRing))
	}

	const fixture = "../../view-service/fixtures-failure-dock/hub-failure-dock-event.json"
	want, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("读不到 view 侧夹具 %s(它必须入仓,否则 JS 那半没有真源): %v\n🔵 本次产出逐字节如下,照它建夹具:\n%s", fixture, err, raw)
	}
	if strings.TrimRight(string(want), "\n") != raw {
		t.Fatalf("🔴 夹具漂移:Hub 产出的看板事件与 view 侧夹具**逐字节不同** ⇒ JS 那半验的是另一份形状。\n"+
			"   got (代码此刻产出) = %s\n   want(夹具里躺着的) = %s\n"+
			"   改法:确认新形状是有意的,然后用 got 覆写夹具(不要改 JS,它读文件)。", raw, strings.TrimRight(string(want), "\n"))
	}
	// 🔴 顺带把**键集合**钉死:多一个键(尤其 turn_id / delivery_id / message_id)就会改变渲染器分流。
	var ev map[string]any
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("产出不是合法 JSON: %v", err)
	}
	wantKeys := map[string]bool{"schema_version": true, "kind": true, "message_id": true, "text": true, "data": true, "updated_at": true}
	for k := range ev {
		if !wantKeys[k] {
			t.Errorf("🔴 看板事件多了一个键 %q ⇒ 若是 turn_id/delivery_id/message_id,渲染器 :3114 会把它早退进回合正文;"+
				"若是别的键,请先想清谁读它(没人读的键就是装饰)", k)
		}
	}
	for k := range wantKeys {
		if _, ok := ev[k]; !ok {
			t.Errorf("看板事件少了键 %q", k)
		}
	}
}
