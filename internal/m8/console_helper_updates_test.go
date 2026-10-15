package m8

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleHelperUpdateAuthorizationAndCatalogOnly(t *testing.T) {
	v := newViewEnv(t)
	h := v.hub
	digest := strings.Repeat("a", 64)
	if err := h.RegisterHelperUpdateCatalog(v.ctx, "ops", HelperUpdateCatalog{SHA256: digest, LockSHA256: strings.Repeat("b", 64), Version: "0.2.3.2", URL: "https://files.example/approved.tgz", Platform: "linux"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/helper-updates", h.RequireConsolePrincipal(h.HandleConsoleHelperUpdates))
	mux.HandleFunc("POST /api/console/helper-updates", h.RequireConsolePrincipal(h.HandleConsoleHelperUpdates))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	call := func(method, tok, origin, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/api/console/helper-updates", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	if st, _ := call("GET", "", "", ""); st != 401 {
		t.Fatalf("unauth GET=%d", st)
	}
	for _, origin := range []string{"", "https://evil.example"} {
		if st, _ := call("POST", v.tokZZC, origin, `{}`); st != 403 {
			t.Fatalf("origin=%q status%d", origin, st)
		}
	}
	_, foreign := v.enrollFor("lbl", "other-update-device", "Other")
	body := `{"release_sha256":"` + digest + `","agent_ids":["` + v.agID + `"]}`
	for name, b := range map[string]string{
		"URL injection":   strings.TrimSuffix(body, "}") + `,"url":"https://evil.example/p.tgz"}`,
		"owner injection": strings.TrimSuffix(body, "}") + `,"owner_id":"lbl"}`,
		"trailing":        body + `{}`,
		"too many":        `{"release_sha256":"` + digest + `","agent_ids":[` + strings.Repeat(`"x",`, 50) + `"x"]}`,
	} {
		if st, _ := call("POST", v.tokZZC, npOrigin, b); st != 400 {
			t.Fatalf("%s status%d", name, st)
		}
	}
	if st, raw := call("POST", v.tokZZC, npOrigin, `{"release_sha256":"`+digest+`","agent_ids":["`+foreign+`"]}`); st != 200 || !strings.Contains(raw, `"error":"forbidden"`) {
		t.Fatalf("foreign=%d %s", st, raw)
	}
	if job, _, _ := h.helperUpdateJob(v.ctx, foreign); job != nil {
		t.Fatal("cross owner created job")
	}
	// Manager does not gain cross-owner rights.
	if err := v.svc.Store.SetUserType(v.ctx, "zzc", identity.UserManager); err != nil {
		t.Fatal(err)
	}
	if st, raw := call("POST", v.tokZZC, npOrigin, `{"release_sha256":"`+digest+`","agent_ids":["`+foreign+`"]}`); st != 200 || !strings.Contains(raw, `"error":"forbidden"`) {
		t.Fatalf("manager foreign=%d %s", st, raw)
	}
	conn, _, raw := v.dial(v.cred, "version=0.2.3.1&os=linux")
	if conn == nil {
		t.Fatal(raw)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, h, AgentKeyID("zzc"), "opus50ts")
	st, raw := call("GET", v.tokZZC, "", "")
	if st != 200 {
		t.Fatalf("GET=%d %s", st, raw)
	}
	if strings.Contains(raw, foreign) || strings.Contains(raw, "files.example") || strings.Contains(raw, "health_nonce") || strings.Contains(raw, "lock_sha256") {
		t.Fatalf("unsafe list projection: %s", raw)
	}
	if !strings.Contains(raw, `"eligible":true`) || !strings.Contains(raw, `"version":"0.2.3.1"`) {
		t.Fatalf("online version=%s", raw)
	}
	// Duplicate targets are collapsed, and a mixed batch never calls core for a foreign target.
	st, raw = call("POST", v.tokZZC, npOrigin, `{"release_sha256":"`+digest+`","agent_ids":["`+v.agID+`","`+v.agID+`","`+foreign+`"]}`)
	var response struct {
		Results []struct {
			AgentID string            `json:"agent_id"`
			OK      bool              `json:"ok"`
			Error   string            `json:"error"`
			Job     *consoleHelperJob `json:"job"`
		} `json:"results"`
	}
	if st != 200 || json.Unmarshal([]byte(raw), &response) != nil || len(response.Results) != 2 || !response.Results[0].OK || response.Results[1].Error != "forbidden" {
		t.Fatalf("batch=%d %s", st, raw)
	}
	offer := v.readEnvUntil(conn, time.Second, func(e model.Envelope) bool { return metaString(e.Meta, "action") == "update_self" })
	if offer == nil || metaString(offer.Meta, "agent_id") != v.agID || offer.From != "workpulse#u_zzc" {
		t.Fatalf("actual offer=%+v", offer)
	}
	st, raw = call("GET", v.tokZZC, "", "")
	if st != 200 || !strings.Contains(raw, `"stage":"offered"`) || strings.Contains(raw, `"stage":"confirmed"`) {
		t.Fatalf("status=%d %s", st, raw)
	}
	if job, _, _ := h.helperUpdateJob(v.ctx, foreign); job != nil {
		t.Fatal("mixed batch created foreign job")
	}
	if st, raw := call("POST", v.tokZZC, npOrigin, body); st != 200 || !strings.Contains(raw, `"error":"job_conflict"`) {
		t.Fatalf("repeat=%d %s", st, raw)
	}
}

func TestConsoleHelperUpdaterVersionAndEligibility(t *testing.T) {
	for _, v := range []string{"0.2.2.1", "0.2.3.0", "0.2.3", "0.2.bad.2", "0.3.0.0", ""} {
		if helperUpdaterVersion(v) {
			t.Fatalf("legacy/unknown %q eligible", v)
		}
	}
	for _, v := range []string{"0.2.3.1", "0.2.3.2", "0.2.4.0"} {
		if !helperUpdaterVersion(v) {
			t.Fatalf("supported %q rejected", v)
		}
	}
	e := newAgentEnv(t)
	ag, err := e.svc.Store.GetAgent(e.ctx, e.agID)
	if err != nil {
		t.Fatal(err)
	}
	if v := e.hub.helperAgentView(*ag); v.Eligible || v.Reason != "offline" {
		t.Fatalf("offline=%+v", v)
	}
	conn, _, raw := e.dial(e.cred, "version=0.2.3.0&os=linux")
	if conn == nil {
		t.Fatal(raw)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), ag.NameLC())
	if v := e.hub.helperAgentView(*ag); v.Eligible || v.Reason != "manual_required" {
		t.Fatalf("legacy=%+v", v)
	}
	ag.Origin = identity.OriginHosted
	if v := e.hub.helperAgentView(*ag); v.Eligible || v.Reason != "not_selfhost" {
		t.Fatalf("hosted=%+v", v)
	}
	ag.Origin = identity.OriginSelfhost
	ag.State = identity.AgentSuspended
	if v := e.hub.helperAgentView(*ag); v.Eligible || v.Reason != "inactive" {
		t.Fatalf("inactive=%+v", v)
	}
}

func TestConsoleHelperUpdateHandlerOriginDefense(t *testing.T) {
	v := newViewEnv(t)
	r := httptest.NewRequest("POST", "/api/console/helper-updates", strings.NewReader("{}"))
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	w := httptest.NewRecorder()
	v.hub.HandleConsoleHelperUpdates(w, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "origin_mismatch") {
		t.Fatalf("unguarded handler origin=%d %s", w.Code, w.Body.String())
	}
}

func TestConsoleHelperUpdateProductionAsset(t *testing.T) {
	v := newViewEnv(t)
	w := httptest.NewRecorder()
	v.hub.HandleConsolePage(w, httptest.NewRequest("GET", "/console", nil))
	if !strings.Contains(w.Body.String(), consoleHelperUpdatesJS) || strings.Count(w.Body.String(), "const API='/api/console/helper-updates'") != 1 {
		t.Fatal("production page omitted or duplicated helper update script")
	}
	v.hub.Onboarding = nil
	w = httptest.NewRecorder()
	v.hub.HandleConsolePage(w, httptest.NewRequest("GET", "/console", nil))
	if strings.Contains(w.Body.String(), consoleHelperUpdatesJS) {
		t.Fatal("legacy page unexpectedly enables new-platform updater")
	}
}
