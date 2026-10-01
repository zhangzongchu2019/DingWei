package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// stubHub 桩 Hub：校验节点凭据（node_id＋secret 绑定），下发指令并读结果。
// 对应 T-G4-10 判据的"Hub 验节点"一侧——真 Hub 用 hosted.AuthenticateNode，桩里用等价映射。
type stubHub struct {
	t      *testing.T
	secrets map[string]string // node_id → secret（绑定校验：换身份持真凭据必拒）
	cmds   chan Command
	rawCmds chan []byte // 原始帧（坏帧探针用——Command 结构 marshal 不出畸形 JSON）
	results chan Result
	all    chan []byte // 全部原始帧（capacity_report 等非 Result 帧断言用）
	srv    *httptest.Server
}

func newStubHub(t *testing.T, secrets map[string]string) *stubHub {
	h := &stubHub{t: t, secrets: secrets, cmds: make(chan Command, 8), rawCmds: make(chan []byte, 8), results: make(chan Result, 8), all: make(chan []byte, 32)}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		want, ok := h.secrets[nodeID]
		if !ok || auth != want { // 未知节点或凭据不绑定 ⇒ 401
			http.Error(w, `{"error":"node_auth_failed"}`, http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"dingwei-node"}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		// 读结果（后台），发指令（前台按测试脚本节奏）
		go func() {
			for {
				_, raw, err := conn.Read(ctx)
				if err != nil {
					return
				}
				var res Result
				if json.Unmarshal(raw, &res) == nil && res.CommandID != "" {
					h.results <- res
				}
				select {
				case h.all <- raw:
				default:
				}
			}
		}()
		for {
			select {
			case cmd, ok := <-h.cmds:
				if !ok {
					return
				}
				raw, _ := json.Marshal(cmd)
				if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
					return
				}
			case raw := <-h.rawCmds: // 坏帧探针：原样下发
				if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
					return
				}
			}
		}
	}))
	return h
}

func (h *stubHub) url() string { return "wss" + strings.TrimPrefix(h.srv.URL, "https") + "/api/node/connect?node_id=nd_t1" }

func (h *stubHub) certSHA256() string {
	sum := sha256.Sum256(h.srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}
func (h *stubHub) close()      { close(h.cmds); h.srv.Close() }
func (h *stubHub) frames() chan []byte { return h.all }

func mkConfig(t *testing.T, hubURL, secret string) Config {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, "node.secret")
	if err := os.WriteFile(sp, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		HubURL: hubURL, NodeID: "nd_t1", SecretPath: sp,
		LedgerPath: filepath.Join(dir, "ledger.json"),
		Labels:     map[string]string{"pool": "test"},
	}
}

func TestLedgerIdempotentAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ledger.json")
	l1, err := loadLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	r := Result{CommandID: "cmd_1", OK: true, Payload: json.RawMessage(`{"image":"x"}`)}
	if err := l1.Put(r); err != nil {
		t.Fatal(err)
	}
	// 模拟进程重启后重放：新台账实例从盘恢复
	l2, err := loadLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := l2.Get("cmd_1")
	if !ok || !got.OK {
		t.Fatal("重启后应命中台账（重放不重复执行）")
	}
	// 权限必须为 600
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("台账权限 %o 应为 600", fi.Mode().Perm())
	}
}

func TestAgentIdempotentAndReplay(t *testing.T) {
	secret := "node-secret-t1"
	hub := newStubHub(t, map[string]string{"nd_t1": secret})
	defer hub.close()

	var calls int32
	cfg := mkConfig(t, hub.url(), secret)
	cfg.HubCertSHA256 = hub.certSHA256()
	ag, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ag.Register(CmdPullImage, func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		n := atomic.AddInt32(&calls, 1)
		return json.Marshal(map[string]int32{"pulled": n})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)
	time.Sleep(300 * time.Millisecond) // 等连上

	// 首发执行
	hub.cmds <- Command{CommandID: "cmd_42", Type: CmdPullImage}
	r1 := <-hub.results
	if !r1.OK {
		t.Fatalf("首发应成功: %+v", r1)
	}
	// 同 command_id 重发 ⇒ 重报首次结果、不重复执行
	hub.cmds <- Command{CommandID: "cmd_42", Type: CmdPullImage}
	r2 := <-hub.results
	if string(r1.Payload) != string(r2.Payload) {
		t.Fatalf("重报应与首次一致: %s vs %s", r1.Payload, r2.Payload)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("执行器应只跑一次, 实际 %d", calls)
	}
	// 断线重连后 Hub 重放旧指令 ⇒ 台账兜住（仍不重复执行）
	hub.srv.CloseClientConnections()
	time.Sleep(700 * time.Millisecond) // 等代理重连上
	hub.cmds <- Command{CommandID: "cmd_42", Type: CmdPullImage}
	r3 := <-hub.results
	if string(r3.Payload) != string(r1.Payload) || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("重连重放应幂等: payload=%s calls=%d", r3.Payload, calls)
	}
	// 白名单外指令 ⇒ 拒（不落台账）
	hub.cmds <- Command{CommandID: "cmd_x", Type: "rm_rf_root"}
	r4 := <-hub.results
	if r4.OK || r4.Error != "unknown_command_type" {
		t.Fatalf("非白名单应拒: %+v", r4)
	}
}

func TestAgentAuthBindsIdentity(t *testing.T) {
	// 换身份持真凭据：用 nd_t1 的凭据、以 nd_evil 名义连 ⇒ 必拒
	secret := "node-secret-t1"
	hub := newStubHub(t, map[string]string{"nd_t1": secret})
	defer hub.close()
	cfg := mkConfig(t, strings.Replace(hub.url(), "node_id=nd_t1", "node_id=nd_evil", 1), secret)
	cfg.HubCertSHA256 = hub.certSHA256()
	ag, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = ag.serve(ctx)
	if err == nil {
		t.Fatal("换身份持真凭据必须被拒（反向）")
	}
}

// r6 整包自查补漏：台账文件损坏 ⇒ fail-closed（errLedgerCorrupt），不得静默清空重跑指令。
func TestLedgerCorruptFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger.json")
	if err := os.WriteFile(p, []byte("{corrupt-not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLedger(p); !errors.Is(err, errLedgerCorrupt) {
		t.Fatalf("损坏台账必须报 errLedgerCorrupt: %v", err)
	}
}

// r6 整包自查补漏：畸形指令帧 ⇒ 回 bad_command_frame 且连接不断（继续收后续指令）。
func TestBadCommandFrameReplied(t *testing.T) {
	secret := "node-secret-t1"
	hub := newStubHub(t, map[string]string{"nd_t1": secret})
	defer hub.close()
	cfg := mkConfig(t, hub.url(), secret)
	cfg.HubCertSHA256 = hub.certSHA256()
	ag, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ag.Register(CmdPullImage, func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(map[string]bool{"pulled": true})
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)
	time.Sleep(300 * time.Millisecond) // 等连上

	hub.rawCmds <- []byte("{not-a-command-frame")
	deadline := time.After(3 * time.Second)
	for {
		select {
		case raw := <-hub.frames():
			var res Result
			if json.Unmarshal(raw, &res) == nil && res.Error == "bad_command_frame" {
				goto gotBadFrame
			}
		case <-deadline:
			t.Fatal("畸形帧应回 bad_command_frame")
		}
	}
gotBadFrame:
	// 连接未断：后续合法指令仍正常执行
	hub.cmds <- Command{CommandID: "cmd_ok", Type: CmdPullImage}
	select {
	case r := <-hub.results:
		if !r.OK {
			t.Fatalf("坏帧后合法指令应照常执行: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("坏帧后连接不应断")
	}
}

// r6 整包自查补漏：钉证校验——指纹格式错/对端不出证/指纹不符三拒，相符才放行。
func TestTLSPinnedTransportRejectsMismatch(t *testing.T) {
	if _, err := TLSPinnedTransport("abcd"); err == nil {
		t.Fatal("指纹非 64 位 hex 必须拒")
	}
	blob := []byte("fake-hub-cert-der")
	sum := sha256.Sum256(blob)
	want := hex.EncodeToString(sum[:])
	tr, err := TLSPinnedTransport(want)
	if err != nil {
		t.Fatal(err)
	}
	verify := tr.TLSClientConfig.VerifyPeerCertificate
	if err := verify(nil, nil); err == nil {
		t.Fatal("对端不出示证书必须拒")
	}
	if err := verify([][]byte{[]byte("other-cert")}, nil); err == nil {
		t.Fatal("指纹不符必须拒")
	}
	if err := verify([][]byte{blob}, nil); err != nil {
		t.Fatalf("指纹相符应放行: %v", err)
	}
}

// r6 整包自查补漏：节点凭据文件读取——缺失/权限宽于 600/空内容三拒（fail-closed）。
func TestSecretFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	missing := Config{SecretPath: filepath.Join(dir, "nope")}
	if _, err := missing.secret(); err == nil {
		t.Fatal("凭据文件缺失必须拒")
	}
	wide := filepath.Join(dir, "wide.secret")
	if err := os.WriteFile(wide, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Config{SecretPath: wide}).secret(); err == nil {
		t.Fatal("权限宽于 600 必须拒")
	}
	empty := filepath.Join(dir, "empty.secret")
	if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Config{SecretPath: empty}).secret(); err == nil {
		t.Fatal("空凭据必须拒")
	}
}
