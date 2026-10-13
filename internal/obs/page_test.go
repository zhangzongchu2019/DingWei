package obs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMonitorPageHeadersAndCSPPinsExactInlineCode(t *testing.T) {
	rec := httptest.NewRecorder()
	MonitorPageHandler(rec, httptest.NewRequest("GET", "/console/monitor", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	h := rec.Header()
	for k, want := range map[string]string{
		"Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff",
		"Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY",
	} {
		if h.Get(k) != want {
			t.Fatalf("%s=%q want %q", k, h.Get(k), want)
		}
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src " + cspHash(monitorPageScript), "style-src " + cspHash(monitorPageStyle),
		"connect-src 'self'", "img-src 'none'", "form-action 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "*") {
		t.Fatalf("CSP too broad: %s", csp)
	}
	body := rec.Body.String()
	// 页面里恰好一段 script、一段 style，且就是哈希过的那两段（改了内容不改哈希，浏览器会拒绝执行）。
	if strings.Count(body, "<script") != 1 || strings.Count(body, "<style") != 1 ||
		!strings.Contains(body, "<script>"+monitorPageScript+"</script>") || !strings.Contains(body, "<style>"+monitorPageStyle+"</style>") {
		t.Fatal("inline script/style must be exactly the hashed constants")
	}
	rec = httptest.NewRecorder()
	MonitorPageHandler(rec, httptest.NewRequest("POST", "/console/monitor", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %d", rec.Code)
	}
}

// 禁止的写法：HTML 注入面、动态执行、外部资源、写操作、额外凭据、版本字面量。
func TestMonitorPageSourceHasNoForbiddenConstructs(t *testing.T) {
	src := monitorPageHTML
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setTimeout('", "setInterval('",
		"http://", "https://", "//cdn", "<script src", "<link", "<img", "<iframe", "localStorage", "sessionStorage", "Authorization", "method: 'POST'",
		"method:'POST'", "'PUT'", "'DELETE'", "sendBeacon", "0.2."} {
		if strings.Contains(src, bad) {
			t.Fatalf("page source contains forbidden %q", bad)
		}
	}
	// 只取同源的快照接口一个地址。
	fetches := regexp.MustCompile(`fetch\(([^,]+),`).FindAllStringSubmatch(monitorPageScript, -1)
	if len(fetches) != 1 || fetches[0][1] != "API" || !strings.Contains(monitorPageScript, "var API = '/api/console/monitor/snapshot';") {
		t.Fatalf("fetch targets %v", fetches)
	}
	for _, need := range []string{"credentials: 'same-origin'", "MAX_BYTES = 1048576", "INTERVAL_MS = 30000", "res.status === 401 || res.status === 403", "inflight", "visibilitychange", "UNKNOWN"} {
		if !strings.Contains(monitorPageScript, need) {
			t.Fatalf("page script missing %q", need)
		}
	}
}

// 内联脚本的语法用 node --check 过一遍（本机没有 node 就跳过并写明）。
func TestMonitorPageScriptSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; inline script syntax not checked")
	}
	f := filepath.Join(t.TempDir(), "monitor.js")
	if err := os.WriteFile(f, []byte(monitorPageScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("node --check: %v %s", err, out)
	}
}
