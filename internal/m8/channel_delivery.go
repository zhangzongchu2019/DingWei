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
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
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
	if h.Identity != nil {
		if owner, ok := strings.CutPrefix(keyID, "u_"); ok && owner != "" {
			u, err := h.Identity.Store.GetUser(ctx, owner)
			if err == nil && u.State == identity.UserActive {
				ident, err := h.Identity.Store.GetIdentity(ctx, owner, "feishu")
				if err == nil {
					return ident.Subject
				}
			}
			return ""
		}
	}
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

type feishuNameMatch struct {
	feishuID string
	owner    string
	kind     string
}

type channelDeliveryResolution struct {
	openID   string
	fellBack bool
	note     string
}

// resolveFeishuName 在 active 成员和 active 群之间做同一轮精确名字匹配。
// ID 形态在调用方先短路；成员表中“显示名就是 ID”的历史行也不得再次作为名字命中。
func (h *Hub) resolveFeishuName(ctx context.Context, name string) ([]feishuNameMatch, []model.Member, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list members for feishu name: %w", err)
	}
	matches := make([]feishuNameMatch, 0, 2)
	seenFeishuID := make(map[string]struct{})
	addMatch := func(feishuID, owner, kind string) {
		feishuID = strings.TrimSpace(feishuID)
		if feishuID == "" {
			return
		}
		if _, exists := seenFeishuID[feishuID]; exists {
			return
		}
		seenFeishuID[feishuID] = struct{}{}
		matches = append(matches, feishuNameMatch{feishuID: feishuID, owner: strings.TrimSpace(owner), kind: kind})
	}
	for _, member := range members {
		displayName := strings.TrimSpace(member.DisplayName)
		if !member.Active || displayName != name || isAddressableFeishuID(displayName) || strings.TrimSpace(member.FeishuOpenID) == "" {
			continue
		}
		addMatch(member.FeishuOpenID, member.OwnerKey, "成员")
	}
	groups, err := h.Repo.ListChatEntities(ctx, model.ChatGroup)
	if err != nil {
		return nil, nil, fmt.Errorf("list groups for feishu name: %w", err)
	}
	for _, group := range groups {
		if !group.Active || strings.TrimSpace(group.DisplayName) != name || strings.TrimSpace(group.FeishuID) == "" {
			continue
		}
		owner := strings.TrimSpace(group.BoundOwner)
		if owner == "" {
			owner = strings.TrimSpace(group.BotChannelID)
		}
		addMatch(group.FeishuID, owner, "群")
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].owner != matches[j].owner {
			return matches[i].owner < matches[j].owner
		}
		return matches[i].feishuID < matches[j].feishuID
	})
	return matches, members, nil
}

func (match feishuNameMatch) attribution() string {
	return match.kind + " " + match.feishuID
}

// channelDeliveryTarget 解析出最终要投的飞书标识、是否兜底与正文末尾说明。
func (h *Hub) channelDeliveryTarget(ctx context.Context, keyID, addr string) (channelDeliveryResolution, error) {
	if isAddressableFeishuID(addr) {
		return channelDeliveryResolution{openID: strings.TrimSpace(addr)}, nil
	}
	if strings.TrimSpace(addr) == "" {
		return channelDeliveryResolution{openID: h.ownerFeishuOpenID(ctx, keyID), fellBack: true}, nil
	}
	if h.Repo == nil {
		return channelDeliveryResolution{}, fmt.Errorf("resolve feishu name: repository unavailable")
	}
	name := strings.TrimSpace(addr)
	matches, _, err := h.resolveFeishuName(ctx, name)
	if err != nil {
		return channelDeliveryResolution{}, err
	}
	if len(matches) == 1 {
		return channelDeliveryResolution{openID: matches[0].feishuID}, nil
	}
	// 兜底与「只写 ＠飞书」同一个取法：ownerFeishuOpenID 新平台读身份库的飞书绑定（console「飞书已绑定」同源），
	// 旧平台仍走 ownerKeyForKey＋成员表。原先这里只查旧平台成员表，新平台业主一律取不到（R-V2-FEISHU-OWNER-ID-DIAG）。
	resolution := channelDeliveryResolution{
		openID:   h.ownerFeishuOpenID(ctx, keyID),
		fellBack: true,
	}
	if len(matches) == 0 {
		resolution.note = fmt.Sprintf("未找到名为「%s」的飞书账号或群，因此发给业主本人", name)
		return resolution, nil
	}
	resolution.note = fmt.Sprintf("因为「%s」和「%s」同名，无法决定，因此发给业主本人", matches[0].attribution(), matches[1].attribution())
	return resolution, nil
}

func appendChannelDeliveryNote(body, note string) string {
	if note == "" {
		return body
	}
	return strings.TrimRight(body, " \t\r\n") + "\n" + note
}

// deliverChannelCandidate 把一条渠道候选投出去。
func (p *amrProcessor) deliverChannelCandidate(job amrRouteJob, candidate amrCandidate) error {
	h := p.hub
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()

	resolution, err := h.channelDeliveryTarget(ctx, job.key.KeyID, candidate.channelAddr)
	if err != nil {
		p.notifySource(job.key, "", "渠道投递失败：飞书名字寻址不可用，本条未发出", true)
		return err
	}
	bot := h.defaultBotName()
	// This records routing intent before RouteEnvelope; it is not a delivery receipt.
	log.Printf("[channel] deliver session=%s channel=%s addr=%q resolved=%q fell_back=%t bot=%q",
		job.key.Session, candidate.channel, candidate.channelAddr, resolution.openID, resolution.fellBack, bot)
	if resolution.openID == "" {
		// 🔴 连业主本人都取不到 ⇒ 没有任何可投的目标。必须可见,不静默。
		p.notifySource(job.key, "", "渠道投递失败：取不到业主本人的飞书标识，本条未发出", true)
		return errChannelNoTarget
	}

	env := model.Envelope{
		ID:   "channel-" + randomHex(16),
		To:   feishuAddress(resolution.openID, job.key.KeyID, bot),
		From: sessionAddress(job.key.Session, job.key.KeyID),
		Body: appendChannelDeliveryNote(candidate.body, resolution.note),
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":               "agent_route",
			"no_mirror":          true,
			"reply_policy":       candidate.replyPolicy,
			"route_source":       "channel_" + candidate.channel,
			"channel":            candidate.channel,
			"channel_addr":       candidate.channelAddr,
			"channel_fell_back":  resolution.fellBack,
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
