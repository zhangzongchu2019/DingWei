package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// view 页提及同组 Agent 的投递（R-V2-0102-VIEW-MENTION-ROUTE；要点板 runbooks/PLAN-R-V2-0102-VIEW-MENTION-ROUTE-opus50tc2k.md）。
// 只认页面发来的结构化 mentions（候选浮层选中的 full_name），不从正文解析（组长 NOFALLBACK 035027）。
// 放行＝该页 Agent 的候选集合（identity.MentionCandidates）∩ 与该 Agent 同 owner（组长 SCOPE 034953）。
// 派生信封 Meta type 留空 ⇒ 走可靠投递（reliablePrimaryEnvelope），离线目标进持久投递；
// mention_verbatim ⇒ 目标那一轮输出里再写提及不再转投（amr.go process 的一跳封顶）。

const (
	viewMentionMaxTargets = 5  // 每条最多投 5 个目标（组长 035235 第 3 条）
	viewMentionMaxInput   = 32 // 请求里最多看前 32 项，其余不回执
)

// viewMentionResult 逐目标回执，随 /input 响应同步返回给页面。
type viewMentionResult struct {
	FullName string `json:"full_name"`
	Status   string `json:"status"`           // delivered | queued | rejected | failed
	Reason   string `json:"reason,omitempty"` // cross_owner | not_in_group | over_limit | offline | error
	Message  string `json:"message"`
}

const viewMentionCrossOwnerText = "暂不支持转给其他用户的 Agent"

// routeViewMentions 当前会话已投成功之后调用。selfFullName＝该 view 页的 Agent；requestID 空（旧口径）时随机生成派生号。
// Identity 为空（旧平台）一律不做，返回 nil。
func (h *Hub) routeViewMentions(ctx context.Context, selfFullName, requestID, text, principalID string, mentions []string) []viewMentionResult {
	if h == nil || h.Identity == nil || len(mentions) == 0 {
		return nil
	}
	self, _, err := h.Identity.ResolveFullName(ctx, strings.TrimSpace(selfFullName))
	if err != nil {
		return nil
	}
	selfFull := h.Identity.FullName(ctx, self)
	cands, err := h.Identity.MentionCandidates(ctx, self.ID)
	if err != nil {
		log.Printf("view mention decision=candidates_unavailable session=%s err=%v", selfFull, err)
		cands = nil
	}
	byFull := make(map[string]identity.MentionCandidate, len(cands))
	for _, c := range cands {
		byFull[strings.ToLower(c.FullName)] = c
	}
	if requestID == "" {
		requestID = "vm-" + randomHex(16)
	}
	if len(mentions) > viewMentionMaxInput {
		mentions = mentions[:viewMentionMaxInput]
	}
	seen := map[string]bool{}
	accepted := 0
	out := make([]viewMentionResult, 0, len(mentions))
	for _, raw := range mentions {
		name := strings.TrimSpace(raw)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		if strings.EqualFold(name, selfFull) {
			continue // 提及自己：当前会话已收到原文，不重复投、不回执
		}
		c, ok := byFull[key]
		if !ok {
			out = append(out, viewMentionResult{FullName: name, Status: "rejected", Reason: "not_in_group", Message: "未转给 " + name + "：不在本组或已不可用"})
			continue
		}
		label := firstNonEmpty(c.Insert, c.FullName)
		if identity.Lower(c.Agent.OwnerLC) != identity.Lower(self.OwnerLC) {
			out = append(out, viewMentionResult{FullName: c.FullName, Status: "rejected", Reason: "cross_owner", Message: "未转给 " + label + "：" + viewMentionCrossOwnerText})
			continue
		}
		if accepted >= viewMentionMaxTargets {
			out = append(out, viewMentionResult{FullName: c.FullName, Status: "rejected", Reason: "over_limit", Message: "未转给 " + label + "：每条最多转给 5 个 Agent"})
			continue
		}
		accepted++
		out = append(out, h.deliverViewMention(ctx, self, selfFull, c, label, requestID, text, principalID))
	}
	return out
}

func (h *Hub) deliverViewMention(ctx context.Context, self *identity.Agent, selfFull string, c identity.MentionCandidate, label, requestID, text, principalID string) viewMentionResult {
	keyID := AgentKeyID(c.Agent.OwnerLC)
	online := h.AgentOnline(c.Agent.ID)
	env := model.Envelope{
		ID:   viewMentionEnvelopeID(requestID, c.Agent.ID),
		To:   sessionAddress(c.Agent.NameLC(), keyID),
		From: sessionAddress("workpulse", keyID),
		Body: terminalSubmission(text),
		TS:   time.Now().Unix(),
		// 🔴 type 必须留空：reliablePrimaryEnvelope 只对空／agent_route 走可靠投递（复核 tosonnet55tc2k 必改 1）。
		Meta: map[string]any{
			"source_kind": "user", "source_display_name": "View",
			"route_source": "view_mention", "mentioned_from": selfFull,
			"source_delivery_id": requestID, "no_mirror": true, "reply_policy": "no_reply",
			metaKeyMentionVerbatim: true,
			"console_principal_id": strings.TrimSpace(principalID), hubIngressMetaKey: hubIngressViewV2,
		},
	}
	if err := h.routeToSession(ctx, keyID, c.Agent.NameLC(), env, false); err != nil {
		log.Printf("view mention decision=failed from=%s to=%s online=%t err=%v", selfFull, c.FullName, online, err)
		if !online {
			return viewMentionResult{FullName: c.FullName, Status: "failed", Reason: "offline", Message: label + " 不在线，暂无法转达"}
		}
		return viewMentionResult{FullName: c.FullName, Status: "failed", Reason: "error", Message: "转给 " + label + " 失败，请稍后再试"}
	}
	log.Printf("view mention decision=routed from=%s to=%s online=%t", selfFull, c.FullName, online)
	if !online {
		return viewMentionResult{FullName: c.FullName, Status: "queued", Message: label + " 不在线，已排队；回复请到 " + label + " 的页面查看"}
	}
	return viewMentionResult{FullName: c.FullName, Status: "delivered", Message: "已转给 " + label + "，回复请到 " + label + " 的页面查看"}
}

// viewMentionEnvelopeID 派生号＝请求号＋目标短哈希：同一请求重试得同号（claimEnvelope 去重），不同目标互不相同。
func viewMentionEnvelopeID(requestID, agentID string) string {
	sum := sha256.Sum256([]byte(agentID))
	return requestID + "-m-" + hex.EncodeToString(sum[:6])
}
