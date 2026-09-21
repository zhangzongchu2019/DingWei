package m8

import (
	"bytes"
	"context"
	"encoding/json"
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
