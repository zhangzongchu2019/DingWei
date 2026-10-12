package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func TestClaimHTTPAnonymousApplyPrivatePollAndReceipt(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	svc := identity.NewService(identity.NewMemStore(), func() time.Time { return now }, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = "fixture-key"
	mux := http.NewServeMux()
	(&httpapi.API{Svc: svc, Claims: ob}).Register(mux)
	call := func(method, path, token string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	r := identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "pmagent", CodeDigest: identity.HashSecret("ABCDEFGHJKMN"), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}
	w := call("POST", "/api/enroll/claims", "", r)
	if w.Code != 201 {
		t.Fatalf("apply %d", w.Code)
	}
	var created struct {
		ID    string `json:"claim_id"`
		Token string `json:"poll_token"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	if n, _ := svc.Store.CountUsers(context.Background()); n != 0 {
		t.Fatal("anonymous apply created user")
	}
	path := "/api/enroll/claims/" + created.ID
	if w = call("GET", path, "wrong", nil); w.Code != 404 {
		t.Fatal("unauthorized poll", w.Code)
	}
	ob.HandleDeviceClaim(context.Background(), identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "person"}, "ABCDEFGHJKMN")
	w = call("GET", path, created.Token, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private poll", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["state"] != "credential_ready" || result["credential"] == "" {
		t.Fatal("no credential")
	}
	if w = call("POST", path+"/ack", "wrong", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w = call("POST", path+"/ack", created.Token, nil); w.Code != 204 {
		t.Fatal(w.Code)
	}
	now = now.Add(2 * time.Second)
	w = call("GET", path, created.Token, nil)
	json.Unmarshal(w.Body.Bytes(), &result)
	// JSON decoding into an existing map retains old keys. Inspect fresh raw response too.
	if strings.Contains(w.Body.String(), `"credential"`) {
		t.Fatal("credential exposed after receipt")
	}
}

func TestClaimApplyRejectsAmbiguousJSON(t *testing.T) {
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = "fixture-key"
	mux := http.NewServeMux()
	(&httpapi.API{Svc: svc, Claims: ob}).Register(mux)
	for _, body := range []string{`{"requested_user_id":"alice","requested_user_id":"bob"}`, `{} {}`, `{"unknown":"x"}`, `{"requested_user_id":true}`} {
		request := httptest.NewRequest("POST", "/api/enroll/claims", strings.NewReader(body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatal("ambiguous claim accepted", response.Code)
		}
	}
}

func TestClaimHTTPCancelProofAndIssuedRefusal(t *testing.T) {
	ctx := context.Background()
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = "fixture-key"
	mux := http.NewServeMux()
	(&httpapi.API{Svc: svc, Claims: ob}).Register(mux)
	apply := func(code string) *identity.ClaimResult {
		t.Helper()
		result, err := ob.ApplyDeviceClaim(ctx, identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "desktop", CodeDigest: identity.HashSecret(code), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}, "fixture-ip")
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	cancel := func(id, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/api/enroll/claims/"+id+"/cancel", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, request)
		return w
	}
	result := apply("ABCDEFGHJKMN")
	for _, token := range []string{"", "wrong"} {
		if w := cancel(result.ID, token); w.Code != 404 {
			t.Fatal("cancel authenticated incorrectly", w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		w := cancel(result.ID, result.PollToken)
		var proof map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &proof) != nil || proof["claim_id"] != result.ID || proof["state"] != "cancelled" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing cancellation proof")
		}
	}
	issued := apply("PQRSTUVWXYZ2")
	ob.HandleDeviceClaim(ctx, identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "person"}, "PQRSTUVWXYZ2")
	if w := cancel(issued.ID, issued.PollToken); w.Code != 403 {
		t.Fatal("issued credential could cancel", w.Code)
	}
}
