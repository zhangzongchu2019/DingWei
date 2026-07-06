package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const testSecOpsAdminOpenID = "ou_testadmin0000000000"

func newTestHub(t *testing.T) (*Hub, *store.SQLite, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "m8.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New(db), db, ctx
}

func useTestSecOpsAdminOpenID(t *testing.T) {
	t.Helper()
	old := secOpsAdminOpenID
	secOpsAdminOpenID = testSecOpsAdminOpenID
	t.Cleanup(func() {
		secOpsAdminOpenID = old
	})
}

func TestIssueAPIKeyStoresHashAndBindScope(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, meta, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || strings.Contains(meta.KeyHash, secret) || !strings.HasPrefix(meta.ID, "FB-") {
		t.Fatalf("secret/hash/key_id invalid secret=%q meta=%+v", secret, meta)
	}
	resolved, err := db.ResolveServiceAPISecret(ctx, meta.ID, HashAPIKey(secret))
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.ID != meta.ID || resolved.KeyHash == secret {
		t.Fatalf("resolved key = %+v", resolved)
	}
	if err := hub.BindAccount(ctx, meta.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "openapi", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatalf("AddPrefixRule bound scope: %v", err)
	}
	badScope, _ := json.Marshal([]string{"dev:personal:bob"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc1", MatchExpr: "codex", AccountScopeJSON: string(badScope), Enabled: true}); err == nil {
		t.Fatalf("AddPrefixRule accepted unbound account scope")
	}
	if err := db.RevokeServiceAPIKey(ctx, meta.ID); err != nil {
		t.Fatalf("RevokeServiceAPIKey: %v", err)
	}
	revoked, err := db.ResolveServiceAPISecret(ctx, meta.ID, HashAPIKey(secret))
	if err != nil {
		t.Fatalf("Resolve revoked key: %v", err)
	}
	if revoked != nil {
		t.Fatalf("revoked key resolved: %+v", revoked)
	}
}

func TestSystemKeywordRoutesToSystemHandler(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.UpsertSystemService(ctx, model.SystemService{Name: "scheduler", Description: "调度", Delivery: "internal", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "#进度汇报", ServiceName: "scheduler", Action: "record", Priority: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "#调整日程", ServiceName: "scheduler", Action: "coordinate", Priority: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "sys:调度", ServiceName: "scheduler", Action: "auto", Priority: 99, Active: true}); err != nil {
		t.Fatal(err)
	}
	handler := &fakeSystemHandler{reply: "已提交"}
	hub.System = handler
	result, err := hub.Dispatch(ctx, model.Message{ID: "sys1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@_user_1 #进度汇报 本周做了X")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "已提交" {
		t.Fatalf("result=%+v", result)
	}
	if handler.service != "scheduler" || handler.action != "record" || handler.body != "本周做了X" {
		t.Fatalf("handler=%+v", handler)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys2", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "#调整日程 唐盛顺延3天")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "coordinate" || handler.body != "唐盛顺延3天" {
		t.Fatalf("coordinate result=%+v handler=%+v", result, handler)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys3", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "我想 #调整日程")
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched {
		t.Fatalf("middle keyword should not match: %+v", result)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys4", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "sys:调度 协调团队排期")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "auto" || handler.body != "协调团队排期" {
		t.Fatalf("sys compat result=%+v handler=%+v", result, handler)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys5", ChatEntityID: "dev:group:oc_team", BotChannelID: "dev", ChatType: model.ChatGroup}, "@UnifiedRobot #调整日程 唐盛顺延3天")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "coordinate" || handler.body != "唐盛顺延3天" {
		t.Fatalf("literal bot mention should strip to system route result=%+v handler=%+v", result, handler)
	}
}

func TestSecurityOpsRoutesToAllSystemSessions(t *testing.T) {
	useTestSecOpsAdminOpenID(t)
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("CC-Connector", "CC-Connector")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: secOpsAdminOpenID, Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "sec", Name: "sec", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "sec", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, secOpsOwnerKey); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	claude := dialSession(t, ctx, srv.URL, "sec-claude", key.ID, secret)
	defer claude.Close(websocket.StatusNormalClosure, "done")
	deepseek := dialSession(t, ctx, srv.URL, "sec-deepseek", key.ID, secret)
	defer deepseek.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "sec-claude")
	waitSessionOnline(t, hub, key.ID, "sec-deepseek")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops1",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: secOpsAdminOpenID,
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "2个sec-ops会话") {
		t.Fatalf("secops result=%+v", result)
	}
	gotClaude := readEnvelope(t, ctx, claude)
	gotDeepseek := readEnvelope(t, ctx, deepseek)
	got := gotClaude.Body + "\n" + gotDeepseek.Body
	if strings.Count(got, "加白名单 1.2.3.4") != 2 || strings.Contains(got, "#系统安全") || strings.Contains(got, "@SYSTEM-V-TASK-INTERNAL") {
		t.Fatalf("secops bodies not stripped:\nclaude=%+v\ndeepseek=%+v", gotClaude, gotDeepseek)
	}
	if gotClaude.Meta["group_chat_id"] != "oc_c610" || gotDeepseek.Meta["sender_open_id"] != secOpsAdminOpenID {
		t.Fatalf("secops meta missing source: claude=%+v deepseek=%+v", gotClaude.Meta, gotDeepseek.Meta)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "secops2",
		ChatEntityID: "CC-Connector:group:oc_c610",
		BotChannelID: "CC-Connector",
		ChatType:     model.ChatGroup,
		SenderOpenID: secOpsAdminOpenID,
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 撤销封禁 2.2.2.2"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 撤销封禁 2.2.2.2")
	if err != nil || !result.Matched {
		t.Fatalf("cc secops result=%+v err=%v", result, err)
	}
	if readEnvelope(t, ctx, claude).Body != "撤销封禁 2.2.2.2" || readEnvelope(t, ctx, deepseek).Body != "撤销封禁 2.2.2.2" {
		t.Fatal("cc connector secops did not fan out stripped command")
	}
}

func TestSecurityOpsRejectsNonZzc(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops-deny",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alice",
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "无权限") {
		t.Fatalf("non zsf secops result=%+v", result)
	}
}

func TestSecurityOpsAllowsBoundZzcOwner(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_alias", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_alias", BoundOwner: "zsf", Active: true}); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops-owner",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alias",
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 改规则 404阈值=30"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 改规则 404阈值=30")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || strings.Contains(result.Reply, "无权限") || !strings.Contains(result.Reply, "无在线") {
		t.Fatalf("bound zsf secops result=%+v", result)
	}
}

type fakeSystemHandler struct {
	service string
	action  string
	body    string
	reply   string
}

func (f *fakeSystemHandler) HandleSystemRequest(_ context.Context, serviceName, action, body string, _ model.Message) (string, error) {
	f.service = serviceName
	f.action = action
	f.body = body
	return f.reply, nil
}

func TestDispatchForwardsOnlineWSWithStripPrefix(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/service/svc1?api_key=" + plain
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("client read: %v", err)
			return
		}
		var req ForwardRequest
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Text != "hello" || req.ChatEntityID != "dev:personal:alice" {
			t.Errorf("request = %+v", req)
		}
		resp, _ := json.Marshal(ForwardResponse{Reply: "service ok"})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			t.Errorf("client write: %v", err)
		}
	}()
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"bot hello"}`}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "service ok" {
		t.Fatalf("result = %+v", result)
	}
	<-done
}

func TestDispatchOfflineReturnsFailureNoReplay(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	_ = setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", false)
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "该消息投递失败") || !strings.Contains(result.Reply, "bot hello") {
		t.Fatalf("result = %+v", result)
	}
}

func TestDispatchDoesNotMatchUnscopedAccount(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	_ = setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", false)
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:bob", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched {
		t.Fatalf("unscoped account matched: %+v", result)
	}
}

func TestDispatchEmptyScopeUsesBoundAccountsOnly(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "bot", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	alice, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !alice.Matched {
		t.Fatalf("bound account should match empty scope")
	}
	bob, err := hub.Dispatch(ctx, model.Message{ID: "m2", ChatEntityID: "dev:personal:bob", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if bob.Matched {
		t.Fatalf("unbound account matched empty scope: %+v", bob)
	}
}

func TestAddPrefixRuleRejectsWildcardOverlapInSameScope(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "openAPI???", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc1", MatchExpr: "openAPI1", AccountScopeJSON: string(scope), Enabled: true}); err == nil {
		t.Fatalf("accepted overlapping wildcard prefix")
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r3", ServiceID: "svc1", MatchExpr: "*", AccountScopeJSON: string(scope), Enabled: true}); err == nil {
		t.Fatalf("accepted high-risk * prefix")
	}
}

func TestAddPrefixRuleAllowsSamePatternForDisjointAccounts(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc2", Name: "svc2", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key1, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	_, key2, err := hub.IssueAPIKey(ctx, "svc2", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:bob"); err != nil {
		t.Fatal(err)
	}
	scopeAlice, _ := json.Marshal([]string{"dev:personal:alice"})
	scopeBob, _ := json.Marshal([]string{"dev:personal:bob"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "open*", AccountScopeJSON: string(scopeAlice), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc2", MatchExpr: "openAPI", AccountScopeJSON: string(scopeBob), Enabled: true}); err != nil {
		t.Fatalf("disjoint account scope reported conflict: %v", err)
	}
}

func TestDispatchWildcardStripPrefix(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot???", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/service/svc1?api_key=" + plain
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("client read: %v", err)
			return
		}
		var req ForwardRequest
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Text != "hello" {
			t.Errorf("request text = %q", req.Text)
		}
		resp, _ := json.Marshal(ForwardResponse{Reply: "service ok"})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			t.Errorf("client write: %v", err)
		}
	}()
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot123 hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "service ok" {
		t.Fatalf("wildcard strip/failure reply = %+v", result)
	}
	<-done
}

func TestDispatchNonEmptyScopeRequiresStillBoundAccount(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "bot", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeServiceAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched {
		t.Fatalf("revoked key still allowed scoped route: %+v", result)
	}
}

func setupRoute(t *testing.T, hub *Hub, ctx context.Context, serviceID, account, prefix string, strip bool) string {
	t.Helper()
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: serviceID, Name: serviceID, DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	plain, key, err := hub.IssueAPIKey(ctx, serviceID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, account); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{account})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{
		ID:               serviceID + "-rule",
		ServiceID:        serviceID,
		MatchExpr:        prefix,
		AccountScopeJSON: string(scope),
		StripPrefix:      strip,
		Enabled:          true,
	}); err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestHashAPIKeyStable(t *testing.T) {
	a := HashAPIKey("secret")
	b := HashAPIKey("secret")
	if a != b || a == "secret" || len(a) == 0 {
		t.Fatalf("hash invalid a=%q b=%q", a, b)
	}
}

func TestSessionAddressingFourFlows(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", keyID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	developer := dialSession(t, ctx, srv.URL, "developer", keyID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "fm1",
		ChatEntityID: "dev:personal:alice",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#home 你好"}`,
	}, "#home 你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("dispatch result = %+v", result)
	}
	gotHome := readEnvelope(t, ctx, home)
	if gotHome.To != "home#"+keyID || gotHome.From != "alice#"+keyID+"#UnifiedRobot" || gotHome.Body != "你好" {
		t.Fatalf("feishu->home envelope = %+v", gotHome)
	}
	assertEnvelopeDoesNotContain(t, gotHome, secret)

	devEnv := model.Envelope{ID: "e2", To: "home#" + keyID, From: "developer#" + keyID, Body: "内部消息"}
	if err := writeEnvelope(ctx, developer, devEnv); err != nil {
		t.Fatalf("developer write: %v", err)
	}
	gotHome = readEnvelope(t, ctx, home)
	if gotHome.ID != "e2" || gotHome.Body != "内部消息" || gotHome.From != "developer#"+keyID {
		t.Fatalf("developer->home envelope = %+v", gotHome)
	}
	assertEnvelopeDoesNotContain(t, gotHome, secret)
	claimed, _, err := hub.claimEnvelope(devEnv)
	if err != nil {
		t.Fatalf("duplicate same envelope returned error: %v", err)
	}
	if claimed {
		t.Fatalf("duplicate same envelope was claimed again")
	}
	diffEnv := devEnv
	diffEnv.Body = "不同内容"
	if _, _, err := hub.claimEnvelope(diffEnv); err == nil {
		t.Fatalf("duplicate id with different body was accepted")
	}

	feishuEnv := model.Envelope{ID: "e3", To: "alice#" + keyID + "#UnifiedRobot", From: "home#" + keyID, Body: "回飞书"}
	if err := writeEnvelope(ctx, home, feishuEnv); err != nil {
		t.Fatalf("home write: %v", err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:alice" || msg.BotChannelID != "dev" || !strings.Contains(msg.Content, "回飞书") {
		t.Fatalf("outbound message = %+v", msg)
	}
	if msg.ChatType != model.ChatPersonal || strings.Contains(messageText(t, msg), "<at user_id=") {
		t.Fatalf("personal outbound should not render at mention: %+v text=%q", msg, messageText(t, msg))
	}

	groupResult, err := hub.Dispatch(ctx, model.Message{
		ID:           "fm-group",
		ChatEntityID: "dev:group:oc_group",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alice",
		Content:      `{"text":"@_user_1 #home 群里你好"}`,
	}, "@_user_1 #home 群里你好")
	if err != nil {
		t.Fatal(err)
	}
	if !groupResult.Matched || groupResult.Reply != "" {
		t.Fatalf("group dispatch result = %+v", groupResult)
	}
	gotGroup := readEnvelope(t, ctx, home)
	if gotGroup.To != "home#"+keyID || gotGroup.From != "ou_alice#"+keyID+"#UnifiedRobot" || gotGroup.Body != "群里你好" {
		t.Fatalf("group feishu->home envelope = %+v", gotGroup)
	}
	if gotGroup.Meta["group_chat_id"] != "oc_group" || gotGroup.Meta["sender_open_id"] != "ou_alice" {
		t.Fatalf("group inbound meta = %+v", gotGroup.Meta)
	}
	assertEnvelopeDoesNotContain(t, gotGroup, secret)

	groupEnv := model.Envelope{
		ID:   "e4",
		To:   "oc_group#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "群回复",
		Meta: map[string]any{"at": []any{"ou_alice"}},
	}
	if err := writeEnvelope(ctx, home, groupEnv); err != nil {
		t.Fatalf("home group write: %v", err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:group:oc_group" || msg.BotChannelID != "dev" || msg.ChatType != model.ChatGroup {
		t.Fatalf("group outbound message = %+v", msg)
	}
	if text := messageText(t, msg); text != `<at user_id="ou_alice"></at> 群回复` {
		t.Fatalf("group outbound text = %q msg=%+v", text, msg)
	}
}

func TestSessionReplyUsesSourceBotChannelContext(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "zsf")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	if err := hub.BindAccount(ctx, keyID, "is3-Connector:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, keyID, "is3-Connector:personal:ou_sender"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, keyID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("is3-Connector", "is3-Connector")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ccbot := dialSession(t, ctx, srv.URL, "ccbot", keyID, secret)
	defer ccbot.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, keyID, "ccbot")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "source-personal",
		ChatEntityID: "is3-Connector:personal:ou_zsf",
		BotChannelID: "is3-Connector",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#ccbot ping"}`,
	}, "#ccbot ping")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("personal dispatch result=%+v", result)
	}
	gotPersonal := readEnvelope(t, ctx, ccbot)
	if gotPersonal.Meta["source_bot_channel_id"] != "is3-Connector" || gotPersonal.Meta["source_chat_type"] != string(model.ChatPersonal) || gotPersonal.Meta["source_open_id"] != "ou_zsf" {
		t.Fatalf("personal source meta=%+v", gotPersonal.Meta)
	}
	if err := writeEnvelope(ctx, ccbot, model.Envelope{
		ID:   "reply-personal",
		To:   "ou_zsf#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "个人回复",
		Meta: gotPersonal.Meta,
	}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "is3-Connector" || msg.ChatEntityID != "is3-Connector:personal:ou_zsf" || msg.ChatType != model.ChatPersonal {
		t.Fatalf("personal reply outbound=%+v", msg)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "source-group",
		ChatEntityID: "is3-Connector:group:oc_team",
		BotChannelID: "is3-Connector",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_sender",
		Content:      `{"text":"@_user_1 #ccbot group ping"}`,
	}, "@_user_1 #ccbot group ping")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("group dispatch result=%+v", result)
	}
	gotGroup := readEnvelope(t, ctx, ccbot)
	if gotGroup.Meta["source_bot_channel_id"] != "is3-Connector" || gotGroup.Meta["source_chat_type"] != string(model.ChatGroup) || gotGroup.Meta["source_chat_id"] != "oc_team" || gotGroup.Meta["source_sender_openid"] != "ou_sender" {
		t.Fatalf("group source meta=%+v", gotGroup.Meta)
	}
	if err := writeEnvelope(ctx, ccbot, model.Envelope{
		ID:   "reply-group",
		To:   "oc_team#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "群回复",
		Meta: gotGroup.Meta,
	}); err != nil {
		t.Fatal(err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "is3-Connector" || msg.ChatEntityID != "is3-Connector:group:oc_team" || msg.ChatType != model.ChatGroup {
		t.Fatalf("group reply outbound=%+v", msg)
	}
	if text := messageText(t, msg); text != `<at user_id="ou_sender"></at> 群回复` {
		t.Fatalf("group reply text=%q msg=%+v", text, msg)
	}

	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "reply-fallback",
		To:   "ou_zsf#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "默认回复",
	}); err != nil {
		t.Fatal(err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "dev" || msg.ChatEntityID != "dev:personal:ou_zsf" || msg.ChatType != model.ChatPersonal {
		t.Fatalf("fallback outbound=%+v", msg)
	}
}

func TestCrossMemberMentionRoutesToTargetSessionAndRepliesToSource(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	zsfSecret, zsfKey, err := hub.IssueAPIKey(ctx, "svc1", "zsf")
	if err != nil {
		t.Fatal(err)
	}
	fuleiSecret, fuleiKey, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, zsfKey.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, fuleiKey.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: "ou_zsf", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "fulei", DisplayName: "符坚", FeishuOpenID: "ou_fulei", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	zsf := dialSession(t, ctx, srv.URL, "home", zsfKey.ID, zsfSecret)
	defer zsf.Close(websocket.StatusNormalClosure, "done")
	fulei := dialSession(t, ctx, srv.URL, "fulei", fuleiKey.ID, fuleiSecret)
	defer fulei.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "cross-1",
		ChatEntityID: "dev:personal:ou_zsf",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"@符坚#fulei 看看这个方案"}`,
	}, "@符坚#fulei 看看这个方案")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("cross dispatch result=%+v", result)
	}
	got := readEnvelope(t, ctx, fulei)
	if got.To != "fulei#"+fuleiKey.ID || got.From != "ou_zsf#"+fuleiKey.ID+"#UnifiedRobot" || got.Body != "看看这个方案" {
		t.Fatalf("cross envelope=%+v", got)
	}
	if got.Meta["reply_prefix"] != "【符坚·fulei】" || got.Meta["cross_member"] != "fulei" {
		t.Fatalf("cross meta=%+v", got.Meta)
	}

	if err := writeEnvelope(ctx, fulei, model.Envelope{ID: "cross-reply", To: got.From, From: got.To, Body: "【符坚·fulei】已看"}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:ou_zsf" || !strings.Contains(messageText(t, msg), "【符坚·fulei】已看") {
		t.Fatalf("cross reply outbound=%+v", msg)
	}
}

func TestCrossMemberMentionRejectsNoDirectorySession(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	zsfSecret, zsfKey, err := hub.IssueAPIKey(ctx, "svc1", "zsf")
	if err != nil {
		t.Fatal(err)
	}
	fuleiSecret, fuleiKey, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, zsfKey.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, fuleiKey.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: "ou_zsf", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "fulei", DisplayName: "符坚", FeishuOpenID: "ou_fulei", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	zsf := dialSession(t, ctx, srv.URL, "home", zsfKey.ID, zsfSecret)
	defer zsf.Close(websocket.StatusNormalClosure, "done")
	secOps := dialSessionWithQuery(t, ctx, srv.URL, "sec-ops", fuleiKey.ID, fuleiSecret, "no_directory=1")
	defer secOps.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "cross-hidden",
		ChatEntityID: "dev:personal:ou_zsf",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"@符坚#sec-ops 帮忙看"}`,
	}, "@符坚#sec-ops 帮忙看")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "该会话为专职隔离会话不接受任务派发" {
		t.Fatalf("hidden cross dispatch result=%+v", result)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "direct-hidden",
		ChatEntityID: "dev:personal:ou_fulei",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#sec-ops 自查"}`,
	}, "#sec-ops 自查")
	if err != nil || !result.Matched {
		t.Fatalf("direct hidden dispatch result=%+v err=%v", result, err)
	}
	got := readEnvelope(t, ctx, secOps)
	if got.To != "sec-ops#"+fuleiKey.ID || got.Body != "自查" {
		t.Fatalf("direct hidden envelope=%+v", got)
	}
}

func TestCrossMemberMentionRequiresSessionWhenMultipleOnline(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "fulei", DisplayName: "符坚", FeishuOpenID: "ou_fulei", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	dev := dialSession(t, ctx, srv.URL, "dev", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "cross-amb", ChatEntityID: "dev:personal:ou_zsf", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@fulei 帮我看")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "多个在线会话") {
		t.Fatalf("ambiguous cross result=%+v", result)
	}
}

func TestCrossMemberMentionHonorsDMOptOut(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "fulei", DisplayName: "符坚", FeishuOpenID: "ou_fulei", Role: model.RoleMember, DMOptOut: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{ID: "cross-optout", ChatEntityID: "dev:personal:ou_zsf", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@符坚#fulei hi")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "未开放会话接入") {
		t.Fatalf("optout cross result=%+v", result)
	}
}

func TestPersonalMessageDefaultsToOnlyOnlineSession(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	fulei := dialSession(t, ctx, srv.URL, "fulei", key.ID, secret)
	defer fulei.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "default-1",
		ChatEntityID: "dev:personal:ou_fulei",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"你好"}`,
	}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("default dispatch result=%+v", result)
	}
	got := readEnvelope(t, ctx, fulei)
	if got.To != "fulei#"+key.ID || got.From != "ou_fulei#"+key.ID+"#UnifiedRobot" || got.Body != "你好" {
		t.Fatalf("default envelope=%+v", got)
	}
	if got.Meta["reply_prefix"] != "【fulei】" {
		t.Fatalf("default envelope meta=%+v", got.Meta)
	}

	if err := writeEnvelope(ctx, fulei, model.Envelope{ID: "default-reply", To: got.From, From: got.To, Body: "【fulei】你好"}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:ou_fulei" || !strings.Contains(messageText(t, msg), "【fulei】你好") {
		t.Fatalf("default reply outbound=%+v", msg)
	}
}

func TestPersonalMessageDefaultRouteRequiresExplicitSessionWhenMultipleOnline(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	fulei := dialSession(t, ctx, srv.URL, "fulei", key.ID, secret)
	defer fulei.Close(websocket.StatusNormalClosure, "done")
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "default-many", ChatEntityID: "dev:personal:ou_fulei", BotChannelID: "dev", ChatType: model.ChatPersonal}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "多个在线会话") || !strings.Contains(result.Reply, "fulei") || !strings.Contains(result.Reply, "home") {
		t.Fatalf("multi default result=%+v", result)
	}
}

func TestPersonalMessageDefaultRouteNoSessionAndGroupAreIgnored(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	personal, err := hub.Dispatch(ctx, model.Message{ID: "default-none", ChatEntityID: "dev:personal:ou_fulei", BotChannelID: "dev", ChatType: model.ChatPersonal}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if personal.Matched || personal.Reply != "" {
		t.Fatalf("no-session personal result=%+v", personal)
	}
	group, err := hub.Dispatch(ctx, model.Message{ID: "default-group", ChatEntityID: "dev:group:oc_group", BotChannelID: "dev", ChatType: model.ChatGroup, SenderOpenID: "ou_fulei"}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if group.Matched || group.Reply != "" {
		t.Fatalf("group default result=%+v", group)
	}
}

func TestSessionAddressingRejectsCrossKey(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", keyID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "bad", To: "developer#other", From: "home#" + keyID, Body: "x"}); err != nil {
		t.Fatalf("write bad envelope: %v", err)
	}
	got := readEnvelope(t, ctx, home)
	if !strings.Contains(got.Body, "key_id") {
		t.Fatalf("cross-key error envelope = %+v", got)
	}
	assertEnvelopeDoesNotContain(t, got, secret)
}

func TestCollectEnvelopeStoresOnlyAndHonorsOptOut(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "collect-1",
		To:   "workpulse#" + key.ID,
		From: "home#" + key.ID,
		Body: "完成自主研发",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "collect", "role": "claude", "session": "home"},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := db.RecentMessages(ctx, model.MessageFilter{ChatEntityID: "home#" + key.ID, Limit: 10})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("collect msgs=%+v err=%v", msgs, err)
	}
	if msgs[0].Direction != model.DirectionCollect || msgs[0].Status != "done" || !strings.Contains(msgs[0].Content, "完成自主研发") {
		t.Fatalf("collect message = %+v", msgs[0])
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionCollect)
	if err != nil || next != nil {
		t.Fatalf("collect should not be claimable next=%+v err=%v", next, err)
	}
	if out, err := db.ClaimNextMessage(ctx, model.DirectionOut); err != nil || out != nil {
		t.Fatalf("collect should not enqueue outbound out=%+v err=%v", out, err)
	}

	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, EvidenceOptOut: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "collect-2",
		To:   "workpulse#" + key.ID,
		From: "home#" + key.ID,
		Body: "不应采集",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "collect", "role": "claude", "session": "home"},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err = db.RecentMessages(ctx, model.MessageFilter{ChatEntityID: "home#" + key.ID, Limit: 10})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("optout collect msgs=%+v err=%v", msgs, err)
	}
}

func TestSessionEndpointTracksClientIPAndOffline(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSessionWithHeader(t, ctx, srv.URL, "home", key.ID, secret, http.Header{"X-Forwarded-For": {"203.0.113.7, 10.0.0.1"}, "X-Real-IP": {"198.51.100.9"}})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].Active || endpoints[0].ClientIP != "203.0.113.7" {
		t.Fatalf("endpoint after connect=%+v err=%v", endpoints, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "done")
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err = db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(endpoints) == 1 && !endpoints[0].Active && endpoints[0].ClientIP == "203.0.113.7" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint not offline after close: %+v", endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionEndpointStoresToolAndModel(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSessionWithQuery(t, ctx, srv.URL, "developer", key.ID, secret, "tool=CODEX&model=gpt-5.5&full_session_name=sh-developer-e0d12642")
	defer conn.Close(websocket.StatusNormalClosure, "done")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if endpoints[0].Tool != "CODEX" || endpoints[0].Model != "gpt-5.5" {
		t.Fatalf("tool/model not stored: %+v", endpoints[0])
	}
	if endpoints[0].SessionName != "developer" || endpoints[0].FullSessionName != "sh-developer-e0d12642" {
		t.Fatalf("session names not stored correctly: %+v", endpoints[0])
	}
}

func TestSessionEndpointStoresHandshakeMirrorToBotUnchanged(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mirrorTo := "ou_zsf#" + key.ID + "#is3-Connector"
	conn := dialSessionWithQuery(t, ctx, srv.URL, "ts3-poc", key.ID, secret, "target_bot=CC-Connector&mirror_to="+url.QueryEscape(mirrorTo))
	defer conn.Close(websocket.StatusNormalClosure, "done")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != mirrorTo {
		t.Fatalf("mirror_to should preserve source bot: %+v want=%q", endpoints[0], mirrorTo)
	}

	reconnect := dialSessionWithQuery(t, ctx, srv.URL, "ts3-poc", key.ID, secret, "target_bot=CC-Connector")
	defer reconnect.Close(websocket.StatusNormalClosure, "done")
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints after reconnect=%+v err=%v", endpoints, err)
	}
	if !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != mirrorTo {
		t.Fatalf("reconnect without mirror_to should keep stored target: %+v want=%q", endpoints[0], mirrorTo)
	}
}

func TestSessionNameAutoSuffixWithinOwner(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_zsf", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_zsf", BoundOwner: "zsf", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_zsf_2", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_zsf_2", BoundOwner: "zsf", Active: true}); err != nil {
		t.Fatal(err)
	}
	secret1, key1, err := hub.IssueAPIKey(ctx, "svc1", "zsf1")
	if err != nil {
		t.Fatal(err)
	}
	secret2, key2, err := hub.IssueAPIKey(ctx, "svc1", "zsf2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:ou_zsf_2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	first := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	defer first.Close(websocket.StatusNormalClosure, "done")
	second := dialSession(t, ctx, srv.URL, "developer", key2.ID, secret2)
	defer second.Close(websocket.StatusNormalClosure, "done")

	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, ep := range endpoints {
		if ep.Active {
			got[ep.KeyID] = ep.SessionName
		}
	}
	if got[key1.ID] != "developer" || got[key2.ID] != "developer2" {
		t.Fatalf("effective names = %+v endpoints=%+v", got, endpoints)
	}

	env := model.Envelope{ID: "suffix-rewrite", To: "workpulse#" + key2.ID, From: "developer#" + key2.ID, Body: "ok"}
	if err := writeEnvelope(ctx, second, env); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err = db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range endpoints {
			if ep.KeyID == key2.ID && ep.SessionName == "developer2" && ep.Active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rewritten effective session missing: %+v", endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionNameReconnectKeepsNameAndReleaseReuses(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_zsf", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_zsf", BoundOwner: "zsf", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_zsf_2", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_zsf_2", BoundOwner: "zsf", Active: true}); err != nil {
		t.Fatal(err)
	}
	secret1, key1, err := hub.IssueAPIKey(ctx, "svc1", "zsf1")
	if err != nil {
		t.Fatal(err)
	}
	secret2, key2, err := hub.IssueAPIKey(ctx, "svc1", "zsf2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:ou_zsf_2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	first := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	reconnect := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	defer reconnect.Close(websocket.StatusNormalClosure, "done")
	_ = first.Close(websocket.StatusNormalClosure, "replaced")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	activeByKey := map[string]string{}
	for _, ep := range endpoints {
		if ep.Active {
			activeByKey[ep.KeyID] = ep.SessionName
		}
	}
	if activeByKey[key1.ID] != "developer" {
		t.Fatalf("reconnect should keep developer: %+v", endpoints)
	}

	_ = reconnect.Close(websocket.StatusNormalClosure, "done")
	waitEndpointInactive(t, ctx, db, key1.ID, "developer")
	second := dialSession(t, ctx, srv.URL, "developer", key2.ID, secret2)
	defer second.Close(websocket.StatusNormalClosure, "done")
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range endpoints {
		if ep.KeyID == key2.ID && ep.Active && ep.SessionName == "developer" {
			return
		}
	}
	t.Fatalf("released name was not reused: %+v", endpoints)
}

func TestSessionEndpointOwnerActiveUniqueIndex(t *testing.T) {
	_, db, ctx := newTestHub(t)
	now := time.Now()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-1", SessionName: "developer", OwnerKey: "zsf", LastSeenAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-2", SessionName: "developer", OwnerKey: "zsf", LastSeenAt: now, Active: true}); err == nil {
		t.Fatal("expected active owner/session unique violation")
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-1", SessionName: "developer", OwnerKey: "zsf", LastSeenAt: now, Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-2", SessionName: "developer", OwnerKey: "zsf", LastSeenAt: now, Active: true}); err != nil {
		t.Fatalf("name should be reusable after inactive: %v", err)
	}
}

func TestOnlineDirectoryBroadcastOwnerIsolationAndUnknownMetadata(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = 30 * time.Millisecond
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: "ou_zsf", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "fulei", DisplayName: "符坚", FeishuOpenID: "ou_fulei", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	zsfSecret, zsfKey, err := hub.IssueAPIKey(ctx, "svc1", "zsf")
	if err != nil {
		t.Fatal(err)
	}
	fuleiSecret, fuleiKey, err := hub.IssueAPIKey(ctx, "svc1", "fulei")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, zsfKey.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, fuleiKey.ID, "dev:personal:ou_fulei"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	zsfDev := dialSessionWithQuery(t, ctx, srv.URL, "developer", zsfKey.ID, zsfSecret, "tool=CODEX&model=gpt-5.5&full_session_name=sh-developer-e0d12642")
	defer zsfDev.Close(websocket.StatusNormalClosure, "done")
	zsfHome := dialSession(t, ctx, srv.URL, "home", zsfKey.ID, zsfSecret)
	defer zsfHome.Close(websocket.StatusNormalClosure, "done")
	fuleiDev := dialSessionWithQuery(t, ctx, srv.URL, "developer", fuleiKey.ID, fuleiSecret, "tool=CLAUDE&model=opus&full_session_name=sh-developer-fulei")
	defer fuleiDev.Close(websocket.StatusNormalClosure, "done")
	zsfSecOps := dialSessionWithQuery(t, ctx, srv.URL, "sec-ops", zsfKey.ID, zsfSecret, "tool=SEC&model=monitor&no_directory=1")
	defer zsfSecOps.Close(websocket.StatusNormalClosure, "done")

	zsfDevMsg := readEnvelope(t, ctx, zsfDev)
	zsfHomeMsg := readEnvelope(t, ctx, zsfHome)
	fuleiMsg := readEnvelope(t, ctx, fuleiDev)
	if !strings.Contains(zsfDevMsg.Body, "sh-developer-e0d12642") || !strings.Contains(zsfDevMsg.Body, "home") {
		t.Fatalf("zsf directory missing own sessions: %s", zsfDevMsg.Body)
	}
	if strings.Contains(zsfDevMsg.Body, "sec-ops") || strings.Contains(zsfHomeMsg.Body, "sec-ops") {
		t.Fatalf("no_directory session leaked into directory: %s / %s", zsfDevMsg.Body, zsfHomeMsg.Body)
	}
	if strings.Contains(zsfDevMsg.Body, "fulei") || strings.Contains(zsfHomeMsg.Body, "fulei") {
		t.Fatalf("zsf directory leaked fulei: %s / %s", zsfDevMsg.Body, zsfHomeMsg.Body)
	}
	if !strings.Contains(fuleiMsg.Body, "sh-developer-fulei") || strings.Contains(fuleiMsg.Body, "ou_zsf") || strings.Contains(fuleiMsg.Body, "home") {
		t.Fatalf("fulei directory wrong/leaked: %s", fuleiMsg.Body)
	}
	for _, want := range []string{"【DingWei在线清单】", "#developer · CODEX/gpt-5.5", "· 全名:sh-developer-e0d12642", "@zsf#developer", "(末"} {
		if !strings.Contains(zsfDevMsg.Body, want) {
			t.Fatalf("zsf directory missing %q: %s", want, zsfDevMsg.Body)
		}
	}
	if !strings.Contains(zsfDevMsg.Body, "#home · 未知/未知") {
		t.Fatalf("old session metadata should render unknown: %s", zsfDevMsg.Body)
	}
	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "hidden-direct",
		ChatEntityID: "dev:personal:ou_zsf",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#sec-ops 直接派活"}`,
	}, "#sec-ops 直接派活")
	if err != nil || !result.Matched {
		t.Fatalf("direct hidden session dispatch result=%+v err=%v", result, err)
	}
	hidden := readEnvelope(t, ctx, zsfSecOps)
	if hidden.To != "sec-ops#"+zsfKey.ID || hidden.Body != "直接派活" {
		t.Fatalf("hidden session should still be addressable: %+v", hidden)
	}
	for i := 0; i < 2; i++ {
		msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
		if err != nil || msg == nil {
			t.Fatalf("dm %d missing msg=%+v err=%v", i, msg, err)
		}
		if msg.ChatEntityID != "dev:personal:ou_zsf" && msg.ChatEntityID != "dev:personal:ou_fulei" {
			t.Fatalf("unexpected dm target: %+v", msg)
		}
	}
}

func TestOnlineDirectoryDebouncesFlapping(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = 80 * time.Millisecond
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: "ou_zsf", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "zsf")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	time.Sleep(20 * time.Millisecond)
	dev := dialSession(t, ctx, srv.URL, "sh-developer-e0d12642", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")

	gotHome := readEnvelope(t, ctx, home)
	gotDev := readEnvelope(t, ctx, dev)
	if strings.Count(gotHome.Body, "\n1. #") != 1 || strings.Count(gotHome.Body, "\n2. #") != 1 || strings.Count(gotDev.Body, "\n1. #") != 1 || strings.Count(gotDev.Body, "\n2. #") != 1 {
		t.Fatalf("debounced directory should contain final two sessions:\nhome=%s\ndev=%s", gotHome.Body, gotDev.Body)
	}
	assertNoEnvelope(t, ctx, home, 120*time.Millisecond)
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ChatEntityID != "dev:personal:ou_zsf" {
		t.Fatalf("dm missing after debounce msg=%+v err=%v", msg, err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("debounce should enqueue one dm, got msg=%+v err=%v", msg, err)
	}
}

func TestOnlineDirectorySkipsNoDirectoryPresenceBroadcast(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "zsf", DisplayName: "张三丰", FeishuOpenID: "ou_zsf", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-hidden", "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-visible", "dev:personal:ou_zsf"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-hidden", SessionName: "sec-ops", OwnerKey: "zsf", LastSeenAt: time.Now(), Active: true, NoDirectory: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-visible", SessionName: "developer", OwnerKey: "zsf", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	hub.scheduleOnlineBroadcastForSession(ctx, "FB-hidden", "sec-ops")
	hub.mu.Lock()
	hiddenTimer := hub.onlineTimers["zsf"]
	hub.mu.Unlock()
	if hiddenTimer != nil {
		hiddenTimer.Stop()
		t.Fatal("no_directory presence should not schedule online directory broadcast")
	}
	hub.scheduleOnlineBroadcastForSession(ctx, "FB-visible", "developer")
	hub.mu.Lock()
	visibleTimer := hub.onlineTimers["zsf"]
	hub.mu.Unlock()
	if visibleTimer == nil {
		t.Fatal("visible presence should schedule online directory broadcast")
	}
	visibleTimer.Stop()
}

func TestOnlineDirectoryBroadcastMarksSingleMirrorPrimary(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	developer := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")
	review := dialSession(t, ctx, srv.URL, "review", key.ID, secret)
	defer review.Close(websocket.StatusNormalClosure, "done")

	if err := hub.BroadcastOnlineDirectories(ctx, false); err != nil {
		t.Fatal(err)
	}
	envs := []model.Envelope{
		readEnvelope(t, ctx, home),
		readEnvelope(t, ctx, developer),
		readEnvelope(t, ctx, review),
	}
	keys := map[string]bool{}
	primaries := 0
	for _, env := range envs {
		if env.Meta["type"] != "online_directory" {
			t.Fatalf("unexpected broadcast meta: %+v", env.Meta)
		}
		key, _ := env.Meta["broadcast_dedup_key"].(string)
		if key == "" {
			t.Fatalf("missing broadcast_dedup_key: %+v", env.Meta)
		}
		keys[key] = true
		if primary, _ := env.Meta["mirror_primary"].(bool); primary {
			primaries++
		}
	}
	if len(keys) != 1 || primaries != 1 {
		t.Fatalf("broadcast mirror metadata keys=%v primaries=%d envs=%+v", keys, primaries, envs)
	}
}

func TestProducerSessionMetadataAndOnlineDirectory(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "system-v-task-internal", DisplayName: "SYSTEM-V-TASK-INTERNAL", Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "system-v-task-internal"); err != nil {
		t.Fatal(err)
	}
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	producer := dialSessionWithQuery(t, ctx, srv.URL, "producer", key.ID, secret, "producer=1&target_group=oc_ai")
	defer producer.Close(websocket.StatusNormalClosure, "done")

	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].Producer || endpoints[0].TargetGroup != "oc_ai" {
		t.Fatalf("producer endpoint=%+v err=%v", endpoints, err)
	}
	if err := hub.BroadcastOnlineDirectories(ctx, true); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, producer)
	if !strings.Contains(got.Body, "Producer:是→oc_ai") {
		t.Fatalf("producer directory missing marker: %s", got.Body)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("system producer directory should not DM real user msg=%+v err=%v", msg, err)
	}
}

func TestProducerSessionTargetBotQueryRoutesGroup(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("CC-Connector", "CC-Connector")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "system-v-task-internal", DisplayName: "SYSTEM-V-TASK-INTERNAL", Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "system-v-task-internal"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	producer := dialSessionWithQuery(t, ctx, srv.URL, "producer", key.ID, secret, "producer=1&target_group=oc_alert&target_bot=CC-Connector")
	defer producer.Close(websocket.StatusNormalClosure, "done")

	if err := writeEnvelope(ctx, producer, model.Envelope{
		ID:   "producer-query-target-bot",
		To:   "oc_alert#" + key.ID + "#UnifiedRobot",
		From: "producer#" + key.ID,
		Body: "告警",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_alert", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if msg.BotChannelID != "CC-Connector" || msg.ChatEntityID != "CC-Connector:group:oc_alert" {
		t.Fatalf("producer query target_bot msg=%+v", msg)
	}
}

func TestClientIPFromRequestTrustsOnlyLoopbackProxy(t *testing.T) {
	trusted := httptest.NewRequest("GET", "/ws/session/home", nil)
	trusted.RemoteAddr = "127.0.0.1:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.2")
	if got := clientIPFromRequest(trusted); got != "203.0.113.8" {
		t.Fatalf("trusted proxy client ip=%q", got)
	}
	untrusted := httptest.NewRequest("GET", "/ws/session/home", nil)
	untrusted.RemoteAddr = "198.51.100.10:5678"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.8")
	if got := clientIPFromRequest(untrusted); got != "198.51.100.10" {
		t.Fatalf("untrusted proxy client ip=%q", got)
	}
}

func TestSessionWildcardRouteDeliversEnvelope(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "tenant1", Name: "tenant", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "tenant1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	sessionService := sessionServiceID(key.ID, "home")
	if err := db.UpsertRegisteredService(ctx, model.RegisteredService{ID: sessionService, Name: "会话 home", DeliveryType: "session", ReplyMode: "none", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "sr1", ServiceID: sessionService, MatchExpr: "dev*", StripPrefix: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "m-wild", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"dev hello"}`}, "dev hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("dispatch result=%+v", result)
	}
	got := readEnvelope(t, ctx, home)
	if got.To != "home#"+key.ID || got.Body != "hello" || got.Meta["route_id"] != "sr1" {
		t.Fatalf("wildcard envelope=%+v", got)
	}
}

func TestMirrorCommandDMControlsSessionAndGroupIgnored(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "mirror on home")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "镜像已开启") {
		t.Fatalf("mirror on result = %+v", result)
	}
	got := readEnvelope(t, ctx, home)
	if got.To != "home#"+key.ID || got.From != "workpulse#"+key.ID || got.Meta["type"] != "mirror_control" || got.Meta["enabled"] != true {
		t.Fatalf("mirror on envelope = %+v", got)
	}
	if got.Meta["mirror_to"] != "alice#"+key.ID+"#UnifiedRobot" {
		t.Fatalf("mirror target = %+v", got.Meta["mirror_to"])
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].MirrorEnabled {
		t.Fatalf("mirror state after on endpoints=%+v err=%v", endpoints, err)
	}

	groupResult, err := hub.Dispatch(ctx, model.Message{ID: "m2", ChatEntityID: "dev:group:oc_group", BotChannelID: "dev", ChatType: model.ChatGroup, SenderOpenID: "alice"}, "mirror off home")
	if err != nil {
		t.Fatal(err)
	}
	if !groupResult.Matched || groupResult.Reply != "" {
		t.Fatalf("group mirror result = %+v", groupResult)
	}
	endpoints, _ = db.ListSessionEndpoints(ctx)
	if !endpoints[0].MirrorEnabled {
		t.Fatalf("group mirror command changed state: %+v", endpoints[0])
	}

	result, err = hub.Dispatch(ctx, model.Message{ID: "m3", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "mirror off home")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "镜像已关闭") {
		t.Fatalf("mirror off result = %+v", result)
	}
	got = readEnvelope(t, ctx, home)
	if got.Meta["type"] != "mirror_control" || got.Meta["enabled"] != false || got.Meta["mirror_to"] != "" {
		t.Fatalf("mirror off envelope = %+v", got)
	}
	endpoints, _ = db.ListSessionEndpoints(ctx)
	if endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != "" {
		t.Fatalf("mirror state after off: %+v", endpoints[0])
	}
}

func TestParseSelectorRejectsHashSpace(t *testing.T) {
	if name, _, ok := parseSelector("#home hello"); !ok || name != "home" {
		t.Fatalf("#home selector not parsed: name=%q ok=%v", name, ok)
	}
	if name, body, ok := parseSelector("@_user_1 #home hello"); !ok || name != "home" || body != "hello" {
		t.Fatalf("@ placeholder selector not parsed: name=%q body=%q ok=%v", name, body, ok)
	}
	if _, _, ok := parseSelector("# home hello"); ok {
		t.Fatalf("# followed by space should not be parsed as selector")
	}
	if action, session, ok := parseMirrorCommand("mirror on home"); !ok || action != "on" || session != "home" {
		t.Fatalf("mirror command not parsed: action=%q session=%q ok=%v", action, session, ok)
	}
	hub, _, _ := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if name, body, ok := hub.parseSelector("dev", "@UnifiedRobot #home hello"); !ok || name != "home" || body != "hello" {
		t.Fatalf("literal bot mention selector not parsed: name=%q body=%q ok=%v", name, body, ok)
	}
}

func TestRouteToSessionAcksDeliveredInboundMessage(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "fm-delivered",
		ChatEntityID: "dev:personal:alice",
		Direction:    model.DirectionIn,
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#home ping"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "fm-delivered",
		To:   "home#" + key.ID,
		From: "alice#" + key.ID + "#UnifiedRobot",
		Body: "ping",
		TS:   time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, home)
	if got.ID != "fm-delivered" || got.Body != "ping" {
		t.Fatalf("delivered envelope = %+v", got)
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage: %v", err)
	}
	if next != nil {
		t.Fatalf("delivered inbound message was re-claimable: %+v", next)
	}
}

func TestNoMirrorSystemEnvelopeSkipsFeishuMirrorButNormalMirrors(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("CC-Connector", "CC-Connector")
	keyID := "FB-test"
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "sys-online",
		To:   "ou_alice#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "在线清单",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "online_directory", "system": true, "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("system no_mirror should not enqueue mirror msg=%+v err=%v", msg, err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "normal-mirror",
		To:   "ou_alice#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "普通 AI 输出",
		TS:   time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("normal mirror missing msg=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "dev:personal:ou_alice" || !strings.Contains(msg.Content, "普通 AI 输出") {
		t.Fatalf("normal mirror msg=%+v", msg)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "CC-Connector:group:oc_group", BotChannelID: "CC-Connector", Type: model.ChatGroup, FeishuID: "oc_group", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-group",
		To:   "oc_group#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "系统任务产出",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_group", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("producer group msg missing msg=%+v err=%v", msg, err)
	}
	if msg.BotChannelID != "CC-Connector" || msg.ChatEntityID != "CC-Connector:group:oc_group" || !strings.Contains(msg.Content, "系统任务产出") {
		t.Fatalf("producer group msg=%+v", msg)
	}
}

func TestProducerTargetBotOverrideAndUnknownGroupDoesNotDefault(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("CC-Connector", "CC-Connector")
	keyID := "FB-test"
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:group:oc_alert", BotChannelID: "dev", Type: model.ChatGroup, FeishuID: "oc_alert", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-override",
		To:   "oc_alert#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "告警",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_alert", "target_bot": "CC-Connector", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("producer override missing msg=%+v err=%v", msg, err)
	}
	if msg.BotChannelID != "CC-Connector" || msg.ChatEntityID != "CC-Connector:group:oc_alert" {
		t.Fatalf("producer override msg=%+v", msg)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-unknown",
		To:   "oc_unknown#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "未知群",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_unknown", "no_mirror": true},
	}); err == nil {
		t.Fatal("unknown producer target group should fail instead of defaulting to UnifiedRobot")
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("unknown producer target should not enqueue msg=%+v err=%v", msg, err)
	}
}

func dialSession(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret string) *websocket.Conn {
	t.Helper()
	return dialSessionWithHeader(t, ctx, baseURL, sessionName, keyID, secret, nil)
}

func dialSessionWithHeader(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret string, header http.Header) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/session/" + sessionName + "?key_id=" + keyID
	if header == nil {
		header = http.Header{}
	}
	header.Set("Authorization", "Bearer "+secret)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("Dial %s: %v", sessionName, err)
	}
	return conn
}

func dialSessionWithQuery(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret, query string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/session/" + sessionName + "?key_id=" + keyID
	if strings.TrimSpace(query) != "" {
		wsURL += "&" + query
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+secret)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("Dial %s: %v", sessionName, err)
	}
	return conn
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env model.Envelope) error {
	payload, _ := json.Marshal(env)
	return conn.Write(ctx, websocket.MessageText, payload)
}

func readEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn) model.Envelope {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertNoEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn, wait time.Duration) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err == nil {
		var env model.Envelope
		_ = json.Unmarshal(data, &env)
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func waitEndpointInactive(t *testing.T, ctx context.Context, db *store.SQLite, keyID, sessionName string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err := db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range endpoints {
			if ep.KeyID == keyID && ep.SessionName == sessionName && !ep.Active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint did not become inactive key=%s session=%s endpoints=%+v", keyID, sessionName, endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitSessionOnline(t *testing.T, hub *Hub, keyID, sessionName string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if hub.sessionOnline(keyID, sessionName) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not become online key=%s session=%s", keyID, sessionName)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertEnvelopeDoesNotContain(t *testing.T, env model.Envelope, secret string) {
	t.Helper()
	payload, _ := json.Marshal(env)
	if strings.Contains(string(payload), secret) {
		t.Fatalf("envelope leaked secret %q: %s", secret, payload)
	}
}

func messageText(t *testing.T, msg *model.Message) string {
	t.Helper()
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		t.Fatalf("unmarshal message content: %v content=%q", err, msg.Content)
	}
	return payload.Text
}

func waitOutbound(t *testing.T, ctx context.Context, q *bus.DBQueue) *model.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msg, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("outbound dequeue: %v", err)
		}
		if msg != nil {
			return msg
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
