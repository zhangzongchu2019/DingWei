package m8

// 渠道投递(G ② 第三格 G2-b)。业主裁定,逐字:
//
//	只有 @飞书/@feishu/@fs           ⇒ 直接发给业主
//	@飞书#账号或群号                 ⇒ 解析成功发该账号/群;**解析失败 ⇒ 发业主本人**
//	默认机器人 = HUB 注册后按机器人名称 a-z 排序取首
//
// 🔴 「解析失败 ⇒ 发业主」这条兜底是本格最容易做成半条的地方:
//    只验"失败会兜底"能被一个**一律发业主**的实现通过 —— 那样 `@飞书#oc_真群号` 也会被吞。
//    ⇒ 阳性对照必须同轮验:解析成功时**不得**误兜底到业主。

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// feishuAddressablePrefixes 是「能直接投递的飞书标识」前缀。
//
// 🔴 取值依据不是猜的:仓内已有两处按 `oc_` 判群聊(hub.go:1556、hub.go:3675),
// 本表与之同源;`ou_` 是用户 open_id 的对应形式。
// 不在表内 ⇒ 判为「解析失败」,按业主裁定兜底给业主本人。
var feishuAddressablePrefixes = []string{"ou_", "oc_", "on_"}

// isAddressableFeishuID 判断 `@飞书#X` 里的 X 是否可直接投递。
func isAddressableFeishuID(id string) bool {
	id = strings.TrimSpace(id)
	for _, prefix := range feishuAddressablePrefixes {
		if strings.HasPrefix(id, prefix) {
			return len(id) > len(prefix)
		}
	}
	return false
}

// defaultBotName 按业主裁定取默认机器人:**注册后按机器人名称 a-z 排序取首**。
//
// 🔴 「注册后排序」而不是「注册顺序」:注册顺序取决于进程启动时的并发,
// 同一份配置两次启动可能给出不同的默认机器人 —— 那是不可复现的行为。
// 排序取首是确定的。
func (h *Hub) defaultBotName() string {
	h.mu.Lock()
	names := make([]string, 0, len(h.botChannels))
	for name := range h.botChannels {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	h.mu.Unlock()
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names[0]
}

// ownerFeishuOpenID 取该 key 所属业主本人的飞书 open_id(兜底目标)。
func (h *Hub) ownerFeishuOpenID(ctx context.Context, keyID string) string {
	if h.Repo == nil {
		return ""
	}
	ownerKey := h.ownerKeyForKey(ctx, keyID)
	if ownerKey == "" {
		return ""
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return ""
	}
	for _, member := range members {
		if member.Active && member.OwnerKey == ownerKey && member.FeishuOpenID != "" {
			return member.FeishuOpenID
		}
	}
	return ""
}

// channelDeliveryTarget 解析出最终要投的飞书标识,并说明是不是走了兜底。
func (h *Hub) channelDeliveryTarget(ctx context.Context, keyID, addr string) (openID string, fellBack bool) {
	if isAddressableFeishuID(addr) {
		return strings.TrimSpace(addr), false
	}
	// addr 为空(只有 @飞书)或不可寻址(写错了/写成中文名)⇒ 一律发业主本人。
	return h.ownerFeishuOpenID(ctx, keyID), true
}

// deliverChannelCandidate 把一条渠道候选投出去。
func (p *amrProcessor) deliverChannelCandidate(job amrRouteJob, candidate amrCandidate) error {
	h := p.hub
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()

	openID, fellBack := h.channelDeliveryTarget(ctx, job.key.KeyID, candidate.channelAddr)
	bot := h.defaultBotName()
	log.Printf("[channel] deliver session=%s channel=%s addr=%q resolved=%q fell_back=%t bot=%q",
		job.key.Session, candidate.channel, candidate.channelAddr, openID, fellBack, bot)
	if openID == "" {
		// 🔴 连业主本人都取不到 ⇒ 没有任何可投的目标。必须可见,不静默。
		p.notifySource(job.key, "", "渠道投递失败：取不到业主本人的飞书标识，本条未发出", true)
		return errChannelNoTarget
	}

	env := model.Envelope{
		ID:   "channel-" + randomHex(16),
		To:   feishuAddress(openID, job.key.KeyID, bot),
		From: sessionAddress(job.key.Session, job.key.KeyID),
		Body: candidate.body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":               "agent_route",
			"no_mirror":          true,
			"reply_policy":       candidate.replyPolicy,
			"route_source":       "channel_" + candidate.channel,
			"channel":            candidate.channel,
			"channel_addr":       candidate.channelAddr,
			"channel_fell_back":  fellBack,
			"source_delivery_id": job.key.DeliveryID,
			"source_turn_id":     job.key.TurnID,
		},
	}
	route := p.routeEnvelope
	if route == nil {
		route = h.RouteEnvelope
	}
	return route(ctx, env)
}

// errChannelNoTarget = 连兜底目标都不存在。
var errChannelNoTarget = channelError("channel: no deliverable target")

type channelError string

func (e channelError) Error() string { return string(e) }
