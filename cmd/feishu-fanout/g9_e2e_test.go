package main

// G9 e2e(设计 v2.3 §I):真 feishu-fanout 二进制,v2.3 全形态——配置文件真源/
// 托管出站(202→sent→幂等重放)/kill -9 跨进程 WAL 耐久/入站 ws 管道(inject 钩)→
// sink 投递 v:1+token→真 HTTP 收方/SIGHUP 热载代次推进/回滚语义(代理停=收方直连不受扰)。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

type v1rec struct {
	mu   sync.Mutex
	got  []map[string]any
	toks []string
}

// v1HubStub 模拟 Hub webhook:核 token+记 v:1 投递(含 challenge 探针路)。
func v1HubStub(r *v1rec, wantToken string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(req.Body).Decode(&p)
		tok, _ := p["token"].(string)
		if tok != wantToken {
			w.WriteHeader(401)
			return
		}
		if ch, _ := p["challenge"].(string); ch != "" { // pre-arm 探针:200 回显不入账
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": ch})
			return
		}
		r.mu.Lock()
		r.got = append(r.got, p)
		r.toks = append(r.toks, tok)
		r.mu.Unlock()
		w.WriteHeader(202)
	}))
}

func (r *v1rec) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.got) }

func writeG9Config(t *testing.T, path, hubURL string, gen int64) {
	t.Helper()
	cfg := map[string]any{
		"generation": gen, "alerts_verified": true,
		"sinks": []map[string]any{{
			"name": "prod", "hub_url": hubURL, "channel": "unifiedrobot",
			"role": "authoritative", "filter": "all", "token": "sink-sek",
		}},
		"callers": map[string]any{
			"caller-prod": map[string]any{"name": "prod-hub", "test_suffix": false},
			"caller-test": map[string]any{"name": "test-hub", "test_suffix": true},
		},
	}
	b, _ := json.Marshal(cfg) // JSON⊂YAML:yaml.v3 可解
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestG9CustodyAndFlipE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("binary e2e")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "feishu-fanout")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}

	hubRec := &v1rec{}
	hub := v1HubStub(hubRec, "sink-sek")
	defer hub.Close()

	cfgPath := filepath.Join(tmp, "fanout.yaml")
	writeG9Config(t, cfgPath, hub.URL, 1)
	spoolPath := filepath.Join(tmp, "spool.db")
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	base := "http://" + addr

	env := append(os.Environ(),
		"FANOUT_ADDR="+addr,
		"FANOUT_PROD_URL="+hub.URL, // v1 探测路
		"FANOUT_CONFIG_FILE="+cfgPath,
		"FANOUT_SPOOL_PATH="+spoolPath,
		"FANOUT_OUTBOUND_FAKE=1",
		"FANOUT_TEST_INJECT=1",
		`FANOUT_ADMIN_TOKENS={"adm":{"id":"ops-a","role":"admin"}}`,
	)
	start := func() *exec.Cmd {
		cmd := exec.Command(bin)
		cmd.Env = env
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waitReady(t, base+"/healthz")
		return cmd
	}
	cmd := start()
	defer func() { _ = cmd.Process.Kill() }()

	// —— 托管出站全链:Hub ProxyGateway(真 HTTP)v:1 → 202 outbound_id → sent
	gw := feishu.NewProxyGateway(base+"/outbound", "caller-prod")
	rid := feishu.DeriveRequestID("reply", "e2e-msg-1", "0")
	obID, err := gw.Send(context.Background(), feishu.OutMessage{
		BotChannelID: "unifiedrobot", ToID: "oc_1", ToType: "group", Text: "hi", RequestID: rid})
	if err != nil || obID == "" {
		t.Fatalf("custody send: %q %v", obID, err)
	}
	waitStatus := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			req, _ := http.NewRequest("GET", base+"/outbound/"+obID, nil)
			req.Header.Set("Authorization", "Bearer caller-prod")
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				var st map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&st)
				resp.Body.Close()
				if st["status"] == want {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("outbound %s 未达 %s", obID, want)
	}
	waitStatus("sent")

	// —— kill -9 跨进程耐久(G13 e2e 层):重启后同 request_id=同 outbound_id
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	cmd = start()
	obID2, err := gw.Send(context.Background(), feishu.OutMessage{
		BotChannelID: "unifiedrobot", ToID: "oc_1", ToType: "group", Text: "hi", RequestID: rid})
	if err != nil || obID2 != obID {
		t.Fatalf("跨进程幂等破:%q vs %q (%v)", obID2, obID, err)
	}

	// —— 入站 ws 管道(inject)→WAL→sink 投递 v:1+token→hub 202;二投幂等
	inject := func(msgID string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"bot_channel": "unifiedrobot", "msg_id": msgID, "chat_type": "group",
			"chat_id": "oc_g", "sender_open_id": "ou_a", "text": "inbound-hi"})
		resp, err := http.Post(base+"/_test/inject", "application/json", bytes.NewReader(body))
		if err != nil || resp.StatusCode != 202 {
			t.Fatalf("inject %s: %v %d", msgID, err, resp.StatusCode)
		}
		resp.Body.Close()
	}
	inject("om_e2e_1")
	inject("om_e2e_1") // at-least-once 二投
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && hubRec.count() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	if hubRec.count() != 1 { // 幂等:恰一次投递
		t.Fatalf("sink 投递数=%d", hubRec.count())
	}
	if p := hubRec.got[0]; p["v"] != float64(1) || p["msg_id"] != "om_e2e_1" || p["token"] != "sink-sek" {
		t.Fatalf("v:1 投递形:%v", p)
	}

	// —— SIGHUP 热载:改配置代次+SIGHUP→ETag 推进(同管道 §E)
	writeG9Config(t, cfgPath, hub.URL, 9)
	_ = cmd.Process.Signal(syscall.SIGHUP)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", base+"/admin/config", nil)
		req.Header.Set("Authorization", "Bearer adm")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			etag := resp.Header.Get("ETag")
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if etag == "9" {
				if bytes.Contains(b, []byte("sink-sek")) || bytes.Contains(b, []byte("caller-prod")) {
					t.Fatalf("掩码失效:%s", b)
				}
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// —— 回滚语义:代理停,收方(生产 Hub 替身)直连不受扰=生产路独立兜底
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	resp, err := http.Post(hub.URL+"/webhook/unifiedrobot", "application/json",
		bytes.NewReader([]byte(`{"token":"sink-sek","challenge":"direct-after-proxy-down"}`)))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("代理停后直连路受扰:%v %d", err, resp.StatusCode)
	}
	resp.Body.Close()
}
