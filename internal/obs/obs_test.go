package obs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func snapshotJSON(t *testing.T, r *Registry) string {
	t.Helper()
	b, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func findSeries(s Snapshot, server, route, method, class string) *RouteSeries {
	for i := range s.HTTP.Series {
		x := &s.HTTP.Series[i]
		if x.Server == server && x.Route == route && x.Method == method && x.Class == class {
			return x
		}
	}
	return nil
}

func TestRouteSeriesBoundedWithOverflow(t *testing.T) {
	r := New(nil)
	for i := 0; i < 200; i++ {
		r.observeRoute("public", fmt.Sprintf("/r%d", i), "GET", "2xx", 1)
	}
	s := r.Snapshot()
	total := len(s.HTTP.Series) + len(s.HTTP.Overflow)
	if total > MaxRouteSeries {
		t.Fatalf("series=%d overflow=%d total=%d > max %d", len(s.HTTP.Series), len(s.HTTP.Overflow), total, MaxRouteSeries)
	}
	if len(s.HTTP.Overflow) != 1 {
		t.Fatalf("want one overflow bucket, got %d", len(s.HTTP.Overflow))
	}
	kept := uint64(len(s.HTTP.Series))
	if s.HTTP.DroppedSamples != 200-kept || s.HTTP.Overflow[0].LatencyMS.Count != 200-kept {
		t.Fatalf("dropped=%d overflowCount=%d want %d", s.HTTP.DroppedSamples, s.HTTP.Overflow[0].LatencyMS.Count, 200-kept)
	}
	// 已有序列在满额后继续累计，不进溢出。
	before := s.HTTP.DroppedSamples
	r.observeRoute("public", "/r0", "GET", "2xx", 1)
	s = r.Snapshot()
	if s.HTTP.DroppedSamples != before || findSeries(s, "public", "/r0", "GET", "2xx").LatencyMS.Count != 2 {
		t.Fatal("existing series must keep counting after the cap")
	}
}

func TestStreamSeriesBounded(t *testing.T) {
	r := New(nil)
	for i := 0; i < 100; i++ {
		st := r.streamOpened("public", fmt.Sprintf("/s%d", i), "sse")
		r.streamClosed(st, time.Second, 10)
	}
	s := r.Snapshot()
	if len(s.Streams.Series)+1 > MaxStreamSeries || s.Streams.Overflow == nil || s.Streams.DroppedSamples == 0 {
		t.Fatalf("streams=%d overflow=%v dropped=%d", len(s.Streams.Series), s.Streams.Overflow != nil, s.Streams.DroppedSamples)
	}
	if s.Streams.Overflow.Opened != s.Streams.DroppedSamples || s.Streams.Overflow.Active != 0 {
		t.Fatalf("overflow opened=%d dropped=%d active=%d", s.Streams.Overflow.Opened, s.Streams.DroppedSamples, s.Streams.Overflow.Active)
	}
}

func TestIdentityUnknownIsNullNotZero(t *testing.T) {
	r := New(nil)
	r.artifactPath = func() (string, error) { return "", errors.New("no exe") }
	r.SetIdentity(Identity{Service: "workpulse", Version: "9.9.9", Commit: "unknown", DeployID: SanitizeDeployID("bad id with spaces")})
	js := snapshotJSON(t, r)
	for _, want := range []string{`"commit":null`, `"artifact_sha256":null`, `"config_sha256":null`, `"deploy_id":null`, `"version":"9.9.9"`} {
		if !strings.Contains(js, want) {
			t.Fatalf("snapshot missing %s: %s", want, js)
		}
	}
	s := r.Snapshot()
	for _, k := range []string{"commit", "artifact_sha256", "config_sha256", "deploy_id"} {
		if !strings.HasPrefix(s.Identity.Unknown[k], "UNKNOWN") {
			t.Fatalf("unknown[%s]=%q", k, s.Identity.Unknown[k])
		}
	}
	if _, ok := s.Identity.Unknown["version"]; ok {
		t.Fatal("version is known; must not be listed as unknown")
	}
	if len(s.NotInstrumented) == 0 {
		t.Fatal("not_instrumented must be explicit")
	}
	for _, n := range s.NotInstrumented {
		if n.Status != "NOT_INSTRUMENTED" {
			t.Fatalf("area %s status %q", n.Area, n.Status)
		}
	}
}

func TestSanitizeDeployID(t *testing.T) {
	for in, want := range map[string]string{
		"deploy-2026.10.13:a1_b": "deploy-2026.10.13:a1_b",
		"":                       "",
		"x y":                    "",
		"tok=abc&q":              "",
		strings.Repeat("a", 129): "",
	} {
		if got := SanitizeDeployID(in); got != want {
			t.Fatalf("SanitizeDeployID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestArtifactSHAOfExecutable(t *testing.T) {
	r := New(nil)
	r.SetIdentity(Identity{Service: "workpulse"})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s := r.Snapshot()
		if s.Identity.ArtifactSHA256 != nil {
			if len(*s.Identity.ArtifactSHA256) != 64 {
				t.Fatalf("artifact sha %q", *s.Identity.ArtifactSHA256)
			}
			return
		}
		if why := s.Identity.Unknown["artifact_sha256"]; !strings.HasPrefix(why, "UNKNOWN") {
			t.Fatalf("pending artifact must be UNKNOWN with reason, got %q", why)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("artifact sha never computed")
}

// 真 HTTP/1.1 慢对端：客户端发完请求就不读。处理器照 consolemux Gate.write 的写法（ResponseController 设写截止、
// 小块写、ctl.Flush()）——截止到点后，第一个错误必须由 ctl.Flush() 交回（小块写都进缓冲），且在截止后限时内出现。
// 吞掉 flush 错误的包装器会让第一个错误改由后续 Write 暴露，或让处理器以为对端还活着。
func TestMiddlewareSlowPeerWriteDeadlineSurfacesFlushError(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	type result struct {
		from    string
		err     error
		elapsed time.Duration
	}
	got := make(chan result, 1)
	const limit = 400 * time.Millisecond
	mux.HandleFunc("GET /feed", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ctl := http.NewResponseController(w)
		start := time.Now()
		if err := ctl.SetWriteDeadline(start.Add(limit)); err != nil {
			got <- result{"deadline", err, 0}
			return
		}
		chunk := ": " + strings.Repeat("x", 1021) + "\n"
		for time.Since(start) < 15*time.Second {
			if _, err := io.WriteString(w, chunk); err != nil {
				got <- result{"write", err, time.Since(start)}
				return
			}
			if err := ctl.Flush(); err != nil {
				got <- result{"flush", err, time.Since(start)}
				return
			}
		}
		got <- result{"none", nil, time.Since(start)}
	})
	srv := httptest.NewServer(r.Middleware("public", mux))
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(4096)
	}
	if _, err := io.WriteString(conn, "GET /feed HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	res := <-got // 客户端从不读
	if res.from != "flush" || res.err == nil {
		t.Fatalf("first error must come from ResponseController.Flush, got from=%s err=%v after %v", res.from, res.err, res.elapsed)
	}
	if res.elapsed < limit || res.elapsed > limit+5*time.Second {
		t.Fatalf("flush error at %v, want shortly after the %v deadline", res.elapsed, limit)
	}
	t.Logf("slow peer: flush error after %v: %v", res.elapsed, res.err)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := r.Snapshot()
		if len(s.Streams.Series) == 1 && s.Streams.Series[0].Closed == 1 && len(s.HTTP.Series) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("slow-peer stream must be recorded as a stream, not a short request: %+v %+v", r.Snapshot().Streams.Series, r.Snapshot().HTTP.Series)
}

func TestSnapshotTimesDistinct(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	now := t0
	r := New(func() time.Time { return now })
	now = t0.Add(90 * time.Second)
	s := r.Snapshot()
	if !s.ProcessStartedAt.Equal(t0) || !s.SnapshotAt.Equal(now) || s.UptimeSeconds != 90 {
		t.Fatalf("started=%v at=%v uptime=%v", s.ProcessStartedAt, s.SnapshotAt, s.UptimeSeconds)
	}
}

// 中间件：路由标签来自注册模板；状态类区分 401／403／503／超时类；原始路径、query、header 不进快照。
func TestMiddlewareRouteTemplateClassesAndNoSensitiveData(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	mux.HandleFunc("GET /api/forbidden", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	mux.HandleFunc("GET /api/busy", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	mux.HandleFunc("GET /api/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "fine") })
	mux.HandleFunc("GET /api/teapot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) })
	srv := httptest.NewServer(r.Middleware("public", mux))
	defer srv.Close()

	secret := "s3cr3t-session-ABC"
	get := func(path string) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Cookie", "dw_np_session="+secret)
		req.Header.Set("X-DW-Owner", "evil-owner")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	get("/api/items/" + secret + "?token=" + secret)
	get("/api/forbidden")
	get("/api/busy")
	get("/api/ok")
	get("/api/teapot")
	get("/nope/" + secret)

	s := r.Snapshot()
	for _, c := range []struct{ route, class string }{
		{"/api/items/{id}", "401"}, {"/api/forbidden", "403"}, {"/api/busy", "503"}, {"/api/ok", "2xx"}, {"/api/teapot", "4xx"}, {"_unmatched", "404"},
	} {
		if findSeries(s, "public", c.route, "GET", c.class) == nil {
			t.Fatalf("missing series %s %s in %+v", c.route, c.class, s.HTTP.Series)
		}
	}
	js := snapshotJSON(t, r)
	for _, bad := range []string{secret, "token", "evil-owner", "Bearer"} {
		if strings.Contains(js, bad) {
			t.Fatalf("snapshot leaked %q", bad)
		}
	}
}

// 客户端在处理器写出任何东西之前走掉 ⇒ client_gone，不是 2xx。
func TestMiddlewareClientGone(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	done := make(chan struct{})
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
		close(done)
	})
	srv := httptest.NewServer(r.Middleware("public", mux))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/slow", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("expected client timeout")
	}
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if findSeries(r.Snapshot(), "public", "/slow", "GET", "client_gone") != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client_gone not recorded: %+v", r.Snapshot().HTTP.Series)
}

// SSE 处理器直接断言 http.Flusher 与 io.StringWriter，并用 ResponseController 设写截止 —— 包装后必须都还能用；
// 流不进短请求直方图，按流计数。
func TestMiddlewarePreservesSSECapabilities(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	capErr := make(chan error, 1)
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, req *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			capErr <- errors.New("no Flusher")
			return
		}
		sw, ok := w.(io.StringWriter)
		if !ok {
			capErr <- errors.New("no StringWriter")
			return
		}
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			capErr <- fmt.Errorf("SetWriteDeadline: %w", err)
			return
		}
		capErr <- nil
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = sw.WriteString("data: one\n\n")
		fl.Flush()
		_, _ = sw.WriteString("data: two\n\n")
		fl.Flush()
	})
	srv := httptest.NewServer(r.Middleware("internal", mux))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	resp.Body.Close()
	if err := <-capErr; err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "data: one|data: two" {
		t.Fatalf("stream body %q", lines)
	}
	var s Snapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s = r.Snapshot()
		if len(s.Streams.Series) == 1 && s.Streams.Series[0].Closed == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.Streams.Series) != 1 {
		t.Fatalf("streams %+v", s.Streams.Series)
	}
	st := s.Streams.Series[0]
	if st.Route != "/events" || st.Kind != "sse" || st.Opened != 1 || st.Closed != 1 || st.Active != 0 || st.Bytes != uint64(len("data: one\n\ndata: two\n\n")) {
		t.Fatalf("stream series %+v", st)
	}
	if len(s.HTTP.Series) != 0 {
		t.Fatalf("SSE must not enter short-request histogram: %+v", s.HTTP.Series)
	}
}

// WebSocket：coder/websocket 沿 Unwrap／Hijacker 找连接 —— 包装后 Accept 必须成功；升级按流计，不进短请求直方图。
func TestMiddlewarePreservesWebSocketUpgrade(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	accepted := make(chan error, 1)
	mux.HandleFunc("GET /ws/{name}", func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
		accepted <- err
		if err != nil {
			return
		}
		wc := r.OpenWS("session")
		_, _, rerr := conn.Read(req.Context())
		code := websocket.CloseStatus(rerr)
		if code != -1 {
			rerr = PeerClose(int(code))
		}
		wc.Closed(rerr)
	})
	srv := httptest.NewServer(r.Middleware("public", mux))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/agent-secret-name", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("Accept through middleware: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "done")
	var s Snapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s = r.Snapshot()
		if len(s.WS) == 1 && s.WS[0].Closed["peer_normal"] == 1 && len(s.Streams.Series) == 1 && s.Streams.Series[0].Closed == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.WS) != 1 || s.WS[0].Kind != "session" || s.WS[0].Opened != 1 || s.WS[0].Active != 0 || s.WS[0].Closed["peer_normal"] != 1 {
		t.Fatalf("ws %+v", s.WS)
	}
	if len(s.Streams.Series) != 1 || s.Streams.Series[0].Kind != "upgrade" || s.Streams.Series[0].Route != "/ws/{name}" {
		t.Fatalf("streams %+v", s.Streams.Series)
	}
	if len(s.HTTP.Series) != 0 {
		t.Fatalf("upgrade must not enter short-request histogram: %+v", s.HTTP.Series)
	}
	if strings.Contains(snapshotJSON(t, r), "agent-secret-name") {
		t.Fatal("raw path leaked")
	}
}

func TestMiddlewarePanicRecordedAndRepanicked(t *testing.T) {
	r := New(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	mux.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	h := r.Middleware("public", mux)
	for _, p := range []string{"/boom", "/abort"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: panic must propagate to net/http", p)
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
		}()
	}
	s := r.Snapshot()
	if findSeries(s, "public", "/boom", "GET", "panic") == nil || findSeries(s, "public", "/abort", "GET", "aborted") == nil {
		t.Fatalf("%+v", s.HTTP.Series)
	}
}

func TestWSConnReasonsAndPing(t *testing.T) {
	r := New(nil)
	a := r.OpenWS("session")
	a.SetLocalReason("keepalive_failed")
	a.SetLocalReason("replaced") // 先到先得
	a.Closed(PeerClose(1000))
	a.Closed(nil) // 重复调用不重复记

	b := r.OpenWS("session")
	b.Closed(PeerClose(4401))
	c := r.OpenWS("session")
	c.Closed(io.EOF)
	d := r.OpenWS("session")
	d.Closed(ContextDone())
	e := r.OpenWS("weird-kind")
	e.SetLocalReason("made-up")
	e.Closed(nil)

	a.Ping(30*time.Millisecond, nil)
	a.Ping(0, errors.New("timeout"))

	var nilConn *WSConn
	nilConn.SetLocalReason("revoked")
	nilConn.Ping(time.Millisecond, nil)
	nilConn.Closed(nil)

	s := r.Snapshot()
	var sess, other *WSView
	for i := range s.WS {
		switch s.WS[i].Kind {
		case "session":
			sess = &s.WS[i]
		case "other":
			other = &s.WS[i]
		}
	}
	if sess == nil || other == nil {
		t.Fatalf("%+v", s.WS)
	}
	want := map[string]uint64{"keepalive_failed": 1, "peer_app_code": 1, "network_eof": 1, "request_ctx_done": 1}
	for k, v := range want {
		if sess.Closed[k] != v {
			t.Fatalf("closed[%s]=%d want %d (%v)", k, sess.Closed[k], v, sess.Closed)
		}
	}
	if sess.Opened != 4 || sess.Active != 0 || sess.PingOK != 1 || sess.PingFail != 1 || sess.PingRTT.Count != 1 {
		t.Fatalf("session %+v", sess)
	}
	if other.Closed["local_other"] != 1 {
		t.Fatalf("unknown reason must collapse to local_other: %v", other.Closed)
	}
}
