package specread

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(r *http.Request, status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), Request: r}
}
func testGitHub(t *testing.T, f transportFunc) *GitHub {
	t.Helper()
	file := t.TempDir() + "/credential"
	if e := os.WriteFile(file, []byte("simulation-only-not-a-real-token"), 0600); e != nil {
		t.Fatal(e)
	}
	g := NewGitHub(file)
	g.Transport = f
	return g
}
func TestGitHubRepositoryIdentityRedirect(t *testing.T) {
	for _, name := range []string{"correct", "wrong_id", "cross_host", "same_host_wrong_id", "permission_redirect"} {
		t.Run(name, func(t *testing.T) {
			requests := 0
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Host != "api.github.com" {
					t.Fatal("credential sent outside GitHub")
				}
				if r.Header.Get("Authorization") == "" {
					t.Fatal("credential absent")
				}
				if requests == 1 && name != "correct" && name != "wrong_id" {
					resp := reply(r, 301, nil)
					target := "https://api.github.com/repos/moved/repo"
					if name == "cross_host" {
						target = "https://evil.invalid/repos/moved/repo"
					}
					resp.Header.Set("Location", target)
					return resp, nil
				}
				id := 1
				if name == "wrong_id" || name == "same_host_wrong_id" {
					id = 2
				}
				return reply(r, 200, map[string]any{"id": id, "full_name": "moved/repo"}), nil
			})
			var e error
			if name == "permission_redirect" {
				e = g.Authorize(context.Background(), Repository{ID: "1", FullName: "owner/repo"}, Binding{AccountID: "1", Login: "alice"})
			} else {
				_, e = g.Repository(context.Background(), "1")
			}
			if name == "correct" && e != nil {
				t.Fatal(e)
			}
			if name != "correct" && e == nil {
				t.Fatal("unsafe redirect/identity accepted")
			}
			if name == "cross_host" && requests != 1 {
				t.Fatal("cross-host followed")
			}
		})
	}
	// Rename redirect with the same stable id is handled.
	n := 0
	g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
		n++
		if n == 1 {
			v := reply(r, 301, nil)
			v.Header.Set("Location", "https://api.github.com/repos/new/name")
			return v, nil
		}
		return reply(r, 200, map[string]any{"id": 1, "full_name": "new/name"}), nil
	})
	v, e := g.Repository(context.Background(), "1")
	if e != nil || v.FullName != "new/name" || n != 2 {
		t.Fatal("canonical rename", v, e)
	}
}
func TestGitHubPermissionIdentityAndFailure(t *testing.T) {
	for _, name := range []string{"read", "none", "wrong_user", "bad_login", "outage", "denied"} {
		t.Run(name, func(t *testing.T) {
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/repos/owner/repo" {
					return reply(r, 200, map[string]any{"id": 1, "full_name": "owner/repo"}), nil
				}
				id, login, permission, status := 1, "alice", "read", 200
				switch name {
				case "none":
					permission = "none"
				case "wrong_user":
					id = 2
				case "bad_login":
					login = "bob"
				case "outage":
					status = 503
				case "denied":
					status = 403
				}
				return reply(r, status, map[string]any{"permission": permission, "user": map[string]any{"id": id, "login": login}}), nil
			})
			e := g.Authorize(context.Background(), Repository{ID: "1", FullName: "owner/repo"}, Binding{AccountID: "1", Login: "alice"})
			if (e == nil) != (name == "read") {
				t.Fatalf("permission %s err %v", name, e)
			}
		})
	}
}
func TestGitHubRateHeadersAndCredentialRotation(t *testing.T) {
	n := 0
	g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
		n++
		v := reply(r, 200, map[string]any{"id": 1, "full_name": "owner/repo"})
		v.Header.Set("X-RateLimit-Remaining", "0")
		v.Header.Set("X-RateLimit-Reset", "4102444800")
		return v, nil
	})
	_, e := g.Repository(context.Background(), "1")
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Repository(context.Background(), "1")
	if e != ErrLimit || n != 1 {
		t.Fatal("rate header ignored")
	}
	os.Remove(g.CredentialFile)
	if g.Ready() {
		t.Fatal("removed credential still usable")
	}
	_, e = g.Repository(context.Background(), "1")
	if e != ErrUnavailable {
		t.Fatal("credential cached")
	}
}
func TestGitHubSecondaryRate(t *testing.T) {
	for _, status := range []int{403, 429} {
		g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
			v := reply(r, status, nil)
			v.Header.Set("Retry-After", "60")
			return v, nil
		})
		if _, e := g.Repository(context.Background(), "1"); e != ErrLimit {
			t.Fatal("secondary limit", e)
		}
	}
}
func TestGitHubTreeTruncationAndTypes(t *testing.T) {
	root, spec, child := strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	for _, name := range []string{"complete", "fallback", "incomplete", "wrong_tree", "root_symlink"} {
		t.Run(name, func(t *testing.T) {
			calls := []string{}
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.RequestURI())
				switch r.URL.Path {
				case "/repos/o/r/git/commits/" + testSHA:
					return reply(r, 200, map[string]any{"sha": testSHA, "tree": map[string]any{"sha": root}}), nil
				case "/repos/o/r/git/trees/" + root:
					mode, kind := "040000", "tree"
					if name == "root_symlink" {
						mode, kind = "120000", "blob"
					}
					return reply(r, 200, gitTree{SHA: root, Entries: []Entry{{Path: "openspec", SHA: spec, Mode: mode, Type: kind}}}), nil
				case "/repos/o/r/git/trees/" + spec:
					sha := spec
					if name == "wrong_tree" {
						sha = child
					}
					if r.URL.Query().Get("recursive") == "1" {
						return reply(r, 200, gitTree{SHA: sha, Truncated: name == "fallback" || name == "incomplete", Entries: []Entry{{Path: "spec.md", SHA: testSHA, Type: "blob", Mode: "100644", Size: 4}}}), nil
					}
					return reply(r, 200, gitTree{SHA: spec, Truncated: name == "incomplete", Entries: []Entry{{Path: "nested", SHA: child, Type: "tree", Mode: "040000"}}}), nil
				case "/repos/o/r/git/trees/" + child:
					return reply(r, 200, gitTree{SHA: child, Entries: []Entry{{Path: "tasks.md", SHA: testSHA, Type: "blob", Mode: "100644", Size: 4}, {Path: "link.md", SHA: testSHA, Type: "blob", Mode: "120000"}, {Path: "module.md", SHA: testSHA, Type: "commit", Mode: "160000"}}}), nil
				}
				t.Fatal("unexpected request", r.URL)
				return nil, nil
			})
			entries, e := g.Tree(context.Background(), Repository{FullName: "o/r"}, testSHA)
			if name == "complete" || name == "fallback" {
				if e != nil {
					t.Fatal(e)
				}
				if name == "fallback" && (len(entries) != 3 || entries[0].Path != "openspec/nested/tasks.md" || entries[1].Markdown() || entries[2].Markdown()) {
					t.Fatal("incomplete fallback/types", entries)
				}
				if len(calls) < 3 || !strings.HasSuffix(calls[2], "?recursive=1") {
					t.Fatal("recursive missing")
				}
			} else if e == nil {
				t.Fatal("invalid tree accepted")
			}
		})
	}
}
func TestGitHubBlobBinding(t *testing.T) {
	data := []byte("# END")
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	sha := hex.EncodeToString(h.Sum(nil))
	for _, name := range []string{"valid", "sha", "size", "hash", "type", "too_large"} {
		t.Run(name, func(t *testing.T) {
			n := 0
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				n++
				v := map[string]any{"sha": sha, "size": len(data), "encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)}
				switch name {
				case "sha":
					v["sha"] = testSHA
				case "size":
					v["size"] = 0
				case "hash":
					v["content"] = base64.StdEncoding.EncodeToString([]byte("# BAD"))
				}
				return reply(r, 200, v), nil
			})
			entry := Entry{Path: "openspec/a.md", SHA: sha, Size: int64(len(data)), Mode: "100644", Type: "blob"}
			if name == "type" {
				entry.Mode = "120000"
			}
			if name == "too_large" {
				entry.Size = MaxFile + 1
			}
			_, e := g.Blob(context.Background(), Repository{FullName: "o/r"}, entry)
			if (e == nil) != (name == "valid") {
				t.Fatal("blob guard", e)
			}
			if (name == "type" || name == "too_large") && n != 0 {
				t.Fatal("blob precheck missing")
			}
		})
	}
}

func TestGitHubRepositoryNameReuseAndTimeout(t *testing.T) {
	for _, name := range []string{"name_reused", "timeout", "removed"} {
		t.Run(name, func(t *testing.T) {
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				if name == "timeout" {
					return nil, context.DeadlineExceeded
				}
				if name == "removed" {
					return reply(r, 404, nil), nil
				}
				return reply(r, 200, map[string]any{"id": 2, "full_name": "owner/repo", "permission": "admin", "user": map[string]any{"id": 1, "login": "alice"}}), nil
			})
			err := g.Authorize(context.Background(), Repository{ID: "1", FullName: "owner/repo"}, Binding{AccountID: "1", Login: "alice"})
			if err == nil {
				t.Fatal("name reuse or service failure accepted")
			}
			if name == "timeout" && err != ErrTimeout {
				t.Fatal("timeout not distinguished", err)
			}
		})
	}
}
func TestGitHubResourceBoundsAndRedirectRate(t *testing.T) {
	for _, name := range []string{"response_bytes", "tree_entries", "redirect_rate"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				calls++
				switch name {
				case "response_bytes":
					v := reply(r, 200, nil)
					v.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", (8<<20)+1)))
					return v, nil
				case "tree_entries":
					return reply(r, 200, gitTree{SHA: testSHA, Entries: make([]Entry, 10001)}), nil
				default:
					v := reply(r, 301, nil)
					v.Header.Set("Location", "https://api.github.com/repos/owner/repo")
					v.Header.Set("X-RateLimit-Remaining", "0")
					v.Header.Set("X-RateLimit-Reset", "4102444800")
					return v, nil
				}
			})
			var err error
			if name == "tree_entries" {
				_, err = g.tree(context.Background(), Repository{FullName: "o/r"}, testSHA, true)
			} else {
				_, err = g.Repository(context.Background(), "1")
			}
			if err != ErrLimit {
				t.Fatal("unbounded provider result", err)
			}
			if name == "redirect_rate" && calls != 1 {
				t.Fatal("rate-limited redirect followed")
			}
		})
	}
}
