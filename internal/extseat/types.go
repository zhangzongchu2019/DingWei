// Package extseat 实现外部席位的 PG 权威令牌、收件箱和投递受理。
package extseat

import (
	"context"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// Seat 是服务端读出的授权快照；公开业务入口始终重新读取权威记录。
// 不接收客户端声明的主体/组/代际作为授权依据。
type Seat struct {
	checkedAt                            time.Time
	proof, ownerShort                    string
	tokenGeneration                      int64
	ID, AgentID, Name, FullName, OwnerLC string
	Principal                            extshared.Principal
	Group                                extshared.AGCode
	Generation                           int64
	TokenID                              string
	AuthGeneration                       int64
	ExpiresAt                            time.Time
}

type Group struct {
	Code       extshared.AGCode
	Generation int64
	Target     identity.Agent
	ExpiresAt  time.Time
}

type CreateGroupRequest struct {
	Principal       extshared.Principal
	ExternalAgentID string
	OwnerLC         string
	Target          identity.Agent
	At              time.Time
	// CreateExternal 在编号分配后、D 写入双成员之前调用；事务由 A 持有。
	// D 必须在同一事务中调用且恰好一次；回调失败整个事务回滚。
	CreateExternal func(Group) error
}

// Groups 由 D 路实现。实现者只使用传入 tx，不另开事务或提交。
// RecheckGroup 在同一事务中锁定有效组并检查成员/代际，与解散互斥。
// TouchGroup 只接受已受理发送/首次 received；事件键持久幂等。
type Groups interface {
	CreateTempGroup(context.Context, repo.Tx, CreateGroupRequest) (Group, error)
	RecheckGroup(context.Context, repo.Tx, Seat, time.Time) (Group, error)
	TouchGroup(context.Context, repo.Tx, Group, string, extshared.GroupUseKind, time.Time) error
}

type Delivery struct {
	// SourceAgent 仅供outbox内部适配，已在PG事务授权；不序列化到通讯响应。
	SourceAgent identity.Agent `json:"-"`
	// 身份抬头所需的属主信息与目标属主短名随锁内快照读取，不暴露给协议响应。
	SourceOwnerDisplayName           string `json:"-"`
	SourceOwnerShortName             string `json:"-"`
	TargetOwnerShortName             string `json:"-"`
	ID                               string
	Generation                       int64
	From, To, Body                   string
	Group                            extshared.AGCode
	CreatedAt, ExpiresAt, LeaseUntil time.Time
	Phase                            extshared.ReceiptPhase
	ExpectReply                      bool `json:"-"`
}

type FeishuCopyEvent struct {
	ID, SeatID, AgentID, Issuer, Subject string
	ExternalName, TargetName             string
	OwnerDisplayName                     string
	OwnerShortName                       string
	Direction, Group, Body               string
	CreatedAt, ExpiresAt                 time.Time
}

// AgentFeishuCopyEvent 是内部 Agent 对话的只读抄送快照。队列实现必须在服务端
// 由 Agent 属主解析唯一、已验证的飞书身份；调用方不得提供收件 open_id。
type AgentFeishuCopyEvent struct {
	ID, AgentID, SourceName, TargetName, Group, Body string
	CreatedAt                                        time.Time
}

// AgentFeishuCopyService 只提供偏好写入和内部消息抄送入队。
type AgentFeishuCopyService interface {
	SetPreference(context.Context, string, string, bool) error
	EnqueueAgent(context.Context, AgentFeishuCopyEvent) error
}

const (
	FeishuCopyExternalToInternal = "external_to_internal"
	FeishuCopyInternalToExternal = "internal_to_external"
	FeishuCopyInternalAgent      = "internal_agent"
)

// FeishuCopyQueue 将抄送副本与主消息在同一 PG 事务中原子入队。
type FeishuCopyQueue interface {
	Enqueue(context.Context, repo.Tx, FeishuCopyEvent) error
}

type SendReceipt struct {
	DeliveryID           string
	Generation           int64
	Stage                extshared.SendStage
	CreatedAt, ExpiresAt time.Time
}

type OnlineAgent struct {
	Agent      identity.Agent
	FullName   string
	LastSeenAt time.Time
	External   bool
}

// Helpers 只返回可观测的 helper presence；组可见性由服务再次过滤。
type Helpers interface {
	Online(context.Context) ([]OnlineAgent, error)
}

// Projector 必须按 DeliveryID 幂等写入 SQLite，返回前提交。
// 重试同一消息不可重复生成 delivery_id；不能修改现有 helper ACK/重试语义。
type Projector interface {
	Project(context.Context, Delivery, identity.Agent) error
}
