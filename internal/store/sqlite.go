package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/redact"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（无 cgo）
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SQLite 是 Repository 的 SQLite 实现。
type SQLite struct {
	db    *sql.DB
	clock clock.Clock // 可注入时钟（测试用 Fake）；默认 clock.Real
}

// SetClock 注入时钟（测试用）。未设置时默认 clock.Real{}。
func (s *SQLite) SetClock(c clock.Clock) {
	if c != nil {
		s.clock = c
	}
}

func (s *SQLite) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}

const messageProcessingLease = 5 * time.Minute

// OpenSQLite 打开（或创建）SQLite 主库，启用 WAL + busy_timeout（§13.2）。
func OpenSQLite(path string) (*SQLite, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写者纪律：写连接数限 1（§13.1），读用 WAL 并发。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

// Migrate 按文件名顺序执行内嵌迁移。
func (s *SQLite) Migrate(ctx context.Context) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if err := s.execMigration(ctx, string(b)); err != nil {
			return fmt.Errorf("migrate %s: %w", e.Name(), err)
		}
	}
	if err := s.ensureSessionEndpointColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureBotChannelColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureSeenPersonTable(ctx); err != nil {
		return err
	}
	if err := s.ensureMemberColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectAggregateSourceTable(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectWeeklyReportTable(ctx); err != nil {
		return err
	}
	return s.ensureDefaultProject(ctx)
}

func (s *SQLite) execMigration(ctx context.Context, sqlText string) error {
	if _, err := s.db.ExecContext(ctx, sqlText); err == nil {
		return nil
	} else if !isDuplicateColumnError(err) {
		return err
	}

	for _, stmt := range strings.Split(sqlText, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			if isDuplicateColumnError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name")
}

func (s *SQLite) ensureBotChannelColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(bot_channel)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if !cols["app_secret_enc"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN app_secret_enc TEXT`); err != nil {
			return err
		}
	}
	if !cols["verification_token"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN verification_token TEXT`); err != nil {
			return err
		}
	}
	if !cols["encrypt_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN encrypt_key TEXT`); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *SQLite) ensureSeenPersonTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seen_person (
  open_id        TEXT NOT NULL,
  bot_channel_id TEXT NOT NULL,
  name           TEXT,
  source         TEXT NOT NULL,
  last_seen_at   TEXT,
  PRIMARY KEY(open_id, bot_channel_id)
)`)
	return err
}

func (s *SQLite) ensureSessionEndpointColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(session_endpoint)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["mirror_enabled"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN mirror_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["mirror_to"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN mirror_to TEXT`); err != nil {
			return err
		}
	}
	if !cols["client_ip"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN client_ip TEXT`); err != nil {
			return err
		}
	}
	if !cols["tool"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN tool TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["model"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["full_session_name"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN full_session_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["owner_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN owner_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["producer"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN producer INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["target_group"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN target_group TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["no_directory"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["no_directory_admin"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory_admin INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["no_directory_reported"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory_reported INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE session_endpoint SET no_directory_reported=no_directory WHERE no_directory=1`); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS ux_session_endpoint_owner_active_name
ON session_endpoint(owner_key, session_name)
WHERE active=1 AND owner_key <> ''`); err != nil {
		return err
	}
	return nil
}

func (s *SQLite) ensureMemberColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(member)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["dm_optout"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE member ADD COLUMN dm_optout INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) ensureProjectColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(project)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["parent_id"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN parent_id TEXT NOT NULL DEFAULT 'proj:default'`); err != nil {
			return err
		}
	}
	if !cols["owner_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN owner_key TEXT`); err != nil {
			return err
		}
	}
	if !cols["product_manager_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN product_manager_key TEXT`); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_parent ON project(parent_id)`)
	return err
}

func (s *SQLite) ensureProjectAggregateSourceTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_aggregate_source (
  aggregate_project_id TEXT NOT NULL,
  source_project_id    TEXT NOT NULL,
  created_at           TEXT,
  PRIMARY KEY(aggregate_project_id, source_project_id)
)`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_aggregate_source_source ON project_aggregate_source(source_project_id)`)
	return err
}

func (s *SQLite) ensureProjectWeeklyReportTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_weekly_report (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  week       TEXT NOT NULL,
  content    TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, week)
)`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_weekly_report_project_week ON project_weekly_report(project_id, week)`)
	return err
}

func (s *SQLite) ensureDefaultProject(ctx context.Context) error {
	notifyChat := os.Getenv("WP_SCHEDULE_NOTIFY_CHAT")
	notifyBot := os.Getenv("WP_SCHEDULE_NOTIFY_BOT")
	if notifyBot == "" {
		notifyBot = os.Getenv("FEISHU_BOT_CHANNEL_ID")
	}
	if notifyBot == "" {
		notifyBot = "unifiedrobot"
	}
	if err := s.UpsertProject(ctx, model.Project{
		ID:           "proj:default",
		Name:         "研发一组",
		ParentID:     "",
		OwnerKey:     "zsf",
		NotifyChatID: notifyChat,
		NotifyBotID:  notifyBot,
		Active:       true,
	}); err != nil {
		return err
	}
	if err := s.ensureCCConnectorBot(ctx); err != nil {
		return err
	}
	if err := s.ensureAIProjects(ctx); err != nil {
		return err
	}
	if err := s.ensureAIAggregateProject(ctx); err != nil {
		return err
	}
	if err := s.ensureSystemProducerIdentity(ctx); err != nil {
		return err
	}
	for _, owner := range []string{"zsf", "fulei", "tanping"} {
		if err := s.AssignProjectMember(ctx, "proj:default", owner); err != nil {
			return err
		}
	}
	if err := s.importDefaultScheduleDoc(ctx, "team", "", scheduleTeamFile(), "import", "migration"); err != nil {
		return err
	}
	for _, item := range []struct {
		owner string
		name  string
	}{
		{"zsf", "张三丰"},
		{"fulei", "符坚"},
		{"tanping", "唐盛"},
	} {
		if err := s.importDefaultScheduleDocAny(ctx, "personal", item.owner, []string{
			filepathJoin(schedulePersonalDir(), "工作计划-"+item.name+".md"),
			filepathJoin(schedulePersonalDir(), "工作计划-"+item.owner+".md"),
		}, "import", "migration"); err != nil {
			return err
		}
	}
	return nil
}

const systemProducerOwner = "system-v-task-internal"

func (s *SQLite) ensureSystemProducerIdentity(ctx context.Context) error {
	if err := s.UpsertMember(ctx, model.Member{
		OwnerKey:     systemProducerOwner,
		DisplayName:  "SYSTEM-V-TASK-INTERNAL",
		FeishuOpenID: "",
		Role:         model.RoleSystem,
		Active:       true,
	}); err != nil {
		return err
	}
	if err := s.UpsertRegisteredService(ctx, model.RegisteredService{
		ID:           systemProducerOwner,
		Name:         "SYSTEM-V-TASK-INTERNAL",
		Description:  "Virtual system task producer for group-only outputs",
		DeliveryType: "session",
		ReplyMode:    "none",
		Enabled:      true,
	}); err != nil {
		return err
	}
	if err := s.UpsertSystemService(ctx, model.SystemService{
		Name:        "sec-ops",
		Description: "SYSTEM-V-TASK-INTERNAL security operations control plane",
		Delivery:    "session",
		Active:      true,
	}); err != nil {
		return err
	}
	if err := s.UpsertSystemRoute(ctx, model.SystemRoute{
		Keyword:     "#系统安全",
		ServiceName: "sec-ops",
		Action:      "fanout",
		Priority:    0,
		Active:      true,
	}); err != nil {
		return err
	}
	keys, err := s.ListServiceAPIKeys(ctx, systemProducerOwner)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.Active {
			if err := s.BindAPIKeyAccount(ctx, key.ID, systemProducerOwner); err != nil {
				return err
			}
			return nil
		}
	}
	secret := strings.TrimSpace(os.Getenv("WP_SYSTEM_V_TASK_SECRET"))
	if secret == "" {
		secret = "unusable-" + newID()
	}
	sum := sha256.Sum256([]byte(secret))
	key := model.ServiceAPIKey{
		ID:        "FB-system-v-task-internal",
		ServiceID: systemProducerOwner,
		KeyHash:   hex.EncodeToString(sum[:]),
		Label:     "SYSTEM-V-TASK-INTERNAL",
		Active:    true,
		CreatedAt: s.now().UTC(),
	}
	if err := s.InsertServiceAPIKey(ctx, key); err != nil && !strings.Contains(strings.ToLower(err.Error()), "unique") {
		return err
	}
	return s.BindAPIKeyAccount(ctx, key.ID, systemProducerOwner)
}

func (s *SQLite) ensureAIAggregateProject(ctx context.Context) error {
	const aggregateID = "proj:ai-research"
	notifyChatID := strings.TrimSpace(os.Getenv("WP_SEED_AGGREGATE_CHAT"))
	project, err := s.GetProject(ctx, aggregateID)
	if err != nil {
		return err
	}
	if project == nil {
		if err := s.UpsertProject(ctx, model.Project{
			ID:           aggregateID,
			Name:         "AI研究项目",
			ParentID:     "proj:default",
			OwnerKey:     "zsf",
			NotifyChatID: notifyChatID,
			NotifyBotID:  "CC-Connector",
			EvidenceCron: "0 2 * * 1,3",
			EvidenceTZ:   "UTC",
			Active:       true,
		}); err != nil {
			return err
		}
	}
	sources, err := s.ListProjectAggregateSources(ctx, aggregateID)
	if err != nil {
		return err
	}
	if len(sources) != 0 {
		return nil
	}
	sourceIDs := make([]string, 0, len(aiProjectSeeds))
	for _, seed := range aiProjectSeeds {
		sourceIDs = append(sourceIDs, seed.ID)
	}
	return s.SetProjectAggregateSources(ctx, aggregateID, sourceIDs)
}

func (s *SQLite) ensureCCConnectorBot(ctx context.Context) error {
	appID := strings.TrimSpace(os.Getenv("WP_SEED_CCCONNECTOR_APPID"))
	return s.UpsertBotChannel(ctx, model.BotChannel{
		ID:         "CC-Connector",
		Name:       "CC-Connector",
		AppID:      appID,
		Purpose:    "group",
		CanSend:    true,
		CanReceive: true,
		Active:     true,
	})
}

var aiProjectSeeds = []struct {
	ID       string
	Name     string
	Keywords []string
}{
	{ID: "proj:imgsearch", Name: "图搜", Keywords: []string{"图搜", "图片搜索", "image search"}},
	{ID: "proj:ai-open-platform", Name: "AI开放平台", Keywords: []string{"AI开放平台", "开放平台"}},
	{ID: "proj:image-generation", Name: "图生图", Keywords: []string{"图生图", "图片生成", "image generation"}},
	{ID: "proj:attribute-analysis", Name: "属性分析", Keywords: []string{"属性分析", "属性"}},
	{ID: "proj:document-recognition", Name: "单据识别", Keywords: []string{"单据识别", "票据识别", "OCR"}},
	{ID: "proj:end-cloud-drive", Name: "端侧云盘", Keywords: []string{"端侧云盘", "云盘"}},
	{ID: "proj:cross-platform-order", Name: "跨平台订单", Keywords: []string{"跨平台订单", "订单"}},
	{ID: "proj:auto-marketing", Name: "自动化营销", Keywords: []string{"自动化营销", "营销"}},
}

func (s *SQLite) ensureAIProjects(ctx context.Context) error {
	teamText := ""
	if data, err := os.ReadFile(scheduleTeamFile()); err == nil {
		teamText = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, seed := range aiProjectSeeds {
		project, err := s.GetProject(ctx, seed.ID)
		if err != nil {
			return err
		}
		if project == nil {
			if err := s.UpsertProject(ctx, model.Project{
				ID:          seed.ID,
				Name:        seed.Name,
				ParentID:    "proj:default",
				OwnerKey:    "zsf",
				NotifyBotID: "unifiedrobot",
				Active:      true,
			}); err != nil {
				return err
			}
		}
		existing, err := s.LatestScheduleDoc(ctx, seed.ID, "team", "")
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if content := extractProjectSchedule(teamText, seed.Name, seed.Keywords); strings.TrimSpace(content) != "" {
			if _, err := s.AppendScheduleDoc(ctx, model.ScheduleDoc{
				ProjectID: seed.ID,
				Kind:      "team",
				Content:   content,
				Source:    "import",
				CreatedBy: "migration",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractProjectSchedule(teamText, name string, keywords []string) string {
	teamText = strings.TrimSpace(teamText)
	if teamText == "" {
		return ""
	}
	lines := strings.Split(teamText, "\n")
	var out []string
	for _, line := range lines {
		if containsAnyFold(line, append([]string{name}, keywords...)) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "# " + name + " 团队排期\n\n" + strings.Join(out, "\n")
}

func containsAnyFold(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		needle = strings.TrimSpace(needle)
		if needle != "" && strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func (s *SQLite) importDefaultScheduleDocAny(ctx context.Context, kind, ownerKey string, paths []string, source, createdBy string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return s.importDefaultScheduleDoc(ctx, kind, ownerKey, path, source, createdBy)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *SQLite) importDefaultScheduleDoc(ctx context.Context, kind, ownerKey, path, source, createdBy string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	existing, err := s.LatestScheduleDoc(ctx, "proj:default", kind, ownerKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}
	_, err = s.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default",
		Kind:      kind,
		OwnerKey:  ownerKey,
		Content:   content,
		Source:    source,
		CreatedBy: createdBy,
	})
	return err
}

func scheduleTeamFile() string {
	if v := os.Getenv("WP_SCHEDULE_TEAM_FILE"); v != "" {
		return v
	}
	return "docs/AI研究任务排期/AI-研究工作内容清单.md"
}

func schedulePersonalDir() string {
	if v := os.Getenv("WP_SCHEDULE_PERSONAL_DIR"); v != "" {
		return v
	}
	return "docs/AI研究任务排期"
}

func filepathJoin(elem ...string) string {
	return strings.Join(elem, string(os.PathSeparator))
}

func (s *SQLite) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&n)
	return n, err
}

func (s *SQLite) CreateAdmin(ctx context.Context, u model.AdminUser) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_user(id, username, password_hash, active, created_at) VALUES(?,?,?,1,?)`,
		u.ID, u.Username, u.PasswordHash, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) GetAdmin(ctx context.Context, username string) (*model.AdminUser, error) {
	var u model.AdminUser
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, active FROM admin_user WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Active = active == 1
	return &u, nil
}

func (s *SQLite) UpsertChatEntity(ctx context.Context, e model.ChatEntity) error {
	active := boolInt(e.Active)
	if e.ID == "" {
		e.ID = fmt.Sprintf("%s:%s", e.BotChannelID, e.FeishuID)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_entity(id, bot_channel_id, type, feishu_id, display_name, bound_owner, active)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(bot_channel_id, feishu_id) DO UPDATE SET type=excluded.type, display_name=excluded.display_name, bound_owner=excluded.bound_owner, active=excluded.active`,
		e.ID, e.BotChannelID, string(e.Type), e.FeishuID, e.DisplayName, e.BoundOwner, active)
	return err
}

func (s *SQLite) UpsertProject(ctx context.Context, p model.Project) error {
	now := s.now().UTC().Format(time.RFC3339)
	active := boolInt(p.Active)
	if strings.TrimSpace(p.NotifyBotID) == "" {
		p.NotifyBotID = "unifiedrobot"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO project(id, name, parent_id, owner_key, product_manager_key, notify_chat_id, notify_bot_id, transcript_dirs, evidence_cron, evidence_tz, active, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, parent_id=excluded.parent_id, owner_key=excluded.owner_key, product_manager_key=excluded.product_manager_key, notify_chat_id=excluded.notify_chat_id, notify_bot_id=excluded.notify_bot_id,
		   transcript_dirs=excluded.transcript_dirs, evidence_cron=excluded.evidence_cron, evidence_tz=excluded.evidence_tz,
		   active=excluded.active, updated_at=excluded.updated_at`,
		p.ID, p.Name, p.ParentID, p.OwnerKey, p.ProductManagerKey, p.NotifyChatID, p.NotifyBotID, p.TranscriptDirs, p.EvidenceCron, p.EvidenceTZ, active, now, now)
	return err
}

func (s *SQLite) GetProject(ctx context.Context, id string) (*model.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, projectSelectSQL()+` WHERE id=?`, id))
}

func (s *SQLite) GetProjectByGroupChat(ctx context.Context, chatID string) (*model.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, projectSelectSQL()+` WHERE notify_chat_id=? AND active=1 ORDER BY id LIMIT 1`, chatID))
}

func (s *SQLite) ListProjects(ctx context.Context, activeOnly bool) ([]model.Project, error) {
	query := projectSelectSQL()
	if activeOnly {
		query += ` WHERE active=1`
	}
	query += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) scanProject(row rowScanner) (*model.Project, error) {
	p, err := scanProjectRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanProjectRow(row rowScanner) (model.Project, error) {
	var p model.Project
	var active int
	var created, updated sql.NullString
	var ownerKey, productManagerKey sql.NullString
	err := row.Scan(&p.ID, &p.Name, &p.ParentID, &ownerKey, &productManagerKey, &p.NotifyChatID, &p.NotifyBotID, &p.TranscriptDirs, &p.EvidenceCron, &p.EvidenceTZ, &active, &created, &updated)
	if err != nil {
		return p, err
	}
	p.Active = active == 1
	if ownerKey.Valid {
		p.OwnerKey = ownerKey.String
	}
	if productManagerKey.Valid {
		p.ProductManagerKey = productManagerKey.String
	}
	if created.Valid {
		p.CreatedAt, _ = time.Parse(time.RFC3339, created.String)
	}
	if updated.Valid {
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	return p, nil
}

func projectSelectSQL() string {
	return `SELECT id, name, parent_id, COALESCE(owner_key, ''), COALESCE(product_manager_key, ''), notify_chat_id, notify_bot_id, transcript_dirs, evidence_cron, evidence_tz, active, created_at, updated_at FROM project`
}

func (s *SQLite) AppendScheduleDoc(ctx context.Context, d model.ScheduleDoc) (model.ScheduleDoc, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	if d.Kind == "" {
		d.Kind = "team"
	}
	if d.ProjectID == "" {
		d.ProjectID = "proj:default"
	}
	if d.Source == "" {
		d.Source = "unknown"
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = s.now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	var latest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(version) FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=?`, d.ProjectID, d.Kind, d.OwnerKey).Scan(&latest); err != nil {
		return d, err
	}
	if d.Version <= 0 {
		d.Version = 1
		if latest.Valid {
			d.Version = int(latest.Int64) + 1
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schedule_doc(id, project_id, kind, owner_key, version, content, source, created_by, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Kind, d.OwnerKey, d.Version, d.Content, d.Source, d.CreatedBy, d.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}

func (s *SQLite) LatestScheduleDoc(ctx context.Context, projectID, kind, ownerKey string) (*model.ScheduleDoc, error) {
	return scanScheduleDoc(s.db.QueryRowContext(ctx,
		`SELECT id, project_id, kind, owner_key, version, content, source, created_by, created_at
		 FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=? ORDER BY version DESC LIMIT 1`,
		projectID, kind, ownerKey))
}

func (s *SQLite) ListScheduleDocVersions(ctx context.Context, projectID, kind, ownerKey string) ([]model.ScheduleDoc, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, kind, owner_key, version, content, source, created_by, created_at
		 FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=? ORDER BY version DESC`,
		projectID, kind, ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ScheduleDoc
	for rows.Next() {
		d, err := scanScheduleDocRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanScheduleDoc(row rowScanner) (*model.ScheduleDoc, error) {
	d, err := scanScheduleDocRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func scanScheduleDocRow(row rowScanner) (model.ScheduleDoc, error) {
	var d model.ScheduleDoc
	var created string
	var createdBy sql.NullString
	err := row.Scan(&d.ID, &d.ProjectID, &d.Kind, &d.OwnerKey, &d.Version, &d.Content, &d.Source, &createdBy, &created)
	if err != nil {
		return d, err
	}
	if createdBy.Valid {
		d.CreatedBy = createdBy.String
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return d, nil
}

func (s *SQLite) AssignProjectMember(ctx context.Context, projectID, ownerKey string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO project_member(project_id, owner_key) VALUES(?,?)`, projectID, ownerKey)
	return err
}

func (s *SQLite) UnassignProjectMember(ctx context.Context, projectID, ownerKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_member WHERE project_id=? AND owner_key=?`, projectID, ownerKey)
	return err
}

func (s *SQLite) ListProjectMembers(ctx context.Context, projectID string) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, pm.owner_key, m.display_name, m.feishu_open_id, m.role, m.evidence_optout, m.dm_optout, m.active
		 FROM project_member pm
		 LEFT JOIN member m ON m.owner_key=pm.owner_key
		 WHERE pm.project_id=?
		 ORDER BY COALESCE(m.display_name, pm.owner_key), pm.owner_key`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Member
	for rows.Next() {
		var m model.Member
		var id, display, feishu, role sql.NullString
		var optOut, dmOptOut, active sql.NullInt64
		if err := rows.Scan(&id, &m.OwnerKey, &display, &feishu, &role, &optOut, &dmOptOut, &active); err != nil {
			return nil, err
		}
		if id.Valid {
			m.ID = id.String
		}
		if display.Valid {
			m.DisplayName = display.String
		}
		if feishu.Valid {
			m.FeishuOpenID = feishu.String
		}
		if role.Valid {
			m.Role = model.Role(role.String)
		} else {
			m.Role = model.RoleMember
		}
		m.EvidenceOptOut = optOut.Valid && optOut.Int64 == 1
		m.DMOptOut = dmOptOut.Valid && dmOptOut.Int64 == 1
		m.Active = !active.Valid || active.Int64 == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) SetProjectAggregateSources(ctx context.Context, aggregateProjectID string, sourceProjectIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_aggregate_source WHERE aggregate_project_id=?`, aggregateProjectID); err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	for _, sourceID := range sourceProjectIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" || sourceID == aggregateProjectID || seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO project_aggregate_source(aggregate_project_id, source_project_id, created_at) VALUES(?,?,?)`,
			aggregateProjectID, sourceID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) ListProjectAggregateSources(ctx context.Context, aggregateProjectID string) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.parent_id, COALESCE(p.owner_key, ''), COALESCE(p.product_manager_key, ''), p.notify_chat_id, p.notify_bot_id, p.transcript_dirs, p.evidence_cron, p.evidence_tz, p.active, p.created_at, p.updated_at
		   FROM project_aggregate_source pas
		   JOIN project p ON p.id=pas.source_project_id
		  WHERE pas.aggregate_project_id=?
		  ORDER BY p.id`, aggregateProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAggregateProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT aggregate_project_id FROM project_aggregate_source ORDER BY aggregate_project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertProjectWeeklyReport(ctx context.Context, r model.ProjectWeeklyReport) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO project_weekly_report(id, project_id, week, content, created_at) VALUES(?,?,?,?,?)
		 ON CONFLICT(project_id, week) DO UPDATE SET content=excluded.content, created_at=excluded.created_at`,
		r.ID, r.ProjectID, r.Week, r.Content, r.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) GetProjectWeeklyReport(ctx context.Context, projectID, week string) (*model.ProjectWeeklyReport, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, week, content, created_at FROM project_weekly_report WHERE project_id=? AND week=?`, projectID, week)
	var r model.ProjectWeeklyReport
	var created string
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Week, &r.Content, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &r, nil
}

func (s *SQLite) GetChatEntity(ctx context.Context, botChannelID, feishuID string) (*model.ChatEntity, error) {
	var e model.ChatEntity
	var typ string
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, bot_channel_id, type, feishu_id, display_name, bound_owner, active
		   FROM chat_entity WHERE bot_channel_id=? AND feishu_id=?`,
		botChannelID, feishuID).
		Scan(&e.ID, &e.BotChannelID, &typ, &e.FeishuID, &e.DisplayName, &e.BoundOwner, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Type = model.ChatType(typ)
	e.Active = active == 1
	return &e, nil
}

func (s *SQLite) ListChatEntities(ctx context.Context, typ model.ChatType) ([]model.ChatEntity, error) {
	query := `SELECT id, bot_channel_id, type, feishu_id, display_name, bound_owner, active FROM chat_entity`
	var args []any
	if typ != "" {
		query += ` WHERE type=?`
		args = append(args, string(typ))
	}
	query += ` ORDER BY bot_channel_id, display_name, feishu_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ChatEntity
	for rows.Next() {
		var e model.ChatEntity
		var rowType string
		var active int
		if err := rows.Scan(&e.ID, &e.BotChannelID, &rowType, &e.FeishuID, &e.DisplayName, &e.BoundOwner, &active); err != nil {
			return nil, err
		}
		e.Type = model.ChatType(rowType)
		e.Active = active == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertBotChannel(ctx context.Context, c model.BotChannel) error {
	secret := any(nil)
	if c.AppSecretEnc != "" {
		secret = c.AppSecretEnc
	}
	token := any(nil)
	if c.VerificationToken != "" {
		token = c.VerificationToken
	}
	encryptKey := any(nil)
	if c.EncryptKey != "" {
		encryptKey = c.EncryptKey
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bot_channel(id, name, app_id, app_secret_enc, verification_token, encrypt_key, purpose, can_send, can_receive, active)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, app_id=excluded.app_id, app_secret_enc=COALESCE(excluded.app_secret_enc, bot_channel.app_secret_enc), verification_token=COALESCE(excluded.verification_token, bot_channel.verification_token), encrypt_key=COALESCE(excluded.encrypt_key, bot_channel.encrypt_key), purpose=excluded.purpose, can_send=excluded.can_send, can_receive=excluded.can_receive, active=excluded.active`,
		c.ID, c.Name, c.AppID, secret, token, encryptKey, c.Purpose, boolInt(c.CanSend), boolInt(c.CanReceive), boolInt(c.Active))
	return err
}

func (s *SQLite) ListBotChannels(ctx context.Context) ([]model.BotChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, app_id, app_secret_enc, COALESCE(verification_token, ''), COALESCE(encrypt_key, ''), purpose, can_send, can_receive, active FROM bot_channel ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BotChannel
	for rows.Next() {
		var c model.BotChannel
		var secret sql.NullString
		var token, encryptKey string
		var canSend, canReceive, active int
		if err := rows.Scan(&c.ID, &c.Name, &c.AppID, &secret, &token, &encryptKey, &c.Purpose, &canSend, &canReceive, &active); err != nil {
			return nil, err
		}
		if secret.Valid {
			c.AppSecretEnc = secret.String
			c.AppSecretSet = secret.String != ""
		}
		c.VerificationToken = token
		c.VerificationTokenSet = token != ""
		c.EncryptKey = encryptKey
		c.EncryptKeySet = encryptKey != ""
		c.CanSend = canSend == 1
		c.CanReceive = canReceive == 1
		c.Active = active == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSeenPerson(ctx context.Context, p model.SeenPerson) error {
	if p.LastSeenAt.IsZero() {
		p.LastSeenAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seen_person(open_id, bot_channel_id, name, source, last_seen_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(open_id, bot_channel_id) DO UPDATE SET name=COALESCE(NULLIF(excluded.name,''), seen_person.name), source=excluded.source, last_seen_at=excluded.last_seen_at`,
		p.OpenID, p.BotChannelID, p.Name, p.Source, p.LastSeenAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListSeenPersons(ctx context.Context) ([]model.SeenPerson, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sp.open_id, sp.bot_channel_id, sp.name, sp.source, sp.last_seen_at,
		        EXISTS(SELECT 1 FROM member m WHERE m.feishu_open_id=sp.open_id AND m.active=1)
		   FROM seen_person sp
		  ORDER BY sp.name, sp.open_id, sp.bot_channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SeenPerson
	for rows.Next() {
		var p model.SeenPerson
		var seen string
		var isMember int
		if err := rows.Scan(&p.OpenID, &p.BotChannelID, &p.Name, &p.Source, &seen, &isMember); err != nil {
			return nil, err
		}
		p.LastSeenAt, _ = time.Parse(time.RFC3339, seen)
		p.IsMember = isMember == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSeenPersonGroup(ctx context.Context, p model.SeenPersonGroup) error {
	if p.LastSeenAt.IsZero() {
		p.LastSeenAt = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seen_person_group(open_id, bot_channel_id, group_chat_id, group_name, last_seen_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(open_id, bot_channel_id, group_chat_id) DO UPDATE SET
		   group_name=COALESCE(NULLIF(excluded.group_name,''), seen_person_group.group_name),
		   last_seen_at=excluded.last_seen_at`,
		p.OpenID, p.BotChannelID, p.GroupChatID, p.GroupName, p.LastSeenAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListSeenPersonsByGroup(ctx context.Context, groupChatID string) ([]model.SeenPersonGroup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT open_id, bot_channel_id, group_chat_id, group_name, last_seen_at
		   FROM seen_person_group
		  WHERE group_chat_id=?
		  ORDER BY group_name, open_id, bot_channel_id`, groupChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SeenPersonGroup
	for rows.Next() {
		var p model.SeenPersonGroup
		var groupName sql.NullString
		var seen string
		if err := rows.Scan(&p.OpenID, &p.BotChannelID, &p.GroupChatID, &groupName, &seen); err != nil {
			return nil, err
		}
		if groupName.Valid {
			p.GroupName = groupName.String
		}
		p.LastSeenAt, _ = time.Parse(time.RFC3339, seen)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteBotChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bot_channel WHERE id=?`, id)
	return err
}

func (s *SQLite) EnqueueMessage(ctx context.Context, m model.Message) error {
	if m.ID == "" {
		m.ID = newID()
	}
	if m.Status == "" {
		m.Status = "queued"
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.Content = redact.Content(m.Content)
	var feishuMsgID any
	if m.FeishuMsgID != "" {
		feishuMsgID = m.FeishuMsgID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO message(id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, content_json, status, attempts, error, created_at, processed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL)`,
		m.ID, m.ChatEntityID, string(m.Direction), m.BotChannelID, feishuMsgID, string(m.ChatType), m.SenderOpenID, m.Content, m.Status, m.Attempts, m.Err, m.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ClaimNextMessage(ctx context.Context, direction model.Direction) (*model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cutoff := time.Now().UTC().Add(-messageProcessingLease).Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`UPDATE message SET status='queued' WHERE direction=? AND status='processing' AND processed_at < ?`,
		string(direction), cutoff); err != nil {
		return nil, err
	}

	var id string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM message WHERE direction=? AND status='queued' ORDER BY created_at, id LIMIT 1`,
		string(direction)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`UPDATE message SET status='processing', attempts=attempts+1, processed_at=? WHERE id=? AND status='queued'`,
		now, id); err != nil {
		return nil, err
	}
	m, err := scanMessageRow(tx.QueryRowContext(ctx,
		`SELECT id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, content_json, status, attempts, error, created_at, processed_at FROM message WHERE id=?`,
		id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *SQLite) AckMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE message SET status='done', error=NULL, processed_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLite) FailMessage(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE message
		   SET status=CASE WHEN attempts >= 3 THEN 'dead' ELSE 'queued' END,
		       error=?,
		       processed_at=?
		 WHERE id=?`,
		reason, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLite) RecoverProcessing(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE message SET status='queued' WHERE status='processing'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLite) RecentMessages(ctx context.Context, filter model.MessageFilter) ([]model.Message, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := `SELECT id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, content_json, status, attempts, error, created_at, processed_at FROM message WHERE 1=1`
	args := []any{}
	if filter.BotChannelID != "" {
		query += ` AND bot_channel_id=?`
		args = append(args, filter.BotChannelID)
	}
	if filter.ChatEntityID != "" {
		query += ` AND chat_entity_id=?`
		args = append(args, filter.ChatEntityID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteCollectMessagesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM message WHERE direction=? AND created_at < ?`,
		string(model.DirectionCollect), cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLite) MessageStats(ctx context.Context) (model.MessageStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM message GROUP BY status`)
	if err != nil {
		return model.MessageStats{}, err
	}
	defer rows.Close()
	var st model.MessageStats
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return model.MessageStats{}, err
		}
		st.Total += n
		switch status {
		case "queued":
			st.Queued = n
		case "processing":
			st.Processing = n
		case "done":
			st.Done = n
		case "dead":
			st.Dead = n
		}
	}
	return st, rows.Err()
}

func (s *SQLite) ListSchedules(ctx context.Context, ownerKey string) ([]model.Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, start_date, end_date, task, status, priority FROM schedule WHERE owner_key=? ORDER BY start_date`, ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Schedule
	for rows.Next() {
		var s model.Schedule
		if err := rows.Scan(&s.ID, &s.OwnerKey, &s.StartDate, &s.EndDate, &s.Task, &s.Status, &s.Priority); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSchedule(ctx context.Context, sc model.Schedule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO schedule(id, owner_key, start_date, end_date, task, status, priority, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(owner_key, start_date, task) DO UPDATE SET end_date=excluded.end_date, status=excluded.status, priority=excluded.priority, updated_at=excluded.updated_at`,
		sc.ID, sc.OwnerKey, sc.StartDate, sc.EndDate, sc.Task, sc.Status, sc.Priority, now, now)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessageRow(row rowScanner) (*model.Message, error) {
	var m model.Message
	var direction, chatType, created string
	var feishuMsgID, errText, processed sql.NullString
	err := row.Scan(&m.ID, &m.ChatEntityID, &direction, &m.BotChannelID, &feishuMsgID, &chatType, &m.SenderOpenID, &m.Content, &m.Status, &m.Attempts, &errText, &created, &processed)
	if err != nil {
		return nil, err
	}
	m.Direction = model.Direction(direction)
	m.ChatType = model.ChatType(chatType)
	if feishuMsgID.Valid {
		m.FeishuMsgID = feishuMsgID.String
	}
	if errText.Valid {
		m.Err = errText.String
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if processed.Valid {
		if t, err := time.Parse(time.RFC3339, processed.String); err == nil {
			m.ProcessedAt = &t
		}
	}
	return &m, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *SQLite) DeleteScheduleByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedule WHERE id=?`, id)
	return err
}

func (s *SQLite) UpsertMember(ctx context.Context, m model.Member) error {
	if m.ID == "" {
		m.ID = newID()
	}
	role := string(m.Role)
	if role == "" {
		role = string(model.RoleMember)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO member(id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(owner_key) DO UPDATE SET display_name=excluded.display_name, feishu_open_id=excluded.feishu_open_id, role=excluded.role, evidence_optout=excluded.evidence_optout, dm_optout=excluded.dm_optout, active=excluded.active`,
		m.ID, m.OwnerKey, m.DisplayName, m.FeishuOpenID, role, boolInt(m.EvidenceOptOut), boolInt(m.DMOptOut), boolInt(m.Active))
	return err
}

func (s *SQLite) GetMemberByOwnerKey(ctx context.Context, ownerKey string) (*model.Member, error) {
	var m model.Member
	var role string
	var optOut, dmOptOut, active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active FROM member WHERE owner_key=?`,
		ownerKey).Scan(&m.ID, &m.OwnerKey, &m.DisplayName, &m.FeishuOpenID, &role, &optOut, &dmOptOut, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Role = model.Role(role)
	m.EvidenceOptOut = optOut == 1
	m.DMOptOut = dmOptOut == 1
	m.Active = active == 1
	return &m, nil
}

func (s *SQLite) ListMembers(ctx context.Context) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active FROM member WHERE active=1 ORDER BY display_name, owner_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Member
	for rows.Next() {
		var m model.Member
		var role string
		var optOut, dmOptOut, active int
		if err := rows.Scan(&m.ID, &m.OwnerKey, &m.DisplayName, &m.FeishuOpenID, &role, &optOut, &dmOptOut, &active); err != nil {
			return nil, err
		}
		m.Role = model.Role(role)
		m.EvidenceOptOut = optOut == 1
		m.DMOptOut = dmOptOut == 1
		m.Active = active == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) AddProgress(ctx context.Context, p model.Progress) error {
	if p.ID == "" {
		p.ID = newID()
	}
	if p.ReportedAt.IsZero() {
		p.ReportedAt = time.Now().UTC()
	}
	if p.Source == "" {
		p.Source = "self"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress(id, owner_key, task_key, note, percent, reported_at, source) VALUES(?,?,?,?,?,?,?)`,
		p.ID, p.OwnerKey, p.TaskKey, p.Note, p.Percent, p.ReportedAt.UTC().Format(time.RFC3339Nano), p.Source)
	return err
}

func (s *SQLite) LatestProgress(ctx context.Context, ownerKey string) ([]model.Progress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.owner_key, p.task_key, p.note, p.percent, p.reported_at, p.source
		   FROM progress p
		  WHERE p.owner_key=?
		    AND NOT EXISTS (
		      SELECT 1 FROM progress q
		       WHERE q.owner_key=p.owner_key AND q.task_key=p.task_key
		         AND (q.reported_at > p.reported_at OR (q.reported_at=p.reported_at AND q.rowid > p.rowid))
		    )
		  ORDER BY p.reported_at DESC, p.id DESC`,
		ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Progress
	for rows.Next() {
		var p model.Progress
		var reported string
		if err := rows.Scan(&p.ID, &p.OwnerKey, &p.TaskKey, &p.Note, &p.Percent, &reported, &p.Source); err != nil {
			return nil, err
		}
		p.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListProgressBetween(ctx context.Context, ownerKey string, from, to time.Time) ([]model.Progress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, task_key, note, percent, reported_at, source
		   FROM progress
		  WHERE owner_key=? AND reported_at>=? AND reported_at<?
		  ORDER BY reported_at ASC, id ASC`,
		ownerKey, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Progress
	for rows.Next() {
		var p model.Progress
		var reported string
		if err := rows.Scan(&p.ID, &p.OwnerKey, &p.TaskKey, &p.Note, &p.Percent, &reported, &p.Source); err != nil {
			return nil, err
		}
		p.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ReportRisk(ctx context.Context, r model.Risk) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.Status == "" {
		r.Status = "open"
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE risk SET reporters_json=? WHERE status='open' AND owner_key=? AND content=?`,
		r.ReportersJSON, r.OwnerKey, r.Content)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO risk(id, owner_key, content, status, related_task_key, reporters_json, created_at) VALUES(?,?,?,?,?,?,?)`,
		r.ID, r.OwnerKey, r.Content, r.Status, r.RelatedTaskKey, r.ReportersJSON, r.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ResolveRisks(ctx context.Context, ownerKey, keyword string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE risk SET status='resolved', resolved_at=? WHERE status='open' AND owner_key=? AND content LIKE ?`,
		time.Now().UTC().Format(time.RFC3339), ownerKey, "%"+keyword+"%")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLite) ListOpenRisks(ctx context.Context, ownerKey string) ([]model.Risk, error) {
	query := `SELECT id, owner_key, content, status, related_task_key, reporters_json, created_at, resolved_at FROM risk WHERE status='open'`
	args := []any{}
	if ownerKey != "" {
		query += ` AND owner_key=?`
		args = append(args, ownerKey)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Risk
	for rows.Next() {
		var r model.Risk
		var owner, related, reporters sql.NullString
		var created string
		var resolved sql.NullString
		if err := rows.Scan(&r.ID, &owner, &r.Content, &r.Status, &related, &reporters, &created, &resolved); err != nil {
			return nil, err
		}
		if owner.Valid {
			r.OwnerKey = owner.String
		}
		if related.Valid {
			r.RelatedTaskKey = related.String
		}
		if reporters.Valid {
			r.ReportersJSON = reporters.String
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if resolved.Valid {
			if t, err := time.Parse(time.RFC3339, resolved.String); err == nil {
				r.ResolvedAt = &t
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) AddAIEvidence(ctx context.Context, e model.AIEvidence) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ai_evidence(id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.OwnerKey, e.SessionID, e.SessionSource, e.WorkItem, e.Artifact, e.FilesJSON, e.ActionType, e.OccurredAt.UTC().Format(time.RFC3339Nano), e.MappedTaskKey, e.Confidence, e.RawExcerptHash)
	return err
}

func (s *SQLite) ListAIEvidence(ctx context.Context, ownerKey, taskKey string) ([]model.AIEvidence, error) {
	query := `SELECT id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash FROM ai_evidence WHERE owner_key=?`
	args := []any{ownerKey}
	if taskKey != "" {
		query += ` AND mapped_task_key=?`
		args = append(args, taskKey)
	}
	query += ` ORDER BY occurred_at DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AIEvidence
	for rows.Next() {
		var e model.AIEvidence
		var occurred string
		if err := rows.Scan(&e.ID, &e.OwnerKey, &e.SessionID, &e.SessionSource, &e.WorkItem, &e.Artifact, &e.FilesJSON, &e.ActionType, &occurred, &e.MappedTaskKey, &e.Confidence, &e.RawExcerptHash); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAIEvidenceBetween(ctx context.Context, from, to time.Time) ([]model.AIEvidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash
		   FROM ai_evidence
		  WHERE occurred_at>=? AND occurred_at<?
		  ORDER BY occurred_at ASC, id ASC`,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AIEvidence
	for rows.Next() {
		var e model.AIEvidence
		var occurred string
		if err := rows.Scan(&e.ID, &e.OwnerKey, &e.SessionID, &e.SessionSource, &e.WorkItem, &e.Artifact, &e.FilesJSON, &e.ActionType, &occurred, &e.MappedTaskKey, &e.Confidence, &e.RawExcerptHash); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) MapAIEvidenceToTask(ctx context.Context, evidenceID, ownerKey, taskKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ai_evidence SET mapped_task_key=? WHERE id=? AND owner_key=?`,
		taskKey, evidenceID, ownerKey)
	return err
}

func (s *SQLite) UpsertReconciliation(ctx context.Context, r model.Reconciliation) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.ComputedAt.IsZero() {
		r.ComputedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM reconciliation WHERE owner_key=? AND task_key=?`, r.OwnerKey, r.TaskKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO reconciliation(id, owner_key, task_key, self_status, evidence_count, verdict, computed_at, detail_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.OwnerKey, r.TaskKey, r.SelfStatus, r.EvidenceCount, r.Verdict, r.ComputedAt.UTC().Format(time.RFC3339Nano), r.DetailJSON); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ListReconciliation(ctx context.Context, ownerKey string) ([]model.Reconciliation, error) {
	query := `SELECT id, owner_key, task_key, self_status, evidence_count, verdict, computed_at, detail_json FROM reconciliation`
	args := []any{}
	if ownerKey != "" {
		query += ` WHERE owner_key=?`
		args = append(args, ownerKey)
	}
	query += ` ORDER BY computed_at DESC, owner_key, task_key`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Reconciliation
	for rows.Next() {
		var r model.Reconciliation
		var computed string
		if err := rows.Scan(&r.ID, &r.OwnerKey, &r.TaskKey, &r.SelfStatus, &r.EvidenceCount, &r.Verdict, &computed, &r.DetailJSON); err != nil {
			return nil, err
		}
		r.ComputedAt, _ = time.Parse(time.RFC3339Nano, computed)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutPending 写入待确认变更：先把该 actor 的旧 pending 置 cancelled，再插入新条（§15.2 同时只一个）。
func (s *SQLite) PutPending(ctx context.Context, actor, payloadJSON string, expiresAt time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE pending_update SET status='cancelled' WHERE open_id=? AND status='pending'`, actor); err != nil {
		return "", err
	}
	id := newID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO pending_update(id, open_id, payload_json, status, created_at, expires_at) VALUES(?,?,?,'pending',?,?)`,
		id, actor, payloadJSON, time.Now().UTC().Format(time.RFC3339), expiresAt.UTC().Format(time.RFC3339)); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *SQLite) GetPending(ctx context.Context, actor string) (*model.Pending, error) {
	var p model.Pending
	var created, expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, open_id, payload_json, status, created_at, expires_at FROM pending_update WHERE open_id=? AND status='pending'`, actor).
		Scan(&p.ID, &p.Actor, &p.PayloadJSON, &p.Status, &created, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if t, e := time.Parse(time.RFC3339, expires); e == nil {
		// 过期则视为无待确认（§15.2 TTL）。用可注入时钟（默认 Real），测试可注入 Fake。
		if s.now().After(t) {
			_ = s.SetPendingStatus(ctx, p.ID, "expired")
			return nil, nil
		}
		p.ExpiresAt = &t
	}
	return &p, nil
}

func (s *SQLite) SetPendingStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_update SET status=? WHERE id=?`, status, id)
	return err
}

func (s *SQLite) WriteAudit(ctx context.Context, actor, action, target string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(id, actor, action, target, ts) VALUES(?,?,?,?,?)`,
		newID(), actor, action, target, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) UpsertAppConfig(ctx context.Context, key, valueJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_config(key, value_json, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		key, valueJSON, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListAppConfig(ctx context.Context) ([]model.AppConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json, updated_at FROM app_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AppConfig
	for rows.Next() {
		var c model.AppConfig
		var updated sql.NullString
		if err := rows.Scan(&c.Key, &c.ValueJSON, &updated); err != nil {
			return nil, err
		}
		if updated.Valid {
			c.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteAppConfig(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_config WHERE key=?`, key)
	return err
}

func (s *SQLite) CleanupBefore(ctx context.Context, cutoff time.Time, confirm bool) (model.CleanupResult, error) {
	cut := cutoff.UTC().Format(time.RFC3339Nano)
	count := func(query string) (int, error) {
		var n int
		err := s.db.QueryRowContext(ctx, query, cut).Scan(&n)
		return n, err
	}
	result := model.CleanupResult{}
	var err error
	if result.Messages, err = count(`SELECT COUNT(*) FROM message WHERE created_at < ?`); err != nil {
		return result, err
	}
	if result.AIEvidence, err = count(`SELECT COUNT(*) FROM ai_evidence WHERE occurred_at < ?`); err != nil {
		return result, err
	}
	if result.Reconciliations, err = count(`SELECT COUNT(*) FROM reconciliation WHERE computed_at < ?`); err != nil {
		return result, err
	}
	if !confirm {
		return result, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message WHERE created_at < ?`, cut); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_evidence WHERE occurred_at < ?`, cut); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reconciliation WHERE computed_at < ?`, cut); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *SQLite) RecordNotification(ctx context.Context, kind, ownerKey, targetKey, remindDate string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO notification_log(id, kind, owner_key, target_key, remind_date, created_at) VALUES(?,?,?,?,?,?)`,
		newID(), kind, ownerKey, targetKey, remindDate, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLite) UpsertSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) error {
	if ep.LastSeenAt.IsZero() {
		ep.LastSeenAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET
		   last_seen_at=excluded.last_seen_at,
		   active=excluded.active,
		   full_session_name=COALESCE(NULLIF(excluded.full_session_name,''), session_endpoint.full_session_name),
		   owner_key=COALESCE(NULLIF(excluded.owner_key,''), session_endpoint.owner_key),
		   client_ip=COALESCE(NULLIF(excluded.client_ip,''), session_endpoint.client_ip),
		   tool=COALESCE(NULLIF(excluded.tool,''), session_endpoint.tool),
		   model=COALESCE(NULLIF(excluded.model,''), session_endpoint.model),
		   producer=excluded.producer,
		   target_group=COALESCE(NULLIF(excluded.target_group,''), session_endpoint.target_group),
		   no_directory_reported=CASE WHEN excluded.active=0 THEN session_endpoint.no_directory_reported ELSE excluded.no_directory_reported END,
		   no_directory=CASE
		     WHEN excluded.active=0 THEN session_endpoint.no_directory
		     WHEN session_endpoint.no_directory_admin=1 OR excluded.no_directory_reported=1 THEN 1
		     ELSE 0
		   END,
		   mirror_enabled=CASE WHEN excluded.mirror_to<>'' THEN excluded.mirror_enabled ELSE session_endpoint.mirror_enabled END,
		   mirror_to=COALESCE(NULLIF(excluded.mirror_to,''), session_endpoint.mirror_to)`,
		ep.KeyID, ep.SessionName, ep.FullSessionName, ep.OwnerKey, ep.LastSeenAt.UTC().Format(time.RFC3339), boolInt(ep.Active), boolInt(ep.MirrorEnabled), ep.MirrorTo, ep.ClientIP, ep.Tool, ep.Model, boolInt(ep.Producer), ep.TargetGroup, boolInt(ep.NoDirectory), boolInt(ep.NoDirectoryAdmin), boolInt(ep.NoDirectory))
	return err
}

func (s *SQLite) ListSessionEndpoints(ctx context.Context) ([]model.SessionEndpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key_id, session_name, COALESCE(full_session_name, ''), COALESCE(owner_key, ''), last_seen_at, active, mirror_enabled, COALESCE(mirror_to, ''), COALESCE(client_ip, ''), COALESCE(tool, ''), COALESCE(model, ''), COALESCE(producer, 0), COALESCE(target_group, ''), COALESCE(no_directory, 0), COALESCE(no_directory_admin, 0)
		   FROM session_endpoint ORDER BY key_id, session_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SessionEndpoint
	for rows.Next() {
		var ep model.SessionEndpoint
		var lastSeen string
		var active, mirrorEnabled, producer, noDirectory, noDirectoryAdmin int
		if err := rows.Scan(&ep.KeyID, &ep.SessionName, &ep.FullSessionName, &ep.OwnerKey, &lastSeen, &active, &mirrorEnabled, &ep.MirrorTo, &ep.ClientIP, &ep.Tool, &ep.Model, &producer, &ep.TargetGroup, &noDirectory, &noDirectoryAdmin); err != nil {
			return nil, err
		}
		ep.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen)
		ep.Active = active == 1
		ep.MirrorEnabled = mirrorEnabled == 1
		ep.Producer = producer == 1
		ep.NoDirectory = noDirectory == 1
		ep.NoDirectoryAdmin = noDirectoryAdmin == 1
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *SQLite) SetSessionMirror(ctx context.Context, keyID, sessionName string, enabled bool, mirrorTo string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET mirror_enabled=excluded.mirror_enabled, mirror_to=excluded.mirror_to`,
		keyID, sessionName, "", "", time.Now().UTC().Format(time.RFC3339), 0, boolInt(enabled), mirrorTo, "", "", "", 0, "", 0, 0, 0)
	return err
}

func (s *SQLite) SetSessionNoDirectory(ctx context.Context, keyID, sessionName string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET
		   no_directory_admin=excluded.no_directory_admin,
		   no_directory=CASE WHEN excluded.no_directory_admin=1 OR session_endpoint.no_directory_reported=1 THEN 1 ELSE 0 END`,
		keyID, sessionName, "", "", time.Now().UTC().Format(time.RFC3339), 0, 0, "", "", "", "", 0, "", boolInt(enabled), boolInt(enabled), 0)
	return err
}

func (s *SQLite) UpsertRegisteredService(ctx context.Context, svc model.RegisteredService) error {
	if svc.ID == "" {
		svc.ID = newID()
	}
	if svc.DeliveryType == "" {
		svc.DeliveryType = "ws"
	}
	if svc.ReplyMode == "" {
		svc.ReplyMode = "sync"
	}
	if svc.Priority == 0 {
		svc.Priority = 100
	}
	if svc.TimeoutMs == 0 {
		svc.TimeoutMs = 5000
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registered_service(id, name, description, delivery_type, endpoint, secret_ref, reply_mode, priority, timeout_ms, retry, enabled, health_status, last_heartbeat)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, delivery_type=excluded.delivery_type, endpoint=excluded.endpoint, secret_ref=excluded.secret_ref, reply_mode=excluded.reply_mode, priority=excluded.priority, timeout_ms=excluded.timeout_ms, retry=excluded.retry, enabled=excluded.enabled, health_status=excluded.health_status, last_heartbeat=excluded.last_heartbeat`,
		svc.ID, svc.Name, svc.Description, svc.DeliveryType, svc.Endpoint, svc.SecretRef, svc.ReplyMode, svc.Priority, svc.TimeoutMs, svc.Retry, boolInt(svc.Enabled), svc.HealthStatus, timePtrString(svc.LastHeartbeatAt))
	return err
}

func (s *SQLite) ListRegisteredServices(ctx context.Context) ([]model.RegisteredService, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, delivery_type, endpoint, secret_ref, reply_mode, priority, timeout_ms, retry, enabled, health_status, last_heartbeat
		   FROM registered_service ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RegisteredService
	for rows.Next() {
		var svc model.RegisteredService
		var enabled int
		var last sql.NullString
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Description, &svc.DeliveryType, &svc.Endpoint, &svc.SecretRef, &svc.ReplyMode, &svc.Priority, &svc.TimeoutMs, &svc.Retry, &enabled, &svc.HealthStatus, &last); err != nil {
			return nil, err
		}
		svc.Enabled = enabled == 1
		if last.Valid {
			if t, err := time.Parse(time.RFC3339, last.String); err == nil {
				svc.LastHeartbeatAt = &t
			}
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteRegisteredService(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM registered_service WHERE id=?`, id)
	return err
}

func (s *SQLite) InsertServiceAPIKey(ctx context.Context, key model.ServiceAPIKey) error {
	if key.ID == "" {
		key.ID = newID()
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO service_api_key(id, service_id, key_hash, label, active, created_at, revoked_at) VALUES(?,?,?,?,?,?,?)`,
		key.ID, key.ServiceID, key.KeyHash, key.Label, boolInt(key.Active), key.CreatedAt.UTC().Format(time.RFC3339), timePtrString(key.RevokedAt))
	return err
}

func (s *SQLite) ListServiceAPIKeys(ctx context.Context, serviceID string) ([]model.ServiceAPIKey, error) {
	query := `SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key`
	args := []any{}
	if serviceID != "" {
		query += ` WHERE service_id=?`
		args = append(args, serviceID)
	}
	query += ` ORDER BY created_at DESC, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ServiceAPIKey
	for rows.Next() {
		var key model.ServiceAPIKey
		var created string
		var revoked sql.NullString
		var active int
		if err := rows.Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked); err != nil {
			return nil, err
		}
		key.Active = active == 1
		key.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if revoked.Valid {
			if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
				key.RevokedAt = &t
			}
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (s *SQLite) ResolveServiceAPIKey(ctx context.Context, keyHash string) (*model.ServiceAPIKey, error) {
	var key model.ServiceAPIKey
	var created string
	var revoked sql.NullString
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key WHERE key_hash=? AND active=1`,
		keyHash).Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key.Active = active == 1
	key.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if revoked.Valid {
		if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
			key.RevokedAt = &t
		}
	}
	return &key, nil
}

func (s *SQLite) ResolveServiceAPISecret(ctx context.Context, keyID, secretHash string) (*model.ServiceAPIKey, error) {
	var key model.ServiceAPIKey
	var created string
	var revoked sql.NullString
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key WHERE id=? AND key_hash=? AND active=1`,
		keyID, secretHash).Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key.Active = active == 1
	key.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if revoked.Valid {
		if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
			key.RevokedAt = &t
		}
	}
	return &key, nil
}

func (s *SQLite) RevokeServiceAPIKey(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE service_api_key SET active=0, revoked_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), keyID)
	return err
}

func (s *SQLite) BindAPIKeyAccount(ctx context.Context, keyID, chatEntityID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO api_key_account(api_key_id, chat_entity_id) VALUES(?,?)`,
		keyID, chatEntityID)
	return err
}

func (s *SQLite) ListAPIKeyAccounts(ctx context.Context, keyID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chat_entity_id FROM api_key_account WHERE api_key_id=? ORDER BY chat_entity_id`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStrings(rows)
}

func (s *SQLite) ListServiceBoundAccounts(ctx context.Context, serviceID string) ([]string, error) {
	if keyID, _, ok := sessionServiceParts(serviceID); ok {
		return s.ListAPIKeyAccounts(ctx, keyID)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT aka.chat_entity_id
		   FROM api_key_account aka
		   JOIN service_api_key k ON k.id=aka.api_key_id
		  WHERE k.service_id=? AND k.active=1
		  ORDER BY aka.chat_entity_id`,
		serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStrings(rows)
}

func sessionServiceParts(serviceID string) (keyID string, sessionName string, ok bool) {
	if !strings.HasPrefix(serviceID, "session:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(serviceID, "session:")
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *SQLite) InsertRoutingRule(ctx context.Context, rule model.RoutingRule) error {
	if rule.ID == "" {
		rule.ID = newID()
	}
	if rule.MatchType == "" {
		rule.MatchType = "prefix"
	}
	if rule.Combine == "" {
		rule.Combine = "or"
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if rule.AccountScopeJSON != "" {
		var scoped []string
		if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
			return err
		}
		bound, err := s.ListServiceBoundAccounts(ctx, rule.ServiceID)
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, id := range bound {
			allowed[id] = true
		}
		for _, id := range scoped {
			if !allowed[id] {
				return fmt.Errorf("account scope %s is not bound to service", id)
			}
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO routing_rule(id, service_id, match_type, match_expr, combine, priority, scope_entity_type, account_scope_json, case_sensitive, strip_prefix, enabled)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		rule.ID, rule.ServiceID, rule.MatchType, rule.MatchExpr, rule.Combine, rule.Priority, rule.ScopeEntityType, rule.AccountScopeJSON, boolInt(rule.CaseSensitive), boolInt(rule.StripPrefix), boolInt(rule.Enabled))
	return err
}

func (s *SQLite) DeleteRoutingRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM routing_rule WHERE id=?`, id)
	return err
}

func (s *SQLite) ListAllPrefixRoutes(ctx context.Context) ([]model.PrefixRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.service_id, r.match_type, r.match_expr, r.combine, r.priority, r.scope_entity_type, r.account_scope_json, r.case_sensitive, r.strip_prefix, r.enabled,
		        s.id, s.name, s.description, s.delivery_type, s.endpoint, s.secret_ref, s.reply_mode, s.priority, s.timeout_ms, s.retry, s.enabled, s.health_status, s.last_heartbeat
		   FROM routing_rule r
		   JOIN registered_service s ON s.id=r.service_id
		  WHERE r.enabled=1 AND s.enabled=1 AND r.match_type='prefix'
		  ORDER BY r.priority ASC, r.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []model.PrefixRoute
	for rows.Next() {
		var route model.PrefixRoute
		var rCase, rStrip, rEnabled, sEnabled int
		var lastHeartbeat sql.NullString
		if err := rows.Scan(
			&route.Rule.ID, &route.Rule.ServiceID, &route.Rule.MatchType, &route.Rule.MatchExpr, &route.Rule.Combine, &route.Rule.Priority, &route.Rule.ScopeEntityType, &route.Rule.AccountScopeJSON, &rCase, &rStrip, &rEnabled,
			&route.Service.ID, &route.Service.Name, &route.Service.Description, &route.Service.DeliveryType, &route.Service.Endpoint, &route.Service.SecretRef, &route.Service.ReplyMode, &route.Service.Priority, &route.Service.TimeoutMs, &route.Service.Retry, &sEnabled, &route.Service.HealthStatus, &lastHeartbeat,
		); err != nil {
			return nil, err
		}
		route.Rule.CaseSensitive = rCase == 1
		route.Rule.StripPrefix = rStrip == 1
		route.Rule.Enabled = rEnabled == 1
		route.Service.Enabled = sEnabled == 1
		if lastHeartbeat.Valid {
			if t, err := time.Parse(time.RFC3339, lastHeartbeat.String); err == nil {
				route.Service.LastHeartbeatAt = &t
			}
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *SQLite) ListPrefixRoutes(ctx context.Context, chatEntityID string) ([]model.PrefixRoute, error) {
	routes, err := s.ListAllPrefixRoutes(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.PrefixRoute
	boundCache := map[string][]string{}
	for _, route := range routes {
		bound, ok := boundCache[route.Service.ID]
		if !ok {
			var err error
			bound, err = s.ListServiceBoundAccounts(ctx, route.Service.ID)
			if err != nil {
				return nil, err
			}
			boundCache[route.Service.ID] = bound
		}
		if routeAllowedForEntity(route.Rule, chatEntityID, bound) {
			out = append(out, route)
		}
	}
	return out, nil
}

func routeAllowedForEntity(rule model.RoutingRule, chatEntityID string, bound []string) bool {
	boundSet := map[string]bool{}
	for _, id := range bound {
		boundSet[id] = true
	}
	if rule.AccountScopeJSON == "" {
		return boundSet[chatEntityID]
	}
	var scoped []string
	if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
		return false
	}
	for _, id := range scoped {
		if id == chatEntityID && boundSet[id] {
			return true
		}
	}
	return false
}

func (s *SQLite) UpsertSystemService(ctx context.Context, svc model.SystemService) error {
	if svc.Delivery == "" {
		svc.Delivery = "internal"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_service(name, description, delivery, endpoint, active)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET description=excluded.description, delivery=excluded.delivery, endpoint=excluded.endpoint, active=excluded.active`,
		svc.Name, svc.Description, svc.Delivery, svc.Endpoint, boolInt(svc.Active))
	return err
}

func (s *SQLite) UpsertSystemRoute(ctx context.Context, route model.SystemRoute) error {
	if route.Priority == 0 {
		route.Priority = 10
	}
	if route.Action == "" {
		route.Action = "auto"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_route(keyword, service_name, action, priority, active)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(keyword) DO UPDATE SET service_name=excluded.service_name, action=excluded.action, priority=excluded.priority, active=excluded.active`,
		route.Keyword, route.ServiceName, route.Action, route.Priority, boolInt(route.Active))
	return err
}

func (s *SQLite) DeleteSystemRoute(ctx context.Context, keyword string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM system_route WHERE keyword=?`, keyword)
	return err
}

func (s *SQLite) ListSystemRoutes(ctx context.Context) ([]model.SystemRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT keyword, service_name, action, priority, active FROM system_route WHERE active=1 ORDER BY priority ASC, keyword ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SystemRoute
	for rows.Next() {
		var route model.SystemRoute
		var active int
		if err := rows.Scan(&route.Keyword, &route.ServiceName, &route.Action, &route.Priority, &active); err != nil {
			return nil, err
		}
		if route.Action == "" {
			route.Action = "auto"
		}
		route.Active = active == 1
		out = append(out, route)
	}
	return out, rows.Err()
}

func (s *SQLite) GetSystemService(ctx context.Context, name string) (*model.SystemService, error) {
	var svc model.SystemService
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT name, description, delivery, endpoint, active FROM system_service WHERE name=? AND active=1`, name).
		Scan(&svc.Name, &svc.Description, &svc.Delivery, &svc.Endpoint, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	svc.Active = active == 1
	return &svc, nil
}

func timePtrString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
