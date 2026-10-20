package m8

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func blockerReadyFixture(t *testing.T) (*shimEnv, store.TaskFlowState, store.TaskBlocker) {
	t.Helper()
	e, _, state := r32FlowEnv(t)
	create := fmt.Sprintf(`{"request_id":"card","operation":"create","expected_version":%d,"kind":"technical","where":"waiting","waiting_principal":"user:zzc","expected_input":"facts"}`, state.Version)
	b := blockerDecode(t, blockerHTTP(e, "POST", "", e.zKimiCred, create)).Blocker
	reply := fmt.Sprintf(`{"request_id":"answer","operation":"input","expected_version":%d,"event_id":"answer","body":"PRIVATE ANSWER NEVER IN WAKE","evidence_ref":"git:evidence"}`, b.Version)
	b = blockerDecode(t, blockerHumanHTTP(e, "zzc", b.ID, reply)).Blocker
	state, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, state, b
}
func TestBlockerOfflineOldHelperAndReliableNonExecutableWake(t *testing.T) {
	e, state, b := blockerReadyFixture(t)
	pending, err := e.db.PendingBlockerWakes(e.ctx, "")
	if err != nil || len(pending) != 1 {
		t.Fatal(err, pending)
	}
	e.hub.dispatchBlockerWakes(e.ctx)
	pending, _ = e.db.PendingBlockerWakes(e.ctx, "")
	if len(pending) != 1 {
		t.Fatal("BLOCKER_OFFLINE_INPUT_LOST")
	}
	conn, status, _ := e.dial(e.zKimiCred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	caps := func(blocker bool) {
		t.Helper()
		meta := map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "task_flow_v1": true}
		if blocker {
			meta["blocker_notice_v1"] = true
			meta["blocker_wake_v1"] = true
		}
		if err := writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: meta}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			e.hub.mu.Lock()
			c := e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
			e.hub.mu.Unlock()
			ready := false
			if c != nil {
				c.stateMu.Lock()
				ready = c.deliveryACKV1 && c.blockerNoticeV1 == blocker
				c.stateMu.Unlock()
			}
			if ready {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("BLOCKER_CAPABILITY_HANDSHAKE_NOT_OBSERVED")
	}
	caps(false)
	e.hub.dispatchBlockerWakes(e.ctx)
	pending, _ = e.db.PendingBlockerWakes(e.ctx, "")
	if len(pending) != 1 {
		t.Fatal("BLOCKER_UNKNOWN_CONTROL_SENT_TO_OLD_HELPER")
	}
	caps(true)
	e.hub.dispatchBlockerWakes(e.ctx)
	var env model.Envelope
	for {
		env = readEnvelopeIncludingSystem(t, ctx, conn)
		if metaString(env.Meta, "kind") == "task_blocker_ready" {
			break
		}
	}
	if metaString(env.Meta, "type") != "delivery" || metaString(env.Meta, "delivery_id") == "" || metaString(env.Meta, "signal_id") != pending[0].ID || metaString(env.Meta, "blocker_id") != b.ID || !metaBool(env.Meta, "automatic_recheck") {
		t.Fatal("BLOCKER_WAKE_NOT_RELIABLE_OR_BOUND", env)
	}
	if strings.Contains(env.Body, "PRIVATE ANSWER") || strings.Contains(env.Body, "git:evidence") {
		t.Fatal("BLOCKER_PRIVATE_REPLY_IN_CONTROL_FRAME", env.Body)
	}
	pending, _ = e.db.PendingBlockerWakes(e.ctx, "")
	if len(pending) != 0 {
		t.Fatal("BLOCKER_DISPATCH_NOT_DURABLE")
	}
	e.hub.dispatchBlockerWakes(e.ctx)
	// A normal routing caller cannot produce an otherwise valid self-addressed
	// blocker frame. Removing the dedicated context guard must make this red.
	if err = e.hub.RouteEnvelope(e.ctx, model.Envelope{ID: "forged-control", From: sessionAddress("kimi", AgentKeyID("zzc")), To: sessionAddress("kimi", AgentKeyID("zzc")), Body: "spoof", Meta: map[string]any{"kind": "task_blocker_ready"}}); err == nil {
		t.Fatal("BLOCKER_FORGED_CONTROL_ROUTED")
	}
	if current, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID); err != nil || current.Version != state.Version {
		t.Fatal("BLOCKER_DELIVERY_CHANGED_BUSINESS_STATE", current, err)
	}
}

type blockerWaitingPage struct {
	*store.SQLite
	offline store.TaskFlowState
	last    string
}

func (p *blockerWaitingPage) ConsoleTaskFlowState(ctx context.Context, id string) (store.TaskFlowState, error) {
	if id == p.offline.ID {
		return p.offline, nil
	}
	return p.SQLite.ConsoleTaskFlowState(ctx, id)
}
func (p *blockerWaitingPage) PendingBlockerWakes(ctx context.Context, after string) ([]store.BlockerWake, error) {
	p.last = after
	if after == "" {
		out := make([]store.BlockerWake, 100)
		for i := range out {
			out[i] = store.BlockerWake{ID: fmt.Sprintf("000-offline-%03d", i), TaskID: p.offline.ID, Recipient: p.offline.Developer, AssignmentVersion: p.offline.AssignmentVersion}
		}
		return out, nil
	}
	if after == "000-offline-099" {
		return p.SQLite.PendingBlockerWakes(ctx, "")
	}
	return nil, nil
}
func TestBlockerOfflineFirstPageDoesNotStarveCurrentRecipient(t *testing.T) {
	e, state, _ := blockerReadyFixture(t)
	other, _, err := e.svc.AuthenticateAgent(e.ctx, e.lKimiCr)
	if err != nil {
		t.Fatal(err)
	}
	offline := state
	offline.ID = "offline-task"
	offline.Developer = other.ID
	p := &blockerWaitingPage{SQLite: e.db, offline: offline}
	e.hub.Repo = p
	conn, status, _ := e.dial(e.zKimiCred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	if err = writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "blocker_notice_v1": true, "blocker_wake_v1": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
		e.hub.mu.Unlock()
		capable := false
		if c != nil {
			c.stateMu.Lock()
			capable = c.blockerNoticeV1
			c.stateMu.Unlock()
		}
		if capable {
			break
		}
		time.Sleep(time.Millisecond)
	}
	e.hub.dispatchBlockerWakes(e.ctx)
	for {
		env := readEnvelopeIncludingSystem(t, ctx, conn)
		if metaString(env.Meta, "kind") == "task_blocker_ready" {
			break
		}
	}
	if p.last == "" || p.last == "000-offline-099" {
		t.Fatal("BLOCKER_OFFLINE_PAGE_STARVED_CURRENT_RECIPIENT", p.last)
	}
}

type blockerFinishWindow struct {
	*store.SQLite
	mu   sync.Mutex
	fail bool
	ids  []string
}

func (p *blockerFinishWindow) EnqueueSessionDelivery(ctx context.Context, d model.SessionDelivery) error {
	if err := p.SQLite.EnqueueSessionDelivery(ctx, d); err != nil {
		return err
	}
	p.mu.Lock()
	p.ids = append(p.ids, d.ID)
	p.mu.Unlock()
	return nil
}
func (p *blockerFinishWindow) FinishBlockerWake(ctx context.Context, id, reason string, now time.Time) error {
	p.mu.Lock()
	fail := p.fail
	p.mu.Unlock()
	if fail && reason == "reliable_delivery_enqueued" {
		return errors.New("injected after durable enqueue before outbox mark")
	}
	return p.SQLite.FinishBlockerWake(ctx, id, reason, now)
}
func TestBlockerDurableDeliverySurvivesFinishWindow(t *testing.T) {
	e, _, _ := blockerReadyFixture(t)
	p := &blockerFinishWindow{SQLite: e.db, fail: true}
	e.hub.Repo = p
	conn, status, _ := e.dial(e.zKimiCred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	var c *sessionClient
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.hub.mu.Lock()
		c = e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
		e.hub.mu.Unlock()
		if c != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if c == nil {
		t.Fatal("BLOCKER_SESSION_REGISTRATION_NOT_OBSERVED")
	}
	c.stateMu.Lock()
	c.deliveryACKV1 = true
	c.blockerNoticeV1 = true
	c.blockerWakeV1 = true
	c.stateMu.Unlock()
	if err := e.db.SetSessionEndpointDeliveryACK(e.ctx, AgentKeyID("zzc"), "kimi", true); err != nil {
		t.Fatal(err)
	}
	e.hub.dispatchBlockerWakes(e.ctx)
	pending, err := e.db.PendingBlockerWakes(e.ctx, "")
	if err != nil || len(pending) != 1 {
		t.Fatal("BLOCKER_FINISH_FAILURE_LOST_OUTBOX", err, pending)
	}
	// Model a restart by forgetting every volatile claim, retain real SQLite.
	e.hub.mu.Lock()
	e.hub.envelopeIDs = map[string]string{}
	e.hub.mu.Unlock()
	p.mu.Lock()
	p.fail = false
	p.mu.Unlock()
	e.hub.dispatchBlockerWakes(e.ctx)
	pending, err = e.db.PendingBlockerWakes(e.ctx, "")
	if err != nil || len(pending) != 0 {
		t.Fatal("BLOCKER_FINISH_RETRY_NOT_COMPLETED", err)
	}
	p.mu.Lock()
	ids := append([]string(nil), p.ids...)
	p.mu.Unlock()
	if len(ids) < 2 {
		t.Fatal("BLOCKER_RESTART_WINDOW_NOT_EXERCISED", ids)
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatal("BLOCKER_RESTART_DUPLICATE_DURABLE_DELIVERY", ids)
		}
	}
	d, err := e.db.GetSessionDelivery(e.ctx, ids[0])
	if err != nil || d == nil {
		t.Fatal("BLOCKER_DURABLE_ID_MISSING", err)
	}
}
