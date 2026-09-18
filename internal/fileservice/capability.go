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

type uploadTicket struct {
	UploadGrant
	used bool
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
	if method == "GET" {
		if _, _, err := parseSingleRange(byteRange, -1); err != nil {
			return "", Capability{}, ErrCapabilityDenied
		}
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

// UploadTicketStore is a separate audience from retrieval capabilities. A
// ticket is opaque, short-lived, session-bound, size-bound, and single-use.
type UploadTicketStore struct {
	mu      sync.Mutex
	tickets map[[32]byte]uploadTicket
	clock   Clock
	maxAge  time.Duration
}

func NewUploadTicketStore(clock Clock, maxAge time.Duration) *UploadTicketStore {
	return &UploadTicketStore{tickets: make(map[[32]byte]uploadTicket), clock: clock, maxAge: maxAge}
}

func (s *UploadTicketStore) Issue(ownerKey, sessionName string, maxBytes int64, ttl time.Duration) (string, UploadGrant, error) {
	if s == nil || s.clock == nil || ownerKey == "" || sessionName == "" || maxBytes <= 0 || ttl <= 0 || ttl > s.maxAge {
		return "", UploadGrant{}, ErrCapabilityDenied
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", UploadGrant{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	expiresAt := s.clock.Now().Add(ttl)
	grant := UploadGrant{OwnerKey: ownerKey, SessionName: sessionName, MaxBytes: maxBytes, ExpiresAt: expiresAt}
	s.mu.Lock()
	s.tickets[sha256.Sum256([]byte(raw))] = uploadTicket{UploadGrant: grant}
	s.mu.Unlock()
	return raw, grant, nil
}

func (s *UploadTicketStore) VerifyUpload(raw, sessionName string, contentLength int64, now time.Time) (UploadGrant, error) {
	if s == nil || raw == "" || sessionName == "" || contentLength < 0 {
		return UploadGrant{}, ErrCapabilityDenied
	}
	h := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[h]
	if !ok || ticket.used || !now.Before(ticket.ExpiresAt) || ticket.SessionName != sessionName || contentLength > ticket.MaxBytes {
		return UploadGrant{}, ErrCapabilityDenied
	}
	ticket.used = true
	s.tickets[h] = ticket
	return ticket.UploadGrant, nil
}
