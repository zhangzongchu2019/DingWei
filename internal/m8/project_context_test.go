package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func pcRequest(h *Hub, cred, query string, change func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/agent/console/project-context?"+query, nil)
	if cred != "" {
		r.Header.Set("Authorization", "Bearer "+cred)
	}
	if change != nil {
		change(r)
	}
	out := httptest.NewRecorder()
	h.HandleProjectContext(out, r)
	return out
}
func pcCursor(t *testing.T, out *httptest.ResponseRecorder) string {
	t.Helper()
	var v struct {
		Meta struct {
			Next string `json:"next_cursor"`
		}
	}
	if e := json.Unmarshal(out.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v.Meta.Next
}
func pcAddTask(t *testing.T, e *shimEnv, g *identity.Group, id string, base store.TaskFlowState) {
	t.Helper()
	old, er := e.db.ListConsoleTaskFlow(e.ctx, base.Developer)
	if er != nil || len(old) == 0 {
		t.Fatal(er)
	}
	p := old[0].ConsoleTaskProposal
	p.ID = id
	p.Version = 0
	p.CreatedAt = e.hub.now()
	p.Status = "planned"
	if er = e.db.CreateConsoleTaskProposal(e.ctx, p, "zzc", false); er != nil {
		t.Fatal(er)
	}
	er = e.svc.Store.(identity.TaskAuthorizationStore).WithTaskAuthorization(e.ctx, "zzc", g.Code, []string{base.Developer, base.Reviewer}, func(_ []identity.Agent, check func() error) error {
		_, x := e.db.AssignConsoleTaskFlow(e.ctx, id, "zzc", g.Code, base.Developer, base.Reviewer, 1, check, e.hub.now())
		return x
	})
	if er != nil {
		t.Fatal(er)
	}
}
func TestProjectContextScopesAndPrivateProjection(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	for _, scope := range []string{"tasks", "sessions", "config", "all"} {
		out := pcRequest(e.hub, e.zKimiCred, "scope="+scope, nil)
		if out.Code != 200 {
			t.Fatalf("PC_SCOPE_FAILED %s %d %s", scope, out.Code, out.Body)
		}
		if out.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("PC_CACHEABLE")
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		expected := 2
		if scope == "all" {
			expected = 4
		}
		if len(data) != expected {
			t.Fatal("PC_EXTRA_SCOPE", data)
		}
		for _, needle := range []string{"credential", "input_anchors_json", "content_markdown", "review_agent_id", "group_code", "client_ip", "datad", "secret", "definition_of_done"} {
			if strings.Contains(out.Body.String(), needle) {
				t.Fatal("PC_PRIVATE_FIELD_LEAKED", needle, out.Body)
			}
		}
	}
}
func TestProjectContextAuthenticationAndQueryMatrix(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	tests := []struct {
		name, cred, q string
		change        func(*http.Request)
		status        int
	}{
		{"missing", "", "scope=all", nil, 401}, {"forged", "not-a-credential", "scope=all", nil, 403},
		{"mixed", e.zKimiCred, "scope=all", func(r *http.Request) { r.Header.Set("Cookie", "x=y") }, 401},
		{"double", e.zKimiCred, "scope=all", func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+e.zKimiCred) }, 401},
		{"spoof", e.zKimiCred, "scope=all", func(r *http.Request) { r.Header.Set("X-Agent-ID", e.agID) }, 400},
		{"no-scope", e.zKimiCred, "", nil, 400}, {"unknown-scope", e.zKimiCred, "scope=private", nil, 400},
		{"duplicate", e.zKimiCred, "scope=all&scope=tasks", nil, 400}, {"selector", e.zKimiCred, "scope=all&owner=lbl", nil, 400},
		{"limit-zero", e.zKimiCred, "scope=all&limit=0", nil, 400}, {"limit-large", e.zKimiCred, "scope=all&limit=101", nil, 400},
		{"limit-float", e.zKimiCred, "scope=all&limit=1.5", nil, 400}, {"bad-cursor", e.zKimiCred, "scope=all&cursor=broken", nil, 400},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			out := pcRequest(e.hub, v.cred, v.q, v.change)
			if out.Code != v.status {
				t.Fatalf("PC_AUTH_OR_QUERY_BYPASS got %d want %d %s", out.Code, v.status, out.Body)
			}
		})
	}
}
func TestProjectContextNoDirectoryAndCrossOwnerMembership(t *testing.T) {
	e, g, _ := r32FlowEnv(t)
	peer, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	// Cross-owner peers are permitted only on actual shared memberships.
	before := pcRequest(e.hub, e.zKimiCred, "scope=sessions", nil)
	if strings.Contains(before.Body.String(), "kimi.lb") {
		t.Fatal("PC_CROSS_GROUP_LEAK")
	}
	if err = e.svc.Store.JoinGroup(e.ctx, g.Code, peer.ID, "lbl", e.hub.now()); err != nil {
		t.Fatal(err)
	}
	for _, a := range []*identity.Agent{peer} {
		key := AgentKeyID(a.OwnerLC)
		if err = e.db.UpsertSessionEndpoint(e.ctx, model.SessionEndpoint{KeyID: key, SessionName: a.NameLC(), Active: true}); err != nil {
			t.Fatal(err)
		}
		e.hub.mu.Lock()
		if e.hub.sessionClients[key] == nil {
			e.hub.sessionClients[key] = map[string]*sessionClient{}
		}
		e.hub.sessionClients[key][a.NameLC()] = &sessionClient{metricsAgentID: a.ID}
		e.hub.mu.Unlock()
	}
	joined := pcRequest(e.hub, e.zKimiCred, "scope=sessions", nil)
	if joined.Code != 200 || !strings.Contains(joined.Body.String(), "kimi.lb") {
		t.Fatal("PC_SHARED_PEER_MISSING", joined.Code, joined.Body)
	}

	e.hub.mu.Lock()
	e.hub.sessionClients[AgentKeyID(peer.OwnerLC)][peer.NameLC()].metricsAgentID = "other-id"
	e.hub.mu.Unlock()
	stale := pcRequest(e.hub, e.zKimiCred, "scope=sessions", nil)
	if strings.Contains(stale.Body.String(), "kimi.lb") {
		t.Fatal("PC_ONLINE_IDENTITY_NOT_BOUND")
	}
	e.hub.mu.Lock()
	e.hub.sessionClients[AgentKeyID(peer.OwnerLC)][peer.NameLC()].metricsAgentID = peer.ID
	e.hub.mu.Unlock()
	if err = e.db.SetSessionNoDirectory(e.ctx, AgentKeyID(peer.OwnerLC), peer.NameLC(), true); err != nil {
		t.Fatal(err)
	}
	hidden := pcRequest(e.hub, e.zKimiCred, "scope=sessions", nil)
	if strings.Contains(hidden.Body.String(), "kimi.lb") {
		t.Fatal("PC_NODIRECTORY_BYPASS")
	}
}
func TestProjectContextPaginationBindingAndDrift(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	pcAddTask(t, e, g, "r32-task-2", state)
	first := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1", nil)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body)
	}
	cursor := pcCursor(t, first)
	if cursor == "" {
		t.Fatal("PC_TRUNCATION_WITHOUT_CURSOR")
	}
	second := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1&cursor="+url.QueryEscape(cursor), nil)
	if second.Code != 200 || pcCursor(t, second) != "" || !strings.Contains(second.Body.String(), "r32-task-2") {
		t.Fatal("PC_PAGE_LOST", second.Code, second.Body)
	}
	other := pcRequest(e.hub, e.lKimiCr, "scope=tasks&limit=1&cursor="+url.QueryEscape(cursor), nil)
	if other.Code != 403 {
		t.Fatal("PC_CURSOR_ACTOR_UNBOUND", other.Code)
	}
	wrongLimit := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=2&cursor="+url.QueryEscape(cursor), nil)
	if wrongLimit.Code != 400 {
		t.Fatal("PC_CURSOR_LIMIT_UNBOUND")
	}
	// A real task mutation invalidates the old continuation, rather than returning cached data.
	_, err := e.db.UpdateConsoleTaskFlow(e.ctx, state.ID, state.Developer, store.TaskFlowUpdate{RequestID: "start-context", ExpectedVersion: state.Version, Operation: "start"}, func() error { return nil }, e.hub.now())
	if err != nil {
		t.Fatal(err)
	}
	changed := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1&cursor="+url.QueryEscape(cursor), nil)
	if changed.Code != 409 {
		t.Fatal("PC_CURSOR_AUTH_DRIFT_IGNORED", changed.Code, changed.Body)
	}
}
func TestProjectContextRevokedAndUnassignedNoData(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	if err := e.db.InvalidateConsoleTaskFlow(e.ctx, state.ID, state.Version, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	out := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 200 || strings.Contains(out.Body.String(), `"task_id":"`+state.ID+`"`) {
		t.Fatal("PC_NEEDS_REASSIGNMENT_LEAK", out.Code, out.Body)
	}
	dev, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Revoke(e.ctx, "zzc", dev.ID); err != nil {
		t.Fatal(err)
	}
	denied := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if denied.Code != 403 {
		t.Fatal("PC_REVOKED_ACCESS", denied.Code)
	}
}

type contextFailureRepo struct {
	store.Repository
	real      projectContextRepo
	during    func()
	failAfter bool
}

func (x contextFailureRepo) ProjectContextCandidates(ctx context.Context, a, b string, n int) ([]store.ContextCandidate, error) {
	p, e := x.real.ProjectContextCandidates(ctx, a, b, n)
	if x.during != nil {
		x.during()
	}
	return p, e
}
func (x contextFailureRepo) WithProjectContextSnapshot(ctx context.Context, a string, c []store.ContextCandidate, f func(*store.ContextSnapshot) error) error {
	e := x.real.WithProjectContextSnapshot(ctx, a, c, f)
	if e == nil && x.failAfter {
		return errors.New("snapshot finish failed")
	}
	return e
}
func TestProjectContextCandidateRaceAndSnapshotFailureClosed(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	real := e.db
	e.hub.Repo = contextFailureRepo{Repository: real, real: real, during: func() {
		if er := real.InvalidateConsoleTaskFlow(e.ctx, state.ID, state.Version, e.hub.now()); er != nil {
			t.Fatal(er)
		}
	}}
	out := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 409 || strings.Contains(out.Body.String(), "spec_id") {
		t.Fatal("PC_CANDIDATE_STATE_STALE", out.Code, out.Body)
	}
	e.hub.Repo = contextFailureRepo{Repository: real, real: real, failAfter: true}
	out = pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 503 || strings.Contains(out.Body.String(), "spec_id") {
		t.Fatal("PC_PARTIAL_BODY_ON_FAILURE", out.Code, out.Body)
	}
}
func TestProjectContextCursorExpiry(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	pcAddTask(t, e, g, "r32-task-2", state)
	now := e.hub.now()
	e.hub.NowFunc = func() time.Time { return now }
	first := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1", nil)
	cursor := pcCursor(t, first)
	if cursor == "" {
		t.Fatal(first.Body)
	}
	now = now.Add(6 * time.Minute)
	out := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1&cursor="+url.QueryEscape(cursor), nil)
	if out.Code != 400 {
		t.Fatal("PC_CURSOR_NO_EXPIRY")
	}
}

func TestProjectContextResponseBudgetAndOversizedItem(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	add := func(id string, size int) {
		t.Helper()
		rows, err := e.db.ListConsoleTaskFlow(e.ctx, state.Developer)
		if err != nil {
			t.Fatal(err)
		}
		p := rows[0].ConsoleTaskProposal
		p.ID = id
		p.Title = strings.Repeat("X", size)
		p.CreatedAt = e.hub.now()
		p.Status = "planned"
		if err = e.db.CreateConsoleTaskProposal(e.ctx, p, "zzc", false); err != nil {
			t.Fatal(err)
		}
		_, err = e.db.AssignConsoleTaskFlow(e.ctx, id, "zzc", g.Code, state.Developer, state.Reviewer, 1, func() error { return nil }, e.hub.now())
		if err != nil {
			t.Fatal(err)
		}
	}
	add("z-large-A", 140000)
	add("z-large-B", 140000)
	first := pcRequest(e.hub, e.zKimiCred, "scope=all&limit=3", nil)
	if first.Code != 200 || first.Body.Len() > contextMaxBytes || pcCursor(t, first) == "" {
		t.Fatal("PC_BODY_LIMIT_OR_CONTINUATION_MISSING", first.Code, first.Body.Len())
	}
	next := pcRequest(e.hub, e.zKimiCred, "scope=all&limit=3&cursor="+url.QueryEscape(pcCursor(t, first)), nil)
	if next.Code != 200 || next.Body.Len() > contextMaxBytes || !strings.Contains(next.Body.String(), `"task_id":"z-large-B"`) {
		t.Fatal("PC_SHRINK_SKIPPED_TASK", next.Code, next.Body.Len())
	}
	add("zz-oversize", 300000)
	cursor := contextCursor{Actor: state.Developer, Scope: "tasks", Limit: 1, TaskAfter: "z-large-B", Expires: e.hub.now().Add(time.Minute).Unix()}
	token, err := e.hub.encodeContextCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	huge := pcRequest(e.hub, e.zKimiCred, "scope=tasks&limit=1&cursor="+url.QueryEscape(token), nil)
	if huge.Code != 413 {
		t.Fatal("PC_SINGLE_ENTRY_TRUNCATED_OR_RETURNED", huge.Code, huge.Body.Len())
	}
}

func TestProjectContextMovedAgentEmptyAndNoReconciliationWrites(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	before, err := e.db.PendingConsoleTaskNotifications(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.Store.LeaveGroup(e.ctx, g.Code, state.Developer, "zzc"); err != nil {
		t.Fatal(err)
	}
	out := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 200 || strings.Contains(out.Body.String(), `"task_id":"`+state.ID+`"`) {
		t.Fatal("PC_MOVED_AGENT_STILL_READS_TASK", out.Code, out.Body)
	}
	current, err := e.db.ConsoleTaskFlowState(e.ctx, state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != state.Version || current.NeedsReassignment {
		t.Fatal("PC_GET_TRIGGERED_RECONCILIATION_WRITE")
	}
	after, err := e.db.PendingConsoleTaskNotifications(e.ctx)
	if err != nil || len(before) != len(after) {
		t.Fatal("PC_GET_CREATED_NOTIFICATION", err)
	}
}

type contextReleaseFailureIdentity struct {
	identity.Store
	authorization identity.ProjectContextAuthorizationStore
}

func (x contextReleaseFailureIdentity) WithProjectContextAuthorization(ctx context.Context, a, b string, c []identity.ContextClaim, f func(identity.ContextAuthorization, func() error) error) error {
	if err := x.authorization.WithProjectContextAuthorization(ctx, a, b, c, f); err != nil {
		return err
	}
	return errors.New("authorization commit failed")
}
func TestProjectContextAuthorizationReleaseFailureNoPartialResponse(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	original := e.svc.Store
	e.svc.Store = contextReleaseFailureIdentity{original, original.(identity.ProjectContextAuthorizationStore)}
	out := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 503 || strings.Contains(out.Body.String(), "spec_id") {
		t.Fatal("PC_PG_RELEASE_FAILURE_LEAKED_BODY", out.Code, out.Body)
	}
}

func TestProjectContextParserLimitAndIdentityInputs(t *testing.T) {
	for _, query := range []string{"scope=all&limit=101", "scope=all&limit=0", "scope=all&limit=", "scope=all&limit=1&limit=2", "scope=all&agent_id=other"} {
		r := httptest.NewRequest("GET", "/?"+query, nil)
		if _, err := parseContextQuery(r); err == nil {
			t.Fatal("PC_QUERY_LIMIT_OR_SELECTOR_GATE", query)
		}
	}
}

type contextCheckFailureIdentity struct {
	identity.Store
	authorization identity.ProjectContextAuthorizationStore
}

func (x contextCheckFailureIdentity) WithProjectContextAuthorization(ctx context.Context, a, b string, c []identity.ContextClaim, f func(identity.ContextAuthorization, func() error) error) error {
	return x.authorization.WithProjectContextAuthorization(ctx, a, b, c, func(v identity.ContextAuthorization, _ func() error) error {
		return f(v, func() error { return errors.New("backend final check failed") })
	})
}
func TestProjectContextFinalAuthorizationRoundtripFailureClosed(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	old := e.svc.Store
	e.svc.Store = contextCheckFailureIdentity{old, old.(identity.ProjectContextAuthorizationStore)}
	out := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if out.Code != 503 || strings.Contains(out.Body.String(), "spec_id") {
		t.Fatal("PC_PG_FINAL_RECHECK_IGNORED", out.Code, out.Body)
	}
}
