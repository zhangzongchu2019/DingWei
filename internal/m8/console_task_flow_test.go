package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
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

func TestR32MCPThroughRealIPCAndAgentHTTP(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent/console/tasks", e.hub.RequireAgentTaskFlow(e.hub.HandleAgentTaskFlow))
	mux.HandleFunc("PATCH /api/agent/console/tasks/{taskID}", e.hub.RequireAgentTaskFlow(e.hub.HandleAgentTaskFlow))
	server := httptest.NewServer(mux)
	defer server.Close()
	python := `import asyncio,json,os,tempfile
from sessionhelper.ipc_listener import serve_ipc
from sessionhelper.task_flow import task_request
from sessionhelper import mcp_shim
async def main():
 with tempfile.TemporaryDirectory(prefix="r32-ipc-") as root:
  socket=root+"/s";ready=asyncio.Event();mcp_shim._IPC_SOCK=socket
  async def task(args):
   return await asyncio.to_thread(task_request,os.environ['R32_TEST_WS'],{'Authorization':'Bearer '+os.environ['R32_TEST_AGENT']},args)
  async def unused(*args):raise AssertionError('send path must not run')
  server=asyncio.create_task(serve_ipc(socket,unused,bound=ready,task_handler=task))
  await asyncio.wait_for(ready.wait(),3)
  try:
   found=await asyncio.to_thread(mcp_shim._dw_task_list,{'limit':10})
   assert found['status']=='ok',found
   item=found['tasks'][0];assert item['task_id']=='r32-task'
   update={'task_id':item['task_id'],'expected_version':item['version'],'request_id':'mcp-stable','operation':'start'}
   result=await asyncio.to_thread(mcp_shim._dw_task_update,update)
   assert result['status']=='ok',result
   again=await asyncio.to_thread(mcp_shim._dw_task_update,update)
   assert again['version']==result['version'],again
   current=await asyncio.to_thread(mcp_shim._dw_task_list,{})
   assert current['tasks'][0]['status']=='in_progress',current
   print('R32_MCP_IPC_HTTP_OK')
  finally:
   server.cancel()
   try:await server
   except asyncio.CancelledError:pass
asyncio.run(main())`
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-c", python)
	source, _ := filepath.Abs("../../tools/sessionhelper")
	command.Env = append(os.Environ(), "PYTHONPATH="+source, "R32_TEST_WS="+strings.Replace(server.URL, "http", "ws", 1), "R32_TEST_AGENT="+e.zKimiCred)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP_IPC_HTTP_CHAIN_FAILED: %v %s", err, output)
	}
	if !strings.Contains(string(output), "R32_MCP_IPC_HTTP_OK") {
		t.Fatalf("missing receipt: %s", output)
	}
	after, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
	if err != nil || after.Version != state.Version+1 {
		t.Fatalf("MCP state %+v %v", after, err)
	}
}

func r32UsePGIdentity(t *testing.T, e *shimEnv) *identity.PGStore {
	t.Helper()
	pool := pgtest.Pool(t)
	if _, err := migrate.Migrate(e.ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal(err)
	}
	access, err := repo.NewAccessCtx("test", "r32-recovery")
	if err != nil {
		t.Fatal(err)
	}
	original := e.svc.Store
	pg := identity.NewPGStore(pool)
	users, err := original.ListUsers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Type == "" {
			u.Type = identity.UserMember
		}
		if err = pg.CreateUser(e.ctx, u); err != nil {
			t.Fatal(err)
		}
		agents, e2 := original.ListAgents(e.ctx, u.UserIDLC())
		if e2 != nil {
			t.Fatal(e2)
		}
		for _, a := range agents {
			_, err = pool.Exec(e.ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,group_code,origin,state,credential_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.ID, a.OwnerLC, a.Name, a.NameLC(), a.GroupID, a.Origin, a.State, a.CredentialHash, a.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	groups, err := original.ListGroups(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		_, err = pool.Exec(e.ctx, access, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,additions_closed,leader_only,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, g.Code, g.ShortName, g.CreatorLC, g.LeaderAgentID, g.AdditionsClosed, g.LeaderOnly, g.CreatedAt, g.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		members, e2 := original.ListGroupMembers(e.ctx, g.Code)
		if e2 != nil {
			t.Fatal(e2)
		}
		for _, m := range members {
			if _, err = pool.Exec(e.ctx, access, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$3)`, m.Code, m.AgentID, m.CreatedAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.svc.Store = pg
	return pg
}
func TestR32TaskRecoveryAfterPGCommitBeforeSQLiteMark(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	pg := r32UsePGIdentity(t, e)
	dev, _ := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err := pg.LeaveGroup(e.ctx, g.Code, dev.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	// Simulate the crash window: PG committed, original Hub never received an
	// Agent/user request and has not yet mirrored anything into SQLite.
	before, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
	if err != nil || before.NeedsReassignment {
		t.Fatalf("fixture crossed crash window: %+v %v", before, err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	restarted := New(e.db, ConsoleAuthDisabled())
	restarted.Identity = identity.NewService(pg, nil, nil)
	defer restarted.Close()
	restarted.StartTaskFlow(ctx)
	deadline := time.Now().Add(5 * time.Second)
	marked := false
	for time.Now().Before(deadline) {
		after, e2 := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
		if e2 == nil && after.NeedsReassignment {
			marked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !marked {
		t.Fatal("RECOVERY_REQUIRED_USER_REQUEST")
	}
	// Re-running reconciliation after another restart must not add another
	// logical owner notification (the outbox key is durable).
	restarted.reconcileTaskFlow(ctx)
	restarted.reconcileTaskFlow(ctx)
	items, err := e.db.PendingConsoleTaskNotifications(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := 0
	for _, item := range items {
		if item.Recipient == "owner:zzc" {
			owner++
		}
	}
	if owner != 1 {
		t.Fatalf("RECOVERY_OWNER_NOTICE_DUPLICATED_OR_LOST: %d", owner)
	}
	if got := r32HTTP(e, "PATCH", state.ID, e.zKimiCred, fmt.Sprintf(`{"request_id":"after-recovery","expected_version":%d,"operation":"start"}`, before.Version)); got.Code != 403 {
		t.Fatalf("RECOVERY_LEFT_WRITE_AUTHORITY: %d", got.Code)
	}
}

func TestR32PublicSpecCannotBypassTaskReadRevocation(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	all, err := e.db.ListAllConsoleTaskProposals(e.ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("fixture %d %v", len(all), err)
	}
	request := httptest.NewRequest("GET", "/api/specs/shared/public", nil)
	request.SetPathValue("specID", all[0].SpecID)
	response := httptest.NewRecorder()
	e.hub.HandleConsoleSpec(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "r32-task") {
		t.Fatalf("PUBLIC_SPEC_LEAKED_R32_TASK: status=%d body=%s", response.Code, response.Body.String())
	}

	filtered, err := e.hub.filterTaskFlowProjection(request, all)
	if err != nil || len(filtered) != 0 {
		t.Fatalf("PUBLIC_SPEC_LEAKED_R32_TASK: %d %v", len(filtered), err)
	}
	ownerRequest := request.WithContext(context.WithValue(request.Context(), consolePrincipalContextKey{}, "zzc"))
	filtered, err = e.hub.filterTaskFlowProjection(ownerRequest, all)
	if err != nil || len(filtered) != 1 {
		t.Fatalf("owner projection %d %v", len(filtered), err)
	}
	if err = e.db.InvalidateConsoleTaskFlow(e.ctx, state.ID, state.Version, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	assigned := context.WithValue(request.Context(), consolePrincipalContextKey{}, "zzc")
	assigned = context.WithValue(assigned, consoleSpecActorContextKey{}, consoleSpecActor{ID: state.Developer, Kind: "agent"})
	filtered, err = e.hub.filterTaskFlowProjection(request.WithContext(assigned), all)
	if err != nil || len(filtered) != 0 {
		t.Fatalf("LEGACY_SPEC_CAPABILITY_LEAKED_REVOKED_TASK: %d %v", len(filtered), err)
	}
}
