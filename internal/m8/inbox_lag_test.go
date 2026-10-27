package m8

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func lagEnvelope(id string, seq int64) model.Envelope {
	age := int64(40 * 60 * 1000)
	raw, _ := json.Marshal(inboxLagSample{Available: true, Schema: "inbox-progress/v1", Instance: strings.Repeat("a", 32), Seq: seq, Count: 1, Oldest: &age, Deliveries: map[string]string{id: "adapter_queued"}})
	return model.Envelope{Body: string(raw), Meta: map[string]any{"type": "inbox_progress_v1"}}
}
func TestInboxLagFreshnessAndEpoch(t *testing.T) {
	h, c, _ := diskEnv(t)
	ctx := context.Background()
	now := time.Now()
	h.NowFunc = func() time.Time { return now }
	id := "dw-dlv_" + strings.Repeat("a", 32)
	if got := h.inboxLagForClient(ctx, c); got["status"] != "unsupported" || got["disk_not_admitted_count"] != nil {
		t.Fatal(got)
	}
	env := lagEnvelope(id, 1)
	h.handleInboxLagControl(ctx, c, env)
	got := h.inboxLagForClient(ctx, c)
	if got["status"] != "ok" || got["disk_not_admitted_count"] != int64(1) || got["oldest_wait_ms"] != int64(2400000) {
		t.Fatal(got)
	}
	if got["deliveries"] != nil {
		t.Fatal("aggregate leaks private message IDs")
	}
	now = now.Add(9 * time.Second)
	h.handleInboxLagControl(ctx, c, env) // duplicate cannot renew
	now = now.Add(time.Second)
	got = h.inboxLagForClient(ctx, c)
	if got["status"] != "stale" || got["disk_not_admitted_count"] != nil {
		t.Fatal(got)
	}
	h.handleInboxLagControl(ctx, c, lagEnvelope(id, 2))
	if h.inboxLagForClient(ctx, c)["status"] != "ok" {
		t.Fatal("fresh sequence rejected")
	}
	replacement := &sessionClient{keyID: c.keyID, sessionName: c.sessionName, metricsAgentID: c.metricsAgentID, metricsGeneration: strings.Repeat("d", 32), connSeq: c.connSeq + 1}
	h.sessionClients[c.keyID][c.sessionName] = replacement
	now = now.Add(time.Second)
	h.handleInboxLagControl(ctx, c, lagEnvelope(id, 3))
	if got := h.inboxLagForClient(ctx, replacement); got["status"] != "unsupported" {
		t.Fatal("old connection repopulated cache", got)
	}
}
func TestInboxLagInvalidAndUnavailableAreUnknown(t *testing.T) {
	for _, mode := range []string{"unbound", "wrong-agent", "spoof-body", "invalid-count", "unavailable", "owner-disabled"} {
		t.Run(mode, func(t *testing.T) {
			h, c, mem := diskEnv(t)
			ctx := context.Background()
			env := lagEnvelope("dw-dlv_"+strings.Repeat("a", 32), 1)
			switch mode {
			case "unbound":
				c.metricsAgentID = ""
			case "wrong-agent":
				c.metricsAgentID = "other"
			case "spoof-body":
				env.Body = strings.Replace(env.Body, `"schema":`, `"agent_id":"other","schema":`, 1)
			case "invalid-count":
				env.Body = strings.Replace(env.Body, `"disk_not_admitted_count":1`, `"disk_not_admitted_count":-1`, 1)
			case "unavailable":
				env.Body = strings.Replace(env.Body, `"available":true`, `"available":false`, 1)
			case "owner-disabled":
				if err := mem.SetUserState(ctx, "zzc", "disabled", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			h.handleInboxLagControl(ctx, c, env)
			got := h.inboxLagForClient(ctx, c)
			if got["status"] == "ok" || got["disk_not_admitted_count"] != nil {
				t.Fatal(got)
			}
		})
	}
}

// Interpose only identity reads, so a connection replacement between the two
// ingress checks is deterministic and needs no production hook or test sleep.
type inboxLagIdentityReads struct {
	identity.Store
	calls      int
	onGetAgent func()
}

func (s *inboxLagIdentityReads) GetAgent(ctx context.Context, id string) (*identity.Agent, error) {
	s.calls++
	if fn := s.onGetAgent; fn != nil {
		s.onGetAgent = nil
		fn()
	}
	return s.Store.GetAgent(ctx, id)
}

func TestInboxLagRejectsOldConnectionBeforeIdentityRead(t *testing.T) {
	h, old, _ := diskEnv(t)
	replacement := &sessionClient{keyID: old.keyID, sessionName: old.sessionName, metricsAgentID: old.metricsAgentID, metricsGeneration: strings.Repeat("d", 32), connSeq: old.connSeq + 1}
	h.sessionClients[old.keyID][old.sessionName] = replacement
	reads := &inboxLagIdentityReads{Store: h.Identity.Store}
	h.Identity.Store = reads
	if !h.handleInboxLagControl(context.Background(), old, lagEnvelope("dw-dlv_"+strings.Repeat("a", 32), 1)) {
		t.Fatal("reserved frame was not consumed")
	}
	if reads.calls != 0 || !old.lastInboxAttempt.IsZero() || old.inboxLag != nil || replacement.inboxLag != nil {
		t.Fatal("old ingress performed identity I/O, charged state or wrote a cache", reads.calls, old.lastInboxAttempt)
	}
}

func TestInboxLagRejectsReplacementDuringIdentityRead(t *testing.T) {
	h, old, _ := diskEnv(t)
	replacement := &sessionClient{keyID: old.keyID, sessionName: old.sessionName, metricsAgentID: old.metricsAgentID, metricsGeneration: strings.Repeat("d", 32), connSeq: old.connSeq + 1}
	reads := &inboxLagIdentityReads{Store: h.Identity.Store, onGetAgent: func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.sessionClients[old.keyID][old.sessionName] = replacement
	}}
	h.Identity.Store = reads
	env := lagEnvelope("dw-dlv_"+strings.Repeat("a", 32), 1)
	h.handleInboxLagControl(context.Background(), old, env)
	if reads.calls != 1 || old.inboxLag != nil || replacement.inboxLag != nil {
		t.Fatal("retired connection wrote a cache after identity recheck", reads.calls)
	}
	h.handleInboxLagControl(context.Background(), replacement, env)
	if replacement.inboxLag == nil {
		t.Fatal("replacement could not publish its own valid sample")
	}
}

func TestInboxLagSenderOwnDeliveryAndCurrentACL(t *testing.T) {
	h, target, mem := diskEnv(t)
	ctx := context.Background()
	e := &agentEnv{t: t, ctx: ctx, hub: h, svc: h.Identity}
	_, sourceID := e.enroll("lag-source", "Source")
	_, leaderID := e.enroll("lag-leader", "Leader")
	source := &sessionClient{keyID: target.keyID, sessionName: "source", metricsAgentID: sourceID, metricsGeneration: strings.Repeat("b", 32)}
	h.sessionClients[source.keyID][source.sessionName] = source
	group, err := h.Identity.CreateGroup(ctx, "zzc", leaderID, "lag")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{sourceID, target.metricsAgentID} {
		if err := h.Identity.JoinGroup(ctx, "zzc", group.Code, id); err != nil {
			t.Fatal(err)
		}
	}
	id := "dw-dlv_" + strings.Repeat("a", 32)
	raw, _ := json.Marshal(model.Envelope{From: sessionAddress(source.sessionName, source.keyID), Body: "private body must not leak"})
	if err := h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: id, KeyID: target.keyID, SessionName: target.sessionName, Envelope: string(raw), SourceMessageID: "source-original", SourceType: "agent_route"}); err != nil {
		t.Fatal(err)
	}
	h.handleInboxLagControl(ctx, target, lagEnvelope(id, 1))
	got := h.deliveryStatusForSender(ctx, source, id)
	if got["ok"] != true || got["phase"] != "adapter_queued" {
		t.Fatal(got)
	}
	aggregate := got["inbox_lag"].(map[string]any)
	for _, field := range []string{"connection_generation", "sample_instance"} {
		if _, leaked := aggregate[field]; leaked {
			t.Fatal("sender query leaked target private epoch", field)
		}
		if h.inboxLagForClient(ctx, target)[field] == nil {
			t.Fatal("View lost its own epoch field", field)
		}
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "private body") {
		t.Fatal("message body leaked")
	}
	if got := h.deliveryStatusForSender(ctx, target, id); got["status"] != "denied" {
		t.Fatal("other sender can inspect delivery", got)
	}
	if err := mem.SetGroupLeaderOnly(ctx, group.Code, "zzc", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := h.deliveryStatusForSender(ctx, source, id); got["status"] != "denied" {
		t.Fatal("leader-only policy bypass", got)
	}
	if err := mem.SetGroupLeaderOnly(ctx, group.Code, "zzc", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.Identity.LeaveGroup(ctx, "zzc", group.Code, sourceID); err != nil {
		t.Fatal(err)
	}
	if got := h.deliveryStatusForSender(ctx, source, id); got["status"] != "denied" {
		t.Fatal("revoked membership retained visibility", got)
	}
}

// Real authenticated sockets exercise capability headers, durable receipts,
// telemetry ingress and correlated read-only control replies together.
func TestInboxLagAuthenticatedWireReceiptAndQuery(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 8*time.Second)
	defer cancel()
	targetCred, targetID := e.enroll("lag-wire-target", "LagTarget")
	group, err := e.svc.CreateGroup(ctx, "zzc", e.agID, "lagwire")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(ctx, "zzc", group.Code, targetID); err != nil {
		t.Fatal(err)
	}
	source, headers := e.dialWithHeaders(e.cred, "")
	defer source.CloseNow()
	if headers.Get("X-DW-Inbox-Progress") != "inbox-progress/v1" {
		t.Fatal(headers)
	}
	target, _, _ := e.dial(targetCred, "")
	defer target.CloseNow()
	key := AgentKeyID("zzc")
	send := func(conn *websocket.Conn, name string, env model.Envelope) {
		t.Helper()
		env.From = name + "#enrolled"
		if env.ID == "" {
			env.ID = randomHex(16)
		}
		if env.To == "" {
			env.To = "workpulse#" + key
		}
		if err := writeEnvelope(ctx, conn, env); err != nil {
			t.Fatal(err)
		}
	}
	readKind := func(conn *websocket.Conn, kind string) model.Envelope {
		t.Helper()
		for {
			env := readEnvelope(t, ctx, conn)
			if metaString(env.Meta, "type") == kind {
				return env
			}
		}
	}
	send(source, "opus50ts", model.Envelope{Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, outboundReceiptRequestKey: true, "outbound_receipt_cap_nonce": strings.Repeat("b", 32)}})
	readKind(source, "outbound_capabilities")
	send(target, "lagtarget", model.Envelope{Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}})
	waitFor(t, time.Second, "target capability", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[key]["lagtarget"]
		e.hub.mu.Unlock()
		return c != nil && e.hub.sessionSupportsDeliveryACK(c)
	})
	send(source, "opus50ts", model.Envelope{To: "lagtarget#" + key, Body: "synthetic queued message", Meta: map[string]any{inboxReceiptRequestKey: true, outboundReceiptNonceKey: strings.Repeat("c", 32)}})
	receipt := readKind(source, "outbound_receipt")
	id := metaString(receipt.Meta, "delivery_id")
	if !inboxDeliveryID.MatchString(id) || metaString(receipt.Meta, "state") != "queued" {
		t.Fatal(receipt)
	}
	delivered := readKind(target, "delivery")
	if delivered.Meta[inboxReceiptRequestKey] != nil || delivered.Meta[outboundReceiptNonceKey] != nil {
		t.Fatal("private receipt fields reached target", delivered.Meta)
	}
	send(target, "lagtarget", model.Envelope{Meta: map[string]any{"type": "delivery_ack", "delivery_id": id, "state": "injected"}})
	sample := lagEnvelope(id, 1)
	send(target, "lagtarget", sample)
	waitFor(t, time.Second, "queued snapshot", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		c := e.hub.sessionClients[key]["lagtarget"]
		return c != nil && c.inboxLag != nil
	})
	send(source, "opus50ts", model.Envelope{Meta: map[string]any{"type": "delivery_status_query_v1", "delivery_id": id, "nonce": strings.Repeat("d", 32)}})
	result := readKind(source, "delivery_status_result_v1")
	status, _ := result.Meta["result"].(map[string]any)
	if status["phase"] != "adapter_queued" {
		t.Fatal("legacy injected ACK became model admission", result)
	}
	// Advancing both independent ingress clocks avoids timing-based test sleeps.
	e.hub.mu.Lock()
	e.hub.sessionClients[key]["lagtarget"].lastInboxAttempt = time.Time{}
	e.hub.sessionClients[key]["opus50ts"].lastInboxQuery = time.Time{}
	e.hub.mu.Unlock()
	var admitted inboxLagSample
	json.Unmarshal([]byte(sample.Body), &admitted)
	admitted.Seq = 2
	admitted.Count = 0
	admitted.Oldest = nil
	admitted.Deliveries[id] = "model_admitted"
	raw, _ := json.Marshal(admitted)
	sample.Body = string(raw)
	send(target, "lagtarget", sample)
	waitFor(t, time.Second, "admitted snapshot", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		return e.hub.sessionClients[key]["lagtarget"].inboxLag.sample.Seq == 2
	})
	send(source, "opus50ts", model.Envelope{Meta: map[string]any{"type": "delivery_status_query_v1", "delivery_id": id, "nonce": strings.Repeat("e", 32)}})
	result = readKind(source, "delivery_status_result_v1")
	status, _ = result.Meta["result"].(map[string]any)
	if status["phase"] != "model_admitted" {
		t.Fatal(result)
	}
}

func TestInboxLagObservationalReceiptPreservesLegacyDirectDelivery(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	cred, id := e.enroll("lag-legacy", "LagLegacy")
	group, err := e.svc.CreateGroup(ctx, "zzc", e.agID, "laglegacy")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(ctx, "zzc", group.Code, id); err != nil {
		t.Fatal(err)
	}
	source, _, _ := e.dial(e.cred, "")
	defer source.CloseNow()
	target, _, _ := e.dial(cred, "")
	defer target.CloseNow()
	// HTTP 101 precedes publication in sessionClients. A legacy target cannot
	// accept an offline durable delivery; send only after its actual admission.
	waitFor(t, 10*time.Second, "legacy target published without delivery ACK", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["laglegacy"]
		return c != nil && c.conn != nil && c.metricsAgentID == id && !e.hub.sessionSupportsDeliveryACK(c)
	})
	if err = writeEnvelope(ctx, source, model.Envelope{ID: "lag-legacy-observed", From: "opus50ts#enrolled", To: "laglegacy#" + AgentKeyID("zzc"), Body: "legacy must still receive once", Meta: map[string]any{inboxReceiptRequestKey: true, outboundReceiptNonceKey: strings.Repeat("a", 32)}}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, target)
	if got.Body != "legacy must still receive once" || got.Meta[inboxReceiptRequestKey] != nil || got.Meta[outboundReceiptNonceKey] != nil {
		t.Fatal(got)
	}
}
