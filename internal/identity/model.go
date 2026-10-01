// Package identity 是新平台 G3「身份与入网」的领域层：用户、渠道身份绑定、注册码、
// console 会话、入网申请、Agent 与其专属凭据。
//
// 设计件：runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md（板 152ae473）。
// 本包只定模型、规则与存储接口；HTTP/WS 接口在 Hub 侧，PG 实现归 G2 的 R0。
package identity

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// UserType 用户类型。
type UserType string

const (
	UserPlatformAdmin UserType = "platform_admin" // 初始化脚本建的第一个管理用户
	UserManager       UserType = "manager"        // 管理用户
	UserMember        UserType = "member"         // 普通用户
)

// UserState 用户状态。
type UserState string

const (
	UserPendingBind UserState = "pending_bind" // 已开户、未绑定飞书
	UserActive      UserState = "active"
	UserDisabled    UserState = "disabled"
)

// User 平台用户。UserID 与 ShortName 显示保留大小写，标识一律按小写（N3 第六节）。
type User struct {
	UserID             string
	ShortName          string
	DisplayName        string
	Type               UserType
	State              UserState
	PasswordHash       string // 只有初始化脚本建的管理用户有
	MustChangePassword bool
	CreatedAt          time.Time
}

func (u User) UserIDLC() string    { return Lower(u.UserID) }
func (u User) ShortNameLC() string { return Lower(u.ShortName) }

// Identity 一个外部渠道身份。本版 Provider 只有 "feishu"，Subject 为发信人 open_id。
type Identity struct {
	Provider string
	Issuer   string
	Subject  string
}

// Session console 会话。库里只存令牌哈希。
type Session struct {
	TokenHash string
	UserIDLC  string
	ExpiresAt time.Time
	RevokedAt time.Time // 零值＝未吊销
}

// AuditEvent 一条审计。🔴 Detail 里不得出现任何密码、码、凭据、令牌的明文。
type AuditEvent struct {
	Actor  string
	Action string
	Target string
	Detail string
	At     time.Time
}

// AppState 入网申请的落库状态。expired 不落库，由 EffectiveState 推出。
type AppState string

const (
	AppPending    AppState = "pending"
	AppApproved   AppState = "approved"
	AppRejected   AppState = "rejected"
	AppSuperseded AppState = "superseded"
	AppExpired    AppState = "expired" // 只作 EffectiveState 的返回值
)

// ApplicationTTL 申请有效期（V1：24 小时作废）。
const ApplicationTTL = 24 * time.Hour

// Application 一条入网申请。
type Application struct {
	ID                string
	OwnerLC           string
	PairingSalt       string // hex
	PairingDigest     string // hex：sha256(盐字节 ＋ 配对码 ASCII)
	PollTokenHash     string
	DeviceFingerprint string
	ClientVersion     string
	SourceIP          string
	RequestedAt       time.Time
	ExpiresAt         time.Time
	State             AppState
	PairingFailures   int
	AgentID           string
	CredentialTakenAt time.Time // 零值＝凭据未被取走
}

// EffectiveState 按时刻推出对外状态：pending 且已到期 ⇒ expired。
func (a Application) EffectiveState(now time.Time) AppState {
	if a.State == AppPending && !now.Before(a.ExpiresAt) {
		return AppExpired
	}
	return a.State
}

// AgentState Agent 状态。吊销只改状态、不删行（V2：审计与「旧凭据被拒」须可判定）。
type AgentState string

const (
	AgentActive AgentState = "active"
	// AgentSuspended 可逆的暂停（托管停用，组长裁定 2026-10-01）：凭据拒连但不作废，名字仍被占用。
	AgentSuspended AgentState = "suspended"
	AgentRevoked   AgentState = "revoked"
)

// AgentOrigin 来源。
type AgentOrigin string

const (
	OriginSelfhost AgentOrigin = "selfhost"
	OriginHosted   AgentOrigin = "hosted"
)

// Agent 一个已入网的 Agent。
type Agent struct {
	ID             string
	OwnerLC        string
	Name           string
	GroupID        string
	Origin         AgentOrigin
	State          AgentState
	CredentialHash string // 为空＝凭据尚未被客户端取走（设计件 1.3）
	CreatedAt      time.Time
	RevokedAt      time.Time
	RevokedBy      string
}

func (a Agent) NameLC() string { return Lower(a.Name) }

// Current 未吊销（active 或 suspended）：占用名字、出现在 Agent 列表里。
func (a Agent) Current() bool { return a.State == AgentActive || a.State == AgentSuspended }

// ApproveInput 批准一条申请所需的输入。配对码比对在调用方完成（需要先读出盐）。
type ApproveInput struct {
	AppID   string
	OwnerLC string
	AgentID string
	Name    string
	GroupID string
}

// 存储层错误。调用方按 errors.Is 判别。
var (
	ErrNotFound      = errors.New("identity: not found")
	ErrConflict      = errors.New("identity: conflict")
	ErrNameTaken     = errors.New("identity: agent name taken")
	ErrIdentityTaken = errors.New("identity: channel identity already bound")
	ErrNotPending    = errors.New("identity: not pending")
	ErrExpired       = errors.New("identity: expired")
	ErrForbidden     = errors.New("identity: forbidden")
	ErrHosted        = errors.New("identity: hosted agent")
	ErrNotHosted     = errors.New("identity: not a hosted agent")
	ErrCodeInvalid   = errors.New("identity: code invalid, used or expired")
)

// Lower 标识规范化：一律转小写（业主裁定：判定标识只以小写字母＋数字判定）。
func Lower(s string) string { return strings.ToLower(s) }

var alnum = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func validAlnum(s string, min, max int) bool {
	return len(s) >= min && len(s) <= max && alnum.MatchString(s)
}

// ValidUserID 用户 ID：大小写字母与数字，2～32 位（台账 c5981300）。
func ValidUserID(s string) bool { return validAlnum(s, 2, 32) }

// ValidShortName 用户 ID 简称：大小写字母与数字，2～16 位（台账 c5981300）。
func ValidShortName(s string) bool { return validAlnum(s, 2, 16) }

// ValidAgentName Agent 名称：大小写字母与数字（V1 第七节）。
// 长度 spec 未定，暂取 2～32，与用户 ID 同。
func ValidAgentName(s string) bool { return validAlnum(s, 2, 32) }
