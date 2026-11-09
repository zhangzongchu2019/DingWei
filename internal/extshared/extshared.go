// Package extshared 外部 MCP Agent 接入（r53）各路共用的类型、常量与校验。
//
// 依据：openspec/changes/agent-http-mcp-gateway（提案 dc9684ad，业主 ㊹-补8 通过）
// 与组长技术定案 ㊹-补7。本包只放各路都要引用、且语义必须一致的东西；
// 表结构、存储与业务逻辑留在各路自己的包里，不在这里加。
package extshared

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 存活期与时限（㊹-补4、㊹-补6、提案 GW-005／GW-006）。
const (
	// TokenTTL 令牌滑动存活期：每次成功鉴权交互续到 now＋TokenTTL。
	TokenTTL = 14 * 24 * time.Hour
	// GroupTTL 临时编组滑动存活期：只由受理成功的发送与首次有效 received 确认续期，轮询不续。
	GroupTTL = 3 * 24 * time.Hour
	// MessageTTL 发往外部席位的消息自入队起的存活期；续令牌、续编组都不延长它。
	MessageTTL = 24 * time.Hour
	// PresenceTimeout 超过这么久没有成功请求即视为离线。
	PresenceTimeout = 90 * time.Second
	// WaitMax 收件长轮询的服务端硬上限。
	WaitMax = 25 * time.Second
)

// AGPrefix 临时编组名前缀；完整编组名为 AGPrefix＋全局序号（AG1、AG2……）。
const AGPrefix = "AG"

// AGCode 临时编组名，例如 "AG12"。
type AGCode string

// FormatAGCode 由全局序号生成编组名。序号从 1 起，不复用。
func FormatAGCode(seq int64) (AGCode, error) {
	if seq < 1 {
		return "", errors.New("extshared: AG sequence must be >= 1")
	}
	return AGCode(AGPrefix + strconv.FormatInt(seq, 10)), nil
}

var agCodeRE = regexp.MustCompile(`^AG[1-9][0-9]{0,17}$`)

// ParseAGCode 校验编组名并取回序号。
func ParseAGCode(s string) (int64, bool) {
	if !agCodeRE.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(s, AGPrefix), 10, 64)
	return n, err == nil && n >= 1
}

var shortNameRE = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

// ValidShortName 使用方自起的短名：仅小写字母与数字，1～20 位（㊿-补29；普通名称规则不变）。
func ValidShortName(s string) bool { return shortNameRE.MatchString(s) }

// AgentName 外部 Agent 名＝短名＋"_"＋编组名，例如 "mybot_AG3"。
func AgentName(short string, ag AGCode) (string, error) {
	if !ValidShortName(short) {
		return "", errors.New("extshared: invalid short name")
	}
	if _, ok := ParseAGCode(string(ag)); !ok {
		return "", errors.New("extshared: invalid AG code")
	}
	return short + "_" + string(ag), nil
}

// SplitAgentName 拆回短名与编组名；不是外部 Agent 名则 ok=false。
func SplitAgentName(name string) (short string, ag AGCode, ok bool) {
	i := strings.LastIndexByte(name, '_')
	if i <= 0 || !ValidShortName(name[:i]) {
		return "", "", false
	}
	if _, valid := ParseAGCode(name[i+1:]); !valid {
		return "", "", false
	}
	return name[:i], AGCode(name[i+1:]), true
}

// Principal 经扫码路验证后的飞书主体。内部唯一键是 (Provider, TenantKey, Issuer, Subject)；
// DisplayName 只用于显示，不作唯一键（㊹-补2）。
type Principal struct {
	Provider    string // 固定 "feishu"
	TenantKey   string // 必须等于本团队租户
	Issuer      string // 飞书应用（app_id）等签发方标识
	Subject     string // open_id
	UnionID     string // 可空；只在同一可信域内作别名
	DisplayName string // 英文名，缺则中文名转拼音，仅 [a-z]
}

// Key 主体的稳定内部键。
func (p Principal) Key() string {
	return p.Provider + "|" + p.TenantKey + "|" + p.Issuer + "|" + p.Subject
}

// ReceiptPhase 外部席位确认阶段。MVP 只有 received 一个阶段（㊹-补7 ②），不冒充 driver。
type ReceiptPhase string

const PhaseReceived ReceiptPhase = "received"

// SendStage Send 返回时消息所处的持久阶段。accepted 不等于已送达（㊹-补7 ③）。
type SendStage string

const (
	StageAccepted  SendStage = "accepted"  // PG 已受理（含 TouchGroup 与入队或 outbox）
	StageProjected SendStage = "projected" // 已投影到 helper 侧 session_delivery
	StageUnknown   SendStage = "unknown"   // 跨库结果未知，待对账
)

// AG 代际（generation）语义（rulings ㊹-补12 ③）：创建即为 1，AG 生命周期内不变；只有「同码复活」才 +1（MVP 无此路径）。
// 续期、到期扫描、解散都不得改动它；A 路据它核对席位与 AG 是否同一代，D 路若误改会让席位全部入口被拒。

// GroupUseKind 能续编组的事件种类；轮询、查在线清单不在其中（㊹-补6）。
type GroupUseKind string

const (
	UseAcceptedSend     GroupUseKind = "accepted_send"
	UseFirstReceivedAck GroupUseKind = "first_received_ack"
)

// 各路统一判别的错误。调用方按 errors.Is 判别；对外错误码映射由协议路统一做。
var (
	ErrUnauthenticated = errors.New("extshared: unauthenticated")
	ErrTokenRevoked    = errors.New("extshared: token revoked")
	ErrTokenExpired    = errors.New("extshared: token expired")
	ErrTenantRejected  = errors.New("extshared: tenant not allowed")
	ErrGroupDissolved  = errors.New("extshared: group dissolved or expired")
	ErrTargetNotInAG   = errors.New("extshared: target not in this AG")
	ErrIdempotencyKey  = errors.New("extshared: idempotency key reused with different request")
	ErrQueueFull       = errors.New("extshared: queue full")
	ErrNotPermitted    = errors.New("extshared: capability not permitted for external seat")
)

// MVPScopes 外部席位 MVP 唯一授予的 scope 集合：固定四项通讯能力，不做子集（接口约定第 6 节）。
// 顺序固定，ScopeString 的结果与 A 路 0105 令牌行持久的 scopes 文本逐字相同。
var MVPScopes = [4]string{"list_online", "send_message", "wait_for_messages", "ack"}

// ScopeString MVPScopes 以单个空格连接，用于持久化与比较。
func ScopeString() string { return strings.Join(MVPScopes[:], " ") }
