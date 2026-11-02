package m8

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const stickyUnconfirmed = "上一条消息的投递结果未确认，请显式指定 Agent 或从候选中选择"
const stickyUnavailable = "暂不可确定默认 Agent，请稍后重试或显式选择"
const stickyCandidateTitle = "请选择一个 Agent："

type stickyLane struct {
	mu   sync.Mutex
	refs int
}
type stickyLanes struct {
	mu    sync.Mutex
	lanes map[store.FeishuStickyKey]*stickyLane
}

func (s *stickyLanes) acquire(k store.FeishuStickyKey) func() {
	s.mu.Lock()
	if s.lanes == nil {
		s.lanes = make(map[store.FeishuStickyKey]*stickyLane)
	}
	lane := s.lanes[k]
	if lane == nil {
		lane = &stickyLane{}
		s.lanes[k] = lane
	}
	lane.refs++
	s.mu.Unlock()
	lane.mu.Lock()
	return func() {
		lane.mu.Unlock()
		s.mu.Lock()
		lane.refs--
		if lane.refs == 0 {
			delete(s.lanes, k)
		}
		s.mu.Unlock()
	}
}
func stickyTrusted(msg model.Message) bool {
	return msg.ChatType == model.ChatPersonal && (msg.IngressProvenance == model.IngressFeishuWSAuthenticated || msg.IngressProvenance == model.IngressWebhookVerified)
}
func stickyCandidateBlock(text string) bool {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	if len(lines) > 0 && (lines[0] == stickyCandidateTitle || strings.HasSuffix(lines[0], "，"+stickyCandidateTitle)) {
		lines = lines[1:]
	}
	if len(lines) < 2 {
		return false
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		name, body, ok := parseSelector(line)
		if !ok || body != "" || !strings.Contains(name, ".") {
			return false
		}
	}
	return true
}
func stickyCandidates(agents []identity.Agent, full map[string]string, previous string) string {
	list := append([]identity.Agent(nil), agents...)
	sort.Slice(list, func(i, j int) bool {
		if list[i].ID == previous {
			return true
		}
		if list[j].ID == previous {
			return false
		}
		return full[list[i].ID] < full[list[j].ID]
	})
	lines := []string{stickyCandidateTitle}
	for _, a := range list {
		lines = append(lines, "#"+full[a.ID])
	}
	if len(list) == 0 {
		return "当前没有本人在线且可用的 Agent"
	}
	return strings.Join(lines, "\n")
}

// An authenticated connection is tied to the stable ID, never just its reused name.
func (h *Hub) stickyAgentOnline(a *identity.Agent) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.sessionClients[AgentKeyID(a.OwnerLC)][a.NameLC()]
	return c != nil && c.metricsAgentID == a.ID
}
func (h *Hub) stickyAgents(ctx context.Context, owner string) ([]identity.Agent, map[string]string, error) {
	all, err := h.Identity.Store.ListAgents(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	u, err := h.Identity.Store.GetUser(ctx, owner)
	if err != nil || u == nil || u.State != identity.UserActive {
		return nil, nil, errors.New("inactive principal")
	}
	full := make(map[string]string)
	var out []identity.Agent
	for _, a := range all {
		if a.OwnerLC == owner && a.State == identity.AgentActive && h.stickyAgentOnline(&a) {
			out = append(out, a)
			full[a.ID] = a.Name + "." + u.ShortName
		}
	}
	return out, full, nil
}

// Hook only at the existing explicit selector and final default decisions.
// Commands, configured prefixes, member mentions and group routing retain order.
func (h *Hub) dispatchFeishuSticky(ctx context.Context, msg model.Message, text string, explicit bool) (model.PrefixDispatchResult, bool, error) {
	if h.Identity == nil || msg.ChatType != model.ChatPersonal || msg.SenderOpenID == "" {
		return model.PrefixDispatchResult{}, false, nil
	}
	reply := func(s string) (model.PrefixDispatchResult, bool, error) {
		return model.PrefixDispatchResult{Matched: true, Reply: s}, true, nil
	}
	// An untrusted inbound cannot consume even the single-agent default.
	if !stickyTrusted(msg) {
		return reply("无法确认消息来源，不能使用私聊默认 Agent")
	}
	u, ok := h.newPlatformFeishuUser(ctx, msg)
	if !ok || u == nil {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, "", msg.BotChannelID)
		return reply("你的账号未绑定或已停用，无法投递消息")
	}
	if u.State != identity.UserActive {
		logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, "", msg.BotChannelID)
		return reply("你的账号已停用，无法投递消息")
	}
	agents, full, err := h.stickyAgents(ctx, u.UserIDLC())
	if err != nil {
		return reply(stickyUnavailable)
	}
	if msg.FeishuMsgID == "" {
		if explicit {
			return model.PrefixDispatchResult{}, false, nil
		}
		return reply(stickyCandidates(agents, full, ""))
	}
	repo, ok := h.Repo.(store.FeishuStickyRepository)
	if !ok {
		return reply(stickyUnavailable)
	}
	k := store.FeishuStickyKey{UserIDLC: u.UserIDLC(), BotChannelID: msg.BotChannelID}
	release := h.stickyLanes.acquire(k)
	defer release()
	// Identity and connections must be re-read after the same-key lane wait.
	u, ok = h.newPlatformFeishuUser(ctx, msg)
	if !ok || u == nil || u.State != identity.UserActive || u.UserIDLC() != k.UserIDLC {
		return reply(stickyUnavailable)
	}
	agents, full, err = h.stickyAgents(ctx, k.UserIDLC)
	if err != nil {
		return reply(stickyUnavailable)
	}
	state, err := repo.GetFeishuSticky(ctx, k)
	if err != nil {
		return reply(stickyUnavailable)
	}
	now := h.now().UTC()
	unresolved, err := repo.ListFeishuStickyUnresolved(ctx, k, now)
	if err != nil {
		return reply(stickyUnavailable)
	}
	blocked := false
	for _, o := range unresolved {
		// An existing durable receipt proves acceptance, never a reason to resend.
		if o.DeliveryID != "" {
			d, e := h.Repo.GetSessionDelivery(ctx, o.DeliveryID)
			if e == nil && d != nil && o.SourceEnvelopeID != "" && d.SourceMessageID == o.SourceEnvelopeID {
				if e = repo.ResolveFeishuSticky(ctx, o.Seq, now); e == nil {
					continue
				}
			}
		}
		blocked = true
	}
	kind := "no-prefix"
	requested, body := "", text
	if explicit {
		requested, body, _ = h.parseSelector(msg.BotChannelID, text)
		kind = "explicit"
		if strings.TrimSpace(body) == "" {
			kind = "select-only"
		}
	}
	op, fresh, err := repo.BeginFeishuSticky(ctx, k, msg.FeishuMsgID, kind, now)
	if err != nil {
		return reply(stickyUnavailable)
	}
	if !fresh {
		if op.UserIDLC != k.UserIDLC {
			return reply(stickyUnavailable)
		}
		if op.Status == "pending" || op.Status == "unknown" {
			return reply(stickyUnconfirmed)
		}
		if op.Status == "rejected" || op.Status == "stale_completion" {
			return reply("该消息此前未建立默认，请显式选择 Agent")
		}
		return reply("") // No business replay or TTL renewal.
	}
	finish := func(status, target, decision string) bool {
		err := repo.FinishFeishuSticky(ctx, op, status, target, decision, h.now().UTC())
		log.Printf("r14 source_message_id=%s user_id_lc=%s bot_channel_id=%s operation_seq=%d base_generation=%d target_agent_id=%s decision=%s status=%s commit_ok=%t", op.SourceMessageID, k.UserIDLC, k.BotChannelID, op.Seq, op.BaseGeneration, target, decision, status, err == nil)
		return err == nil
	}
	refuse := func(reason, decision string) (model.PrefixDispatchResult, bool, error) {
		if !finish("rejected", "", decision) {
			return reply(stickyUnconfirmed)
		}
		return reply(reason)
	}
	if stickyCandidateBlock(text) {
		return refuse("请选择一行并补充消息", "candidate_block")
	}
	if blocked && !explicit {
		return refuse(stickyUnconfirmed, "unresolved")
	}
	previous := ""
	oldUnavailable := false
	offlineName := ""
	if state != nil {
		old, e := h.Identity.Store.GetAgent(ctx, state.AgentID)
		if e != nil && !errors.Is(e, identity.ErrNotFound) {
			return refuse(stickyUnavailable, "identity_error")
		}
		if old == nil || old.OwnerLC != k.UserIDLC || old.State != identity.AgentActive || !h.stickyAgentOnline(old) {
			oldUnavailable = true
			if old != nil && old.OwnerLC == k.UserIDLC && old.State == identity.AgentActive {
				owner, e := h.Identity.Store.GetUser(ctx, k.UserIDLC)
				if e == nil {
					offlineName = "上次 Agent " + old.Name + "." + owner.ShortName + " 已离线，"
				}
			}
		} else {
			previous = old.ID
		}
	}
	candidates := stickyCandidates(agents, full, previous)
	var chosen *identity.Agent
	decision := ""
	if explicit {
		if strings.Contains(requested, ".") {
			// Foreign full names reuse the existing cross-member authorization path,
			// and must never replace the caller's personal default.
			target, owner, e := h.Identity.ResolveFullName(ctx, requested)
			if e == nil && target != nil && owner != nil && target.OwnerLC != k.UserIDLC {
				if target.State != identity.AgentActive || owner.State != identity.UserActive || !h.stickyAgentOnline(target) || !h.newPlatformUsersShareAgentGroup(ctx, k.UserIDLC, target) {
					return refuse(groupAgentNotFoundText, "cross_owner_denied")
				}
				if strings.TrimSpace(body) == "" {
					return refuse("跨成员目标不能设为本人默认 Agent", "cross_owner_select")
				}
				// No R14 state write for an explicit cross-member message.
				result, handled, e := h.dispatchMemberMention(ctx, msg, "@"+owner.ShortName+"#"+target.Name+" "+body)
				status := "rejected" // terminal metadata only; no sticky acceptance
				if !finish(status, "", "cross_member") {
					return reply(stickyUnconfirmed)
				}
				return result, handled, e
			}
			for i := range agents {
				if strings.EqualFold(full[agents[i].ID], requested) {
					chosen = &agents[i]
					break
				}
			}
		} else {
			for i := range agents {
				if strings.EqualFold(agents[i].Name, requested) {
					chosen = &agents[i]
					break
				}
			}
			if chosen == nil {
				var matches []identity.Agent
				for _, a := range agents {
					if strings.HasPrefix(a.NameLC(), identity.Lower(requested)) {
						matches = append(matches, a)
					}
				}
				if len(matches) == 1 {
					chosen = &matches[0]
				}
			}
		}
		if chosen == nil {
			return refuse("未找到你名下在线且可用的 Agent："+requested+"\n"+candidates, "explicit_invalid")
		}
		decision = "explicit"
	} else {
		if oldUnavailable {
			return refuse(offlineName+"上次 Agent 不可用，"+candidates, "target_unavailable")
		}
		// Any wall-clock rollback is fail closed, including the single-agent path.
		if state != nil && now.Before(state.SelectedAt) {
			return refuse("默认会话时钟异常，"+candidates, "clock_rollback")
		}
		if len(agents) == 1 {
			chosen = &agents[0]
			decision = "unique"
		} else if state != nil && state.Valid(now) {
			for i := range agents {
				if agents[i].ID == state.AgentID {
					chosen = &agents[i]
					break
				}
			}
			decision = "sticky"
		}
		if chosen == nil {
			return refuse("默认会话未选择或已过期，"+candidates, "candidates")
		}
	}
	// Recheck stable identity, ownership, active state and credential-bound socket
	// immediately before sending, not merely before waiting for the lane.
	current, e := h.Identity.Store.GetAgent(ctx, chosen.ID)
	if e != nil || current == nil || current.OwnerLC != k.UserIDLC || current.State != identity.AgentActive || !h.stickyAgentOnline(current) {
		return refuse("目标 Agent 已不可用，"+candidates, "target_changed")
	}
	user, e := h.Identity.Store.GetUser(ctx, k.UserIDLC)
	if e != nil || user.State != identity.UserActive {
		return refuse(stickyUnavailable, "principal_changed")
	}
	op.SourceEnvelopeID = firstNonEmpty(msg.ID, msg.FeishuMsgID)
	if e = repo.RecordFeishuStickyIntent(ctx, op, chosen.ID, decision, ""); e != nil {
		return reply(stickyUnconfirmed)
	}
	if kind != "select-only" {
		keyID := AgentKeyID(k.UserIDLC)
		env := model.Envelope{ID: op.SourceEnvelopeID, To: sessionAddress(current.NameLC(), keyID), From: feishuAddress(feishuOpenIDFromAccount(sourceAccount(msg)), keyID, h.botName(msg.BotChannelID)), Body: body, TS: h.now().Unix(), Meta: map[string]any{"chat_type": string(msg.ChatType), "raw": msg.Content, "bot_channel_id": msg.BotChannelID, "reply_prefix": fmt.Sprintf("【%s】", current.NameLC())}}
		h.annotateSourceContext(ctx, env.Meta, msg)
		if e = h.RouteEnvelope(ctx, env); e != nil {
			return refuse("该消息投递失败，请显式选择后重试", "route_rejected")
		}
		op.DeliveryID = metaString(env.Meta, "delivery_id")
		if e = repo.RecordFeishuStickyIntent(ctx, op, chosen.ID, decision, op.DeliveryID); e != nil {
			return reply(stickyUnconfirmed)
		}
	}
	if !finish("accepted", chosen.ID, decision) {
		return reply(stickyUnconfirmed)
	}
	if kind == "select-only" {
		return reply("已选择 Agent：" + full[chosen.ID])
	}
	return reply("")
}
func (h *Hub) startFeishuStickyCleanup() {
	if _, ok := h.Repo.(store.FeishuStickyRepository); !ok {
		return
	}
	ctx := h.lifetime().ctx
	h.backgroundWork("owned", func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep, cancel := context.WithTimeout(ctx, time.Minute)
				h.cleanupFeishuSticky(sweep, h.now().UTC())
				cancel()
			}
		}
	})
}
func (h *Hub) cleanupFeishuSticky(ctx context.Context, now time.Time) {
	repo, ok := h.Repo.(store.FeishuStickyRepository)
	if !ok || h.Identity == nil {
		return
	}
	h.stickyCleanupMu.Lock()
	defer h.stickyCleanupMu.Unlock()
	states, err := repo.ListFeishuStickyStates(ctx, store.FeishuStickyCleanupBatch, h.stickyCleanupCursor)
	if err != nil {
		return
	}
	removed := 0
	for _, s := range states {
		h.stickyCleanupCursor = s.FeishuStickyKey
		u, e := h.Identity.Store.GetUser(ctx, s.UserIDLC)
		invalid := errors.Is(e, identity.ErrNotFound) || (e == nil && u.State != identity.UserActive)
		if !invalid {
			a, e := h.Identity.Store.GetAgent(ctx, s.AgentID)
			invalid = errors.Is(e, identity.ErrNotFound) || (e == nil && (a.OwnerLC != s.UserIDLC || a.State != identity.AgentActive))
		}
		if invalid {
			if repo.DeleteFeishuSticky(ctx, s) == nil {
				removed++
			}
		}
	}
	if len(states) < store.FeishuStickyCleanupBatch {
		h.stickyCleanupCursor = store.FeishuStickyKey{}
	}
	if removed == store.FeishuStickyCleanupBatch {
		return
	}
	if _, err = repo.CleanupFeishuSticky(ctx, now, store.FeishuStickyCleanupBatch-removed); err != nil {
		log.Printf("r14 cleanup failed: %v", err)
	}
}
