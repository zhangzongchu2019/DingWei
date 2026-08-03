package m8

// reply-origin / loop-termination 契约的纯逻辑原语(冻结锚 5db177e，含 b6d3182 澄清)。
// 本文件只提供无副作用的字段访问 / 分类 / 终止判据 + 一个带 TTL 的去重表；
// 不接入 live 路由(段2 才把它们挂到 RouteEnvelope / helper)。这样段1可独立编译+单测。
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
		case "delivery", "delivery_ack", "delivery_cancel", "delivery_cancel_ack",
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
	seen map[string]int64 // dedup_key -> 首见 unixNano
	ttl  time.Duration
	now  func() time.Time // 可注入,便于测试
}

func newReplyDedup(ttl time.Duration) *replyDedup {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &replyDedup{seen: map[string]int64{}, ttl: ttl, now: time.Now}
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
	if first, ok := d.seen[key]; ok {
		if nowNano-first < ttlNano {
			return true // 仍在 TTL 内 = 重复
		}
		// 已过期:当作首见,刷新时间戳
		d.seen[key] = nowNano
		return false
	}
	d.seen[key] = nowNano
	return false
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
