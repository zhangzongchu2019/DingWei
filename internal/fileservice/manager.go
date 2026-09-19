package fileservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
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

var identitySegment = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

func normalizedMedia(mediaType, originalName string) (string, string) {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0]))
	nameExt := strings.TrimPrefix(strings.ToLower(path.Ext(safeName(originalName))), ".")
	dangerousInlineTypes := map[string]bool{
		"text/html": true, "text/xml": true, "application/xhtml+xml": true, "application/xml": true, "image/svg+xml": true,
	}
	if dangerousInlineTypes[base] {
		return "application/octet-stream", "bin"
	}
	dangerousNameExts := map[string]bool{"php": true, "html": true, "htm": true, "xhtml": true, "xml": true, "svg": true, "svgz": true}
	if dangerousNameExts[nameExt] {
		return "application/octet-stream", "bin"
	}
	downloadExts := map[string]string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "docx",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
		"application/msword": "doc", "application/vnd.ms-excel": "xls", "application/vnd.ms-powerpoint": "ppt",
		"application/zip": "zip", "application/x-zip-compressed": "zip",
	}
	if ext, ok := downloadExts[base]; ok {
		return "application/octet-stream", ext
	}
	filenameTextExts := map[string]string{
		"txt": "txt", "md": "md", "csv": "csv", "json": "json", "log": "log", "py": "py", "js": "js", "go": "go",
		"java": "java", "yaml": "yaml", "yml": "yml", "toml": "toml", "ini": "ini", "sh": "sh", "sql": "sql",
	}
	if ext, ok := filenameTextExts[nameExt]; ok {
		return "text/plain", ext
	}
	inlineCandidate := strings.HasPrefix(base, "image/") || strings.HasPrefix(base, "video/") || strings.HasPrefix(base, "audio/") || strings.HasPrefix(base, "text/") || base == "application/pdf" || base == "application/json"
	exts := map[string]string{
		"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/webp": "webp", "image/avif": "avif",
		"image/heic": "heic", "image/bmp": "bmp", "image/tiff": "tiff",
		"video/mp4": "mp4", "video/webm": "webm", "video/ogg": "ogv", "video/quicktime": "mov", "video/x-matroska": "mkv",
		"audio/ogg": "oga", "audio/wav": "wav", "audio/x-wav": "wav", "audio/vnd.wave": "wav",
		"application/pdf": "pdf", "application/json": "json",
		"text/plain": "txt", "text/markdown": "md", "text/csv": "csv", "text/x-python": "py", "text/javascript": "js",
		"text/x-go": "go", "text/x-java-source": "java", "application/yaml": "yaml", "application/toml": "toml",
		"text/x-ini": "ini", "text/x-log": "log", "application/sql": "sql", "application/x-shellscript": "sh",
	}
	if ext, ok := exts[base]; ok {
		return base, ext
	}
	if inlineCandidate {
		if strings.HasPrefix(base, "text/") {
			return base, "txt"
		}
	}
	filenameAudioExts := map[string]string{
		"mp3": "mp3", "m4a": "m4a", "flac": "flac", "aac": "aac", "opus": "opus", "ogg": "oga", "wav": "wav",
	}
	filenameAudioTypes := map[string]string{
		"mp3": "audio/mpeg", "m4a": "audio/mp4", "flac": "audio/flac", "aac": "audio/aac", "opus": "audio/opus",
		"ogg": "audio/ogg", "wav": "audio/wav",
	}
	if objectExt, ok := filenameAudioExts[nameExt]; ok {
		return filenameAudioTypes[nameExt], objectExt
	}
	return "application/octet-stream", "bin"
}

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
	if m.Repo == nil || m.Objects == nil || m.Clock == nil || in.Body == nil || in.ContentLength < 0 || in.ContentLength > m.MaxFileBytes || !identitySegment.MatchString(in.OwnerKey) || !identitySegment.MatchString(in.SessionName) {
		return File{}, ErrInvalidUpload
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, err := randomID()
	if err != nil {
		return File{}, err
	}
	now := m.Clock.Now().UTC()
	mediaType, ext := normalizedMedia(in.MediaType, in.OriginalName)
	key := fmt.Sprintf("%s/%s/%02d/%02d%02d/%s.%s", in.OwnerKey, in.SessionName, now.Year()%100, int(now.Month()), now.Day(), id, ext)
	h := sha256.New()
	limited := &io.LimitedReader{R: in.Body, N: m.MaxFileBytes + 1}
	reader := io.TeeReader(limited, h)
	if err := m.Objects.Put(ctx, key, reader, in.ContentLength, mediaType); err != nil {
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
	expires := now.Add(m.Retention)
	f := File{ID: id, ObjectKey: key, OwnerKey: in.OwnerKey, SessionName: in.SessionName, OriginalName: safeName(in.OriginalName), MediaType: mediaType, SizeBytes: info.Size, SHA256: digest, State: StateReady, CreatedAt: now, ExpiresAt: expires}
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
	s = path.Base(strings.ReplaceAll(s, `\`, "/"))
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == ".." || s == "/" {
		return "download"
	}
	return s
}
