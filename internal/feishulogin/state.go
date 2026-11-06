package feishulogin

import (
	"context"
	"sync"
	"time"
)

// MemoryStateStore is safe for concurrent use in one process. It is intended
// for tests and single-process development, not a multi-instance deployment.
type MemoryStateStore struct {
	mu       sync.Mutex
	attempts map[string]LoginAttempt
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{attempts: make(map[string]LoginAttempt)}
}

func (s *MemoryStateStore) Create(_ context.Context, attempt LoginAttempt) error {
	if attempt.StateHash == "" || attempt.AppID == "" || attempt.ExpiresAt.IsZero() {
		return ErrConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts == nil {
		s.attempts = make(map[string]LoginAttempt)
	}
	if _, exists := s.attempts[attempt.StateHash]; exists {
		return ErrInvalidState
	}
	s.attempts[attempt.StateHash] = attempt
	return nil
}

func (s *MemoryStateStore) Consume(_ context.Context, stateHash string, now time.Time) (LoginAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[stateHash]
	if !ok {
		return LoginAttempt{}, ErrInvalidState
	}
	delete(s.attempts, stateHash)
	if !now.Before(attempt.ExpiresAt) {
		return LoginAttempt{}, ErrStateExpired
	}
	return attempt, nil
}
