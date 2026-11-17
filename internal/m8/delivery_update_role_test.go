package m8

import (
	"encoding/json"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestDeliveryUpdateForDeliveryPublishesDistinctViewRoles(t *testing.T) {
	h := &Hub{terminals: make(map[string]*terminalState)}
	envelope, err := json.Marshal(model.Envelope{
		ID: "source-message", From: "sender#sender-key", To: "target#target-key", Body: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	d := model.SessionDelivery{
		ID: "delivery-1", KeyID: "target-key", SessionName: "target",
		SourceMessageID: "source-message", Envelope: string(envelope), AckProtocol: 2,
		ProcessingMode: "driver",
	}
	h.publishDeliveryUpdateForDelivery(d, "admission_unknown", "helper_queued", "unconfirmed_retryable", "unconfirmed_retryable")

	for _, tc := range []struct {
		key, session, role string
	}{
		{key: "target-key", session: "target", role: "primary"},
		{key: "sender-key", session: "sender", role: "copy"},
	} {
		replay, _, unsubscribe := h.subscribeSessionEvents(tc.key, tc.session, "role-test")
		unsubscribe()
		if len(replay) != 1 {
			t.Fatalf("%s replay count=%d, want 1", tc.role, len(replay))
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(replay[0]), &event); err != nil {
			t.Fatal(err)
		}
		delivery, _ := event["delivery"].(map[string]any)
		if event["kind"] != "delivery_update" || event["message_id"] != "source-message" ||
			event["observed_delivery_id"] != "delivery-1" || delivery["role"] != tc.role {
			t.Fatalf("%s event=%s", tc.role, replay[0])
		}
	}
}
