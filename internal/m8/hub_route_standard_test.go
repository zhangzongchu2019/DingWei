package m8

import (
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// TestHubStandardFiveRouteReverseMatrix is the tester regression gate for the
// five public routing flows. The positive paths live in
// TestSessionAddressingFourFlows,
// TestFeishuIngressNewChannelAutoBindingAndReplyToSender, and
// TestCrossMemberAddressingAndReply. This matrix pins the inverse security
// property for each flow so a functional route cannot silently become an
// owner/key bypass.
func TestHubStandardFiveRouteReverseMatrix(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc", Name: "svc", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, member := range []model.Member{
		{OwnerKey: "alice", FeishuOpenID: "ou_alice", Active: true},
		{OwnerKey: "bob", FeishuOpenID: "ou_bob", Active: true},
	} {
		if err := db.UpsertMember(ctx, member); err != nil {
			t.Fatal(err)
		}
	}
	_, aliceKey, err := hub.IssueAPIKey(ctx, "svc", "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, bobKey, err := hub.IssueAPIKey(ctx, "svc", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, aliceKey.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, bobKey.ID, "dev:personal:ou_bob"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.sessionClients[aliceKey.ID] = map[string]*sessionClient{"alice-home": {keyID: aliceKey.ID, sessionName: "alice-home"}}
	hub.sessionClients[bobKey.ID] = map[string]*sessionClient{"bob-home": {keyID: bobKey.ID, sessionName: "bob-home"}}
	hub.mu.Unlock()

	t.Run("feishu_to_session_requires_bound_owner", func(t *testing.T) {
		msg := model.Message{ID: "unbound", ChatEntityID: "dev:personal:ou_attacker", BotChannelID: "dev", ChatType: model.ChatPersonal}
		result, handled, err := hub.dispatchSelector(ctx, msg, "#alice-home steal")
		if err != nil {
			t.Fatal(err)
		}
		if handled || result.Matched {
			t.Fatalf("unbound Feishu sender reached owner route: handled=%v result=%+v", handled, result)
		}
	})

	t.Run("session_to_feishu_rejects_cross_key", func(t *testing.T) {
		err := hub.RouteEnvelope(ctx, model.Envelope{ID: "cross-key-feishu", From: "alice-home#" + aliceKey.ID, To: "ou_alice#" + bobKey.ID + "#UnifiedRobot", Body: "x"})
		if err == nil || err.Error() != "to/from key_id mismatch" {
			t.Fatalf("cross-key Feishu route err=%v", err)
		}
	})

	t.Run("same_owner_session_requires_online_target", func(t *testing.T) {
		err := hub.RouteEnvelope(ctx, model.Envelope{ID: "offline", From: "alice-home#" + aliceKey.ID, To: "alice-offline#" + aliceKey.ID, Body: "x"})
		if err == nil {
			t.Fatal("offline same-owner target was accepted")
		}
	})

	t.Run("session_to_session_rejects_cross_owner", func(t *testing.T) {
		err := hub.RouteEnvelope(ctx, model.Envelope{ID: "cross-owner", From: "alice-home#" + aliceKey.ID, To: "bob-home#" + bobKey.ID, Body: "x"})
		if err == nil {
			t.Fatal("direct cross-owner target was accepted")
		}
	})

	t.Run("reply_to_sender_requires_complete_source_context", func(t *testing.T) {
		for _, meta := range []map[string]any{
			nil,
			{"source_chat_type": string(model.ChatPersonal)},
			{"source_chat_type": string(model.ChatGroup), "source_sender_openid": "ou_alice"},
			{"source_chat_type": "unknown", "source_chat_id": "oc_team"},
		} {
			if address, ok := replySourceAddress(meta); ok || address != "" {
				t.Fatalf("incomplete source context produced reply target %q from %+v", address, meta)
			}
		}
	})
}
