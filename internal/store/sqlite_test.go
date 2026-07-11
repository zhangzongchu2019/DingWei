package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func newTestSQLite(t *testing.T) (*SQLite, context.Context) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "workpulse-test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db, ctx
}

func TestMigrateUpgradesExistingDBWithSeenPersonAndBotSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workpulse-old.db")
	ctx := context.Background()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite first: %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TABLE bot_channel (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  app_id      TEXT NOT NULL,
  purpose     TEXT NOT NULL DEFAULT 'general',
  can_send    INTEGER NOT NULL DEFAULT 1,
  can_receive INTEGER NOT NULL DEFAULT 1,
  active      INTEGER NOT NULL DEFAULT 1
)`); err != nil {
		t.Fatalf("seed old bot_channel: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate old db: %v", err)
	}
	if !columnExists(t, db, "bot_channel", "app_secret_enc") || !columnExists(t, db, "bot_channel", "verification_token") || !columnExists(t, db, "bot_channel", "encrypt_key") {
		t.Fatal("bot_channel webhook security columns missing after migration")
	}
	if !tableExists(t, db, "seen_person") {
		t.Fatal("seen_person table missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "client_ip") {
		t.Fatal("session_endpoint.client_ip missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "full_session_name") {
		t.Fatal("session_endpoint.full_session_name missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "owner_key") {
		t.Fatal("session_endpoint.owner_key missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "producer") || !columnExists(t, db, "session_endpoint", "target_group") {
		t.Fatal("session_endpoint producer metadata missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "no_directory") {
		t.Fatal("session_endpoint.no_directory missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "no_directory_admin") || !columnExists(t, db, "session_endpoint", "no_directory_reported") {
		t.Fatal("session_endpoint no_directory split columns missing after migration")
	}
	if !columnExists(t, db, "session_endpoint", "conn_seq") {
		t.Fatal("session_endpoint.conn_seq missing after migration")
	}
	if !columnExists(t, db, "project", "parent_id") {
		t.Fatal("project.parent_id missing after migration")
	}
	if !columnExists(t, db, "project", "owner_key") || !columnExists(t, db, "project", "product_manager_key") {
		t.Fatal("project owner fields missing after migration")
	}
	if !tableExists(t, db, "project_weekly_report") {
		t.Fatal("project_weekly_report table missing after migration")
	}
	projects, err := db.ListProjects(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != "proj:default" || projects[0].OwnerKey != "" {
		t.Fatalf("unexpected project bootstrap projects=%+v", projects)
	}
	channels, err := db.ListBotChannels(ctx)
	if err != nil {
		t.Fatalf("ListBotChannels: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("business bot channels should not be seeded: %+v", channels)
	}
	aggregateIDs, err := db.ListAggregateProjectIDs(ctx)
	if err != nil || len(aggregateIDs) != 0 {
		t.Fatalf("aggregate projects should not be seeded ids=%+v err=%v", aggregateIDs, err)
	}
	member, err := db.GetMemberByOwnerKey(ctx, "systemtaskintl")
	if err != nil || member == nil || member.Role != model.RoleSystem || member.FeishuOpenID != "" {
		t.Fatalf("system producer member=%+v err=%v", member, err)
	}
	keys, err := db.ListServiceAPIKeys(ctx, "systemtaskintl")
	if err != nil || len(keys) == 0 {
		t.Fatalf("system producer keys=%+v err=%v", keys, err)
	}
	accounts, err := db.ListAPIKeyAccounts(ctx, keys[0].ID)
	if err != nil || len(accounts) != 1 || accounts[0] != "systemtaskintl" {
		t.Fatalf("system producer key accounts=%+v err=%v", accounts, err)
	}
	routes, err := db.ListSystemRoutes(ctx)
	if err != nil {
		t.Fatalf("ListSystemRoutes: %v", err)
	}
	if !hasSystemRoute(routes, "#系统安全", "sec-ops") {
		t.Fatalf("sec-ops system route missing: %+v", routes)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate old db second time: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite second: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate reopened: %v", err)
	}
	if !columnExists(t, reopened, "bot_channel", "app_secret_enc") || !tableExists(t, reopened, "seen_person") {
		t.Fatal("schema missing after reopen migration")
	}
}

func TestSessionEndpointConnSeqAndTouch(t *testing.T) {
	db, ctx := newTestSQLite(t)
	initial := time.Now().UTC().Add(-time.Minute)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "developer", LastSeenAt: initial, Active: true}); err != nil {
		t.Fatal(err)
	}
	seq, err := db.IncrementSessionEndpointConnSeq(ctx, "FB-test", "developer")
	if err != nil || seq != 1 {
		t.Fatalf("first conn seq=%d err=%v", seq, err)
	}
	seq, err = db.IncrementSessionEndpointConnSeq(ctx, "FB-test", "developer")
	if err != nil || seq != 2 {
		t.Fatalf("second conn seq=%d err=%v", seq, err)
	}
	touched := time.Now().UTC()
	if err := db.TouchSessionEndpoint(ctx, "FB-test", "developer", touched); err != nil {
		t.Fatal(err)
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if endpoints[0].ConnSeq != 2 {
		t.Fatalf("conn_seq=%d want 2", endpoints[0].ConnSeq)
	}
	if endpoints[0].LastSeenAt.Sub(touched).Abs() > time.Second {
		t.Fatalf("last_seen=%s want %s", endpoints[0].LastSeenAt, touched)
	}
}

func TestUpsertSessionEndpointPreservesMirrorToWhenReconnectOmitsIt(t *testing.T) {
	db, ctx := newTestSQLite(t)
	mirrorTo := "ou_X#FB-Y#is3-Connector"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{
		KeyID:         "FB-Y",
		SessionName:   "ccbot-sh",
		LastSeenAt:    time.Now(),
		Active:        true,
		MirrorEnabled: true,
		MirrorTo:      mirrorTo,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{
		KeyID:       "FB-Y",
		SessionName: "ccbot-sh",
		LastSeenAt:  time.Now(),
		Active:      true,
	}); err != nil {
		t.Fatal(err)
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != mirrorTo {
		t.Fatalf("mirror target should survive empty reconnect: %+v want=%q", endpoints[0], mirrorTo)
	}
}

func TestSessionNoDirectoryCombinesReportedAndAdmin(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "sec-ops", LastSeenAt: time.Now(), Active: true, NoDirectory: true}); err != nil {
		t.Fatal(err)
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].NoDirectory || endpoints[0].NoDirectoryAdmin {
		t.Fatalf("reported no_directory not effective endpoints=%+v err=%v", endpoints, err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "sec-ops", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || endpoints[0].NoDirectory {
		t.Fatalf("reported no_directory should clear on reconnect without flag endpoints=%+v err=%v", endpoints, err)
	}
	if err := db.SetSessionNoDirectory(ctx, "FB-test", "sec-ops", true); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-test", SessionName: "sec-ops", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].NoDirectory || !endpoints[0].NoDirectoryAdmin {
		t.Fatalf("admin no_directory should persist across reconnect endpoints=%+v err=%v", endpoints, err)
	}
	if err := db.SetSessionNoDirectory(ctx, "FB-test", "sec-ops", false); err != nil {
		t.Fatal(err)
	}
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || endpoints[0].NoDirectory || endpoints[0].NoDirectoryAdmin {
		t.Fatalf("admin no_directory off should restore directory eligibility endpoints=%+v err=%v", endpoints, err)
	}
}

func TestDefaultPersonalImportDisabled(t *testing.T) {
	dir := t.TempDir()
	personalDir := filepath.Join(dir, "plans")
	if err := os.MkdirAll(personalDir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WP_SCHEDULE_PERSONAL_DIR", personalDir)
	t.Setenv("WP_SCHEDULE_TEAM_FILE", filepath.Join(dir, "missing-team.md"))
	if err := os.WriteFile(filepath.Join(personalDir, "工作计划-u1.md"), []byte("# u1\n\nowner key 文件"), 0o640); err != nil {
		t.Fatal(err)
	}
	db, ctx := newTestSQLite(t)
	doc, err := db.LatestScheduleDoc(ctx, "proj:default", "personal", "u1")
	if err != nil {
		t.Fatalf("LatestScheduleDoc: %v", err)
	}
	if doc != nil {
		t.Fatalf("personal docs should not be imported from business seed files: %+v", doc)
	}
}

func TestProjectAggregateSourcesCRUD(t *testing.T) {
	db, ctx := newTestSQLite(t)
	report := model.ProjectWeeklyReport{ProjectID: "proj:owner-fields", Week: "2026-06-29", Content: "初稿", CreatedAt: time.Now()}
	if err := db.UpsertProjectWeeklyReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	report.Content = "覆盖稿"
	if err := db.UpsertProjectWeeklyReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	gotReport, err := db.GetProjectWeeklyReport(ctx, "proj:owner-fields", "2026-06-29")
	if err != nil || gotReport == nil || gotReport.Content != "覆盖稿" {
		t.Fatalf("weekly report=%+v err=%v", gotReport, err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:owner-fields", Name: "负责人项目", OwnerKey: "alice", ProductManagerKey: "bob", Active: true}); err != nil {
		t.Fatal(err)
	}
	ownerProject, err := db.GetProject(ctx, "proj:owner-fields")
	if err != nil || ownerProject == nil || ownerProject.OwnerKey != "alice" || ownerProject.ProductManagerKey != "bob" {
		t.Fatalf("owner project=%+v err=%v", ownerProject, err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:agg", Name: "聚合", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:a", Name: "A", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:b", Name: "B", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:agg", []string{"proj:a", "proj:b", "proj:a", "proj:agg"}); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListProjectAggregateSources(ctx, "proj:agg")
	if err != nil || len(sources) != 2 || sources[0].ID != "proj:a" || sources[1].ID != "proj:b" {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:agg", []string{"proj:b"}); err != nil {
		t.Fatal(err)
	}
	sources, err = db.ListProjectAggregateSources(ctx, "proj:agg")
	if err != nil || len(sources) != 1 || sources[0].ID != "proj:b" {
		t.Fatalf("sources after replace=%+v err=%v", sources, err)
	}
}

func columnExists(t *testing.T, db *SQLite, table, column string) bool {
	t.Helper()
	rows, err := db.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	return false
}

func hasSystemRoute(routes []model.SystemRoute, keyword, service string) bool {
	for _, route := range routes {
		if route.Keyword == keyword && route.ServiceName == service && route.Active {
			return true
		}
	}
	return false
}

func tableExists(t *testing.T, db *SQLite, table string) bool {
	t.Helper()
	var name string
	err := db.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == nil {
		return true
	}
	if err == sql.ErrNoRows {
		return false
	}
	t.Fatalf("query sqlite_master: %v", err)
	return false
}

func TestScheduleRepositoryRoundTripAndDelete(t *testing.T) {
	db, ctx := newTestSQLite(t)

	s := model.Schedule{
		ID:        "s1",
		OwnerKey:  "alice",
		StartDate: "2026-07-20",
		EndDate:   "2026-07-22",
		Task:      "写方案",
		Status:    "planned",
		Priority:  100,
	}
	if err := db.UpsertSchedule(ctx, s); err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}
	list, err := db.ListSchedules(ctx, "alice")
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(list) != 1 || list[0].ID != "s1" || list[0].Task != "写方案" {
		t.Fatalf("list = %+v", list)
	}

	if err := db.DeleteScheduleByID(ctx, "s1"); err != nil {
		t.Fatalf("DeleteScheduleByID: %v", err)
	}
	list, err = db.ListSchedules(ctx, "alice")
	if err != nil {
		t.Fatalf("ListSchedules after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("after delete len=%d, list=%+v", len(list), list)
	}
}

func TestPendingRepositoryReplacesExistingPending(t *testing.T) {
	db, ctx := newTestSQLite(t)

	firstID, err := db.PutPending(ctx, "alice", `{"n":1}`, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PutPending first: %v", err)
	}
	secondID, err := db.PutPending(ctx, "alice", `{"n":2}`, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PutPending second: %v", err)
	}
	if firstID == secondID {
		t.Fatalf("pending id reused: %q", firstID)
	}
	p, err := db.GetPending(ctx, "alice")
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if p == nil || p.ID != secondID || p.PayloadJSON != `{"n":2}` {
		t.Fatalf("pending = %+v, want second pending", p)
	}

	var oldStatus string
	if err := db.db.QueryRowContext(ctx, `SELECT status FROM pending_update WHERE id=?`, firstID).Scan(&oldStatus); err != nil {
		t.Fatalf("query first pending status: %v", err)
	}
	if oldStatus != "cancelled" {
		t.Fatalf("first pending status=%q, want cancelled", oldStatus)
	}
}

func TestPendingRepositoryExpiresPastPending(t *testing.T) {
	db, ctx := newTestSQLite(t)

	ref := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	db.SetClock(&clock.Fake{T: ref})

	// expires_at = 假时钟 - 1 分钟 → 已过期
	expired := ref.Add(-time.Minute)
	id, err := db.PutPending(ctx, "alice", `{"n":1}`, expired)
	if err != nil {
		t.Fatalf("PutPending: %v", err)
	}
	// 过期 → GetPending 返回 nil（惰性 GC）
	p, err := db.GetPending(ctx, "alice")
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if p != nil {
		t.Fatalf("expired pending = %+v, want nil", p)
	}

	// 惰性回收：状态应被标记为 expired
	var status string
	if err := db.db.QueryRowContext(ctx, `SELECT status FROM pending_update WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "expired" {
		t.Fatalf("status=%q, want expired", status)
	}
}

func TestMessageQueueIdempotentClaimAckAndFail(t *testing.T) {
	db, ctx := newTestSQLite(t)
	msg := model.Message{
		ID:           "m1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionIn,
		BotChannelID: "bot1",
		FeishuMsgID:  "f1",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"hello"}`,
	}
	if err := db.EnqueueMessage(ctx, msg); err != nil {
		t.Fatalf("EnqueueMessage first: %v", err)
	}
	dup := msg
	dup.ID = "m2"
	if err := db.EnqueueMessage(ctx, dup); err != nil {
		t.Fatalf("EnqueueMessage duplicate: %v", err)
	}
	claimed, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage: %v", err)
	}
	if claimed == nil || claimed.ID != "m1" || claimed.Status != "processing" || claimed.Attempts != 1 {
		t.Fatalf("claimed = %+v", claimed)
	}
	if err := db.AckMessage(ctx, claimed.ID); err != nil {
		t.Fatalf("AckMessage: %v", err)
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage after ack: %v", err)
	}
	if next != nil {
		t.Fatalf("next after duplicate+ack = %+v, want nil", next)
	}
}

func TestSeenPersonUpsertAndMemberFlag(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.UpsertSeenPerson(ctx, model.SeenPerson{OpenID: "ou_1", BotChannelID: "bot1", Name: "Alice", Source: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSeenPerson(ctx, model.SeenPerson{OpenID: "ou_1", BotChannelID: "bot1", Source: "inbound"}); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListSeenPersons(ctx)
	if err != nil || len(items) != 1 || items[0].Name != "Alice" || items[0].IsMember {
		t.Fatalf("seen persons=%+v err=%v", items, err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	items, err = db.ListSeenPersons(ctx)
	if err != nil || len(items) != 1 || !items[0].IsMember {
		t.Fatalf("seen persons after member=%+v err=%v", items, err)
	}
}

func TestMessageQueueFailRetriesThenDead(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "m1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionOut,
		BotChannelID: "bot1",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"hello"}`,
	}); err != nil {
		t.Fatalf("EnqueueMessage: %v", err)
	}
	for i := 1; i <= 3; i++ {
		claimed, err := db.ClaimNextMessage(ctx, model.DirectionOut)
		if err != nil {
			t.Fatalf("ClaimNextMessage #%d: %v", i, err)
		}
		if claimed == nil {
			t.Fatalf("ClaimNextMessage #%d = nil", i)
		}
		if err := db.FailMessage(ctx, claimed.ID, "send failed"); err != nil {
			t.Fatalf("FailMessage #%d: %v", i, err)
		}
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil {
		t.Fatalf("ClaimNextMessage after dead: %v", err)
	}
	if next != nil {
		t.Fatalf("next after dead = %+v, want nil", next)
	}
	var status string
	if err := db.db.QueryRowContext(ctx, `SELECT status FROM message WHERE id='m1'`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "dead" {
		t.Fatalf("status=%q, want dead", status)
	}
}

func TestEnqueueMessageRedactsContentBeforeStorage(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "m1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionIn,
		BotChannelID: "bot1",
		FeishuMsgID:  "f1",
		ChatType:     model.ChatPersonal,
		Content:      `{"token":"abc123","text":"Bearer abc.def.ghi 手机 17607679850"}`,
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil {
		t.Fatal("no message")
	}
	for _, leaked := range []string{"abc123", "abc.def.ghi", "17607679850"} {
		if strings.Contains(msg.Content, leaked) {
			t.Fatalf("stored content leaked %q: %s", leaked, msg.Content)
		}
	}
	if !strings.Contains(msg.Content, "***") || !strings.Contains(msg.Content, "1**********") {
		t.Fatalf("stored content not redacted: %s", msg.Content)
	}
}

func TestApproveKeyApplicationWithGrantCommitsCredentialAndGrantAtomically(t *testing.T) {
	db, ctx := newTestSQLite(t)
	app, err := db.CreateKeyApplication(ctx, model.KeyApplication{
		ID:               "ka1",
		ApplicantOpenID:  "ou_applicant",
		ApplicantAccount: "dev:personal:ou_applicant",
		ApplicantBotID:   "dev",
		ApplicantBotName: "UnifiedRobot",
		Description:      "接入测试",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.ApproveKeyApplicationWithGrant(ctx, app.ID, "ou_approver",
		model.RegisteredService{ID: "apply:ou-applicant", Name: "申请人", DeliveryType: "ws", ReplyMode: "sync", Enabled: true},
		model.ServiceAPIKey{ID: "FB-test-key", ServiceID: "apply:ou-applicant", KeyHash: "hash", Label: "ou_applicant", Active: true, CreatedAt: now},
		"dev:personal:ou_applicant",
		model.ChatEntity{ID: "dev:personal:ou_applicant", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_applicant", DisplayName: "Applicant", Active: true},
		model.Message{ID: "grant-ka1", ChatEntityID: "dev:personal:ou_applicant", Direction: model.DirectionOut, BotChannelID: "dev", FeishuMsgID: "grant-ka1", ChatType: model.ChatPersonal, Content: `{"text":"key_id: FB-test-key\nsecret 已隐藏"}`, Status: "queued"},
		now,
	); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetKeyApplication(ctx, app.ID)
	if err != nil || got == nil || got.Status != "approved" || got.KeyID != "FB-test-key" || got.ServiceID != "apply:ou-applicant" {
		t.Fatalf("application=%+v err=%v", got, err)
	}
	accounts, err := db.ListAPIKeyAccounts(ctx, "FB-test-key")
	if err != nil || len(accounts) != 1 || accounts[0] != "dev:personal:ou_applicant" {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ID != "grant-ka1" || strings.Contains(msg.Content, "wp_") {
		t.Fatalf("grant message=%+v err=%v", msg, err)
	}
}

func TestControlPlaneP1QueueRulesRetryReaperAndStats(t *testing.T) {
	db, ctx := newTestSQLite(t)
	rules, err := db.ListL1DecisionRules(ctx)
	if err != nil {
		t.Fatalf("ListL1DecisionRules: %v", err)
	}
	if len(rules) != 10 {
		t.Fatalf("L1 rules count=%d, want 10", len(rules))
	}
	if rules[0].Intent != "command.unlock" || rules[9].Intent != "unknown" {
		t.Fatalf("unexpected L1 rule order: first=%+v last=%+v", rules[0], rules[9])
	}
	expire := time.Now().UTC().Add(-time.Minute)
	task, inserted, err := db.EnqueueControlTask(ctx, model.ControlTask{
		ID:         "ct1",
		Source:     "feishu",
		SourceAddr: "ou_1#FB-test#UnifiedRobot",
		OwnerKey:   "u1",
		RawInput:   "hello",
		ExpireAt:   &expire,
	})
	if err != nil || !inserted {
		t.Fatalf("EnqueueControlTask inserted=%v err=%v", inserted, err)
	}
	if task.MaxAttempts != 3 || task.Status != "queued" || task.ExpireAt == nil {
		t.Fatalf("task defaults not applied: %+v", task)
	}
	if _, err := db.RetryControlTask(ctx, "ct1", "temporary"); err != nil {
		t.Fatalf("RetryControlTask: %v", err)
	}
	got, err := db.GetControlTask(ctx, "ct1")
	if err != nil {
		t.Fatalf("GetControlTask: %v", err)
	}
	if got.Attempts != 1 || got.Status != "queued" {
		t.Fatalf("retry state=%+v", got)
	}
	expired, err := db.ReapExpiredControlTasks(ctx, time.Now().UTC())
	if err != nil || len(expired) != 1 {
		t.Fatalf("ReapExpiredControlTasks expired=%+v err=%v", expired, err)
	}
	stats, err := db.ControlTaskStats(ctx)
	if err != nil {
		t.Fatalf("ControlTaskStats: %v", err)
	}
	if stats.Status["expired"] != 1 || stats.ExpiredRate != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestMessageQueueSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	ctx := context.Background()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite first: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate first: %v", err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "m1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionIn,
		BotChannelID: "bot1",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"persist"}`,
	}); err != nil {
		t.Fatalf("EnqueueMessage: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite second: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate second: %v", err)
	}
	msg, err := reopened.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage after reopen: %v", err)
	}
	if msg == nil || msg.ID != "m1" || msg.Content != `{"text":"persist"}` {
		t.Fatalf("message after reopen = %+v", msg)
	}
}

func TestRecoverProcessingAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recover.db")
	ctx := context.Background()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite first: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate first: %v", err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "m1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionIn,
		BotChannelID: "bot1",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"recover"}`,
	}); err != nil {
		t.Fatalf("EnqueueMessage: %v", err)
	}
	claimed, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage first: %v", err)
	}
	if claimed == nil || claimed.Attempts != 1 || claimed.Status != "processing" {
		t.Fatalf("claimed first = %+v", claimed)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite second: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate second: %v", err)
	}
	n, err := reopened.RecoverProcessing(ctx)
	if err != nil {
		t.Fatalf("RecoverProcessing: %v", err)
	}
	if n != 1 {
		t.Fatalf("recovered=%d, want 1", n)
	}
	reclaimed, err := reopened.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage second: %v", err)
	}
	if reclaimed == nil || reclaimed.ID != "m1" || reclaimed.Attempts != 2 || reclaimed.Status != "processing" {
		t.Fatalf("reclaimed = %+v", reclaimed)
	}
}

func TestMemberProgressAndRiskRepository(t *testing.T) {
	db, ctx := newTestSQLite(t)

	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatalf("UpsertMember: %v", err)
	}
	member, err := db.GetMemberByOwnerKey(ctx, "alice")
	if err != nil {
		t.Fatalf("GetMemberByOwnerKey: %v", err)
	}
	if member == nil || member.DisplayName != "Alice" || member.Role != model.RoleMember {
		t.Fatalf("member = %+v", member)
	}
	members, err := db.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	var foundAlice bool
	for _, item := range members {
		if item.OwnerKey == "alice" {
			foundAlice = true
		}
	}
	if !foundAlice {
		t.Fatalf("members = %+v", members)
	}

	sameTime := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "任务", Note: "旧", ReportedAt: sameTime}); err != nil {
		t.Fatalf("AddProgress old: %v", err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "任务", Note: "新", ReportedAt: sameTime}); err != nil {
		t.Fatalf("AddProgress new: %v", err)
	}
	progress, err := db.LatestProgress(ctx, "alice")
	if err != nil {
		t.Fatalf("LatestProgress: %v", err)
	}
	if len(progress) != 1 || progress[0].Note != "新" {
		t.Fatalf("latest progress = %+v", progress)
	}

	if err := db.ReportRisk(ctx, model.Risk{OwnerKey: "alice", Content: "环境不稳", Status: "open"}); err != nil {
		t.Fatalf("ReportRisk alice: %v", err)
	}
	if err := db.ReportRisk(ctx, model.Risk{OwnerKey: "bob", Content: "环境不稳", Status: "open"}); err != nil {
		t.Fatalf("ReportRisk bob: %v", err)
	}
	aliceRisks, err := db.ListOpenRisks(ctx, "alice")
	if err != nil {
		t.Fatalf("ListOpenRisks alice: %v", err)
	}
	bobRisks, err := db.ListOpenRisks(ctx, "bob")
	if err != nil {
		t.Fatalf("ListOpenRisks bob: %v", err)
	}
	if len(aliceRisks) != 1 || len(bobRisks) != 1 {
		t.Fatalf("risks alice=%+v bob=%+v", aliceRisks, bobRisks)
	}
	n, err := db.ResolveRisks(ctx, "alice", "环境")
	if err != nil {
		t.Fatalf("ResolveRisks: %v", err)
	}
	if n != 1 {
		t.Fatalf("resolved = %d", n)
	}
	aliceRisks, _ = db.ListOpenRisks(ctx, "alice")
	bobRisks, _ = db.ListOpenRisks(ctx, "bob")
	if len(aliceRisks) != 0 || len(bobRisks) != 1 {
		t.Fatalf("after resolve alice=%+v bob=%+v", aliceRisks, bobRisks)
	}
}

func TestCleanupBefore_DryRunAndConfirm(t *testing.T) {
	db, ctx := newTestSQLite(t)

	// 插入旧消息
	old := time.Now().Add(-2 * time.Hour)
	for i, id := range []string{"old1", "old2"} {
		if err := db.EnqueueMessage(ctx, model.Message{
			ID:           id,
			ChatEntityID: "chat1",
			Direction:    model.DirectionIn,
			BotChannelID: "bot1",
			ChatType:     model.ChatPersonal,
			Content:      `{"text":"old"}`,
			CreatedAt:    old,
		}); err != nil {
			t.Fatalf("EnqueueMessage %d: %v", i, err)
		}
	}

	// 插入新消息（不应被清理）
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "new1",
		ChatEntityID: "chat1",
		Direction:    model.DirectionIn,
		BotChannelID: "bot1",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"new"}`,
	}); err != nil {
		t.Fatalf("EnqueueMessage new: %v", err)
	}

	// Dry-run：只计数不删除
	cutoff := time.Now().Add(-1 * time.Hour)
	result, err := db.CleanupBefore(ctx, cutoff, false)
	if err != nil {
		t.Fatalf("CleanupBefore dry: %v", err)
	}
	if result.Messages < 2 {
		t.Fatalf("dry run messages=%d, want >=2 (2 old messages)", result.Messages)
	}

	// 确认清理
	result, err = db.CleanupBefore(ctx, cutoff, true)
	if err != nil {
		t.Fatalf("CleanupBefore confirm: %v", err)
	}
	if result.Messages < 2 {
		t.Fatalf("confirm messages=%d, want >=2", result.Messages)
	}

	// 新消息仍存在
	claimed, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage after cleanup: %v", err)
	}
	if claimed == nil || claimed.ID != "new1" {
		t.Fatalf("new message after cleanup = %+v", claimed)
	}
	// 旧消息已被删除
	claimed2, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage second: %v", err)
	}
	if claimed2 != nil {
		t.Fatalf("old message not cleaned up: %+v", claimed2)
	}
}
