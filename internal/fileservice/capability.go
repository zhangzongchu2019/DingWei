package fileservice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrCapabilityDenied = errors.New("capability denied")

type grant struct {
	Capability
	revoked bool
}

// GrantStore holds opaque bearer capabilities by hash so raw tokens cannot
// appear in metadata, diagnostics, or logs. Scheme A intentionally does not
// bind a token to a device: copying it to another host works until expiry or
// revocation. That is a required, documented property of this security tier.
type GrantStore struct {
	mu     sync.Mutex
	grants map[[32]byte]grant
	clock  Clock
	maxAge time.Duration
}

func NewGrantStore(clock Clock, maxAge time.Duration) *GrantStore {
	return &GrantStore{grants: make(map[[32]byte]grant), clock: clock, maxAge: maxAge}
}

func (s *GrantStore) Issue(subject, fileID, method, byteRange string, ttl time.Duration) (string, Capability, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if subject == "" || fileID == "" || (method != "HEAD" && method != "GET") || ttl <= 0 || ttl > s.maxAge {
		return "", Capability{}, ErrCapabilityDenied
	}
	if method == "HEAD" && byteRange != "" {
		return "", Capability{}, ErrCapabilityDenied
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Capability{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	cap := Capability{Subject: subject, FileID: fileID, Method: method, Range: byteRange, ExpiresAt: s.clock.Now().Add(ttl)}
	s.mu.Lock()
	s.grants[sha256.Sum256([]byte(raw))] = grant{Capability: cap}
	s.mu.Unlock()
	return raw, cap, nil
}

func (s *GrantStore) Verify(raw, method, fileID, byteRange string, now time.Time) (Capability, error) {
	if raw == "" {
		return Capability{}, ErrCapabilityDenied
	}
	h := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	g, ok := s.grants[h]
	s.mu.Unlock()
	if !ok || g.revoked || !now.Before(g.ExpiresAt) || g.FileID != fileID || g.Method != strings.ToUpper(method) || g.Range != byteRange {
		return Capability{}, ErrCapabilityDenied
	}
	return g.Capability, nil
}

func (s *GrantStore) Revoke(raw string) {
	h := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	g, ok := s.grants[h]
	if ok {
		g.revoked = true
		s.grants[h] = g
	}
	s.mu.Unlock()
}
