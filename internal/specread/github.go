package specread

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub uses the public API instance only. Credential paths are server configuration.
// Read on every call so rotation/removal does not leave a live in-memory credential.
type GitHub struct {
	CredentialFile string
	Transport      http.RoundTripper
	mu             sync.Mutex
	blockedUntil   time.Time
	slots          chan struct{}
}

func NewGitHub(file string) *GitHub {
	return &GitHub{CredentialFile: file, slots: make(chan struct{}, 4)}
}
func (g *GitHub) credential() (string, error) {
	if !filepath.IsAbs(g.CredentialFile) {
		return "", ErrUnavailable
	}
	f, err := os.Open(g.CredentialFile)
	if err != nil {
		return "", ErrUnavailable
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 4096 {
		return "", ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	v := strings.TrimSuffix(string(b), "\n")
	if err != nil || len(v) < 10 || strings.ContainsAny(v, "\r\n\x00 \t") {
		return "", ErrUnavailable
	}
	return v, nil
}
func (g *GitHub) Ready() bool { _, e := g.credential(); return e == nil }
func safeAPI(u *url.URL) bool {
	return u.Scheme == "https" && u.Host == "api.github.com" && u.User == nil && u.Fragment == "" && u.Opaque == ""
}
func (g *GitHub) get(ctx context.Context, p string, out any) error {
	if g.slots == nil {
		return ErrUnavailable
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	case <-ctx.Done():
		return ErrTimeout
	}
	token, err := g.credential()
	if err != nil {
		return err
	}
	g.mu.Lock()
	blocked := time.Now().Before(g.blockedUntil)
	g.mu.Unlock()
	if blocked {
		return ErrLimit
	}
	client := &http.Client{Transport: g.Transport, Timeout: 10 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.Response != nil {
			g.noteRate(r.Response)
			g.mu.Lock()
			blocked := time.Now().Before(g.blockedUntil)
			g.mu.Unlock()
			if blocked {
				return ErrLimit
			}
		}
		// Only repository discovery may follow a canonical rename/transfer URL.
		// Other endpoint responses lack repository identity fields: never follow
		// their redirects to a different repository, even on the same API host.
		if len(via) > 3 || !safeAPI(r.URL) || !strings.HasPrefix(via[0].URL.Path, "/repositories/") || (r.URL.Path != via[0].URL.Path && !strings.HasPrefix(r.URL.Path, "/repos/")) {
			return ErrInvalid
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com"+p, nil)
	if err != nil || !safeAPI(req.URL) {
		return ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return ErrLimit
		}
		if errors.Is(err, ErrInvalid) {
			return ErrInvalid
		}
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return ErrTimeout
		}
		return ErrUnavailable
	}
	defer resp.Body.Close()
	until := g.noteRate(resp)
	switch resp.StatusCode {
	case 200:
	case 401, 403:
		if !until.IsZero() {
			return ErrLimit
		}
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 429:
		return ErrLimit
	default:
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return ErrUnavailable
	}
	if len(data) > 8<<20 {
		return ErrLimit
	}
	if json.Unmarshal(data, out) != nil {
		return ErrInvalid
	}
	return nil
}

func (g *GitHub) noteRate(resp *http.Response) time.Time {
	// Inspect each response, not just /rate_limit. Secondary limits can use Retry-After.
	until := time.Time{}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		n, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
		until = time.Unix(n, 0)
		if !until.After(time.Now()) {
			until = time.Now().Add(time.Minute)
		}
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 {
			until = time.Now().Add(time.Duration(min(n, 3600)) * time.Second)
		} else if t, e := http.ParseTime(v); e == nil {
			until = t
		}
	}
	if (resp.StatusCode == 403 || resp.StatusCode == 429) && until.IsZero() {
		until = time.Now().Add(time.Minute)
	}
	g.mu.Lock()
	if until.After(g.blockedUntil) {
		g.blockedUntil = until
	}
	g.mu.Unlock()
	return until
}

var fullNamePattern = regexpFullName()

func regexpFullName() *regexp.Regexp { return regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`) }
func (g *GitHub) Repository(ctx context.Context, id string) (Repository, error) {
	if !idPattern.MatchString(id) {
		return Repository{}, ErrInvalid
	}
	var v struct {
		ID       json.Number `json:"id"`
		FullName string      `json:"full_name"`
	}
	if e := g.get(ctx, "/repositories/"+id, &v); e != nil {
		return Repository{}, e
	}
	if string(v.ID) != id || !fullNamePattern.MatchString(v.FullName) || strings.HasSuffix(v.FullName, "/.") || strings.HasSuffix(v.FullName, "/..") {
		return Repository{}, ErrInvalid
	}
	return Repository{ID: id, FullName: v.FullName}, nil
}
func repoAPI(r Repository) string { return "/repos/" + r.FullName }
func (g *GitHub) Authorize(ctx context.Context, r Repository, b Binding) error {
	// Revalidate the canonical name against the numeric identity before each ACL
	// check (including the final check after content retrieval). A reused old name
	// must never inherit a binding for the original stable repository.
	var identity struct {
		ID       json.Number `json:"id"`
		FullName string      `json:"full_name"`
	}
	if e := g.get(ctx, repoAPI(r), &identity); e != nil {
		return e
	}
	if string(identity.ID) != r.ID || identity.FullName != r.FullName {
		return ErrInvalid
	}

	var v struct {
		Permission string `json:"permission"`
		User       struct {
			ID    json.Number `json:"id"`
			Login string      `json:"login"`
		} `json:"user"`
	}
	if e := g.get(ctx, repoAPI(r)+"/collaborators/"+url.PathEscape(b.Login)+"/permission", &v); e != nil {
		return e
	}
	if string(v.User.ID) != b.AccountID || !strings.EqualFold(v.User.Login, b.Login) {
		return ErrForbidden
	}
	switch v.Permission {
	case "read", "pull", "triage", "write", "push", "maintain", "admin":
		return nil
	}
	return ErrForbidden
}
func (g *GitHub) Resolve(ctx context.Context, r Repository, ref string) (string, error) {
	var v struct {
		SHA string `json:"sha"`
	}
	if e := g.get(ctx, repoAPI(r)+"/commits/"+url.PathEscape(ref), &v); e != nil {
		return "", e
	}
	if !ValidSHA(v.SHA) {
		return "", ErrInvalid
	}
	return v.SHA, nil
}

type gitTree struct {
	SHA       string  `json:"sha"`
	Truncated bool    `json:"truncated"`
	Entries   []Entry `json:"tree"`
}

func (g *GitHub) tree(ctx context.Context, r Repository, sha string, recursive bool) (gitTree, error) {
	var v gitTree
	q := ""
	if recursive {
		q = "?recursive=1"
	}
	e := g.get(ctx, repoAPI(r)+"/git/trees/"+sha+q, &v)
	if e != nil {
		return v, e
	}
	if v.SHA != sha {
		return v, ErrInvalid
	}
	if len(v.Entries) > 10000 {
		return v, ErrLimit
	}
	for _, x := range v.Entries {
		if !ValidSHA(x.SHA) || x.Path == "" || strings.ContainsAny(x.Path, "\\%\x00\r\n\t") || strings.HasPrefix(x.Path, "/") || strings.Contains(x.Path, "../") {
			return v, ErrInvalid
		}
	}
	return v, nil
}
func (g *GitHub) Tree(ctx context.Context, r Repository, commit string) ([]Entry, error) {
	var v struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if e := g.get(ctx, repoAPI(r)+"/git/commits/"+commit, &v); e != nil {
		return nil, e
	}
	if v.SHA != commit || !ValidSHA(v.Tree.SHA) {
		return nil, ErrInvalid
	}
	root, e := g.tree(ctx, r, v.Tree.SHA, false)
	if e != nil {
		return nil, e
	}
	if root.Truncated {
		return nil, ErrLimit
	}
	var spec string
	for _, x := range root.Entries {
		if x.Path == "openspec" {
			if x.Type != "tree" || x.Mode != "040000" {
				return nil, ErrInvalid
			}
			spec = x.SHA
		}
	}
	if spec == "" {
		return nil, ErrNotFound
	}
	all, e := g.tree(ctx, r, spec, true)
	if e != nil {
		return nil, e
	}
	entries := []Entry{}
	if !all.Truncated {
		for _, x := range all.Entries {
			x.Path = "openspec/" + x.Path
			entries = append(entries, x)
		}
		return entries, nil
	}
	// A truncated recursive result is never presented as complete. Walk subtrees
	// by immutable tree IDs; bound both breadth and request count, including cycles.
	type pending struct{ sha, prefix string }
	queue := []pending{{spec, "openspec/"}}
	calls := 0
	for len(queue) > 0 {
		calls++
		if calls > 256 {
			return nil, ErrLimit
		}
		q := queue[0]
		queue = queue[1:]
		t, e := g.tree(ctx, r, q.sha, false)
		if e != nil {
			return nil, e
		}
		if t.Truncated {
			return nil, ErrLimit
		}
		for _, x := range t.Entries {
			if strings.Contains(x.Path, "/") || x.Path == "." || x.Path == ".." {
				return nil, ErrInvalid
			}
			x.Path = q.prefix + x.Path
			if x.Type == "tree" && x.Mode == "040000" {
				queue = append(queue, pending{x.SHA, x.Path + "/"})
			} else {
				entries = append(entries, x)
			}
			if len(entries)+len(queue) > 10000 {
				return nil, ErrLimit
			}
		}
	}
	return entries, nil
}
func (g *GitHub) Blob(ctx context.Context, r Repository, e Entry) ([]byte, error) {
	if !e.Markdown() || !ValidSHA(e.SHA) {
		return nil, ErrInvalid
	}
	if e.Size > MaxFile {
		return nil, ErrLarge
	}
	var v struct {
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := g.get(ctx, repoAPI(r)+"/git/blobs/"+e.SHA, &v); err != nil {
		return nil, err
	}
	if v.SHA != e.SHA || v.Size != e.Size || v.Encoding != "base64" {
		return nil, ErrInvalid
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if err != nil || int64(len(b)) != e.Size {
		return nil, ErrInvalid
	}
	if len(b) > MaxFile {
		return nil, ErrLarge
	}
	sum := sha1.New()
	fmt.Fprintf(sum, "blob %d\x00", len(b))
	sum.Write(b)
	if hex.EncodeToString(sum.Sum(nil)) != e.SHA {
		return nil, ErrInvalid
	}
	return b, nil
}
func (g *GitHub) Source(r Repository, commit, p string) string {
	return "https://github.com/" + r.FullName + "/blob/" + commit + "/" + p
}
