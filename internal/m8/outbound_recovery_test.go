package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOutboundRecoveryReplaysOriginalAcceptanceAndRejectsConflict(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	w.send(model.Envelope{ID: "recovery-cap", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("a", 32)}})
	cap := w.until(func(env model.Envelope) bool { return metaString(env.Meta, "type") == "outbound_capabilities" })
	if !metaBool(cap.Meta, outboundRecoveryKey) || metaString(cap.Meta, "sender_agent_id") != e.agID {
		t.Fatalf("recovery not negotiated: %+v", cap.Meta)
	}
	env := model.Envelope{ID: strings.Repeat("b", 32), To: "opus50ts.zzc#enrolled", Body: "durable body", Meta: map[string]any{"type": "agent_route", outboundReceiptRequestKey: true, outboundRecoveryKey: true, outboundReceiptNonceKey: strings.Repeat("c", 32), "outbound_sender_id": e.agID, "outbound_target_id": e.agID, ipcBodyAMRMetaKey: true}}
	receipt := func() model.Envelope {
		w.send(env)
		return w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_receipt" })
	}
	first := receipt()
	if metaString(first.Meta, "state") != "queued" {
		t.Fatalf("not accepted: %+v", first.Meta)
	}
	db := e.hub.Repo.(*store.SQLite)
	old, err := db.GetOutboundIntent(e.ctx, e.agID, env.ID)
	if err != nil || old == nil || old.DeliveryID != metaString(first.Meta, "delivery_id") {
		t.Fatal("receipt not durable", err)
	}
	delivered := w.until(func(e model.Envelope) bool { return e.ID == env.ID && metaString(e.Meta, "type") == "delivery" })
	for _, key := range []string{outboundRecoveryKey, outboundReceiptNonceKey, "outbound_sender_id", "outbound_target_id", ipcBodyAMRMetaKey} {
		if _, exists := delivered.Meta[key]; exists {
			t.Fatal("sender transport metadata leaked to recipient", key)
		}
	}
	// Existing in-memory envelope claims cannot eat the replay receipt.
	second := receipt()
	if metaString(second.Meta, "delivery_id") != old.DeliveryID {
		t.Fatal("replay changed delivery identity", second.Meta)
	}
	env.Body = "different body"
	conflict := receipt()
	if metaString(conflict.Meta, "reason") != "idempotency_conflict" {
		t.Fatal("changed body was accepted", conflict.Meta)
	}
	env.ID = strings.Repeat("d", 32)
	env.Body = "@other.zzc fanout"
	env.Meta[ipcBodyAMRMetaKey] = true
	rejected := receipt()
	if metaString(rejected.Meta, "state") != "rejected" {
		t.Fatal("AMR silently accepted", rejected.Meta)
	}
	w.send(model.Envelope{ID: "disable-ack", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": false}})
	waitFor(t, time.Second, "non-durable target capability", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
		e.hub.mu.Unlock()
		return !e.hub.sessionSupportsDeliveryACK(c)
	})
	env.ID = strings.Repeat("e", 32)
	env.Body = "ordinary unsupported recipient"
	unsupported := receipt()
	if metaString(unsupported.Meta, "state") != "rejected" || metaString(unsupported.Meta, "reason") != "outbound_target_unsupported" {
		t.Fatal("non-durable target treated as temporary/accepted", unsupported.Meta)
	}
	if found, err := db.GetOutboundIntent(e.ctx, e.agID, env.ID); err != nil || found != nil {
		t.Fatal("unsupported target acquired durable acceptance", found, err)
	}
}

// A new Hub instance has no envelope claim map or receipt waiter from the first
// connection. Acceptance must still be recovered from the SQLite fence.
func TestOutboundRecoveryNewHubRestoresReceiptWithoutSecondDelivery(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	w.send(model.Envelope{ID: "caps-restart", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("1", 32)}})
	w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_capabilities" })
	env := model.Envelope{ID: strings.Repeat("2", 32), To: "opus50ts.zzc#enrolled", Body: "restart body", Meta: map[string]any{"type": "agent_route", outboundReceiptRequestKey: true, outboundRecoveryKey: true, outboundReceiptNonceKey: strings.Repeat("3", 32), "outbound_sender_id": e.agID, "outbound_target_id": e.agID}}
	w.send(env)
	db := e.hub.Repo.(*store.SQLite)
	var original *model.OutboundIntent
	waitFor(t, time.Second, "committed before client confirmation", func() bool { original, _ = db.GetOutboundIntent(e.ctx, e.agID, env.ID); return original != nil })
	w.conn.CloseNow()
	e.hub.Close()
	h := New(db, ConsoleAuthDisabled())
	h.newSessionNotice = false
	h.Identity = e.svc
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	e.hub = h
	e.srv = server
	w2 := subWire(t, e)
	env.Meta[outboundReceiptNonceKey] = strings.Repeat("4", 32)
	w2.send(env)
	got := w2.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_receipt" })
	if metaString(got.Meta, "state") != "queued" || metaString(got.Meta, "delivery_id") != original.DeliveryID || metaString(got.Meta, "nonce") != strings.Repeat("4", 32) {
		t.Fatal("restart lost original receipt", got.Meta)
	}
}

type outboundDeliveryReadFault struct{ *store.SQLite }

func (r *outboundDeliveryReadFault) GetOutboundIntentForDelivery(context.Context, string) (*model.OutboundIntent, error) {
	return nil, errors.New("synthetic temporary read")
}
func TestOutboundRecoveryPhysicalGuardPreservesTemporaryError(t *testing.T) {
	h, db, ctx := newTestHub(t)
	h.Repo = &outboundDeliveryReadFault{db}
	err := h.writeSubAgentDelivery(ctx, &sessionClient{}, &model.SessionDelivery{ID: "fixture"}, model.Envelope{})
	if err == nil || subAgentPermanent(err) || !strings.Contains(err.Error(), "synthetic temporary read") {
		t.Fatalf("temporary read misclassified: %v", err)
	}
}

func TestOutboundRecoveryStableTargetAndRevocationCheckedAtPhysicalWrite(t *testing.T) {
	e := newAgentEnv(t)
	codes, err := e.svc.Store.ListAgentGroups(e.ctx, e.agID)
	if err != nil || len(codes) == 0 {
		t.Fatal(err)
	}
	e.enrollInGroup("target-device", "Receiver", codes[0])
	target, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "receiver")
	if err != nil {
		t.Fatal(err)
	}
	db := e.hub.Repo.(*store.SQLite)
	intent := model.OutboundIntent{SenderID: e.agID, SourceMessageID: strings.Repeat("e", 32), TargetID: target.ID, Digest: strings.Repeat("f", 64)}
	d := model.SessionDelivery{ID: "dw-dlv_" + strings.Repeat("f", 32), KeyID: AgentKeyID("zzc"), SessionName: "receiver", Envelope: "{}", SourceMessageID: intent.SourceMessageID}
	if _, _, err = db.CommitOutboundSessionDelivery(e.ctx, intent, d); err != nil {
		t.Fatal(err)
	}
	original := &sessionClient{metricsAgentID: target.ID, keyID: d.KeyID, sessionName: d.SessionName}
	if err = e.hub.checkOutboundDelivery(e.ctx, original, &d); err != nil {
		t.Fatal("healthy target rejected", err)
	}
	replacement := &sessionClient{metricsAgentID: "new-target", keyID: d.KeyID, sessionName: d.SessionName}
	if err = e.hub.checkOutboundDelivery(e.ctx, replacement, &d); !errors.Is(err, errOutboundDenied) || !subAgentPermanent(err) {
		t.Fatal("same-name replacement inherited delivery", err)
	}
	if _, err = e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
		t.Fatal(err)
	}
	if err = e.hub.checkOutboundDelivery(e.ctx, original, &d); !errors.Is(err, errOutboundDenied) {
		t.Fatal("revoked sender reached target", err)
	}
}

type outboundIdentityCatalogFault struct {
	identity.Store
	fail atomic.Bool
}

func (s *outboundIdentityCatalogFault) GetUser(ctx context.Context, id string) (*identity.User, error) {
	if s.fail.Load() {
		return nil, errors.New("temporary identity read")
	}
	return s.Store.GetUser(ctx, id)
}
func TestOutboundRecoveryDirectoryFailureIsNotUnsupportedProtocol(t *testing.T) {
	e := newAgentEnv(t)
	fault := &outboundIdentityCatalogFault{Store: e.svc.Store}
	e.svc.Store = fault
	w := subWire(t, e)
	cap := func() model.Envelope {
		w.send(model.Envelope{ID: randomHex(16), Meta: map[string]any{"type": "session_capabilities", outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("a", 32)}})
		return w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_capabilities" })
	}
	fault.fail.Store(true)
	partial := cap()
	if !metaBool(partial.Meta, outboundRecoveryKey) || metaBool(partial.Meta, "targets_available") || metaString(partial.Meta, "sender_agent_id") != e.agID {
		t.Fatal("temporary catalog failure disguised as old Hub", partial.Meta)
	}
	fault.fail.Store(false)
	restored := cap()
	if !metaBool(restored.Meta, "targets_available") {
		t.Fatal("catalog did not recover on same socket", restored.Meta)
	}
}

func TestOutboundRecoveryUnsupportedCannotFallThroughToOutputOrLegacySend(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	w.send(model.Envelope{ID: "scope-cap", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("a", 32)}})
	w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_capabilities" })
	for i, typ := range []string{"terminal_output", "agent_route"} {
		meta := map[string]any{"type": typ, outboundReceiptRequestKey: true, outboundRecoveryKey: true, outboundReceiptNonceKey: strings.Repeat("b", 32), "outbound_sender_id": e.agID, "outbound_target_id": e.agID}
		if i == 1 {
			meta[outboundRecoveryKey] = false
		}
		w.send(model.Envelope{ID: strings.Repeat(string(rune('c'+i)), 32), To: "opus50ts.zzc#enrolled", Body: "MUST-NOT-REACH-OUTPUT", Meta: meta})
		response := w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_receipt" })
		if metaString(response.Meta, "state") != "rejected" {
			t.Fatal("unsupported path silently wrote body", response.Meta)
		}
	}
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	if st := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]; st != nil && strings.Contains(st.buffer, "MUST-NOT-REACH-OUTPUT") {
		t.Fatal("rejected intent affected terminal output")
	}
}

func TestOutboundRecoveryFailureNoticeIsVisibleAndBoundToOriginalSender(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	w.send(model.Envelope{ID: "notice-caps", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("a", 32)}})
	w.until(func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_capabilities" })
	db := e.hub.Repo.(*store.SQLite)
	env := model.Envelope{ID: strings.Repeat("a", 32), From: sessionAddress("opus50ts", AgentKeyID("zzc")), To: sessionAddress("losttarget", AgentKeyID("zzc")), Body: "BUSINESS-BODY-NEVER-IN-NOTICE"}
	raw, _ := json.Marshal(env)
	d := model.SessionDelivery{ID: "dw-dlv_" + strings.Repeat("a", 32), KeyID: AgentKeyID("zzc"), SessionName: "losttarget", Envelope: string(raw), SourceMessageID: env.ID, SourceType: "session_inbound"}
	intent := model.OutboundIntent{SenderID: e.agID, TargetID: "lost-target-id", SourceMessageID: env.ID, Digest: strings.Repeat("a", 64)}
	if _, _, err := db.CommitOutboundSessionDelivery(e.ctx, intent, d); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSessionDelivery(e.ctx, d.ID, "dead", "outbound_permission_denied", time.Now()); err != nil {
		t.Fatal(err)
	}
	notice, err := db.GetSessionDelivery(e.ctx, model.FailureNoticeDeliveryID(d.ID))
	if err != nil || notice == nil {
		t.Fatal("permanent failure was invisible", err)
	}
	e.hub.mu.Lock()
	source := e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
	e.hub.mu.Unlock()
	if err = e.hub.checkOutboundDelivery(e.ctx, source, notice); err != nil {
		t.Fatal("lost target stranded Hub status", err)
	}
	replacement := &sessionClient{keyID: source.keyID, sessionName: source.sessionName, metricsAgentID: "new-source-id"}
	if err = e.hub.checkOutboundDelivery(e.ctx, replacement, notice); !errors.Is(err, errOutboundDenied) {
		t.Fatal("source name replacement inherited old notice", err)
	}
	e.hub.drainDurableFailureNotice(e.ctx, d)
	got := w.until(func(e model.Envelope) bool { return metaString(e.Meta, "reason_code") == "outbound_permission_denied" })
	if strings.Contains(got.Body, env.Body) || metaString(got.Meta, "reply_policy") != "no_reply" {
		t.Fatal("failure exposed original business or opened reply loop")
	}
}
