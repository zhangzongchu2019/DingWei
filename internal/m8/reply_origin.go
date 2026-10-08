package m8

// reply-origin / loop-termination 契约的纯逻辑原语(冻结锚 5db177e，含 b6d3182 澄清)。
// 本文件只提供无副作用的字段访问 / 分类 / 终止判据 + 一个带 TTL 的去重表；
// 🔴 2026-09-19 更正这行注释(它此前说「不接入 live 路由」,而代码早已接上,骗过一次派工):
//    **段 2 已接入**:applyReplyOriginTermination 就是 RouteEnvelope 的第一行(hub.go:2612),
//    replyDedup 在 hub.go:235 初始化 ⇒ R4 hop 兜底 / R5 dedup / origin 捕获 三项都在执行路径上。
//    仍**不在本文件**的是 reply 生成侧(R1/R2/R3)—— 它在 reply_to_origin.go,
//    接在 amr worker 的 non_candidate 分支上。
//
// 设计对应:
//   §1 envelope 新字段 = 走 Meta 加性可选(向后兼容,旧端遇未知字段 ignore)。
//   B3 protocol_class{conversation,control} + payload classifier(mcs_dispatch_v1/mcs_receipt_v1/
//      delivery_ack/board_control ⇒ control;显式 conversation 却命中 reserved payload = spoof)。
//   R4 hop_count>=max_hops ⇒ 硬兜底丢弃。
//   R5 dedup_key 已见 ⇒ 丢弃(带 TTL 防表膨胀,§10)。
//   R1/R2/R3 = reply 生成侧闸(helper),不在本文件。
//   B1 ACK 独立:ack 用 delivery_id/ack_for，绝不用 in_reply_to(现码本就零 in_reply_to,by construction 满足)。

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ---- envelope Meta 字段键(冻结命名) ----
const (
	metaKeyProtocolClass       = "protocol_class"
	metaKeyKind                = "kind"
	metaKeyReplyPolicy         = "reply_policy"
	metaKeyConversationTermnl  = "conversation_terminal"
	metaKeyConversationID      = "conversation_id"
	metaKeyInReplyTo           = "in_reply_to"
	metaKeyReplyTo             = "reply_to"
	metaKeyOriginKind          = "origin_kind"
	metaKeyOriginAddr          = "origin_addr"
	metaKeyHopCount            = "hop_count"
	metaKeyMaxHops             = "max_hops"
	metaKeyDedupKey            = "dedup_key"
)

// ---- 枚举值(冻结) ----
const (
	protocolClassConversation = "conversation"
	protocolClassControl      = "control"

	kindContent = "content"
	kindReply   = "reply"
	kindAck     = "ack"
	kindMirror  = "mirror"

	replyPolicyExpectReply = "expect_reply"
	replyPolicyNoReply     = "no_reply"
	replyPolicyAckOnly     = "ack_only"

	originKindSessionHelper = "session_helper"
	originKindFeishu        = "feishu"
	originKindViewPage      = "view_page"
)

// defaultMaxHops = R4 未显式给 max_hops 时的硬兜底上限。
const defaultMaxHops = 16

// reservedControlPayloadTokens = B3 payload classifier 识别的 reserved-control 载荷 schema。
// 与现码 precedent 一致:internal/mcs/hubdriver/inbox.go 用 strings.Contains(Body,`"mcs_receipt_v1"`) 判回执。
var reservedControlPayloadTokens = []string{
	`"mcs_dispatch_v1"`,
	`"mcs_receipt_v1"`,
	`"board_control"`,
	`"delivery_ack"`,
}

// isReservedControlPayload 按 payload(Body/Meta)识别 M5 reserved-control 信封,
// 不依赖 protocol_class/kind(旧信封二者皆可能缺)。
func isReservedControlPayload(env model.Envelope) bool {
	if t := metaString(env.Meta, "type"); t != "" {
		switch t {
		// "delivery" is a transport-layer wrapper used by every reliable
		// session message, not a protocol-semantic control type. Treating it
		// as control structurally disables reply-to-origin for normal traffic.
		case "delivery_ack", "delivery_cancel", "delivery_cancel_ack",
			"mirror_control", "provision", "provision_ack":
			return true
		}
	}
	if k := metaString(env.Meta, metaKeyKind); k == kindAck {
		return true
	}
	body := env.Body
	for _, tok := range reservedControlPayloadTokens {
		if strings.Contains(body, tok) {
			return true
		}
	}
	return false
}

// protocolClassResult = 分类结果。Spoof=显式声明 conversation 却命中 reserved-control payload(§混版鲁棒性:reject+audit)。
type protocolClassResult struct {
	Class string // conversation | control
	Spoof bool
}

// classifyProtocolClass 判定 envelope 的 protocol_class(B3)。
// 顺序:先 payload-classifier 识别 reserved →命中则 control;若显式=conversation 且命中 reserved ⇒ spoof;
// 未命中 reserved 按显式 protocol_class(缺=conversation 默认)。
func classifyProtocolClass(env model.Envelope) protocolClassResult {
	explicit := metaString(env.Meta, metaKeyProtocolClass)
	reserved := isReservedControlPayload(env)
	if reserved {
		if explicit == protocolClassConversation {
			return protocolClassResult{Class: protocolClassControl, Spoof: true}
		}
		return protocolClassResult{Class: protocolClassControl}
	}
	if explicit == protocolClassControl {
		return protocolClassResult{Class: protocolClassControl}
	}
	return protocolClassResult{Class: protocolClassConversation}
}

// isControlEnvelope = 该信封是否属 control 类(短路:不进 conversation 路由/终止/reply 生成)。
func isControlEnvelope(env model.Envelope) bool {
	return classifyProtocolClass(env).Class == protocolClassControl
}

// ---- 字段访问器(默认值合冻结语义) ----

func kindOf(env model.Envelope) string {
	k := metaString(env.Meta, metaKeyKind)
	switch k {
	case kindContent, kindReply, kindAck, kindMirror:
		return k
	default:
		return kindContent // 缺省=content
	}
}

func replyPolicyOf(env model.Envelope) string {
	return metaString(env.Meta, metaKeyReplyPolicy) // "" = 未指定(helper 按 kind 定默认)
}

func conversationTerminalOf(env model.Envelope) bool {
	return metaBool(env.Meta, metaKeyConversationTermnl)
}

func hopCountOf(env model.Envelope) int {
	return metaInt(env.Meta, metaKeyHopCount)
}

func maxHopsOf(env model.Envelope) int {
	if m := metaInt(env.Meta, metaKeyMaxHops); m > 0 {
		return m
	}
	return defaultMaxHops
}

func dedupKeyOf(env model.Envelope) string {
	return metaString(env.Meta, metaKeyDedupKey)
}

func originKindOf(env model.Envelope) string {
	return metaString(env.Meta, metaKeyOriginKind)
}

func originAddrOf(env model.Envelope) string {
	return metaString(env.Meta, metaKeyOriginAddr)
}

func replyToOf(env model.Envelope) string {
	return metaString(env.Meta, metaKeyReplyTo)
}

// ---- R4 hop 兜底 ----

// hopBackstopExceeded 判定是否已达 max_hops 硬兜底(R4),达到则本消息应被丢弃、绝不再转发。
func hopBackstopExceeded(env model.Envelope) bool {
	return hopCountOf(env) >= maxHopsOf(env)
}

// ---- R5 dedup(带 TTL) ----

// replyDedup = dedup_key 去重表,带 TTL(§10:=session_delivery 周期,防表膨胀)。
// 线程安全。Seen 惰性清理过期条目。
type replyDedup struct {
	mu   sync.Mutex
	seen map[string]int64 // dedup_key -> 最近一次见到的 unixNano（R-V2-0105：滑动续期）
	ttl  time.Duration
	max  int              // 全局条数上限（R-V2-0105 件三；0 = 不限）
	now  func() time.Time // 可注入,便于测试
}

// replyDedupDefaultTTL／replyDedupDefaultMax：R-V2-0105 件三（要点第十、十二节，组长复定 24h／20 万条约 36 MB）。
// dedup_key＝reply-origin:<delivery>:<turn> 对每个回合唯一，正常业务不会对同一回合合法地再回一次 ⇒ 延长无误杀。
const (
	replyDedupDefaultTTL = 24 * time.Hour
	replyDedupDefaultMax = 200000
)

func newReplyDedup(ttl time.Duration) *replyDedup {
	if ttl <= 0 {
		ttl = replyDedupDefaultTTL
	}
	return &replyDedup{seen: map[string]int64{}, ttl: ttl, max: replyDedupDefaultMax, now: time.Now}
}

// SeenOrMark 返回 true 表示 key 之前已见(=应丢弃,R5);false 表示首见(已登记)。
// 空 key 视为不去重(始终 false)。
func (d *replyDedup) SeenOrMark(key string) bool {
	if key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	nowNano := d.now().UnixNano()
	ttlNano := d.ttl.Nanoseconds()
	if last, ok := d.seen[key]; ok {
		if nowNano-last < ttlNano {
			// 仍在窗内 = 重复；滑动续期：周期性重发只要间隔小于窗就一直被挡（原按首见计，720 s 节律每次越窗放行）
			d.seen[key] = nowNano
			return true
		}
		// 已过期:当作首见,刷新时间戳
		d.seen[key] = nowNano
		return false
	}
	d.seen[key] = nowNano
	if d.max > 0 && len(d.seen) > d.max {
		d.evictLocked(nowNano, ttlNano)
	}
	return false
}

// evictLocked 超上限时先清过期，仍超则按最早时间淘汰一批（上限的 1%，摊薄 O(n) 扫描）。
// 原先 Sweep 无任何调用方，表只增不减；这里在插入路径上兜住总量。
func (d *replyDedup) evictLocked(nowNano, ttlNano int64) {
	for k, at := range d.seen {
		if nowNano-at >= ttlNano {
			delete(d.seen, k)
		}
	}
	over := len(d.seen) - d.max
	if over <= 0 {
		return
	}
	batch := d.max / 100
	if batch < over {
		batch = over
	}
	type entry struct {
		key string
		at  int64
	}
	oldest := make([]entry, 0, len(d.seen))
	for k, at := range d.seen {
		oldest = append(oldest, entry{k, at})
	}
	sort.Slice(oldest, func(i, j int) bool { return oldest[i].at < oldest[j].at })
	for i := 0; i < batch && i < len(oldest); i++ {
		delete(d.seen, oldest[i].key)
	}
}

// Sweep 清理所有过期条目(可由后台定时调用;§10 防膨胀)。返回清理条数。
func (d *replyDedup) Sweep() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	nowNano := d.now().UnixNano()
	ttlNano := d.ttl.Nanoseconds()
	n := 0
	for k, first := range d.seen {
		if nowNano-first >= ttlNano {
			delete(d.seen, k)
			n++
		}
	}
	return n
}

func (d *replyDedup) size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}

// ---- 路由前钩子(段2:挂在 RouteEnvelope 顶部) ----

// applyReplyOriginTermination 在 conversation-class 消息进入路由前应用终止兜底 + origin 捕获。
// 返回 drop=true 表示应【静默丢弃】(不投递、不回错给发方——loop-protection 不该变成噪声源)。
// 向后兼容:control 类短路透传;无新字段的 legacy 消息 hop 从 0→1、不触发丢弃,行为不变。
func (h *Hub) applyReplyOriginTermination(env model.Envelope) (model.Envelope, bool) {
	// B3 control 类短路:mcs/ack/board 等不进 conversation 终止/origin/hop 逻辑。
	if isControlEnvelope(env) {
		return env, false
	}
	// R4 hop 硬兜底:达 max_hops ⇒ 丢弃,即使 R1-R3 漏也绝不无限。
	if hopBackstopExceeded(env) {
		log.Printf("[reply-origin] R4 hop 兜底丢弃 id=%s from=%s hop=%d max=%d", env.ID, env.From, hopCountOf(env), maxHopsOf(env))
		return env, true
	}
	// R5 dedup:dedup_key 已见 ⇒ 丢弃(防重复投递触发重复 reply loop)。
	if key := dedupKeyOf(env); key != "" && h.replyDedup.SeenOrMark(key) {
		log.Printf("[reply-origin] R5 dedup 丢弃 id=%s dedup_key=%s", env.ID, key)
		return env, true
	}
	// origin 捕获:首跳(无 origin_addr)记来源,供 reply-to-origin 回程路由。
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	if originAddrOf(env) == "" {
		env.Meta[metaKeyOriginAddr] = env.From
		if originKindOf(env) == "" {
			env.Meta[metaKeyOriginKind] = originKindSessionHelper
		}
	}
	// hop_count 递增(onward),作为 R4 的计数基。
	env.Meta[metaKeyHopCount] = hopCountOf(env) + 1
	return env, false
}
