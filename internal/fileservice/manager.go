package fileservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidUpload = errors.New("invalid upload")
	ErrIntegrity     = errors.New("object integrity check failed")
	ErrAccessDenied  = errors.New("file access denied")
	ErrNotReady      = errors.New("file is not ready")
)

type Upload struct {
	OwnerKey, SessionName, OriginalName, MediaType, ExpectedSHA256 string
	ContentLength                                                  int64
	Body                                                           io.Reader
}

type Manager struct {
	Repo                     *Repository
	Objects                  ObjectStore
	Clock                    Clock
	MaxFileBytes, QuotaBytes int64
	Retention                time.Duration
	mu                       sync.Mutex
}

func (m *Manager) Put(ctx context.Context, in Upload) (File, error) {
	if m.Repo == nil || m.Objects == nil || m.Clock == nil || in.Body == nil || in.ContentLength < 0 || in.ContentLength > m.MaxFileBytes || in.OwnerKey == "" || in.SessionName == "" {
		return File{}, ErrInvalidUpload
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, err := randomID()
	if err != nil {
		return File{}, err
	}
	key := "objects/" + id
	h := sha256.New()
	limited := &io.LimitedReader{R: in.Body, N: m.MaxFileBytes + 1}
	reader := io.TeeReader(limited, h)
	if err := m.Objects.Put(ctx, key, reader, in.ContentLength, in.MediaType); err != nil {
		return File{}, err
	}
	cleanup := func() { _ = m.Objects.Delete(context.Background(), key) }
	if limited.N == 0 {
		cleanup()
		return File{}, ErrInvalidUpload
	}
	info, err := m.Objects.Stat(ctx, key)
	if err != nil {
		cleanup()
		return File{}, err
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if info.Size != in.ContentLength || (in.ExpectedSHA256 != "" && !strings.EqualFold(digest, in.ExpectedSHA256)) {
		cleanup()
		return File{}, ErrIntegrity
	}
	now := m.Clock.Now()
	if m.Retention <= 0 {
		cleanup()
		return File{}, ErrInvalidUpload
	}
	f := File{ID: id, ObjectKey: key, OwnerKey: in.OwnerKey, SessionName: in.SessionName, OriginalName: safeName(in.OriginalName), MediaType: in.MediaType, SizeBytes: info.Size, SHA256: digest, State: StateReady, CreatedAt: now, ExpiresAt: now.Add(m.Retention)}
	if err := m.Repo.InsertReady(ctx, f); err != nil {
		cleanup()
		return File{}, err
	}
	if err := m.enforceLocked(ctx); err != nil {
		return File{}, err
	}
	return m.Repo.Get(ctx, id)
}

func (m *Manager) Sweep(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enforceLocked(ctx)
}

// Describe authorizes before consulting metadata. This ordering is deliberate:
// callers without a valid capability get the same answer for unknown, ready,
// expired, and evicted IDs.
func (m *Manager) Describe(ctx context.Context, verifier CapabilityVerifier, raw, method, fileID, byteRange string) (File, error) {
	if m.Repo == nil || m.Clock == nil || verifier == nil {
		return File{}, ErrAccessDenied
	}
	capability, err := verifier.Verify(raw, method, fileID, byteRange, m.Clock.Now())
	if err != nil {
		return File{}, ErrAccessDenied
	}
	f, err := m.Repo.Get(ctx, fileID)
	if err != nil {
		return File{}, ErrAccessDenied
	}
	if capability.Subject != f.OwnerKey+"/"+f.SessionName {
		return File{}, ErrAccessDenied
	}
	if f.State != StateReady {
		return f, ErrNotReady
	}
	return f, nil
}

func (m *Manager) Open(ctx context.Context, verifier CapabilityVerifier, raw, fileID, byteRange string, offset, length int64) (File, io.ReadCloser, error) {
	f, err := m.Describe(ctx, verifier, raw, "GET", fileID, byteRange)
	if err != nil {
		return f, nil, err
	}
	r, err := m.Objects.Open(ctx, f.ObjectKey, offset, length)
	if err != nil {
		return File{}, nil, err
	}
	return f, r, nil
}

func (m *Manager) enforceLocked(ctx context.Context) error {
	candidates, err := m.Repo.RemovalCandidates(ctx, m.Clock.Now(), m.QuotaBytes)
	if err != nil {
		return err
	}
	for _, f := range candidates {
		if err := m.Objects.Delete(ctx, f.ObjectKey); err != nil {
			return fmt.Errorf("delete %s: %w", f.ID, err)
		}
		state, reason := StateEvicted, "quota_fifo"
		if !f.ExpiresAt.After(m.Clock.Now()) {
			state, reason = StateExpired, "retention"
		}
		if err := m.Repo.Tombstone(ctx, f.ID, state, m.Clock.Now(), reason); err != nil {
			return err
		}
	}
	return nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "download"
	}
	return s
}
