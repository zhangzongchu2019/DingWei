package main

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// configureR9PMFeishu wires only the existing private-message gateway. The Hub
// starts its persisted outbox worker only when the R9 feature flag is exactly 1.
func configureR9PMFeishu(h *m8.Hub, gateway feishu.Gateway) {
	switch gateway.(type) {
	case *feishu.Fake, feishu.Stub, *feishu.Stub, nil:
		h.ConfigureR9PMFeishuSender(nil)
		return
	}
	h.ConfigureR9PMFeishuSender(func(ctx context.Context, issuer, subject, operationID, text string) (string, error) {
		return feishu.SendPilotQuestion(ctx, gateway, feishu.OutMessage{BotChannelID: issuer, ToID: subject, ToType: "personal", Text: text, RequestID: feishu.DeriveRequestID("reply", operationID, "r9-pm")})
	})
}
