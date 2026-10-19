package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

type quotaRouteGateway struct{ calls int }

func (g *quotaRouteGateway) Send(_ context.Context, _ string, c hosted.NodeCommand) (hosted.NodeResult, error) {
	g.calls++
	var p map[string]string
	_ = json.Unmarshal(c.Params, &p)
	p["state"] = "stopped"
	b, _ := json.Marshal(p)
	return hosted.NodeResult{OK: true, Payload: b}, nil
}

func quotaRouteRig(t *testing.T) (hostedQuotaRoutes, *identity.MemStore, string, *quotaRouteGateway) {
	t.Helper()
	ctx := context.Background()
	mem := identity.NewMemStore()
	svc := identity.NewService(mem, nil, nil)
	ob := identity.NewOnboarding(svc)
	n3 := &httpapi.N3{OB: ob}
	for _, u := range []identity.User{
		{UserID: "owner", ShortName: "own", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "admin", ShortName: "adm", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "manager", ShortName: "mgr", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "member", ShortName: "mem", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := mem.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := mem.CreateSession(ctx, identity.Session{UserIDLC: u.UserIDLC(), TokenHash: identity.HashSecret("tok-" + u.UserID), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	digest, _ := identity.PairingDigest(strings.Repeat("01", 16), "123456")
	a, err := svc.Apply(ctx, identity.ApplyRequest{OwnerUserID: "owner", PairingSalt: strings.Repeat("01", 16), PairingDigest: digest, DeviceFingerprint: "fixture-ops", SourceIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.Approve(ctx, "owner", a.ApplicationID, "123456", "prodops", ""); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Poll(ctx, a.ApplicationID, a.PollToken)
	if err != nil {
		t.Fatal(err)
	}
	st := hosted.NewStore(pgtest.Pool(t))
	if err = st.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	rev := strings.Repeat("a", 64)
	if err = r31LegacyUpsertNode(st, ctx, hosted.Node{ID: "nd_release", Name: "release", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	frame := `{"quota_mode":"soft","quota_policy_revision":"` + rev + `","hold_release_version":1,"soft_seats":{"ag_release":{"hold":"disk_limit","hold_id":"` + strings.Repeat("b", 32) + `","stop_state":"stopped","writer_root":"private-secret"}}}`
	if err = r31LegacySetOnline(st, ctx, "nd_release", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	g := &quotaRouteGateway{}
	return hostedQuotaRoutes{access: testHostedAccess(t, "owner"), store: st, gw: g, identity: svc, actor: n3.Actor, owner: "owner", ops: "prodops.own", origin: "https://fixture.invalid"}, mem, p.Credential, g
}

func TestHostedQuotaRealIdentityOwnerOnlyAndOpsReadOnly(t *testing.T) {
	d, mem, credential, g := quotaRouteRig(t)
	mux := http.NewServeMux()
	d.register(mux)
	call := func(method, path, token, bearer, origin, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	path := "/api/console/hosted/nodes/nd_release/holds/ag_release/release"
	body := `{"hold_id":"` + strings.Repeat("b", 32) + `","confirm":true}`
	for _, token := range []string{"", "tok-admin", "tok-manager", "tok-member", "forged-owner"} {
		if code, _ := call("POST", path, token, "", d.origin, body); code != 403 {
			t.Fatalf("nonowner %s=%d", token, code)
		}
	}
	for _, token := range []string{"", "tok-owner"} {
		if code, _ := call("POST", path, token, credential, d.origin, body); code != 403 {
			t.Fatal("Agent sharing owner got release authority")
		}
	}
	if g.calls != 0 {
		t.Fatal("unauthorized command sent")
	}
	if code, _ := call("POST", path, "tok-owner", "", "https://wrong.invalid", body); code != 403 {
		t.Fatal("CSRF accepted")
	}
	for _, b := range []string{`{}`, body + `{}`, `{"hold_id":"` + strings.Repeat("b", 32) + `","confirm":true,"actor":"owner"}`} {
		if code, _ := call("POST", path, "tok-owner", "", d.origin, b); code != 400 {
			t.Fatalf("malformed %d", code)
		}
	}
	if code, b := call("POST", path, "tok-owner", "", d.origin, body); code != 200 || !strings.Contains(b, "released_stopped") || g.calls != 1 {
		t.Fatalf("owner %d %s %d", code, b, g.calls)
	}
	if n, _ := d.store.CountAudit(context.Background(), "node.soft_quota.released", "nd_release"); n != 1 {
		t.Fatal("owner audit missing")
	}
	for _, c := range []struct {
		token, bearer string
		allowed       bool
		release       bool
	}{{"tok-owner", "", true, true}, {"", credential, true, false}, {"tok-manager", "", false, false}, {"tok-member", "", false, false}, {"tok-admin", "", false, false}, {"", "invalid", false, false}} {
		code, b := call("GET", "/api/console/hosted/quota-monitor", c.token, c.bearer, "", "")
		if (code == 200) != c.allowed || strings.Contains(b, "private-secret") {
			t.Fatalf("monitor %d %s", code, b)
		}
		if c.allowed {
			var v struct {
				CanRelease bool `json:"can_release"`
			}
			_ = json.Unmarshal([]byte(b), &v)
			if v.CanRelease != c.release {
				t.Fatal("read-only authority widened")
			}
		}
	}
	if code, _ := call("GET", "/console/hosted-quota", "", credential, "", ""); code != 403 {
		t.Fatal("ops got owner page")
	}
	if code, _ := call("GET", "/console/hosted-quota", "tok-owner", "", "", ""); code != 200 {
		t.Fatal("owner page unavailable")
	}
	if err := mem.RevokeSession(context.Background(), identity.HashSecret("tok-owner"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("POST", path, "tok-owner", "", d.origin, body); code != 403 {
		t.Fatal("revoked owner session accepted")
	}
}
