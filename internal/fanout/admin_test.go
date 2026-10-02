package fanout

// 段3 测试:配置热载防呆(G5)+管理API(G6)+pre-arm 门+TLS规则(G12 部分)。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func adminServer(t *testing.T, cfgPath string) *Server {
	t.Helper()
	s := New(Config{
		ProdURL: "http://127.0.0.1:1", SpoolPool: spoolTestPool(t),
		ConfigPath: cfgPath,
		AdminTokens: map[string]AdminIdentity{
			"adm-tok": {ID: "ops-a", Role: "admin"},
			"opr-tok": {ID: "ops-b", Role: "operator"},
		},
		SkipPreArm: true,
	})
	return s
}

func adminReq(t *testing.T, s *Server, method, path, token, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:9999" // loopback(§F 默认监听面)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func validFC(gen int64) *FileConfig {
	return &FileConfig{
		Generation: gen, AlertsVerified: true,
		Sinks: []SinkConfig{
			{Name: "prod", HubURL: "https://hub.internal", Channel: "c", Role: "authoritative", Filter: "all", Token: "k"},
		},
		Callers: map[string]CallerConfig{"ct": {Name: "prod-hub"}},
	}
}

func TestFileConfigValidateTLSAndForce(t *testing.T) { // G5/G12:TLS 规则+force 防呆
	fc := validFC(0)
	if err := fc.Validate(nil); err != nil {
		t.Fatalf("合法配置被拒:%v", err)
	}
	bad := validFC(0)
	bad.Sinks[0].HubURL = "http://10.0.0.5:8080" // 远端 http=拒
	if err := bad.Validate(nil); err == nil {
		t.Fatal("远端 http sink 应拒载")
	}
	loop := validFC(0)
	loop.Sinks[0].HubURL = "http://127.0.0.1:8790" // loopback http=允许
	if err := loop.Validate(nil); err != nil {
		t.Fatalf("loopback http 应放行:%v", err)
	}
	// force:删 authoritative 无 force=拒;有 force=过
	next := &FileConfig{AlertsVerified: true, Callers: validFC(0).Callers}
	if err := next.Validate(validFC(0)); err == nil {
		t.Fatal("删 authoritative 无 force 应拒")
	}
	next.Force = true
	if err := next.Validate(validFC(0)); err != nil {
		t.Fatalf("有 force 应过:%v", err)
	}
}

func TestFileConfigAtomicSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fanout.yaml")
	fc := validFC(7)
	if err := SaveFileConfigAtomic(path, fc); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0600 {
		t.Fatalf("权限=%v", fi.Mode().Perm())
	}
	got, err := LoadFileConfig(path)
	if err != nil || got.Generation != 7 || len(got.Sinks) != 1 || got.Sinks[0].Token != "k" {
		t.Fatalf("roundtrip: %v %+v", err, got)
	}
}

func TestAdminAuthAndMask(t *testing.T) { // G6:401/非loopback/掩码
	s := adminServer(t, "")
	_ = s.ApplyFileConfig(context.Background(), validFC(3))
	if w := adminReq(t, s, "GET", "/admin/config", "wrong", "", nil); w.Code != 401 {
		t.Fatalf("坏token want 401 got %d", w.Code)
	}
	req := httptest.NewRequest("GET", "/admin/config", nil)
	req.RemoteAddr = "10.1.2.3:555" // 非 loopback
	req.Header.Set("Authorization", "Bearer adm-tok")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("非loopback want 401 got %d", w.Code)
	}
	w2 := adminReq(t, s, "GET", "/admin/config", "adm-tok", "", nil)
	if w2.Code != 200 || w2.Header().Get("ETag") != "3" {
		t.Fatalf("GET %d etag=%q", w2.Code, w2.Header().Get("ETag"))
	}
	if body := w2.Body.String(); strings.Contains(body, `"k"`) || strings.Contains(body, "ct") {
		t.Fatalf("秘密回显:%s", body) // token/caller键 恒掩码
	}
}

func TestAdminPutCASAndApply(t *testing.T) { // G5:412 代次冲突;PUT 生效=表已换
	cfgPath := filepath.Join(t.TempDir(), "fanout.yaml")
	s := adminServer(t, cfgPath)
	_ = s.ApplyFileConfig(context.Background(), validFC(3))
	next := validFC(0)
	next.Sinks[0].Channel = "c2" // 非身份字段变更(改名=删权威,应走 force,另测)
	body, _ := json.Marshal(next)
	if w := adminReq(t, s, "PUT", "/admin/config", "adm-tok", string(body),
		map[string]string{"If-Match": "99"}); w.Code != 412 {
		t.Fatalf("代次错 want 412 got %d", w.Code)
	}
	w := adminReq(t, s, "PUT", "/admin/config", "adm-tok", string(body),
		map[string]string{"If-Match": "3"})
	if w.Code != 200 {
		t.Fatalf("PUT %d: %s", w.Code, w.Body.String())
	}
	if sk, ok := s.sinkByName("prod"); !ok || sk.Channel != "c2" {
		t.Fatalf("热载未生效:%+v", sk)
	}
	if got, _ := LoadFileConfig(cfgPath); got.Generation != 4 || got.Sinks[0].Channel != "c2" {
		t.Fatalf("真源未落盘:%+v", got)
	}
	// 烂配置不接管:非法 sink → 400,当前配置保持
	badBody := strings.Replace(string(body), "authoritative", "bogus-role", 1)
	if w := adminReq(t, s, "PUT", "/admin/config", "adm-tok", badBody,
		map[string]string{"If-Match": "4"}); w.Code != 400 {
		t.Fatalf("烂配置 want 400 got %d", w.Code)
	}
	if sk, ok := s.sinkByName("prod"); !ok || sk.Channel != "c2" {
		t.Fatal("烂配置把好表冲掉了")
	}
}

func TestAdminRBACForceAndWaive(t *testing.T) { // G6/T2:operator 越权=403
	s := adminServer(t, "")
	_ = s.ApplyFileConfig(context.Background(), validFC(3))
	del := &FileConfig{AlertsVerified: true, Force: true, Callers: validFC(0).Callers}
	body, _ := json.Marshal(del)
	if w := adminReq(t, s, "PUT", "/admin/config", "opr-tok", string(body),
		map[string]string{"If-Match": "3"}); w.Code != 403 {
		t.Fatalf("operator force want 403 got %d", w.Code)
	}
	// waive:先造一个 dlq 行
	_, _ = s.spool.AcceptInbound(context.Background(), "k1", []byte("{}"),
		[]SinkTarget{{Name: "prod", Role: "authoritative"}})
	job, _ := s.spool.NextDueInbound(context.Background(), time.Now().Add(time.Hour))
	_, _ = s.spool.MarkSinkFailed(context.Background(), job, "boom", 1, 0, true)
	wv := `{"msg_key":"k1","sink":"prod","reason":"retire"}`
	if w := adminReq(t, s, "POST", "/admin/dlq/waive", "opr-tok", wv, nil); w.Code != 403 {
		t.Fatalf("operator waive want 403 got %d", w.Code)
	}
	if w := adminReq(t, s, "POST", "/admin/dlq/waive", "adm-tok", wv, nil); w.Code != 204 {
		t.Fatalf("admin waive want 204 got %d", w.Code)
	}
	st, _ := s.spool.InboundGlobalStatus(context.Background(), "k1")
	if st != "delivered_with_waiver" {
		t.Fatalf("waive 后=%s", st)
	}
}

func TestAdminDLQReplay(t *testing.T) {
	hub := &sinkHub{status: 422}
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	s := adminServer(t, "")
	fc := validFC(1)
	fc.Sinks[0].HubURL = ts.URL
	fc.Sinks[0].HubURL = "http://127.0.0.1" + strings.TrimPrefix(ts.URL, "http://127.0.0.1")
	_ = s.ApplyFileConfig(context.Background(), fc)
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_r", "ou", "oc"))
	drainSinks(t, s, 2) // 毒件→dlq
	hub.mu.Lock()
	hub.status = 200
	hub.mu.Unlock()
	if w := adminReq(t, s, "POST", "/admin/dlq/replay", "adm-tok", "", nil); w.Code != 200 {
		t.Fatalf("replay %d: %s", w.Code, w.Body.String())
	}
	drainSinks(t, s, 2)
	st, _ := s.spool.InboundGlobalStatus(context.Background(), "b\x1fom_r")
	if st != "delivered" { // C2:replay 成功后才 delivered
		t.Fatalf("replay 后=%s", st)
	}
}

func TestPreArmGate(t *testing.T) { // B2 pre-arm:探测失败=拒连;全绿=起
	okHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["challenge"] == "t17-prearm" && p["token"] == "k" {
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": "t17-prearm"})
			return
		}
		w.WriteHeader(401)
	}))
	defer okHub.Close()
	mk := func(token string, alerts bool) *Server {
		s := New(Config{ProdURL: "http://127.0.0.1:1",
			SpoolPool: spoolTestPool(t)})
		fc := validFC(1)
		fc.AlertsVerified = alerts
		fc.Sinks[0].HubURL = okHub.URL
		fc.Sinks[0].Token = token
		s.mu.Lock()
		s.dynSinks, s.fileCfg = fc.Sinks, fc
		s.mu.Unlock()
		return s
	}
	if err := mk("k", true).PreArm(context.Background()); err != nil {
		t.Fatalf("全绿应过:%v", err)
	}
	if err := mk("wrong", true).PreArm(context.Background()); err == nil {
		t.Fatal("token 探测失败应拒") // 门②
	}
	if err := mk("k", false).PreArm(context.Background()); err == nil {
		t.Fatal("alerts 未 attest 应拒") // 门⑤
	}
}
