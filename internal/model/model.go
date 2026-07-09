// Package model 定义 WorkPulse 的核心领域类型（语言无关的数据结构）。
package model

import "time"

// ChatType 会话主体类型：个人账号 或 群。
type ChatType string

const (
	ChatPersonal ChatType = "personal"
	ChatGroup    ChatType = "group"
)

// Direction 消息方向。
type Direction string

const (
	DirectionIn      Direction = "in"
	DirectionOut     Direction = "out"
	DirectionCollect Direction = "collect"
)

// Role 成员角色。
type Role string

const (
	RoleMember       Role = "member"
	RoleCollaborator Role = "collaborator"
	RoleManager      Role = "manager"
	RoleSystem       Role = "system"
)

// BotChannel 机器人管道（一个飞书应用，可多个，均可收发）。
type BotChannel struct {
	ID                   string
	Name                 string
	AppID                string
	AppSecretEnc         string
	AppSecretSet         bool
	VerificationToken    string
	VerificationTokenSet bool
	EncryptKey           string
	EncryptKeySet        bool
	Purpose              string // dm | group | general
	CanSend              bool
	CanReceive           bool
	Active               bool
}

// ChatEntity 会话主体（队列分区键）：记录 渠道+id+名称+类型。
type ChatEntity struct {
	ID           string
	BotChannelID string
	Type         ChatType
	FeishuID     string // 个人=open_id / 群=chat_id
	DisplayName  string
	BoundOwner   string // 个人主体可绑定 member.owner_key
	Active       bool
}

// Message 收发统一消息（完整内容直接入库）。
type Message struct {
	ID           string
	ChatEntityID string
	Direction    Direction
	BotChannelID string
	FeishuMsgID  string // 幂等去重
	ChatType     ChatType
	SenderOpenID string // 群消息=发言人
	Content      string // content_json
	Status       string
	Attempts     int
	Err          string
	CreatedAt    time.Time
	ProcessedAt  *time.Time
}

// MessageFilter 最近消息只读筛选条件。
type MessageFilter struct {
	BotChannelID string
	ChatEntityID string
	Limit        int
}

// MessageStats 运行状态页消息队列概况。
type MessageStats struct {
	Total      int
	Queued     int
	Processing int
	Done       int
	Dead       int
}

// ControlTask is the platform control-plane L0 queue item.
type ControlTask struct {
	ID           string
	ParentID     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Source       string
	SourceAddr   string
	OwnerKey     string
	BotChannelID string
	RawInput     string
	Intent       string
	Layer        string
	Target       string
	Result       string
	Status       string
	Priority     int
	Attempts     int
	MaxAttempts  int
	Error        string
	LeaseOwner   string
	LeaseUntil   *time.Time
	ExpireAt     *time.Time
}

// L1DecisionRule is a data-driven, ordered L1 rule row.
type L1DecisionRule struct {
	ID          string
	Seq         int
	MatchType   string
	Pattern     string
	Intent      string
	Action      string
	ExitQueue   bool
	Enabled     bool
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ControlTaskStats summarizes the control-plane queue for observability.
type ControlTaskStats struct {
	Total         int
	Depth         int
	Status        map[string]int
	FailedRate    float64
	ExpiredRate   float64
	L2InFlight    int
	L2FailureRate float64
	L2P50MS       int64
	L2P95MS       int64
}

// ControlTaskL2Metric is one recorded L2 processing attempt for observability.
type ControlTaskL2Metric struct {
	TaskID     string
	DurationMS int64
	Success    bool
	Error      string
	CreatedAt  time.Time
}

// L2TriageContext is the structured input sent to the dispatch LLM.
type L2TriageContext struct {
	RequestID      string            `json:"request_id"`
	RawInput       string            `json:"raw_input"`
	Source         string            `json:"source"`
	OwnerKey       string            `json:"owner_key"`
	OnlineSessions []L2OnlineSession `json:"online_sessions"`
	RecentContext  string            `json:"recent_context,omitempty"`
}

type L2OnlineSession struct {
	Session string `json:"session"`
	Tool    string `json:"tool"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Busy    bool   `json:"busy"`
}

// L2TriageResult is the constrained JSON result from the dispatch LLM.
type L2TriageResult struct {
	Intent     string     `json:"intent"`
	Reply      string     `json:"reply"`
	Targets    []L2Target `json:"targets"`
	Subtasks   []L2Target `json:"subtasks"`
	Confidence float64    `json:"confidence"`
}

type L2Target struct {
	Session     string `json:"session"`
	Instruction string `json:"instruction"`
}

// AppConfig 是 M9 管理的运行时配置键值。
type AppConfig struct {
	Key       string
	ValueJSON string
	UpdatedAt time.Time
}

// AuditLog is one admin/system audit entry.
type AuditLog struct {
	ID     string
	Actor  string
	Action string
	Target string
	TS     time.Time
}

// SessionEndpoint 是统一寻址协议中的会话端点元数据。
type SessionEndpoint struct {
	KeyID            string
	SessionName      string
	FullSessionName  string
	OwnerKey         string
	LastSeenAt       time.Time
	Active           bool
	MirrorEnabled    bool
	MirrorTo         string
	ClientIP         string
	Tool             string
	Model            string
	Producer         bool
	TargetGroup      string
	NoDirectory      bool
	NoDirectoryAdmin bool
}

// CleanupResult 是数据清理 dry-run/confirm 的统计。
type CleanupResult struct {
	Messages        int
	AIEvidence      int
	Reconciliations int
}

// Member 排期成员。
type Member struct {
	ID             string
	OwnerKey       string
	DisplayName    string
	FeishuOpenID   string
	Role           Role
	EvidenceOptOut bool
	DMOptOut       bool
	Active         bool
}

// SeenPerson 是 M9 成员候选池中已采集到的飞书用户。
type SeenPerson struct {
	OpenID       string
	BotChannelID string
	Name         string
	Source       string // group|inbound
	LastSeenAt   time.Time
	IsMember     bool
}

// Project is a WorkPulse schedule project/team.
type Project struct {
	ID                string
	Name              string
	ParentID          string
	OwnerKey          string
	ProductManagerKey string
	NotifyChatID      string
	NotifyBotID       string
	TranscriptDirs    string
	EvidenceCron      string
	EvidenceTZ        string
	Active            bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ProjectMember links an owner_key to a schedule project.
type ProjectMember struct {
	ProjectID string
	OwnerKey  string
}

// ProjectAggregateSource links an aggregate project to projects included in its summary.
type ProjectAggregateSource struct {
	AggregateProjectID string
	SourceProjectID    string
	CreatedAt          time.Time
}

// ScheduleDoc is the versioned Markdown truth for team and personal schedules.
type ScheduleDoc struct {
	ID        string
	ProjectID string
	Kind      string // team|personal
	OwnerKey  string
	Version   int
	Content   string
	Source    string
	CreatedBy string
	CreatedAt time.Time
}

// SeenPersonGroup records which group a person has been observed in.
type SeenPersonGroup struct {
	OpenID       string
	BotChannelID string
	GroupChatID  string
	GroupName    string
	LastSeenAt   time.Time
}

// AdminUser 后台管理员（密码哈希，不存明文）。
type AdminUser struct {
	ID           string
	Username     string
	PasswordHash string
	Active       bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
}

// Pending 待确认变更（两步式写：diff 预览 → 确认/取消，规范 §15.2）。
type Pending struct {
	ID          string
	Actor       string // 发起人键（M1 用 owner_key）
	PayloadJSON string // 序列化的待应用变更集
	Status      string // pending|confirmed|cancelled|expired
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

// Schedule 排期条目。
type Schedule struct {
	ID        string
	OwnerKey  string
	StartDate string // YYYY-MM-DD
	EndDate   string
	Task      string
	Status    string // planned|in_progress|done|delayed|canceled
	Priority  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Progress 进度留痕。历史追加保存，展示取同 owner/task 最新一条。
type Progress struct {
	ID         string
	OwnerKey   string
	TaskKey    string
	Note       string
	Percent    int
	ReportedAt time.Time
	Source     string
}

// ProjectWeeklyReport 是项目周报产物；week 使用 UTC 周一日期 YYYY-MM-DD。
type ProjectWeeklyReport struct {
	ID          string
	ProjectID   string
	Week        string
	Content     string
	Status      string // final|draft|approved|vetoed|published
	ApprovedAt  *time.Time
	VetoedAt    *time.Time
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Risk 风险记录。相同内容的 open 风险会归集到同一条。
type Risk struct {
	ID             string
	OwnerKey       string
	Content        string
	Status         string
	RelatedTaskKey string
	ReportersJSON  string
	CreatedAt      time.Time
	ResolvedAt     *time.Time
}

// AIEvidence 是从 AI 会话 transcript 抽取出的工作摘要；不保存原始 transcript。
type AIEvidence struct {
	ID             string
	OwnerKey       string
	SessionID      string
	SessionSource  string
	WorkItem       string
	Artifact       string
	FilesJSON      string
	ActionType     string
	OccurredAt     time.Time
	MappedTaskKey  string
	Confidence     float64
	RawExcerptHash string
}

// Reconciliation 是自报进度与 AI 证据的对账结果。
type Reconciliation struct {
	ID            string
	OwnerKey      string
	TaskKey       string
	SelfStatus    string
	EvidenceCount int
	Verdict       string
	ComputedAt    time.Time
	DetailJSON    string
}

// RegisteredService M8 注册方服务。
type RegisteredService struct {
	ID              string
	Name            string
	Description     string
	DeliveryType    string
	Endpoint        string
	SecretRef       string
	ReplyMode       string
	Priority        int
	TimeoutMs       int
	Retry           int
	Enabled         bool
	HealthStatus    string
	LastHeartbeatAt *time.Time
}

// RoutingRule M8 路由规则，支持 prefix 通配符与同作用域覆盖检测。
type RoutingRule struct {
	ID               string
	ServiceID        string
	MatchType        string
	MatchExpr        string
	Combine          string
	Priority         int
	ScopeEntityType  string
	AccountScopeJSON string
	CaseSensitive    bool
	StripPrefix      bool
	Enabled          bool
}

// ServiceAPIKey 注册方 API key 元数据。ID 是公开 key_id；KeyHash 是私密 secret 的哈希。
type ServiceAPIKey struct {
	ID        string
	ServiceID string
	KeyHash   string
	Label     string
	Active    bool
	CreatedAt time.Time
	RevokedAt *time.Time
}

// PrefixRoute 是一条可执行的 prefix 路由视图。
type PrefixRoute struct {
	Rule    RoutingRule
	Service RegisteredService
}

// PrefixDispatchResult 是 M8 prefix 路由转发结果。
type PrefixDispatchResult struct {
	Matched bool
	Reply   string
}

// SystemService 是全局系统级服务，不归属任何租户 key_id。
type SystemService struct {
	Name        string
	Description string
	Delivery    string
	Endpoint    string
	Active      bool
}

// SystemRoute 是全局系统关键词到系统服务的映射。
type SystemRoute struct {
	Keyword     string
	ServiceName string
	Action      string // record|coordinate|auto
	Priority    int
	Active      bool
}

// Envelope 是 M8/M10 统一寻址协议的 To/From 信封。
type Envelope struct {
	ID   string         `json:"id"`
	To   string         `json:"to"`
	From string         `json:"from"`
	Body string         `json:"body"`
	TS   int64          `json:"ts"`
	Meta map[string]any `json:"meta,omitempty"`
}
