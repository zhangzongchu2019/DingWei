package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func r32FlowEnv(t *testing.T) (*shimEnv, *identity.Group, store.TaskFlowState) {
	t.Helper()
	e := newShimEnv(t)
	h := e.hub
	result := directSpecRequest(t, e, "POST", "", e.zKimiCred, "direct-v1", validSpecBody)
	if result.Code != 201 {
		t.Fatalf("create: %s", result.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(result.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	ctx := e.ctx
	if err := e.db.SubmitConsoleSpecReview(ctx, spec.ID, "zzc", h.now()); err != nil {
		t.Fatal(err)
	}
	if err := e.db.SaveConsoleSpecGateReview(ctx, spec.ID, "zzc", model.ConsoleSpecGateReview{ReviewAgentID: e.agID, IssuesJSON: `[{"id":"R32-review","text":"independent review","response":"checked","outcome":"false_positive"}]`}, h.now()); err != nil {
		t.Fatal(err)
	}
	if err := e.db.FinalizeConsoleSpec(ctx, spec.ID, "zzc", h.now()); err != nil {
		t.Fatal(err)
	}
	p := model.ConsoleTaskProposal{ID: "r32-task", SpecID: spec.ID, Title: "task", TaskContent: "implement", Status: "planned", DependsOnJSON: `[]`, DefinitionOfDone: "passed", CreatedAt: h.now(), ReviewedBy: "zzc"}
	if err := e.db.CreateConsoleTaskProposal(ctx, p, "zzc", false); err != nil {
		t.Fatal(err)
	}
	dev, err := e.svc.Store.FindAgentByName(ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	g, err := e.svc.CreateGroup(ctx, "zzc", e.agID, "R32task")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(ctx, "zzc", g.Code, dev.ID); err != nil {
		t.Fatal(err)
	}
	var state store.TaskFlowState
	err = h.Identity.Store.(identity.TaskAuthorizationStore).WithTaskAuthorization(ctx, "zzc", g.Code, []string{dev.ID, e.agID}, func(_ []identity.Agent, check func() error) error {
		var e2 error
		state, e2 = e.db.AssignConsoleTaskFlow(ctx, p.ID, "zzc", g.Code, dev.ID, e.agID, 1, check, h.now())
		return e2
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, g, state
}
func r32HTTP(e *shimEnv, method, task, credential, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/agent/console/tasks", strings.NewReader(body))
	r.SetPathValue("taskID", task)
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	out := httptest.NewRecorder()
	e.hub.RequireAgentTaskFlow(e.hub.HandleAgentTaskFlow)(out, r)
	return out
}
func TestR32TaskHTTPAuthOwnListStateAndMembershipRevocation(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	if got := r32HTTP(e, "GET", "", "", ""); got.Code != 401 {
		t.Fatalf("missing auth %d", got.Code)
	}
	if got := r32HTTP(e, "GET", "", e.lKimiCr, ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"tasks":[]`) {
		t.Fatalf("OTHER_AGENT_TASK_LEAK: %d %s", got.Code, got.Body.String())
	}
	got := r32HTTP(e, "GET", "", e.zKimiCred, "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"task_id":"r32-task"`) || !strings.Contains(got.Body.String(), `"cases"`) {
		t.Fatalf("list: %d %s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), "last_source_ip") || strings.Contains(got.Body.String(), "credential") {
		t.Fatal("TASK_PRIVATE_IDENTITY_LEAK")
	}
	update := fmt.Sprintf(`{"request_id":"start","expected_version":%d,"operation":"start"}`, state.Version)
	if got = r32HTTP(e, "PATCH", state.ID, e.lKimiCr, update); got.Code != 403 {
		t.Fatalf("OTHER_AGENT_TASK_WRITE: %d", got.Code)
	}
	if got = r32HTTP(e, "PATCH", state.ID, e.zKimiCred, update); got.Code != 200 {
		t.Fatalf("start: %d %s", got.Code, got.Body.String())
	}
	dev, _ := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err := e.svc.LeaveGroup(e.ctx, "zzc", g.Code, dev.ID); err != nil {
		t.Fatal(err)
	}
	if got = r32HTTP(e, "GET", "", e.zKimiCred, ""); got.Code != 200 || strings.Contains(got.Body.String(), `"r32-task"`) {
		t.Fatalf("LEFT_GROUP_TASK_READ: %d %s", got.Code, got.Body.String())
	}
	if got = r32HTTP(e, "PATCH", state.ID, e.zKimiCred, update); got.Code != 403 {
		t.Fatalf("LEFT_GROUP_TASK_WRITE: %d", got.Code)
	}
	after, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
	if err != nil || !after.NeedsReassignment {
		t.Fatalf("INVALID_TASK_NOT_MARKED: %+v %v", after, err)
	}
	pending, _ := e.db.PendingConsoleTaskNotifications(e.ctx)
	ownerNotice := false
	for _, item := range pending {
		if item.Recipient == "owner:zzc" {
			ownerNotice = true
		}
	}
	if !ownerNotice {
		t.Fatal("REASSIGN_OWNER_NOT_NOTIFIED")
	}
}
func TestR32AssignmentPairNamesGroupAndSameOwnerRuling(t *testing.T) {
	e, g, _ := r32FlowEnv(t)
	dev, _ := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	peer, _ := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	pair := []store.ConsoleSpecPresplitTask{{ID: "candidate", Title: "task", Content: "work", DeveloperAgent: "Kimi.zzc", ReviewerAgent: "Opus50TS.zzc"}}
	called := false
	if err := e.hub.withTaskCandidates(e.ctx, "zzc", g.Code, pair, func(items []store.ConsoleSpecPresplitTask, check func() error) error {
		called = true
		if items[0].DeveloperAgentID != dev.ID || items[0].ReviewerAgentID != e.agID {
			t.Fatal("bad identity")
		}
		return check()
	}); err != nil || !called {
		t.Fatalf("SAME_OWNER_DIFFERENT_AGENTS_REJECTED: %v", err)
	}
	pair[0].ReviewerAgent = "Kimi.zzc"
	if err := e.hub.withTaskCandidates(e.ctx, "zzc", g.Code, pair, func([]store.ConsoleSpecPresplitTask, func() error) error {
		t.Fatal("SAME_AGENT_PAIR_ACCEPTED")
		return nil
	}); err == nil {
		t.Fatal("SAME_AGENT_PAIR_ACCEPTED")
	}
	pair[0].ReviewerAgent = "kimi.lb"
	if err := e.hub.withTaskCandidates(e.ctx, "zzc", g.Code, pair, func([]store.ConsoleSpecPresplitTask, func() error) error {
		t.Fatal("CROSS_GROUP_PAIR_ACCEPTED")
		return nil
	}); err == nil {
		t.Fatal("CROSS_GROUP_PAIR_ACCEPTED")
	}
	if err := e.svc.JoinGroup(e.ctx, "lbl", g.Code, peer.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.withTaskCandidates(e.ctx, "zzc", g.Code, pair, func([]store.ConsoleSpecPresplitTask, func() error) error { return nil }); err != nil {
		t.Fatalf("CROSS_USER_SAME_GROUP_REJECTED: %v", err)
	}
}
func TestR32TaskNotificationOfflineQueueOldHelperAndReliableFrame(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	items, err := e.db.PendingConsoleTaskNotifications(e.ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("offline queue %v %v", items, err)
	}
	e.hub.dispatchTaskNotifications(e.ctx)
	again, _ := e.db.PendingConsoleTaskNotifications(e.ctx)
	if len(again) != 2 {
		t.Fatal("OFFLINE_NOTICE_LOST")
	}
	conn, status, _ := e.dial(e.zKimiCred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	// A legacy peer advertises delivery ACK only. Task payload remains in outbox.
	if err = writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	dev, _ := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")][dev.NameLC()]
		e.hub.mu.Unlock()
		if c != nil && e.hub.sessionSupportsDeliveryACK(c) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.hub.dispatchTaskNotifications(e.ctx)
	again, _ = e.db.PendingConsoleTaskNotifications(e.ctx)
	if len(again) != 2 {
		t.Fatal("OLD_HELPER_TASK_PAYLOAD_SENT")
	}
	if err = writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "task_flow_v1": true}}); err != nil {
		t.Fatal(err)
	}
	var delivery model.Envelope
	for {
		delivery = readEnvelopeIncludingSystem(t, ctx, conn)
		if metaString(delivery.Meta, "kind") == "task_assigned" {
			break
		}
	}
	if metaString(delivery.Meta, "type") != "delivery" || metaString(delivery.Meta, "delivery_id") == "" || !strings.Contains(delivery.Body, "r32-task") {
		t.Fatalf("TASK_NOTICE_NOT_RELIABLE: %+v", delivery)
	}
	if err = e.hub.RouteEnvelope(e.ctx, model.Envelope{ID: "spoof", Meta: map[string]any{"kind": "task_assigned"}}); err == nil {
		t.Fatal("FORGED_TASK_NOTIFICATION_ACCEPTED")
	}
}
