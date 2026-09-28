package m8

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const (
	groupCodeA = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	groupCodeB = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	groupCodeC = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
)

type groupingPolicyFixture struct {
	hub *Hub
	db  *store.SQLite
	ctx context.Context
	ep  map[string]agentEndpointIdentity
}

func newGroupingPolicyFixture(t *testing.T) groupingPolicyFixture {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	endpoints := map[string]agentEndpointIdentity{
		"a": {KeyID: "policy-ka", SessionName: "owner-a-policy-a", OwnerKey: "owner-a"},
		"b": {KeyID: "policy-kb", SessionName: "owner-a-policy-b", OwnerKey: "owner-a"},
		"c": {KeyID: "policy-kc", SessionName: "owner-c-policy-c", OwnerKey: "owner-c"},
		"d": {KeyID: "policy-kd", SessionName: "owner-d-policy-d", OwnerKey: "owner-d"},
	}
	for _, ep := range endpoints {
		if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: ep.KeyID, SessionName: ep.SessionName, OwnerKey: ep.OwnerKey, Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	return groupingPolicyFixture{hub: hub, db: db, ctx: ctx, ep: endpoints}
}

func (f groupingPolicyFixture) enforce(from, to string) error {
	source, target := f.ep[from], f.ep[to]
	return f.hub.enforceAgentGrouping(f.ctx,
		address{Kind: addressSession, KeyID: source.KeyID, SessionName: source.SessionName},
		model.Envelope{From: source.SessionName + "#" + source.KeyID, To: target.SessionName + "#" + target.KeyID},
		target.KeyID, target.SessionName)
}

func (f groupingPolicyFixture) createGroup(t *testing.T, code, leader string) {
	t.Helper()
	ep := f.ep[leader]
	if err := f.db.UpsertAgentGroup(f.ctx, model.AgentGroup{Code: code, LeaderKeyID: ep.KeyID, LeaderSessionName: ep.SessionName, LeaderOwnerKey: ep.OwnerKey}); err != nil {
		t.Fatal(err)
	}
}

func (f groupingPolicyFixture) add(t *testing.T, code, endpoint string) {
	t.Helper()
	ep := f.ep[endpoint]
	if err := f.db.AssignAgentGroup(f.ctx, model.AgentGroupMember{Code: code, KeyID: ep.KeyID, SessionName: ep.SessionName, OwnerKey: ep.OwnerKey}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerGroupingRule01SameExplicitGroupCommunicates(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "a")
	f.add(t, groupCodeA, "c")
	if err := f.enforce("a", "c"); err != nil {
		t.Fatalf("same explicit group must communicate: %v", err)
	}
}

func TestOwnerGroupingRule02UngroupedAgentsUseTheOwnerDefaultGroup(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeC, "c") // activate grouping without grouping a/b
	if err := f.enforce("a", "b"); err != nil {
		t.Fatalf("ungrouped same-owner Agents must share the default group: %v", err)
	}
}

func TestOwnerGroupingRule03ExplicitMembershipRemovesTheDefaultGroup(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "a")
	if err := f.enforce("a", "b"); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("explicitly grouped Agent must not retain the owner default group: %v", err)
	}
}

func TestOwnerGroupingRule04DifferentGroupsAreIsolated(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "a")
	f.createGroup(t, groupCodeB, "c")
	if err := f.enforce("a", "c"); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("different explicit groups must be isolated: %v", err)
	}
}

func TestOwnerGroupingRule05MultipleGroupsProvideMultipleKeys(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "a")
	f.createGroup(t, groupCodeB, "c")
	f.add(t, groupCodeA, "d")
	f.add(t, groupCodeB, "d")
	if errA, errB := f.enforce("a", "d"), f.enforce("c", "d"); errA != nil || errB != nil {
		t.Fatalf("multi-group Agent must communicate through every membership: groupA=%v groupB=%v", errA, errB)
	}
}

func TestOwnerGroupingRule02ExitAllGroupsRestoresTheDefaultGroup(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "c")
	f.add(t, groupCodeA, "a")
	if err := f.enforce("a", "b"); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("explicit membership precondition did not remove default group: %v", err)
	}
	a := f.ep["a"]
	if err := f.db.UnassignAgentGroup(f.ctx, groupCodeA, a.KeyID, a.SessionName); err != nil {
		t.Fatal(err)
	}
	if err := f.enforce("a", "b"); err != nil {
		t.Fatalf("leaving every explicit group must restore the owner default group: %v", err)
	}
}

type groupingFixture struct {
	hub                               *Hub
	db                                *store.SQLite
	ctx                               context.Context
	source, same, outside, multi      *websocket.Conn
	sourceKey, sameKey, outsideKey    model.ServiceAPIKey
	multiKey                          model.ServiceAPIKey
	sourceFull, sameFull, outsideFull string
	multiFull                         string
}

func newGroupingFixture(t *testing.T) groupingFixture {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "grouping", Name: "grouping", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	issue := func(owner, session string) (*websocket.Conn, model.ServiceAPIKey, string) {
		secret, key, err := hub.IssueAPIKey(ctx, "grouping", owner)
		if err != nil {
			t.Fatal(err)
		}
		entityID := "grouping:personal:ou_" + owner
		if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: owner, FeishuOpenID: "ou_" + owner, Active: true}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: entityID, BotChannelID: "grouping", Type: model.ChatPersonal, FeishuID: "ou_" + owner, BoundOwner: owner, Active: true}); err != nil {
			t.Fatal(err)
		}
		if err := hub.BindAccount(ctx, key.ID, entityID); err != nil {
			t.Fatal(err)
		}
		mux, srv := sessionTestServer(t, hub)
		_ = mux
		conn := dialSession(t, ctx, srv.URL, session, key.ID, secret)
		full := owner + "-" + session + "-" + keyTail(key.ID)
		t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "done") })
		return conn, key, full
	}
	source, sourceKey, _ := issue("owner-a", "source")
	same, sameKey, _ := issue("owner-b", "same")
	outside, outsideKey, _ := issue("owner-c", "outside")
	multi, multiKey, _ := issue("owner-d", "multi")
	// Handshake persists the canonical full name as SessionName. Use the actual
	// endpoint rows rather than reconstructing it in the policy fixture.
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	find := func(keyID string) model.SessionEndpoint {
		for _, ep := range endpoints {
			if ep.KeyID == keyID {
				return ep
			}
		}
		t.Fatalf("endpoint for %s not found", keyID)
		return model.SessionEndpoint{}
	}
	sourceEP, sameEP, outsideEP, multiEP := find(sourceKey.ID), find(sameKey.ID), find(outsideKey.ID), find(multiKey.ID)
	// dialSession returns after reading onboarding, but HandleSessionWS deliberately
	// sends that frame before publishing sessionClients (hub.go:887/899), so it is
	// not an online barrier. In the full suite the last-created multi connection
	// exposed this window; alone, the following DB work usually masked it. Wait on
	// the actual condition—a fixed sleep would only move the load-sensitive race.
	for _, ep := range []model.SessionEndpoint{sourceEP, sameEP, outsideEP, multiEP} {
		waitSessionOnline(t, hub, ep.KeyID, ep.SessionName)
	}
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: sourceEP.KeyID, LeaderSessionName: sourceEP.SessionName, LeaderOwnerKey: sourceEP.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeA, KeyID: sameEP.KeyID, SessionName: sameEP.SessionName, OwnerKey: sameEP.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeB, LeaderKeyID: outsideEP.KeyID, LeaderSessionName: outsideEP.SessionName, LeaderOwnerKey: outsideEP.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeA, KeyID: multiEP.KeyID, SessionName: multiEP.SessionName, OwnerKey: multiEP.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: groupCodeB, KeyID: multiEP.KeyID, SessionName: multiEP.SessionName, OwnerKey: multiEP.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	return groupingFixture{hub: hub, db: db, ctx: ctx, source: source, same: same, outside: outside, multi: multi,
		sourceKey: sourceKey, sameKey: sameKey, outsideKey: outsideKey, multiKey: multiKey,
		sourceFull: sourceEP.SessionName, sameFull: sameEP.SessionName, outsideFull: outsideEP.SessionName, multiFull: multiEP.SessionName}
}

func sessionTestServer(t *testing.T, hub *Hub) (*http.ServeMux, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return mux, srv
}

func TestAgentGroupingReachabilityAndIndistinguishableMiss(t *testing.T) {
	f := newGroupingFixture(t)
	if err := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: "same-group", From: f.sourceFull + "#" + f.sourceKey.ID, To: f.sameFull + "#" + f.sourceKey.ID, Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, f.ctx, f.same); got.Body != "ok" {
		t.Fatalf("same-group delivery=%+v", got)
	}

	outsideErr := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: "outside-group", From: f.sourceFull + "#" + f.sourceKey.ID, To: f.outsideFull + "#" + f.sourceKey.ID, Body: "blocked"})
	missingErr := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: "missing", From: f.sourceFull + "#" + f.sourceKey.ID, To: "owner-z-missing-deadbeef#" + f.sourceKey.ID, Body: "blocked"})
	if !errors.Is(outsideErr, errGroupAgentNotFound) || !errors.Is(missingErr, errGroupAgentNotFound) || outsideErr.Error() != missingErr.Error() || outsideErr.Error() != groupAgentNotFoundText {
		t.Fatalf("outside=%q missing=%q", outsideErr, missingErr)
	}
	// send.py observes success before Hub's asynchronous routing decision. Both
	// denials must come back later with byte-identical bodies, or the sender can
	// enumerate globally existing Agents.
	if err := writeEnvelope(f.ctx, f.source, model.Envelope{ID: "outside-async", From: f.sourceFull + "#" + f.sourceKey.ID, To: f.outsideFull + "#" + f.sourceKey.ID, Body: "blocked"}); err != nil {
		t.Fatal(err)
	}
	outsideReply := readEnvelope(t, f.ctx, f.source)
	if err := writeEnvelope(f.ctx, f.source, model.Envelope{ID: "missing-async", From: f.sourceFull + "#" + f.sourceKey.ID, To: "owner-z-missing-deadbeef#" + f.sourceKey.ID, Body: "blocked"}); err != nil {
		t.Fatal(err)
	}
	missingReply := readEnvelope(t, f.ctx, f.source)
	if outsideReply.Body != groupAgentNotFoundText || outsideReply.Body != missingReply.Body {
		t.Fatalf("async outside=%q missing=%q", outsideReply.Body, missingReply.Body)
	}
}

func TestAgentGroupingAllowsAgentToFeishuAddress(t *testing.T) {
	f := newGroupingFixture(t)
	f.hub.RegisterBot("grouping", "UnifiedRobot")
	f.hub.Outbound = bus.NewDBQueue(f.db, model.DirectionOut)
	env := model.Envelope{
		ID:   "agent-to-feishu",
		From: sessionAddress(f.sourceFull, f.sourceKey.ID),
		// send.py supplies a two-part address; the ou_ prefix identifies the
		// target as Feishu rather than an Agent session.
		To:   sessionAddress("ou_owner", f.sourceKey.ID),
		Body: "给业主",
	}
	if err := f.hub.RouteEnvelope(f.ctx, env); err != nil {
		t.Fatalf("Agent 发飞书 openID 必须绕过 Agent 编组闸: %v", err)
	}
	got, err := f.db.ClaimNextMessage(f.ctx, model.DirectionOut)
	if err != nil || got == nil {
		t.Fatalf("飞书出站没有真正入队: msg=%+v err=%v", got, err)
	}
	if got.ChatEntityID != "grouping:personal:ou_owner" || !strings.Contains(got.Content, "给业主") {
		t.Fatalf("飞书出站目标或正文错误: %+v", got)
	}
}

func TestGroupingRejectionReasonsReflectRealCondition(t *testing.T) {
	f := newGroupingPolicyFixture(t)
	f.createGroup(t, groupCodeA, "a")
	f.createGroup(t, groupCodeB, "c")
	fromFeishu := address{Kind: addressFeishu, OpenID: "ou_stranger", KeyID: f.ep["a"].KeyID, BotName: "bot"}
	err := f.hub.enforceAgentGrouping(f.ctx, fromFeishu, model.Envelope{}, f.ep["a"].KeyID, f.ep["a"].SessionName)
	if !errors.Is(err, errGroupAgentNotFound) || groupingRejectionReason(err) != "sender_not_active_member" {
		t.Fatalf("飞书非成员拒绝必须记录真实条件: err=%v reason=%s", err, groupingRejectionReason(err))
	}
	err = f.enforce("a", "c")
	if !errors.Is(err, errGroupAgentNotFound) || groupingRejectionReason(err) != "cross_group_denied" {
		t.Fatalf("跨组拒绝必须记录真实条件: err=%v reason=%s", err, groupingRejectionReason(err))
	}
}

func TestAgentGroupingSupportsMultipleGroupsAndOneLeader(t *testing.T) {
	f := newGroupingFixture(t)
	if err := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: "multi-a", From: f.sourceFull + "#" + f.sourceKey.ID, To: f.multiFull + "#" + f.sourceKey.ID, Body: "a"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, f.ctx, f.multi); got.Body != "a" {
		t.Fatalf("group A delivery=%+v", got)
	}
	if err := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: "multi-b", From: f.outsideFull + "#" + f.outsideKey.ID, To: f.multiFull + "#" + f.outsideKey.ID, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, f.ctx, f.multi); got.Body != "b" {
		t.Fatalf("group B delivery=%+v", got)
	}
	groups, err := f.hub.Repo.ListAgentGroups(f.ctx)
	if err != nil || len(groups) != 2 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	for _, group := range groups {
		if group.LeaderKeyID == "" || group.LeaderSessionName == "" {
			t.Fatalf("group without exactly one stored leader: %+v", group)
		}
	}
}

func TestGroupingOwnerExemptionUsesSenderIdentityNotChannel(t *testing.T) {
	f := newGroupingFixture(t)
	if err := f.hub.Repo.UpsertMember(f.ctx, model.Member{OwnerKey: "owner-human", DisplayName: "Owner", FeishuOpenID: "ou_owner", Active: true}); err != nil {
		t.Fatal(err)
	}
	ownerEnv := model.Envelope{ID: "owner-exempt", From: feishuAddress("ou_owner", f.outsideKey.ID, "bot"), To: f.outsideFull + "#" + f.outsideKey.ID, Body: "owner"}
	if err := f.hub.RouteEnvelope(f.ctx, ownerEnv); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, f.ctx, f.outside); got.Body != "owner" {
		t.Fatalf("owner delivery=%+v", got)
	}
	nonOwnerEnv := model.Envelope{ID: "same-channel-non-owner", From: feishuAddress("ou_stranger", f.outsideKey.ID, "bot"), To: f.outsideFull + "#" + f.outsideKey.ID, Body: "stranger"}
	if err := f.hub.RouteEnvelope(f.ctx, nonOwnerEnv); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("non-owner on same channel err=%v", err)
	}

	for _, principal := range []model.ConsolePrincipal{
		{ID: "principal-owner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "owner", DisplayName: "principal-owner", Active: true},
		{ID: "principal-viewer", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "viewer", DisplayName: "principal-viewer", Active: true},
	} {
		if err := f.db.UpsertConsolePrincipal(f.ctx, principal); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.UpsertConsolePrincipalRole(f.ctx, model.ConsolePrincipalRole{PrincipalID: "principal-owner", Role: "owner", OwnerKey: "owner-human", Active: true}); err != nil {
		t.Fatal(err)
	}
	ownerView := model.Envelope{ID: "owner-view", From: sessionAddress(systemAddressName, f.outsideKey.ID), To: f.outsideFull + "#" + f.outsideKey.ID, Body: "owner-view",
		Meta: map[string]any{hubIngressMetaKey: hubIngressViewV2, "console_principal_id": "principal-owner"}}
	if err := f.hub.RouteEnvelope(f.ctx, ownerView); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, f.ctx, f.outside); got.Body != "owner-view" {
		t.Fatalf("owner view delivery=%+v", got)
	}
	nonOwnerView := model.Envelope{ID: "non-owner-view", From: sessionAddress(systemAddressName, f.outsideKey.ID), To: f.outsideFull + "#" + f.outsideKey.ID, Body: "viewer",
		Meta: map[string]any{hubIngressMetaKey: hubIngressViewV2, "console_principal_id": "principal-viewer"}}
	if err := f.hub.RouteEnvelope(f.ctx, nonOwnerView); !errors.Is(err, errGroupAgentNotFound) {
		t.Fatalf("non-owner view err=%v", err)
	}
}

func TestGroupingDefaultOwnerGroupIsOverriddenByExplicitCode(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "defaults", Name: "defaults", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Pure policy fixture: ungrouped endpoints of one owner share the default;
	// assigning one endpoint replaces, rather than augments, that default.
	a := agentEndpointIdentity{KeyID: "ka", SessionName: "a", OwnerKey: "owner"}
	b := agentEndpointIdentity{KeyID: "kb", SessionName: "b", OwnerKey: "owner"}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: a.KeyID, SessionName: a.SessionName, OwnerKey: a.OwnerKey, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: b.KeyID, SessionName: b.SessionName, OwnerKey: b.OwnerKey, Active: true}); err != nil {
		t.Fatal(err)
	}
	ag, _ := hub.groupingKeys(ctx, a)
	bg, _ := hub.groupingKeys(ctx, b)
	if !ag["default-owner:owner"] || !bg["default-owner:owner"] {
		t.Fatalf("default groups a=%v b=%v", ag, bg)
	}
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: groupCodeA, LeaderKeyID: a.KeyID, LeaderSessionName: a.SessionName, LeaderOwnerKey: a.OwnerKey}); err != nil {
		t.Fatal(err)
	}
	ag, _ = hub.groupingKeys(ctx, a)
	if ag["default-owner:owner"] || !ag[groupCodeA] {
		t.Fatalf("explicit group did not replace default: %v", ag)
	}
}
