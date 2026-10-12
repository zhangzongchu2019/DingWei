package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConfirmedOutboundQueuesBeforePositiveReceipt(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "receipt-fixture", Name: "receipt", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "receipt-fixture", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	source := dialSession(t, ctx, srv.URL, "source", key.ID, secret)
	defer source.Close(websocket.StatusNormalClosure, "done")
	target := dialSession(t, ctx, srv.URL, "target", key.ID, secret)
	defer target.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, target, model.Envelope{ID: "target-caps", From: sessionAddress("target", key.ID), To: sessionAddress("workpulse", key.ID), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "delivery capability", func() bool {
		hub.mu.Lock()
		c := hub.sessionClients[key.ID]["target"]
		hub.mu.Unlock()
		return c != nil && hub.sessionSupportsDeliveryACK(c)
	})
	if err := writeEnvelope(ctx, source, model.Envelope{ID: "source-confirmed-caps", From: sessionAddress("source", key.ID), To: sessionAddress("workpulse", key.ID), Meta: map[string]any{"type": "session_capabilities", outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": "abcdef0123456789abcdef0123456789"}}); err != nil {
		t.Fatal(err)
	}
	capability := readEnvelope(t, ctx, source)
	if metaString(capability.Meta, "type") != "outbound_capabilities" || metaString(capability.Meta, "nonce") != "abcdef0123456789abcdef0123456789" || !metaBool(capability.Meta, "durable_agent_queue") {
		t.Fatal("capability negotiation failed")
	}
	env := model.Envelope{ID: "outbound-fixture", From: sessionAddress("source", key.ID), To: sessionAddress("target", key.ID), Body: "synthetic reply", Meta: map[string]any{outboundReceiptRequestKey: true, outboundReceiptNonceKey: "0123456789abcdef0123456789abcdef", "in_reply_to": "fixture-original", "kind": "reply"}}
	if err := writeEnvelope(ctx, source, env); err != nil {
		t.Fatal(err)
	}
	receipt := readEnvelope(t, ctx, source)
	if receipt.From != sessionAddress("workpulse", key.ID) || receipt.Body != "" || metaString(receipt.Meta, "state") != "queued" || !metaBool(receipt.Meta, "durable") || metaString(receipt.Meta, "source_message_id") != env.ID {
		t.Fatalf("unexpected receipt: %#v", receipt.Meta)
	}
	d, err := db.GetSessionDelivery(ctx, metaString(receipt.Meta, "delivery_id"))
	if err != nil || d == nil || d.SourceMessageID != env.ID {
		t.Fatal("receipt precedes durable commit")
	}
	delivered := readEnvelope(t, ctx, target)
	if _, leaked := delivered.Meta[outboundReceiptRequestKey]; leaked {
		t.Fatal("sender control flag leaked to target")
	}
	if _, leaked := delivered.Meta[outboundReceiptNonceKey]; leaked {
		t.Fatal("private receipt nonce leaked to target")
	}
	if metaString(receipt.Meta, "nonce") != "0123456789abcdef0123456789abcdef" {
		t.Fatal("nonce correlation lost")
	}
	if metaString(delivered.Meta, "in_reply_to") != "fixture-original" {
		t.Fatal("reply correlation lost")
	}
}

func TestConfirmedOutboundRejectsLegacyTargetWithoutWritingBody(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "receipt-legacy", Name: "receipt", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "receipt-legacy", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	source := dialSession(t, ctx, srv.URL, "source", key.ID, secret)
	defer source.Close(websocket.StatusNormalClosure, "done")
	target := dialSession(t, ctx, srv.URL, "legacy", key.ID, secret)
	defer target.Close(websocket.StatusNormalClosure, "done")
	for _, to := range []string{sessionAddress("legacy", key.ID), sessionAddress("missing", key.ID), "invalid address", "ou_fixture#" + key.ID} {
		env := model.Envelope{ID: randomHex(16), From: sessionAddress("source", key.ID), To: to, Body: "must not silently direct-write", Meta: map[string]any{outboundReceiptRequestKey: true, outboundReceiptNonceKey: "0123456789abcdef0123456789abcdef"}}
		if err := writeEnvelope(ctx, source, env); err != nil {
			t.Fatal(err)
		}
		receipt := readEnvelope(t, ctx, source)
		if metaString(receipt.Meta, "state") != "rejected" || metaBool(receipt.Meta, "durable") || receipt.Body != "" || metaString(receipt.Meta, "source_message_id") != env.ID {
			t.Fatal("invalid rejection")
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, _, err := target.Read(readCtx); err == nil || readCtx.Err() != context.DeadlineExceeded {
		t.Fatal("rejected request wrote a target frame or closed its connection")
	}
}

func TestConfirmedOutboundValidationDoesNotOptInOnStringFlag(t *testing.T) {
	if outboundReceiptRequested(model.Envelope{Meta: map[string]any{outboundReceiptRequestKey: "true"}}) {
		t.Fatal("string flag enabled confirmation")
	}
	if outboundReceiptRequested(model.Envelope{}) {
		t.Fatal("legacy request changed")
	}
}
