// Package store 是数据访问层（Repository 抽象）。
//
// 规范 §14.1：Repository 接口 → SQLite/PG 可换；本期实现为 SQLite（modernc.org/sqlite，纯 Go 无 cgo）。
// 容量基线 ≤100 人 / ≤200 写每秒，SQLite WAL 足够（§13.2）。
package store

import (
	"context"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Repository 数据访问抽象（便于替换为 PG / 便于测试 fake）。
type Repository interface {
	Migrate(ctx context.Context) error
	Close() error

	// 后台管理员（M9 登录）
	AdminCount(ctx context.Context) (int, error)
	CreateAdmin(ctx context.Context, u model.AdminUser) error
	GetAdmin(ctx context.Context, username string) (*model.AdminUser, error)

	// 会话主体（M0）
	UpsertChatEntity(ctx context.Context, e model.ChatEntity) error
	GetChatEntity(ctx context.Context, botChannelID, feishuID string) (*model.ChatEntity, error)
	ListChatEntities(ctx context.Context, typ model.ChatType) ([]model.ChatEntity, error)
	UpsertBotChannel(ctx context.Context, c model.BotChannel) error
	ListBotChannels(ctx context.Context) ([]model.BotChannel, error)
	DeleteBotChannel(ctx context.Context, id string) error
	UpsertSeenPerson(ctx context.Context, p model.SeenPerson) error
	ListSeenPersons(ctx context.Context) ([]model.SeenPerson, error)
	UpsertSeenPersonGroup(ctx context.Context, p model.SeenPersonGroup) error
	ListSeenPersonsByGroup(ctx context.Context, groupChatID string) ([]model.SeenPersonGroup, error)

	// 项目组 / 版本化排期（R14）
	UpsertProject(ctx context.Context, p model.Project) error
	GetProject(ctx context.Context, id string) (*model.Project, error)
	GetProjectByGroupChat(ctx context.Context, chatID string) (*model.Project, error)
	ListProjects(ctx context.Context, activeOnly bool) ([]model.Project, error)
	AppendScheduleDoc(ctx context.Context, d model.ScheduleDoc) (model.ScheduleDoc, error)
	LatestScheduleDoc(ctx context.Context, projectID, kind, ownerKey string) (*model.ScheduleDoc, error)
	ListScheduleDocVersions(ctx context.Context, projectID, kind, ownerKey string) ([]model.ScheduleDoc, error)
	AssignProjectMember(ctx context.Context, projectID, ownerKey string) error
	UnassignProjectMember(ctx context.Context, projectID, ownerKey string) error
	ListProjectMembers(ctx context.Context, projectID string) ([]model.Member, error)
	SetProjectAggregateSources(ctx context.Context, aggregateProjectID string, sourceProjectIDs []string) error
	ListProjectAggregateSources(ctx context.Context, aggregateProjectID string) ([]model.Project, error)
	ListAggregateProjectIDs(ctx context.Context) ([]string, error)
	UpsertProjectWeeklyReport(ctx context.Context, r model.ProjectWeeklyReport) error
	GetProjectWeeklyReport(ctx context.Context, projectID, week string) (*model.ProjectWeeklyReport, error)

	// 消息总线（M0）
	EnqueueMessage(ctx context.Context, m model.Message) error
	ClaimNextMessage(ctx context.Context, direction model.Direction) (*model.Message, error)
	AckMessage(ctx context.Context, id string) error
	FailMessage(ctx context.Context, id, reason string) error
	RecoverProcessing(ctx context.Context) (int64, error)
	RecentMessages(ctx context.Context, filter model.MessageFilter) ([]model.Message, error)
	DeleteCollectMessagesBefore(ctx context.Context, cutoff time.Time) (int64, error)
	MessageStats(ctx context.Context) (model.MessageStats, error)

	// 平台总控 L0/L1（P1）
	EnqueueControlTask(ctx context.Context, task model.ControlTask) (model.ControlTask, bool, error)
	GetControlTask(ctx context.Context, id string) (*model.ControlTask, error)
	UpdateControlTaskAfterL1(ctx context.Context, id, intent, layer, target, result, status, errText string) error
	RetryControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error)
	ReapExpiredControlTasks(ctx context.Context, now time.Time) ([]model.ControlTask, error)
	ClaimNextL2ControlTask(ctx context.Context, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error)
	RetryL2ControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error)
	CompleteControlTaskL2(ctx context.Context, id, intent, target, result string, duration time.Duration) error
	RecordControlTaskL2Failure(ctx context.Context, id, errText string, duration time.Duration) error
	CreateControlSubtasks(ctx context.Context, parent model.ControlTask, children []model.ControlTask) error
	CompleteControlSubtask(ctx context.Context, id, result, status, errText string) (*model.ControlTask, error)
	ListControlSubtasks(ctx context.Context, parentID string) ([]model.ControlTask, error)
	TryClaimControlParentForAggregation(ctx context.Context, parentID, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error)
	ClaimNextAggregateControlTask(ctx context.Context, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error)
	RetryAggregatingControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error)
	ControlTaskStats(ctx context.Context) (model.ControlTaskStats, error)
	ListRecentControlTaskL2Metrics(ctx context.Context, limit int) ([]model.ControlTaskL2Metric, error)
	ListL1DecisionRules(ctx context.Context) ([]model.L1DecisionRule, error)

	// 排期（M1）
	ListSchedules(ctx context.Context, ownerKey string) ([]model.Schedule, error)
	UpsertSchedule(ctx context.Context, s model.Schedule) error
	DeleteScheduleByID(ctx context.Context, id string) error

	// 成员 / 权限（M2/M5/M6 基础）
	UpsertMember(ctx context.Context, m model.Member) error
	GetMemberByOwnerKey(ctx context.Context, ownerKey string) (*model.Member, error)
	ListMembers(ctx context.Context) ([]model.Member, error)

	// 进度（M2）
	AddProgress(ctx context.Context, p model.Progress) error
	LatestProgress(ctx context.Context, ownerKey string) ([]model.Progress, error)
	ListProgressBetween(ctx context.Context, ownerKey string, from, to time.Time) ([]model.Progress, error)

	// 风险（M6）
	ReportRisk(ctx context.Context, r model.Risk) error
	ResolveRisks(ctx context.Context, ownerKey, keyword string) (int, error)
	ListOpenRisks(ctx context.Context, ownerKey string) ([]model.Risk, error)

	// AI 佐证 / 对账（M4）
	AddAIEvidence(ctx context.Context, e model.AIEvidence) error
	ListAIEvidence(ctx context.Context, ownerKey, taskKey string) ([]model.AIEvidence, error)
	ListAIEvidenceBetween(ctx context.Context, from, to time.Time) ([]model.AIEvidence, error)
	MapAIEvidenceToTask(ctx context.Context, evidenceID, ownerKey, taskKey string) error
	UpsertReconciliation(ctx context.Context, r model.Reconciliation) error
	ListReconciliation(ctx context.Context, ownerKey string) ([]model.Reconciliation, error)

	// 待确认变更（两步式写，§15.2）：同一 actor 同时只一个 pending
	PutPending(ctx context.Context, actor, payloadJSON string, expiresAt time.Time) (string, error)
	GetPending(ctx context.Context, actor string) (*model.Pending, error)
	SetPendingStatus(ctx context.Context, id, status string) error

	// 审计
	WriteAudit(ctx context.Context, actor, action, target string) error
	UpsertAppConfig(ctx context.Context, key, valueJSON string) error
	ListAppConfig(ctx context.Context) ([]model.AppConfig, error)
	DeleteAppConfig(ctx context.Context, key string) error
	CleanupBefore(ctx context.Context, cutoff time.Time, confirm bool) (model.CleanupResult, error)
	UpsertSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) error
	ListSessionEndpoints(ctx context.Context) ([]model.SessionEndpoint, error)
	SetSessionMirror(ctx context.Context, keyID, sessionName string, enabled bool, mirrorTo string) error
	SetSessionNoDirectory(ctx context.Context, keyID, sessionName string, enabled bool) error

	// 通知幂等（M7）
	RecordNotification(ctx context.Context, kind, ownerKey, targetKey, remindDate string) (bool, error)

	// M8 注册服务 / API key / 路由
	UpsertRegisteredService(ctx context.Context, s model.RegisteredService) error
	ListRegisteredServices(ctx context.Context) ([]model.RegisteredService, error)
	DeleteRegisteredService(ctx context.Context, id string) error
	InsertServiceAPIKey(ctx context.Context, key model.ServiceAPIKey) error
	ListServiceAPIKeys(ctx context.Context, serviceID string) ([]model.ServiceAPIKey, error)
	ResolveServiceAPIKey(ctx context.Context, keyHash string) (*model.ServiceAPIKey, error)
	ResolveServiceAPISecret(ctx context.Context, keyID, secretHash string) (*model.ServiceAPIKey, error)
	RevokeServiceAPIKey(ctx context.Context, keyID string) error
	BindAPIKeyAccount(ctx context.Context, keyID, chatEntityID string) error
	ListAPIKeyAccounts(ctx context.Context, keyID string) ([]string, error)
	ListServiceBoundAccounts(ctx context.Context, serviceID string) ([]string, error)
	InsertRoutingRule(ctx context.Context, rule model.RoutingRule) error
	DeleteRoutingRule(ctx context.Context, id string) error
	ListAllPrefixRoutes(ctx context.Context) ([]model.PrefixRoute, error)
	ListPrefixRoutes(ctx context.Context, chatEntityID string) ([]model.PrefixRoute, error)
	UpsertSystemService(ctx context.Context, svc model.SystemService) error
	UpsertSystemRoute(ctx context.Context, route model.SystemRoute) error
	DeleteSystemRoute(ctx context.Context, keyword string) error
	ListSystemRoutes(ctx context.Context) ([]model.SystemRoute, error)
	GetSystemService(ctx context.Context, name string) (*model.SystemService, error)
}
