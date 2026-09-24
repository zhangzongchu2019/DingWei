package m8

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestConsoleAgentGroupUserShortNameCollisionIsReportedNotRedrawn(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	a1 := addOwnedGroupAgent(t, db, "owner-a", "concurrent-a1", "concurrent-ka1", "owner-a-concurrent-one-2642", "oa")
	a2 := addOwnedGroupAgent(t, db, "owner-a", "concurrent-a2", "concurrent-ka2", "owner-a-concurrent-two-2642", "oa")
	for iteration := 0; iteration < 20; iteration++ {
		shortName := fmt.Sprintf("a%07d", iteration)
		ready := make(chan struct{}, 2)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for worker, leader := range []model.ConsoleAgent{a1, a2} {
			wg.Add(1)
			go func(worker int, leader model.ConsoleAgent) {
				defer wg.Done()
				_, err := createConsoleAgentGroup(ctx, db, "owner-a", leader, "oa", shortName, func() string {
					ready <- struct{}{}
					<-start
					return fmt.Sprintf("%032d", 1000+iteration*2+worker)
				})
				errs <- err
			}(worker, leader)
		}
		<-ready
		<-ready
		close(start)
		wg.Wait()
		close(errs)
		succeeded, conflicted := 0, 0
		for err := range errs {
			if err == nil {
				succeeded++
			} else if errors.Is(err, store.ErrAgentGroupShortNameConflict) {
				conflicted++
			} else {
				t.Fatalf("iteration %d unexpected error: %v", iteration, err)
			}
		}
		groups, err := db.ListAgentGroups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, group := range groups {
			if group.ShortName == shortName {
				seen++
			}
		}
		if succeeded != 1 || conflicted != 1 || seen != 1 {
			t.Fatalf("iteration %d succeeded=%d conflicted=%d groups=%d", iteration, succeeded, conflicted, seen)
		}
	}
}

func TestConsoleAgentGroupConcurrentDeleteAndJoinLeavesNoMembers(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	leader := addOwnedGroupAgent(t, db, "owner-a", "race-leader", "race-leader-key", "owner-a-race-leader-2642", "oa")
	joining := addOwnedGroupAgent(t, db, "owner-a", "race-joining", "race-joining-key", "owner-a-race-joining-2642", "oa")
	for iteration := 0; iteration < 20; iteration++ {
		code := fmt.Sprintf("R%031d", iteration)
		shortName := fmt.Sprintf("e%07d", iteration)
		code = shortName + code[8:]
		group := model.AgentGroup{Code: code, ShortName: shortName, CreatorPrincipal: "owner-a", LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}
		if err := db.CreateConsoleAgentGroup(ctx, group); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; results <- db.DeleteConsoleAgentGroup(ctx, code, "owner-a", false) }()
		go func() { <-start; results <- db.AssignOwnedConsoleAgentGroup(ctx, code, "owner-a", joining.ID) }()
		close(start)
		first, second := <-results, <-results
		if first != nil && !errors.Is(first, sql.ErrNoRows) {
			t.Fatalf("iteration %d unexpected race result: %v", iteration, first)
		}
		if second != nil && !errors.Is(second, sql.ErrNoRows) {
			t.Fatalf("iteration %d unexpected race result: %v", iteration, second)
		}
		members, err := db.ListAgentGroupMembers(ctx, code)
		if err != nil || len(members) != 0 {
			t.Fatalf("iteration %d delete/join left members=%+v err=%v", iteration, members, err)
		}
	}
}

func TestConsoleAgentGroupHTTPCreateCloseJoinListAndDelete(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	a1 := addOwnedGroupAgent(t, db, "owner-a", "a1", "ka1", "owner-a-one-2642", "oa")
	a2 := addOwnedGroupAgent(t, db, "owner-a", "a2", "ka2", "owner-a-two-2642", "oa")
	b1 := addOwnedGroupAgent(t, db, "owner-b", "b1", "kb1", "owner-b-full-2642", "ob")
	hub.sessionClients[a1.KeyID] = map[string]*sessionClient{a1.SessionName: {keyID: a1.KeyID, sessionName: a1.SessionName}}
	hub.sessionClients[a2.KeyID] = map[string]*sessionClient{a2.SessionName: {keyID: a2.KeyID, sessionName: a2.SessionName}}
	hub.sessionClients[b1.KeyID] = map[string]*sessionClient{b1.SessionName: {keyID: b1.KeyID, sessionName: b1.SessionName}}
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
	rr := call("owner-a", http.MethodPost, "/api/console/agent-groups", "", "", []byte(`{"agent_id":"`+a1.ID+`","short_name":"mygroup"}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var group model.AgentGroup
	if err := json.Unmarshal(rr.Body.Bytes(), &group); err != nil || len(group.Code) != 32 || group.ShortName != "mygroup" || strings.HasPrefix(group.Code, "mygroup") {
		t.Fatalf("created group=%+v err=%v", group, err)
	}
	unknownCode := strings.Repeat("z", 32)
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+unknownCode+"/members", unknownCode, "", []byte(`{"agent_id":"`+a2.ID+`"}`))
	if rr.Code != http.StatusNotFound || strings.TrimSpace(rr.Body.String()) != "组码不存在" {
		t.Fatalf("unknown 32-character group code must be visibly distinct from short input validation code=%d body=%q", rr.Code, rr.Body.String())
	}
	t.Logf("unknown 32-character group code: status=%d body=%q", rr.Code, strings.TrimSpace(rr.Body.String()))
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/additions", group.Code, "", []byte(`{"closed":true}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("close additions code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/additions", group.Code, "", []byte(`{"closed":false}`))
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "无权管理该编组") {
		t.Fatalf("existing group additions authorization must be explicit code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code, group.Code, "", nil)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "无权管理该编组") {
		t.Fatalf("existing group delete authorization must be explicit code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+b1.ID+`"}`))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "该编组已关闭添加") {
		t.Fatalf("closed non-creator join must be visibly rejected code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+a2.ID+`"}`))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "该编组已关闭添加") {
		t.Fatalf("closed group must reject a second Agent code=%d body=%s", rr.Code, rr.Body.String())
	}
	t.Logf("closed additions: status=%d body=%q", rr.Code, strings.TrimSpace(rr.Body.String()))
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/additions", group.Code, "", []byte(`{"closed":false}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("open additions code=%d body=%s", rr.Code, rr.Body.String())
	}
	delete(hub.sessionClients[a2.KeyID], a2.SessionName)
	rr = call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+a2.ID+`"}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("group membership is configuration, so an owned active offline Agent must be addable code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+b1.ID+`"}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("open group must accept an online Agent from another owner through the group-code credential, code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "", []byte(`{"agent_id":"`+b1.ID+`"}`))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "该 Agent 已在该编组中") {
		t.Fatalf("duplicate code join must be a visible, specific failure code=%d body=%s", rr.Code, rr.Body.String())
	}
	t.Logf("duplicate member: status=%d body=%q", rr.Code, strings.TrimSpace(rr.Body.String()))
	rr = call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+a2.ID, group.Code, a2.ID, nil)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "无权管理该编组") {
		t.Fatalf("existing member removal authorization must be explicit code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+a1.ID, group.Code, a1.ID, nil)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "组长不可剔除") {
		t.Fatalf("leader removal must be visibly rejected code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+strings.Repeat("z", 32), strings.Repeat("z", 32), "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown group must remain not found code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodGet, "/api/console/agent-groups", "", "", nil)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"session_name":"owner-a-two-2642"`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"session_name":"owner-b-full-2642"`)) {
		t.Fatalf("creator list lacks the joined members code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+b1.ID, group.Code, b1.ID, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("creator must remove the cross-owner member code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+b1.ID, group.Code, b1.ID, nil)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "该 Agent 不在本编组中，可能列表已过期，请刷新") {
		t.Fatalf("stale non-member removal must be visibly rejected code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodGet, "/api/console/agent-groups", "", "", nil)
	if rr.Code != http.StatusOK || bytes.Contains(rr.Body.Bytes(), []byte(`"session_name":"owner-b-full-2642"`)) {
		t.Fatalf("removed cross-owner member remains visible code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = call("owner-a", http.MethodDelete, "/api/console/agent-groups/"+group.Code, group.Code, "", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete code=%d body=%s", rr.Code, rr.Body.String())
	}
	if members, err := db.ListAgentGroupMembers(context.Background(), group.Code); err != nil || len(members) != 0 {
		t.Fatalf("delete did not detach relations members=%+v err=%v", members, err)
	}
}

func TestConsoleAgentGroupNameIsTrimmedExactAndSuggestsWithinEightRunes(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	leader := addOwnedGroupAgent(t, db, "owner-a", "name-leader", "name-key", "owner-a-name-2642", "oa")
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/console/agent-groups", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
		rr := httptest.NewRecorder()
		hub.HandleConsoleAgentGroups(rr, req)
		return rr
	}
	first := call(`{"agent_id":"` + leader.ID + `","short_name":"  团队1  "}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("trimmed create code=%d body=%s", first.Code, first.Body.String())
	}
	var group model.AgentGroup
	if err := json.Unmarshal(first.Body.Bytes(), &group); err != nil || group.ShortName != "团队1" || len(group.Code) != 32 {
		t.Fatalf("trimmed group=%+v err=%v", group, err)
	}
	duplicate := call(`{"agent_id":"` + leader.ID + `","short_name":"团队1"}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "团队名称不可用；建议名称：团队11、团队12") {
		t.Fatalf("duplicate feedback code=%d body=%q", duplicate.Code, duplicate.Body.String())
	}
	t.Logf("duplicate group name: status=%d body=%q", duplicate.Code, strings.TrimSpace(duplicate.Body.String()))
	eight := call(`{"agent_id":"` + leader.ID + `","short_name":"甲乙丙丁戊己庚辛"}`)
	if eight.Code != http.StatusCreated {
		t.Fatalf("eight-rune create code=%d body=%s", eight.Code, eight.Body.String())
	}
	eightDuplicate := call(`{"agent_id":"` + leader.ID + `","short_name":"甲乙丙丁戊己庚辛"}`)
	if eightDuplicate.Code != http.StatusConflict || !strings.Contains(eightDuplicate.Body.String(), "建议名称：甲乙丙丁戊己庚1、甲乙丙丁戊己庚2") {
		t.Fatalf("eight-rune suggestion code=%d body=%q", eightDuplicate.Code, eightDuplicate.Body.String())
	}
	invalid := call(`{"agent_id":"` + leader.ID + `","short_name":"team-01"}`)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "1 to 8 letters or numbers") {
		t.Fatalf("punctuation must be rejected code=%d body=%q", invalid.Code, invalid.Body.String())
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

	occupied := model.AgentGroup{Code: "abc-1234" + groupCodeA[8:], ShortName: "abc1234", CreatorPrincipal: "owner-a", LeaderKeyID: a1.KeyID, LeaderSessionName: a1.SessionName, LeaderOwnerKey: "oa"}
	if err := db.CreateConsoleAgentGroup(ctx, occupied); err != nil {
		t.Fatal(err)
	}
	created, err := createConsoleAgentGroup(ctx, db, "owner-a", a2, "oa", "abc1234", func() string { return groupCodeB })
	if !errors.Is(err, store.ErrAgentGroupShortNameConflict) {
		t.Fatalf("duplicate user short name must be reported without redraw group=%+v err=%v", created, err)
	}
	created, err = createConsoleAgentGroup(ctx, db, "owner-a", a2, "oa", "def5678", func() string { return groupCodeB })
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-b", b1.ID); err != nil {
		t.Fatalf("the group-code credential must accept another owner's online Agent: %v", err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-a", b2.ID); err == nil {
		t.Fatal("principal added another owners Agent")
	}
	if err = db.SetConsoleAgentGroupAdditionsClosed(ctx, created.Code, "owner-a", true, false); err != nil {
		t.Fatal(err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-b", b2.ID); err == nil {
		t.Fatal("closed group accepted a non-creator agent")
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-a", a1.ID); err == nil {
		t.Fatal("closed group accepted a second Agent")
	}
	if err = db.SetConsoleAgentGroupAdditionsClosed(ctx, created.Code, "owner-a", false, false); err != nil {
		t.Fatalf("open additions: %v", err)
	}
	if err = db.AssignOwnedConsoleAgentGroup(ctx, created.Code, "owner-a", a1.ID); err != nil {
		t.Fatalf("open group must add the owner's Agent: %v", err)
	}
	if err = db.DeleteConsoleAgentGroup(ctx, created.Code, "owner-b", false); err == nil {
		t.Fatal("non-creator deleted group")
	}
	if err = db.DeleteConsoleAgentGroup(ctx, created.Code, "owner-a", false); err != nil {
		t.Fatal(err)
	}
	if members, err := db.ListAgentGroupMembers(ctx, created.Code); err != nil || len(members) != 0 {
		t.Fatalf("delete must cascade all member relations members=%+v err=%v", members, err)
	}
}

func TestConsoleAgentGroupsLegacyLeaderOwnerSeesTheDeleteActionTheHandlerAllows(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	leader := addOwnedGroupAgent(t, db, "owner-a", "legacy-ui-leader", "legacy-ui-key", "owner-a-legacy-ui-2642", "oa")
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/console/agent-groups", nil)
	get = get.WithContext(context.WithValue(get.Context(), consolePrincipalContextKey{}, "owner-a"))
	view := httptest.NewRecorder()
	hub.HandleConsoleAgentGroups(view, get)
	if view.Code != http.StatusOK || !bytes.Contains(view.Body.Bytes(), []byte(`"can_manage":true`)) {
		t.Fatalf("legacy leader owner can delete through the handler but UI hides the action code=%d body=%s", view.Code, view.Body.String())
	}
	remove := httptest.NewRequest(http.MethodDelete, "/api/console/agent-groups/"+groupCodeA, nil)
	remove.SetPathValue("code", groupCodeA)
	remove = remove.WithContext(context.WithValue(remove.Context(), consolePrincipalContextKey{}, "owner-a"))
	deleted := httptest.NewRecorder()
	hub.HandleConsoleAgentGroup(deleted, remove)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("legacy leader owner delete handler disagrees with can_manage code=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestConsoleAgentGroupsLegacyLeaderCanClaimManageAndDelete(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "owner-c", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oc", DisplayName: "owner-c", Active: true}); err != nil {
		t.Fatal(err)
	}
	leader := addOwnedGroupAgent(t, db, "owner-a", "legacy-leader", "legacy-key", "owner-a-legacy-2642", "oa")
	member := addOwnedGroupAgent(t, db, "owner-b", "legacy-member", "legacy-member-key", "owner-b-member-2642", "ob")
	ordinaryAgent := addOwnedGroupAgent(t, db, "owner-c", "legacy-ordinary", "legacy-ordinary-key", "owner-c-ordinary-2642", "oc")
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeA, KeyID: member.KeyID, SessionName: member.SessionName, OwnerKey: "ob"}); err != nil {
		t.Fatal(err)
	}
	hub.sessionClients[member.KeyID] = map[string]*sessionClient{member.SessionName: {keyID: member.KeyID, sessionName: member.SessionName}}
	hub.sessionClients[ordinaryAgent.KeyID] = map[string]*sessionClient{ordinaryAgent.SessionName: {keyID: ordinaryAgent.KeyID, sessionName: ordinaryAgent.SessionName}}
	// Positive control first: the leader endpoint really resolves to an active
	// console Agent. Management must therefore not treat this group as orphaned.
	groups, err := db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || !groups[0].LeaderConsoleMapped {
		t.Fatalf("mapped leader precondition missing groups=%+v err=%v", groups, err)
	}
	ordinaryCall := func(method, path, agentID string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.SetPathValue("code", groupCodeA)
		req.SetPathValue("agentID", agentID)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-c"))
		rr := httptest.NewRecorder()
		hub.HandleConsoleAgentGroup(rr, req)
		return rr
	}
	for _, probe := range []struct {
		method, path, agentID string
		body                  []byte
	}{
		{http.MethodPost, "/api/console/agent-groups/" + groupCodeA + "/additions", "", []byte(`{"closed":true}`)},
		{http.MethodDelete, "/api/console/agent-groups/" + groupCodeA, "", nil},
		{http.MethodDelete, "/api/console/agent-groups/" + groupCodeA + "/members/" + leader.ID, leader.ID, nil},
		{http.MethodPost, "/api/console/agent-groups/" + groupCodeA + "/members", "", []byte(`{"agent_id":"` + ordinaryAgent.ID + `"}`)},
	} {
		if rr := ordinaryCall(probe.method, probe.path, probe.agentID, probe.body); rr.Code != http.StatusNotFound {
			t.Fatalf("ordinary caller must not disclose mapped legacy group method=%s path=%s code=%d body=%s", probe.method, probe.path, rr.Code, rr.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/console/agent-groups/"+groupCodeA+"/additions", bytes.NewBufferString(`{"closed":true}`))
	req.SetPathValue("code", groupCodeA)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr := httptest.NewRecorder()
	hub.HandleConsoleAgentGroup(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("management acted on mapped legacy group code=%d body=%s", rr.Code, rr.Body.String())
	}
	groups, err = db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].CreatorPrincipal != "" {
		t.Fatalf("management stole mapped legacy group groups=%+v err=%v", groups, err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/console/agent-groups", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr = httptest.NewRecorder()
	hub.HandleConsoleAgentGroups(rr, req)
	if rr.Code != http.StatusOK || bytes.Contains(rr.Body.Bytes(), []byte(groupCodeA)) {
		t.Fatalf("management UI exposed mapped legacy group as manageable code=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeA, "owner-a", true, false); err != nil {
		t.Fatalf("legacy leader close additions: %v", err)
	}
	groups, err = db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].CreatorPrincipal != "owner-a" || !groups[0].AdditionsClosed {
		t.Fatalf("legacy claim was not persisted groups=%+v err=%v", groups, err)
	}
	if err := db.UnassignConsoleAgentGroup(ctx, groupCodeA, "owner-a", member.ID, false); err != nil {
		t.Fatalf("legacy leader kick: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "owner-a", false); err != nil {
		t.Fatalf("legacy leader delete: %v", err)
	}

	// Independent positive control: a newly-created group retains the same actions.
	newGroup := model.AgentGroup{Code: "new-1234" + groupCodeB[8:], ShortName: "new1234", CreatorPrincipal: "owner-a", LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: "oa"}
	if err := db.CreateConsoleAgentGroup(ctx, newGroup); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, newGroup.Code, "owner-a", true, false); err != nil {
		t.Fatalf("new group close additions: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, newGroup.Code, "owner-a", false); err != nil {
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
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeA, "owner-a", true, false); err == nil {
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
	if err := db.UnassignConsoleAgentGroup(ctx, groupCodeA, "manager", member.ID, false); err != nil {
		t.Fatalf("management kick legacy orphan member: %v", err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "manager", false); err != nil {
		t.Fatalf("management delete legacy orphan group: %v", err)
	}
}

func TestConsoleAgentGroupFailedRequestsDoNotClaimAndClosedRequiresPresence(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: "orphan-key", LeaderSessionName: "orphan-session", LeaderOwnerKey: "legacy"}); err != nil {
		t.Fatal(err)
	}
	request := func(method, suffix, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/console/agent-groups/"+groupCodeA+suffix, strings.NewReader(body))
		req.SetPathValue("code", groupCodeA)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
		req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
		rr := httptest.NewRecorder()
		hub.HandleConsoleAgentGroup(rr, req)
		return rr
	}
	for _, tc := range []struct {
		method, suffix, body string
		want                 int
	}{
		{http.MethodPut, "", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/additions", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/additions", `null`, http.StatusBadRequest},
	} {
		if rr := request(tc.method, tc.suffix, tc.body); rr.Code != tc.want {
			t.Fatalf("failed request code=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
		}
		groups, err := db.ListAgentGroups(ctx)
		if err != nil || len(groups) != 1 || groups[0].CreatorPrincipal != "" {
			t.Fatalf("failed request claimed orphan group groups=%+v err=%v", groups, err)
		}
	}
	if rr := request(http.MethodPost, "/additions", `{"closed":true}`); rr.Code != http.StatusNoContent {
		t.Fatalf("valid close code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := request(http.MethodPost, "/additions", `{}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing closed reopened group code=%d body=%s", rr.Code, rr.Body.String())
	}
	groups, _ := db.ListAgentGroups(ctx)
	if len(groups) != 1 || !groups[0].AdditionsClosed {
		t.Fatalf("missing closed changed state groups=%+v", groups)
	}
	if rr := request(http.MethodPost, "/additions", `{"closed":false}`); rr.Code != http.StatusNoContent {
		t.Fatalf("explicit false code=%d body=%s", rr.Code, rr.Body.String())
	}
	groups, _ = db.ListAgentGroups(ctx)
	if len(groups) != 1 || groups[0].AdditionsClosed {
		t.Fatalf("explicit false did not reopen groups=%+v", groups)
	}
}

func TestConsoleAgentGroupOrphanDecisionIsRecheckedAtActionTime(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: "late-key", LeaderSessionName: "late-session", LeaderOwnerKey: "ob"}); err != nil {
		t.Fatal(err)
	}
	// T1: management observes a true orphan through the same list API used by UI.
	req := httptest.NewRequest(http.MethodGet, "/api/console/agent-groups", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr := httptest.NewRecorder()
	hub.HandleConsoleAgentGroups(rr, req)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"can_manage":true`)) {
		t.Fatalf("orphan precondition missing code=%d body=%s", rr.Code, rr.Body.String())
	}
	// T2: before the action, the real owner mapping arrives.
	addOwnedGroupAgent(t, db, "owner-b", "late-agent", "late-key", "late-session", "ob")
	// T3: stale management intent must be rejected by the action SQL itself.
	req = httptest.NewRequest(http.MethodPost, "/api/console/agent-groups/"+groupCodeA+"/additions", strings.NewReader(`{"closed":true}`))
	req.SetPathValue("code", groupCodeA)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr = httptest.NewRecorder()
	hub.HandleConsoleAgentGroup(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("stale management intent acted after owner appeared code=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := db.SetConsoleAgentGroupAdditionsClosed(ctx, groupCodeA, "owner-b", true, false); err != nil {
		t.Fatalf("real owner could not manage after rejected stale intent: %v", err)
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
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "principal-a", Provider: "fixture", ProviderIssuer: "fixture", ProviderSubject: "a", DisplayName: "principal-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "agent-a", KeyID: "ka", SessionName: "owner-a-agent-2642", OwnerPrincipal: "principal-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteConsoleAgentGroup(ctx, groupCodeA, "principal-a", false); err != nil {
		t.Fatal(err)
	}
	if err := hub.enforceAgentGrouping(ctx, from, env, "kb", "owner-b-agent-2642"); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("delete-all must not silently restore global reachability err=%v", err)
	}
}
