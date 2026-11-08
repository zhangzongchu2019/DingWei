package main

// 本夹具以测试 overlay 在 run 的装配末端加受控负载；停机实现不替换。
// 普通套件明确跳过，专用 runner 开启后使用真实 PG、HTTP、WS 与 Python VS。
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

var hlcFinalFixture func() map[string]any

var hlcReceiverFactory func(<-chan struct{}) (feishu.Receiver, func(context.Context) error, func() map[string]int64)

func hlcLoadSetup(h *m8.Hub, st *store.SQLite, public, internal *http.ServeMux) feishu.Receiver {
	release := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-signals; time.Sleep(2 * time.Second); close(release); signal.Stop(signals) }()
	rx, invoke, counts := hlcReceiverFactory(release)
	var active, completed atomic.Int64
	hlcFinalFixture = func() map[string]any {
		return map[string]any{"callback": counts(), "http_committed": completed.Load(), "http_active": active.Load()}
	}
	slow := func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		<-release
		if err := st.UpsertAppConfig(r.Context(), "hlc-slow-"+r.PathValue("id"), `{"committed":true}`); err != nil {
			http.Error(w, "transaction failed", 500)
			return
		}
		completed.Add(1)
		w.WriteHeader(204)
	}
	public.HandleFunc("GET /hlc/slow/{id}", slow)
	internal.HandleFunc("GET /hlc/slow/{id}", slow)
	public.HandleFunc("POST /hlc/callback", func(w http.ResponseWriter, r *http.Request) {
		if err := invoke(r.Context()); err != nil {
			http.Error(w, "callback failed", 500)
			return
		}
		w.WriteHeader(204)
	})
	public.HandleFunc("GET /hlc/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"hub": h.LifecycleCounts(), "http_active": active.Load(), "http_committed": completed.Load(), "callback": counts(), "ingress": rx.(feishu.ReceiverLifecycle).IngressSnapshot()})
	})
	return rx
}

func TestHLCLoadProcessChild(t *testing.T) {
	if os.Getenv("HLC_LOAD_CHILD") != "1" {
		return
	}
	err := run(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if hlcFinalFixture != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"hlc_fixture_final": hlcFinalFixture()})
	}
	os.Exit(exitCode(err))
}

type hlcLockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *hlcLockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *hlcLockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.b.Bytes()...)
}

// Test-only transport: fail actual close-frame writes to otherwise live peers.
type hlcCloseWriteFailConn struct{ net.Conn }

func (c hlcCloseWriteFailConn) Write(p []byte) (int, error) {
	if len(p) > 0 && p[0] == 0x88 {
		return 0, os.ErrDeadlineExceeded
	}
	return c.Conn.Write(p)
}

type hlcCloseWriteFailListener struct{ net.Listener }

func (l hlcCloseWriteFailListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return hlcCloseWriteFailConn{c}, nil
}

type hlcChild struct {
	cmd              *exec.Cmd
	log              *hlcLockedBuffer
	public, internal string
	ready            time.Time
	connected        time.Time
	done             chan error
}

func hlcStart(t *testing.T, root, dsn, routing, public, internal string) *hlcChild {
	t.Helper()
	c := &hlcChild{public: public, internal: internal, log: &hlcLockedBuffer{}, done: make(chan error, 1)}
	c.cmd = exec.Command(os.Args[0], "-test.run=^TestHLCLoadProcessChild$", "-test.count=1")
	c.cmd.Dir = root
	sock := filepath.Join(root, fmt.Sprintf("notify-%d.sock", time.Now().UnixNano()))
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer notify.Close()
	c.cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TZ=UTC", "HLC_LOAD_CHILD=1",
		"WP_DATA_DIR=" + root, "WP_DB_PATH=" + filepath.Join(root, "hub.db"), "WP_ADDR=" + public, "WP_INTERNAL_ADDR=" + internal,
		"FEISHU_TRANSPORT=fake", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(root, "no-team.md"), "WP_SCHEDULE_PERSONAL_DIR=" + root,
		"WP_CONSOLE_AUTH_ENABLED=false", "DW_NEWPLAT=1", "DW_PUBLIC_ORIGIN=http://" + public, "DW_NEWPLAT_PG_DSN=" + dsn,
		"NOTIFY_SOCKET=" + sock, "VIEW_ROUTING_CONFIG=" + routing, "GOCACHE=" + os.Getenv("GOCACHE"), "GOMODCACHE=" + os.Getenv("GOMODCACHE"), "GOPATH=" + os.Getenv("GOPATH")}
	c.cmd.Stdout = c.log
	c.cmd.Stderr = c.log
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { c.done <- c.cmd.Wait() }()
	t.Cleanup(func() {
		_ = c.cmd.Process.Kill()
		if out := os.Getenv("HLC_MATRIX_EVIDENCE"); out != "" {
			os.WriteFile(filepath.Join(out, fmt.Sprintf("child-%d.log", c.cmd.Process.Pid)), c.log.Bytes(), 0644)
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "http://"+public+"/hlc/status", nil)
		client := &http.Client{Timeout: 200 * time.Millisecond}
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				c.connected = time.Now()
				notify.SetReadDeadline(deadline)
				buffer := make([]byte, 1024)
				for {
					n, _, err := notify.ReadFromUnix(buffer)
					if err != nil {
						t.Fatalf("READY notify: %v", err)
					}
					if strings.Contains(string(buffer[:n]), "READY=1") {
						c.ready = time.Now()
						return c
					}
				}
			}
		}
		select {
		case err := <-c.done:
			t.Fatalf("child startup exit=%v log=%s", err, c.log.Bytes())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child ready deadline log=%s", c.log.Bytes())
	return nil
}

func hlcReadStatus(t *testing.T, c *hlcChild) map[string]any {
	t.Helper()
	res, err := http.Get("http://" + c.public + "/hlc/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var s map[string]any
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
func hlcUntil(t *testing.T, why string, f func() bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture readiness: " + why)
}

func hlcOwner(i int) string { return fmt.Sprintf("hlcowner%d", i/4) }
func hlcName(i int) string  { return fmt.Sprintf("hlcagent%02d", i) }
func hlcFull(i int) string  { return hlcName(i) + "." + hlcOwner(i) }
func hlcCred(i int) string  { return fmt.Sprintf("hlc-isolated-synthetic-agent-credential-%02d", i) }
func hlcToken(i int) string { return fmt.Sprintf("hlc-isolated-synthetic-session-%d", i) }

func hlcSeed(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	ids, pool, closePool, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer closePool()
	ac, _ := repo.NewAccessCtx("test", "hlc-process-matrix")
	for i := 0; i < 5; i++ {
		u := fmt.Sprintf("hlcowner%d", i)
		if err := ids.CreateUser(ctx, identity.User{UserID: u, ShortName: u, Type: identity.UserMember, State: identity.UserActive, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := ids.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret(hlcToken(i)), UserIDLC: u, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		n := hlcName(i)
		_, err := pool.Exec(ctx, ac, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,credential_hash,created_at) VALUES($1,$2,$3,$3,'selfhost','active',$4,clock_timestamp())`, n, hlcOwner(i), n, identity.HashSecret(hlcCred(i)))
		if err != nil {
			t.Fatal(err)
		}
	}
}

type hlcFrame struct {
	Event, ID, Data string
	At              time.Time
}
type hlcStream struct {
	body   io.ReadCloser
	mu     sync.Mutex
	frames []hlcFrame
	done   chan struct{}
	status int
	feed   map[string]any
}

func (s *hlcStream) snapshot() []hlcFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hlcFrame(nil), s.frames...)
}
func hlcOpenSSE(t *testing.T, url, token string) *hlcStream {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("stream status=%d body=%s", res.StatusCode, b)
	}
	s := &hlcStream{body: res.Body, done: make(chan struct{}), status: res.StatusCode}
	go func() {
		defer close(s.done)
		scan := bufio.NewScanner(res.Body)
		scan.Buffer(make([]byte, 4096), 2<<20)
		var f hlcFrame
		for scan.Scan() {
			v := scan.Text()
			if v == "" {
				if f.Data != "" {
					s.mu.Lock()
					f.At = time.Now()
					s.frames = append(s.frames, f)
					s.mu.Unlock()
				}
				f = hlcFrame{}
				continue
			}
			if strings.HasPrefix(v, "data: ") {
				f.Data = v[6:]
			}
			if strings.HasPrefix(v, "event: ") {
				f.Event = v[7:]
			}
			if strings.HasPrefix(v, "id: ") {
				f.ID = v[4:]
			}
		}
	}()
	t.Cleanup(func() { res.Body.Close() })
	return s
}

type hlcAgent struct {
	conn     *websocket.Conn
	done     chan struct{}
	mu       sync.Mutex
	received []string
}

func hlcDialAgent(t *testing.T, c *hlcChild, i int) *hlcAgent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, "ws://"+c.public+"/ws/agent?tool=hlc-fixture&version=0.3.0", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + hlcCred(i)}}})
	if err != nil {
		code := 0
		if res != nil {
			code = res.StatusCode
		}
		t.Fatalf("helper %d dial status=%d err=%v", i, code, err)
	}
	a := &hlcAgent{conn: conn, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		for {
			_, p, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			a.mu.Lock()
			a.received = append(a.received, string(p))
			a.mu.Unlock()
		}
	}()
	t.Cleanup(func() { conn.CloseNow() })
	return a
}
func hlcPublish(t *testing.T, a *hlcAgent, i int, id string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"kind": "assistant_text", "text": id, "delivery_id": id, "turn_id": "hlc-turn"})
	for _, env := range []model.Envelope{
		{ID: id + "-event", Body: string(body), Meta: map[string]any{"type": "session_event"}},
		{ID: id + "-terminal", Body: id, Meta: map[string]any{"type": "terminal_output"}},
	} {
		b, _ := json.Marshal(env)
		if err := a.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
}
func hlcJSON(t *testing.T, method, url, origin, token string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var v map[string]any
	if res.StatusCode < 200 || res.StatusCode > 299 {
		if os.Getenv("HLC_S011_EXPECT_RESET_REJECT") == "1" {
			return map[string]any{"_http_status": res.StatusCode, "_body": string(raw)}
		}
		t.Fatalf("control %s status=%d body=%s", method, res.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func hlcOpenMux(t *testing.T, c *hlcChild, owner int, after map[string]string) *hlcStream {
	t.Helper()
	base := "http://" + c.public
	token := hlcToken(owner)
	ac := hlcJSON(t, "GET", base+"/api/console/event-context", base, token, nil)["auth_context"]
	subs := []map[string]string{}
	for j := 0; j < 4; j++ {
		i := owner*4 + j
		d := map[string]string{"full_name": hlcFull(i), "iframe_instance": fmt.Sprintf("%032x", i+100)}
		if after[hlcFull(i)] != "" {
			d["after"] = after[hlcFull(i)]
		}
		subs = append(subs, d)
	}
	feed := hlcJSON(t, "POST", base+"/api/console/event-feeds", base, token, map[string]any{"request_id": fmt.Sprintf("%032x", owner+200), "auth_context": ac, "console_instance": fmt.Sprintf("%032x", owner+300), "subscriptions": subs})
	stream := hlcOpenSSE(t, base+"/api/console/event-feeds/"+feed["feed_id"].(string)+"/events", token)
	stream.feed = feed
	return stream
}

func hlcRoutingAndVS(t *testing.T, root, internal string) string {
	t.Helper()
	targets := map[string]m8.ViewTarget{}
	sessions := map[string]string{}
	for n := 0; n < 3; n++ {
		name := fmt.Sprintf("hlc-vs%d", n)
		addr := hlcFreeAddress(t)
		forward := filepath.Join(root, name+"-forward")
		reverse := filepath.Join(root, name+"-reverse")
		// Synthetic one-use fixture keys remain in temporary HOME, never in evidence.
		if err := os.WriteFile(forward, bytes.Repeat([]byte("f"), 32), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reverse, bytes.Repeat([]byte("r"), 32), 0600); err != nil {
			t.Fatal(err)
		}
		targets[name] = m8.ViewTarget{URL: "http://" + addr, ViewUnlockV2: true, HubToVSSecretFile: forward, VSToHubSecretFile: reverse}
		t.Logf("RESOURCE view=%s port=%s directory=%s", name, addr, root)
		resourceFile, resourceErr := os.OpenFile(filepath.Join(os.Getenv("HLC_MATRIX_EVIDENCE"), "RESOURCES.txt"), os.O_APPEND|os.O_WRONLY, 0644)
		if resourceErr != nil {
			t.Fatal(resourceErr)
		}
		fmt.Fprintf(resourceFile, "view=%s address=%s directory=%s\n", name, addr, root)
		resourceFile.Close()
		cmd := exec.Command("python3", "view_service.py")
		cmd.Dir = filepath.Join(os.Getenv("HLC_REPO"), "view-service")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TZ=UTC", "VS_PORT=" + strings.Split(addr, ":")[1], "VS_HUB_INTERNAL=http://" + internal, "VS_VIEW_UNLOCK_V2=1", "VS_TARGET_NAME=" + name, "VS_HUB_TO_VS_SECRET_FILE=" + forward, "VS_TO_HUB_SECRET_FILE=" + reverse, "VS_PAGE_DB=" + filepath.Join(root, name+".db"), "VS_STATE_FILE=" + filepath.Join(root, name+".json")}
		log := &hlcLockedBuffer{}
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		hlcUntil(t, "VS listener", func() bool {
			res, e := http.Get("http://" + addr + "/tasks")
			if e == nil {
				res.Body.Close()
				return true
			}
			return false
		})
	}
	for i := 0; i < 20; i++ {
		sessions[hlcFull(i)] = fmt.Sprintf("hlc-vs%d", (i%4)%3)
	}
	path := filepath.Join(root, "routing.json")
	b, _ := json.Marshal(m8.ViewRouting{Default: "hlc-vs0", Targets: targets, Sessions: sessions})
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hlcStop(t *testing.T, c *hlcChild, out, tag string) (shutdownReport, time.Time, time.Time) {
	t.Helper()
	start := time.Now()
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var processErr error
	select {
	case processErr = <-c.done:
	case <-time.After(16 * time.Second):
		t.Fatal("HLC real process exceeded 15s plus 0.5s measurement tolerance")
	}
	ended := time.Now()
	raw := c.log.Bytes()
	os.WriteFile(filepath.Join(out, tag+"-process.log"), raw, 0644)
	report := shutdownReport{}
	var finalFixture map[string]any
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var row struct {
			Shutdown *shutdownReport `json:"shutdown"`
			Fixture  map[string]any  `json:"hlc_fixture_final"`
		}
		if json.Unmarshal(scan.Bytes(), &row) == nil {
			if row.Shutdown != nil {
				report = *row.Shutdown
			}
			if row.Fixture != nil {
				finalFixture = row.Fixture
			}
		}
	}
	result := map[string]any{"signal_utc": start.UTC(), "exit_utc": ended.UTC(), "stop_ms": ended.Sub(start).Milliseconds(), "process_error": fmt.Sprint(processErr), "report": report, "fixture": finalFixture}
	b, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile(filepath.Join(out, tag+"-shutdown.json"), b, 0644)
	t.Logf("%s stop_ms=%d process_error=%v report_exit=%d stages=%+v", tag, ended.Sub(start).Milliseconds(), processErr, report.ExitCode, report.Stages)
	if finalFixture == nil {
		t.Fatal("missing fixture DB call counts")
	}
	callback := finalFixture["callback"].(map[string]any)
	if callback["late_db_calls"] != float64(0) {
		t.Fatalf("S006_POST_SEAL_DB_ZERO: %v", callback)
	}
	expectedExit := 0
	if os.Getenv("HLC_CASE") == "S014-live-failure" {
		expectedExit = 1
	}
	actualExit := 0
	if processErr != nil {
		if e, ok := processErr.(*exec.ExitError); ok {
			actualExit = e.ExitCode()
		} else {
			t.Fatal(processErr)
		}
	}
	if actualExit != expectedExit || report.ExitCode != expectedExit || len(report.Stages) != 5 {
		t.Fatalf("%s UNEXPECTED_EXIT: process=%v report=%+v", tag, processErr, report)
	}
	if ended.Sub(start) > 15500*time.Millisecond {
		t.Fatal("stop total budget exceeded")
	}

	snap := report.Snapshot.(map[string]any)
	servers := snap["servers"].(map[string]any)
	closed := []time.Time{}
	for _, name := range []string{"public", "internal"} {
		entry := servers[name].(map[string]any)
		when, err := time.Parse(time.RFC3339Nano, entry["listener_closed_utc"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if entry["error"] != "nil" || when.Sub(start) > time.Second {
			t.Fatalf("listener/server %s=%v", name, entry)
		}
		closed = append(closed, when)
	}
	diff := closed[0].Sub(closed[1])
	if diff < 0 {
		diff = -diff
	}
	if diff > 100*time.Millisecond {
		t.Fatalf("LISTENER_DELTA: %v", diff)
	}
	for _, kind := range []string{"hub", "http_overlaps_streams", "consumers", "producers"} {
		for label, n := range snap[kind].(map[string]any) {
			if n != float64(0) {
				t.Fatalf("OWNED_WORK_NOT_JOINED: %s/%s=%v", kind, label, n)
			}
		}
	}
	return report, start, ended
}

func TestHLCLoadMatrix(t *testing.T) {
	if os.Getenv("HLC_LOAD_ENABLED") != "1" {
		t.Skip("isolated actual-process matrix: tests/hlc/process_matrix.py")
	}
	ctx := context.Background()
	root := t.TempDir()
	out := os.Getenv("HLC_MATRIX_EVIDENCE")
	if os.Getenv("HLC_REPEAT") != "" && os.Getenv("HLC_REPEAT") != "1" {
		out = filepath.Join(out, fmt.Sprintf("run-%d", time.Now().UnixNano()))
		t.Setenv("HLC_MATRIX_EVIDENCE", out)
	}
	os.MkdirAll(out, 0755)
	dsn := pgtest.DSN(t)
	hlcSeed(t, dsn)
	public, internal := hlcFreeAddress(t), hlcFreeAddress(t)
	pgURL, parseErr := url.Parse(dsn)
	if parseErr != nil {
		t.Fatal("invalid private PG fixture DSN")
	}
	resource := fmt.Sprintf("RESOURCE tmp=%s sqlite=%s public=%s internal=%s pg=%s database=%s", root, filepath.Join(root, "hub.db"), public, internal, pgURL.Host, pgURL.Path)
	t.Log(resource)
	os.WriteFile(filepath.Join(out, "RESOURCES.txt"), []byte(resource+"\n"), 0644)
	routing := hlcRoutingAndVS(t, root, internal)
	t.Logf("RESOURCE home=%s sqlite=%s public=%s internal=%s pg=private-pgtest notify=%s", root, filepath.Join(root, "hub.db"), public, internal, root)
	c := hlcStart(t, root, dsn, routing, public, internal)
	agents := make([]*hlcAgent, 20)
	seed := time.Now().UnixNano()
	random := rand.New(rand.NewSource(seed))
	t.Logf("fixture_random_seed=%d", seed)
	for _, i := range random.Perm(len(agents)) {
		agents[i] = hlcDialAgent(t, c, i)
		hlcPublish(t, agents[i], i, fmt.Sprintf("hlc-before-%02d", i))
	}
	if mode := os.Getenv("HLC_CASE"); mode == "S014" || mode == "S014-hub-first" || mode == "S014-live-failure" {
		hlcUntil(t, "20 helper handlers", func() bool { return hlcReadStatus(t, c)["hub"].(map[string]any)["session_ws"] == float64(20) })
		if mode == "S014" {
			for _, a := range agents {
				_ = a.conn.CloseNow()
			}
		}
		report, _, _ := hlcStop(t, c, out, mode)
		snapshot := report.Snapshot.(map[string]any)["hub_ws_close"].(map[string]any)
		expected := float64(0)
		if mode == "S014" {
			expected = 20
		}
		failed := float64(0)
		if mode == "S014-live-failure" {
			failed = 20
		}
		if snapshot["already_closed"] != expected || snapshot["failed"] != failed {
			t.Fatalf("S014_COUNTS: %v", snapshot)
		}
		records := snapshot["records"].([]any)
		if len(records) != 20 {
			t.Fatalf("S014_RECORD_COUNT: %d", len(records))
		}
		if mode == "S014" || mode == "S014-live-failure" {
			for _, raw := range records {
				if raw.(map[string]any)["error"] == "" {
					t.Fatal("S014_RAW_ERROR_MISSING")
				}
			}
		}
		return
	}
	for i := 0; i < 20; i++ {
		i := i
		hlcUntil(t, fmt.Sprintf("terminal source %d", i), func() bool {
			client := &http.Client{Timeout: time.Second}
			res, e := client.Get("http://" + internal + "/internal/view/" + hlcFull(i) + "/events")
			if e != nil {
				return false
			}
			res.Body.Close()
			return res.StatusCode == 200
		})
	}

	streams := []*hlcStream{}
	for i := 0; i < 100; i++ {
		streams = append(streams, hlcOpenSSE(t, "http://"+internal+"/internal/view/"+hlcFull(i%20)+"/events", ""))
	}
	muxes := []*hlcStream{}
	for owner := 0; owner < 5; owner++ {
		muxes = append(muxes, hlcOpenMux(t, c, owner, nil))
	}
	// Exactly 20 mux/bridge handlers: five public mux writers + fifteen signed VS bridges.
	hlcUntil(t, "100 SSE + 5 mux + 15 bridge + 20 session WS", func() bool {
		s := hlcReadStatus(t, c)
		h := s["hub"].(map[string]any)
		return h["internal_sse"] == float64(100) && h["public_mux"] == float64(5) && h["internal_bridge"] == float64(15) && h["session_ws"] == float64(20)
	})
	baseline := hlcReadStatus(t, c)
	b, _ := json.MarshalIndent(baseline, "", "  ")
	os.WriteFile(filepath.Join(out, os.Getenv("HLC_CASE")+"-load.json"), b, 0644)
	// Keep a real viewer WS for S011's third push channel, beyond the twenty helpers.
	viewCtx, viewCancel := context.WithTimeout(ctx, 3*time.Second)
	defer viewCancel()
	viewer, _, err := websocket.Dial(viewCtx, "ws://"+public+"/ws/view/"+hlcFull(0), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://" + public}, "Cookie": []string{httpapi.SessionCookie + "=" + hlcToken(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	viewDone := make(chan struct{})
	go func() {
		defer close(viewDone)
		for {
			if _, _, err := viewer.Read(ctx); err != nil {
				return
			}
		}
	}()
	defer viewer.CloseNow()
	pending := make(chan int, 101)
	for _, i := range random.Perm(100) {
		go func(i int) {
			addr := public
			if i >= 50 {
				addr = internal
			}
			res, err := http.Get(fmt.Sprintf("http://%s/hlc/slow/%03d", addr, i))
			if err != nil {
				pending <- 0
				return
			}
			res.Body.Close()
			pending <- res.StatusCode
		}(i)
	}
	go func() {
		res, err := http.Post("http://"+public+"/hlc/callback", "application/json", nil)
		if err != nil {
			pending <- 0
			return
		}
		res.Body.Close()
		pending <- res.StatusCode
	}()
	hlcUntil(t, "100 slow HTTP + inflight callback", func() bool {
		s := hlcReadStatus(t, c)
		return s["http_active"] == float64(100) && s["ingress"].(map[string]any)["active_callbacks"] == float64(1)
	})
	report, stopped, exited := hlcStop(t, c, out, os.Getenv("HLC_CASE"))
	for i := 0; i < 101; i++ {
		select {
		case status := <-pending:
			if status != 204 {
				t.Fatalf("finite work response=%d", status)
			}
		case <-time.After(time.Second):
			t.Fatal("finite work failed to return")
		}
	}
	for _, s := range append(streams, muxes...) {
		select {
		case <-s.done:
		case <-time.After(time.Second):
			t.Fatal("stream not ended by shutdown")
		}
	}
	for _, a := range agents {
		select {
		case <-a.done:
		case <-time.After(time.Second):
			t.Fatal("helper WS did not close")
		}
	}
	select {
	case <-viewDone:
	case <-time.After(time.Second):
		t.Fatal("viewer WS not closed")
	}
	snapshot := report.Snapshot.(map[string]any)
	ingress := snapshot["feishu_ingress"].(map[string]any)
	if ingress["active_callbacks"] != float64(0) || ingress["rejected_after_seal"] != float64(1) {
		t.Fatalf("callback final counts=%v", ingress)
	}
	// Reopen the actual old SQLite only after waitpid. Verify all accepted HTTP commits survive.
	db, err := store.OpenSQLite(filepath.Join(root, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	configs, err := db.ListAppConfig(ctx)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, v := range configs {
		if strings.HasPrefix(v.Key, "hlc-slow-") {
			commits++
		}
	}
	if commits != 100 {
		t.Fatalf("durable HTTP commits=%d want100", commits)
	}
	if os.Getenv("HLC_CASE") == "S006" {
		t.Logf("S006 first run: persisted_http=%d SSE=100 public_mux=5 signed_bridge=15 helper_ws=20 callback_rejected=1 callback_active=0", commits)
		return
	}
	// S011 continues with the same durable SQLite/PG, three unchanged VS processes and addresses.
	c2 := hlcStart(t, root, dsn, routing, public, internal)
	ready := c2.ready
	helpersReady := time.Now()
	published := map[string]time.Time{}
	helperRecovery := make([]map[string]any, 0, 20)
	agents2 := make([]*hlcAgent, 20)
	for i := range agents2 {
		dialStarted := time.Now()
		agents2[i] = hlcDialAgent(t, c2, i)
		connected := time.Now()
		helperRecovery = append(helperRecovery, map[string]any{"helper_id": i, "dial_started_utc": dialStarted.UTC(), "connected_utc": connected.UTC(), "from_ready_ms": connected.Sub(ready).Milliseconds()})
		id := fmt.Sprintf("hlc-after-%02d", i)
		published[id] = time.Now()
		hlcPublish(t, agents2[i], i, id)
	}
	helpersReady = time.Now()
	oldCursor := ""
	for _, f := range streams[0].snapshot() {
		if f.ID != "" {
			oldCursor = f.ID
		}
	}
	resumed := hlcOpenSSE(t, "http://"+internal+"/internal/view/"+hlcFull(0)+"/events?after="+oldCursor, "")
	muxResumed := hlcOpenMux(t, c2, 0, map[string]string{hlcFull(0): oldCursor})
	viewer2, _, err := websocket.Dial(ctx, "ws://"+public+"/ws/view/"+hlcFull(0), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://" + public}, "Cookie": []string{httpapi.SessionCookie + "=" + hlcToken(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer2.CloseNow()

	type wsObservation struct {
		at      time.Time
		payload string
	}
	wsFrames := make(chan wsObservation, 20)
	go func() {
		for {
			_, p, e := viewer2.Read(ctx)
			if e != nil {
				return
			}
			wsFrames <- wsObservation{time.Now(), string(p)}
		}
	}()
	published["hlc-after-verify"] = time.Now()
	hlcPublish(t, agents2[0], 0, "hlc-after-verify")
	sseReady, muxReady, wsReady := time.Time{}, time.Time{}, time.Time{}
	first := func(stream *hlcStream) time.Time {
		for _, f := range stream.snapshot() {
			if strings.Contains(f.Data, "hlc-after-verify") {
				return f.At
			}
		}
		return time.Time{}
	}
	for time.Now().Before(ready.Add(5 * time.Second)) {
		sseReady, muxReady = first(resumed), first(muxResumed)
		select {
		case frame := <-wsFrames:
			if strings.Contains(frame.payload, "hlc-after-verify") {
				wsReady = frame.at
			}
		default:
		}
		if !sseReady.IsZero() && !muxReady.IsZero() && !wsReady.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	gap, muxGap, duplicates := 0, 0, 0
	seen := map[string]bool{}
	for _, f := range resumed.snapshot() {
		if f.Event == "replay_gap" {
			gap++
		}
		if f.ID != "" {
			if seen[f.ID] {
				duplicates++
			}
			seen[f.ID] = true
		}
	}
	for _, f := range muxResumed.snapshot() {
		var row map[string]any
		json.Unmarshal([]byte(f.Data), &row)
		if row["type"] == "gap" {
			muxGap++
		}
	}
	countIDs := func(stream *hlcStream, ids []string) (map[string]int, int, int) {
		counts := map[string]int{}
		for _, f := range stream.snapshot() {
			var row map[string]any
			json.Unmarshal([]byte(f.Data), &row)
			if p, ok := row["payload"].(map[string]any); ok {
				row = p
			}
			id, _ := row["delivery_id"].(string)
			if id != "" {
				counts[id]++
			}
		}
		missing, dups := 0, 0
		for _, id := range ids {
			if counts[id] == 0 {
				missing++
			}
			if counts[id] > 1 {
				dups += counts[id] - 1
			}
		}
		return counts, missing, dups
	}
	sseCounts, sseMissing, sseDups := countIDs(resumed, []string{"hlc-after-00", "hlc-after-verify"})
	muxCounts, muxMissing, muxDups := countIDs(muxResumed, []string{"hlc-after-00", "hlc-after-01", "hlc-after-02", "hlc-after-03", "hlc-after-verify"})
	elapsed := func(at time.Time) any {
		if at.IsZero() {
			return nil
		}
		return at.Sub(ready).Milliseconds()
	}
	result := map[string]any{"stop_utc": stopped.UTC(), "old_exit_utc": exited.UTC(), "new_listener_observed_utc": c2.connected.UTC(), "new_ready_utc": ready.UTC(), "helpers_ready_utc": helpersReady.UTC(),
		"sse_ready_utc": sseReady.UTC(), "mux_ready_utc": muxReady.UTC(), "ws_ready_utc": wsReady.UTC(), "stop_to_ready_ms": ready.Sub(stopped).Milliseconds(), "helpers_from_ready_ms": helpersReady.Sub(ready).Milliseconds(),
		"sse_from_ready_ms": elapsed(sseReady), "mux_from_ready_ms": elapsed(muxReady), "ws_from_ready_ms": elapsed(wsReady), "old_cursor": oldCursor, "sse_gap_count": gap, "mux_gap_count": muxGap,
		"sse_duplicate_cursors": duplicates, "sse_id_counts": sseCounts, "sse_missing": sseMissing, "sse_duplicate_messages": sseDups, "mux_id_counts": muxCounts, "mux_missing": muxMissing, "mux_duplicate_messages": muxDups,
		"sse_frames": resumed.snapshot(), "mux_frames": muxResumed.snapshot(), "automatic_reset_performed": false, "helper_count": 20, "helper_recovery": helperRecovery}
	for _, frame := range muxResumed.snapshot() {
		var row map[string]any
		json.Unmarshal([]byte(frame.Data), &row)
		if row["type"] == "gap" {
			result["mux_gap_from_stop_ms"] = frame.At.Sub(stopped).Milliseconds()
			break
		}
	}
	if !muxReady.IsZero() {
		result["mux_from_stop_ms"] = muxReady.Sub(stopped).Milliseconds()
	}
	b, _ = json.MarshalIndent(result, "", "  ")
	os.WriteFile(filepath.Join(out, "S011-recovery.json"), b, 0644)
	t.Logf("S011 recovery=%s", b)
	// Preserve automatic recovery as a red observation. Explicit reset is a separate
	// user action, matching closeTab(remove(..., true)) + openTab in the real broker.
	helperOverLimit := false
	for _, item := range helperRecovery {
		if item["from_ready_ms"].(int64) > 5000 {
			helperOverLimit = true
		}
	}
	if ready.Sub(stopped) > 20*time.Second || helpersReady.Sub(ready) > 5*time.Second || helperOverLimit || sseReady.IsZero() || sseReady.Sub(ready) > 5*time.Second || wsReady.IsZero() || wsReady.Sub(ready) > 5*time.Second || gap == 0 || muxGap != 1 || duplicates+sseDups+muxDups+sseMissing != 0 || muxMissing != 2 {
		t.Fatal("S011_AUTO_OBSERVATION_CHANGED: inspect raw evidence before reset")
	}
	base := "http://" + c2.public
	feed := muxResumed.feed
	subscriptions := []map[string]string{}
	var reset map[string]any
	for _, raw := range feed["subscriptions"].([]any) {
		scope := raw.(map[string]any)
		name := scope["full_name"].(string)
		desired := map[string]string{"full_name": name, "iframe_instance": scope["iframe_instance"].(string)}
		if name == hlcFull(0) {
			reset = map[string]any{"full_name": name, "iframe_instance": scope["iframe_instance"], "generation": scope["generation"], "replay_round": scope["replay_round"]}
			desired["iframe_instance"] = fmt.Sprintf("%032x", 900)
		} else {
			// Other panes keep their applied cursor when the closed pane is reopened.
			for _, f := range muxResumed.snapshot() {
				var row map[string]any
				json.Unmarshal([]byte(f.Data), &row)
				if row["type"] == "event" && row["full_name"] == name {
					desired["after"] = row["epoch"].(string) + "-" + row["seq"].(string)
				}
			}
		}
		subscriptions = append(subscriptions, desired)
	}
	click := time.Now()
	meta := hlcJSON(t, "PUT", base+"/api/console/event-feeds/"+feed["feed_id"].(string), base, hlcToken(0), map[string]any{
		"request_id": fmt.Sprintf("%032x", 901), "reset_request_id": fmt.Sprintf("%032x", 902),
		"auth_context":      hlcJSON(t, "GET", base+"/api/console/event-context", base, hlcToken(0), nil)["auth_context"],
		"expected_revision": feed["revision"], "subscriptions": subscriptions, "reset": reset,
	})
	var output time.Time
	outputID := "hlc-after-00"
	for time.Now().Before(click.Add(5 * time.Second)) {
		for _, frame := range muxResumed.snapshot() {
			var row map[string]any
			if json.Unmarshal([]byte(frame.Data), &row) != nil {
				continue
			}
			if payload, ok := row["payload"].(map[string]any); ok {
				row = payload
			}
			if row["delivery_id"] == outputID && frame.At.After(click) {
				output = frame.At
				break
			}
		}
		if !output.IsZero() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	countsAfter, missingAfter, dupsAfter := countIDs(muxResumed, []string{"hlc-after-00", "hlc-after-01", "hlc-after-02", "hlc-after-03", "hlc-after-verify"})
	fates := []map[string]any{}
	for _, id := range []string{"hlc-after-00", "hlc-after-verify"} {
		item := map[string]any{"delivery_id": id, "session_event_envelope_id": id + "-event", "published_send_started_utc": published[id].UTC(),
			"published_after_stop_ms": published[id].Sub(stopped).Milliseconds(), "published_after_new_ready_ms": published[id].Sub(ready).Milliseconds(),
			"before_resync_count": muxCounts[id], "after_resync_count": countsAfter[id], "old_hub_memory_loss": false}
		for _, f := range resumed.snapshot() {
			var row map[string]any
			json.Unmarshal([]byte(f.Data), &row)
			if row["delivery_id"] == id {
				item["new_hub_sse_cursor"] = f.ID
				item["sse_observed_utc"] = f.At.UTC()
			}
		}
		for _, f := range muxResumed.snapshot() {
			var row map[string]any
			json.Unmarshal([]byte(f.Data), &row)
			if payload, ok := row["payload"].(map[string]any); ok && payload["delivery_id"] == id {
				item["mux_cursor"] = row["epoch"].(string) + "-" + row["seq"].(string)
				item["mux_observed_utc"] = f.At.UTC()
			}
		}
		if countsAfter[id] == 1 {
			item["fate"] = "显式重新同步后补回"
		} else {
			item["fate"] = "未补回，需核查，不据此推断旧Hub内存丢失"
		}
		fates = append(fates, item)
	}
	explicit := map[string]any{"automatic_recovery_pass": false, "gap_informed": muxGap, "click_utc": click.UTC(), "output_utc": output.UTC(),
		"click_to_output_ms": output.Sub(click).Milliseconds(), "stop_to_output_ms": output.Sub(stopped).Milliseconds(),
		"missing_before": muxMissing, "missing_after": missingAfter, "duplicates_after": dupsAfter, "id_counts_after": countsAfter,
		"fates": fates, "reset_response": meta, "frames_after": muxResumed.snapshot(), "explicit_output_delivery_id": outputID,
		"method": "真实Hub/VS；按console_auth.go closeTab remove(true)与console_mux_broker.js resetIntents/sync协议模拟显式操作；非浏览器点击读数"}
	b, _ = json.MarshalIndent(explicit, "", "  ")
	os.WriteFile(filepath.Join(out, "S011-explicit-resync.json"), b, 0644)
	if output.IsZero() || output.Sub(click) > 5*time.Second || missingAfter != 0 || dupsAfter != 0 {
		t.Fatal("S011_EXPLICIT_RESYNC_MISSING_OR_TIMING: see fate evidence")
	}
	for _, s := range []*hlcStream{resumed, muxResumed} {
		s.body.Close()
	}
	for _, a := range agents2 {
		a.conn.CloseNow()
	}
	viewer2.CloseNow()
	hlcStop(t, c2, out, "S011-new-cleanup")
}
