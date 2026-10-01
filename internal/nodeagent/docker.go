package nodeagent

// Docker Engine API 最小客户端（T-G4-12）——走 /var/run/docker.sock，stdlib only。
// 🔴 不加 docker SDK：主模块直接依赖数有 dependency-ratchet 闸（direct_count=6 基线），
//    且我们只用得到七八个端点。
// 可测试性：base 与 http.Client 可注入 ⇒ 单测用内存假 Engine，真机上才是 unix socket。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type engine struct {
	hc   *http.Client
	base string // "http://d"（socket 语义占位）
}

// dialFunc 拨号器（可注入——socket 路径隔离的反向用例要核拨号目标）。
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// netDial 底层拨号（测试可替换以记录拨号目标；生产即 net.Dialer）。
var netDial dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func unixDialer(sockPath string) dialFunc {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		// 🔴 拨号目标只取配置值（闭包里的 sockPath），忽略调用方传入的 addr——
		// 组长硬要求（台账 01e52e7d）：任何指令不得落到默认 /var/run/docker.sock。
		return netDial(ctx, "unix", sockPath)
	}
}

func newEngine(sockPath string) *engine {
	return newEngineWithDial(unixDialer(sockPath))
}

// newEngineWithDial 自定义拨号器的 Engine（测试记录拨号目标用）。
func newEngineWithDial(d dialFunc) *engine {
	tr := &http.Transport{DialContext: d}
	return &engine{hc: &http.Client{Transport: tr, Timeout: 10 * time.Minute}, base: "http://d"}
}

// 假 Engine 注入（测试用）。
func newFakeEngine(hc *http.Client, base string) *engine { return &engine{hc: hc, base: base} }

type engineError struct {
	Status  int
	Message string
}

func (e *engineError) Error() string { return fmt.Sprintf("docker engine %d: %s", e.Status, e.Message) }

func (e *engine) call(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		return &engineError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// imagePull 拉取并校验 digest（fromImage 带 @sha256:）。
func (e *engine) imagePull(ctx context.Context, ref string) error {
	// fromImage 与 tag 分传：repo@sha256:digest 时 tag 置空，Engine 按 digest 拉。
	return e.call(ctx, "POST", "/images/create?fromImage="+urlQueryEscape(ref), nil, nil)
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer("/", "%2F", ":", "%3A", "@", "%40")
	return r.Replace(s)
}

type containerCreateResp struct {
	ID string `json:"Id"`
}

type containerState struct {
	Status  string `json:"Status"`
	Running bool   `json:"Running"`
	Health  *struct {
		Status string `json:"Status"`
	} `json:"Health,omitempty"`
}

type containerInspectResp struct {
	ID    string         `json:"Id"`
	Name  string         `json:"Name"`
	State containerState `json:"State"`
	Image string         `json:"Image"`
}

func (e *engine) containerCreate(ctx context.Context, name string, spec map[string]any) (string, error) {
	var out containerCreateResp
	if err := e.call(ctx, "POST", "/containers/create?name="+name, spec, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (e *engine) containerStart(ctx context.Context, id string) error {
	return e.call(ctx, "POST", "/containers/"+id+"/start", nil, nil)
}

func (e *engine) containerStop(ctx context.Context, id string) error {
	err := e.call(ctx, "POST", "/containers/"+id+"/stop?t=10", nil, nil)
	if ee, ok := err.(*engineError); ok && ee.Status == 304 { // 已停
		return nil
	}
	return err
}

func (e *engine) containerRestart(ctx context.Context, id string) error {
	return e.call(ctx, "POST", "/containers/"+id+"/restart?t=10", nil, nil)
}

func (e *engine) containerRemove(ctx context.Context, id string) error {
	err := e.call(ctx, "DELETE", "/containers/"+id+"?force=true", nil, nil)
	if ee, ok := err.(*engineError); ok && ee.Status == 404 {
		return nil
	}
	return err
}

func (e *engine) containerInspect(ctx context.Context, id string) (containerInspectResp, error) {
	var out containerInspectResp
	err := e.call(ctx, "GET", "/containers/"+id+"/json", nil, &out)
	return out, err
}

func (e *engine) networkCreate(ctx context.Context, name string) error {
	return e.call(ctx, "POST", "/networks/create", map[string]any{
		"Name": name, "Driver": "bridge", "Internal": false,
	}, nil)
}

func (e *engine) networkRemove(ctx context.Context, name string) error {
	err := e.call(ctx, "DELETE", "/networks/"+name, nil, nil)
	if ee, ok := err.(*engineError); ok && ee.Status == 404 {
		return nil
	}
	return err
}

// NewEngine 真机 Engine（unix socket；cmd/nodeagent 用）。
func NewEngine(sockPath string) *engine { return newEngine(sockPath) }
