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
	secret, err := st.RegisterNode(ctx, hosted.Node{ID: "nd_g1", Name: "gate-test", Labels: json.RawMessage(`{}`),
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
