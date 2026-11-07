package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type r6Provider struct {
	mu      sync.Mutex
	denied  map[string]bool
	outage  bool
	wrong   bool
	missing bool
	blobs   int
	seen    []string
	auths   []string
}

func (p *r6Provider) Repository(_ context.Context, id string) (specread.Repository, error) {
	return specread.Repository{ID: id, FullName: "test/repo"}, nil
}
func (p *r6Provider) Authorize(_ context.Context, _ specread.Repository, b specread.Binding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.auths = append(p.auths, b.Principal)
	if p.outage {
		return specread.ErrUnavailable
	}
	if p.denied[b.Principal] {
		return specread.ErrForbidden
	}
	return nil
}
func (p *r6Provider) Resolve(_ context.Context, _ specread.Repository, ref string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, ref)
	if p.wrong {
		return strings.Repeat("c", 40), nil
	}
	if p.missing {
		return "", specread.ErrNotFound
	}
	return ref, nil
}
func (p *r6Provider) Tree(_ context.Context, _ specread.Repository, sha string) ([]specread.Entry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, sha)
	out := []specread.Entry{}
	for _, path := range []string{"openspec/specs/cap/context.md", "openspec/specs/cap/spec.md", "openspec/changes/example/proposal.md", "openspec/changes/example/tasks.md"} {
		out = append(out, specread.Entry{Path: path, SHA: sha, Type: "blob", Mode: "100644", Size: int64(71683)})
	}
	return out, nil
}
func (p *r6Provider) Blob(context.Context, specread.Repository, specread.Entry) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blobs++
	return []byte(strings.Repeat("x", 71680) + "END"), nil
}
func (p *r6Provider) Source(_ specread.Repository, sha, path string) string {
	return "https://github.com/test/repo/blob/" + sha + "/" + path
}
func r6Fixture(t *testing.T) (*shimEnv, *r6Provider, cr.Create) {
	t.Helper()
	t.Setenv("DW_OPENSPEC_REVIEW_ENABLED", "1")
	e := newShimEnv(t)
	p := &r6Provider{denied: map[string]bool{}}
	h := e.hub
	for _, id := range []string{"zzc", "lbl"} {
		if err := h.syncNewPlatformUser(e.ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		b := specread.Binding{Principal: id, Provider: "github", Instance: "github.com", AccountID: "1", Login: "owner"}
		if err := e.db.SetSpecReadBinding(e.ctx, "admin", b); err != nil {
			t.Fatal(err)
		}
	}
	h.SpecReader = &specread.Service{Bindings: e.db, Repositories: map[string]bool{"github:github.com:1": true}, Providers: map[string]specread.Provider{"github:github.com": p, "gongfeng:git.code.tencent.com": specread.GongFeng{}}}
	agent := e.agentID("zzc", "Kimi")
	b := specread.Binding{Principal: cr.AgentActor(agent), Provider: "github", Instance: "github.com", AccountID: "2", Login: "reviewer"}
	if err := e.db.SetCoordinateReviewAgentBinding(e.ctx, "admin", agent, &b); err != nil {
		t.Fatal(err)
	}
	h.ConfigureCoordinateReviews(func(context.Context, string, string, string, string) (string, error) { return "receipt", nil })
	return e, p, cr.Create{Coordinate: cr.Coordinate{Provider: "github", Coordinate: sc.Coordinate{Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/example", Commit: strings.Repeat("a", 40)}}, AgentID: agent, Operation: "create-operation-001"}
}
func r6HTTP(e *shimEnv, method, id, action, token, cred, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, npOrigin+"/api/console/coordinate-reviews", strings.NewReader(body))
	r.SetPathValue("requestID", id)
	r.SetPathValue("action", action)
	r.Header.Set("Origin", npOrigin)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	}
	if cred != "" {
		r.Header.Set("Authorization", "Bearer "+cred)
	}
	w := httptest.NewRecorder()
	if cred != "" {
		e.hub.RequireAgentTaskFlow(e.hub.HandleAgentCoordinateReviews)(w, r)
	} else {
		e.hub.RequireConsolePrincipal(e.hub.HandleCoordinateReviews)(w, r)
	}
	return w
}
func r6Create(t *testing.T, e *shimEnv, q cr.Create) cr.Record {
	t.Helper()
	b, _ := json.Marshal(q)
	w := r6HTTP(e, "POST", "", "", e.tokZZC, "", string(b))
	if w.Code != 200 {
		t.Fatalf("R6_CREATE %d %s", w.Code, w.Body)
	}
	var v struct {
		Request cr.Record `json:"request"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Body)
	}
	return v.Request
}
func TestR6DefaultOffMissingDependenciesAndHTTPBounds(t *testing.T) {
	for _, flag := range []string{"", "1"} {
		t.Run("enabled="+flag, func(t *testing.T) {
			t.Setenv("DW_OPENSPEC_REVIEW_ENABLED", flag)
			h := newConsolePlatformTestHub(t)
			h.ConfigureCoordinateReviews(nil)
			for _, fn := range []http.HandlerFunc{h.HandleCoordinateReviews, h.HandleAgentCoordinateReviews, h.HandleCoordinateReviewPage, h.HandleCoordinateReviewAgentBinding} {
				w := httptest.NewRecorder()
				fn(w, httptest.NewRequest("GET", "/", nil))
				if w.Code != 503 {
					t.Fatal("R6_DEFAULT_NOT_DARK", w.Code)
				}
			}
			if h.coordinateReviewEntry(context.Background(), "owner") {
				t.Fatal("R6_DISABLED_ENTRY")
			}
		})
	}
	e, _, q := r6Fixture(t)
	b, _ := json.Marshal(q)
	raw := strings.TrimSuffix(string(b), "}") + `,"source":"secret source"}`
	for _, body := range []string{raw, string(b) + "{}", strings.Repeat("x", 17000)} {
		w := r6HTTP(e, "POST", "", "", e.tokZZC, "", body)
		if w.Code != 400 {
			t.Fatal("R6_001_EXTRA_BODY", w.Code, w.Body)
		}
	}
	req := httptest.NewRequest("POST", npOrigin+"/api/console/coordinate-reviews", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	w := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.HandleCoordinateReviews)(w, req)
	if w.Code != 403 {
		t.Fatal("R6_CSRF", w.Code)
	}
	q.Coordinate.Provider = "gongfeng"
	q.Coordinate.Instance = "git.code.tencent.com"
	w = r6HTTP(e, "POST", "", "", e.tokZZC, "", r6JSON(q))
	if w.Code == 200 {
		t.Fatal("R6_GONGFENG_FAIL_OPEN")
	}
}

func TestR52CoordinateMutationTrustedOriginMatrix(t *testing.T) {
	h := &Hub{PublicOrigin: "https://dw123.cmjrt.com"}
	trusted, err := trustedorigin.ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, source, target string
		configured           bool
		wantAllowed          bool
	}{
		{"empty canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", false, true},
		{"empty extra port", "https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com:50443", false, false},
		{"empty case variation", "HTTPS://DW123.CMJRT.COM:50443", "https://DW123.CMJRT.COM:50443", false, false},
		{"configured canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", true, true},
		{"configured normalized hit", "HTTPS://DW123.CMJRT.COM:50443", "https://dw123.cmjrt.com:50443", true, true},
		{"configured different port", "https://dw123.cmjrt.com:50444", "https://dw123.cmjrt.com:50444", true, false},
		{"configured HTTP impostor", "http://dw123.cmjrt.com:50443", "http://dw123.cmjrt.com:50443", true, false},
		{"configured missing origin", "", "https://dw123.cmjrt.com:50443", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.configured {
				h.TrustedOrigins = trusted
			} else {
				h.TrustedOrigins = nil
			}
			r := httptest.NewRequest(http.MethodPost, tc.target+"/api/console/coordinate-reviews", strings.NewReader("{}"))
			if tc.source != "" {
				r.Header.Set("Origin", tc.source)
			}
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			gotAllowed := h.coordinateMutation(w, r, false)
			if gotAllowed != tc.wantAllowed {
				t.Fatalf("allowed=%v want=%v status=%d body=%s", gotAllowed, tc.wantAllowed, w.Code, w.Body.String())
			}
			if gotAllowed && w.Code != http.StatusOK || !gotAllowed && w.Code != http.StatusForbidden {
				t.Fatalf("status=%d allowed=%v", w.Code, gotAllowed)
			}
		})
	}
}
func r6JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestR6R5IndependentACLRevocationAndForeignCommits(t *testing.T) {
	for _, who := range []string{"owner", "agent", "foreign_owner", "missing_agent_binding", "outage", "foreign_commit", "missing_change"} {
		t.Run(who, func(t *testing.T) {
			e, p, q := r6Fixture(t)
			switch who {
			case "owner":
				p.denied["zzc"] = true
			case "agent":
				p.denied[cr.AgentActor(q.AgentID)] = true
			case "foreign_owner":
				q.AgentID = e.agentID("lbl", "Kimi")
			case "missing_agent_binding":
				_ = e.db.SetCoordinateReviewAgentBinding(e.ctx, "admin", q.AgentID, nil)
			case "outage":
				p.outage = true
			case "foreign_commit":
				p.wrong = true
			case "missing_change":
				q.Coordinate.ChangeDir = "openspec/changes/absent"
			}
			w := r6HTTP(e, "POST", "", "", e.tokZZC, "", r6JSON(q))
			if w.Code == 200 {
				t.Fatal("R6_002_UNAUTHORIZED_DISPATCH", who)
			}
			ids, _ := e.db.CoordinateReviewIDs(e.ctx, "", 100)
			if len(ids) != 0 {
				t.Fatal("R6_002_REJECTED_PERSISTED")
			}
		})
	}
	e, p, q := r6Fixture(t)
	r := r6Create(t, e, q)
	before := p.blobs
	p.denied[cr.AgentActor(q.AgentID)] = true
	for _, action := range []string{"read", "open"} {
		body := "{}"
		if action == "read" {
			body = `{"path":"openspec/specs/cap/context.md"}`
		}
		w := r6HTTP(e, "POST", r.ID, action, "", e.zKimiCred, body)
		if w.Code != 403 {
			t.Fatal("R6_002_REVOKED_READ", w.Code, w.Body)
		}
	}
	if p.blobs != before {
		t.Fatal("R6_002_REVOKED_BLOB")
	}
	w := r6HTTP(e, "GET", r.ID, "", e.tokZZC, "", "")
	if w.Code != 403 || strings.Contains(w.Body.String(), q.Coordinate.Commit) {
		t.Fatal("R6_008_REVOKED_HISTORY")
	}
}
func TestR6HTTPPinnedReadOpenResultsAndMultiReviewer(t *testing.T) {
	e, p, q := r6Fixture(t)
	r := r6Create(t, e, q)
	w := r6HTTP(e, "GET", r.ID, "", "", e.zKimiCred, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"opened_at":"0001-`) {
		t.Fatal("R6_005_GET_MARKED_OPEN", w.Code, w.Body)
	}
	w = r6HTTP(e, "POST", r.ID, "open", "", e.lKimiCr, "{}")
	if w.Code != 403 {
		t.Fatal("R6_005_OTHER_PERSON_OPEN", w.Code)
	}
	w = r6HTTP(e, "POST", r.ID, "open", "", e.zKimiCred, "{}")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, path := range []string{"openspec/specs/cap/context.md", "openspec/specs/cap/spec.md", "openspec/changes/example/proposal.md"} {
		w = r6HTTP(e, "POST", r.ID, "read", "", e.zKimiCred, fmt.Sprintf(`{"path":%q}`, path))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "END") {
			t.Fatal("R6_003_PARTIAL_OR_MISSING", w.Code)
		}
	}
	for _, sha := range p.seen {
		if sha != q.Coordinate.Commit {
			t.Fatal("R6_003_HEAD_READ", sha)
		}
	}
	w = r6HTTP(e, "POST", r.ID, "timeline", "", e.zKimiCred, "{}")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	body := `{"operation_id":"result-operation-001","commit_sha":"` + q.Coordinate.Commit + `","verdict":"PASS","evidence":"report:verified-coordinate"}`
	for i := 0; i < 2; i++ {
		w = r6HTTP(e, "POST", r.ID, "result", "", e.zKimiCred, body)
		if w.Code != 200 {
			t.Fatal("R6_004_HTTP_RESULT", w.Code, w.Body)
		}
	}
	q.AgentID = e.agID
	q.Operation = "create-operation-002"
	b := specread.Binding{Principal: cr.AgentActor(q.AgentID), Provider: "github", Instance: "github.com", AccountID: "3", Login: "other"}
	if err := e.db.SetCoordinateReviewAgentBinding(e.ctx, "admin", q.AgentID, &b); err != nil {
		t.Fatal(err)
	}
	r2 := r6Create(t, e, q)
	if len(r2.Results) != 0 {
		t.Fatal("R6_008_FILLED_OTHER")
	}
	w = r6HTTP(e, "GET", "", "", e.tokZZC, "", "")
	if w.Code != 200 || strings.Count(w.Body.String(), `"verdict":"PASS"`) != 1 || strings.Contains(w.Body.String(), "confirmed_version") {
		t.Fatal("R6_008_FALSE_COMMITTEE", w.Code, w.Body)
	}
}
func TestR6ActualReliableEnvelopeCoordinateOnlyAndNoACKCompletion(t *testing.T) {
	e, _, q := r6Fixture(t)
	conn, st, _ := e.dial(e.zKimiCred, "")
	if st != 101 {
		t.Fatal(st)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	if err := writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#u_zzc", Meta: map[string]any{"type": "session_capabilities", "coordinate_review_v1": true, "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.hub.mu.Lock()
		client := e.hub.sessionClients["u_zzc"]["kimi"]
		e.hub.mu.Unlock()
		ready := false
		if client != nil {
			client.stateMu.Lock()
			ready = client.coordinateReviewV1 && client.deliveryACKV1
			client.stateMu.Unlock()
		}
		if ready {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r := r6Create(t, e, q)
	e.hub.processCoordinateReviews(ctx)
	var env model.Envelope
	for {
		env = readEnvelopeIncludingSystem(t, ctx, conn)
		if coordinateReviewEnvelope(env) {
			break
		}
	}
	var fields map[string]any
	if json.Unmarshal([]byte(env.Body), &fields) != nil || len(fields) != 5 || fields["commit_sha"] != q.Coordinate.Commit || fields["provider"] != "github" || fields["change_dir"] != q.Coordinate.ChangeDir {
		t.Fatal("R6_001_SOURCE_IN_ACTUAL_ENVELOPE", env.Body)
	}
	if env.From != "workpulse#u_zzc" || !metaBool(env.Meta, "no_mirror") || metaString(env.Meta, "request_id") != r.ID {
		t.Fatal("R6_001_BAD_ROUTING_METADATA")
	}
	if err := writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#u_zzc", Meta: map[string]any{"type": "delivery_ack", "delivery_id": metaString(env.Meta, "delivery_id")}}); err != nil {
		t.Fatal(err)
	}
	got, err := e.hub.CoordinateReviews.Get(ctx, "zzc", r.ID)
	if err != nil || !got.Opened.IsZero() || !got.Completed.IsZero() || len(got.Results) > 0 {
		t.Fatal("R6_004_ACK_IS_RESULT", err)
	}
	if err = e.hub.RouteEnvelope(ctx, env); err == nil {
		t.Fatal("R6_001_FORGED_SYSTEM_CONTROL")
	}

}
func TestR6AgentBindingsNeverInheritOwner(t *testing.T) {
	e, _, q := r6Fixture(t)
	if err := e.db.SetCoordinateReviewAgentBinding(e.ctx, "admin", q.AgentID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.hub.CoordinateReviews.Create(e.ctx, "zzc", q); !errors.Is(err, cr.ErrForbidden) {
		t.Fatal("R6_AGENT_MAP_MISSING_INHERITED", err)
	}
	if e.hub.coordinateReviewEntry(e.ctx, "zzc") {
		t.Fatal("R6_ENTRY_MISSING_MAP")
	}
}

func TestR6MissingSenderKeepsConfiguredEntryAndAPIDark(t *testing.T) {
	e, _, _ := r6Fixture(t)
	e.hub.ConfigureCoordinateReviews(nil)
	if e.hub.coordinateReviewEntry(e.ctx, "zzc") {
		t.Fatal("R6_DEPENDENCY_FAIL_OPEN")
	}
	w := r6HTTP(e, "GET", "", "", e.tokZZC, "", "")
	if w.Code != 503 {
		t.Fatal("R6_MISSING_SENDER_HTTP", w.Code)
	}
}

func TestR6WorkerPersonalOnlyTrustedContactAndMissingContactFailure(t *testing.T) {
	for _, contact := range []bool{true, false} {
		t.Run(fmt.Sprint("contact=", contact), func(t *testing.T) {
			e, _, q := r6Fixture(t)
			now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			e.hub.CoordinateReviews.Now = func() time.Time { return now }
			sent := 0
			if contact {
				if err := e.svc.Store.CreateRegistrationCode(e.ctx, "zzc", "r6-only-test-digest", now.Add(24*time.Hour)); err != nil {
					t.Fatal(err)
				}
				if _, err := e.svc.Store.RedeemRegistrationCode(e.ctx, "r6-only-test-digest", identity.Identity{Provider: "feishu", Issuer: "test-bot", Subject: "test-reviewer"}, now); err != nil {
					t.Fatal(err)
				}
			}
			e.hub.CoordinateReviewSender = func(_ context.Context, issuer, subject, id, body string) (string, error) {
				sent++
				var fields map[string]any
				if issuer != "test-bot" || subject != "test-reviewer" || !strings.HasPrefix(id, "r6:") || json.Unmarshal([]byte(body), &fields) != nil || len(fields) != 5 || fields["commit_sha"] != q.Coordinate.Commit {
					t.Fatal("R6_005_PERSONAL_SOURCE_LEAK", issuer, subject, body)
				}
				return "receipt-test", nil
			}
			r := r6Create(t, e, q)
			now = now.Add(30 * time.Minute)
			for i := 0; i < 3; i++ {
				e.hub.processCoordinateReviews(e.ctx)
			}
			got, err := e.hub.CoordinateReviews.Get(e.ctx, "zzc", r.ID)
			if err != nil || len(got.Notices) != 1 {
				t.Fatal("R6_005_WORKER_SLOT", err, got.Notices)
			}
			if contact {
				if sent != 1 || got.Notices[0].State != "sent" {
					t.Fatal("R6_005_PERSONAL_NOT_SENT")
				}
			} else if sent != 0 || got.Notices[0].State != "unknown" {
				t.Fatal("R6_005_MISSING_CONTACT_GUESSED")
			}
			w := r6HTTP(e, "POST", r.ID, "open", "", e.zKimiCred, "{}")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			now = now.Add(4 * time.Hour)
			for i := 0; i < 3; i++ {
				e.hub.processCoordinateReviews(e.ctx)
			}
			got, _ = e.hub.CoordinateReviews.Get(e.ctx, "zzc", r.ID)
			if len(got.Notices) != 1 {
				t.Fatal("R6_005_WORKER_IGNORES_OPEN")
			}
		})
	}
}
