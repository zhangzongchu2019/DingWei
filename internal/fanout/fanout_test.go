package fanout

// F1-F4(设计 §6):复制/白名单/测试标记/降级。F5 e2e 见 cmd/feishu-fanout。

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

type hubRec struct {
	mu     sync.Mutex
	bodies [][]byte
	paths  []string
}

func (h *hubRec) handler(status int, respBody string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, raw)
		h.paths = append(h.paths, r.URL.Path)
		h.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	})
}

func (h *hubRec) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.bodies)
}

func (h *hubRec) last() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bodies) == 0 {
		return nil
	}
	return h.bodies[len(h.bodies)-1]
}

type sendRec struct {
	mu   sync.Mutex
	outs []feishu.OutMessage
}

func (s *sendRec) Send(ctx context.Context, out feishu.OutMessage) (string, error) {
	s.mu.Lock()
	s.outs = append(s.outs, out)
	s.mu.Unlock()
	return "msg-1", nil
}

func event(openID, eventID string) []byte {
	payload := map[string]any{
		"schema": "2.0",
		"header": map[string]any{"event_id": eventID, "event_type": "im.message.receive_v1", "token": "tok"},
		"event": map[string]any{
			"sender":  map[string]any{"sender_id": map[string]any{"open_id": openID}},
			"message": map[string]any{"message_id": eventID, "chat_type": "p2p", "chat_id": openID, "content": `{"text":"hi"}`},
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func writeWhitelist(t *testing.T, wl Whitelist) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wl.json")
	data, _ := json.Marshal(wl)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newServer(t *testing.T, prod, test string, wlFile string, sender Sender) *Server {
	t.Helper()
	s := New(Config{
		ProdURL:       prod,
		TestURL:       test,
		Channels:      map[string]ChannelConfig{"unifiedrobot": {VerificationToken: "tok"}},
		WhitelistFile: wlFile,
		OutboundToken: "secret-token",
		TestTO:        2 * time.Second,
		Sender:        sender,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.testWorker(ctx)
	return s
}

func postWebhook(t *testing.T, s *Server, channel string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook/"+channel, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func waitCount(t *testing.T, f func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("count=%d want=%d", f(), want)
}

// F1 复制:白名单内消息双路收到同一字节;生产响应原样中继
func TestF1ReplicateWhitelisted(t *testing.T) {
	prod, test := &hubRec{}, &hubRec{}
	prodSrv := httptest.NewServer(prod.handler(202, `{"status":"queued"}`))
	defer prodSrv.Close()
	testSrv := httptest.NewServer(test.handler(202, `{}`))
	defer testSrv.Close()
	wl := writeWhitelist(t, Whitelist{OpenIDs: []string{"ou_vip"}})
	s := newServer(t, prodSrv.URL, testSrv.URL, wl, nil)

	body := event("ou_vip", "ev-1")
	w := postWebhook(t, s, "unifiedrobot", body)
	if w.Code != 202 || w.Body.String() != `{"status":"queued"}` {
		t.Fatalf("prod relay broken: %d %s", w.Code, w.Body.String())
	}
	waitCount(t, prod.count, 1)
	waitCount(t, test.count, 1)
	if !bytes.Equal(prod.last(), body) || !bytes.Equal(test.last(), body) {
		t.Fatal("bytes not identical on both paths")
	}
	if test.paths[0] != "/webhook/unifiedrobot" {
		t.Fatalf("test path=%s", test.paths[0])
	}
}

// F1 补:加密事件也能判白名单(解密仅读 sender,转发仍密文原样)
func TestF1EncryptedWhitelist(t *testing.T) {
	prod, test := &hubRec{}, &hubRec{}
	prodSrv := httptest.NewServer(prod.handler(202, `ok`))
	defer prodSrv.Close()
	testSrv := httptest.NewServer(test.handler(202, `ok`))
	defer testSrv.Close()
	wl := writeWhitelist(t, Whitelist{OpenIDs: []string{"ou_vip"}})
	s := newServer(t, prodSrv.URL, testSrv.URL, wl, nil)
	s.SetChannels(map[string]ChannelConfig{"unifiedrobot": {EncryptKey: "k1", VerificationToken: "tok"}})

	cipherBody := encryptPayload(t, event("ou_vip", "ev-enc"), "k1")
	postWebhook(t, s, "unifiedrobot", cipherBody)
	waitCount(t, test.count, 1)
	if !bytes.Equal(test.last(), cipherBody) {
		t.Fatal("test path must receive raw (still-encrypted) bytes")
	}
}

// F2 白名单:名单外仅生产;热重载生效;文件坏=fail-closed
func TestF2Whitelist(t *testing.T) {
	prod, test := &hubRec{}, &hubRec{}
	prodSrv := httptest.NewServer(prod.handler(202, `ok`))
	defer prodSrv.Close()
	testSrv := httptest.NewServer(test.handler(202, `ok`))
	defer testSrv.Close()
	wlPath := writeWhitelist(t, Whitelist{OpenIDs: []string{"ou_vip"}})
	s := newServer(t, prodSrv.URL, testSrv.URL, wlPath, nil)

	postWebhook(t, s, "unifiedrobot", event("ou_other", "ev-2"))
	waitCount(t, prod.count, 1)
	time.Sleep(150 * time.Millisecond)
	if test.count() != 0 {
		t.Fatal("non-whitelisted must not reach test hub")
	}
	if s.metrics.WhitelistMiss.Load() != 1 {
		t.Fatalf("whitelist_miss=%d", s.metrics.WhitelistMiss.Load())
	}

	// 热重载:把 ou_other 加进名单
	data, _ := json.Marshal(Whitelist{OpenIDs: []string{"ou_vip", "ou_other"}})
	if err := os.WriteFile(wlPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	postWebhook(t, s, "unifiedrobot", event("ou_other", "ev-3"))
	waitCount(t, test.count, 1)

	// 文件坏 → fail-closed:测试路全停
	if err := os.WriteFile(wlPath, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	postWebhook(t, s, "unifiedrobot", event("ou_vip", "ev-4"))
	waitCount(t, prod.count, 3)
	time.Sleep(150 * time.Millisecond)
	if test.count() != 1 {
		t.Fatalf("fail-closed broken: test got %d", test.count())
	}
}

// F3 测试标记:后缀恰一个+幂等;鉴权;生产路无标记(由 F1 字节等价隐含)
func TestF3OutboundSuffix(t *testing.T) {
	rec := &sendRec{}
	s := newServer(t, "http://127.0.0.1:1", "", "", rec)

	send := func(token, text string) *httptest.ResponseRecorder {
		out, _ := json.Marshal(feishu.OutMessage{BotChannelID: "unifiedrobot", ToID: "ou_vip", Text: text})
		req := httptest.NewRequest(http.MethodPost, "/outbound", bytes.NewReader(out))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}

	if w := send("wrong", "hi"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token accepted: %d", w.Code)
	}
	if w := send("secret-token", "hi"); w.Code != http.StatusOK {
		t.Fatalf("outbound failed: %d %s", w.Code, w.Body.String())
	}
	if got := rec.outs[0].Text; got != "hi"+TestEnvSuffix {
		t.Fatalf("suffix wrong: %q", got)
	}
	// 幂等:已带后缀不重复加
	send("secret-token", "done"+TestEnvSuffix)
	if got := rec.outs[1].Text; got != "done"+TestEnvSuffix {
		t.Fatalf("suffix not idempotent: %q", got)
	}
}

// F4 降级:测试 Hub down 生产照常;生产 down 对飞书 5xx 且测试不投;challenge 生产权威
func TestF4Degradation(t *testing.T) {
	prod := &hubRec{}
	prodSrv := httptest.NewServer(prod.handler(202, `ok`))
	defer prodSrv.Close()
	wl := writeWhitelist(t, Whitelist{OpenIDs: []string{"ou_vip"}})

	// 测试 Hub 不可达(死端口):生产照常 202,test_failed 计数
	s := newServer(t, prodSrv.URL, "http://127.0.0.1:1", wl, nil)
	if w := postWebhook(t, s, "unifiedrobot", event("ou_vip", "ev-5")); w.Code != 202 {
		t.Fatalf("prod must be unaffected: %d", w.Code)
	}
	waitCount(t, func() int { return int(s.metrics.TestFailed.Load()) }, 1)

	// 生产不可达:502 给飞书,测试路不投
	test := &hubRec{}
	testSrv := httptest.NewServer(test.handler(202, `ok`))
	defer testSrv.Close()
	s2 := newServer(t, "http://127.0.0.1:1", testSrv.URL, wl, nil)
	if w := postWebhook(t, s2, "unifiedrobot", event("ou_vip", "ev-6")); w.Code != http.StatusBadGateway {
		t.Fatalf("prod-down must 5xx: %d", w.Code)
	}
	time.Sleep(150 * time.Millisecond)
	if test.count() != 0 {
		t.Fatal("test must not see event before prod")
	}

	// challenge:生产响应原样中继;测试路不复制
	chSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"challenge":"abc"}`))
	}))
	defer chSrv.Close()
	s3 := newServer(t, chSrv.URL, testSrv.URL, wl, nil)
	challenge := []byte(`{"challenge":"abc","token":"tok","type":"url_verification"}`)
	if w := postWebhook(t, s3, "unifiedrobot", challenge); w.Body.String() != `{"challenge":"abc"}` {
		t.Fatalf("challenge relay broken: %s", w.Body.String())
	}
	time.Sleep(150 * time.Millisecond)
	if test.count() != 0 {
		t.Fatal("challenge must not be copied to test hub")
	}
}

// 测试路 event_id 去重(代理重试防重复注入)
func TestEventDedupOnTestPath(t *testing.T) {
	prod, test := &hubRec{}, &hubRec{}
	prodSrv := httptest.NewServer(prod.handler(202, `ok`))
	defer prodSrv.Close()
	testSrv := httptest.NewServer(test.handler(202, `ok`))
	defer testSrv.Close()
	wl := writeWhitelist(t, Whitelist{OpenIDs: []string{"ou_vip"}})
	s := newServer(t, prodSrv.URL, testSrv.URL, wl, nil)

	same := event("ou_vip", "ev-dup")
	postWebhook(t, s, "unifiedrobot", same)
	postWebhook(t, s, "unifiedrobot", same)
	waitCount(t, prod.count, 2) // 生产路不去重(与现状一致,Hub 自行处理)
	waitCount(t, test.count, 1) // 测试路恰一次
}

func encryptPayload(t *testing.T, plain []byte, key string) []byte {
	t.Helper()
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	enc := base64.StdEncoding.EncodeToString(append(iv, out...))
	return []byte(fmt.Sprintf(`{"encrypt":%q}`, enc))
}
