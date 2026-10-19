package nodegate

// 节点网关集成用例：真 ws 客户端（对偶 nodeagent 的拨入形状）＋pgtest 真库。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func mkGate(t *testing.T) (*Gate, *hosted.Store, string, string) {
	t.Helper()
	pool := pgtest.Pool(t)
	ctx := context.Background()
	st := hosted.NewStore(pool)
	if err := st.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := st.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// R31 requires an explicit policy revision for newly registered hard nodes.
	// This is a protocol/gateway fixture, not a production capacity policy.
	secret, err := st.RegisterNode(ctx, hosted.Node{ID: "nd_g1", Name: "gate-test", Labels: json.RawMessage(`{}`),
		QuotaMode: "hard", QuotaPolicyRevision: strings.Repeat("a", 64),
		DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	g := New(st)
	g.SetSendTimeout(2 * time.Second)
	return g, st, "nd_g1", secret
}

func dial(t *testing.T, srv *httptest.Server, nodeID, secret string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/node/connect?node_id=" + nodeID
	conn, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
		Subprotocols: []string{"dingwei-node"},
		HTTPHeader:   map[string][]string{"Authorization": {"Bearer " + secret}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func TestGateAuthCommandCapacityLifecycle(t *testing.T) {
	g, st, nodeID, secret := mkGate(t)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx := context.Background()

	// 反向①：坏凭据 ⇒ 401
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/node/connect?node_id=" + nodeID
	_, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"dingwei-node"},
		HTTPHeader:   map[string][]string{"Authorization": {"Bearer wrong"}},
	})
	if err == nil {
		t.Fatal("坏凭据必须拒")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("应 401: %d", resp.StatusCode)
	}

	// 反向②：未连接节点 ⇒ Send 报 node_offline（编排器失败链入口）
	if _, err := g.Send(ctx, nodeID, hosted.NodeCommand{Type: "pull_image"}); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("未连接应 node_offline: %v", err)
	}

	// 接入 ⇒ 节点 online
	conn := dial(t, srv, nodeID, secret)
	defer conn.CloseNow()
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, _ := st.GetNode(ctx, nodeID)
		if n.State == "online" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("接入后节点应 online")
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 容量帧入账
	capPayload, _ := json.Marshal(map[string]any{"mem_free_mb": 15360, "cpu_total": 16})
	frame, _ := json.Marshal(map[string]any{"type": "capacity_report", "payload": json.RawMessage(capPayload)})
	if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		n, _ := st.GetNode(ctx, nodeID)
		var c struct {
			MemFreeMB int64 `json:"mem_free_mb"`
		}
		_ = json.Unmarshal(n.Capacity, &c)
		if c.MemFreeMB == 15360 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("容量应入账: %s", n.Capacity)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 指令往返：Send 下发 ⇒ 节点收到（command_id 非空、type/params 逐字）⇒ 回结果 ⇒ Send 返回
	type recvCmd struct {
		CommandID string          `json:"command_id"`
		Type      string          `json:"type"`
		Params    json.RawMessage `json:"params"`
	}
	go func() {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var rc recvCmd
		if json.Unmarshal(raw, &rc) != nil || rc.CommandID == "" || rc.Type != "pull_image" {
			return
		}
		resp, _ := json.Marshal(map[string]any{"command_id": rc.CommandID, "ok": true})
		_ = conn.Write(ctx, websocket.MessageText, resp)
	}()
	res, err := g.Send(ctx, nodeID, hosted.NodeCommand{Type: "pull_image", Params: json.RawMessage(`{"image":"x"}`)})
	if err != nil || !res.OK {
		t.Fatalf("指令往返: %+v err=%v", res, err)
	}

	// 断线 ⇒ 节点 offline ⇒ Send 报 node_offline
	_ = conn.Close(websocket.StatusNormalClosure, "bye")
	deadline = time.Now().Add(3 * time.Second)
	for {
		n, _ := st.GetNode(ctx, nodeID)
		if n.State == "offline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("断线后节点应 offline")
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, err := g.Send(ctx, nodeID, hosted.NodeCommand{Type: "pull_image"}); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("断线后应 node_offline: %v", err)
	}
}

// 组长 7fabddb0 ⑤：节点接入成功须触发 OnConnect 对账回调。
func TestGateOnConnectFires(t *testing.T) {
	g, _, nodeID, secret := mkGate(t)
	fired := make(chan string, 1)
	g.OnConnect = func(ctx context.Context, id string) { fired <- id }
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	conn := dial(t, srv, nodeID, secret)
	defer conn.CloseNow()
	select {
	case id := <-fired:
		if id != nodeID {
			t.Fatalf("回调节点 id: %s", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("接入后 OnConnect 未触发")
	}
}

// 复验阻断（review-g4-t50-rework 86d78377）：OnConnect 回调里经 Gate.Send 等结果必须可行——
// 同步回调会把读循环卡死在 60s 超时（结果靠读循环收，读循环等回调返回）。相接用例：
// 回调里 Send 一条指令，节点侧应答，回调须毫秒级完成（而不是撑满超时）。
func TestOnConnectCallbackCanSendAndReceive(t *testing.T) {
	g, _, nodeID, secret := mkGate(t)
	type outcome struct {
		ok  bool
		err error
	}
	done := make(chan outcome, 1)
	g.OnConnect = func(ctx context.Context, id string) {
		res, err := g.Send(ctx, id, hosted.NodeCommand{Type: "destroy_seat", Params: json.RawMessage(`{"seat_id":"ag_rc"}`)})
		done <- outcome{ok: res.OK, err: err}
	}
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	conn := dial(t, srv, nodeID, secret)
	defer conn.CloseNow()
	// 节点侧：读指令→回 ok（真读循环路径）
	go func() {
		_, raw, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		var rc struct {
			CommandID string `json:"command_id"`
		}
		if json.Unmarshal(raw, &rc) != nil || rc.CommandID == "" {
			return
		}
		resp, _ := json.Marshal(map[string]any{"command_id": rc.CommandID, "ok": true})
		_ = conn.Write(context.Background(), websocket.MessageText, resp)
	}()
	select {
	case o := <-done:
		if o.err != nil || !o.ok {
			t.Fatalf("回调内 Send 应成功: %+v", o)
		}
	case <-time.After(5 * time.Second): // 修复前会撑满 60s sendMax；5s 裕度足够卡死现形
		t.Fatal("回调内 Send 卡死——同步回调把读循环堵住了")
	}
}

// TestSendWithTimeoutHonorsCallerDeadline（G11，R-G4-BACKUP-CHECKSUM-CTRL 并入）：
// SendWithTimeout 的上限必须真生效——慢节点（睡 500ms 再回）在 200ms 上限下 ⇒ node_command_timeout；
// 同节点 3s 上限下 ⇒ 正常返回。反向：上限被忽略（回落 sendMax 60s）⇒ 本格红（200ms 那一支会等到 500ms 的正常回包）。
func TestSendWithTimeoutHonorsCallerDeadline(t *testing.T) {
	g, _, nodeID, secret := mkGate(t)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	ctx := context.Background()
	conn := dial(t, srv, nodeID, secret)
	defer conn.CloseNow()
	// Dial observes the HTTP upgrade before serve necessarily registers the
	// connection. Establish the send precondition without sending a command;
	// otherwise this deadline test can measure node_offline instead of timeout.
	deadline := time.Now().Add(3 * time.Second)
	for {
		g.mu.Lock()
		registered := g.conns[nodeID] != nil
		g.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("node connection was not registered before deadline test")
		}
		time.Sleep(time.Millisecond)
	}

	// 慢节点：每条指令睡 500ms 再回 ok
	go func() {
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var rc struct {
				CommandID string `json:"command_id"`
			}
			if json.Unmarshal(raw, &rc) != nil || rc.CommandID == "" {
				continue
			}
			time.Sleep(500 * time.Millisecond)
			resp, _ := json.Marshal(map[string]any{"command_id": rc.CommandID, "ok": true})
			if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
				return
			}
		}
	}()

	if _, err := g.SendWithTimeout(ctx, nodeID, hosted.NodeCommand{Type: "export_seat"}, 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), "node_command_timeout") {
		t.Fatalf("200ms 上限对 500ms 慢节点必须超时: %v", err)
	}
	if res, err := g.SendWithTimeout(ctx, nodeID, hosted.NodeCommand{Type: "export_seat"}, 3*time.Second); err != nil || !res.OK {
		t.Fatalf("3s 上限应正常返回: %+v %v", res, err)
	}
}
