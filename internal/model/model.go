// Package model 定义 WorkPulse 的核心领域类型（语言无关的数据结构）。
package model

import (
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"time"
)

// ChatType 会话主体类型：个人账号 或 群。
type ChatType string

const (
	ChatPersonal ChatType = "personal"
	ChatGroup    ChatType = "group"
)

// IngressProvenance records which ingress boundary authenticated an inbound
// message. Empty, unknown and pre-migration values are always untrusted.
type IngressProvenance string

const (
	IngressUntrusted             IngressProvenance = "untrusted"
	IngressWebhookVerified       IngressProvenance = "webhook_verified"
	IngressFeishuWSAuthenticated IngressProvenance = "feishu_ws_authenticated"
)

func NormalizeIngressProvenance(value IngressProvenance) IngressProvenance {
	switch value {
	case IngressWebhookVerified, IngressFeishuWSAuthenticated:
		return value
	default:
		return IngressUntrusted
	}
}

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
	// IngressProvenance is assigned by the authenticated ingress adapter, never
	// decoded from message content. Direct/internal enqueue defaults untrusted.
	IngressProvenance IngressProvenance
	// Assigned only by authenticated SDK ingress. Never decoded from ordinary
	// client JSON, never displayed or logged. FCS must verify its own service
	// attestation before it may preserve this metadata; a provenance string alone
	// is insufficient. SQLite stores the snapshot across worker restarts.
	FeishuReply *FeishuReplyMetadata `json:"-"`
	Content     string               // content_json
	// SensitiveContent is an in-memory-only delivery override. It is never stored.
	SensitiveContent string
	Status           string
	Attempts         int
	Err              string
	CreatedAt        time.Time
	ProcessedAt      *time.Time
}

type FeishuReplyMetadata struct {
	Authenticated bool   `json:"authenticated"`
	ContentSHA    string `json:"content_sha256"`
	EventID       string `json:"event_id"`
	MessageID     string `json:"message_id"`
	ParentID      string `json:"parent_id"`
	RootID        string `json:"root_id"`
	MessageType   string `json:"message_type"`
	Tenant        string `json:"tenant"`
	SenderTenant  string `json:"sender_tenant,omitempty"`
	AppID         string `json:"app_id"`
	ChatID        string `json:"chat_id"`
	SenderType    string `json:"sender_type"`
}

// SessionDelivery is a durable Hub-to-session delivery record.
type SessionDelivery struct {
	SubRoute        *subagent.DeliveryRoute // Hub-only, persisted atomically with the delivery.
	ID              string
	KeyID           string
	SessionName     string
	Envelope        string
	SourceMessageID string
	SourceType      string
	// Provenance is the Hub-attested display provenance JSON. Empty means the
	// historical/unknown case and must remain fail-closed.
	Provenance    string
	State         string
	Attempts      int
	NextAttemptAt time.Time
	LeaseUntil    time.Time
	LastError     string
	CreatedAt     time.Time
	DeliveredAt   *time.Time
	ProcessedAt   *time.Time
	// AckProtocol/ProcessingMode/AckGeneration are bound once at first dispatch.
	// Zero/empty denotes a historical, unknown semantic, never a v2 receipt.
	AckProtocol    int
	ProcessingMode string
	AckGeneration  int
	InjectedAt     *time.Time
	TurnID         string
	TurnOutcome    string
}

// SessionDeliveryACK is one immutable lifecycle observation, not permission to
// execute a task. Generation is logical delivery identity, not a WS retry count.
type SessionDeliveryACK struct {
	DeliveryID     string
	KeyID          string
	SessionName    string
	Protocol       int
	ProcessingMode string
	Generation     int
	Phase          string
	FailureReason  string
	Retryable      bool
	OccurredAt     time.Time
	TurnID         string
	TurnOutcome    string
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
	PrivateClientIP  bool `json:"-"` // Write-only admission intent; never a directory field.
	Tool             string
	Model            string
	Version          string
	Producer         bool
	TargetGroup      string
	NoDirectory      bool
	NoDirectoryAdmin bool
	ConnSeq          int
	DeliveryACKV1    bool
}

// AgentGroup is one project grouping capability. Code is the 32-character
// base64 capability shared by owners; every group has exactly one leader.
type AgentGroup struct {
	Code                string    `json:"code"`
	ShortName           string    `json:"short_name"`
	CreatorPrincipal    string    `json:"creator_principal"`
	AdditionsClosed     bool      `json:"additions_closed"`
	LeaderKeyID         string    `json:"leader_key_id"`
	LeaderSessionName   string    `json:"leader_session_name"`
	LeaderOwnerKey      string    `json:"leader_owner_key"`
	LeaderConsoleMapped bool      `json:"leader_console_mapped"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AgentGroupMember links one Agent endpoint to one or more explicit groups.
// No rows means the Agent belongs to its owner's synthetic default group.
type AgentGroupMember struct {
	AgentID     string    `json:"agent_id"`
	Code        string    `json:"code"`
	KeyID       string    `json:"key_id"`
	SessionName string    `json:"session_name"`
	OwnerKey    string    `json:"owner_key"`
	CreatedAt   time.Time `json:"created_at"`
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

// ConsolePrincipal is an authenticated human identity. ProviderSubject is
// meaningful only inside ProviderIssuer; neither owner_key nor API key scopes
// are identity boundaries for the unified console.
type ConsolePrincipal struct {
	ID              string
	ShortName       string // Filled only by ListActiveConsolePrincipals; all other loaders leave it empty.
	Provider        string
	ProviderIssuer  string
	ProviderSubject string
	DisplayName     string
	Active          bool
}

// ConsoleAgent is the authorization target independently of shared key_id.
type ConsoleAgent struct {
	ID             string `json:"id"`
	KeyID          string `json:"key_id"`
	SessionName    string `json:"session_name"`
	OwnerPrincipal string `json:"owner_principal"`
	Active         bool   `json:"active"`
}

type ConsoleAgentGrant struct {
	PrincipalID string
	AgentID     string
	Permission  string // view | interact
	Active      bool
}

// ConsolePrincipalRole grants a human principal a role inside one explicit
// data-owner scope. It is seeded policy, never inferred from a shared key or
// display name.
type ConsolePrincipalRole struct {
	PrincipalID string
	Role        string // currently: owner
	OwnerKey    string
	Active      bool
}

// ConsolePlatformRole controls platform-wide work management. It is separate
// from ConsoleAgentGrant: one governs management actions, the other governs a
// principal's access to one specific Agent.
type ConsolePlatformRole struct {
	PrincipalID string
	Role        string // management | owner
	Active      bool
}

type ConsolePlatformItem struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Href        string    `json:"href"`
	Target      string    `json:"target,omitempty"` // 逐项:独立文档入口 _blank;页内锚(#agents)留空。空=同标签页,与既有行为逐字相同
	Status      string    `json:"status"`           // pending | approved
	SubmittedBy string    `json:"submitted_by,omitempty"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type ConsoleSpec struct {
	ID                 string    `json:"id"`
	PrincipalID        string    `json:"-"`
	OwnerShortName     string    `json:"owner_short_name"`
	Title              string    `json:"title"`
	State              string    `json:"state"`
	CurrentRevision    int       `json:"current_revision"`
	CurrentEditorID    string    `json:"-"`
	CurrentEditorShort string    `json:"current_editor_short_name"`
	Archived           bool      `json:"archived"`
	TaskState          string    `json:"task_state"`
	CanEdit            bool      `json:"can_edit"`
	CanSplit           bool      `json:"can_split"`
	CanDelete          bool      `json:"can_delete"`
	EditCapabilityHash string    `json:"-"`
	EditingClosed      bool      `json:"editing_closed"`
	DerivedFrom        string    `json:"derived_from,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ConsoleSpecRevision struct {
	SpecID           string    `json:"spec_id"`
	Revision         int       `json:"revision"`
	ContentMarkdown  string    `json:"content_markdown"`
	AcceptanceJSON   string    `json:"acceptance_json"`
	InputAnchorsJSON string    `json:"input_anchors_json"`
	SourceRefsJSON   string    `json:"source_refs_json,omitempty"`
	NormAnchorsJSON  string    `json:"norm_anchors_json,omitempty"`
	ContentSHA256    string    `json:"content_sha256"`
	CreatedBy        string    `json:"-"`
	CreatedByKind    string    `json:"created_by_kind"`
	CreatedVia       string    `json:"created_via"`
	Supersedes       int       `json:"supersedes_revision,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type ConsoleSpecAttribution struct {
	Creator      string    `json:"creator"`
	CreatorKind  string    `json:"creator_kind"`
	CreatedAt    time.Time `json:"created_at"`
	LastEditor   string    `json:"last_editor"`
	LastEditedAt time.Time `json:"last_edited_at"`
	Participants []string  `json:"participants"`
}

type ConsoleSpecReview struct {
	ID          string    `json:"review_id"`
	SpecID      string    `json:"spec_id"`
	Revision    int       `json:"revision"`
	Stage       string    `json:"stage"`
	Verdict     string    `json:"verdict"`
	ReasonCodes string    `json:"reason_codes_json,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	ReviewedBy  string    `json:"reviewed_by"`
	ReviewedAt  time.Time `json:"reviewed_at"`
}

// ConsoleSpecGateReview is the persisted pre-finalization AI review. Issues are
// kept as structured JSON so the blocked text, final response and false-positive
// outcome remain available for the three-version calibration requested by owner.
type ConsoleSpecGateReview struct {
	ID                string    `json:"review_id"`
	SpecID            string    `json:"spec_id"`
	Revision          int       `json:"revision"`
	ReviewAgentID     string    `json:"review_agent_id"`
	OperatorPrincipal string    `json:"operator_principal"`
	IssuesJSON        string    `json:"issues_json"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ConsoleSpecEvent struct {
	ID       string    `json:"id"`
	SpecID   string    `json:"spec_id"`
	Revision int       `json:"revision"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Detail   string    `json:"detail,omitempty"`
	At       time.Time `json:"at"`
}

type ConsoleTaskProposal struct {
	AssignmentNotificationPending bool       `json:"assignment_notification_pending,omitempty"`
	GroupCode                     string     `json:"group_code,omitempty"`
	Version                       int        `json:"version,omitempty"`
	NeedsReassignment             bool       `json:"needs_reassignment,omitempty"`
	ID                            string     `json:"proposal_id"`
	SchemaVersion                 int        `json:"schema_version"`
	SpecID                        string     `json:"spec_id"`
	Revision                      int        `json:"revision"`
	ContentSHA256                 string     `json:"content_sha256"`
	Title                         string     `json:"title"`
	TaskContent                   string     `json:"content"`
	Status                        string     `json:"status"`
	EstimatedHours                float64    `json:"estimated_hours"`
	DependsOnJSON                 string     `json:"depends_on_json"`
	DependedBy                    []string   `json:"depended_by,omitempty"`
	DependencyLevel               int        `json:"dependency_level"`
	Parallel                      bool       `json:"parallel"`
	AcceptanceJSON                string     `json:"acceptance_json"`
	DefinitionOfDone              string     `json:"definition_of_done"`
	InputAnchorsJSON              string     `json:"input_anchors_json"`
	DeveloperAgentID              string     `json:"developer_agent_id,omitempty"`
	DeveloperAgent                string     `json:"developer_agent,omitempty"`
	DeveloperOnline               bool       `json:"developer_online"`
	ReviewerAgentID               string     `json:"reviewer_agent_id,omitempty"`
	ReviewerAgent                 string     `json:"reviewer_agent,omitempty"`
	ReviewerOnline                bool       `json:"reviewer_online"`
	PassedCases                   int        `json:"passed_cases"`
	TotalCases                    int        `json:"total_cases"`
	ReviewID                      string     `json:"review_id"`
	ReviewedBy                    string     `json:"-"`
	ReviewedAt                    time.Time  `json:"reviewed_at"`
	CreatedAt                     time.Time  `json:"created_at"`
	StartedAt                     *time.Time `json:"started_at,omitempty"`
	PlannedFinishAt               *time.Time `json:"planned_finish_at,omitempty"`
	Blocker                       *string    `json:"blocker,omitempty"`
	ResourceCoordination          *string    `json:"resource_coordination,omitempty"`
}

// ConsoleTaskDriverStart is the durable seven-part project-start record. Token
// values are deliberately absent; TokenInstructions describes how an Agent
// obtains its own short-lived capability.
type ConsoleTaskDriverStart struct {
	SpecID                string     `json:"spec_id"`
	Revision              int        `json:"revision"`
	ContentSHA256         string     `json:"content_sha256"`
	RepositoryName        string     `json:"repository_name"`
	BranchName            string     `json:"branch_name"`
	Schedule              string     `json:"schedule"`
	TaskEndpoint          string     `json:"task_endpoint"`
	TokenInstructions     string     `json:"token_instructions"`
	AnnouncedBy           string     `json:"announced_by"`
	AnnouncedAt           time.Time  `json:"announced_at"`
	OwnerReviewedAt       *time.Time `json:"owner_reviewed_at,omitempty"`
	DeploymentRequestedAt *time.Time `json:"deployment_requested_at,omitempty"`
}

type ConsoleTaskPauseReport struct {
	SpecID     string    `json:"spec_id"`
	Revision   int       `json:"revision"`
	AgentID    string    `json:"agent_id"`
	ReportedAt time.Time `json:"reported_at"`
}

// ConsoleSpecPresplitRequest persists the one-shot Agent pre-split lifecycle.
// Agent identity is resolved from its authenticated session connection; these
// fields are server-owned and never accepted from callback JSON.
type ConsoleSpecPresplitRequest struct {
	ID             string     `json:"request_id"`
	SpecID         string     `json:"spec_id"`
	Revision       int        `json:"revision"`
	AgentID        string     `json:"agent_id"`
	OwnerPrincipal string     `json:"-"`
	Status         string     `json:"status"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	DeadlineAt     time.Time  `json:"deadline_at"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type ConsoleSession struct {
	ID          string
	PrincipalID string
	TokenHash   string
	ExpiresAt   time.Time
	RevokedAt   time.Time
	// CreatedAt 是滑动续期的**绝对上限**起算点(D1-b:无论多活跃,30 天后必须重登)。
	// 没有它就只能纯滑动 ⇒ 一个被窃的 cookie 永久有效。
	CreatedAt time.Time
}

type ConsolePageCapability struct {
	ID          string
	SessionID   string
	PrincipalID string
	AgentID     string
	PageID      string
	TokenHash   string
	Generation  int
	ExpiresAt   time.Time
	RevokedAt   time.Time
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

// KeyApplication records a self-service session key request and its review trail.
type KeyApplication struct {
	ID               string
	ApplicantOpenID  string
	ApplicantAccount string
	ApplicantBotID   string
	ApplicantBotName string
	Description      string
	Status           string
	ApproverOpenID   string
	ServiceID        string
	KeyID            string
	RejectReason     string
	CreatedAt        time.Time
	ReviewedAt       *time.Time
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

// hub-meta-types:begin
// Envelope.Meta["type"] 的跨语言单一真源。
//
// 🔴 sessionHelper(Python)侧不得另抄这些字面量:
//
//	tools/sessionhelper/sessionhelper/hub_meta.py 由
//	tools/sessionhelper/gen_hub_meta.py 从本块生成,
//	tools/sessionhelper/tests/test_hub_notice_no_inject.py 的漂移闸逐字节比对两侧。
//	两侧漂移的后果不是编译错,是「通知不再被识别 ⇒ 重新注入 CLI ⇒ 配额环复活」,
//	即静默失效。改本块后须重跑生成器:
//	  python3 tools/sessionhelper/gen_hub_meta.py
//
// 注意:annotateRestartWindow 的 reason 标签(如 hub.go 里的 "delivery_failure")
// 是另一套词汇,语义不同,不要复用这里的常量。
const (
	// MetaTypeDeliveryFailure 是 Hub 自发的投递失败回执(hub.go writeDeliveryFailure)。
	MetaTypeDeliveryFailure = "delivery_failure"
	// MetaTypeDeliveryResult 是 AMR 路由结果通知(amr.go notify)。
	MetaTypeDeliveryResult = "delivery_result"
)

// hub-meta-types:end

// NewPlatformConsoleProjection 新平台一个用户投影进旧 console 表的内容（store.SyncNewPlatformConsoleIdentity；R-V2-SPEC-TASK-SHIM-IMPL）。
type NewPlatformConsoleProjection struct {
	PrincipalID string // 用户 ID 小写
	Issuer      string
	DisplayName string // 用户简称；与旧主体撞名时存储改写为「简称·用户ID」
	OwnerKey    string // u_<用户 ID 小写>，与 /ws/agent 会话的 key_id 相同
	Active      bool   // 用户 active
	Management  bool   // 可管理用户（identity.CanManageUsers）⇒ console_platform_role＝management
	Agents      []NewPlatformConsoleAgent
	Session     *ConsoleSession // 非空 ⇒ 同事务投影这一个新平台会话（id／token_hash 带 np: 前缀）
	Now         time.Time
}

// NewPlatformConsoleAgent 一个未吊销的新平台 Agent。
type NewPlatformConsoleAgent struct {
	ID          string // np_agent.id
	SessionName string // 名称小写（/ws/agent 会话名）
	Active      bool   // np_agent.state==active（暂停 ⇒ false）
}
