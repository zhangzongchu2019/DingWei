package nodeagent

import (
	"context"
	"errors"
	"net"
	"testing"
)

// TestDedicatedSocketIsolation 反向用例（组长硬要求，台账 01e52e7d）：
// socket 配成独立实例时，任何指令都不得落到默认 /var/run/docker.sock。
// 经 newEngine(独立路径) 全链路拨号，记录实际拨号目标。
func TestDedicatedSocketIsolation(t *testing.T) {
	const dedicated = "/var/run/dingwei-hosted/docker.sock"
	const def = "/var/run/docker.sock"

	var dialed []string
	orig := netDial
	netDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, errors.New("recorded-only")
	}
	t.Cleanup(func() { netDial = orig })

	// 真实指令路径（拉镜像）走一次——拨号目标必须是独立实例、默认 socket 命中数恒 0
	eng := newEngine(dedicated)
	_ = eng.imagePull(context.Background(), "hub.local/dingwei/base@sha256:"+
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	if len(dialed) == 0 {
		t.Fatal("应发生一次拨号")
	}
	for _, a := range dialed {
		if a != dedicated {
			t.Fatalf("拨号目标必须是独立实例 %s, 实得 %s", dedicated, a)
		}
		if a == def {
			t.Fatalf("任何指令不得落到默认 socket %s", def)
		}
	}
}
