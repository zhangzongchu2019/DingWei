package m8

// 回程段 2 —— reply 生成侧(设计里的 R1/R2/R3),接在 amr worker 的 non_candidate 分支上。
//
// 业主原话(SPEC-G 一之四,逐字):
//   「A 以 send.py 或者正文中 @提及 的方式显式发送消息到 B,B 在控制台输出的内容
//     如果没有使用 @提及 或者使用 send.py 回应 其内容本身应该回给 A。
//     但 A 此时收到 B 的消息不能再回到 B,除非 A 再次 send.py,或者使用 @提及功能」
//
// 🔴 来源【不来自任何新建的内存表】,来自 session_deliveries 这条 durable 记录:
//    Hub 投给 B 时写下 {delivery_id → 投出去那条信封的完整 JSON}(hub.go EnqueueSessionDelivery),
//    而 amr 的 turn key 上带着同一个 delivery_id ⇒ 一次主键查询就知道「这一轮是谁触发的」。
//
//    ⚠️ 不要改成内存表。组长 2026-09-19 据此立判据:
//       **凡承载"用户可感知行为"的状态,内存态即等于给它配了一个静默失效开关。**
//       自检:这个状态在重启后消失,用户会看到什么?答"什么也看不到"⇒ 不能用内存态。
//       此处若用内存表:Hub 一重启,B 的回程就静默失效 —— 正是 T-139「发得出去、回不来」的形状。
//
// 🔴 R-2(A 收到 B 的回复后不再自动回 B)在这里是**结构性**的,不靠 hop/dedup 兜底:
//    回程信封带 kind=reply ⇒ A 那一轮的入站 kind 就是 reply ⇒ 下面第二道判断直接 return。
//    兜底(R4 hop / R5 dedup,在 RouteEnvelope 里)仍然独立存在,两层各测各的(判据 9)。
//
// 🔴 一切"回不去"的情况都必须**有可见表现**,不得静默丢弃:
//    静默丢弃 = 业主又得到一条"发了没反应"的半通道,那正是 T-139 的病。

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// replyToOriginRouteSource 标记回程信封的来路,便于在日志/断言里与 @提及 路由区分。
const replyToOriginRouteSource = "reply_to_origin"

// replyToOrigin 处理「B 本轮有输出,但正文里没有任何 @提及目标」这一支。
// 它把 B 的这一轮输出原样回给「触发这一轮的那条投递的来源」。
//
// 调用点:amrProcessor.process() 的 non_candidate 分支(见 amr.go)。
func (p *amrProcessor) replyToOrigin(job amrRouteJob) {
	if p == nil || p.hub == nil || p.hub.Repo == nil {
		return
	}
	if job.key.DeliveryID == "" || job.body == "" {
		return
	}
	if job.systemNotification {
		log.Printf("[reply-origin] R1 跳过 reason=system_notification session=%s delivery_id=%s", job.key.Session, job.key.DeliveryID)
		p.bumpReplyOrigin(&p.stats.replyOriginSkipped)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()

	started := time.Now()
	delivery, err := p.hub.Repo.GetSessionDelivery(ctx, job.key.DeliveryID)
	// 🔴 组长 S2:这条查询每轮输出都会走一次,把单次耗时打出来,便于实测而不是估计。
	//    **不要为此加缓存** —— 加了就又回到内存态(见文件头那条判据)。
	log.Printf("[reply-origin] lookup session=%s delivery_id=%s cost=%s err=%v found=%t",
		job.key.Session, job.key.DeliveryID, time.Since(started), err, delivery != nil)
	if err != nil || delivery == nil {
		p.replyToOriginUnavailable(job, "本轮的来源记录已不可用,内容未自动回程")
		return
	}
	var inbound model.Envelope
	if e := json.Unmarshal([]byte(delivery.Envelope), &inbound); e != nil {
		p.replyToOriginUnavailable(job, "本轮的来源记录无法解析,内容未自动回程")
		return
	}
	// control 类(mcs/ack/board 等)不产生会话回程。
	if isControlEnvelope(inbound) {
		log.Printf("[reply-origin] R1 跳过 reason=control_inbound session=%s delivery_id=%s", job.key.Session, job.key.DeliveryID)
		p.bumpReplyOrigin(&p.stats.replyOriginSkipped)
		return
	}
	// 🔴 R-2:入站本身就是自动回程 ⇒ 不再生成下一跳。这是结构性的,不是兜底。
	if kindOf(inbound) == kindReply {
		log.Printf("[reply-origin] R2 跳过 reason=inbound_is_reply session=%s delivery_id=%s", job.key.Session, job.key.DeliveryID)
		p.bumpReplyOrigin(&p.stats.replyOriginSkipped)
		return
	}
	origin := originAddrOf(inbound)
	if origin == "" {
		origin = inbound.From
	}
	self := sessionAddress(job.key.Session, job.key.KeyID)
	if origin == "" || origin == self {
		p.replyToOriginUnavailable(job, "本轮没有可回程的来源地址,内容未自动回程")
		return
	}

	env := model.Envelope{
		ID:   "reply-origin-" + randomHex(16),
		To:   origin,
		From: self,
		Body: job.body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":               "agent_route",
			"no_mirror":          true,
			metaKeyKind:          kindReply,
			metaKeyReplyPolicy:   replyPolicyNoReply,
			metaKeyInReplyTo:     delivery.SourceMessageID,
			metaKeyDedupKey:      "reply-origin:" + job.key.DeliveryID + ":" + job.key.TurnID,
			"source_delivery_id": job.key.DeliveryID,
			"source_turn_id":     job.key.TurnID,
			"route_source":       replyToOriginRouteSource,
		},
	}
	// hop 沿链累加:RouteEnvelope 里的 R4 兜底靠它计数。不带的话每一跳都从 0 起算,兜底永远够不着。
	env.Meta[metaKeyHopCount] = hopCountOf(inbound)
	if m := metaInt(inbound.Meta, metaKeyMaxHops); m > 0 {
		env.Meta[metaKeyMaxHops] = m
	}
	if cid := metaString(inbound.Meta, metaKeyConversationID); cid != "" {
		env.Meta[metaKeyConversationID] = cid
	} else {
		env.Meta[metaKeyConversationID] = delivery.SourceMessageID
	}
	if p.hub.hasFeishuSyncOrMirror(ctx, job.key.KeyID, job.key.Session, origin) {
		p.bumpReplyOrigin(&p.stats.replyOriginSkipped)
		log.Printf("[reply-origin] 已由同步送达，跳过回程 session=%s to=%s delivery_id=%s", job.key.Session, origin, job.key.DeliveryID)
		return
	}

	// 用可注入的 p.routeEnvelope(构造时 = h.RouteEnvelope),与 @提及 路由同一个出口 ——
	// 测试据此捕获真实投出去的信封,而不是另造一个假出口。
	route := p.routeEnvelope
	if route == nil {
		route = p.hub.RouteEnvelope
	}
	if err := route(ctx, env); err != nil {
		p.replyToOriginUnavailable(job, "自动回程投递失败:"+err.Error())
		return
	}
	p.bumpReplyOrigin(&p.stats.replyOriginRouted)
	log.Printf("[reply-origin] R1 已回程 session=%s to=%s delivery_id=%s in_reply_to=%s hop=%d",
		job.key.Session, origin, job.key.DeliveryID, delivery.SourceMessageID, hopCountOf(inbound))
}

// bumpReplyOrigin 在锁下累加计数,供心跳日志与用例观察。
func (p *amrProcessor) bumpReplyOrigin(counter *uint64) {
	p.mu.Lock()
	*counter++
	p.mu.Unlock()
}

// replyToOriginUnavailable 把"回不去"这件事变成可见的一条 —— 日志 ＋ 回 B 一条投递失败。
// 🔴 组长 C1:过期/查不到时必须有可见表现,不能静默丢弃。
func (p *amrProcessor) replyToOriginUnavailable(job amrRouteJob, reason string) {
	log.Printf("[reply-origin] R1 回程未发生 session=%s delivery_id=%s reason=%s", job.key.Session, job.key.DeliveryID, reason)
	p.bumpReplyOrigin(&p.stats.replyOriginUnavailable)
	// 🔴 target 传空串,不传 replyToOriginRouteSource:
	//    deliveryFailureEnvelope 的 `→ %s` 槽位语义是**投递目标(会话名)**,
	//    hub.go:3005 写死了「凡是内部状态码、原因码、阶段名,一律传 ""」——
	//    传 "reply_to_origin" 会渲染成「投递失败 … → reply_to_origin」,读起来像投给了一个叫它的会话。
	p.notifySourceVisible(job.key, "", reason, true, job.body)
}
