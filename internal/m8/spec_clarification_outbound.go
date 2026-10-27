package m8

import (
	"context"
	"strconv"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func (h *Hub) withQuestionNoticeIdentity(ctx context.Context, repo questionRepository, c store.QuestionContext, actor blockerCredential, ref store.PilotQuestion, n store.QuestionNotice, fn func(identity.ContextAuthorization, func() error, store.ClarificationPolicy) error) error {
	if cr, ok := repo.(clarificationRepository); ok {
		p, err := cr.ClarificationPolicy(ctx, c.SpecID)
		if err != nil {
			return err
		}
		if p.Version > 0 {
			return h.withClarificationIdentity(ctx, cr, c, p, actor, []string{n.Recipient}, func(a identity.ContextAuthorization, check func() error) error { return fn(a, check, p) })
		}
	}
	return h.withQuestionIdentity(ctx, c, actor, ref, []string{n.Recipient}, false, func(a identity.ContextAuthorization, check func() error) error {
		return fn(a, check, store.ClarificationPolicy{})
	})
}

func clarificationNoticeText(q store.PilotQuestion, n store.ClarificationNotice, link string) string {
	return "研发问题待解释。\nSpec修订：" + q.SpecID + " / " + strconv.Itoa(q.Revision) + "\n条目／子例：" + strings.Join(n.EntryRefs, "、") + "\n直接回复本消息＝对本问题的解释，不改范围。补充请以“补充：”开头。普通解释无需前缀或重写条目ID。同一通知再次回复会新增补充楼层。\n需改Spec请打开手机Console：" + link + "\n通知7日有效；以已入第N条回执为准，未收到回执请在Console核对，不盲重发。"
}
