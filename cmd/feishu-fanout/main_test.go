package main

// F5 e2e binary smoke(设计 §6):真 feishu-fanout 进程+两个真 HTTP Hub 替身。
// 覆盖:入站双路复制(白名单)/名单外仅生产/healthz startup 依赖/outbound 后缀(fake sender)。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

type rec struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (r *rec) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.bodies) }

func hubStub(r *rec) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/readyz" {
			w.WriteHeader(200)
			return
		}
		raw, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, raw)
		r.mu.Unlock()
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	}))
}

func event(openID, eventID string) []byte {
	data, _ := json.Marshal(map[string]any{
		"header": map[string]any{"event_id": eventID, "token": "tok"},
		"event": map[string]any{
			"sender":  map[string]any{"sender_id": map[string]any{"open_id": openID}},
			"message": map[string]any{"chat_id": openID, "content": `{"text":"hi"}`},
		},
	})
	return data
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("proxy not ready: %s", url)
}

func TestF5BinarySmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("binary smoke")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "feishu-fanout")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".") // 同 g9:免 VCS stamping 假 FAIL(平行实例一并修)
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}

	prod, test := &rec{}, &rec{}
	prodSrv, testSrv := hubStub(prod), hubStub(test)
	defer prodSrv.Close()
	defer testSrv.Close()

	wlPath := filepath.Join(tmp, "wl.json")
	if err := os.WriteFile(wlPath, []byte(`{"open_ids":["ou_vip"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	base := "http://" + addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		"FANOUT_ADDR="+addr,
		"FANOUT_PROD_URL="+prodSrv.URL,
		"FANOUT_TEST_URL="+testSrv.URL,
		"FANOUT_WHITELIST_FILE="+wlPath,
		"FANOUT_OUTBOUND_TOKEN=t17-token",
		"FANOUT_OUTBOUND_FAKE=1",
		`FANOUT_CHANNELS={"unifiedrobot":{"verification_token":"tok"}}`,
	)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitReady(t, base+"/healthz") // healthz 200 隐含 startup 依赖检查通过(生产 Hub 可达)

	// 白名单内:双路复制
	resp, err := http.Post(base+"/webhook/unifiedrobot", "application/json", bytes.NewReader(event("ou_vip", "e2e-1")))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 202 || string(body) != `{"status":"queued"}` {
		t.Fatalf("prod relay: %d %s", resp.StatusCode, body)
	}
	// 名单外:仅生产
	resp, err = http.Post(base+"/webhook/unifiedrobot", "application/json", bytes.NewReader(event("ou_other", "e2e-2")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (prod.count() < 2 || test.count() < 1) {
		time.Sleep(50 * time.Millisecond)
	}
	if prod.count() != 2 || test.count() != 1 {
		t.Fatalf("fanout counts prod=%d test=%d", prod.count(), test.count())
	}

	// outbound:经 hub 侧 ProxyGateway 真 HTTP 调用,fake sender 不触真 Lark
	gw := feishu.NewProxyGateway(base+"/outbound", "t17-token")
	msgID, err := gw.Send(context.Background(), feishu.OutMessage{BotChannelID: "unifiedrobot", ToID: "ou_vip", Text: "reply"})
	if err != nil || msgID != "fake-msg-id" {
		t.Fatalf("outbound via ProxyGateway: id=%q err=%v", msgID, err)
	}
	// 鉴权负例
	if _, err := feishu.NewProxyGateway(base+"/outbound", "wrong").Send(context.Background(), feishu.OutMessage{Text: "x"}); err == nil {
		t.Fatal("wrong token must fail")
	}

	// metrics 佐证
	mresp, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]int64
	_ = json.NewDecoder(mresp.Body).Decode(&m)
	mresp.Body.Close()
	if m["prod_forwarded"] != 2 || m["test_sent"] != 1 || m["whitelist_miss"] != 1 || m["outbound_sent"] != 1 {
		t.Fatalf("metrics: %v", m)
	}
}
