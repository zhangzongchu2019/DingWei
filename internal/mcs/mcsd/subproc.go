// subproc.go —— E2:真进程边界驱动。
//
// 结构:Daemon 侧起一个 unix socket 监听;SubprocDriver.Spawn 用 os/exec 拉起真子进程 worker
// (同一二进制的 -worker 模式,或测试二进制的 helper 模式);worker 连回 socket,JSON 行协议:
//   worker→daemon: {"hello":"<worker-id>"} 然后 {"receipt":"processing"|"terminal",...}
//   daemon→worker: {"node":...,"gen":...,"rid":...,"behavior":{...}}(envelope;行为注入随件下发,E3 剧本用)
// Health=进程活性(Signal 0);Kill9=真 SIGKILL(模拟崩死);Stop=SIGTERM。
// 回执跨进程到达 daemon 后走与 E1 相同的 guarded 入口(ReceiptProcessing/ReceiptTerminal)。
package mcsd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// WireEnvelope daemon→worker 投递件(behavior=剧本注入)。
type WireEnvelope struct {
	Node     string       `json:"node"`
	Gen      int64        `json:"gen"`
	RID      string       `json:"rid"`
	Behavior WireBehavior `json:"behavior"`
}

// WireBehavior 每件行为注入(E3 剧本随件下发)。
type WireBehavior struct {
	DelayMS        int  `json:"delay_ms,omitempty"`
	Fail           bool `json:"fail,omitempty"`
	DropProcessing bool `json:"drop_processing,omitempty"`
	DropTerminal   bool `json:"drop_terminal,omitempty"`
}

// WireReceipt worker→daemon 回执。
type WireReceipt struct {
	Hello   string `json:"hello,omitempty"`
	Receipt string `json:"receipt,omitempty"` // processing|terminal
	Node    string `json:"node,omitempty"`
	Gen     int64  `json:"gen,omitempty"`
	RID     string `json:"rid,omitempty"`
	OK      bool   `json:"ok,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// SubprocDriver 真子进程驱动(实现 pool.DriverAdapter)。
type SubprocDriver struct {
	Bin      string   // worker 可执行(默认 os.Executable())
	Args     []string // worker 参数模板(不含 -worker-id;测试=helper 模式 env)
	Env      []string // 附加环境(测试 helper 用)
	SockDir  string   // unix socket 目录

	mu      sync.Mutex
	n       int
	daemon  *Daemon
	ln      net.Listener
	sock    string
	conns   map[string]net.Conn // workerID→conn
	workers []*subprocWorker
	// BehaviorFor 剧本钩子:按 node 出行为注入(nil=零注入)。
	BehaviorFor func(node string) WireBehavior
}

// Listen 起 daemon 侧 socket 服务(Spawn 前必须先调)。
func (s *SubprocDriver) Listen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SockDir == "" {
		s.SockDir = os.TempDir()
	}
	s.sock = filepath.Join(s.SockDir, fmt.Sprintf("mcsd-%d.sock", os.Getpid()))
	os.Remove(s.sock)
	ln, err := net.Listen("unix", s.sock)
	if err != nil {
		return err
	}
	s.ln = ln
	s.conns = map[string]net.Conn{}
	go s.acceptLoop()
	return nil
}

// Bind 注入守护(回执入账)。
func (s *SubprocDriver) Bind(d *Daemon) { s.mu.Lock(); s.daemon = d; s.mu.Unlock() }

// Close 关服务(worker 由池 Stop)。
func (s *SubprocDriver) Close() { s.mu.Lock(); defer s.mu.Unlock(); if s.ln != nil { s.ln.Close() }; os.Remove(s.sock) }

func (s *SubprocDriver) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(conn)
	}
}

func (s *SubprocDriver) serveConn(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	var workerID string
	for sc.Scan() {
		var r WireReceipt
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Hello != "" {
			workerID = r.Hello
			s.mu.Lock()
			s.conns[workerID] = conn
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		d := s.daemon
		s.mu.Unlock()
		if d == nil {
			continue
		}
		ctx := context.Background()
		switch r.Receipt {
		case "processing":
			_ = d.ReceiptProcessing(ctx, r.Node, r.Gen, r.RID)
		case "terminal":
			_ = d.ReceiptTerminal(ctx, r.Node, r.Gen, r.RID, r.OK, r.Ref)
		}
	}
	if workerID != "" {
		s.mu.Lock()
		delete(s.conns, workerID)
		s.mu.Unlock()
	}
}

// Spawn 实现 pool.DriverAdapter:拉真子进程。
func (s *SubprocDriver) Spawn(ctx context.Context) (pool.Worker, error) {
	s.mu.Lock()
	id := fmt.Sprintf("sw%d", s.n)
	s.n++
	bin := s.Bin
	sock := s.sock
	args := append([]string{}, s.Args...)
	env := append([]string{}, s.Env...)
	s.mu.Unlock()
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		bin = exe
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "MCS_WORKER_SOCK="+sock, "MCS_WORKER_ID="+id)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcsd/subproc: spawn: %w", err)
	}
	go cmd.Wait() // 收尸,防僵尸
	w := &subprocWorker{id: id, drv: s, proc: cmd.Process}
	s.mu.Lock()
	s.workers = append(s.workers, w)
	s.mu.Unlock()
	// 等 hello(有界):连不上=起失败
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, ok := s.conns[id]
		s.mu.Unlock()
		if ok {
			return w, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("mcsd/subproc: worker %s 未连回(5s)", id)
}

// Workers 观测(剧本/测试用)。
func (s *SubprocDriver) Workers() []*subprocWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*subprocWorker, len(s.workers))
	copy(out, s.workers)
	return out
}

type subprocWorker struct {
	id   string
	drv  *SubprocDriver
	proc *os.Process
}

func (w *subprocWorker) ID() string { return w.id }
func (w *subprocWorker) Caps() pool.Caps {
	return pool.Caps{"send": true}
}

// Health 真进程活性:Signal 0(kill -9 后立刻失败)。
func (w *subprocWorker) Health(context.Context) error {
	if err := w.proc.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("worker %s: %w", w.id, err)
	}
	return nil
}

// Deliver 经 socket 下发 envelope(worker 掉线=错,交池/redeliver 收敛)。
func (w *subprocWorker) Deliver(_ context.Context, env pool.Envelope) error {
	w.drv.mu.Lock()
	conn, ok := w.drv.conns[w.id]
	beh := WireBehavior{}
	if w.drv.BehaviorFor != nil {
		beh = w.drv.BehaviorFor(env.NodeID)
	}
	w.drv.mu.Unlock()
	if !ok {
		return fmt.Errorf("worker %s: no conn", w.id)
	}
	bs, err := json.Marshal(WireEnvelope{Node: env.NodeID, Gen: env.Generation, RID: env.RID, Behavior: beh})
	if err != nil {
		return err
	}
	_, err = conn.Write(append(bs, '\n'))
	return err
}

func (w *subprocWorker) Stop(context.Context) error {
	_ = w.proc.Signal(syscall.SIGTERM)
	return nil
}

// Kill9 剧本用:真 SIGKILL 模拟崩死(无任何回执)。
func (w *subprocWorker) Kill9() { _ = w.proc.Kill() }

// RunWorker worker 侧主循环(cmd -worker 模式与测试 helper 共用):
// 连 socket→hello→循环收 envelope→按 behavior 模拟执行→回 processing/terminal。
func RunWorker() error {
	sock := os.Getenv("MCS_WORKER_SOCK")
	id := os.Getenv("MCS_WORKER_ID")
	if sock == "" || id == "" {
		return fmt.Errorf("mcs worker: MCS_WORKER_SOCK/MCS_WORKER_ID 未设")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return err
	}
	defer conn.Close()
	var wmu sync.Mutex
	enc := json.NewEncoder(conn)
	send := func(r WireReceipt) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = enc.Encode(r)
	}
	if err := func() error { wmu.Lock(); defer wmu.Unlock(); return enc.Encode(WireReceipt{Hello: id}) }(); err != nil {
		return err
	}
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		var env WireEnvelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			continue
		}
		go func(env WireEnvelope) {
			if env.Behavior.DelayMS > 0 {
				time.Sleep(time.Duration(env.Behavior.DelayMS) * time.Millisecond)
			}
			if !env.Behavior.DropProcessing {
				send(WireReceipt{Receipt: "processing", Node: env.Node, Gen: env.Gen, RID: env.RID})
			}
			if env.Behavior.DropTerminal {
				return
			}
			send(WireReceipt{Receipt: "terminal", Node: env.Node, Gen: env.Gen, RID: env.RID,
				OK: !env.Behavior.Fail, Ref: "subproc://" + env.Node})
		}(env)
	}
	return nil
}
