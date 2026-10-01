package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// 业务层错误（HTTP 层据此映射状态码与机器码，设计件第二、三节）。
var (
	ErrBadRequest        = errors.New("identity: bad request")
	ErrUnknownOwner      = errors.New("identity: unknown owner")
	ErrNameInvalid       = errors.New("identity: agent name invalid")
	ErrCredentialInvalid = errors.New("identity: credential invalid")
	ErrCredentialRevoked = errors.New("identity: credential revoked")
)

// RateLimitedError 限频；RetryAfter 供应答头与正文。
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("identity: rate limited, retry after %s", e.RetryAfter)
}

// PairingMismatchError 配对码不符；Rejected 为真表示已达上限、申请已转 rejected。
type PairingMismatchError struct {
	Remaining int
	Rejected  bool
}

func (e *PairingMismatchError) Error() string {
	return fmt.Sprintf("identity: pairing code mismatch (remaining=%d rejected=%v)", e.Remaining, e.Rejected)
}

// 限额（设计件 2.1、2.2、3.1）。
const (
	ApplyPerIPPerHour   = 10
	MaxPendingPerOwner  = 20
	PollMinInterval     = 2 * time.Second
	MaxPairingFailures  = 5
	CredentialPrefix    = "dwa_"
	maxFingerprintBytes = 128
	maxVersionBytes     = 64
)

// Service 入网、审批、吊销与凭据认证的业务层。
type Service struct {
	Store Store
	Now   func() time.Time
	Rand  io.Reader
	// OnRevoked 在吊销成功（非重复吊销）后、Revoke 返回前同步调用：Hub 据此以 4401 断开在线连接并重推在线清单（V2 A1）。
	OnRevoked func(agentID string)

	mu        sync.Mutex
	applyHits map[string][]time.Time // 来源 IP → 最近一小时的申请时刻
	lastPoll  map[string]time.Time   // application_id → 上次轮询时刻
}

// NewService 建业务层；now 与 rnd 为空时用真实时钟与 crypto/rand。
func NewService(s Store, now func() time.Time, rnd io.Reader) *Service {
	if now == nil {
		now = time.Now
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	return &Service{Store: s, Now: now, Rand: rnd, applyHits: map[string][]time.Time{}, lastPoll: map[string]time.Time{}}
}

// HashSecret 令牌、凭据、码的库内表示：sha256 hex（与旧平台 HashAPIKey 同法）。
func HashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// PairingDigest sha256(盐字节 ＋ 配对码 ASCII) 的 hex；盐为 hex 串。与客户端 dbfc9f14 一致。
func PairingDigest(saltHex, code string) (string, error) {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return "", ErrBadRequest
	}
	sum := sha256.Sum256(append(salt, []byte(code)...))
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.Rand, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (s *Service) audit(ctx context.Context, actor, action, target, detail string) {
	_ = s.Store.AppendAudit(ctx, AuditEvent{Actor: actor, Action: action, Target: target, Detail: detail, At: s.Now()})
}

// ── 客户端接口 ──────────────────────────────────────────────────────────────

// ApplyRequest 对应 POST /api/enroll/applications。
type ApplyRequest struct {
	OwnerUserID       string
	PairingSalt       string
	PairingDigest     string
	DeviceFingerprint string
	ClientVersion     string
	SourceIP          string
}

// ApplyResult 申请结果；PollToken 只在此出现一次，库里只存其哈希。
type ApplyResult struct {
	ApplicationID string
	PollToken     string
	ExpiresAt     time.Time
}

// Apply 发起入网申请（V1 第二节 1、2）。
func (s *Service) Apply(ctx context.Context, r ApplyRequest) (*ApplyResult, error) {
	if !ValidUserID(r.OwnerUserID) || !isHex(r.PairingSalt, 32) || !isHex(r.PairingDigest, 64) ||
		r.DeviceFingerprint == "" || len(r.DeviceFingerprint) > maxFingerprintBytes || len(r.ClientVersion) > maxVersionBytes {
		return nil, ErrBadRequest
	}
	now := s.Now()
	if err := s.takeApplySlot(r.SourceIP, now); err != nil {
		return nil, err
	}
	owner, err := s.Store.GetUser(ctx, r.OwnerUserID)
	if errors.Is(err, ErrNotFound) || (err == nil && owner.State != UserActive) {
		return nil, ErrUnknownOwner
	}
	if err != nil {
		return nil, err
	}
	pending, err := s.Store.ListPendingApplications(ctx, owner.UserIDLC(), now)
	if err != nil {
		return nil, err
	}
	if len(pending) >= MaxPendingPerOwner {
		return nil, &RateLimitedError{RetryAfter: time.Hour}
	}
	id, err := s.randHex(16)
	if err != nil {
		return nil, err
	}
	token, err := s.randHex(32)
	if err != nil {
		return nil, err
	}
	app := Application{
		ID: id, OwnerLC: owner.UserIDLC(), PairingSalt: Lower(r.PairingSalt), PairingDigest: Lower(r.PairingDigest),
		PollTokenHash: HashSecret(token), DeviceFingerprint: r.DeviceFingerprint, ClientVersion: r.ClientVersion,
		SourceIP: r.SourceIP, RequestedAt: now, ExpiresAt: now.Add(ApplicationTTL), State: AppPending,
	}
	if err := s.Store.CreateApplication(ctx, app); err != nil {
		return nil, err
	}
	s.audit(ctx, "client@"+r.SourceIP, "enroll.apply", id, "owner="+owner.UserIDLC())
	return &ApplyResult{ApplicationID: id, PollToken: token, ExpiresAt: app.ExpiresAt}, nil
}

// takeApplySlot 同一来源 IP 每小时最多 ApplyPerIPPerHour 次（内存计数，单实例 Hub）。
func (s *Service) takeApplySlot(ip string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := now.Add(-time.Hour)
	hits := s.applyHits[ip][:0]
	for _, t := range s.applyHits[ip] {
		if t.After(cut) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= ApplyPerIPPerHour {
		s.applyHits[ip] = hits
		return &RateLimitedError{RetryAfter: hits[0].Add(time.Hour).Sub(now)}
	}
	s.applyHits[ip] = append(hits, now)
	return nil
}

// PollResult 对应 GET /api/enroll/applications/{id} 的应答。
type PollResult struct {
	State         AppState
	AgentName     string
	AgentFullName string
	Credential    string // 只在第一次取回时非空
}

// Poll 轮询申请；approved 时第一次取回即生成凭据（设计件 1.3：库里只存哈希）。
// 申请不存在与令牌不符一律 ErrNotFound（不泄露申请是否存在）。
func (s *Service) Poll(ctx context.Context, appID, pollToken string) (*PollResult, error) {
	app, err := s.Store.GetApplication(ctx, appID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(HashSecret(pollToken)), []byte(app.PollTokenHash)) != 1 {
		return nil, ErrNotFound
	}
	now := s.Now()
	if err := s.takePollSlot(appID, now); err != nil {
		return nil, err
	}
	st := app.EffectiveState(now)
	res := &PollResult{State: st}
	if st != AppApproved {
		return res, nil
	}
	cred, err := s.randHex(32)
	if err != nil {
		return nil, err
	}
	cred = CredentialPrefix + cred
	ag, took, err := s.Store.TakeCredential(ctx, appID, HashSecret(cred), now)
	if err != nil {
		return nil, err
	}
	if !took {
		return res, nil // 已被取走（或 Agent 已在取走前被吊销）：不再给凭据
	}
	res.AgentName = ag.Name
	res.AgentFullName = s.fullName(ctx, ag)
	res.Credential = cred
	s.audit(ctx, "client", "agent.credential_issued", ag.ID, "application="+appID)
	return res, nil
}

func (s *Service) takePollSlot(appID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.lastPoll[appID]; ok && now.Sub(last) < PollMinInterval {
		return &RateLimitedError{RetryAfter: PollMinInterval - now.Sub(last)}
	}
	s.lastPoll[appID] = now
	return nil
}

func (s *Service) fullName(ctx context.Context, ag *Agent) string {
	u, err := s.Store.GetUser(ctx, ag.OwnerLC)
	if err != nil {
		return ag.Name
	}
	return ag.Name + "." + u.ShortName
}

// AuthenticateAgent 连接握手：按凭据认出 Agent（组长裁定：只认凭据不认名）。
func (s *Service) AuthenticateAgent(ctx context.Context, credential string) (*Agent, string, error) {
	ag, err := s.Store.ResolveAgentByCredential(ctx, HashSecret(credential))
	if errors.Is(err, ErrNotFound) {
		return nil, "", ErrCredentialInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if ag.State == AgentRevoked {
		return nil, "", ErrCredentialRevoked
	}
	return ag, s.fullName(ctx, ag), nil
}

// ── console 接口（actor＝当前登录用户的 user_id_lc）──────────────────────────

// ListPending 审核区：只列 actor 名下的 pending 申请（V1 A6）。
func (s *Service) ListPending(ctx context.Context, actorLC string) ([]Application, error) {
	return s.Store.ListPendingApplications(ctx, actorLC, s.Now())
}

// Approve 核对配对码、下发名称与编组（V1 第二节 4、5；设计件 3.1）。
func (s *Service) Approve(ctx context.Context, actorLC, appID, pairingCode, name, groupID string) (*Agent, string, error) {
	if !ValidAgentName(name) {
		return nil, "", ErrNameInvalid
	}
	now := s.Now()
	app, err := s.Store.GetApplication(ctx, appID)
	if err != nil {
		return nil, "", err
	}
	if app.OwnerLC != Lower(actorLC) {
		return nil, "", ErrNotFound
	}
	switch app.EffectiveState(now) {
	case AppPending:
	case AppExpired:
		return nil, "", ErrExpired
	default:
		return nil, "", ErrNotPending
	}
	want, err := PairingDigest(app.PairingSalt, pairingCode)
	if err != nil {
		return nil, "", err
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(app.PairingDigest)) != 1 {
		remaining, rejected, ferr := s.Store.RecordPairingFailure(ctx, appID, actorLC, MaxPairingFailures, now)
		if ferr != nil {
			return nil, "", ferr
		}
		s.audit(ctx, actorLC, "enroll.pairing_failed", appID, fmt.Sprintf("remaining=%d", remaining))
		return nil, "", &PairingMismatchError{Remaining: remaining, Rejected: rejected}
	}
	agentID, err := s.randHex(16)
	if err != nil {
		return nil, "", err
	}
	ag, err := s.Store.Approve(ctx, ApproveInput{AppID: appID, OwnerLC: actorLC, AgentID: "ag_" + agentID, Name: name, GroupID: groupID}, now)
	if err != nil {
		return nil, "", err
	}
	s.audit(ctx, actorLC, "enroll.approve", appID, "agent="+ag.ID)
	return ag, s.fullName(ctx, ag), nil
}

// Reject 拒绝（＝踢走）一条申请。
func (s *Service) Reject(ctx context.Context, actorLC, appID string) error {
	if err := s.Store.Reject(ctx, appID, actorLC, s.Now()); err != nil {
		return err
	}
	s.audit(ctx, actorLC, "enroll.reject", appID, "")
	return nil
}

// ListAgents Agent 列表：actor 名下的 active Agent。
func (s *Service) ListAgents(ctx context.Context, actorLC string) ([]Agent, error) {
	return s.Store.ListAgents(ctx, actorLC)
}

// Revoke 吊销（V2）。重复吊销返回 already=true，不写审计、不触发断开（V2 A10）。
func (s *Service) Revoke(ctx context.Context, actorLC, agentID string) (bool, error) {
	already, err := s.Store.Revoke(ctx, agentID, actorLC, actorLC, s.Now())
	if err != nil || already {
		return already, err
	}
	s.audit(ctx, actorLC, "agent.revoke", agentID, "")
	if s.OnRevoked != nil {
		s.OnRevoked(agentID)
	}
	return false, nil
}
