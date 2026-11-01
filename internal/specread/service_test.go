package specread

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

var testSHA = strings.Repeat("a", 40)

type bindingMock struct {
	b      *Binding
	reads  int
	revoke bool
}

func (b *bindingMock) SpecReadBinding(context.Context, string, string, string) (*Binding, error) {
	b.reads++
	if b.revoke && b.reads > 1 {
		return nil, nil
	}
	return b.b, nil
}
func (b *bindingMock) SetSpecReadBinding(context.Context, string, Binding) error { return nil }
func (b *bindingMock) DeleteSpecReadBinding(context.Context, string, string, string, string) error {
	return nil
}

type mockProvider struct {
	allow               bool
	authErr             error
	revokes             bool
	auths, blobs, trees int
	entries             []Entry
	data                []byte
	resolved, repoID    string
	seen                []string
}

func (p *mockProvider) Repository(_ context.Context, id string) (Repository, error) {
	if p.repoID != "" {
		id = p.repoID
	}
	return Repository{ID: id, FullName: "owner/repo"}, nil
}
func (p *mockProvider) Authorize(context.Context, Repository, Binding) error {
	p.auths++
	if p.authErr != nil {
		return p.authErr
	}
	if !p.allow || (p.revokes && p.auths > 1) {
		return ErrForbidden
	}
	return nil
}
func (p *mockProvider) Resolve(_ context.Context, _ Repository, ref string) (string, error) {
	p.seen = append(p.seen, ref)
	if p.resolved != "" {
		return p.resolved, nil
	}
	return testSHA, nil
}
func (p *mockProvider) Tree(_ context.Context, _ Repository, sha string) ([]Entry, error) {
	p.trees++
	p.seen = append(p.seen, sha)
	return p.entries, nil
}
func (p *mockProvider) Blob(context.Context, Repository, Entry) ([]byte, error) {
	p.blobs++
	return p.data, nil
}
func (p *mockProvider) Source(r Repository, sha, path string) string {
	return "https://github.com/" + r.FullName + "/blob/" + sha + "/" + path
}
func fixture() (*Service, *mockProvider, *bindingMock, Coordinate) {
	b := &bindingMock{b: &Binding{Principal: "alice", Provider: "github", Instance: "github.com", AccountID: "123", Login: "alice"}}
	p := &mockProvider{allow: true, data: []byte("# CSR-001\n- [x] T-01\nEND"), entries: []Entry{{Path: "openspec/spec.md", SHA: testSHA, Type: "blob", Mode: "100644", Size: 26}}}
	p.entries[0].Size = int64(len(p.data))
	s := &Service{Bindings: b, Providers: map[string]Provider{"github:github.com": p}, Repositories: map[string]bool{"github:github.com:1": true, "github:github.com:2": true}}
	return s, p, b, Coordinate{Provider: "github", Instance: "github.com", Repository: "1", Commit: testSHA, Path: "openspec/spec.md"}
}
func TestReadAuthorizationAndCoordinates(t *testing.T) {
	for _, name := range []string{"unbound", "no_read", "outage", "other_principal", "wrong_binding", "repo_identity", "bad_commit", "revoked_during_read", "unbound_during_read", "foreign_repository", "foreign_instance"} {
		t.Run(name, func(t *testing.T) {
			s, p, b, c := fixture()
			switch name {
			case "unbound":
				b.b = nil
			case "no_read":
				p.allow = false
			case "outage":
				p.authErr = ErrUnavailable
			case "other_principal":
				b.b.Principal = "bob"
			case "wrong_binding":
				b.b.Instance = "other"
			case "repo_identity":
				p.repoID = "2"
			case "bad_commit":
				p.resolved = strings.Repeat("b", 40)
			case "revoked_during_read":
				p.revokes = true
			case "unbound_during_read":
				b.revoke = true
			case "foreign_repository":
				c.Repository = "3"
			case "foreign_instance":
				c.Instance = "evil.invalid"
			}
			out, e := s.Read(context.Background(), "alice", c, "")
			if e == nil || out.Text != "" || len(out.Entries) != 0 {
				t.Fatalf("guard failed: err=%v text=%d entries=%d", e, len(out.Text), len(out.Entries))
			}
		})
	}
}
func TestReadRevocationAndCrossRepository(t *testing.T) {
	s, p, _, c := fixture()
	out, e := s.Read(context.Background(), "alice", c, "")
	if e != nil || out.Text != string(p.data) {
		t.Fatal(e)
	}
	p.allow = false
	out, e = s.Read(context.Background(), "alice", c, "")
	if !errors.Is(e, ErrForbidden) || out.Text != "" {
		t.Fatal("revoked read leaked")
	}
	p.allow = true
	c.Repository = "2"
	p.data = []byte("SECOND_REPOSITORY")
	p.entries[0].Size = int64(len(p.data))
	out, e = s.Read(context.Background(), "alice", c, "")
	if e != nil || out.Repository != "2" || out.Text != "SECOND_REPOSITORY" {
		t.Fatal("cross repository result", e)
	}
	p.authErr = ErrUnavailable
	out, e = s.Read(context.Background(), "alice", c, "")
	if e == nil || out.Text != "" {
		t.Fatal("outage returned previous result")
	}
}
func TestReadImmutableRef(t *testing.T) {
	s, p, _, c := fixture()
	c.Commit = ""
	out, e := s.Read(context.Background(), "alice", c, "feature/moving")
	if e != nil || out.Commit != testSHA || p.seen[0] != "feature/moving" || p.seen[1] != testSHA {
		t.Fatal(out, e, p.seen)
	}
}
func TestReadPathAndTypes(t *testing.T) {
	for _, path := range []string{"../secret.md", "openspec/../secret.md", "/openspec/spec.md", "openspec//spec.md", "openspec/%2e%2e/x.md", "openspec/%252fsecret.md", "openspec/a\\b.md", "openspec/a\x00.md", "src/code.md", "https://evil.invalid/a.md", "openspec/a.md?x"} {
		t.Run(path, func(t *testing.T) {
			s, p, _, c := fixture()
			c.Path = path
			if _, e := s.Read(context.Background(), "alice", c, ""); e == nil || p.blobs != 0 || p.trees != 0 {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	for _, mode := range []string{"120000", "160000"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _, c := fixture()
			p.entries[0].Mode = mode
			if mode == "160000" {
				p.entries[0].Type = "commit"
			}
			out, e := s.Read(context.Background(), "alice", c, "")
			if e == nil || p.blobs != 0 || out.Text != "" {
				t.Fatal("followed nonregular file")
			}
		})
	}
}
func TestReadSizeEncodingAndCompleteness(t *testing.T) {
	for _, n := range []int{70000, 70 * 1024, MaxFile, MaxFile + 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, p, _, c := fixture()
			p.data = []byte(strings.Repeat("x", n-3) + "END")
			p.entries[0].Size = int64(n)
			out, e := s.Read(context.Background(), "alice", c, "")
			if n <= MaxFile {
				if e != nil || out.Text != string(p.data) || !strings.HasSuffix(out.Text, "END") {
					t.Fatal("incomplete file", n, e)
				}
			} else if !errors.Is(e, ErrLarge) || p.blobs != 0 || out.Text != "" || !strings.Contains(out.Source, testSHA) {
				t.Fatal("size precheck missing", e, p.blobs)
			}
		})
	}
	for _, name := range []string{"size_mismatch", "invalid_utf8", "actual_overflow"} {
		t.Run(name, func(t *testing.T) {
			s, p, _, c := fixture()
			switch name {
			case "size_mismatch":
				p.data = append(p.data, 'x')
			case "invalid_utf8":
				p.data[0] = 255
			case "actual_overflow":
				p.data = make([]byte, MaxFile+1)
			}
			out, e := s.Read(context.Background(), "alice", c, "")
			if e == nil || out.Text != "" || (name == "actual_overflow" && !errors.Is(e, ErrLarge)) {
				t.Fatal("invalid bytes exposed")
			}
		})
	}
}
func TestGongFengUnavailable(t *testing.T) {
	s, _, _, c := fixture()
	s.Providers["gongfeng:git.code.tencent.com"] = GongFeng{}
	s.Repositories["gongfeng:git.code.tencent.com:1"] = true
	c.Provider = "gongfeng"
	c.Instance = "git.code.tencent.com"
	_, e := s.Read(context.Background(), "alice", c, "")
	if e == nil {
		t.Fatal("gongfeng unexpectedly available")
	}
	if _, e = (GongFeng{}).Repository(context.Background(), "1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
