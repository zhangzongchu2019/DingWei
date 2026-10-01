package identity

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemStore 是 Store 的内存实现：G3 在 R0 的 PG 测试环境就绪前用它跑契约测试与 Hub 侧集成用例。
// 一把互斥锁保护全部状态，每个方法即一个"事务"。返回值一律是副本。
type MemStore struct {
	mu         sync.Mutex
	users      map[string]*User    // user_id_lc → user
	shortNames map[string]string   // short_name_lc → user_id_lc
	idents     map[Identity]string // 渠道身份 → user_id_lc
	regCodes   map[string]*regCode // code_digest → 码
	sessions   map[string]*Session // token_hash → 会话
	audit      []AuditEvent
	apps       map[string]*Application
	agents     map[string]*Agent
	creds      map[string]string // credential_hash → agent_id
}

type regCode struct {
	userLC    string
	expiresAt time.Time
	redeemed  bool
}

// NewMemStore 建一个空的内存存储。
func NewMemStore() *MemStore {
	return &MemStore{
		users:      map[string]*User{},
		shortNames: map[string]string{},
		idents:     map[Identity]string{},
		regCodes:   map[string]*regCode{},
		sessions:   map[string]*Session{},
		apps:       map[string]*Application{},
		agents:     map[string]*Agent{},
		creds:      map[string]string{},
	}
}

var _ Store = (*MemStore)(nil)

// AuditLog 返回审计副本（测试用）。
func (m *MemStore) AuditLog() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AuditEvent(nil), m.audit...)
}

// ── IdentityStore ──────────────────────────────────────────────────────────

func (m *MemStore) CountUsers(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *MemStore) CreateUser(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.UserIDLC()]; ok {
		return ErrConflict
	}
	if _, ok := m.shortNames[u.ShortNameLC()]; ok {
		return ErrConflict
	}
	cp := u
	m.users[u.UserIDLC()] = &cp
	m.shortNames[u.ShortNameLC()] = u.UserIDLC()
	return nil
}

func (m *MemStore) GetUser(_ context.Context, userIDLC string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *MemStore) SetUserState(_ context.Context, userIDLC string, state UserState, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return ErrNotFound
	}
	u.State = state
	if state == UserDisabled {
		for _, s := range m.sessions {
			if s.UserIDLC == u.UserIDLC() && s.RevokedAt.IsZero() {
				s.RevokedAt = now
			}
		}
	}
	return nil
}

func (m *MemStore) SetPassword(_ context.Context, userIDLC, hash string, mustChange bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = hash
	u.MustChangePassword = mustChange
	return nil
}

func (m *MemStore) ResolveIdentity(_ context.Context, ident Identity) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc, ok := m.idents[ident]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m.users[lc]
	return &cp, nil
}

func (m *MemStore) CreateRegistrationCode(_ context.Context, userIDLC, codeDigest string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc := Lower(userIDLC)
	if _, ok := m.users[lc]; !ok {
		return ErrNotFound
	}
	if _, ok := m.regCodes[codeDigest]; ok {
		return ErrConflict
	}
	for d, c := range m.regCodes {
		if c.userLC == lc {
			delete(m.regCodes, d)
		}
	}
	m.regCodes[codeDigest] = &regCode{userLC: lc, expiresAt: expiresAt}
	return nil
}

func (m *MemStore) RedeemRegistrationCode(_ context.Context, codeDigest string, ident Identity, now time.Time) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.regCodes[codeDigest]
	if !ok || c.redeemed || !now.Before(c.expiresAt) {
		return nil, ErrCodeInvalid
	}
	if _, taken := m.idents[ident]; taken {
		return nil, ErrIdentityTaken
	}
	u := m.users[c.userLC]
	if u == nil {
		return nil, ErrCodeInvalid
	}
	if u.State != UserPendingBind {
		return nil, ErrNotPending
	}
	c.redeemed = true
	m.idents[ident] = c.userLC
	u.State = UserActive
	cp := *u
	return &cp, nil
}

func (m *MemStore) CreateSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.TokenHash]; ok {
		return ErrConflict
	}
	cp := s
	cp.UserIDLC = Lower(s.UserIDLC)
	m.sessions[s.TokenHash] = &cp
	return nil
}

func (m *MemStore) ResolveSession(_ context.Context, tokenHash string, now time.Time) (*Session, *User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tokenHash]
	if !ok || !s.RevokedAt.IsZero() || !now.Before(s.ExpiresAt) {
		return nil, nil, ErrNotFound
	}
	u := m.users[s.UserIDLC]
	if u == nil || u.State != UserActive {
		return nil, nil, ErrNotFound
	}
	sc, uc := *s, *u
	return &sc, &uc, nil
}

func (m *MemStore) AppendAudit(_ context.Context, e AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, e)
	return nil
}

// ── EnrollStore ────────────────────────────────────────────────────────────

func (m *MemStore) CreateApplication(_ context.Context, a Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.apps[a.ID]; ok {
		return ErrConflict
	}
	cp := a
	cp.OwnerLC = Lower(a.OwnerLC)
	if cp.State == "" {
		cp.State = AppPending
	}
	m.apps[a.ID] = &cp
	return nil
}

func (m *MemStore) GetApplication(_ context.Context, appID string) (*Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.apps[appID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *MemStore) ListPendingApplications(_ context.Context, ownerLC string, now time.Time) ([]Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner := Lower(ownerLC)
	var out []Application
	for _, a := range m.apps {
		if a.OwnerLC == owner && a.EffectiveState(now) == AppPending {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.Before(out[j].RequestedAt)
		}
		return out[i].ID < out[j].ID // 同一时刻按申请号，保证顺序确定（契约要求）
	})
	return out, nil
}

// pendingAppLocked 取属于 owner、当前仍 pending 的申请；不属于 owner 一律按不存在处理（不泄露别人的申请）。
func (m *MemStore) pendingAppLocked(appID, ownerLC string, now time.Time) (*Application, error) {
	a, ok := m.apps[appID]
	if !ok || a.OwnerLC != Lower(ownerLC) {
		return nil, ErrNotFound
	}
	switch a.EffectiveState(now) {
	case AppPending:
		return a, nil
	case AppExpired:
		return nil, ErrExpired
	default:
		return nil, ErrNotPending
	}
}

func (m *MemStore) RecordPairingFailure(_ context.Context, appID, ownerLC string, max int, now time.Time) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.pendingAppLocked(appID, ownerLC, now)
	if err != nil {
		return 0, false, err
	}
	a.PairingFailures++
	remaining := max - a.PairingFailures
	if remaining <= 0 {
		a.State = AppRejected
		return 0, true, nil
	}
	return remaining, false, nil
}

func (m *MemStore) Approve(_ context.Context, in ApproveInput, now time.Time) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.pendingAppLocked(in.AppID, in.OwnerLC, now)
	if err != nil {
		return nil, err
	}
	if _, ok := m.agents[in.AgentID]; ok {
		return nil, ErrConflict
	}
	nameLC := Lower(in.Name)
	for _, ag := range m.agents {
		if ag.OwnerLC == a.OwnerLC && ag.State == AgentActive && ag.NameLC() == nameLC {
			return nil, ErrNameTaken
		}
	}
	ag := &Agent{
		ID:        in.AgentID,
		OwnerLC:   a.OwnerLC,
		Name:      in.Name,
		GroupID:   in.GroupID,
		Origin:    OriginSelfhost,
		State:     AgentActive,
		CreatedAt: now,
	}
	m.agents[ag.ID] = ag
	a.State = AppApproved
	a.AgentID = ag.ID
	for _, o := range m.apps {
		if o.ID != a.ID && o.OwnerLC == a.OwnerLC && o.DeviceFingerprint == a.DeviceFingerprint &&
			o.EffectiveState(now) == AppPending {
			o.State = AppSuperseded
		}
	}
	cp := *ag
	return &cp, nil
}

func (m *MemStore) Reject(_ context.Context, appID, ownerLC string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.pendingAppLocked(appID, ownerLC, now)
	if err != nil {
		return err
	}
	a.State = AppRejected
	return nil
}

func (m *MemStore) TakeCredential(_ context.Context, appID, credentialHash string, now time.Time) (*Agent, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.apps[appID]
	if !ok {
		return nil, false, ErrNotFound
	}
	if a.State != AppApproved || !a.CredentialTakenAt.IsZero() || !now.Before(a.ExpiresAt) {
		return nil, false, nil
	}
	ag := m.agents[a.AgentID]
	if ag == nil || ag.State != AgentActive {
		return nil, false, nil
	}
	if _, dup := m.creds[credentialHash]; dup {
		return nil, false, ErrConflict
	}
	if ag.CredentialHash != "" {
		delete(m.creds, ag.CredentialHash) // 凭据轮换：旧凭据作废
	}
	ag.CredentialHash = credentialHash
	m.creds[credentialHash] = ag.ID
	a.CredentialTakenAt = now
	cp := *ag
	return &cp, true, nil
}

func (m *MemStore) ResolveAgentByCredential(_ context.Context, credentialHash string) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.creds[credentialHash]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m.agents[id]
	return &cp, nil
}

func (m *MemStore) GetAgent(_ context.Context, agentID string) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.agents[agentID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *ag
	return &cp, nil
}

func (m *MemStore) ListAgents(_ context.Context, ownerLC string) ([]Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner := Lower(ownerLC)
	var out []Agent
	for _, ag := range m.agents {
		if ag.OwnerLC == owner && ag.State == AgentActive {
			out = append(out, *ag)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NameLC() < out[j].NameLC() })
	return out, nil
}

func (m *MemStore) Revoke(_ context.Context, agentID, ownerLC, by string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.agents[agentID]
	if !ok {
		return false, ErrNotFound
	}
	if ag.OwnerLC != Lower(ownerLC) {
		return false, ErrForbidden
	}
	if ag.Origin == OriginHosted {
		return false, ErrHosted
	}
	if ag.State == AgentRevoked {
		return true, nil
	}
	ag.State = AgentRevoked
	ag.RevokedAt = now
	ag.RevokedBy = by
	return false, nil
}

func (m *MemStore) nameTakenLocked(ownerLC, nameLC string) bool {
	for _, ag := range m.agents {
		if ag.OwnerLC == ownerLC && ag.State == AgentActive && ag.NameLC() == nameLC {
			return true
		}
	}
	return false
}

func (m *MemStore) CreateHostedAgent(_ context.Context, ag Agent, app Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[ag.ID]; ok {
		return ErrConflict
	}
	if _, ok := m.apps[app.ID]; ok {
		return ErrConflict
	}
	owner := Lower(ag.OwnerLC)
	if m.nameTakenLocked(owner, ag.NameLC()) {
		return ErrNameTaken
	}
	a := ag
	a.OwnerLC, a.Origin, a.State, a.CredentialHash = owner, OriginHosted, AgentActive, ""
	m.agents[a.ID] = &a
	ap := app
	ap.OwnerLC, ap.State, ap.AgentID = owner, AppApproved, a.ID
	m.apps[ap.ID] = &ap
	return nil
}

func (m *MemStore) AddHostedToken(_ context.Context, agentID string, app Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.agents[agentID]
	if !ok {
		return ErrNotFound
	}
	if ag.Origin != OriginHosted {
		return ErrNotHosted
	}
	if ag.State != AgentActive {
		return ErrNotPending
	}
	if _, ok := m.apps[app.ID]; ok {
		return ErrConflict
	}
	ap := app
	ap.OwnerLC, ap.State, ap.AgentID = ag.OwnerLC, AppApproved, ag.ID
	m.apps[ap.ID] = &ap
	return nil
}

func (m *MemStore) RetireAgent(_ context.Context, agentID, by string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.agents[agentID]
	if !ok {
		return false, ErrNotFound
	}
	if ag.State == AgentRevoked {
		return true, nil
	}
	ag.State, ag.RevokedAt, ag.RevokedBy = AgentRevoked, now, by
	return false, nil
}

func (m *MemStore) FindAgentByName(_ context.Context, ownerLC, nameLC string) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner, name := Lower(ownerLC), Lower(nameLC)
	var best *Agent
	for _, ag := range m.agents {
		if ag.OwnerLC != owner || ag.NameLC() != name {
			continue
		}
		if ag.State == AgentActive {
			cp := *ag
			return &cp, nil
		}
		if best == nil || ag.RevokedAt.After(best.RevokedAt) {
			best = ag
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	cp := *best
	return &cp, nil
}
