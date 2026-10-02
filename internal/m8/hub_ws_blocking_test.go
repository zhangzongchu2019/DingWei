package m8

// 组长 lead-m8-flake-findings 产品侧三处（板 ffd328b9）：对端不读时，Hub 的出站写、吊销／暂停请求、
// 服务连接的关闭都不得被 5 秒级的握手／pong 等待拖住。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ① 保活 Ping 在途（对端不读 ⇒ pong 永远不来）时，该会话的出站写不得被挡。
// 判据是先后而不是时长：持锁实现下写只能在 Ping 返回之后完成，所以「Ping 先返回」即红。
func TestKeepalivePingDoesNotBlockSessionWrites(t *testing.T) {
	hub, _, _ := newTestHub(t)
	useTestSessionKeepalive(t, hub, 20*time.Millisecond, 3*time.Second)
	pingStarted := make(chan struct{})
	pingReturned := make(chan struct{})
	var started, returned bool
	hook := func(done bool) {
		if !done && !started {
			started = true
			close(pingStarted)
		}
		if done && !returned {
			returned = true
			close(pingReturned)
		}
	}
	keepalivePingHook.Store(&hook)
	t.Cleanup(func() { keepalivePingHook.Store(nil) })

	clients := make(chan *sessionClient, 1)
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := &sessionClient{conn: conn}
		go hub.watchSessionKeepalive(ctx, c)
		clients <- c
		<-stop
		_ = conn.CloseNow()
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })

	peer, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.CloseNow() // 对端从不读：不回 pong
	c := <-clients
	<-pingStarted
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- c.write(context.Background(), model.Envelope{Body: "outbound while ping in flight"})
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	case <-pingReturned:
		t.Fatal("session write was blocked until the keepalive ping finished (ping holds the write lock)")
	}
}

// ② 吊销／暂停时对端不读：DisconnectAgent 不得陪着等满 5 秒关闭握手；关闭帧（4401）照样送达。
func TestDisconnectAgentReturnsPromptlyWhenPeerNotReading(t *testing.T) {
	e := newAgentEnv(t)
	conn, st, body := e.dial(e.cred, "version=0.3.0&tool=claude&os=linux")
	if conn == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	start := time.Now()
	e.hub.DisconnectAgent(e.agID) // 对端此刻没有任何读者
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("DisconnectAgent took %s with a non-reading peer (want well under the 5s close handshake)", d)
	}
	// 关闭帧已在返回前发出：对端此后开始读，读到的是 4401。
	select {
	case code := <-reader(conn):
		if code != StatusCredentialRevoked {
			t.Fatalf("peer got close code %d, want %d", code, StatusCredentialRevoked)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("peer never got the close frame")
	}
}

// ③ 服务连接空闲时对端优雅关闭：Hub 必须在读，关闭帧立即被回应，对端 Close 不等满 5 秒；Hub 侧随即下线该服务。
func TestServiceWSPeerCloseIsAnsweredPromptly(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/service/svc1?api_key="+plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitServiceOnline(t, hub, "svc1", true)
	start := time.Now()
	if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("peer close: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("peer graceful close took %s: the hub never read the close frame", d)
	}
	waitServiceOnline(t, hub, "svc1", false)
}

// ③ 读协程是唯一读者：forward 在途时服务连接断开，forward 立即报错，不等到调用超时。
func TestForwardFailsFastWhenServiceConnCloses(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/service/svc1?api_key="+plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitServiceOnline(t, hub, "svc1", true)
	go func() {
		_, data, err := conn.Read(ctx) // 收到请求后不回复，直接断开
		if err == nil {
			var req ForwardRequest
			_ = json.Unmarshal(data, &req)
		}
		_ = conn.Close(websocket.StatusGoingAway, "gone")
	}()
	start := time.Now()
	if _, err := hub.forward(ctx, "svc1", ForwardRequest{ID: "r1", Text: "hello"}, 30000); err == nil {
		t.Fatal("forward succeeded on a closed service connection")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("forward took %s after the service closed (want fail-fast, not the 30s call timeout)", d)
	}
}

// 服务回复照常交给 forward（读协程转交，不丢、不串）：连续两次调用各得各的回复。
func TestForwardRepliesThroughReader(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/service/svc1?api_key="+plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitServiceOnline(t, hub, "svc1", true)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var req ForwardRequest
			_ = json.Unmarshal(data, &req)
			resp, _ := json.Marshal(ForwardResponse{Reply: "re:" + req.Text})
			_ = conn.Write(ctx, websocket.MessageText, resp)
		}
	}()
	for _, text := range []string{"one", "two"} {
		got, err := hub.forward(ctx, "svc1", ForwardRequest{ID: text, Text: text}, 5000)
		if err != nil || got != "re:"+text {
			t.Fatalf("forward %s: %q %v", text, got, err)
		}
	}
}

func waitServiceOnline(t *testing.T, hub *Hub, serviceID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(envelopeReadTimeout)
	for {
		hub.mu.Lock()
		online := hub.serviceClients[serviceID] != nil
		hub.mu.Unlock()
		if online == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("service %s online=%v, want %v", serviceID, online, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ③ 复验方 tosonnet55tc2k 补格（REVIEW-HUB-WS-BLOCKING 附录，去 ZZ 前缀并入）：第一次调用超时之后，它的迟到回复
// 在【第二次调用已把请求写出去之后】才到。ForwardResponse 没有请求 ID，回复只能按先后对应 —— 第二次调用绝不能
// 把第一次的迟到回复当成自己的答案。修法（超时即关连接）下第二次调用报错；断言：报错或拿到 re:two，绝不能是 re:one。
func TestLateReplyArrivingAfterNextRequestIsNotMisattributed(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/service/svc1?api_key="+plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitServiceOnline(t, hub, "svc1", true)
	releaseFirst := make(chan struct{})
	go func() {
		for i := 0; ; i++ {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var req ForwardRequest
			_ = json.Unmarshal(data, &req)
			if i == 0 {
				<-releaseFirst // 第二次调用的请求已经写出去之后，才回第一次
			}
			resp, _ := json.Marshal(ForwardResponse{Reply: "re:" + req.Text})
			_ = conn.Write(ctx, websocket.MessageText, resp)
		}
	}()
	if _, err := hub.forward(ctx, "svc1", ForwardRequest{ID: "one", Text: "one"}, 100); err == nil {
		t.Fatal("first forward should time out")
	}
	type result struct {
		got string
		err error
	}
	res := make(chan result, 1)
	go func() {
		g, e := hub.forward(ctx, "svc1", ForwardRequest{ID: "two", Text: "two"}, 5000)
		res <- result{g, e}
	}()
	time.Sleep(300 * time.Millisecond) // 第二次调用已写出请求（或已失败）
	close(releaseFirst)                // 服务现在才回第一次（迟到），随后回第二次
	r := <-res
	if r.got == "re:one" || (r.err == nil && r.got != "re:two") {
		t.Fatalf("second forward got %q %v: the first call's late reply was taken as the second call's answer", r.got, r.err)
	}
}

// ③ 超时即关连接：这条服务连接随即下线；服务重连后，新调用拿到的是自己的回复。
func TestForwardTimeoutClosesServiceConnection(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/service/svc1?api_key=" + plain
	slow, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.CloseNow()
	waitServiceOnline(t, hub, "svc1", true)
	go func() { _, _, _ = slow.Read(ctx) }() // 收请求，不回
	if _, err := hub.forward(ctx, "svc1", ForwardRequest{ID: "one", Text: "one"}, 100); err == nil {
		t.Fatal("forward to a silent service should time out")
	}
	waitServiceOnline(t, hub, "svc1", false)
	fresh, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close(websocket.StatusNormalClosure, "done")
	waitServiceOnline(t, hub, "svc1", true)
	go func() {
		_, data, err := fresh.Read(ctx)
		if err != nil {
			return
		}
		var req ForwardRequest
		_ = json.Unmarshal(data, &req)
		resp, _ := json.Marshal(ForwardResponse{Reply: "re:" + req.Text})
		_ = fresh.Write(ctx, websocket.MessageText, resp)
	}()
	if got, err := hub.forward(ctx, "svc1", ForwardRequest{ID: "two", Text: "two"}, 5000); err != nil || got != "re:two" {
		t.Fatalf("after reconnect: %q %v, want re:two", got, err)
	}
}

// ③ 复验方 tosonnet55tc2k 补格（REVIEW-HUB-WSB-FIX 附录，组长 R-HUB-WSB-CLOSENOW 并入，去 ZZ 前缀）：
// 服务端根本不读时，forward 超时后的关连接也必须立即返回，不得陪对端走 5 秒的关闭握手
// （持着 c.mu，别的调用都被挡住）——钉住 CloseNow 而不是 Close。
func TestForwardTimeoutReturnsPromptlyWhenPeerNeverReads(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/service/svc1?api_key="+plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitServiceOnline(t, hub, "svc1", true) // 对端从此不读
	start := time.Now()
	if _, err := hub.forward(ctx, "svc1", ForwardRequest{ID: "one", Text: "one"}, 100); err == nil {
		t.Fatal("forward should time out")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("forward took %s to return after a 100ms timeout with a non-reading peer (graceful close handshake while holding the call lock?)", d)
	}
	waitServiceOnline(t, hub, "svc1", false)
}
