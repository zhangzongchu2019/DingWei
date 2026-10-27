package m8

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type clarificationTransportRepository interface {
	clarificationRepository
	PrepareClarificationNotice(context.Context, store.QuestionContext, store.QuestionNotice, store.ClarificationBinding, func() error, time.Time) (store.ClarificationNotice, error)
	ClarificationQuestionRefs(context.Context, string) ([]string, error)
	ClarificationNoticeByParent(context.Context, string, string, string) (store.ClarificationNotice, error)
	IsKnownClarificationReply(context.Context, string, string, string) (bool, error)
	HasPreparedClarificationNotice(context.Context, string, string, time.Time) (bool, error)
	StageClarificationInbound(context.Context, model.Message, string, time.Time) (store.ClarificationInbound, error)
	RecordFeishuClarification(context.Context, store.QuestionContext, store.ClarificationNotice, store.ClarificationBinding, store.BlockerActor, store.ClarificationInbound, store.ClarificationUpdate, bool, func() error, time.Time) (store.ClarificationResult, error)
	RejectClarificationInbound(context.Context, store.ClarificationInbound, string) error
	PendingClarificationInbound(context.Context, int64) ([]store.ClarificationInbound, error)
	ClaimClarificationReceipt(context.Context, store.ClarificationInbound) (bool, error)
	FinishClarificationReceipt(context.Context, store.ClarificationInbound, bool) error
}

func clarificationBinding(id identity.Identity, authority ClarificationReplyAuthority) store.ClarificationBinding {
	raw, _ := json.Marshal(id)
	return store.ClarificationBinding{Issuer: id.Issuer, Subject: id.Subject, AppID: authority.AppID, Tenant: authority.Tenant, BindingSHA: fmt.Sprintf("%x", sha256.Sum256(raw))}
}

// Resolve tenant only from authenticated native metadata and this issuer's
// configured application. A nonempty operator pin always remains mandatory.
func (h *Hub) clarificationReplyAuthority(msg model.Message) (ClarificationReplyAuthority, bool) {
	authority, ready := h.ClarificationReplyAuthorities[msg.BotChannelID]
	r := msg.FeishuReply
	if !ready || r == nil || !r.Authenticated || msg.IngressProvenance != model.IngressFeishuWSAuthenticated || authority.AppID == "" || r.AppID != authority.AppID || strings.TrimSpace(r.Tenant) != r.Tenant || len(r.Tenant) > 256 || strings.ContainsAny(r.Tenant, "\x00\r\n") {
		return ClarificationReplyAuthority{}, false
	}
	if authority.Tenant != "" {
		if r.Tenant != authority.Tenant {
			return ClarificationReplyAuthority{}, false
		}
	} else {
		if r.Tenant == "" || r.SenderTenant != r.Tenant {
			return ClarificationReplyAuthority{}, false
		}
		authority.Tenant = r.Tenant
	}
	return authority, true
}

var clarificationCorrection = regexp.MustCompile(`^更正(?:第)?[ \t]*([1-9][0-9]{0,17})(?:条)?[：: \n]+([\s\S]+)$`)

func parseClarificationReply(text string) (store.ClarificationUpdate, error) {
	u := store.ClarificationUpdate{Kind: "explanation", Body: strings.TrimSpace(text)}
	if strings.HasPrefix(u.Body, "需改spec") || strings.HasPrefix(u.Body, "需改 spec") || strings.HasPrefix(u.Body, "需改 Spec") {
		return u, store.ErrTaskFlowInvalid
	}
	if strings.HasPrefix(u.Body, "条目：") || strings.HasPrefix(u.Body, "条目:") {
		lines := strings.SplitN(u.Body, "\n", 2)
		if len(lines) != 2 {
			return u, store.ErrTaskFlowInvalid
		}
		refs := strings.TrimPrefix(strings.TrimPrefix(lines[0], "条目："), "条目:")
		for _, r := range strings.Split(refs, ",") {
			u.EntryRefs = append(u.EntryRefs, strings.TrimSpace(r))
		}
		u.Body = strings.TrimSpace(lines[1])
	}
	if strings.HasPrefix(u.Body, "补充：") || strings.HasPrefix(u.Body, "补充:") {
		u.Kind = "supplement"
		u.Body = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(u.Body, "补充："), "补充:"))
	}
	if u.Kind == "explanation" && strings.HasPrefix(u.Body, "更正") {
		m := clarificationCorrection.FindStringSubmatch(u.Body)
		if m == nil {
			return u, store.ErrTaskFlowInvalid
		}
		n, e := strconv.ParseInt(m[1], 10, 64)
		if e != nil {
			return u, store.ErrTaskFlowInvalid
		}
		u.Kind, u.Corrects, u.Body, u.Reason = "correction", n, strings.TrimSpace(m[2]), strings.TrimSpace(m[2])
	}
	if u.Body == "" || len(u.Body) > 8192 || len(u.Reason) > 2048 {
		return u, store.ErrTaskFlowInvalid
	}
	return u, nil
}

// Recognized replies are consumed before the Agent/control/LLM route even when
// the feature is disabled or the metadata is invalid. Unknown unrelated chat
// remains on the existing route; a root reference never authorizes an answer.
func (h *Hub) dispatchClarificationReply(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	r := msg.FeishuReply
	if r == nil || r.ParentID == "" && r.RootID == "" {
		return model.PrefixDispatchResult{}, false, nil
	}
	repo, ok := h.Repo.(clarificationTransportRepository)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	known, err := repo.IsKnownClarificationReply(ctx, msg.BotChannelID, r.ParentID, r.RootID)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	prepared, err := repo.HasPreparedClarificationNotice(ctx, msg.BotChannelID, msg.SenderOpenID, h.now())
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if !known && !prepared {
		return model.PrefixDispatchResult{}, false, nil
	}
	matched := model.PrefixDispatchResult{Matched: true}
	_, ready := h.clarificationReplyAuthority(msg)
	if !ready {
		return matched, true, nil
	}
	in, err := repo.StageClarificationInbound(ctx, msg, text, h.now())
	if err != nil {
		if errors.Is(err, store.ErrTaskFlowConflict) || errors.Is(err, store.ErrTaskFlowPermission) || errors.Is(err, store.ErrTaskFlowInvalid) {
			return matched, true, nil
		}
		return matched, true, err
	}
	if !h.SpecClarificationEnabled || !h.SpecClarificationFeishuReplyEnabled {
		if err = repo.RejectClarificationInbound(ctx, in, "reply_disabled"); err != nil {
			return matched, true, err
		}
		return matched, true, nil
	}
	if err = h.recordClarificationInbound(ctx, repo, in); err != nil {
		return matched, true, err
	}
	return matched, true, nil
}

func (h *Hub) recordClarificationInbound(ctx context.Context, repo clarificationTransportRepository, in store.ClarificationInbound) error {
	r := in.Message.FeishuReply
	if r == nil {
		return store.ErrTaskFlowInvalid
	}
	if in.State == "rejected" {
		h.sendClarificationReceipt(ctx, repo, in, "未入帖：该回复未通过校验，请登录 Console 核对。")
		return nil
	}
	n, err := repo.ClarificationNoticeByParent(ctx, in.Message.BotChannelID, r.Tenant, r.ParentID)
	if errors.Is(err, sql.ErrNoRows) {
		known, e := repo.IsKnownClarificationReply(ctx, in.Message.BotChannelID, r.ParentID, r.RootID)
		if e != nil {
			return e
		}
		expires, e := time.Parse(time.RFC3339Nano, in.ExpiresAt)
		if e != nil {
			return e
		}
		if known || !h.now().Before(expires) {
			if e = repo.RejectClarificationInbound(ctx, in, "exact_parent_unavailable_or_expired"); e != nil {
				return e
			}
			h.sendClarificationReceipt(ctx, repo, in, "未入帖：无法核对原通知，请登录 Console 回答。")
			return nil
		}
		return nil // Finish may commit the exact mapping later; no guessed match.
	}
	if err != nil {
		return err
	}
	reject := func(reason string) error {
		if e := repo.RejectClarificationInbound(ctx, in, reason); e != nil {
			return e
		}
		h.sendClarificationReceipt(ctx, repo, in, "未入帖：当前身份、通知或条目已变化，请登录 Console 核对。")
		return nil
	}
	authority, ready := h.clarificationReplyAuthority(in.Message)
	if !h.SpecClarificationEnabled || !h.SpecClarificationFeishuReplyEnabled || !ready {
		return reject("reply_disabled_or_authority_changed")
	}
	if h.Identity == nil {
		return reject("identity_unavailable")
	}
	user, err := h.Identity.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: in.Message.BotChannelID, Subject: in.Message.SenderOpenID})
	if err != nil || user == nil || user.State != identity.UserActive || user.UserIDLC() != n.PMUser {
		return reject("current_pm_identity_required")
	}
	c, err := repo.QuestionAuthorizationContext(ctx, "", "", n.QuestionID)
	if err != nil {
		return reject("question_context_unavailable")
	}
	p, err := repo.ClarificationPolicy(ctx, c.SpecID)
	if err != nil {
		return err
	}
	u, err := parseClarificationReply(in.Body)
	if err != nil {
		return reject("reply_fields_invalid_use_console")
	}
	actor := blockerCredential{actor: store.BlockerActor{UserID: user.UserIDLC()}}
	var out store.ClarificationResult
	err = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(a identity.ContextAuthorization, check func() error) error {
		binding, bound := a.Bindings[n.PMUser]
		if !bound || binding.Provider != "feishu" || binding.Issuer != in.Message.BotChannelID || binding.Subject != in.Message.SenderOpenID {
			return identity.ErrForbidden
		}
		var e error
		out, e = repo.RecordFeishuClarification(ctx, c, n, clarificationBinding(binding, authority), actor.actor, in, u, true, check, h.now())
		return e
	})
	if err != nil {
		if errors.Is(err, store.ErrTaskFlowPermission) || errors.Is(err, store.ErrTaskFlowConflict) || errors.Is(err, store.ErrTaskFlowInvalid) || errors.Is(err, identity.ErrForbidden) || errors.Is(err, identity.ErrNotFound) {
			return reject("current_authorization_or_notice_rejected")
		}
		return err // transient persistence failure: same durable event, no new ID.
	}
	h.sendClarificationReceipt(ctx, repo, in, fmt.Sprintf("已入第%d条。解释不改范围；答复到件不等于任务恢复或业务签收。", out.Post.Floor))
	return nil
}

func (h *Hub) sendClarificationReceipt(ctx context.Context, repo clarificationTransportRepository, in store.ClarificationInbound, text string) {
	if !h.SpecClarificationFeishuReplyEnabled || h.ClarificationReceiptSender == nil || !feishu.IsPersonalOpenID(in.Message.SenderOpenID) {
		return
	}
	claimed, err := repo.ClaimClarificationReceipt(ctx, in)
	if err != nil || !claimed {
		return
	}
	key := fmt.Sprintf("clarification-receipt:%x", sha256.Sum256([]byte(in.Message.BotChannelID+"\x00"+in.Message.FeishuReply.Tenant+"\x00"+in.Message.FeishuMsgID)))
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	id, err := h.ClarificationReceiptSender(sendCtx, in.Message.BotChannelID, in.Message.SenderOpenID, key, text)
	finish, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer done()
	_ = repo.FinishClarificationReceipt(finish, in, err == nil && id != "")
}

func (h *Hub) reconcileClarificationReplies(parent context.Context) {
	if !h.clarificationDispatchMu.TryLock() {
		return
	}
	defer h.clarificationDispatchMu.Unlock()
	repo, ok := h.Repo.(clarificationTransportRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		page, err := repo.PendingClarificationInbound(ctx, h.clarificationInboundAfter)
		if err != nil {
			return
		}
		if len(page) == 0 {
			h.clarificationInboundAfter = 0
			return
		}
		for _, in := range page {
			if ctx.Err() != nil {
				return
			}
			h.clarificationInboundAfter = in.Sequence
			if err = h.recordClarificationInbound(ctx, repo, in); err != nil {
				return
			}
		}
	}
}
