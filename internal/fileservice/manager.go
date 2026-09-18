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
	TombstoneRetention       time.Duration
	ObjectPrefix             string
	mu                       sync.Mutex
}

func (m *Manager) SetArchivedByOwner(ctx context.Context, id string, archived bool, actorPrincipal, ownerKey, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.Repo.Get(ctx, id)
	if err != nil || f.OwnerKey != ownerKey || f.State != StateReady {
		return ErrAccessDenied
	}
	if m.Retention <= 0 {
		return ErrInvalidUpload
	}
	return m.Repo.SetArchivedLocationByOwner(ctx, id, "", archived, actorPrincipal, ownerKey, reason, m.Clock.Now(), m.Retention)
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
	now := m.Clock.Now().UTC()
	prefix := m.ObjectPrefix
	if prefix == "" {
		prefix = "files"
	}
	key := fmt.Sprintf("%s/%02d/%02d%02d/%s-%s", prefix, now.Year()%100, int(now.Month()), now.Day(), id, now.Format("20060102T150405.000000000Z"))
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
	if m.Retention <= 0 {
		cleanup()
		return File{}, ErrInvalidUpload
	}
	// Directory retention is day-granular: a file expires at the start of the
	// next UTC day plus Retention, yielding an actual 60..61 day window.
	expires := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC).Add(m.Retention)
	f := File{ID: id, ObjectKey: key, OwnerKey: in.OwnerKey, SessionName: in.SessionName, OriginalName: safeName(in.OriginalName), MediaType: in.MediaType, SizeBytes: info.Size, SHA256: digest, State: StateReady, CreatedAt: now, ExpiresAt: expires}
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
	deleting, err := m.Repo.ListDeleting(ctx)
	if err != nil {
		return err
	}
	for _, f := range deleting {
		if err := m.finishRemoval(ctx, f); err != nil {
			return err
		}
	}
	candidates, err := m.Repo.RemovalCandidates(ctx, m.Clock.Now(), m.QuotaBytes)
	if err != nil {
		return err
	}
	for _, f := range candidates {
		reason := "quota_fifo"
		if !f.ExpiresAt.After(m.Clock.Now()) {
			reason = "retention"
		}
		if err := m.Repo.MarkDeleting(ctx, f.ID, reason); err != nil {
			return err
		}
		f.State, f.RemovalReason = StateDeleting, reason
		if err := m.finishRemoval(ctx, f); err != nil {
			return err
		}
	}
	if m.TombstoneRetention > 0 {
		if _, err := m.Repo.PurgeTombstones(ctx, m.Clock.Now().Add(-m.TombstoneRetention)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) finishRemoval(ctx context.Context, f File) error {
	current, err := m.Repo.Get(ctx, f.ID)
	if err != nil || current.State != StateDeleting || current.Archived || (current.RemovalReason != "retention" && current.RemovalReason != "quota_fifo") {
		return fmt.Errorf("refuse unsafe delete: %w", firstError(err, ErrAccessDenied))
	}
	if err := m.Objects.Delete(ctx, current.ObjectKey); err != nil {
		return fmt.Errorf("delete %s: %w", f.ID, err)
	}
	if err := m.Repo.FinishDeleting(ctx, f.ID, m.Clock.Now()); err != nil {
		return fmt.Errorf("finish delete %s: %w", f.ID, err)
	}
	return nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
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
