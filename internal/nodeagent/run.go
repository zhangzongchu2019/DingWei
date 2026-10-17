package nodeagent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Agent 节点代理骨架。
type Agent struct {
	cfg         Config
	handlers    map[string]Handler
	ledger      *ledger
	docker      *dockerOps
	commandGate func(Command) error
	background  func(context.Context)
	policyClose func()
	// T-G4-13：容量采集器（dockerOps 注册；未注册则心跳静默跳过）
	capacity          func(ctx context.Context) (Capacity, error)
	heartbeatInterval time.Duration
}

func New(cfg Config) (*Agent, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := secureLedgerDir(cfg.LedgerPath); err != nil {
		return nil, err
	}
	l, err := loadLedger(cfg.LedgerPath)
	if err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, handlers: map[string]Handler{}, ledger: l}
	// 骨架自带的容量上报桩：T-G4-13 换成真采集。
	a.handlers[CmdReportCapacity] = func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(map[string]any{
			"node_id": cfg.NodeID, "labels": cfg.Labels, "at": time.Now().UTC(),
		})
	}
	return a, nil
}

// Register 注册指令执行器（T-G4-12 的 Docker 指令集）。
func (a *Agent) Register(cmdType string, h Handler) { a.handlers[cmdType] = h }

// Run 长连主循环：连接→握手→读指令→执行/幂等重报→回送；断线退避重连。
func (a *Agent) Run(ctx context.Context) error {
	if a.policyClose != nil {
		defer a.policyClose()
	}
	if a.background != nil {
		bgctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); a.background(bgctx) }()
		defer func() { cancel(); <-done }()
	}
	backoff := reconnectBase
	for {
		if err := a.serve(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 断线重连（对账由 Hub 侧发起：重连后 Hub 重放未确认指令 ⇒ 幂等台账兜住）
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > reconnectMax {
			backoff = reconnectMax
		}
	}
}

func (a *Agent) serve(ctx context.Context) error {
	secret, err := a.cfg.secret()
	if err != nil {
		return err
	}
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+secret) // 凭据只在这里出现；不进 URL、不进日志
	opts := &websocket.DialOptions{
		HTTPHeader:   hdr,
		Subprotocols: []string{"dingwei-node"},
	}
	if a.cfg.HubCertSHA256 != "" { // 钉证模式（自签阶段）；为空则走系统 CA
		tr, err := TLSPinnedTransport(a.cfg.HubCertSHA256)
		if err != nil {
			return err
		}
		opts.HTTPClient = &http.Client{Transport: tr}
	}
	conn, _, err := websocket.Dial(ctx, a.cfg.HubURL, opts)
	if err != nil {
		return fmt.Errorf("nodeagent: dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	w := &wsWriter{conn: conn}
	// 重连对账＋周期心跳＋变更触发（T-G4-13）；容量采集器未注册时静默跳过
	hbCtx, hbStop := context.WithCancel(ctx)
	defer hbStop()
	if a.capacity != nil {
		go a.heartbeat(hbCtx, w)
	}

	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var ack struct {
			Type string   `json:"type"`
			IDs  []string `json:"ids"`
		}
		if json.Unmarshal(raw, &ack) == nil && ack.Type == "quota_event_ack" {
			if a.docker != nil && a.docker.soft != nil {
				_ = a.docker.soft.acknowledge(ack.IDs)
			}
			continue
		}
		var cmd Command
		if err := json.Unmarshal(raw, &cmd); err != nil {
			a.reply(ctx, w, Result{CommandID: "", OK: false, Error: "bad_command_frame"})
			continue
		}
		a.reply(ctx, w, a.execute(ctx, cmd))
	}
}

// wsWriter 并发写锁（心跳 goroutine 与指令回送同用一条连接）。
type wsWriter struct {
	conn *websocket.Conn
}

func (w *wsWriter) write(ctx context.Context, raw []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return w.conn.Write(wctx, websocket.MessageText, raw)
}

// capacityFrame 容量上报帧（与 Result 区分：type=capacity_report）。
type capacityFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// heartbeat 重连对账即发一次；此后每 15s 检查一次：距上次满一个周期 ⇒ 周期心跳；
// 余量/席位清单变化超阈值 ⇒ 变更触发即发（不等周期）。
func (a *Agent) heartbeat(ctx context.Context, w *wsWriter) {
	interval := a.heartbeatInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	var last Capacity
	lastSent := time.Now()
	send := func() {
		c, err := a.capacity(ctx)
		if err != nil {
			return
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return
		}
		frame, err := json.Marshal(capacityFrame{Type: "capacity_report", Payload: raw})
		if err != nil {
			return
		}
		if w.write(ctx, frame) == nil {
			last = c
			lastSent = time.Now()
		}
	}
	send() // 对账：连上即发
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, err := a.capacity(ctx)
			if err != nil {
				continue
			}
			if time.Since(lastSent) >= interval || changedBeyond(last, c) {
				send()
			}
		}
	}
}

// execute 幂等执行：台账命中 ⇒ 直接重报首次结果（不重复执行）；
// 非白名单类型 ⇒ 拒（unknown_command_type，不落台账——它本来就不该被认成指令）。
func (a *Agent) execute(ctx context.Context, cmd Command) Result {
	if !commandWhitelist[cmd.Type] {
		return Result{CommandID: cmd.CommandID, OK: false, Error: "unknown_command_type"}
	}
	if a.commandGate != nil {
		if err := a.commandGate(cmd); err != nil {
			return Result{CommandID: cmd.CommandID, OK: false, Error: err.Error()}
		}
	}
	skipLedger := noLedgerCommands[cmd.Type]
	if !skipLedger {
		if r, ok := a.ledger.Get(cmd.CommandID); ok {
			return r // 幂等重报：重发与重连重放都走这条
		}
	}
	h, ok := a.handlers[cmd.Type]
	var r Result
	if !ok {
		r = Result{CommandID: cmd.CommandID, OK: false, Error: "handler_not_ready"}
	} else {
		payload, err := h(ctx, cmd.Params)
		if err != nil {
			r = Result{CommandID: cmd.CommandID, OK: false, Error: err.Error()}
		} else {
			r = Result{CommandID: cmd.CommandID, OK: true, Payload: payload}
		}
	}
	if skipLedger {
		return r
	}
	if err := a.ledger.Put(r); err != nil {
		return Result{CommandID: cmd.CommandID, OK: false, Error: "ledger_write_failed"}
	}
	return r
}

func (a *Agent) reply(ctx context.Context, w *wsWriter, r Result) {
	raw, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = w.write(ctx, raw)
}

// TLSPinnedTransport 节点验 Hub 的一层：钉住服务证书 SHA-256（自签阶段尤其要钉）。
// 供 Hub 正式证书未就位前使用；就位后可换系统 CA 校验。
func TLSPinnedTransport(wantSHA256 string) (*http.Transport, error) {
	want := strings.ToLower(strings.ReplaceAll(wantSHA256, ":", ""))
	if len(want) != 64 {
		return nil, fmt.Errorf("nodeagent: HubCertSHA256 需 64 位 hex")
	}
	return &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, // 钉证模式：跳过 CA 链，改在 VerifyPeerCertificate 里钉指纹
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("nodeagent: hub 未出示证书")
			}
			sum := sha256.Sum256(rawCerts[0])
			if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
				return fmt.Errorf("nodeagent: hub 证书指纹不符（换证须重新下发配置）")
			}
			return nil
		},
	}}, nil
}
