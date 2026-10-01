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
	// GetUserByShortName 按简称（小写）找用户；full_name（名称.简称）解析用。
	GetUserByShortName(ctx context.Context, shortNameLC string) (*User, error)
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
	// Approve 一个事务：申请属该 owner、仍 pending 未过期 ⇒ 名称在该 owner 的【未吊销】（active 或 suspended）Agent 中按小写不重
	// ⇒ 建 Agent ⇒ 申请转 approved ⇒ 同 owner 同设备的其它 pending 申请转 superseded（V1 A10）。
	Approve(ctx context.Context, in ApproveInput, now time.Time) (*Agent, error)
	Reject(ctx context.Context, appID, ownerLC string, now time.Time) error
	// TakeCredential 一个事务：申请 approved、未过期、凭据未被取走、Agent 仍 active ⇒ 写凭据哈希与取走时刻 ⇒ true。
	// Agent 原有凭据（若有）随之作废：之后用旧凭据认人 ⇒ ErrNotFound（凭据轮换，V8 重建／升级）。
	TakeCredential(ctx context.Context, appID, credentialHash string, now time.Time) (*Agent, bool, error)
	// ResolveAgentByCredential 含 revoked 行，供握手区分 credential_invalid 与 credential_revoked。
	ResolveAgentByCredential(ctx context.Context, credentialHash string) (*Agent, error)
	// GetAgent 按 agent_id 取（含 revoked 行）；Hub 据此在吊销后找到在线连接。
	GetAgent(ctx context.Context, agentID string) (*Agent, error)
	// FindAgentByName 按 (owner, 名称小写) 查：有未吊销的（active 或 suspended）返回它；否则返回最近一次被吊销的；都没有 ⇒ ErrNotFound。
	// 供路由失败时区分「目标已吊销」与「目标不存在／离线」（V2 A5）。
	FindAgentByName(ctx context.Context, ownerLC, nameLC string) (*Agent, error)
	// ListAgents 列该 owner 的未吊销 Agent（active 与 suspended；调用方看 State 区分）。
	ListAgents(ctx context.Context, ownerLC string) ([]Agent, error)
	// Revoke 不属于该 owner ⇒ ErrForbidden；托管 ⇒ ErrHosted；已吊销 ⇒ already=true 且不改任何字段。
	Revoke(ctx context.Context, agentID, ownerLC, by string, now time.Time) (already bool, err error)

	// ── 托管（V6／V8，Hub 内部调用，不经 console）──
	// CreateHostedAgent 一个事务：名称在该 owner 的 active Agent 中按小写不重（ErrNameTaken）⇒ 建 origin=hosted 的 Agent
	// ⇒ 建一条已批准、指向它的申请（其 PollTokenHash 即一次性入网令牌的哈希）。
	CreateHostedAgent(ctx context.Context, ag Agent, app Application) error
	// AddHostedToken 给已有的 active 托管 Agent 再发一条已批准申请（容器重建、升级时重签）。非托管 ⇒ ErrHosted 的反义：ErrNotHosted。
	AddHostedToken(ctx context.Context, agentID string, app Application) error
	// SetAgentSuspended 托管席位暂停／恢复（V8 停用／启用；组长裁定：停用期间凭据拒连）。
	// 只改状态、不动凭据哈希（恢复后原凭据照常可用）；非托管 ⇒ ErrNotHosted；已吊销 ⇒ ErrNotPending；已是目标状态 ⇒ already=true。
	SetAgentSuspended(ctx context.Context, agentID string, suspend bool, by string, now time.Time) (already bool, err error)
	// RetireAgent 内部吊销（V8 销毁）：不验归属、不拒托管；暂停中的也可吊销；已吊销 ⇒ already=true 且不改任何字段。
	RetireAgent(ctx context.Context, agentID, by string, now time.Time) (already bool, err error)
}

// Store 两个接口的合集；内存实现与 PG 实现都实现它。
type Store interface {
	IdentityStore
	EnrollStore
}
