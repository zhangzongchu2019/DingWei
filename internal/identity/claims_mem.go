package identity

import (
	"context"
	"crypto/subtle"
	"time"
)

func (m *MemStore) CreateDeviceClaim(_ context.Context, c DeviceClaim) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claims == nil {
		m.claims = map[string]*DeviceClaim{}
	}
	if m.claims[c.ID] != nil {
		return ErrConflict
	}
	for _, v := range m.claims {
		if v.CodeDigest == c.CodeDigest {
			return ErrConflict
		}
	}
	cp := c
	m.claims[c.ID] = &cp
	return nil
}
func (m *MemStore) GetDeviceClaim(_ context.Context, id string) (*DeviceClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.claims[id]
	if c == nil {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}
func (m *MemStore) DeviceClaimByCode(_ context.Context, digest string) (*DeviceClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.claims {
		if c.CodeDigest == digest {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}
func (m *MemStore) ClaimDevice(_ context.Context, digest string, ident Identity, agentID, hash string, now time.Time) (*DeviceClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var c *DeviceClaim
	for _, v := range m.claims {
		if v.CodeDigest == digest {
			c = v
			break
		}
	}
	if c == nil {
		return nil, ErrNotFound
	}
	if !now.Before(c.ExpiresAt) {
		return nil, ErrExpired
	}
	if c.State != "awaiting_feishu" {
		return nil, ErrNotPending
	}
	lc := Lower(c.UserID)
	bound := m.idents[ident]
	u := m.users[lc]
	if bound != "" {
		// A free preferred ID is not authority over another account. Reuse the
		// sender's existing account, while any occupied other ID is refused.
		if u != nil && bound != lc {
			return nil, ErrForbidden
		}
		u = m.users[bound]
		if u == nil || u.State != UserActive {
			return nil, ErrForbidden
		}
		lc = bound
	} else if u != nil {
		return nil, ErrForbidden
	}
	if u == nil && m.shortNames[Lower(c.ShortName)] != "" {
		return nil, ErrConflict
	}
	for _, ag := range m.agents {
		if ag.OwnerLC == lc && ag.NameLC() == Lower(c.AgentName) && ag.Current() {
			return nil, ErrNameTaken
		}
	}
	if m.agents[agentID] != nil || m.creds[hash] != "" {
		return nil, ErrConflict
	}
	// All checks precede the first mutation: one lock is this store's transaction.
	if u == nil {
		u = &User{UserID: c.UserID, ShortName: c.ShortName, DisplayName: c.UserID, Type: UserMember, State: UserActive, CreatedAt: now}
		m.users[lc] = u
		m.shortNames[u.ShortNameLC()] = lc
		m.idents[ident] = lc
	}
	ag := &Agent{ID: agentID, OwnerLC: lc, Name: c.AgentName, Origin: OriginSelfhost, State: AgentActive, CredentialHash: hash, CreatedAt: now}
	m.agents[ag.ID] = ag
	m.creds[hash] = ag.ID
	c.UserID = u.UserID
	c.ShortName = u.ShortName
	c.LastError = ""
	c.State = "credential_ready"
	c.AgentID = ag.ID
	c.CredentialHash = hash
	m.audit = append(m.audit, AuditEvent{Actor: lc, Action: "enroll.feishu_claim", Target: c.ID, At: now})
	cp := *c
	return &cp, nil
}
func (m *MemStore) AckDeviceClaim(_ context.Context, id, tokenHash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.claims[id]
	if c == nil || subtle.ConstantTimeCompare([]byte(c.PollTokenHash), []byte(tokenHash)) != 1 {
		return ErrNotFound
	}
	if !now.Before(c.ExpiresAt) {
		return ErrExpired
	}
	if c.State == "completed" {
		return nil
	}
	if c.State != "credential_ready" {
		return ErrNotPending
	}
	c.State = "completed"
	return nil
}

func (m *MemStore) DeviceClaimError(_ context.Context, id, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.claims[id]
	if c == nil {
		return ErrNotFound
	}
	if c.State == "awaiting_feishu" {
		c.LastError = code
	}
	return nil
}
