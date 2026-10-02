package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-SPEC-TASK-SHIM-IMPL 任务侧测试格（H6 推论辅助、H7 预拆、H8 看板指派、H9 开工与 Agent 拉任务）。
// Agent 一律经真 /ws/agent 以专属凭据连入；能力令牌一律经真签发口签出（复验方先审一：不插库）。

type shimTaskEnv struct {
	*shimEnv
	specID string
}

func newShimTaskEnv(t *testing.T) *shimTaskEnv {
	t.Helper()
	e := &shimTaskEnv{shimEnv: newShimEnv(t)}
	h := e.hub
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/specs", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("submit", h.HandleConsoleSpecs)))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", h.RequireConsoleAgent("interact", h.HandleConsoleCapability))
	mux.HandleFunc("POST /api/console/specs/{specID}/assist/inference", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("submit", h.HandleConsoleSpecInferenceAssist)))
	mux.HandleFunc("POST /api/agent/console/specs/{specID}/inferences", h.RequireConsoleSpecAgentCapability(func(r *http.Request) string { return ConsoleAgentSpecCapabilityPage(r.PathValue("specID")) }, h.HandleConsoleSpecAgentInference))
	mux.HandleFunc("POST /api/console/specs/{specID}/presplit", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("submit", h.HandleConsoleSpecPresplit)))
	mux.HandleFunc("PATCH /api/console/task-board/tasks/{taskID}", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("submit", h.HandleConsoleTaskBoardAssignment)))
	mux.HandleFunc("GET /api/agent/console/task-driver/specs/{specID}/{revision}/tasks", h.RequireConsoleSpecAgentCapability(func(*http.Request) string { return ConsoleAgentCapabilityTasks }, h.HandleAgentConsoleTasks))
	e.shimEnv.srv.Close()
	e.shimEnv.srv = httptest.NewServer(mux)
	t.Cleanup(e.shimEnv.srv.Close)
	if err := h.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	return e
}

// createSpec zzc 经 HTTP 建一个 spec（editing）。
func (e *shimTaskEnv) createSpec(title string) string {
	e.t.Helper()
	st, body := e.do("POST", "/api/console/specs", e.tokZZC, "", "", specBodyClaimingCreator(e.t, title, "x"))
	if st != 201 {
		e.t.Fatalf("create spec: %d %s", st, body)
	}
	var spec model.ConsoleSpec
	_ = json.Unmarshal([]byte(body), &spec)
	return spec.ID
}

// finalSpec zzc 建 spec → 转给 lb 评审 → 门审（评审 Agent 为 zzc 的 Opus50TS）→ lbl 终稿。全部主体都是垫片投影的新平台用户。
func (e *shimTaskEnv) finalSpec() string {
	e.t.Helper()
	id := e.createSpec("final for tasks")
	if err := e.db.TransferConsoleSpec(e.ctx, id, "zzc", "lb", "review", e.hub.now()); err != nil {
		e.t.Fatalf("transfer to new-platform user lb: %v", err)
	}
	issues := `[{"id":"q1","text":"review","response":"checked","outcome":"false_positive"}]`
	if err := e.db.SaveConsoleSpecGateReview(e.ctx, id, "lbl", model.ConsoleSpecGateReview{ReviewAgentID: e.agentID("zzc", "Opus50TS"), IssuesJSON: issues}, e.hub.now()); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.FinalizeConsoleSpec(e.ctx, id, "lbl", e.hub.now()); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// online 以专属凭据连入，返回收到的信封通道（读到连接结束为止）与连接。
func (e *shimTaskEnv) online(cred, keyID, session string) (<-chan model.Envelope, *websocket.Conn) {
	e.t.Helper()
	conn, st, body := e.dial(cred, "")
	if conn == nil {
		e.t.Fatalf("dial: %d %s", st, body)
	}
	e.t.Cleanup(func() { conn.CloseNow() })
	got := make(chan model.Envelope, 64)
	go func() {
		for {
			_, b, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var env model.Envelope
			if json.Unmarshal(b, &env) == nil {
				got <- env
			}
		}
	}()
	waitSessionOnline(e.t, e.hub, keyID, session)
	return got, conn
}

func waitEnvelope(t *testing.T, ch <-chan model.Envelope, match func(model.Envelope) bool) model.Envelope {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-ch:
			if match(env) {
				return env
			}
		case <-deadline:
			t.Fatal("envelope not delivered within 3s")
		}
	}
}

// H6 推论辅助：新平台下不再 503；信封投到 Agent 的新平台会话（u_zzc／kimi），里面的 console-spec:<id> 令牌
// 让该 Agent 凭专属凭据回写推论成功；别的 Agent 拿这张令牌被拒。
func TestShimH6_InferenceAssistRoundTrip(t *testing.T) {
	e := newShimTaskEnv(t)
	inbox, _ := e.online(e.zKimiCred, "u_zzc", "kimi")
	specID := e.createSpec("assist me")
	kimi := e.agentID("zzc", "Kimi")
	if st, body := e.do("POST", "/api/console/specs/"+specID+"/assist/inference", e.tokZZC, "", "", `{"agent_id":"`+kimi+`"}`); st != 202 {
		t.Fatalf("assist: %d %s", st, body)
	}
	env := waitEnvelope(t, inbox, func(env model.Envelope) bool { return strings.Contains(env.Body, `"callback"`) })
	var payload struct {
		Callback   string `json:"callback"`
		Capability string `json:"capability"`
	}
	if err := json.Unmarshal([]byte(env.Body), &payload); err != nil || payload.Capability == "" || payload.Callback != "/api/agent/console/specs/"+specID+"/inferences" {
		t.Fatalf("assist envelope: %s err=%v", env.Body, err)
	}
	if st, body := e.do("POST", payload.Callback, "", e.lKimiCr, payload.Capability, `{"text":"stolen"}`); st != 403 {
		t.Errorf("another user's Agent with the capability: %d %s", st, body)
	}
	if st, body := e.do("POST", payload.Callback, "", e.zKimiCred, payload.Capability, `{"text":"agent finding"}`); st/100 != 2 {
		t.Fatalf("Agent writes inference back: %d %s", st, body)
	}
}

// H7 预拆：推送到 Agent 的新平台会话；回执按会话端点（u_zzc／kimi）认出投影 Agent、写入任务；别人的会话回执不算。
func TestShimH7_PresplitPushAndReceipt(t *testing.T) {
	e := newShimTaskEnv(t)
	inbox, _ := e.online(e.zKimiCred, "u_zzc", "kimi")
	specID := e.finalSpec()
	kimi := e.agentID("zzc", "Kimi")
	st, body := e.do("POST", "/api/console/specs/"+specID+"/presplit", e.tokZZC, "", "", `{"agent_id":"`+kimi+`"}`)
	if st != 202 {
		t.Fatalf("presplit: %d %s", st, body)
	}
	var started struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal([]byte(body), &started)
	env := waitEnvelope(t, inbox, func(env model.Envelope) bool { return metaString(env.Meta, "request_id") == started.RequestID })
	if !strings.Contains(env.Body, "content_markdown") {
		t.Fatalf("presplit envelope: %s", env.Body)
	}
	result := `{"request_id":"` + started.RequestID + `","tasks":[{"title":"DB","content":"schema","definition_of_done":"migrated","estimated_hours":2,"depends_on_indexes":[]}]}`
	resEnv := model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: result}
	e.hub.handleConsoleSpecPresplitResult(e.ctx, &sessionClient{keyID: "u_lbl", sessionName: "kimi"}, resEnv) // lbl 的同名 Agent：不是被派的那个
	if tasks, _ := e.db.ListConsoleTaskProposals(e.ctx, specID); len(tasks) != 0 {
		t.Fatalf("receipt from another user's session wrote tasks: %+v", tasks)
	}
	e.hub.handleConsoleSpecPresplitResult(e.ctx, &sessionClient{keyID: "u_zzc", sessionName: "kimi"}, resEnv)
	tasks, err := e.db.ListConsoleTaskProposals(e.ctx, specID)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "DB" || tasks[0].ReviewedBy != "zzc" {
		t.Fatalf("tasks after receipt: %+v err=%v", tasks, err)
	}
}

// taskForAssign 在终稿 spec 上建一个待指派任务。
func (e *shimTaskEnv) taskForAssign(specID string) model.ConsoleTaskProposal {
	e.t.Helper()
	rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", specID, 1, false)
	if err != nil {
		e.t.Fatal(err)
	}
	p := model.ConsoleTaskProposal{ID: "np-task-1", SpecID: specID, Revision: 1, ContentSHA256: rev.ContentSHA256, Title: "t1", TaskContent: "work", Status: "planned",
		DependsOnJSON: "[]", AcceptanceJSON: "[]", InputAnchorsJSON: "[]", DefinitionOfDone: "done", ReviewedBy: "zzc", ReviewedAt: e.hub.now(), CreatedAt: e.hub.now()}
	if err := e.db.CreateConsoleTaskProposal(e.ctx, p, "zzc", false); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// H8 看板指派：在线＝已连 /ws/agent 的属主 Agent；成对、不同、在线才收；离线拒；他人 Agent 拒。
func TestShimH8_TaskBoardAssignmentUsesNewPlatformOnline(t *testing.T) {
	e := newShimTaskEnv(t)
	specID := e.finalSpec()
	task := e.taskForAssign(specID)
	kimi, opus, lkimi := e.agentID("zzc", "Kimi"), e.agentID("zzc", "Opus50TS"), e.agentID("lbl", "kimi")
	assign := func(dev, rev string) (int, string) {
		return e.do("PATCH", "/api/console/task-board/tasks/"+task.ID, e.tokZZC, "", "", `{"developer_agent_id":"`+dev+`","reviewer_agent_id":"`+rev+`"}`)
	}
	_, _ = e.online(e.zKimiCred, "u_zzc", "kimi")
	if st, body := assign(kimi, opus); st/100 == 2 {
		t.Fatalf("reviewer offline accepted: %d %s", st, body)
	}
	_, _ = e.online(e.cred, "u_zzc", "opus50ts")
	if st, body := assign(kimi, kimi); st/100 == 2 {
		t.Fatalf("same Agent for both roles accepted: %d %s", st, body)
	}
	_, _ = e.online(e.lKimiCr, "u_lbl", "kimi")
	if st, body := assign(kimi, lkimi); st/100 == 2 {
		t.Fatalf("another user's Agent accepted: %d %s", st, body)
	}
	if st, body := assign(kimi, opus); st/100 != 2 {
		t.Fatalf("both online, owned, distinct: %d %s", st, body)
	}
}

// H9 开工与 Agent 拉任务：dwa 凭据＋console-tasks 令牌拉到分给自己的任务；吊销后同一凭据与令牌被拒。
func TestShimH9_AgentPullsTasksAndRevokeStopsIt(t *testing.T) {
	e := newShimTaskEnv(t)
	specID := e.finalSpec()
	task := e.taskForAssign(specID)
	kimi, opus := e.agentID("zzc", "Kimi"), e.agentID("zzc", "Opus50TS")
	if err := e.db.UpdateConsoleSpecTaskAssignees(e.ctx, task.ID, "zzc", kimi, opus); err != nil {
		t.Fatal(err)
	}
	rev, _ := e.db.ConsoleSpecRevision(e.ctx, "zzc", specID, 1, false)
	if err := e.db.StartConsoleTaskDriver(e.ctx, model.ConsoleTaskDriverStart{SpecID: specID, Revision: 1, ContentSHA256: rev.ContentSHA256, RepositoryName: "dingwei", BranchName: "feature",
		Schedule: "2026-10-03", TaskEndpoint: "/api/agent/console/task-driver", TokenInstructions: "由控制台签发 capability", AnnouncedBy: "zzc", AnnouncedAt: e.hub.now()}); err != nil {
		t.Fatal(err)
	}
	st, capTasks := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilityTasks)
	if st != 200 {
		t.Fatalf("issue console-tasks: %d", st)
	}
	path := "/api/agent/console/task-driver/specs/" + specID + "/1/tasks"
	st, body := e.do("GET", path, "", e.zKimiCred, capTasks, "")
	if st != 200 || !strings.Contains(body, `"proposal_id":"`+task.ID+`"`) {
		t.Fatalf("Agent pulls tasks: %d %s", st, body)
	}
	if _, err := e.svc.Revoke(e.ctx, "zzc", kimi); err != nil {
		t.Fatal(err)
	}
	if st, body := e.do("GET", path, "", e.zKimiCred, capTasks, ""); st != 403 {
		t.Fatalf("after revoke: %d %s", st, body)
	}
}
