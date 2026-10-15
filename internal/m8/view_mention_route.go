package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// view 页提及同组 Agent 的投递（R-V2-0102-VIEW-MENTION-ROUTE；要点板 runbooks/PLAN-R-V2-0102-VIEW-MENTION-ROUTE-opus50tc2k.md）。
// 只认页面发来的结构化 mentions（候选浮层选中的 full_name），不从正文解析（组长 NOFALLBACK 035027）。
// 放行＝该页 Agent 的当前编组候选集合；跨属主按 Agent 来信处理，不继承目标业主权限。
// 派生信封 Meta type 留空 ⇒ 走可靠投递（reliablePrimaryEnvelope），离线目标进持久投递；
// mention_verbatim ⇒ 目标那一轮输出里再写提及不再转投（amr.go process 的一跳封顶）。

const (
	metaKeyViewMention    = "_hub_view_mention" // Hub-only attestation; stripped from Agent WS ingress.
	viewMentionMaxTargets = 5                   // 每条最多投 5 个目标（组长 035235 第 3 条）
	viewMentionMaxInput   = 32                  // 请求里最多看前 32 项，其余不回执
)

// viewMentionResult 逐目标回执，随 /input 响应同步返回给页面。
type viewMentionResult struct {
	FullName string `json:"full_name"`
	Status   string `json:"status"`           // delivered | queued | rejected | failed
	Reason   string `json:"reason,omitempty"` // not_in_group | over_limit | offline | error
	Message  string `json:"message"`
}

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
	// 🔴 组长 NOACK 050053（tosonnet55tc2k 复现）：对端不报 delivery_ack_v1 ⇒ 派生信封会直写、没有 delivery_id，
	//    amr.go process 的一跳封顶查不回入站、不生效 ⇒ 不投，回执写明。判据与 routeToSession 相同：在线会话自报，
	//    否则看已登记端点；从未连过（端点未知）仍走下面的离线失败。
	if ack, known := h.viewMentionPeerACK(ctx, keyID, c.Agent.NameLC()); known && !ack {
		log.Printf("view mention decision=no_ack from=%s to=%s online=%t", selfFull, c.FullName, online)
		return viewMentionResult{FullName: c.FullName, Status: "failed", Reason: "no_ack", Message: label + " 暂不支持可靠转发，未转"}
	}
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
	if identity.Lower(c.Agent.OwnerLC) != identity.Lower(self.OwnerLC) {
		// This is an Agent-mediated cross-owner message, never a command from
		// the recipient's owner. Do not carry the View owner-exemption marker.
		sender := firstNonEmpty(strings.TrimSpace(principalID), self.OwnerLC)
		if u, err := h.Identity.Store.GetUser(ctx, sender); err == nil {
			sender = firstNonEmpty(u.DisplayName, u.UserID)
		}
		env.From = sessionAddress(self.NameLC(), AgentKeyID(self.OwnerLC))
		env.Meta["source_kind"] = "agent"
		env.Meta["source_display_name"] = fmt.Sprintf("%s 经 %s 的 View", sender, selfFull)
		env.Meta["cross_owner"] = true
		env.Meta[metaKeyViewMention] = true
		env.Meta["view_sender_principal_id"] = firstNonEmpty(strings.TrimSpace(principalID), self.OwnerLC)
		env.Meta["source_key_id"] = AgentKeyID(self.OwnerLC)
		env.Meta["source_session_name"] = self.NameLC()
		delete(env.Meta, "console_principal_id")
		env.Meta[hubIngressMetaKey] = hubIngressAgent
		// Recheck live membership at delivery as well as candidate enumeration.
		from := address{Kind: addressSession, KeyID: AgentKeyID(self.OwnerLC), SessionName: self.NameLC()}
		if err := h.enforceAgentGrouping(ctx, from, env, keyID, c.Agent.NameLC()); err != nil {
			return viewMentionResult{FullName: c.FullName, Status: "rejected", Reason: "not_in_group", Message: "未转给 " + label + "：不在本组或已不可用"}
		}
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

// viewMentionPeerACK 对端是否报 delivery_ack_v1（ack），以及是否已知该端点（known＝在线，或曾登记过端点）。
// 与 routeToSession 走持久投递的判据同源：会话在线且自报 ⇒ true；否则查端点登记。
func (h *Hub) viewMentionPeerACK(ctx context.Context, keyID, sessionName string) (ack, known bool) {
	h.mu.Lock()
	c := h.sessionClients[keyID][sessionName]
	h.mu.Unlock()
	if c != nil && h.sessionSupportsDeliveryACK(c) {
		return true, true
	}
	known = c != nil
	if h.Repo != nil {
		repoACK, endpointKnown, err := h.Repo.SessionEndpointDeliveryACK(ctx, keyID, sessionName)
		if err != nil {
			return false, known
		}
		return repoACK, known || endpointKnown
	}
	return false, known
}

// viewMentionEnvelopeID 派生号＝请求号＋目标短哈希：同一请求重试得同号（claimEnvelope 去重），不同目标互不相同。
func viewMentionEnvelopeID(requestID, agentID string) string {
	sum := sha256.Sum256([]byte(agentID))
	return requestID + "-m-" + hex.EncodeToString(sum[:6])
}
