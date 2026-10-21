package m8

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func (h *Hub) questionLink(qid string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(h.PublicOrigin))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	u.Path = "/console/questions"
	q := url.Values{}
	q.Set("question_id", qid)
	u.RawQuery = q.Encode()
	return u.String(), true
}
func questionMessage(q store.PilotQuestion, kind, link string) string {
	lead := "研发问题有新待办。"
	if kind == "remind_60m" {
		lead = "研发问题登记已超过60分钟，仍缺实质答复；请组长协调。"
	} else if kind == "assignment" {
		lead = "有研发问题已明确转派，请登录核对所需答复。"
	}
	names := map[string]string{"requirement_unclear": "需求不清", "change": "变更", "technical": "技术受阻", "resource": "缺资源"}
	// No fact/body, spec text, evidence/attachment, bearer or personal contact ID
	// leaves the platform. The recipient still logs in to the authorized object.
	return lead + "\n类型：" + names[q.Kind] + "\n请打开待答清单并登录平台：" + link + "\n直接回复此消息不会回写问题。答复到件仍须重核，不是业务签收或执行授权。"
}
func (h *Hub) processQuestions(parent context.Context) {
	if !h.questionDispatchMu.TryLock() {
		return
	}
	defer h.questionDispatchMu.Unlock()
	repo, err := h.questionRepo()
	if err != nil {
		return
	}
	// Independent budgets and remembered keysets prevent a large unconfigured
	// first page or a busy sender from starving reminders and later recipients.
	func() {
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		h.dispatchQuestionNotices(ctx, repo)
	}()
	func() {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		h.queueQuestionReminders(ctx, repo)
	}()
}
func (h *Hub) queueQuestionReminders(ctx context.Context, repo questionRepository) {
	for ctx.Err() == nil {
		ids, e := repo.OpenQuestionIDs(ctx, h.questionReminderAfter)
		if e != nil {
			return
		}
		if len(ids) == 0 {
			h.questionReminderAfter = ""
			return
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			h.questionReminderAfter = id
			c, e := repo.QuestionAuthorizationContext(ctx, "", "", id)
			if e != nil {
				continue
			}
			ref, e := repo.QuestionReferences(ctx, id)
			if e != nil {
				continue
			}
			actor := blockerCredential{actor: store.BlockerActor{UserID: c.Owner}}
			e = h.withQuestionIdentity(ctx, c, actor, ref, nil, false, func(_ identity.ContextAuthorization, check func() error) error {
				return repo.QueueQuestionReminder(ctx, c, id, check, h.now())
			})
			if e != nil && c.Task != nil {
				h.invalidateTaskIfDenied(ctx, *c.Task, e)
			}
		}
	}
}
func (h *Hub) dispatchQuestionNotices(ctx context.Context, repo questionRepository) {
	for ctx.Err() == nil {
		notices, e := repo.PendingQuestionNotices(ctx, h.questionOutboxAfter)
		if e != nil {
			return
		}
		if len(notices) == 0 {
			h.questionOutboxAfter = ""
			return
		}
		for _, n := range notices {
			if ctx.Err() != nil {
				return
			}
			h.questionOutboxAfter = n.ID
			c, e := repo.QuestionAuthorizationContext(ctx, "", "", n.QuestionID)
			if e != nil {
				continue
			}
			ref, e := repo.QuestionReferences(ctx, n.QuestionID)
			if e != nil {
				continue
			}
			actor := blockerCredential{actor: store.BlockerActor{UserID: c.Owner}}
			prepared, attempted := false, false
			e = h.withQuestionIdentity(ctx, c, actor, ref, []string{n.Recipient}, false, func(a identity.ContextAuthorization, check func() error) error {
				if n.Recipient != "" && !a.Users[n.Recipient] {
					return identity.ErrForbidden
				}
				binding, bound := a.Bindings[n.Recipient]
				link, originOK := h.questionLink(n.QuestionID)
				configured := h.QuestionFeishuEnabled && h.QuestionSender != nil && bound && a.Users[n.Recipient] && binding.Issuer != "" && feishu.IsPersonalOpenID(binding.Subject) && originOK
				unconfiguredReason := ""
				if !h.QuestionFeishuEnabled {
					unconfiguredReason = store.QuestionFeishuDisabledReason
				}
				q, ready, x := repo.PrepareQuestionNotice(ctx, c, n, configured, check, h.now(), unconfiguredReason)
				if x != nil || !ready {
					return x
				}
				prepared = true
				// Keep the question advisory and active identity locks through send. An
				// answer cannot commit between this check and the provider request.
				if x = check(); x != nil {
					return x
				}
				sendCtx, done := context.WithTimeout(ctx, 8*time.Second)
				defer done()
				attempted = true
				id, sendErr := h.QuestionSender(sendCtx, binding.Issuer, binding.Subject, n.ID, questionMessage(q, n.Kind, link))
				return repo.FinishQuestionNotice(ctx, n.ID, id, sendErr == nil, h.now())
			})
			if e != nil && !attempted {
				if reset, ok := repo.(unsentQuestionRepository); ok {
					state := "pending_config"
					if prepared {
						state = "queued"
					}
					if questionNoticeRevoked(e) {
						state = "cancelled"
					}
					cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
					_ = reset.DeferUnsentQuestionNotice(cleanup, n, state, prepared, h.now())
					done()
				}
			}
			if e != nil && c.Task != nil {
				h.invalidateTaskIfDenied(ctx, *c.Task, e)
			}
		}
	}
}

func questionNoticeRevoked(err error) bool {
	return errors.Is(err, identity.ErrForbidden) || errors.Is(err, identity.ErrNotFound) || errors.Is(err, identity.ErrGroupNotFound) || errors.Is(err, store.ErrTaskFlowPermission)
}
