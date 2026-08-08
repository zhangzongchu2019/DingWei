// Package hubdriver 是 M1 派发桥:pool.Worker/DriverAdapter 的真 CLI 实现,
// 经 DingWei hub 投递任务信封到既有活会话(send.py 同信道)。
//
// M1 边界(排期 v0.3 冻结):
//   - Deliver=外呼半边(记账先行已由 dispatch 层做);回执走 mcsd 收件面(P3 集成件)。
//   - Spawn=绑定 Registry 里既有活会话,不真拉进程(真拉起=M3 池/L2 轮换域)。
//   - Health=在线清单新鲜度+该会话在列(双向 challenge 探活=M4 域,此处仅只读判据)。
//
// 契约依据:M0 v0.7(e49378b) C-5:本层 worker=SS 级执行体,不持 task authority,
// 结果以 envelope 经收件面走 role×op 闸入账;身份=会话自身 principal,不代写。
package hubdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// Sender 发送一条信封到目标会话(全名寻址)。生产=SendPy;测试=fake。
type Sender interface {
	Send(ctx context.Context, toFullName string, body []byte) error
}

// SendPy 经本机 send.py 外呼(sessionHelper 注入凭据)。
type SendPy struct {
	Script string // 默认 /home/devo48/.dingwei/send.py
}

func (s *SendPy) Send(ctx context.Context, to string, body []byte) error {
	script := s.Script
	if script == "" {
		script = os.ExpandEnv("$HOME/.dingwei/send.py")
	}
	// 信封走临时文件+--file(团队规约:结构化正文不过 shell 内联)。
	f, err := os.CreateTemp("", "mcs-env-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	f.Close()
	cmd := exec.CommandContext(ctx, "python3", script, to, "--file", f.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("send.py %s: %v: %s", to, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// OnlineList 只读活性判据:清单文件新鲜(≤stale)且全名在列。
type OnlineList struct {
	Path  string
	Stale time.Duration // 默认 2min(清单头部自述)
}

func (o *OnlineList) alive(fullName string) error {
	stale := o.Stale
	if stale <= 0 {
		stale = 2 * time.Minute
	}
	st, err := os.Stat(o.Path)
	if err != nil {
		return fmt.Errorf("在线清单不可读: %w", err)
	}
	if age := time.Since(st.ModTime()); age > stale {
		return fmt.Errorf("在线清单过期 %s(>%s),本机 sessionHelper 疑异常", age.Round(time.Second), stale)
	}
	b, err := os.ReadFile(o.Path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), fullName) {
		return fmt.Errorf("%s 不在在线清单", fullName)
	}
	return nil
}

// WireEnvelope 是 hub 信封线格式(收件面 Receiver 对偶解析;字段冻结于 M1 出件评审)。
// 收发人强制(张宗楚 07-25 令):From/To 缺一即拒发,不出线。
type WireEnvelope struct {
	Kind       string `json:"kind"` // 恒 "mcs_dispatch_v1"
	NodeID     string `json:"node_id"`
	Generation int64  `json:"generation"`
	RID        string `json:"rid"` // delivery_id,幂等键(执行点拦)
	Payload    string `json:"payload,omitempty"`
	From       string `json:"from"` // 派发方全名(必填)
	To         string `json:"to"`   // 收件工人全名(必填,信封自含防错投取证)
}

// Worker 绑定一个既有活会话。
type Worker struct {
	full   string
	caps   pool.Caps
	send   Sender
	online *OnlineList
	from   string
	mu     sync.Mutex
	closed bool
}

func (w *Worker) ID() string      { return w.full }
func (w *Worker) Caps() pool.Caps { return w.caps }

func (w *Worker) Health(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("worker %s 已停用", w.full)
	}
	w.mu.Unlock()
	return w.online.alive(w.full)
}

func (w *Worker) Deliver(ctx context.Context, env pool.Envelope) error {
	if w.from == "" || w.full == "" {
		return fmt.Errorf("信封收发人不全(from=%q to=%q),拒发(张宗楚 07-25 令)", w.from, w.full)
	}
	if err := w.Health(ctx); err != nil {
		return fmt.Errorf("deliver 前置健康检查: %w", err)
	}
	body, err := json.Marshal(WireEnvelope{
		Kind: "mcs_dispatch_v1", NodeID: env.NodeID, Generation: env.Generation,
		RID: env.RID, Payload: string(env.Payload), From: w.from, To: w.full,
	})
	if err != nil {
		return err
	}
	return w.send.Send(ctx, w.full, body)
}

func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true // M1 只解除绑定;真收尸/围栏=M4/L2 域
	return nil
}

// Adapter 从 Registry(会话全名清单)按序绑定,实现 pool.DriverAdapter。
type Adapter struct {
	Registry []string // 可绑定的工人会话全名(配置注入)
	Caps     pool.Caps
	Send     Sender
	Online   *OnlineList
	From     string // 派发方全名

	mu   sync.Mutex
	next int
}

func (a *Adapter) Spawn(ctx context.Context) (pool.Worker, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for tried := 0; tried < len(a.Registry); tried++ {
		full := a.Registry[a.next%len(a.Registry)]
		a.next++
		w := &Worker{full: full, caps: a.Caps, send: a.Send, online: a.Online, from: a.From}
		if err := w.Health(ctx); err != nil {
			continue // 死会话跳过,试下一个
		}
		return w, nil
	}
	return nil, fmt.Errorf("registry %d 个会话均不可用", len(a.Registry))
}
