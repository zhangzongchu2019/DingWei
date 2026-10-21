package main

import (
	"context"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// Called once at startup. Other platform send/receive wiring is independent.
// Only an explicit 1 enables D3 outbound notices; restart the Hub after changing it.
func configurePilotQuestionFeishu(h *m8.Hub, gateway feishu.Gateway) {
	h.QuestionFeishuEnabled = strings.TrimSpace(os.Getenv("DW_PILOT_QUESTION_FEISHU_ENABLED")) == "1"
	h.QuestionSender = nil
	if !h.QuestionFeishuEnabled {
		return
	}
	switch gateway.(type) {
	case *feishu.Fake, feishu.Stub, *feishu.Stub, nil:
		return
	}
	h.QuestionSender = func(ctx context.Context, issuer, subject, noticeID, text string) (string, error) {
		return feishu.SendPilotQuestion(ctx, gateway, feishu.OutMessage{BotChannelID: issuer, ToID: subject, ToType: "personal", Text: text, RequestID: feishu.DeriveRequestID("reply", noticeID, "pilot-question")})
	}
}
