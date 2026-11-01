package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	cf "github.com/zhangzongchu2019/dingwei/internal/changeflow"
	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

type uiChangeProvider struct {
	mu       sync.Mutex
	f        cf.Snapshot
	allowed  bool
	comments int
}

func (p *uiChangeProvider) WithAccess(_ context.Context, actor string, c cf.Coordinate, _ string, fn func(cf.Reader) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.allowed || actor != "manager" || c != p.f.Coordinate {
		return cf.ErrForbidden
	}
	return fn(p)
}
func (p *uiChangeProvider) Read(context.Context, cf.Coordinate) (cf.Snapshot, error) {
	b, _ := json.Marshal(p.f)
	var f cf.Snapshot
	json.Unmarshal(b, &f)
	return f, nil
}
func (p *uiChangeProvider) UpsertComment(_ context.Context, c cf.Comment) (cf.CommentReceipt, error) {
	p.comments++
	return cf.CommentReceipt{Status: "delivered", Location: "https://provider.invalid/comment/1"}, nil
}
func uiFacts() cf.Snapshot {
	c := cf.Coordinate{Provider: "github", ChangeID: "42", Coordinate: sc.Coordinate{Instance: "github.com", Repository: "owner/repo", ChangeDir: "openspec/changes/demo", Commit: strings.Repeat("a", 40)}}
	rules, _ := cf.ParseRules("", nil)
	return cf.Snapshot{Coordinate: c, CurrentHead: c.Commit, Owner: "manager", Version: "snapshot-1", LabelsKnown: true, ProposalKnown: true, Labels: []string{"risk:privacy"}, Input: cf.RiskInput{Coordinate: c, Base: strings.Repeat("b", 40), Head: c.Commit, Complete: true, Labels: []string{"risk:privacy"}}, Rules: rules, ReviewsKnown: true, RequiredReviewers: []string{"reviewer"}, Reviews: []cf.Review{{Actor: "reviewer", Commit: c.Commit, Decision: "APPROVED", Valid: true, Evidence: "git:review/1"}}, PMKnown: true, TasksKnown: true, Tasks: []cf.Task{{ID: "T01", Actor: "dev", Summary: "handoff", Commit: c.Commit, Evidence: "git:handoff/1", Complete: true}}, ScenariosKnown: true, Scenarios: []string{"S01", "S02"}, Release: cf.Release{ID: "release-1", Environment: "staging", Commit: c.Commit, Coordinate: c, Success: true, Verified: true, Evidence: "report:release/1"}}
}
func changeCall(h *Hub, method, path, body, actor string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, actor))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.HandleChangeEvidence(w, r)
	return w
}
func changeURL(c cf.Coordinate) string {
	q := url.Values{"provider": {c.Provider}, "git_instance": {c.Instance}, "repository": {c.Repository}, "change_dir": {c.ChangeDir}, "commit_sha": {c.Commit}, "change_id": {c.ChangeID}}
	return "/api/console/change-evidence?" + q.Encode()
}
func TestChangeEvidenceHTTPAuthorityAndDefaults(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	c := uiFacts().Coordinate
	if rr := changeCall(h, "GET", changeURL(c), "", "manager", nil); rr.Code != 503 {
		t.Fatal("default activated", rr.Code)
	}
	p := &uiChangeProvider{f: uiFacts(), allowed: true}
	h.ChangeEvidence = &cf.Service{Store: h.Repo.(cf.Store), Provider: p}
	if rr := r7Call(h, "GET", r7URL(c.Coordinate), ""); rr.Code != 503 {
		t.Fatal("R7 default changed", rr.Code)
	}
	for _, actor := range []string{"", "other"} {
		if rr := changeCall(h, "GET", changeURL(c), "", actor, nil); rr.Code != 403 || strings.Contains(rr.Body.String(), c.Commit) {
			t.Fatal("ACL bypass")
		}
	}
	q := map[string]any{"action": "detect", "coordinate": c}
	b, _ := json.Marshal(q)
	rr := changeCall(h, "POST", "/api/console/change-evidence", string(b), "manager", nil)
	if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var v cf.View
	json.Unmarshal(rr.Body.Bytes(), &v)
	if v.State.Reminder == nil {
		t.Fatal("no reminder")
	}
	q["action"] = "ack"
	q["revision"] = v.State.Reminder.Revisions[0].ID
	b, _ = json.Marshal(q)
	rr = changeCall(h, "POST", "/api/console/change-evidence", string(b), "manager", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	for _, bad := range []string{string(b) + "{}", strings.Replace(string(b), "\"action\"", "\"actor\":\"spoof\",\"action\"", 1)} {
		if rr := changeCall(h, "POST", "/api/console/change-evidence", bad, "manager", nil); rr.Code != 400 {
			t.Fatal("unknown fields accepted", rr.Code)
		}
	}
	for _, headers := range []map[string]string{{"Sec-Fetch-Site": "cross-site"}, {"Origin": "https://other.invalid"}} {
		rr := changeCall(h, "POST", "/api/console/change-evidence", string(b), "manager", headers)
		if rr.Code != 400 && rr.Code != 403 {
			t.Fatal("CSRF accepted")
		}
	}
	p.allowed = false
	if rr := changeCall(h, "GET", changeURL(c), "", "manager", nil); rr.Code != 403 || strings.Contains(rr.Body.String(), "RuleVersion") {
		t.Fatal("revoked cache")
	}
	for _, provider := range []cf.Provider{cf.GitHub{}, cf.GongFeng{}} {
		called := false
		e := provider.WithAccess(context.Background(), "manager", c, "comment", func(cf.Reader) error { called = true; return nil })
		if !errors.Is(e, cf.ErrUnavailable) || called {
			t.Fatal("stub used real provider")
		}
	}
}
func TestChangeEvidenceConsoleBrowser(t *testing.T) {
	module := os.Getenv("CHANGEFLOW_PLAYWRIGHT")
	if module == "" {
		t.Skip("set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration")
	}
	h := newConsolePlatformTestHub(t)
	p := &uiChangeProvider{f: uiFacts(), allowed: true}
	h.ChangeEvidence = &cf.Service{Store: h.Repo.(cf.Store), Provider: p}
	mux := http.NewServeMux()
	mux.HandleFunc("/console/change-evidence", func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
		h.HandleChangeEvidencePage(w, r)
	})
	mux.HandleFunc("/api/console/change-evidence", func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
		h.HandleChangeEvidence(w, r)
	})
	mux.HandleFunc("/test/labels", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.f.Labels = append(p.f.Labels, "risk:funds")
		p.f.Input.Labels = p.f.Labels
		w.WriteHeader(204)
	})
	mux.HandleFunc("/test/revoke", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.allowed = false
		p.mu.Unlock()
		w.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.Command("node", "testdata/change_evidence_browser.cjs")
	cmd.Env = append(os.Environ(), "CHANGEFLOW_BASE="+server.URL, "CHANGEFLOW_PLAYWRIGHT="+module)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e := cmd.Run(); e != nil {
		t.Fatalf("browser %v\n%s", e, output.String())
	}
	t.Log(output.String())
}
