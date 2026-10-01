package nodeagent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// r6 整包自查补漏：docker 应答——200 但 JSON 畸形 ⇒ 解析错误上抛（不拿零值结构继续跑）；
// ≥300 ⇒ engineError 带状态码。
func TestEngineMalformedResponseFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/json") {
			_, _ = w.Write([]byte("{malformed-json"))
			return
		}
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	eng := newFakeEngine(srv.Client(), srv.URL)
	if _, err := eng.containerInspect(context.Background(), "abc"); err == nil {
		t.Fatal("200 但畸形 JSON 必须报错（不得拿零值继续）")
	}
	err := eng.imagePull(context.Background(), "hub.local/dingwei/base@sha256:"+
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	ee, ok := err.(*engineError)
	if !ok || ee.Status != 500 {
		t.Fatalf("≥300 应为 engineError 带状态码: %v", err)
	}
}
