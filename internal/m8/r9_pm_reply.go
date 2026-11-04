package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type r9PMReplyStore interface {
	R9PMReplyTarget(context.Context, string) (*store.R9PMReplyTarget, error)
	AcceptR9PMResponse(context.Context, store.R9PMResponseInput) (bool, error)
}

func parseR9PMDecision(text string) (decision, comment string, ok bool) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", "", false
	}
	for _, v := range []struct{ label, code string }{{"同意", "agree"}, {"不同意", "disagree"}, {"需修改", "revise"}} {
		if text == v.label {
			return v.code, "", true
		}
		if strings.HasPrefix(text, v.label+"：") {
			return v.code, strings.TrimSpace(strings.TrimPrefix(text, v.label+"：")), true
		}
		if strings.HasPrefix(text, v.label+"\n") {
			return v.code, strings.TrimSpace(strings.TrimPrefix(text, v.label+"\n")), true
		}
	}
	return "", "", false
}
func (h *Hub) dispatchR9PMReply(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	if !R9PMDecisionRequestEnabled() {
		return model.PrefixDispatchResult{}, false, nil
	}
	meta := msg.FeishuReply
	if msg.ChatType != model.ChatPersonal || meta == nil || strings.TrimSpace(meta.ParentID) == "" || (msg.IngressProvenance != model.IngressFeishuWSAuthenticated && msg.IngressProvenance != model.IngressWebhookVerified) {
		return model.PrefixDispatchResult{}, false, nil
	}
	repo, ok := h.Repo.(r9PMReplyStore)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	target, err := repo.R9PMReplyTarget(ctx, meta.ParentID)
	if err != nil {
		return model.PrefixDispatchResult{}, false, nil
	}
	if strings.TrimSpace(os.Getenv("FCS_RPC_URL")) != "" {
		log.Printf("R9 Feishu response rejected: FCS_RPC_URL configured request_id=%s", target.RequestID)
		return model.PrefixDispatchResult{Matched: true, Reply: "当前飞书回应通道不可用，请在 Console 待办中回应。"}, true, nil
	}
	if !meta.Authenticated || meta.MessageID == "" || meta.Tenant == "" || meta.SenderType != "user" || meta.MessageType != "text" || msg.BotChannelID == "" || msg.SenderOpenID == "" || h.Identity == nil || h.Identity.Store == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "未能验证此飞书回应，请使用 Console 待办。"}, true, nil
	}
	authority, ready := h.ClarificationReplyAuthorities[msg.BotChannelID]
	if !ready || authority.AppID == "" || meta.AppID != authority.AppID || strings.TrimSpace(meta.Tenant) != meta.Tenant || len(meta.Tenant) > 256 || strings.ContainsAny(meta.Tenant, "\x00\r\n") {
		return model.PrefixDispatchResult{Matched: true, Reply: "飞书应用验证失败，请使用 Console 待办。"}, true, nil
	}
	if authority.Tenant != "" {
		if meta.Tenant != authority.Tenant {
			return model.PrefixDispatchResult{Matched: true, Reply: "飞书租户验证失败，请使用 Console 待办。"}, true, nil
		}
	} else if meta.Tenant == "" || meta.SenderTenant != meta.Tenant {
		return model.PrefixDispatchResult{Matched: true, Reply: "飞书租户验证失败，请使用 Console 待办。"}, true, nil
	}
	u, err := h.Identity.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: msg.BotChannelID, Subject: msg.SenderOpenID})
	if err != nil || u.State != identity.UserActive || u.UserIDLC() != target.PMUserIDLC {
		return model.PrefixDispatchResult{Matched: true, Reply: "只有当前负责 PM 可以回应此请求。"}, true, nil
	}
	if h.SpecReader == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "当前仓库读取权限不可核验，请使用 Console 待办。"}, true, nil
	}
	if _, err = h.SpecReader.Read(ctx, u.UserIDLC(), specread.Coordinate{Provider: target.Provider, Instance: target.Instance, Repository: target.RepositoryID, Commit: target.CommitSHA, Path: "openspec/changes/" + target.ChangeDirectory + "/proposal.md"}, ""); err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "当前仓库读取权限不可核验，请使用 Console 待办。"}, true, nil
	}
	decision, comment, ok := parseR9PMDecision(text)
	if !ok {
		return model.PrefixDispatchResult{Matched: true, Reply: "请明确回复「同意」「不同意」或「需修改」。"}, true, nil
	}
	digest := sha256.Sum256([]byte(msg.BotChannelID + "\x00" + meta.Tenant + "\x00" + meta.MessageID))
	responseID := hex.EncodeToString(digest[:])
	accepted, err := repo.AcceptR9PMResponse(ctx, store.R9PMResponseInput{ResponseID: responseID, RequestID: target.RequestID, Generation: target.Generation, PrincipalUserIDLC: u.UserIDLC(), Channel: "feishu", Decision: decision, Comment: comment, Issuer: msg.BotChannelID, Tenant: meta.Tenant, MessageID: meta.MessageID, EventID: meta.EventID, PayloadSHA256: meta.ContentSHA, OperationID: meta.EventID})
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if !accepted {
		return model.PrefixDispatchResult{Matched: true, Reply: "此请求已回应或已失效，本次回应记为迟到。"}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已记录你的 PM 决定。"}, true, nil
}

type r9PMCycleStore interface {
	RunR9PMScheduler(context.Context, time.Time) (int, error)
}

func (h *Hub) startR9PMScheduler() {
	if !R9PMDecisionRequestEnabled() {
		return
	}
	repo, ok := h.Repo.(r9PMCycleStore)
	if !ok {
		return
	}
	ctx := h.lifetime().ctx
	h.backgroundWork("owned", func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		run := func() {
			sweep, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			n, err := repo.RunR9PMScheduler(sweep, h.now().UTC())
			if err != nil {
				log.Printf("R9 PM scheduler failed: %v", err)
			} else if n > 0 {
				log.Printf("R9 PM scheduler advanced requests=%d", n)
			}
		}
		run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if !R9PMDecisionRequestEnabled() {
					return
				}
				run()
			}
		}
	})
}

type r9PMDeliveryStore interface {
	ListR9PMFeishuDeliveries(context.Context, int) ([]store.R9PMDelivery, error)
	StartR9PMDeliveryAttempt(context.Context, string) (bool, error)
	CompleteR9PMDelivery(context.Context, string, string) error
}

func (h *Hub) ConfigureR9PMFeishuSender(sender QuestionSender) {
	h.R9PMQuestionSender = nil
	if !R9PMDecisionRequestEnabled() || sender == nil {
		return
	}
	h.R9PMQuestionSender = sender
	h.startR9PMDeliveryWorker()
}
func (h *Hub) startR9PMDeliveryWorker() {
	if !R9PMDecisionRequestEnabled() || h.R9PMQuestionSender == nil {
		return
	}
	repo, ok := h.Repo.(r9PMDeliveryStore)
	if !ok {
		return
	}
	ctx := h.lifetime().ctx
	h.backgroundWork("owned", func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		h.dispatchR9PMDeliveries(ctx, repo)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !R9PMDecisionRequestEnabled() {
					return
				}
				h.dispatchR9PMDeliveries(ctx, repo)
			}
		}
	})
}
func (h *Hub) dispatchR9PMDeliveries(ctx context.Context, repo r9PMDeliveryStore) {
	if h.R9PMQuestionSender == nil || !R9PMDecisionRequestEnabled() {
		return
	}
	items, err := repo.ListR9PMFeishuDeliveries(ctx, 20)
	if err != nil {
		log.Printf("R9 PM delivery queue read failed: %v", err)
		return
	}
	for _, d := range items {
		started, err := repo.StartR9PMDeliveryAttempt(ctx, d.OperationID)
		if err != nil {
			log.Printf("R9 PM delivery claim failed operation_id=%s: %v", d.OperationID, err)
			continue
		}
		if !started {
			continue
		}
		body := fmt.Sprintf("PM 决策请求 %s（代际 %d）\n决策点：%s\n固定提交：%s\n摘要：%s\n请明确回复：同意 / 不同意 / 需修改。", d.RequestID, d.Generation, d.PointID, d.CommitSHA, d.SummaryJSON)
		providerID, err := h.R9PMQuestionSender(ctx, d.Issuer, d.RecipientID, d.OperationID, body)
		if err == nil && providerID == "" {
			err = errors.New("empty Feishu receipt")
		}
		if err != nil {
			log.Printf("R9 PM delivery receipt unknown operation_id=%s", d.OperationID)
			continue
		}
		if err = repo.CompleteR9PMDelivery(ctx, d.OperationID, providerID); err != nil {
			log.Printf("R9 PM delivery receipt persist failed operation_id=%s", d.OperationID)
		}
	}
}
