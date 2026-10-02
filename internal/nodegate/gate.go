// Package nodegate Hub 侧节点网关（T-G4-35 同批装配①）：节点代理的 WSS 服务端。
//
// 协议（与 internal/nodeagent 对偶）：
//
//	· 节点拨入 GET /api/node/connect?node_id=nd_x，Authorization: Bearer <节点凭据>，
//	  子协议 dingwei-node；验签＝hosted.Store.AuthenticateNode（哈希比对、撤销即拒）；
//	· Hub→节点：Command{command_id, type, params}；节点→Hub：Result{command_id, ok, payload|error}
//	  与容量帧 {"type":"capacity_report","payload":{...}}；
//	· 节点台账幂等（command_id 去重），Hub 侧断线即该连接的未决指令一律失败
//	  （node_offline——编排器失败链走 Retire/解绑清理；重发策略留给后续切片，节点侧重放安全）。
//
// 连接即在线：接入成功置节点 online（首帧容量入账）；断线置 offline。同 node_id 新连接顶掉旧的。
package nodegate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

var ErrNodeOffline = errors.New("node_offline")

type command struct {
	CommandID string          `json:"command_id"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params,omitempty"`
}

type resultFrame struct {
	CommandID string          `json:"command_id"`
	OK        bool            `json:"ok"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type capacityFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type nodeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex // 写锁：指令与（未来的）Hub 主动帧同一条连接
}

// Gate 节点网关：http.Handler（接入点）＋hosted.NodeGateway（指令下发）。
type Gate struct {
	st      *hosted.Store
	sendMax time.Duration // 单指令等结果上限（默认 60s）
	// OnConnect 节点接入成功回调（Hub 侧对账入口：离线销毁的补清——组长 7fabddb0 ⑤）。
	// 在置 online 之后、读循环之前同步调用；nil 跳过。
	OnConnect func(ctx context.Context, nodeID string)

	mu      sync.Mutex
	conns   map[string]*nodeConn              // nodeID → 活连接
	waiters map[string]chan hosted.NodeResult // commandID → 等待者
}

func New(st *hosted.Store) *Gate {
	return &Gate{st: st, sendMax: 60 * time.Second, conns: map[string]*nodeConn{}, waiters: map[string]chan hosted.NodeResult{}}
}

// SetSendTimeout 测试用。
func (g *Gate) SetSendTimeout(d time.Duration) { g.sendMax = d }

// Handler 接入点（挂 mux：/api/node/connect）。
func (g *Gate) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if nodeID == "" || secret == "" || g.st.AuthenticateNode(r.Context(), nodeID, secret) != nil {
			http.Error(w, `{"error":"node_auth_failed"}`, http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"dingwei-node"}})
		if err != nil {
			return
		}
		g.serve(r.Context(), nodeID, conn)
	})
}

func (g *Gate) serve(ctx context.Context, nodeID string, conn *websocket.Conn) {
	defer conn.CloseNow()
	nc := &nodeConn{conn: conn}
	g.mu.Lock()
	if old := g.conns[nodeID]; old != nil {
		old.conn.Close(websocket.StatusGoingAway, "replaced") // 同节点新连接顶掉旧的
	}
	g.conns[nodeID] = nc
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		if g.conns[nodeID] == nc {
			delete(g.conns, nodeID)
		}
		g.mu.Unlock()
		_ = g.st.SetNodeState(context.Background(), nodeID, "offline")
	}()
	// 接入即在线（容量空对象入账；首帧 capacity_report 随后覆盖）
	_ = g.st.SetNodeOnline(context.Background(), nodeID, json.RawMessage(`{}`))
	// 🔴 OnConnect 必须异步跑（复验 REVIEW-G4-T50-REWORK 第三节）：装配里它是 orch.ReconcileNode，会经 Send 下发
	//    destroy_seat 并【等结果】，而结果帧要靠下面这个读循环来收。同步调用 ⇒ 读循环没起来 ⇒ Send 等满 sendMax
	//    超时 ⇒ reconcile_done 永远写不下（节点其实已执行），且每个待对账席位卡住本连接的读循环。
	//    连接一断 connCtx 即取消，回调里尚未完成的 Send 随之返回。
	connCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if g.OnConnect != nil {
		go g.OnConnect(connCtx, nodeID)
	}

	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		// 帧分路：容量帧（type=capacity_report）与指令结果（command_id 非空）
		var probe struct {
			Type      string `json:"type"`
			CommandID string `json:"command_id"`
		}
		if json.Unmarshal(raw, &probe) != nil {
			continue // 畸形帧跳过：读路径不放大为断连（节点侧坏帧有 bad_command_frame 对偶）
		}
		if probe.Type == "capacity_report" {
			var cf capacityFrame
			if json.Unmarshal(raw, &cf) == nil {
				_ = g.st.SetNodeOnline(context.Background(), nodeID, cf.Payload)
			}
			continue
		}
		if probe.CommandID == "" {
			continue
		}
		var rf resultFrame
		if json.Unmarshal(raw, &rf) != nil {
			continue
		}
		g.mu.Lock()
		ch := g.waiters[rf.CommandID]
		delete(g.waiters, rf.CommandID)
		g.mu.Unlock()
		if ch != nil {
			ch <- hosted.NodeResult{OK: rf.OK, Error: rf.Error, Payload: rf.Payload}
		}
	}
}

// Send 实现 hosted.NodeGateway：下发指令并等结果。节点未连接 ⇒ ErrNodeOffline。
func (g *Gate) Send(ctx context.Context, nodeID string, cmd hosted.NodeCommand) (hosted.NodeResult, error) {
	g.mu.Lock()
	nc := g.conns[nodeID]
	g.mu.Unlock()
	if nc == nil {
		return hosted.NodeResult{}, ErrNodeOffline
	}
	id, err := newCommandID()
	if err != nil {
		return hosted.NodeResult{}, err
	}
	ch := make(chan hosted.NodeResult, 1)
	g.mu.Lock()
	g.waiters[id] = ch
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.waiters, id)
		g.mu.Unlock()
	}()

	raw, _ := json.Marshal(command{CommandID: id, Type: cmd.Type, Params: cmd.Params})
	nc.mu.Lock()
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = nc.conn.Write(wctx, websocket.MessageText, raw)
	cancel()
	nc.mu.Unlock()
	if err != nil {
		return hosted.NodeResult{}, ErrNodeOffline // 写失败≈连接已死，读循环会把节点置 offline
	}
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(g.sendMax):
		return hosted.NodeResult{}, errors.New("node_command_timeout")
	case <-ctx.Done():
		return hosted.NodeResult{}, ctx.Err()
	}
}

func newCommandID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "cmd_" + hex.EncodeToString(b[:]), nil
}
