package nodeagent

// 账号登录节点侧（R-V2-ACCOUNT-LOGIN-FLOW，设计件第十二、十三节）：一次性容器规格、链接与码只取固定模式、
// 凭证经主动上传不走结果、不入账本、重放幂等、取消幂等、启动清扫、日志不含链接／码／凭证。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	lfID   = "0123456789abcdef0123456789abcdef"
	lfID2  = "fedcba9876543210fedcba9876543210"
	lfURL  = "https://auth.example.test/codex/device"
	lfCode = "ABCD-12345"
	lfCred = `{"tokens":{"id_token":"x.y.z","refresh_token":"RT-SECRET-MARKER"}}`
)

var lfImage = "hub.local/dingwei/codex@sha256:" + strings.Repeat("ab", 32)

func frame(s string) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 8)
	hdr[0] = 1
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(s)))
	b.Write(hdr)
	b.WriteString(s)
	return b.Bytes()
}

// lfEngine 登录用假 Docker：attach 回放给定输出；wait 阻塞到 release；记录调用与 spec。
type lfEngine struct {
	mu       sync.Mutex
	calls    []string
	specs    []map[string]any
	output   string // attach 流内容（多路复用帧之前的原文）
	exitCode int
	release  chan struct{}
	relOnce  sync.Once
	onStart  func()
	labeled  []containerListItem
	srv      *httptest.Server
}

func newLFEngine(t *testing.T, output string) *lfEngine {
	f := &lfEngine{output: output, release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.calls = append(f.calls, key)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case key == "POST /networks/create", strings.HasPrefix(key, "DELETE /networks/"), strings.HasPrefix(key, "DELETE /containers/"):
			w.Write([]byte(`{}`))
		case key == "POST /containers/create":
			var spec map[string]any
			json.NewDecoder(r.Body).Decode(&spec)
			f.mu.Lock()
			f.specs = append(f.specs, spec)
			f.mu.Unlock()
			w.Write([]byte(`{"Id":"cid-` + r.URL.Query().Get("name") + `"}`))
		case strings.HasSuffix(key, "/attach"):
			w.WriteHeader(200)
			w.Write(frame(f.output))
		case strings.HasSuffix(key, "/start"):
			if f.onStart != nil {
				f.onStart()
			}
			w.Write([]byte(`{}`))
		case strings.HasSuffix(key, "/wait"):
			select {
			case <-f.release:
			case <-r.Context().Done():
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"StatusCode": f.exitCode})
		case key == "GET /containers/json":
			json.NewEncoder(w).Encode(f.labeled)
		default:
			http.Error(w, "unexpected "+key, 500)
		}
	}))
	t.Cleanup(f.srv.Close)
	t.Cleanup(f.letExit) // 先于 srv.Close 执行（LIFO）：断言失败时阻塞中的 /wait 也得放行，否则关服务器会卡到超时
	return f
}

// letExit 放行所有 /wait（幂等）。
func (f *lfEngine) letExit() { f.relOnce.Do(func() { close(f.release) }) }

func (f *lfEngine) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// lfHub 假 Hub 上传端点（TLS）。
type lfHub struct {
	mu   sync.Mutex
	got  []map[string]string
	auth []string
	srv  *httptest.Server
}

func newLFHub(t *testing.T) *lfHub {
	h := &lfHub{}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		json.NewDecoder(r.Body).Decode(&m)
		h.mu.Lock()
		h.got = append(h.got, m)
		h.auth = append(h.auth, r.Header.Get("Authorization"))
		h.mu.Unlock()
		w.WriteHeader(204)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *lfHub) uploads() []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]string(nil), h.got...)
}

func mkLoginOps(t *testing.T, f *lfEngine, h *lfHub) *loginOps {
	t.Helper()
	return &loginOps{
		d:    &dockerOps{eng: newFakeEngine(f.srv.Client(), f.srv.URL), registry: "hub.local/dingwei", cgroupParent: "dingwei-hosted.slice", seatProxy: "http://relay.test:11080"},
		root: t.TempDir(), hubOrigin: h.srv.URL, secretFn: func() (string, error) { return "node-secret", nil },
		hc: h.srv.Client(), sessions: map[string]*loginSession{},
	}
}

func lfParams(l *loginOps, id string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"login_id": id, "image": lfImage, "upload_url": l.hubOrigin + "/api/node/login/" + id})
	return b
}

const lfOutput = "Welcome to Codex\nFollow these steps to sign in with ChatGPT using device code authorization:\n\n1. Open this link in your browser and sign in to your account\n   " + lfURL + "\n\n2. Enter this one-time code (expires in 15 minutes)\n   " + lfCode + "\n\nContinue only if you started this login in Codex.\n"

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("超时未等到：%s", what)
}

// L1：成功路径——回链接与码；容器规格（日志 none、只挂本会话内存盘目录、10001、设备码命令）；凭证经上传；清场；日志无敏感串。
func TestLoginStartUploadsCredentialAndCleansUp(t *testing.T) {
	f := newLFEngine(t, lfOutput)
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)
	var homeAtStart os.FileInfo
	f.onStart = func() { // 起容器那一刻 CODEX_HOME 必须已存在且 0700（0.159.2 不存在即拒启）；CLI 随后才写 auth.json
		dir := filepath.Join(l.dir(lfID), ".codex")
		homeAtStart, _ = os.Stat(dir)
		_ = os.WriteFile(filepath.Join(dir, "auth.json"), []byte(lfCred), 0o600)
	}

	raw, err := l.start(context.Background(), lfParams(l, lfID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	var res map[string]string
	json.Unmarshal(raw, &res)
	if res["url"] != lfURL || res["user_code"] != lfCode || len(res) != 2 {
		t.Fatalf("结果只应有链接与码: %v", res)
	}
	if strings.Contains(string(raw), "RT-SECRET") {
		t.Fatal("命令结果不得含凭证")
	}
	if fi, err := os.Stat(l.dir(lfID)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("内存盘目录应 0700: %v %v", fi, err)
	}
	if homeAtStart == nil || !homeAtStart.IsDir() || homeAtStart.Mode().Perm() != 0o700 {
		t.Fatalf("起容器前须已建 CODEX_HOME（.codex，0700）: %v", homeAtStart)
	}
	spec := f.specs[0]
	hc := spec["HostConfig"].(map[string]any)
	if lc, _ := hc["LogConfig"].(map[string]any); lc == nil || lc["Type"] != "none" {
		t.Fatalf("日志驱动须 none: %v", lc)
	}
	binds := hc["Binds"].([]any)
	if len(binds) != 1 || binds[0] != l.dir(lfID)+":/h" {
		t.Fatalf("只许挂本会话目录: %v", binds)
	}
	if spec["User"] != "10001:10001" || hc["ReadonlyRootfs"] != true || hc["NetworkMode"] != "loginnet-"+lfID {
		t.Fatalf("隔离参数: %v", spec)
	}
	cmd, _ := json.Marshal(spec["Cmd"])
	if !strings.Contains(string(cmd), "--device-auth") || !strings.Contains(string(cmd), `cli_auth_credentials_store`) {
		t.Fatalf("命令: %s", cmd)
	}

	f.letExit()
	waitFor(t, "上传", func() bool { return len(h.uploads()) == 1 })
	up := h.uploads()[0]
	if up["status"] != "ok" || up["credential"] != lfCred {
		t.Fatalf("应上传 ok＋凭证: %v", up)
	}
	if h.auth[0] != "Bearer node-secret" {
		t.Fatalf("上传须带节点密钥: %q", h.auth[0])
	}
	waitFor(t, "清场", func() bool {
		_, err := os.Stat(l.dir(lfID))
		l.mu.Lock()
		n := len(l.sessions)
		l.mu.Unlock()
		return os.IsNotExist(err) && n == 0
	})
	if f.count("DELETE /containers/login-"+lfID) == 0 || f.count("DELETE /networks/loginnet-"+lfID) == 0 {
		t.Fatalf("须删容器与网络: %v", f.calls)
	}
	for _, s := range []string{lfURL, lfCode, "RT-SECRET-MARKER"} {
		if strings.Contains(logBuf.String(), s) {
			t.Fatalf("日志不得含 %q", s)
		}
	}
}

// L2：重放 login_start（不入账本后的自保）⇒ 返回现状，只建一个容器；L3：取消即清场、不上传；取消不存在的也成功。
func TestLoginStartReplayAndCancel(t *testing.T) {
	f := newLFEngine(t, lfOutput)
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	a, err := l.start(context.Background(), lfParams(l, lfID))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.start(context.Background(), lfParams(l, lfID))
	if err != nil || string(a) != string(b) {
		t.Fatalf("重放应返回现状: %s vs %s (%v)", a, b, err)
	}
	if n := f.count("POST /containers/create"); n != 1 {
		t.Fatalf("重放不得再起容器: %d", n)
	}
	cp, _ := json.Marshal(map[string]string{"login_id": lfID})
	if _, err := l.cancel(context.Background(), cp); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(l.dir(lfID)); !os.IsNotExist(err) {
		t.Fatal("取消后内存盘目录须删")
	}
	if f.count("DELETE /containers/login-"+lfID) == 0 {
		t.Fatal("取消须删容器")
	}
	time.Sleep(50 * time.Millisecond)
	if len(h.uploads()) != 0 {
		t.Fatalf("取消不得上传: %v", h.uploads())
	}
	cp2, _ := json.Marshal(map[string]string{"login_id": lfID2})
	if _, err := l.cancel(context.Background(), cp2); err != nil {
		t.Fatalf("取消不存在的应成功: %v", err)
	}
}

// L4：输出里没有码（如设备码未开、CLI 直接退出）⇒ 失败、错误不带输出原文、清场。
func TestLoginStartNoCodeFailsCleanly(t *testing.T) {
	f := newLFEngine(t, "Error logging in with device code: OUTPUT-MARKER-XYZ\n")
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	_, err := l.start(context.Background(), lfParams(l, lfID))
	if err == nil || !strings.Contains(err.Error(), "no_device_code") {
		t.Fatalf("应 no_device_code, 得 %v", err)
	}
	if strings.Contains(err.Error(), "OUTPUT-MARKER") {
		t.Fatal("错误不得带容器输出原文")
	}
	if _, serr := os.Stat(l.dir(lfID)); !os.IsNotExist(serr) || len(l.sessions) != 0 {
		t.Fatal("失败须清场")
	}
}

// L5：CLI 非零退出 ⇒ 上传 failed（原因码，不带内容）。
func TestLoginExitNonZeroUploadsFailure(t *testing.T) {
	f := newLFEngine(t, lfOutput)
	f.exitCode = 1
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	f.letExit()
	waitFor(t, "失败上传", func() bool { return len(h.uploads()) == 1 })
	if up := h.uploads()[0]; up["status"] != "failed" || up["reason"] != "login_exit_1" || up["credential"] != "" {
		t.Fatalf("应上传 failed/login_exit_1: %v", up)
	}
}

// L6：每节点并发 2；L7：参数闸（上传地址须本 Hub 本会话端点、login_id 格式）。
func TestLoginBusyAndParamGuards(t *testing.T) {
	f := newLFEngine(t, lfOutput)
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	for _, id := range []string{lfID, lfID2} {
		if _, err := l.start(context.Background(), lfParams(l, id)); err != nil {
			t.Fatal(err)
		}
	}
	third := "11111111111111111111111111111111"
	if _, err := l.start(context.Background(), lfParams(l, third)); !errors.Is(err, ErrLoginBusy) {
		t.Fatalf("第三个应 login_busy, 得 %v", err)
	}
	for _, raw := range []string{
		`{"login_id":"` + third + `","image":"` + lfImage + `","upload_url":"https://evil.test/api/node/login/` + third + `"}`,
		`{"login_id":"` + third + `","image":"` + lfImage + `","upload_url":"` + l.hubOrigin + `/api/node/login/` + lfID + `"}`,
		`{"login_id":"NOT-HEX","image":"` + lfImage + `","upload_url":"` + l.hubOrigin + `/api/node/login/NOT-HEX"}`,
		`{"login_id":"` + third + `","image":"` + lfImage + `","upload_url":"` + l.hubOrigin + `/api/node/login/` + third + `","env":["X=1"]}`,
	} {
		if _, err := l.start(context.Background(), json.RawMessage(raw)); !errors.Is(err, ErrParams) {
			t.Fatalf("%s 应 ErrParams, 得 %v", raw, err)
		}
	}
	f.letExit()
}

// L8：登录指令不入账本（结果含一次性链接与码）；其它指令照旧入账本。
func TestLoginCommandsSkipLedger(t *testing.T) {
	lp := filepath.Join(t.TempDir(), "ledger.json")
	ld, err := loadLedger(lp)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{handlers: map[string]Handler{}, ledger: ld}
	a.Register(CmdLoginStart, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"url":"` + lfURL + `","user_code":"` + lfCode + `"}`), nil
	})
	a.Register(CmdStopSeat, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	if r := a.execute(context.Background(), Command{CommandID: "c-login", Type: CmdLoginStart}); !r.OK {
		t.Fatalf("login_start: %+v", r)
	}
	a.execute(context.Background(), Command{CommandID: "c-stop", Type: CmdStopSeat})
	if _, ok := a.ledger.Get("c-login"); ok {
		t.Fatal("login_start 结果不得入账本")
	}
	if _, ok := a.ledger.Get("c-stop"); !ok {
		t.Fatal("其它指令须照旧入账本")
	}
	raw, _ := os.ReadFile(lp)
	if bytes.Contains(raw, []byte(lfCode)) || bytes.Contains(raw, []byte(lfURL)) {
		t.Fatal("账本文件不得含链接或码")
	}
	if !commandWhitelist[CmdLoginStart] || !commandWhitelist[CmdLoginCancel] {
		t.Fatal("登录指令须在白名单")
	}
}

// L9：启动清扫——带标签的容器连同网络与目录删；内存盘下只删名字严格匹配且属主为席位用户的目录。
func TestLoginCleanupLeftovers(t *testing.T) {
	f := newLFEngine(t, "")
	f.labeled = []containerListItem{{ID: "c1", Labels: map[string]string{loginLabel: lfID}}}
	h := newLFHub(t)
	l := mkLoginOps(t, f, h)
	for _, n := range []string{"login-" + lfID, "login-" + lfID2, "login-notours", "other"} {
		os.Mkdir(filepath.Join(l.root, n), 0o700)
	}
	prevOwn := ownedBySeatUser
	ownedBySeatUser = func(p string) bool { return !strings.HasSuffix(p, lfID2) } // lfID2 那个模拟别人的
	defer func() { ownedBySeatUser = prevOwn }()
	l.cleanupLeftovers(context.Background())
	for n, want := range map[string]bool{"login-" + lfID: false, "login-" + lfID2: true, "login-notours": true, "other": true} {
		_, err := os.Stat(filepath.Join(l.root, n))
		if (err == nil) != want {
			t.Fatalf("%s 存在=%v 期望 %v", n, err == nil, want)
		}
	}
	if f.count("DELETE /containers/login-"+lfID) == 0 || f.count("DELETE /networks/loginnet-"+lfID) == 0 {
		t.Fatalf("残留容器与网络须删: %v", f.calls)
	}
}

// L10：凭证文件只认常规文件、须 JSON、限长；错误不含内容。
func TestReadLoginCredentialGuards(t *testing.T) {
	d := t.TempDir()
	good := filepath.Join(d, "good")
	os.WriteFile(good, []byte(lfCred), 0o600)
	if s, err := readLoginCredential(good); err != nil || s != lfCred {
		t.Fatalf("good: %v", err)
	}
	link := filepath.Join(d, "link")
	os.Symlink(good, link)
	bad := filepath.Join(d, "bad")
	os.WriteFile(bad, []byte("NOT-JSON-MARKER"), 0o600)
	big := filepath.Join(d, "big")
	os.WriteFile(big, bytes.Repeat([]byte("a"), loginMaxCredSize+1), 0o600)
	for p, want := range map[string]string{link: "credential_missing", bad: "credential_not_json", big: "credential_size", filepath.Join(d, "none"): "credential_missing"} {
		_, err := readLoginCredential(p)
		if err == nil || err.Error() != want || strings.Contains(err.Error(), "MARKER") {
			t.Fatalf("%s: 得 %v 期望 %s", filepath.Base(p), err, want)
		}
	}
}

// 帧解复用：stdout／stderr 两路帧合并、长度精确。
func TestDemuxReader(t *testing.T) {
	var b bytes.Buffer
	b.Write(frame("ab\n"))
	e := frame("cd\n")
	e[0] = 2
	b.Write(e)
	got, _ := io.ReadAll(demuxReader(&b))
	if string(got) != "ab\ncd\n" {
		t.Fatalf("%q", got)
	}
}
