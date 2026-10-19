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
	claims     map[string]*DeviceClaim
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
	groups     map[string]*Group
	members    map[string]map[string]time.Time // code → agent_id → 入组时刻
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
		groups:     map[string]*Group{},
		members:    map[string]map[string]time.Time{},
	}
}

var _ Store = (*MemStore)(nil)

// CountAudit 数某动作对某目标的审计行（测试用，满足 identitytest.AuditCounter）。
func (m *MemStore) CountAudit(_ context.Context, action, target string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.audit {
		if e.Action == action && e.Target == Lower(target) {
			n++
		}
	}
	return n, nil
}

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

func (m *MemStore) createUserLocked(u User) error {
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

func (m *MemStore) CreateUser(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createUserLocked(u)
}

func (m *MemStore) CreateFirstAdmin(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.users {
		if x.Type == UserPlatformAdmin || x.Type == UserManager {
			return ErrAdminExists
		}
	}
	return m.createUserLocked(u)
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

func (m *MemStore) SetUserType(_ context.Context, userIDLC string, t UserType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return ErrNotFound
	}
	u.Type = t
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

func (m *MemStore) ResetPassword(_ context.Context, userIDLC, hash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash, u.MustChangePassword = hash, true
	for _, s := range m.sessions {
		if s.UserIDLC == u.UserIDLC() && s.RevokedAt.IsZero() {
			s.RevokedAt = now
		}
	}
	return nil
}

func (m *MemStore) PromoteToPlatformAdmin(_ context.Context, userIDLC, actor, action string, now time.Time) (UserType, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[Lower(userIDLC)]
	if !ok {
		return "", ErrNotFound
	}
	if u.State != UserActive {
		return "", ErrUserNotActive
	}
	if u.Type == UserPlatformAdmin {
		return "", ErrAlreadyPlatformAdmin
	}
	old := u.Type
	u.Type = UserPlatformAdmin
	m.audit = append(m.audit, AuditEvent{Actor: actor, Action: action, Target: Lower(userIDLC), Detail: promoteAuditDetail(old), At: now})
	return old, nil
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

func (m *MemStore) GetIdentity(_ context.Context, userIDLC, provider string) (*Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	userIDLC = Lower(userIDLC)
	for ident, owner := range m.idents {
		if owner == userIDLC && ident.Provider == provider {
			cp := ident
			return &cp, nil
		}
	}
	return nil, ErrNotFound
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

func (m *MemStore) ListActiveRegistrationCodes(_ context.Context, now time.Time) ([]RegistrationCodeRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RegistrationCodeRow
	for d, c := range m.regCodes {
		if !c.redeemed && c.expiresAt.After(now) {
			out = append(out, RegistrationCodeRow{UserIDLC: c.userLC, CodeDigest: d, ExpiresAt: c.expiresAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserIDLC < out[j].UserIDLC })
	return out, nil
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
	switch {
	case u.State == UserPendingBind:
		u.State = UserActive
	case u.State == UserActive:
		for id, lc := range m.idents {
			if lc == c.userLC && id.Provider == ident.Provider {
				return nil, ErrAlreadyBound
			}
		}
		// 本人自取的绑定码：只绑身份，状态不变
	default:
		return nil, ErrNotPending
	}
	c.redeemed = true
	m.idents[ident] = c.userLC
	cp := *u
	return &cp, nil
}

func (m *MemStore) UserHasIdentity(_ context.Context, userIDLC, provider string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc := Lower(userIDLC)
	for id, u := range m.idents {
		if u == lc && id.Provider == provider {
			return true, nil
		}
	}
	return false, nil
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

func (m *MemStore) RevokeSession(ctx context.Context, tokenHash string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[tokenHash]; s != nil && s.RevokedAt.IsZero() {
		s.RevokedAt = now.UTC()
	}
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
		if ag.OwnerLC == a.OwnerLC && ag.Current() && ag.NameLC() == nameLC {
			return nil, ErrNameTaken
		}
	}
	if err := m.checkGroupChoiceLocked(a.OwnerLC, in.GroupID, in.NewGroup); err != nil {
		return nil, err
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
	m.applyGroupChoiceLocked(ag.ID, in.GroupID, in.NewGroup, now)
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
		if ag.OwnerLC == owner && ag.Current() {
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
		if ag.OwnerLC == ownerLC && ag.Current() && ag.NameLC() == nameLC {
			return true
		}
	}
	return false
}

func (m *MemStore) CreateHostedAgent(_ context.Context, ag Agent, app Application, newGroup *Group) error {
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
	if err := m.checkGroupChoiceLocked(owner, ag.GroupID, newGroup); err != nil {
		return err
	}
	a := ag
	a.OwnerLC, a.Origin, a.State, a.CredentialHash = owner, OriginHosted, AgentActive, ""
	m.agents[a.ID] = &a
	m.applyGroupChoiceLocked(a.ID, a.GroupID, newGroup, a.CreatedAt)
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
		if ag.Current() {
			cp := *ag
			return &cp, nil
		}
		// 吊销时刻相同（平票）时取 id 大的，与 PG 的 ORDER BY revoked_at DESC, id DESC 一致（qwen 复验 🔵1）
		if best == nil || ag.RevokedAt.After(best.RevokedAt) || (ag.RevokedAt.Equal(best.RevokedAt) && ag.ID > best.ID) {
			best = ag
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	cp := *best
	return &cp, nil
}

func (m *MemStore) SetAgentSuspended(_ context.Context, agentID string, suspend bool, by string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.agents[agentID]
	if !ok {
		return false, ErrNotFound
	}
	if ag.Origin != OriginHosted {
		return false, ErrNotHosted
	}
	switch {
	case ag.State == AgentRevoked:
		return false, ErrNotPending
	case suspend && ag.State == AgentSuspended, !suspend && ag.State == AgentActive:
		return true, nil
	case suspend:
		ag.State, ag.SuspendedAt, ag.SuspendedBy = AgentSuspended, now, by
	default:
		ag.State, ag.SuspendedAt, ag.SuspendedBy = AgentActive, time.Time{}, ""
	}
	return false, nil
}

func (m *MemStore) GetUserByShortName(_ context.Context, shortNameLC string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc, ok := m.shortNames[Lower(shortNameLC)]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m.users[lc]
	return &cp, nil
}

func (m *MemStore) ListUsers(context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserIDLC() < out[j].UserIDLC() })
	return out, nil
}

// ── GroupStore ─────────────────────────────────────────────────────────────

// ownedCurrentAgentLocked agentID 是否为 ownerLC 名下未吊销的 Agent。
func (m *MemStore) ownedCurrentAgentLocked(agentID, ownerLC string) bool {
	ag := m.agents[agentID]
	return ag != nil && ag.OwnerLC == Lower(ownerLC) && ag.Current()
}

func (m *MemStore) CreateGroup(_ context.Context, g Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ownedCurrentAgentLocked(g.LeaderAgentID, g.CreatorLC) {
		return ErrGroupAgentUnavailable
	}
	if _, ok := m.groups[g.Code]; ok {
		return ErrConflict
	}
	for _, o := range m.groups {
		if o.ShortName == g.ShortName {
			return ErrGroupNameTaken
		}
	}
	cp := g
	cp.CreatorLC = Lower(g.CreatorLC)
	m.groups[g.Code] = &cp
	m.members[g.Code] = map[string]time.Time{g.LeaderAgentID: g.CreatedAt}
	return nil
}

func (m *MemStore) GetGroup(_ context.Context, code string) (*Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *g
	return &cp, nil
}

func (m *MemStore) ListGroups(context.Context) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Group, 0, len(m.groups))
	for _, g := range m.groups {
		out = append(out, *g)
	}
	sortGroups(out)
	return out, nil
}

func (m *MemStore) ListGroupMembers(_ context.Context, code string) ([]GroupMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GroupMember, 0, len(m.members[code]))
	for id, at := range m.members[code] {
		out = append(out, GroupMember{Code: code, AgentID: id, CreatedAt: at})
	}
	sortGroupMembers(out)
	return out, nil
}

func (m *MemStore) ListAgentGroups(_ context.Context, agentID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for code, ms := range m.members {
		if _, ok := ms[agentID]; ok {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemStore) JoinGroup(_ context.Context, code, agentID, ownerLC string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return ErrNotFound
	}
	if g.AdditionsClosed {
		return ErrGroupClosed
	}
	if !m.ownedCurrentAgentLocked(agentID, ownerLC) {
		return ErrGroupAgentUnavailable
	}
	if _, in := m.members[code][agentID]; in {
		return ErrGroupMemberExists
	}
	m.members[code][agentID] = now
	return nil
}

func (m *MemStore) LeaveGroup(_ context.Context, code, agentID, actorLC string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return ErrNotFound
	}
	if _, in := m.members[code][agentID]; !in {
		return ErrGroupMemberNotFound
	}
	if agentID == g.LeaderAgentID {
		return ErrGroupLeaderProtected
	}
	actor := Lower(actorLC)
	if ag := m.agents[agentID]; g.CreatorLC != actor && (ag == nil || ag.OwnerLC != actor) {
		return ErrForbidden
	}
	delete(m.members[code], agentID)
	return nil
}

func (m *MemStore) SetGroupAdditionsClosed(_ context.Context, code, actorLC string, closed bool, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return ErrNotFound
	}
	if g.CreatorLC != Lower(actorLC) {
		return ErrForbidden
	}
	g.AdditionsClosed, g.UpdatedAt = closed, now
	return nil
}

func (m *MemStore) DeleteGroup(_ context.Context, code, actorLC string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return ErrNotFound
	}
	if g.CreatorLC != Lower(actorLC) {
		return ErrForbidden
	}
	delete(m.groups, code)
	delete(m.members, code)
	return nil
}

// checkGroupChoiceLocked 批准／托管新建前校验编组选择（不改状态）：新组 ⇒ 组码与组名不撞；已有组 ⇒ 存在且未关闭添加。
func (m *MemStore) checkGroupChoiceLocked(ownerLC, code string, newGroup *Group) error {
	if code == "" && newGroup == nil {
		return nil // 不入组（业主令：编组不是必须的）
	}
	if newGroup != nil {
		if _, ok := m.groups[newGroup.Code]; ok {
			return ErrConflict
		}
		for _, o := range m.groups {
			if o.ShortName == newGroup.ShortName {
				return ErrGroupNameTaken
			}
		}
		return nil
	}
	g, ok := m.groups[code]
	if !ok || !m.groupUsableByLocked(g, ownerLC) {
		return ErrGroupNotFound // 别人的组与不存在同一答复，不暴露组存在
	}
	if g.AdditionsClosed {
		return ErrGroupClosed
	}
	return nil
}

// groupUsableByLocked「本人可用」（组长令 2026-10-01，收紧 GroupAllowed）：本人建的组，或本人已有（未吊销）成员在内的组。
// 业主 S5 已把「加入他人编组」从页面拿掉；只凭组码就能把自己的 Agent 塞进别人的组，等于绕过。
func (m *MemStore) groupUsableByLocked(g *Group, ownerLC string) bool {
	owner := Lower(ownerLC)
	if g.CreatorLC == owner {
		return true
	}
	for id := range m.members[g.Code] {
		if ag := m.agents[id]; ag != nil && ag.OwnerLC == owner && ag.Current() {
			return true
		}
	}
	return false
}

// applyGroupChoiceLocked 校验通过后落地：建新组（该 Agent 为组长）或入已有组。
func (m *MemStore) applyGroupChoiceLocked(agentID, code string, newGroup *Group, now time.Time) {
	if code == "" && newGroup == nil {
		return
	}
	if newGroup != nil {
		g := *newGroup
		g.CreatorLC, g.LeaderAgentID = Lower(g.CreatorLC), agentID
		m.groups[g.Code] = &g
		m.members[g.Code] = map[string]time.Time{agentID: now}
		return
	}
	m.members[code][agentID] = now
}

func (m *MemStore) SetGroupLeaderOnly(_ context.Context, code, actorLC string, leaderOnly bool, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[code]
	if !ok {
		return ErrNotFound
	}
	if g.CreatorLC != Lower(actorLC) {
		return ErrForbidden
	}
	g.LeaderOnly, g.UpdatedAt = leaderOnly, now
	return nil
}
