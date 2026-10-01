package identity

import (
	"context"
	"time"
)

// IdentityStore 用户、身份绑定、注册码、会话与审计（N3）。设计件第五节。
type IdentityStore interface {
	CountUsers(ctx context.Context) (int, error)
	// CreateUser user_id 或 short_name 按小写冲突 ⇒ ErrConflict。
	CreateUser(ctx context.Context, u User) error
	GetUser(ctx context.Context, userIDLC string) (*User, error)
	// SetUserState 转 disabled 时同一事务吊销该用户全部会话（N3 A13）。
	SetUserState(ctx context.Context, userIDLC string, state UserState, now time.Time) error
	SetPassword(ctx context.Context, userIDLC, hash string, mustChange bool) error
	ResolveIdentity(ctx context.Context, ident Identity) (*User, error)
	// CreateRegistrationCode 同时作废该用户的旧码。
	CreateRegistrationCode(ctx context.Context, userIDLC, codeDigest string, expiresAt time.Time) error
	// RedeemRegistrationCode 一个事务：码有效 ⇒ 绑定身份 ⇒ 标记已用 ⇒ 用户转 active。
	// 码不存在／已用／过期 ⇒ ErrCodeInvalid；身份已绑定 ⇒ ErrIdentityTaken；用户非 pending_bind ⇒ ErrNotPending。
	// 任一失败都不改任何状态。
	RedeemRegistrationCode(ctx context.Context, codeDigest string, ident Identity, now time.Time) (*User, error)
	CreateSession(ctx context.Context, s Session) error
	// ResolveSession 会话不存在／过期／已吊销，或用户非 active ⇒ ErrNotFound。
	ResolveSession(ctx context.Context, tokenHash string, now time.Time) (*Session, *User, error)
	AppendAudit(ctx context.Context, e AuditEvent) error
}

// EnrollStore 入网申请、Agent 与凭据（V1、V2）。设计件第五节。
type EnrollStore interface {
	CreateApplication(ctx context.Context, a Application) error
	GetApplication(ctx context.Context, appID string) (*Application, error)
	// ListPendingApplications 只列该 owner 名下、仍 pending 且未过期的申请（V1 A6、A7）。
	// 顺序：按 requested_at 升序，同一时刻按 application_id 升序（必须确定，契约会测）。
	ListPendingApplications(ctx context.Context, ownerLC string, now time.Time) ([]Application, error)
	// RecordPairingFailure 错误核码计数；达到 max 时申请转 rejected。
	RecordPairingFailure(ctx context.Context, appID, ownerLC string, max int, now time.Time) (remaining int, rejected bool, err error)
	// Approve 一个事务：申请属该 owner、仍 pending 未过期 ⇒ 名称在该 owner 的 active Agent 中按小写不重
	// ⇒ 建 Agent ⇒ 申请转 approved ⇒ 同 owner 同设备的其它 pending 申请转 superseded（V1 A10）。
	Approve(ctx context.Context, in ApproveInput, now time.Time) (*Agent, error)
	Reject(ctx context.Context, appID, ownerLC string, now time.Time) error
	// TakeCredential 一个事务：申请 approved、凭据未被取走、Agent 仍 active ⇒ 写凭据哈希与取走时刻 ⇒ true。
	TakeCredential(ctx context.Context, appID, credentialHash string, now time.Time) (*Agent, bool, error)
	// ResolveAgentByCredential 含 revoked 行，供握手区分 credential_invalid 与 credential_revoked。
	ResolveAgentByCredential(ctx context.Context, credentialHash string) (*Agent, error)
	// GetAgent 按 agent_id 取（含 revoked 行）；Hub 据此在吊销后找到在线连接。
	GetAgent(ctx context.Context, agentID string) (*Agent, error)
	// ListAgents 只列该 owner 的 active Agent。
	ListAgents(ctx context.Context, ownerLC string) ([]Agent, error)
	// Revoke 不属于该 owner ⇒ ErrForbidden；托管 ⇒ ErrHosted；已吊销 ⇒ already=true 且不改任何字段。
	Revoke(ctx context.Context, agentID, ownerLC, by string, now time.Time) (already bool, err error)
}

// Store 两个接口的合集；内存实现与 PG 实现都实现它。
type Store interface {
	IdentityStore
	EnrollStore
}
