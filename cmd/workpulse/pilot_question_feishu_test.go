package main

import (
	"context"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

type questionSwitchGateway struct {
	feishu.Stub
	sent []feishu.OutMessage
}

func (g *questionSwitchGateway) Send(_ context.Context, m feishu.OutMessage) (string, error) {
	g.sent = append(g.sent, m)
	return "provider-question", nil
}

func TestPilotQuestionFeishuStartupSwitch(t *testing.T) {
	for _, value := range []string{"", "0", "false", "true", "garbage", "1", " 1 "} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", value)
			// The future inbound opt-in cannot turn on D3 outbound.
			t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", "1")
			g := &questionSwitchGateway{}
			h := &m8.Hub{}
			configurePilotQuestionFeishu(h, g)
			enabled := value == "1" || value == " 1 "
			if h.QuestionFeishuEnabled != enabled || (h.QuestionSender != nil) != enabled {
				t.Fatal("D3_SWITCH_STARTUP_WIRING", value, h.QuestionFeishuEnabled, h.QuestionSender != nil)
			}
			// No runtime environment reread; a Hub restart/configuration is required.
			t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", "0")
			t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", "0")
			if enabled {
				id, err := h.QuestionSender(context.Background(), "bot", "ou_pm", "notice-one", "question")
				if err != nil || id != "provider-question" || len(g.sent) != 1 {
					t.Fatal("D3_SWITCH_ENABLED_SEND", id, err, len(g.sent))
				}
				m := g.sent[0]
				if m.BotChannelID != "bot" || m.ToID != "ou_pm" || m.ToType != "personal" || m.RequestID != feishu.DeriveRequestID("reply", "notice-one", "pilot-question") {
					t.Fatal("D3_SWITCH_SEND_IDENTITY", m)
				}
			} else if len(g.sent) != 0 {
				t.Fatal("D3_SWITCH_DISABLED_SENT", len(g.sent))
			}
			// Other platform traffic keeps the same gateway, even when D3 is off.
			if _, err := g.Send(context.Background(), feishu.OutMessage{Text: "ordinary-platform-message"}); err != nil || g.sent[len(g.sent)-1].Text != "ordinary-platform-message" {
				t.Fatal("D3_SWITCH_AFFECTED_OTHER_MESSAGES", err)
			}
		})
	}
}

func TestPilotQuestionFeishuFakeStubRemainUnwired(t *testing.T) {
	fake := &feishu.Fake{}
	for _, g := range []feishu.Gateway{fake, feishu.Stub{}, &feishu.Stub{}, nil} {
		for _, value := range []string{"0", "1"} {
			h := &m8.Hub{QuestionSender: func(context.Context, string, string, string, string) (string, error) {
				t.Fatal("STALE_D3_SENDER")
				return "", nil
			}}
			t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", value)
			configurePilotQuestionFeishu(h, g)
			if h.QuestionSender != nil {
				t.Fatalf("D3_FAKE_STUB_SENDER_WIRED: %T value=%s", g, value)
			}
		}
	}
	if len(fake.SentMessages()) != 0 {
		t.Fatal("D3_FAKE_STUB_SENT")
	}
}

func TestPilotQuestionFeishuInboundOutboundFlagsIndependent(t *testing.T) {
	for _, inbound := range []string{"0", "1"} {
		t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", inbound)
		t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", "1")
		h := &m8.Hub{}
		configurePilotQuestionFeishu(h, &questionSwitchGateway{})
		if !h.QuestionFeishuEnabled || h.QuestionSender == nil {
			t.Fatal("D3_INBOUND_FLAG_COUPLED_TO_OUTBOUND", inbound)
		}
	}
}
