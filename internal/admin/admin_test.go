package admin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/datad"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newAdminTestServer(t *testing.T) (*Server, *store.SQLite, *http.ServeMux, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := New(db)
	srv.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	srv.SecretKey = "test-secret-key"
	mux := http.NewServeMux()
	srv.Mount(mux)
	return srv, db, mux, ctx
}

func TestAdminRequiresLogin(t *testing.T) {
	_, _, mux, _ := newAdminTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("location=%q", loc)
	}
}

func TestAdminAgentGroupingMinimalEntryConfiguresCodeAndLeader(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "key-a", SessionName: "owner-agent-a", OwnerKey: "owner", Active: true}); err != nil {
		t.Fatal(err)
	}
	code := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rec := postFormRaw(mux, "/admin/agent-groups", url.Values{
		"action": {"upsert_group"}, "code": {code}, "leader_key_id": {"key-a"},
		"leader_session_name": {"owner-agent-a"}, "leader_owner_key": {"owner"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/agent-groups" {
		t.Fatalf("POST status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	page := getAuth(t, mux, "/admin/agent-groups")
	if !strings.Contains(page.Body.String(), code) || !strings.Contains(page.Body.String(), "owner/owner-agent-a#key-a") {
		t.Fatalf("group page missing configured group: %s", page.Body.String())
	}
}

func TestAdminAggregateManualEndpoints(t *testing.T) {
	srv, db, mux, _ := newAdminTestServer(t)
	svc := scheduler.New(scheduler.Config{}, nil, nil, srv.Outbound)
	svc.Repo = db
	srv.Scheduler = svc

	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/admin/aggregate/draft-now", nil))
	if unauth.Code != http.StatusSeeOther || unauth.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unauth draft code=%d location=%q", unauth.Code, unauth.Header().Get("Location"))
	}

	srv.sessions["tok"] = "admin"
	for _, path := range []string{"/admin/aggregate/draft-now", "/admin/aggregate/publish-now"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var got struct {
			Processed  int      `json:"processed"`
			ProjectIDs []string `json:"project_ids"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s json: %v body=%s", path, err, rec.Body.String())
		}
		if got.Processed != 0 || len(got.ProjectIDs) != 0 {
			t.Fatalf("%s response=%+v", path, got)
		}
	}
}

func TestPortalRendersTeamScheduleProgressAndRisk(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{ID: "s1", OwnerKey: "alice", StartDate: "2026-07-20", EndDate: "2026-07-21", Task: "写方案", Status: "planned", Priority: 100}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "写方案", Note: "完成60%", Percent: 60, ReportedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReportRisk(ctx, model.Risk{OwnerKey: "alice", Content: "环境不稳", Status: "open"}); err != nil {
		t.Fatal(err)
	}

	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/portal", nil))
	if unauth.Code != http.StatusSeeOther || unauth.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unauth portal code=%d location=%q", unauth.Code, unauth.Header().Get("Location"))
	}

	srv.sessions["tok"] = "admin"
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"WorkPulse 团队门户", "Alice", "写方案", "完成60%", "环境不稳"} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal missing %q in %s", want, body)
		}
	}
}

func TestAdminStatusRequiresAuthAndShowsStats(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	if err := db.EnqueueMessage(ctx, model.Message{ID: "m1", ChatEntityID: "chat1", Direction: model.DirectionIn, BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"hi"}`}); err != nil {
		t.Fatal(err)
	}
	srv.sessions["tok"] = "admin"
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "服务运行中") || !strings.Contains(body, "queued: 1") || !strings.Contains(body, "/admin/messages") || !strings.Contains(body, "/admin/control-plane") {
		t.Fatalf("admin status body = %s", body)
	}
}

func TestAdminControlPlaneDashboardEmptyData(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/admin/control-plane", nil))
	if unauth.Code != http.StatusSeeOther || unauth.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unauth dashboard code=%d location=%q", unauth.Code, unauth.Header().Get("Location"))
	}
	srv.sessions["tok"] = "admin"
	body := getAuth(t, mux, "/admin/control-plane").Body.String()
	for _, want := range []string{"总控指标看板", "队列深度", "暂无总控任务", "暂无 L2 处理记录", "0.00%"} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty dashboard missing %q in %s", want, body)
		}
	}
}

func TestAdminControlPlaneDashboardWithData(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	now := time.Now().UTC()
	tasks := []model.ControlTask{
		{ID: "ctl-queued", Status: "queued", Priority: 1},
		{ID: "ctl-awaiting", Status: "awaiting_children", Priority: 2},
		{ID: "ctl-done", Status: "done"},
		{ID: "ctl-failed", Status: "failed", Error: "boom"},
	}
	for _, task := range tasks {
		task.Source = "feishu"
		task.SourceAddr = "ou_1#dev#UnifiedRobot"
		task.OwnerKey = "u1"
		task.RawInput = task.ID
		task.CreatedAt = now
		if _, _, err := db.EnqueueControlTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CompleteControlTaskL2(ctx, "l2-ok", "dispatch", "developer", "ok", 120*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordControlTaskL2Failure(ctx, "l2-fail", "down", 360*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	body := getAuth(t, mux, "/admin/control-plane").Body.String()
	for _, want := range []string{
		"队列深度", "<b>2</b>", "Total", "<b>4</b>",
		"失败率", "25.00%", "awaiting_children", "纳入 Depth",
		"L2 近段处理", "最近记录", "<b>2</b>", "成功", "失败", "360ms",
		"queued 1", "failed 1", "l2-ok", "l2-fail", "<svg",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q in %s", want, body)
		}
	}
}

func TestAdminMessagesRedactsAndFilters(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.EnqueueMessage(ctx, model.Message{ID: "m1", ChatEntityID: "chat1", Direction: model.DirectionIn, BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"token":"abc123","text":"visible"}`}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{ID: "m2", ChatEntityID: "chat2", Direction: model.DirectionIn, BotChannelID: "other", ChatType: model.ChatPersonal, Content: `{"text":"other"}`}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/messages?channel=dev", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "visible") || strings.Contains(body, "abc123") || strings.Contains(body, "other") {
		t.Fatalf("messages redaction/filter body = %s", body)
	}
	if !strings.Contains(body, "token") || !strings.Contains(body, "***") {
		t.Fatalf("token not redacted as expected: %s", body)
	}
}

func TestAdminM9ConfigCRUDAndRouteConflict(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	postForm(t, mux, "/admin/members", url.Values{"owner_key": {"dev:personal:alice"}, "display_name": {"Alice"}, "role": {"member"}})
	if m, _ := db.GetMemberByOwnerKey(ctx, "dev:personal:alice"); m == nil || m.DisplayName != "Alice" {
		t.Fatalf("member not saved: %+v", m)
	}
	postForm(t, mux, "/admin/bot-channels", url.Values{"id": {"dev"}, "name": {"Dev Bot"}, "app_id": {"cli_x"}, "app_secret": {"secret-value"}, "verification_token": {"verify-value"}, "encrypt_key": {"encrypt-value"}, "purpose": {"general"}})
	channels, _ := db.ListBotChannels(ctx)
	devChannel := findBotChannel(channels, "dev")
	if devChannel == nil || !devChannel.AppSecretSet || strings.Contains(devChannel.AppSecretEnc, "secret-value") || devChannel.VerificationToken != "verify-value" || devChannel.EncryptKey != "encrypt-value" {
		t.Fatalf("channels = %+v", channels)
	}
	listBots := getAuth(t, mux, "/admin/bot-channels")
	if strings.Contains(listBots.Body.String(), "secret-value") || strings.Contains(listBots.Body.String(), "verify-value") || strings.Contains(listBots.Body.String(), "encrypt-value") || !strings.Contains(listBots.Body.String(), "已设置") {
		t.Fatalf("bot channel page leaked or missed secret status: %s", listBots.Body.String())
	}
	postForm(t, mux, "/admin/services", url.Values{"id": {"svc1"}, "name": {"Service 1"}, "delivery_type": {"ws"}})
	var issued map[string]string
	postFormJSON(t, mux, "/admin/api-keys", url.Values{"action": {"issue"}, "service_id": {"svc1"}, "label": {"main"}}, &issued)
	if issued["api_key"] == "" || issued["id"] == "" {
		t.Fatalf("issued = %+v", issued)
	}
	listKeys := getAuth(t, mux, "/admin/api-keys?service_id=svc1")
	if strings.Contains(listKeys.Body.String(), issued["api_key"]) {
		t.Fatalf("api key list leaked plaintext: %s", listKeys.Body.String())
	}
	postForm(t, mux, "/admin/api-keys", url.Values{"action": {"bind"}, "key_id": {issued["id"]}, "chat_entity_id": {"dev:personal:alice"}})
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: issued["id"], SessionName: "home", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"add_wildcard"}, "key_id": {issued["id"]}, "session_name": {"home"}, "match_expr": {"openAPI???"}})
	rec := postFormRaw(mux, "/admin/sessions", url.Values{"action": {"add_wildcard"}, "key_id": {issued["id"]}, "session_name": {"home"}, "match_expr": {"openAPI1"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("overlap route status=%d body=%s", rec.Code, rec.Body.String())
	}
	postForm(t, mux, "/admin/config", url.Values{"key": {"retention"}, "value_json": {`{"hot_days":30}`}})
	cfg, _ := db.ListAppConfig(ctx)
	if len(cfg) != 1 || cfg[0].Key != "retention" {
		t.Fatalf("config = %+v", cfg)
	}
	postForm(t, mux, "/admin/config", url.Values{"action": {"upsert_system_route"}, "keyword": {"#测试汇报"}, "route_action": {"record"}, "service_name": {"scheduler"}, "priority": {"7"}})
	systemRoutes, _ := db.ListSystemRoutes(ctx)
	foundSystemRoute := false
	for _, route := range systemRoutes {
		if route.Keyword == "#测试汇报" && route.Action == "record" && route.Priority == 7 {
			foundSystemRoute = true
		}
	}
	if !foundSystemRoute {
		t.Fatalf("system route not saved: %+v", systemRoutes)
	}
	configPage := getAuth(t, mux, "/admin/config").Body.String()
	if !strings.Contains(configPage, "系统关键词") || !strings.Contains(configPage, "#测试汇报") {
		t.Fatalf("config page missing system route controls: %s", configPage)
	}
	postForm(t, mux, "/admin/config", url.Values{"action": {"delete_system_route"}, "keyword": {"#测试汇报"}})
	systemRoutes, _ = db.ListSystemRoutes(ctx)
	for _, route := range systemRoutes {
		if route.Keyword == "#测试汇报" {
			t.Fatalf("system route not deleted: %+v", systemRoutes)
		}
	}
	routes, _ := db.ListAllPrefixRoutes(ctx)
	if len(routes) != 1 {
		t.Fatalf("route not created: %+v", routes)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"delete_wildcard"}, "key_id": {issued["id"]}, "session_name": {"home"}, "route_id": {routes[0].Rule.ID}})
	if routes, _ := db.ListAllPrefixRoutes(ctx); len(routes) != 0 {
		t.Fatalf("route not deleted: %+v", routes)
	}
	postForm(t, mux, "/admin/config", url.Values{"action": {"delete"}, "key": {"retention"}})
	if cfg, _ := db.ListAppConfig(ctx); len(cfg) != 0 {
		t.Fatalf("config not deleted: %+v", cfg)
	}
	postForm(t, mux, "/admin/bot-channels", url.Values{"action": {"delete"}, "id": {"dev"}})
	if channels, _ := db.ListBotChannels(ctx); findBotChannel(channels, "dev") != nil {
		t.Fatalf("channel not deleted: %+v", channels)
	}
	postForm(t, mux, "/admin/members", url.Values{"action": {"delete"}, "owner_key": {"dev:personal:alice"}})
	if m, _ := db.GetMemberByOwnerKey(ctx, "dev:personal:alice"); m == nil || m.Active {
		t.Fatalf("member not disabled: %+v", m)
	}
}

func TestAdminConfigRejectsHubManagedHelperUpdateKeys(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	keys := []string{m8.HelperUpdateCatalogPrefix + strings.Repeat("a", 64), "helper_update.job.agent-1"}
	for _, key := range keys {
		// Add is forbidden before a value exists.
		for _, form := range []url.Values{
			{"key": {key}, "value_json": {`{"forged":true}`}},
		} {
			rec := postFormRaw(mux, "/admin/config", form)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("key=%q form=%v status=%d body=%s", key, form, rec.Code, rec.Body.String())
			}
		}
		if err := db.UpsertAppConfig(ctx, key, `{"seed":true}`); err != nil {
			t.Fatal(err)
		}
		for _, form := range []url.Values{
			{"key": {key}, "value_json": {`{"forged":true}`}}, // modify
			{"key": {key}, "action": {"delete"}},              // delete
		} {
			rec := postFormRaw(mux, "/admin/config", form)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("key=%q form=%v status=%d body=%s", key, form, rec.Code, rec.Body.String())
			}
		}
		if err := db.DeleteAppConfig(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := db.ListAppConfig(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("Hub-managed config mutated rows=%+v err=%v", rows, err)
	}
}

func TestAdminSeenPersonCandidatePromotesToMember(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSeenPerson(ctx, model.SeenPerson{OpenID: "ou_1", BotChannelID: "dev", Name: "Alice", Source: "inbound", LastSeenAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	page := getAuth(t, mux, "/admin/members").Body.String()
	if !strings.Contains(page, "候选池") || !strings.Contains(page, "ou_1") || !strings.Contains(page, "录为成员") {
		t.Fatalf("members page missing candidate controls: %s", page)
	}
	postForm(t, mux, "/admin/members", url.Values{"action": {"promote_seen"}, "owner_key": {"alice"}, "display_name": {"Alice"}, "feishu_open_id": {"ou_1"}, "role": {"manager"}})
	m, err := db.GetMemberByOwnerKey(ctx, "alice")
	if err != nil || m == nil || m.FeishuOpenID != "ou_1" || m.Role != model.RoleManager {
		t.Fatalf("promoted member=%+v err=%v", m, err)
	}
	page = getAuth(t, mux, "/admin/members").Body.String()
	if !strings.Contains(page, "已是成员") || !strings.Contains(page, "<th>open_id</th>") || !strings.Contains(page, "ou_1") {
		t.Fatalf("member open_id not visible: %s", page)
	}
}

func TestAdminRefreshSeenPersonsUsesCollector(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	srv.Collector = fakeCollector{persons: []model.SeenPerson{{OpenID: "ou_2", BotChannelID: "dev", Name: "Bob", Source: "group", LastSeenAt: time.Now()}}}
	postForm(t, mux, "/admin/members", url.Values{"action": {"refresh_seen"}})
	items, err := db.ListSeenPersons(ctx)
	if err != nil || len(items) != 1 || items[0].OpenID != "ou_2" || items[0].Source != "group" {
		t.Fatalf("seen persons=%+v err=%v", items, err)
	}
}

type fakeCollector struct {
	persons []model.SeenPerson
}

func (f fakeCollector) CollectSeenPersons(context.Context) ([]model.SeenPerson, error) {
	return f.persons, nil
}

func TestAdminM9PagesExposeCRUDForms(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "home", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/members", "/admin/bot-channels", "/admin/services", "/admin/api-keys", "/admin/config", "/admin/sessions"} {
		rec := getAuth(t, mux, path)
		body := rec.Body.String()
		if !strings.Contains(body, "<form") || !strings.Contains(body, "<button") || !strings.Contains(body, "<input") {
			t.Fatalf("%s should expose CRUD controls, body=%s", path, body)
		}
	}
}

func TestAdminSessionMirrorPersists(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "UnifiedRobot", AppID: "cli_x", Purpose: "general", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-test", "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "dev:personal:ou_alice", DisplayName: "UserOne", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "home", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	page := getAuth(t, mux, "/admin/sessions").Body.String()
	if !strings.Contains(page, "机器人账号") || strings.Contains(page, "ou_alice#FB-test#UnifiedRobot") {
		t.Fatalf("session page should hide raw mirror address before enabled: %s", page)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"mirror_on"}, "key_id": {"FB-test"}, "session_name": {"home"}, "bot_name": {"UnifiedRobot"}})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != "ou_alice#FB-test#UnifiedRobot" {
		t.Fatalf("mirror on endpoints=%+v err=%v", endpoints, err)
	}
	page = getAuth(t, mux, "/admin/sessions").Body.String()
	if !strings.Contains(page, "UserOne via UnifiedRobot") || strings.Contains(page, "ou_alice#FB-test#UnifiedRobot") {
		t.Fatalf("session page should show friendly mirror target: %s", page)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"mirror_off"}, "key_id": {"FB-test"}, "session_name": {"home"}})
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != "" {
		t.Fatalf("mirror off endpoints=%+v err=%v", endpoints, err)
	}
}

func TestAdminSessionsShowsMemberDirectory(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.BindAPIKeyAccount(ctx, "FB-test", "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "home", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	page := getAuth(t, mux, "/admin/sessions").Body.String()
	if !strings.Contains(page, "成员会话目录") || !strings.Contains(page, "@Alice#home") {
		t.Fatalf("member session directory missing: %s", page)
	}
}

func TestAdminSessionNoDirectoryPersists(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "sec-ops", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	page := getAuth(t, mux, "/admin/sessions").Body.String()
	if !strings.Contains(page, "不进清单不可派发") || !strings.Contains(page, "生效:false 后台:false") {
		t.Fatalf("session isolation controls missing: %s", page)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"no_directory_on"}, "key_id": {"FB-test"}, "session_name": {"sec-ops"}})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].NoDirectory || !endpoints[0].NoDirectoryAdmin {
		t.Fatalf("no_directory on endpoints=%+v err=%v", endpoints, err)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"no_directory_off"}, "key_id": {"FB-test"}, "session_name": {"sec-ops"}})
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || endpoints[0].NoDirectory || endpoints[0].NoDirectoryAdmin {
		t.Fatalf("no_directory off endpoints=%+v err=%v", endpoints, err)
	}
}

func TestAdminSessionMirrorAdvancedAddressStillWorks(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "home", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	postForm(t, mux, "/admin/sessions", url.Values{"action": {"mirror_on"}, "key_id": {"FB-test"}, "session_name": {"home"}, "mirror_to": {"oc_group#FB-test#UnifiedRobot"}})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || endpoints[0].MirrorTo != "oc_group#FB-test#UnifiedRobot" {
		t.Fatalf("advanced mirror endpoints=%+v err=%v", endpoints, err)
	}
}

func TestAdminProjectsCRUDAndMemberAssignment(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:group:oc_team", BotChannelID: "dev", Type: model.ChatGroup, FeishuID: "oc_team", DisplayName: "项目群", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "bob", DisplayName: "Bob", FeishuOpenID: "ou_bob", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSeenPersonGroup(ctx, model.SeenPersonGroup{OpenID: "ou_alice", BotChannelID: "dev", GroupChatID: "oc_team", GroupName: "项目群", LastSeenAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	missingOwner := postFormRaw(mux, "/admin/projects", url.Values{"id": {"proj:bad"}, "name": {"缺负责人"}})
	if missingOwner.Code != http.StatusBadRequest || !strings.Contains(missingOwner.Body.String(), "project owner_key required") {
		t.Fatalf("missing owner response=%d %s", missingOwner.Code, missingOwner.Body.String())
	}
	postForm(t, mux, "/admin/projects", url.Values{"id": {"proj:test"}, "name": {"测试项目"}, "parent_id": {"proj:default"}, "owner_key": {"alice"}, "product_manager_key": {"bob"}, "notify_chat_id": {"oc_team"}, "notify_bot_id": {"UnifiedRobot"}, "active": {"1"}})
	p, err := db.GetProject(ctx, "proj:test")
	if err != nil || p == nil || p.NotifyChatID != "oc_team" || p.ParentID != "proj:default" || p.Name != "测试项目" || p.OwnerKey != "alice" || p.ProductManagerKey != "bob" {
		t.Fatalf("project=%+v err=%v", p, err)
	}
	page := getAuth(t, mux, "/admin/projects").Body.String()
	for _, want := range []string{"项目组管理", "测试项目", "项目群", "parent", "负责人", "产品经理", "Alice (alice)", "Bob (bob)", "生效通知群", "成员数"} {
		if !strings.Contains(page, want) {
			t.Fatalf("projects page missing %q: %s", want, page)
		}
	}
	membersPage := getAuth(t, mux, "/admin/projects/proj:test/members").Body.String()
	if !strings.Contains(membersPage, "候选池") || !strings.Contains(membersPage, "ou_alice") || !strings.Contains(membersPage, "加入") {
		t.Fatalf("members page missing candidate: %s", membersPage)
	}
	postForm(t, mux, "/admin/projects/proj:test/members", url.Values{"action": {"add"}, "owner_key": {"alice"}})
	members, err := db.ListProjectMembers(ctx, "proj:test")
	if err != nil || len(members) != 1 || members[0].OwnerKey != "alice" {
		t.Fatalf("project members=%+v err=%v", members, err)
	}
	postForm(t, mux, "/admin/projects/proj:test/members", url.Values{"action": {"remove"}, "owner_key": {"alice"}})
	members, err = db.ListProjectMembers(ctx, "proj:test")
	if err != nil || len(members) != 0 {
		t.Fatalf("project member not removed: %+v err=%v", members, err)
	}
	postForm(t, mux, "/admin/projects", url.Values{"action": {"disable"}, "id": {"proj:test"}})
	p, _ = db.GetProject(ctx, "proj:test")
	if p == nil || p.Active {
		t.Fatalf("project not disabled: %+v", p)
	}
}

func TestAdminBotChannelPageCanCreateAndEditBotChannel(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	channels, err := db.ListBotChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Fatalf("bot channels should not be seeded: %+v", channels)
	}
	page := getAuth(t, mux, "/admin/bot-channels").Body.String()
	for _, want := range []string{"app_secret", "保存", "WS状态"} {
		if !strings.Contains(page, want) {
			t.Fatalf("bot channel page missing %q: %s", want, page)
		}
	}
	postForm(t, mux, "/admin/bot-channels", url.Values{"id": {"bot-test"}, "name": {"bot-test"}, "app_id": {"cli_testbot000000000"}, "purpose": {"general"}})
	channels, err = db.ListBotChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cc := findBotChannel(channels, "bot-test")
	if cc == nil || cc.Purpose != "general" || !cc.CanSend || !cc.CanReceive {
		t.Fatalf("bot-test edit failed: %+v", cc)
	}
}

func TestAdminAggregateSourcesPageSavesSources(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	for _, p := range []model.Project{
		{ID: "proj:agg", Name: "聚合项目", ParentID: "proj:default", NotifyBotID: "bot-test", EvidenceCron: "0 2 * * 1,3", Active: true},
		{ID: "proj:a", Name: "项目A", ParentID: "proj:default", Active: true},
		{ID: "proj:b", Name: "项目B", ParentID: "proj:default", Active: true},
	} {
		if err := db.UpsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	page := getAuth(t, mux, "/admin/projects/proj:agg/aggregate-sources").Body.String()
	for _, want := range []string{"聚合来源", "项目A", "项目B", "选择同parent子项目"} {
		if !strings.Contains(page, want) {
			t.Fatalf("aggregate source page missing %q: %s", want, page)
		}
	}
	postForm(t, mux, "/admin/projects/proj:agg/aggregate-sources", url.Values{"action": {"save"}, "source_project_id": {"proj:a", "proj:b"}})
	sources, err := db.ListProjectAggregateSources(ctx, "proj:agg")
	if err != nil || len(sources) != 2 {
		t.Fatalf("aggregate sources=%+v err=%v", sources, err)
	}
	postForm(t, mux, "/admin/projects/proj:agg/aggregate-sources", url.Values{"action": {"select_children"}})
	sources, err = db.ListProjectAggregateSources(ctx, "proj:agg")
	if err != nil || len(sources) == 0 {
		t.Fatalf("aggregate child sources=%+v err=%v", sources, err)
	}
}

func TestAdminSchedulePasteRejectsInvalidAndAppendsWithRollback(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:test", Name: "测试项目", NotifyBotID: "UnifiedRobot", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignProjectMember(ctx, "proj:test", "alice"); err != nil {
		t.Fatal(err)
	}
	prev, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:test", Kind: "team", Content: "# 团队排期\n\n这里是足够长的初始团队排期内容。", Source: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	bad := postFormRaw(mux, "/admin/projects/proj:test/team-schedule", url.Values{"content": {"短"}, "confirm": {"1"}})
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "校验失败") {
		t.Fatalf("bad paste status=%d body=%s", bad.Code, bad.Body.String())
	}
	next := "# 团队排期\n\n这里是足够长的更新后团队排期内容，包含明确任务。"
	preview := postFormRaw(mux, "/admin/projects/proj:test/team-schedule", url.Values{"content": {next}})
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "预览 diff") || !strings.Contains(preview.Body.String(), "+") {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if latest, _ := db.LatestScheduleDoc(ctx, "proj:test", "team", ""); latest.Version != prev.Version {
		t.Fatalf("preview should not append: %+v", latest)
	}
	postForm(t, mux, "/admin/projects/proj:test/team-schedule", url.Values{"content": {next}, "confirm": {"1"}})
	latest, err := db.LatestScheduleDoc(ctx, "proj:test", "team", "")
	if err != nil || latest.Version != prev.Version+1 || latest.Content != next {
		t.Fatalf("team latest=%+v err=%v", latest, err)
	}
	history := getAuth(t, mux, "/admin/projects/proj:test/history").Body.String()
	if !strings.Contains(history, "排期版本历史") || !strings.Contains(history, "回滚为新版") {
		t.Fatalf("history missing controls: %s", history)
	}
	postForm(t, mux, "/admin/projects/proj:test/history", url.Values{"version": {strconv.Itoa(prev.Version)}})
	latest, _ = db.LatestScheduleDoc(ctx, "proj:test", "team", "")
	if latest.Version != prev.Version+2 || latest.Content != prev.Content || latest.Source != "rollback" {
		t.Fatalf("rollback latest=%+v prev=%+v", latest, prev)
	}

	personal := "# Alice 个人日程\n\n这里是足够长的个人日程整贴内容，包含任务安排。"
	postForm(t, mux, "/admin/projects/proj:test/members/alice/schedule", url.Values{"content": {personal}, "confirm": {"1"}})
	pdoc, err := db.LatestScheduleDoc(ctx, "proj:test", "personal", "alice")
	if err != nil || pdoc == nil || pdoc.Version != 1 || pdoc.Content != personal {
		t.Fatalf("personal doc=%+v err=%v", pdoc, err)
	}
	postForm(t, mux, "/admin/projects/proj:test/members/alice/schedule/history", url.Values{"version": {"1"}})
	pdoc, _ = db.LatestScheduleDoc(ctx, "proj:test", "personal", "alice")
	if pdoc.Version != 2 || pdoc.Source != "rollback" {
		t.Fatalf("personal rollback=%+v", pdoc)
	}
}

func TestAdminSessionsShowsToolModelAndFullSessionName(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "developer", FullSessionName: "sh-developer-e0d12642", Tool: "CODEX", Model: "gpt-5.5", Producer: true, TargetGroup: "oc_ai", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	page := getAuth(t, mux, "/admin/sessions").Body.String()
	for _, want := range []string{"<th>工具</th>", "<th>大模型</th>", "<th>完整会话名</th>", "<th>producer</th>", "<th>target_group</th>", "CODEX", "gpt-5.5", "sh-developer-e0d12642", "oc_ai"} {
		if !strings.Contains(page, want) {
			t.Fatalf("sessions page missing %q: %s", want, page)
		}
	}
}

func TestAdminDelegateScheduleCreatesPendingAndNotice(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	owner := "dev:personal:alice"
	postForm(t, mux, "/admin/delegate/schedule", url.Values{"owner_key": {owner}, "start_date": {"2026-07-20"}, "end_date": {"2026-07-21"}, "task": {"代改任务"}})
	pending, err := db.GetPending(ctx, owner)
	if err != nil || pending == nil || !strings.Contains(pending.PayloadJSON, "代改任务") {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || !strings.Contains(msg.Content, "确认") {
		t.Fatalf("delegate notice msg=%+v err=%v", msg, err)
	}
}

func TestAdminCleanupRequiresOneMonthCutoff(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	srv.sessions["tok"] = "admin"
	recent := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rec := postFormRaw(mux, "/admin/cleanup", url.Values{"cutoff": {recent}, "confirm": {"1"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("recent cleanup status=%d body=%s", rec.Code, rec.Body.String())
	}
	old := time.Now().AddDate(0, -2, 0).Format("2006-01-02")
	rec = postFormRaw(mux, "/admin/cleanup", url.Values{"cutoff": {old}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Messages") {
		t.Fatalf("dry-run cleanup status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminProvisionPageRequiresAuthAndShowsOnlineSessions(t *testing.T) {
	srv, db, mux, ctx := newAdminTestServer(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{
		KeyID:       "FB-test",
		SessionName: "developer",
		OwnerKey:    "alice",
		LastSeenAt:  time.Now().UTC(),
		Active:      true,
		Tool:        "codex",
		Model:       "gpt-5",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteAudit(ctx, "developer#FB-test", "provision_ack", `{"action":"install_skill","ok":true,"message":"skill installed"}`); err != nil {
		t.Fatal(err)
	}
	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/admin/provision", nil))
	if unauth.Code != http.StatusSeeOther || unauth.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unauth provision code=%d location=%q", unauth.Code, unauth.Header().Get("Location"))
	}
	srv.sessions["tok"] = "admin"
	body := getAuth(t, mux, "/admin/provision").Body.String()
	for _, want := range []string{"Provision 下发", "developer", "alice", "install_skill", "打包当前 sessionHelper", "最近 Provision 回执", "skill installed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("provision page missing %q in %s", want, body)
		}
	}
}

func TestAdminProvisionTargetLimitAndVersionMonotonic(t *testing.T) {
	srv, db, _, ctx := newAdminTestServer(t)
	for _, name := range []string{"dev1", "dev2"} {
		if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: name, OwnerKey: "alice", LastSeenAt: time.Now().UTC(), Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	targets, err := srv.resolveProvisionTargets(ctx, "owner", "", "", "alice", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].OwnerKey != "alice" {
		t.Fatalf("targets=%+v", targets)
	}
	srv.recordProvisionVersion(ctx, "install_skill", "FB-test", "dev1", "demo", "2.2.0")
	if err := srv.ensureProvisionVersionMonotonic(ctx, "install_skill", "FB-test", "dev1", "demo", "2.1.9"); err == nil {
		t.Fatal("downgrade should be denied")
	}
	if err := srv.ensureProvisionVersionMonotonic(ctx, "install_skill", "FB-test", "dev1", "demo", "2.2"); err != nil {
		t.Fatalf("equivalent shorter version should pass: %v", err)
	}
	if err := srv.ensureProvisionVersionMonotonic(ctx, "install_skill", "FB-test", "dev1", "demo", "2.2.1"); err != nil {
		t.Fatalf("upgrade should pass: %v", err)
	}
}

// F2: update_self 的单调闸以 helper 在线上报版本(session_endpoint.version)为准，
// 不依赖 app_config 记账——这样失败下发不会烧号，成功升级重连即刷新。
func TestAdminProvisionUpdateSelfGatesOnLiveSessionVersion(t *testing.T) {
	srv, db, _, ctx := newAdminTestServer(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{
		KeyID: "FB-test", SessionName: "dev1", OwnerKey: "alice",
		LastSeenAt: time.Now().UTC(), Active: true, Version: "0.1.2.6",
	}); err != nil {
		t.Fatal(err)
	}
	// 比在线版本低 → 拒
	if err := srv.ensureProvisionVersionMonotonic(ctx, "update_self", "FB-test", "dev1", "self", "0.1.2.5"); err == nil {
		t.Fatal("低于在线版本的 update_self 应被拒")
	}
	// 高于在线版本 → 放行
	if err := srv.ensureProvisionVersionMonotonic(ctx, "update_self", "FB-test", "dev1", "self", "0.1.2.7"); err != nil {
		t.Fatalf("高于在线版本应放行: %v", err)
	}
	// 一条陈旧的 app_config 记录不得影响 update_self（它只看在线版本）——
	// 复现 V1 的 bug：一次失败下发若曾写过高版本记录，也不能把合法下发挡住。
	srv.recordProvisionVersion(ctx, "update_self", "FB-test", "dev1", "self", "9.9.9")
	if err := srv.ensureProvisionVersionMonotonic(ctx, "update_self", "FB-test", "dev1", "self", "0.1.2.7"); err != nil {
		t.Fatalf("陈旧 app_config 记录不应影响 update_self 闸: %v", err)
	}
}

func TestAdminProvisionValidatesURLHost(t *testing.T) {
	t.Setenv("WP_PROVISION_ALLOWED_HOSTS", "")
	t.Setenv("SH_PROVISION_ALLOWED_HOSTS", "")
	defaultHosts := provisionAllowedHosts()
	if !defaultHosts["dingwei.wegoab.com"] {
		t.Fatal("default provision hosts must include dingwei.wegoab.com")
	}
	if defaultHosts["ts.wegoab.com"] {
		t.Fatal("default provision hosts must not include legacy ts.wegoab.com")
	}
	if err := validateProvisionURL("https://dingwei.wegoab.com/a.tar", defaultHosts); err != nil {
		t.Fatalf("default dingwei host should pass: %v", err)
	}
	if err := validateProvisionURL("https://ts.wegoab.com/a.tar", defaultHosts); err == nil {
		t.Fatal("legacy ts host should not be allowed by default")
	}
	t.Setenv("WP_PROVISION_ALLOWED_HOSTS", "dingwei.wegoab.com,localhost")
	if err := validateProvisionURL("https://dingwei.wegoab.com/a.tar", provisionAllowedHosts()); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisionURL("https://evil.example/a.tar", provisionAllowedHosts()); err == nil {
		t.Fatal("evil host should be denied")
	}
	if !validSHA256(strings.Repeat("a", 64)) || validSHA256(strings.Repeat("z", 64)) {
		t.Fatal("sha256 validation mismatch")
	}
}

func TestAdminProvisionPackageSessionHelperCreatesDownloadArtifact(t *testing.T) {
	srv, _, _, _ := newAdminTestServer(t)
	dl := t.TempDir()
	t.Setenv("WP_PROVISION_DL_DIR", dl)
	secretConfig := filepath.Join("..", "..", "tools", "sessionhelper", "config-secret.json")
	if err := os.WriteFile(secretConfig, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secretConfig) })
	req := httptest.NewRequest(http.MethodPost, "http://localhost/admin/provision", nil)
	info, err := srv.packageSessionHelper(req, "9.9.9-test")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != info.SHA256 {
		t.Fatalf("sha=%s want %s", got, info.SHA256)
	}
	if !strings.Contains(info.URL, "/dl/sessionhelper-9.9.9-test.tar.gz") || info.Version != "9.9.9-test" {
		t.Fatalf("info=%+v", info)
	}
	names := tarNames(t, data)
	if !names["sessionhelper/config.py"] {
		t.Fatalf("package missing sessionhelper/config.py names=%v", names)
	}
	if !names["config.example"] {
		t.Fatalf("package missing config.example names=%v", names)
	}
	for name := range names {
		if strings.HasPrefix(filepath.Base(name), "config-") && strings.HasSuffix(name, ".json") {
			t.Fatalf("package leaked config json: %s", name)
		}
	}
}

func tarNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return out
			}
			t.Fatal(err)
		}
		out[hdr.Name] = true
	}
}

func postForm(t *testing.T, mux *http.ServeMux, path string, form url.Values) {
	t.Helper()
	rec := postFormRaw(mux, path, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
}

func postFormJSON(t *testing.T, mux *http.ServeMux, path string, form url.Values, out any) {
	t.Helper()
	rec := postFormRaw(mux, path, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("unmarshal %s: %v body=%s", path, err, rec.Body.String())
	}
}

func postFormRaw(mux *http.ServeMux, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func getAuth(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	return rec
}

func findBotChannel(channels []model.BotChannel, id string) *model.BotChannel {
	for i := range channels {
		if channels[i].ID == id {
			return &channels[i]
		}
	}
	return nil
}

func TestPortalDatadBoardRendersTasksAndSessions(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/tasks":
			_, _ = w.Write([]byte(`[{"id":"G4-1","title":"联调","owner_session":"zzc-devo48-2642","status":"in_progress","anchor":"7b8df4c","updated_at":"2026-08-05T02:00:00Z"}]`))
		case "/v1/sessions":
			_, _ = w.Write([]byte(`[{"session":"zzc-devo48-2642","state":"active","watermark_pct":42,"current_task":"G4-1","updated_at":"2026-08-05T02:30:00Z"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	srv.Datad = datad.NewClient(fake.URL, "read-tok")

	srv.sessions["tok"] = "admin"
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"任务看板(datad)", "待办", "G4-1", "zzc-devo48-2642", "in_progress", "会话状态(datad)", "active", "2026-08-05T02:00:00Z"} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal datad board missing %q in %s", want, body)
		}
	}
}

func TestPortalDatadUnreachableShowsBannerNotFallback(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	fake.Close() // 直接关掉=连接层不可达
	srv.Datad = datad.NewClient(fake.URL, "read-tok")

	srv.sessions["tok"] = "admin"
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "datad 不可达") {
		t.Fatalf("expected unreachable banner, body=%s", body)
	}
	if strings.Contains(body, "任务台账(datad)") {
		t.Fatalf("must not render datad tables on failure")
	}
	if rec.Code != http.StatusOK || !strings.Contains(body, "团队排期") {
		t.Fatalf("page must still render local sections, code=%d", rec.Code)
	}
}

func TestDatadProxyEndpoints(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/tasks":
			_, _ = w.Write([]byte(`[{"id":"G4-1","title":"联调","owner_session":"zzc-devo48-2642","status":"in_progress","updated_at":"2026-08-05T02:00:00Z"}]`))
		case "/v1/sessions":
			_, _ = w.Write([]byte(`[{"session":"zzc-devo48-2642","state":"active","updated_at":"2026-08-05T02:30:00Z"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)

	// 未登录:代理端点必须挡在登录门外(token 不可被无凭据外泄)。
	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/admin/datad/tasks", nil))
	if unauth.Code != http.StatusSeeOther {
		t.Fatalf("unauth proxy code=%d", unauth.Code)
	}

	// 未配置 datad:503 明示。
	srv.sessions["tok"] = "admin"
	authedGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := authedGet("/admin/datad/tasks"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured proxy code=%d body=%s", rec.Code, rec.Body.String())
	}

	srv.Datad = datad.NewClient(fake.URL, "read-tok")
	rec := authedGet("/admin/datad/tasks")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"G4-1"`) || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("tasks proxy code=%d ct=%s body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "read-tok") {
		t.Fatalf("token must not leak into proxy response")
	}
	rec = authedGet("/admin/datad/sessions")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"active"`) {
		t.Fatalf("sessions proxy code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWriteDatadTaskBoardLayout(t *testing.T) {
	mk := func(id, status string, ageHours int) datad.Task {
		return datad.Task{ID: id, Title: id + "标题", OwnerSession: "zzc-devo48-2642",
			Status: status, UpdatedAt: time.Now().Add(-time.Duration(ageHours) * time.Hour)}
	}
	var tasks []datad.Task
	tasks = append(tasks, mk("TODO-1", "in_progress", 1), mk("TODO-2", "proposed", 2))
	tasks = append(tasks, mk("ARCH-1", "archived", 1)) // 软删不展示
	for i := 0; i < 14; i++ {                          // 14 件 done,超 cap 10
		tasks = append(tasks, mk(fmt.Sprintf("DONE-%02d", i), "done", i)) // i 小=新
	}
	var buf strings.Builder
	writeDatadTaskBoard(&buf, tasks)
	html := buf.String()

	// ① 双列自适应网格(auto-fill grid)。
	if !strings.Contains(html, "grid-template-columns:repeat(auto-fill,minmax(260px,1fr))") {
		t.Fatal("双列自适应网格样式缺失")
	}
	// ② 待办段在已完成段之前(todo 在上)。
	todoIdx := strings.Index(html, "待办")
	doneIdx := strings.Index(html, "已完成")
	if todoIdx < 0 || doneIdx < 0 || todoIdx > doneIdx {
		t.Fatalf("待办须在已完成之前: todo=%d done=%d", todoIdx, doneIdx)
	}
	// ③ 已完成最多 10 条(按更新时间新→旧):DONE-00..09(最新 10)在,DONE-10..13 截。
	if !strings.Contains(html, "显示最近 10 / 共 14") {
		t.Fatal("超 cap 应标注总数")
	}
	for _, keep := range []string{"DONE-00", "DONE-09"} {
		if !strings.Contains(html, keep) {
			t.Fatalf("已完成前 10(新→旧)应含 %s", keep)
		}
	}
	for _, cut := range []string{"DONE-10", "DONE-13"} {
		if strings.Contains(html, cut) {
			t.Fatalf("超 cap 的 %s 应被截断", cut)
		}
	}
	// archived 不展示。
	if strings.Contains(html, "ARCH-1") {
		t.Fatal("archived 软删不应展示")
	}
	// 待办两件都在。
	if !strings.Contains(html, "TODO-1") || !strings.Contains(html, "TODO-2") {
		t.Fatal("待办件缺失")
	}
	// esc 保持(标题经 esc)。
	if !strings.Contains(html, "TODO-1标题") {
		t.Fatal("标题渲染缺失")
	}
}

// 新平台不注册旧 admin 编组页，也不给入口（qwen 复验 G3 托管＋PG 层 🟡第七节第 3 条，组长 15:22:39Z ①）：
// 它读写旧 agent_group* 表，而这三张表在新平台不用、不 PG 化。旧平台照旧（对照格）。
func TestAdminLegacyGroupsPageNotMountedOnNewPlatform(t *testing.T) {
	for _, newPlatform := range []bool{false, true} {
		db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "admin.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		srv := New(db)
		srv.NewPlatform = newPlatform
		srv.sessions["tok"] = "admin"
		mux := http.NewServeMux()
		srv.Mount(mux)
		pageReq := httptest.NewRequest(http.MethodGet, "/admin/agent-groups", nil)
		pageReq.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
		page := httptest.NewRecorder()
		mux.ServeHTTP(page, pageReq) // 不用 getAuth：它断言 200，而新平台这里应是 404
		post := postFormRaw(mux, "/admin/agent-groups", url.Values{"action": {"upsert_group"}})
		status := getAuth(t, mux, "/admin")
		hasLink := strings.Contains(status.Body.String(), `href="/admin/agent-groups"`)
		if newPlatform {
			if page.Code != http.StatusNotFound || post.Code != http.StatusNotFound || hasLink {
				t.Errorf("new platform: GET %d POST %d link=%v, want 404 404 false", page.Code, post.Code, hasLink)
			}
		} else if page.Code != http.StatusOK || !hasLink {
			t.Errorf("legacy: GET %d link=%v, want 200 true", page.Code, hasLink)
		}
		_ = db.Close()
	}
}

func TestAdminLegacyIdentityPagesHiddenAndUnmountedOnNewPlatform(t *testing.T) {
	getPaths := []string{"/admin/members", "/admin/api-keys", "/admin/sessions", "/admin/provision"}
	postPaths := []string{"/admin/members", "/admin/api-keys", "/admin/sessions", "/admin/provision", "/admin/cleanup"}
	// 现行旧门实际共 9 条：前四页各 GET+POST，加 cleanup 的 POST；历史上没有 GET /admin/cleanup。
	if len(getPaths)+len(postPaths) != 9 {
		t.Fatal("legacy admin route fixture must enumerate all nine registered routes")
	}
	for _, newPlatform := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "new-platform"}[newPlatform], func(t *testing.T) {
			db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "admin.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			srv := New(db)
			srv.NewPlatform = newPlatform
			srv.sessions["tok"] = "admin"
			mux := http.NewServeMux()
			srv.Mount(mux)

			status := getAuth(t, mux, "/admin").Body.String()
			for _, path := range []string{"/portal", "/admin/messages", "/admin/bot-channels", "/admin/projects", "/admin/services", "/admin/control-plane", "/admin/files", "/admin/config"} {
				if !strings.Contains(status, `href="`+path+`"`) {
					t.Errorf("navigation removed unrelated entry %s in newPlatform=%v", path, newPlatform)
				}
			}
			for _, path := range getPaths {
				hasLink := strings.Contains(status, `href="`+path+`"`)
				if hasLink == newPlatform {
					t.Errorf("navigation %s present=%v in newPlatform=%v", path, hasLink, newPlatform)
				}
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if newPlatform && rec.Code != http.StatusNotFound {
					t.Errorf("new platform GET %s=%d, want 404", path, rec.Code)
				}
				if !newPlatform && rec.Code != http.StatusOK {
					t.Errorf("legacy GET %s=%d, want 200", path, rec.Code)
				}
			}
			for _, path := range postPaths {
				rec := postFormRaw(mux, path, url.Values{})
				if newPlatform && rec.Code != http.StatusNotFound {
					t.Errorf("new platform POST %s=%d, want 404", path, rec.Code)
				}
				if !newPlatform && rec.Code == http.StatusNotFound {
					t.Errorf("legacy POST %s unexpectedly unmounted", path)
				}
			}
		})
	}
}
