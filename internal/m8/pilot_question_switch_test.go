package m8

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestPilotQuestionFeishuDisabledRetainsConsoleAndCanLaterSend(t *testing.T) {
	e, q := questionWorkerConfigured(t)
	original := e.svc.Store
	e.svc.Store = questionCheckFixture{Store: original, auth: original.(identity.QuestionAuthorizationStore)}
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	sends := 0
	// Defensive worker gate must hold even if an internal caller injected a sender.
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
		sends++
		return "provider-confirmed", nil
	}
	readNotice := func() store.QuestionDelivery {
		t.Helper()
		r := questionHTTP(e, "GET", q.ID, "", "zzc", "", "")
		if r.Code != 200 || !strings.Contains(r.Body.String(), "实际现象") {
			t.Fatal("D3_SWITCH_CONSOLE_UNAVAILABLE", r.Code, r.Body)
		}
		var body struct {
			Question store.PilotQuestion `json:"question"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Question.Notifications) != 1 {
			t.Fatal(body.Question.Notifications)
		}
		return body.Question.Notifications[0]
	}
	for i := 0; i < 2; i++ {
		e.hub.processQuestions(e.ctx)
		n := readNotice()
		if sends != 0 || n.State != "pending_config" || n.Reason != store.QuestionFeishuDisabledReason {
			t.Fatal("D3_SWITCH_DISABLED_OUTBOX", sends, n)
		}
	}
	// Simulate the next startup's explicit outbound opt-in; no HTTP runtime toggle exists.
	e.hub.QuestionFeishuEnabled = true
	e.hub.processQuestions(e.ctx)
	n := readNotice()
	if sends != 1 || n.State != "sent" {
		t.Fatal("D3_SWITCH_ENABLED_BOUND_SEND", sends, n)
	}
	e.hub.processQuestions(e.ctx)
	if sends != 1 {
		t.Fatal("D3_SWITCH_DUPLICATE_SEND", sends)
	}
}
