package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func addOwnedGroupAgent(t *testing.T, db *store.SQLite, principal, id, key, session, owner string) model.ConsoleAgent {
	t.Helper()
	ctx := context.Background()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: key, SessionName: session, OwnerKey: owner, Active: true}); err != nil {
		t.Fatal(err)
	}
	agent := model.ConsoleAgent{ID: id, KeyID: key, SessionName: session, OwnerPrincipal: principal, Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestConsoleAgentGroupHTTPCreateCloseJoinListAndDelete(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	a1 := addOwnedGroupAgent(t, db, "owner-a", "a1", "ka1", "owner-a-one-2642", "oa")
	a2 := addOwnedGroupAgent(t, db, "owner-a", "a2", "ka2", "owner-a-two-2642", "oa")
	b1 := addOwnedGroupAgent(t, db, "owner-b", "b1", "kb1", "owner-b-full-2642", "ob")
	call := func(principal, method, path, code, agentID string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.SetPathValue("code", code)
		req.SetPathValue("agentID", agentID)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principal))
		rr := httptest.NewRecorder()
		if path == "/api/console/agent-groups" {
			hub.HandleConsoleAgentGroups(rr, req)
		} else {
			hub.HandleConsoleAgentGroup(rr, req)
		}
		return rr
	}
	rr := call("owner-a", http.MethodPost, "/api/console/agent-groups", "", "", []byte(`{"agent_id":"`+a1.ID+`"}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var group model.AgentGroup
	if err := json.Unmarshal(rr.Body.Bytes(), &group); err != nil || len(group.Code) != 32 || len(group.ShortName) != 6 {
		t.Fatalf("created group=%+v err=%v", group, err)
	}
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/additions", group.Code, "", []byte(`{"closed":true}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("close additions code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+b1.ID+`"}`))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("closed non-creator join code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+a2.ID+`"}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("closed creator join code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/additions", group.Code, "", []byte(`{"closed":false}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("open additions code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+b1.ID+`"}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("consented join code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodGet, "/api/console/agent-groups", "", "", nil)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"session_name":"owner-b-full-2642"`)) {
		t.Fatalf("creator list lacks joined full name code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+group.Code, group.Code, "", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete code=%d body=%s", rr.Code, rr.Body.String())
	}
	if members, err := db.ListAgentGroupMembers(context.Background(), group.Code); err != nil || len(members) != 0 {
		t.Fatalf("delete did not detach relations members=%+v err=%v", members, err)
	}
}

func TestConsoleAgentGroupsRetryCollisionAndEnforceOwnership(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	a1 := addOwnedGroupAgent(t, db, "owner-a", "a1", "ka1", "owner-a-one-2642", "oa")
	a2 := addOwnedGroupAgent(t, db, "owner-a", "a2", "ka2", "owner-a-two-2642", "oa")
	b1 := addOwnedGroupAgent(t, db, "owner-b", "b1", "kb1", "owner-b-one-2642", "ob")
	b2 := addOwnedGroupAgent(t, db, "owner-b", "b2", "kb2", "owner-b-two-2642", "ob")

	occupied := model.AgentGroup{Code: groupCodeA, ShortName: "abc123", CreatorPrincipal: "owner-a", LeaderKeyID: a1.KeyID, LeaderSessionName: a1.SessionName, LeaderOwnerKey: "oa"}
	if err := db.CreateConsoleAgentGroup(ctx, occupied); err != nil {
		t.Fatal(err)
	}
	candidates := [][2]string{{groupCodeB, "abc123"}, {"CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", "def456"}}
	index := 0
	created, err := createConsoleAgentGroup(ctx, db, "owner-a", a2, "oa", func() (string, string) {
		value := candidates[index]
		index++
		return value[0], value[1]
	})
	if err != nil || created.ShortName != "def456" || index != 2 {
		t.Fatalf("collision must retry with a globally unique short name group=%+v attempts=%d err=%v", created, index, err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-b", b1.ID); err != nil {
		t.Fatalf("other owner consented join failed: %v", err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-a", b2.ID); err == nil {
		t.Fatal("principal added another owners Agent")
	}
	if err = db.SetConsoleAgentGroupAdditionsClosed(ctx, created.Code, "owner-a", true); err != nil {
		t.Fatal(err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-b", b2.ID); err == nil {
		t.Fatal("closed group accepted a non-creator agent")
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-a", a1.ID); err != nil {
		t.Fatalf("creator must still add own agent while closed: %v", err)
	}
	if err = db.UnassignConsoleAgentGroup(ctx, created.Code, "owner-a", b1.ID); err != nil {
		t.Fatalf("creator kick failed: %v", err)
	}
	if err = db.DeleteConsoleAgentGroup(ctx, created.Code, "owner-b"); err == nil {
		t.Fatal("non-creator deleted group")
	}
	if err = db.DeleteConsoleAgentGroup(ctx, created.Code, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if members, err := db.ListAgentGroupMembers(ctx, created.Code); err != nil || len(members) != 0 {
		t.Fatalf("delete must cascade all member relations members=%+v err=%v", members, err)
	}
}

func TestConsoleAgentGroupsLegacyLeaderCanClaimManageAndDelete(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	leader := addOwnedGroupAgent(t, db, "owner-a", "legacy-leader", "legacy-key", "owner-a-legacy-2642", "oa")
	member := addOwnedGroupAgent(t, db, "owner-b", "legacy-member", "legacy-member-key", "owner-b-member-2642", "ob")
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeA, KeyID: member.KeyID, SessionName: member.SessionName, OwnerKey: "ob"}); err != nil {
		t.Fatal(err)
	}
	// Positive control first: the leader endpoint really resolves to an active
	// console Agent. Management must therefore not treat this group as orphaned.
	groups, err := db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || !groups[0].LeaderConsoleMapped {
		t.Fatalf("mapped leader precondition missing groups=%+v err=%v", groups, err)
	}
	if err := db.ClaimLegacyConsoleAgentGroupAsManagement(ctx, groupCodeA, "manager"); err != nil {
		t.Fatal(err)
	}
	groups, err = db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].CreatorPrincipal != "" {
		t.Fatalf("management stole mapped legacy group groups=%+v err=%v", groups, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/console/agent-groups", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr := httptest.NewRecorder()
	hub.HandleConsoleAgentGroups(rr, req)
	if rr.Code != http.StatusOK || bytes.Contains(rr.Body.Bytes(), []byte(groupCodeA)) {
		t.Fatalf("management UI exposed mapped legacy group as manageable code=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeA, "owner-a", true); err != nil {
		t.Fatalf("legacy leader close additions: %v", err)
	}
	groups, err = db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].CreatorPrincipal != "owner-a" || !groups[0].AdditionsClosed {
		t.Fatalf("legacy claim was not persisted groups=%+v err=%v", groups, err)
	}
	if err := db.UnassignConsoleAgentGroup(ctx, groupCodeA, "owner-a", member.ID); err != nil {
		t.Fatalf("legacy leader kick: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "owner-a"); err != nil {
		t.Fatalf("legacy leader delete: %v", err)
	}

	// Independent positive control: a newly-created group retains the same actions.
	newGroup := model.AgentGroup{Code: groupCodeB, ShortName: "new123", CreatorPrincipal: "owner-a", LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}
	if err := db.CreateConsoleAgentGroup(ctx, newGroup); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeB, "owner-a", true); err != nil {
		t.Fatalf("new group close additions: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeB, "owner-a"); err != nil {
		t.Fatalf("new group delete: %v", err)
	}
}

func TestConsoleAgentGroupsLegacyWithoutConsoleAgentRequiresManagementClaim(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	member := addOwnedGroupAgent(t, db, "owner-b", "orphan-member", "orphan-member-key", "owner-b-orphan-member-2642", "ob")
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: "missing-leader-key", LeaderSessionName: "missing-leader-session", LeaderOwnerKey: "legacy-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeA, KeyID: member.KeyID, SessionName: member.SessionName, OwnerKey: "ob"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeA, "owner-a", true); err == nil {
		t.Fatal("ordinary owner claimed legacy group whose leader has no console_agent mapping")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/console/agent-groups", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr := httptest.NewRecorder()
	hub.HandleConsoleAgentGroups(rr, req)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"can_manage":true`)) {
		t.Fatalf("management cannot see manageable legacy orphan code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/console/agent-groups/"+groupCodeA+"/additions", bytes.NewBufferString(`{"closed":true}`))
	req.SetPathValue("code", groupCodeA)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr = httptest.NewRecorder()
	hub.HandleConsoleAgentGroup(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("management close legacy orphan group code=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := db.UnassignConsoleAgentGroup(ctx, groupCodeA, "manager", member.ID); err != nil {
		t.Fatalf("management kick legacy orphan member: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "manager"); err != nil {
		t.Fatalf("management delete legacy orphan group: %v", err)
	}
}

func TestGroupingEmptyTableDistinguishesNeverConfiguredFromDeletedAll(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	for _, ep := range []model.SessionEndpoint{
		{KeyID: "ka", SessionName: "owner-a-agent-2642", OwnerKey: "owner-a", Active: true},
		{KeyID: "kb", SessionName: "owner-b-agent-2642", OwnerKey: "owner-b", Active: true},
	} {
		if err := db.UpsertSessionEndpoint(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	from := address{Kind: addressSession, KeyID: "ka", SessionName: "owner-a-agent-2642"}
	env := model.Envelope{From: "owner-a-agent-2642#ka", To: "owner-b-agent-2642#kb"}
	if err := hub.enforceAgentGrouping(ctx, from, env, "kb", "owner-b-agent-2642"); err != nil {
		t.Fatalf("never-configured migration state must preserve pre-G behavior: %v", err)
	}
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: "ka", LeaderSessionName: "owner-a-agent-2642", LeaderOwnerKey: "owner-a"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "principal-a", Provider: "fixture", ProviderIssuer: "fixture", ProviderSubject: "a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "agent-a", KeyID: "ka", SessionName: "owner-a-agent-2642", OwnerPrincipal: "principal-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "principal-a"); err != nil {
		t.Fatal(err)
	}
	if err := hub.enforceAgentGrouping(ctx, from, env, "kb", "owner-b-agent-2642"); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("delete-all must not silently restore global reachability err=%v", err)
	}
}
