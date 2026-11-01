// Package specread reads untrusted OpenSpec text at immutable provider coordinates.
// Authorization is never cached. It has no Git write or source-relay capability.
package specread

import (
	"context"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxFile = 2 * 1024 * 1024

var (
	ErrUnavailable = errors.New("仓库读取服务不可用")
	ErrTimeout     = errors.New("提供方读取超时")
	ErrForbidden   = errors.New("未绑定提供方账号或无仓库读取权限")
	ErrInvalid     = errors.New("仓库坐标、路径或文件类型无效")
	ErrLarge       = errors.New("文件过大，请在仓库托管平台查看")
	ErrLimit       = errors.New("提供方限速或目录读取超限")
	ErrEncoding    = errors.New("文件不是有效 UTF-8")
	ErrNotFound    = errors.New("仓库版本或文档不存在")
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	idPattern      = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	loginPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
)

type Binding struct {
	Principal string `json:"principal"`
	Provider  string `json:"provider"`
	Instance  string `json:"instance"`
	AccountID string `json:"account_id"`
	Login     string `json:"login"`
}

func (b Binding) Valid() bool {
	return b.Principal != "" && len(b.Principal) <= 200 && b.Provider == "github" && b.Instance == "github.com" && idPattern.MatchString(b.AccountID) && loginPattern.MatchString(b.Login)
}

type Bindings interface {
	SpecReadBinding(context.Context, string, string, string) (*Binding, error)
	SetSpecReadBinding(context.Context, string, Binding) error
	DeleteSpecReadBinding(context.Context, string, string, string, string) error
}
type Coordinate struct {
	Provider   string `json:"provider"`
	Instance   string `json:"instance"`
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
}
type Repository struct{ ID, FullName string }
type Entry struct {
	Path string `json:"path"`
	SHA  string `json:"sha,omitempty"`
	Type string `json:"type,omitempty"`
	Mode string `json:"mode,omitempty"`
	Size int64  `json:"size"`
}

func (e Entry) Markdown() bool {
	return e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") && ValidPath(e.Path)
}
func ValidPath(p string) bool {
	return len(p) <= 2048 && strings.HasPrefix(p, "openspec/") && strings.HasSuffix(strings.ToLower(p), ".md") && path.Clean(p) == p && !strings.ContainsAny(p, "\\%\x00\r\n\t?#:") && !strings.Contains(p, "//")
}
func ValidSHA(s string) bool { return shaPattern.MatchString(s) }

type Provider interface {
	Repository(context.Context, string) (Repository, error)
	Authorize(context.Context, Repository, Binding) error
	Resolve(context.Context, Repository, string) (string, error)
	Tree(context.Context, Repository, string) ([]Entry, error)
	Blob(context.Context, Repository, Entry) ([]byte, error)
	Source(Repository, string, string) string
}
type Service struct {
	Bindings     Bindings
	Providers    map[string]Provider
	Repositories map[string]bool
}
type Result struct {
	Coordinate
	Source  string  `json:"source"`
	Entries []Entry `json:"entries"`
	Text    string  `json:"text"`
}

// Read is bounded as a whole, including tree fallbacks and both ACL checks.
func (s *Service) Read(ctx context.Context, actor string, c Coordinate, ref string) (Result, error) {
	out := Result{Coordinate: c}
	if s == nil || s.Bindings == nil {
		return out, ErrUnavailable
	}
	p := s.Providers[c.Provider+":"+c.Instance]
	if p == nil {
		return out, ErrUnavailable
	}
	if actor == "" || !s.Repositories[c.Provider+":"+c.Instance+":"+c.Repository] {
		return out, ErrForbidden
	}
	if c.Path != "" && !ValidPath(c.Path) {
		return out, ErrInvalid
	}
	if (c.Commit == "") == (ref == "") || (c.Commit != "" && !ValidSHA(c.Commit)) || len(ref) > 256 || strings.ContainsAny(ref, "\\%\x00\r\n\t?#") || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
		return out, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binding, repo, err := s.AuthorizeRead(ctx, actor, c)
	if err != nil {
		return out, err
	}
	b := &binding
	requested := c.Commit
	if requested == "" {
		requested = ref
	}
	commit, err := p.Resolve(ctx, repo, requested)
	if err != nil {
		return out, err
	}
	if !ValidSHA(commit) || (c.Commit != "" && commit != c.Commit) {
		return out, ErrInvalid
	}
	out.Commit = commit
	out.Source = p.Source(repo, commit, c.Path)
	entries, err := p.Tree(ctx, repo, commit)
	if err != nil {
		return out, err
	}
	found := c.Path == ""
	for _, e := range entries {
		if e.Markdown() {
			out.Entries = append(out.Entries, e)
		}
		if e.Path == c.Path {
			found = true
			if !e.Markdown() || !ValidSHA(e.SHA) || e.Size < 0 {
				return Result{Coordinate: out.Coordinate}, ErrInvalid
			}
			if e.Size > MaxFile {
				return Result{Coordinate: out.Coordinate, Source: out.Source}, ErrLarge
			}
			data, e2 := p.Blob(ctx, repo, e)
			if e2 != nil {
				return Result{Coordinate: out.Coordinate}, e2
			}
			if len(data) > MaxFile {
				return Result{Coordinate: out.Coordinate, Source: out.Source}, ErrLarge
			}
			if int64(len(data)) != e.Size {
				return Result{Coordinate: out.Coordinate}, ErrInvalid
			}
			if !utf8.Valid(data) {
				return Result{Coordinate: out.Coordinate}, ErrEncoding
			}
			out.Text = string(data)
		}
	}
	if !found {
		return Result{Coordinate: out.Coordinate}, ErrNotFound
	}
	// Re-read binding as well: an administrator can unbind while a slow fetch runs.
	current, err := s.Bindings.SpecReadBinding(ctx, actor, c.Provider, c.Instance)
	if err != nil {
		return Result{Coordinate: out.Coordinate}, ErrUnavailable
	}
	if current == nil || *current != *b {
		return Result{Coordinate: out.Coordinate}, ErrForbidden
	}
	if err = p.Authorize(ctx, repo, *current); err != nil {
		return Result{Coordinate: out.Coordinate}, err
	}
	return out, nil
}

// GongFeng deliberately has no credential, SSH, or alternate transport fallback.
type GongFeng struct{}

func (GongFeng) Repository(context.Context, string) (Repository, error) {
	return Repository{}, ErrUnavailable
}
func (GongFeng) Authorize(context.Context, Repository, Binding) error { return ErrUnavailable }
func (GongFeng) Resolve(context.Context, Repository, string) (string, error) {
	return "", ErrUnavailable
}
func (GongFeng) Tree(context.Context, Repository, string) ([]Entry, error) {
	return nil, ErrUnavailable
}
func (GongFeng) Blob(context.Context, Repository, Entry) ([]byte, error) { return nil, ErrUnavailable }
func (GongFeng) Source(Repository, string, string) string                { return "" }
