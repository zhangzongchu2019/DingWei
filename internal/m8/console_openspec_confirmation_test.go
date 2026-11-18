package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

type r7Authority struct {
	read, committee bool
	err             error
	coordinate      sc.Coordinate
	actor           string
}

func (a *r7Authority) WithAccess(ctx context.Context, actor string, c sc.Coordinate, write bool, fn func(context.Context) error) error {
	a.coordinate = c
	a.actor = actor
	if a.err != nil {
		return a.err
	}
	if !a.read || (write && !a.committee) {
		return sc.ErrForbidden
	}
	return fn(ctx)
}
func r7Request() sc.Request {
	return sc.Request{Coordinate: sc.Coordinate{Instance: "git.code.tencent.com", Repository: "wego-ai/dingwei", ChangeDir: "openspec/changes/example", Commit: strings.Repeat("a", 40)}, OperationID: "r7-test-operation-001", Meaning: "确认本版本范围，<script>alert(1)</script>"}
}
func r7URL(c sc.Coordinate) string {
	return "/api/console/openspec-confirmations?" + url.Values{"git_instance": {c.Instance}, "repository": {c.Repository}, "change_dir": {c.ChangeDir}, "commit_sha": {c.Commit}}.Encode()
}
func r7Call(h *Hub, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
	rr := httptest.NewRecorder()
	h.HandleOpenSpecConfirmations(rr, r)
	return rr
}
func TestR7OpenSpecConfirmationHTTPFactsAndForcePush(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	a := &r7Authority{read: true, committee: true}
	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	h.SpecConfirmations = &sc.Service{Store: h.Repo.(sc.Store), Authority: a, Now: func() time.Time { return now }}
	body, _ := json.Marshal(r7Request())
	rr := r7Call(h, "POST", "/api/console/openspec-confirmations", string(body))
	if rr.Code != 200 {
		t.Fatalf("confirm %d %s", rr.Code, rr.Body)
	}
	var fact sc.Confirmation
	if e := json.Unmarshal(rr.Body.Bytes(), &fact); e != nil {
		t.Fatal(e)
	}
	if fact.Commit != r7Request().Commit || fact.ConfirmedBy != "manager" || !fact.ConfirmedAt.Equal(now) || fact.Meaning != r7Request().Meaning || a.actor != "manager" {
		t.Fatalf("incorrect binding %+v", fact)
	}
	if strings.Contains(rr.Body.String(), "<script>") {
		t.Fatal("JSON not HTML escaped")
	}
	now = now.Add(time.Hour)
	retry := r7Call(h, "POST", "/api/console/openspec-confirmations", string(body))
	if retry.Code != 200 || !bytes.Equal(rr.Body.Bytes(), retry.Body.Bytes()) {
		t.Fatalf("retry changed fact %s", retry.Body)
	}
	c := r7Request().Coordinate
	c.Commit = strings.Repeat("b", 40)
	rr = r7Call(h, "GET", r7URL(c), "")
	if rr.Code != 200 {
		t.Fatalf("list %d %s", rr.Code, rr.Body)
	}
	var result struct {
		Current sc.Coordinate `json:"current"`
		Records []sc.View     `json:"records"`
	}
	if e := json.Unmarshal(rr.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.Current.Commit != c.Commit || len(result.Records) != 1 || result.Records[0].Commit != fact.Commit || result.Records[0].Assessment.Status != sc.Unknown {
		t.Fatalf("branch transferred confirmation %+v", result)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private records cacheable")
	}
	a.read = false
	rr = r7Call(h, "GET", r7URL(c), "")
	if rr.Code != 403 || strings.Contains(rr.Body.String(), fact.Meaning) {
		t.Fatalf("revoked leak %d %s", rr.Code, rr.Body)
	}
	a.read = true
	a.committee = false
	rr = r7Call(h, "POST", "/api/console/openspec-confirmations", string(body))
	if rr.Code != 403 {
		t.Fatalf("management mistaken for committee %d", rr.Code)
	}
	a.err = errors.New("upstream with private detail")
	rr = r7Call(h, "GET", r7URL(c), "")
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "private detail") {
		t.Fatalf("unknown mapping failed open/leaked %s", rr.Body)
	}
}

func TestR52OpenSpecConfirmationTrustedOriginMatrix(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	h.PublicOrigin = "https://dw123.cmjrt.com"
	trusted, err := trustedorigin.ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	h.SpecConfirmations = &sc.Service{Store: h.Repo.(sc.Store), Authority: &r7Authority{read: true, committee: true}}
	cases := []struct {
		name, source, target string
		configured           bool
		want                 int
	}{
		{"empty canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", false, 200},
		{"empty extra port", "https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com:50443", false, 403},
		{"empty case variation", "HTTPS://DW123.CMJRT.COM:50443", "https://DW123.CMJRT.COM:50443", false, 403},
		{"configured canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", true, 200},
		{"configured normalized hit", "HTTPS://DW123.CMJRT.COM:50443", "https://dw123.cmjrt.com:50443", true, 200},
		{"configured different port", "https://dw123.cmjrt.com:50444", "https://dw123.cmjrt.com:50444", true, 403},
		{"configured HTTP impostor", "http://dw123.cmjrt.com:50443", "http://dw123.cmjrt.com:50443", true, 403},
		{"configured missing origin", "", "https://dw123.cmjrt.com:50443", true, 403},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.configured {
				h.TrustedOrigins = trusted
			} else {
				h.TrustedOrigins = nil
			}
			req := r7Request()
			req.OperationID = fmt.Sprintf("r52-origin-matrix-%02d", i)
			body, _ := json.Marshal(req)
			r := httptest.NewRequest("POST", tc.target+"/api/console/openspec-confirmations", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			if tc.source != "" {
				r.Header.Set("Origin", tc.source)
			}
			r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
			w := httptest.NewRecorder()
			h.HandleOpenSpecConfirmations(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
func TestR7OpenSpecConfirmationHTTPRejectsForgeriesAndUnavailable(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	req := r7Request()
	body, _ := json.Marshal(req)
	if rr := r7Call(h, "POST", "/api/console/openspec-confirmations", string(body)); rr.Code != 503 {
		t.Fatalf("unwired backend %d", rr.Code)
	}
	h.SpecConfirmations = &sc.Service{Store: h.Repo.(sc.Store), Authority: &r7Authority{read: true, committee: true}}
	for _, suffix := range []string{`,"confirmed_by":"victim"}`, `,"branch":"main"}`, `,"confirmed_at":"2026-01-01T00:00:00Z"}`} {
		forged := strings.TrimSuffix(string(body), "}") + suffix
		if rr := r7Call(h, "POST", "/api/console/openspec-confirmations", forged); rr.Code != 400 {
			t.Fatalf("forgery %d %s", rr.Code, rr.Body)
		}
	}
	if rr := r7Call(h, "POST", "/api/console/openspec-confirmations", string(body)+" {}"); rr.Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
	for _, extra := range []string{"&branch=main", "&commit_sha=" + strings.Repeat("b", 40), "&ref=HEAD"} {
		if rr := r7Call(h, "GET", r7URL(req.Coordinate)+extra, ""); rr.Code != 400 {
			t.Fatalf("ambiguous ref accepted %d", rr.Code)
		}
	}
	r := httptest.NewRequest("POST", "https://console.example/api/console/openspec-confirmations", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
	rr := httptest.NewRecorder()
	h.HandleOpenSpecConfirmations(rr, r)
	if rr.Code != 403 {
		t.Fatalf("cross origin %d", rr.Code)
	}
	r = httptest.NewRequest("POST", "/api/console/openspec-confirmations", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	h.HandleOpenSpecConfirmations(rr, r)
	if rr.Code != 403 {
		t.Fatal("anonymous handler call reached service")
	}
	// Invalid requests above must not have left any fact behind.
	rr = r7Call(h, "GET", r7URL(req.Coordinate), "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"records":[]`) {
		t.Fatalf("rejected request persisted %s", rr.Body)
	}
}
func TestR7OpenSpecConfirmationPageVersionAndMRSeparation(t *testing.T) {
	h := &Hub{}
	rr := httptest.NewRecorder()
	h.HandleOpenSpecConfirmationPage(rr, httptest.NewRequest("GET", "/console/openspec-confirmations", nil))
	s := rr.Body.String()
	for _, want := range []string{"研发管理委员会确认", "MR 审阅", "所确认 commit", "当前浏览的完整 commit SHA", "fact.commit_sha", "fact.confirmed_by", "fact.confirmed_at", "fact.meaning", "待核对", "textContent", "button id=\"confirm\" disabled"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing UI %s", want)
		}
	}
	if strings.Contains(s, "innerHTML") || strings.Contains(s, "window.location.hash") {
		t.Fatal("unsafe or mutable rendering")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("page cache")
	}
}

// S-R62-CONSOLE-SLIM：委员会版本确认入口按业主裁定摘除（后端路由与数据保留）。
// 原格断言「就绪适配器必须下发菜单入口」，现翻转为「任何适配器状态下菜单都不得
// 出现该入口」；「看见入口不等于获得委员会权限」的 POST 403 判据原样保留。
func TestR7OpenSpecConfirmationSlimMenuNeverExposesEntry(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	get := func() string {
		rr := callConsolePlatform(t, h, "manager", "read", "GET", "/api/console/platform", nil, h.HandleConsolePlatform)
		if rr.Code != 200 {
			t.Fatal(rr.Code)
		}
		return rr.Body.String()
	}
	if strings.Contains(get(), "/console/openspec-confirmations") {
		t.Fatal("unwired entry exposed")
	}
	h.SpecConfirmations = &sc.Service{Store: h.Repo.(sc.Store)}
	if strings.Contains(get(), "/console/openspec-confirmations") {
		t.Fatal("slimmed entry exposed without authority")
	}
	h.SpecConfirmations.Authority = &r7Authority{read: true, committee: false}
	if strings.Contains(get(), "/console/openspec-confirmations") {
		t.Fatal("slimmed entry exposed with ready adapter")
	}
	// 直连旧 URL 不 404 化，但平台管理员仍不因此获得委员会权限。
	body, _ := json.Marshal(r7Request())
	if rr := r7Call(h, "POST", "/api/console/openspec-confirmations", string(body)); rr.Code != 403 {
		t.Fatal("navigation granted committee authority")
	}
}
