package m8

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type ackV2Fixture struct {
	*agentEnv
	conn   *websocket.Conn
	client *sessionClient
	wire   model.Envelope
	stamp  time.Time
}

func newACKV2Fixture(t *testing.T, v2 bool) *ackV2Fixture {
	t.Helper()
	e := newAgentEnv(t)
	conn, status, raw := e.dial(e.cred, "version=0.2.3.14&os=linux")
	if conn == nil {
		t.Fatalf("local helper-peer WS status=%d body=%s", status, raw)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	e.sendEnv(conn, model.Envelope{ID: "ack-caps", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "delivery_ack_v2": v2, "processing_mode": "driver"}})
	var client *sessionClient
	waitFor(t, time.Second, "ACK negotiation", func() bool {
		e.hub.mu.Lock()
		client = e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
		e.hub.mu.Unlock()
		if client == nil {
			return false
		}
		client.stateMu.Lock()
		defer client.stateMu.Unlock()
		return client.helperReady && client.deliveryACKV1 && client.deliveryACKV2 == v2
	})
	now := time.Now().UTC()
	original := model.Envelope{ID: "ack-source", From: "source#" + AgentKeyID("zzc"), To: "opus50ts#" + AgentKeyID("zzc"), Body: "local ACK lifecycle probe", Meta: map[string]any{"type": "delivery", "delivery_id": "ack-wire", "delivery_ack_protocol": 99, "generation": 7}}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.hub.Repo.EnqueueSessionDelivery(e.ctx, model.SessionDelivery{ID: "ack-wire", KeyID: client.keyID, SessionName: client.sessionName, SourceMessageID: original.ID, SourceType: "session_inbound", Envelope: string(body), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	e.hub.drainSessionDeliveries(e.ctx, client)
	wire := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "delivery_id") == "ack-wire" })
	if wire == nil {
		t.Fatal("local Hub did not send delivery")
	}
	return &ackV2Fixture{agentEnv: e, conn: conn, client: client, wire: *wire, stamp: now.Add(-time.Second)}
}

func (f *ackV2Fixture) ack(phase, turn, outcome string, at time.Time) model.Envelope {
	return model.Envelope{ID: "ack-" + phase, To: "workpulse#enrolled", Meta: map[string]any{
		"type": "delivery_ack", "delivery_id": "ack-wire", "ack_protocol": metaInt(f.wire.Meta, "delivery_ack_protocol"),
		"processing_mode": metaString(f.wire.Meta, "processing_mode"), "ack_generation": metaInt(f.wire.Meta, "ack_generation"),
		"state": phase, "occurred_at": at.Format(time.RFC3339Nano), "turn_id": turn, "turn_outcome": outcome,
	}}
}

func (f *ackV2Fixture) sendAndConfirm(phase, turn, outcome string, at time.Time) {
	f.t.Helper()
	f.sendEnv(f.conn, f.ack(phase, turn, outcome, at))
	confirm := f.readEnvUntil(f.conn, time.Second, func(env model.Envelope) bool {
		return metaString(env.Meta, "type") == "delivery_ack_confirm" && metaString(env.Meta, "state") == phase
	})
	if confirm == nil || metaString(confirm.Meta, "delivery_id") != "ack-wire" || metaInt(confirm.Meta, "ack_protocol") != 2 || metaInt(confirm.Meta, "ack_generation") != 1 || metaString(confirm.Meta, "processing_mode") != "driver" {
		f.t.Fatalf("missing/wrong durable phase confirmation: %+v", confirm)
	}
}

func (f *ackV2Fixture) record() *model.SessionDelivery {
	f.t.Helper()
	d, err := f.hub.Repo.GetSessionDelivery(f.ctx, "ack-wire")
	if err != nil || d == nil {
		f.t.Fatalf("delivery record %+v %v", d, err)
	}
	return d
}

func (f *ackV2Fixture) updates() []map[string]any {
	f.t.Helper()
	f.hub.mu.Lock()
	st := f.hub.terminals[terminalKey(f.client.keyID, f.client.sessionName)]
	var raw []string
	if st != nil {
		raw = append(raw, st.eventRing...)
	}
	f.hub.mu.Unlock()
	var updates []map[string]any
	for _, line := range raw {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			f.t.Fatal(err)
		}
		if event["kind"] == "delivery_update" && event["observed_delivery_id"] == "ack-wire" {
			updates = append(updates, event)
		}
	}
	return updates
}

func TestDeliveryACKV2NegotiatedWireOverridesUntrustedProtocolWithoutChangingTaskGeneration(t *testing.T) {
	f := newACKV2Fixture(t, true)
	if metaInt(f.wire.Meta, "delivery_ack_protocol") != 2 || metaInt(f.wire.Meta, "ack_generation") != 1 || metaString(f.wire.Meta, "processing_mode") != "driver" || metaInt(f.wire.Meta, "generation") != 7 {
		t.Fatalf("bad protocol binding or task generation overwritten: %+v", f.wire.Meta)
	}
	if d := f.record(); d.AckProtocol != 2 || d.AckGeneration != 1 || d.ProcessingMode != "driver" || d.State != "sent" {
		t.Fatalf("wire was not bound before send: %+v", d)
	}
}

func TestDeliveryACKV2RealWSCommitsBeforeConfirmAndKeepsTruthfulMaximumStage(t *testing.T) {
	f := newACKV2Fixture(t, true)
	f.sendAndConfirm("queued", "", "", f.stamp)
	if d := f.record(); d.State != "delivered" || d.InjectedAt != nil || d.DeliveredAt == nil {
		t.Fatalf("queued confirmation preceded commit or implied injection: %+v", d)
	}
	// A rejected premature terminal is fenced by a valid duplicate queued ACK.
	f.sendEnv(f.conn, f.ack("processed", "original-turn", "completed", f.stamp.Add(time.Second)))
	f.sendAndConfirm("queued", "", "", f.stamp)
	if d := f.record(); d.State != "delivered" || d.ProcessedAt != nil {
		t.Fatalf("processed skipped admission: %+v", d)
	}
	f.sendAndConfirm("injected", "original-turn", "", f.stamp.Add(time.Second))
	if d := f.record(); d.State != "injected" || d.InjectedAt == nil || !d.InjectedAt.Equal(f.stamp.Add(time.Second)) {
		t.Fatalf("admission time not preserved: %+v", d)
	}
	f.sendAndConfirm("processed", "original-turn", "completed", f.stamp.Add(2*time.Second))
	terminal := f.record()
	if terminal.State != "processed" || terminal.TurnOutcome != "completed" || terminal.ProcessedAt == nil || !terminal.ProcessedAt.Equal(f.stamp.Add(2*time.Second)) {
		t.Fatalf("bad terminal: %+v", terminal)
	}
	f.sendAndConfirm("queued", "", "", f.stamp)
	if d := f.record(); d.State != "processed" || !d.DeliveredAt.Equal(*terminal.DeliveredAt) || !d.InjectedAt.Equal(*terminal.InjectedAt) {
		t.Fatalf("late queued ACK regressed state or reset first times: %+v", d)
	}
	waitFor(t, time.Second, "terminal projection with original turn evidence", func() bool {
		updates := f.updates()
		if len(updates) == 0 {
			return false
		}
		last := updates[len(updates)-1]
		return last["delivery_stage"] == "turn_processed" && last["turn_id"] == "original-turn" && last["turn_outcome"] == "completed" && last["occurred_at"] == f.stamp.Add(2*time.Second).Format(time.RFC3339Nano) && metaInt(last, "ack_protocol") == 2
	})
}

func TestDeliveryACKV2WrongGenerationCannotProduceAdmission(t *testing.T) {
	f := newACKV2Fixture(t, true)
	f.sendAndConfirm("queued", "", "", f.stamp)
	bad := f.ack("injected", "wrong-generation-turn", "", f.stamp)
	bad.Meta["ack_generation"] = 2
	f.sendEnv(f.conn, bad)
	f.sendAndConfirm("queued", "", "", f.stamp)
	if d := f.record(); d.State != "delivered" || d.InjectedAt != nil || d.TurnID != "" {
		t.Fatalf("wrong generation produced admission: %+v", d)
	}
}

func TestDeliveryACKV2AdmissionUnknownDoesNotPublishInjected(t *testing.T) {
	f := newACKV2Fixture(t, true)
	f.sendAndConfirm("queued", "", "", f.stamp)
	f.sendAndConfirm("admission_unknown", "", "", f.stamp)
	if d := f.record(); d.State != "admission_unknown" || d.InjectedAt != nil || d.ProcessedAt != nil {
		t.Fatalf("unknown became model execution/completion: %+v", d)
	}
	waitFor(t, time.Second, "unknown visible without execution claim", func() bool {
		for _, event := range f.updates() {
			if event["delivery_stage"] == "cli_injected" || event["delivery_stage"] == "turn_processed" {
				t.Fatal("unknown published a false model phase")
			}
			if event["reason"] == "model_admission_unknown" && event["cancellable"] == false {
				return true
			}
		}
		return false
	})
}

func TestDeliveryACKV2LegacyProcessedIsOnlyAReceipt(t *testing.T) {
	f := newACKV2Fixture(t, false)
	if metaInt(f.wire.Meta, "delivery_ack_protocol") != 1 || metaString(f.wire.Meta, "processing_mode") != "legacy" {
		t.Fatalf("old helper received new semantics: %+v", f.wire.Meta)
	}
	f.sendEnv(f.conn, model.Envelope{ID: "legacy-ack", To: "workpulse#enrolled", Meta: map[string]any{"type": "delivery_ack", "delivery_id": "ack-wire", "state": "processed"}})
	waitFor(t, time.Second, "legacy transport receipt", func() bool { return f.record().State == "legacy_received" })
	f.hub.handleSessionDeliveryAck(f.ctx, f.client, model.Envelope{ID: "late-delivered", Meta: map[string]any{"type": "delivery_ack", "delivery_id": "ack-wire", "state": "delivered"}})
	if d := f.record(); d.State != "legacy_received" {
		t.Fatalf("late legacy ACK regressed transport terminal: %+v", d)
	}
	if d := f.record(); d.ProcessedAt != nil || d.InjectedAt != nil {
		t.Fatalf("legacy processed was promoted to turn evidence: %+v", d)
	}
	waitFor(t, time.Second, "legacy receipt projection", func() bool {
		for _, event := range f.updates() {
			if event["delivery_stage"] == "cli_injected" || event["delivery_stage"] == "turn_processed" {
				t.Fatal("legacy ACK falsely published a model phase")
			}
			if event["reason"] == "legacy_injection_unknown" {
				return true
			}
		}
		return false
	})
}

// Queued receipt has no turn evidence; later phases bind the same turn.
func ackTestTurn(phase string) string {
	if phase == "queued" {
		return ""
	}
	return "stage-test-turn"
}
func ackTestOutcome(phase string) string {
	if phase == "processed" {
		return "completed"
	}
	return ""
}
