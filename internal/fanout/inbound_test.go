package fanout

// 段2 测试:ws 接入+N-sink 投递+全局终态(设计 v2.3 §A/§B/§K1)。
// 盖 G1(装配归一 payload 形)、G2 逻辑层(WAL-先于-ack/崩溃窗恢复)、
// G10(auth 语义/waive/besteffort 有界/毒件不 HOL)、G12 入站侧(401 恒重试)。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func wsMsg(bot, msgID, sender, chatID string) model.Message {
	return model.Message{
		ID: msgID, ChatEntityID: bot + ":group:" + chatID, Direction: model.DirectionIn,
		BotChannelID: bot, FeishuMsgID: msgID, ChatType: model.ChatGroup,
		SenderOpenID: sender, Content: `{"text":"hi"}`, CreatedAt: time.Unix(1690000000, 0),
	}
}

// sinkHub 假 Hub:记录收到的投递,可编程响应码。
type sinkHub struct {
	mu     sync.Mutex
	got    []map[string]any
	status int
}

func (h *sinkHub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		h.mu.Lock()
		h.got = append(h.got, p)
		st := h.status
		h.mu.Unlock()
		if st == 0 {
			st = 200
		}
		w.WriteHeader(st)
	}
}

func (h *sinkHub) count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.got) }

func inboundServer(t *testing.T, sinks []SinkConfig, wl Whitelist) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	wlPath := filepath.Join(dir, "wl.json")
	b, _ := json.Marshal(wl)
	if err := os.WriteFile(wlPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	s := New(Config{
		ProdURL: "http://127.0.0.1:1", WhitelistFile: wlPath,
		SpoolPool: pgtest.Pool(t), Sinks: sinks,
		MaxAttempts: 3, BesteffortMaxAttempts: 2,
	})
	if s.spool == nil {
		t.Fatal("spool 未打开")
	}
	return s, dir
}

// drainSinks 驱动 sink worker 逻辑至无到期件(越过退避窗)。
func drainSinks(t *testing.T, s *Server, rounds int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < rounds; i++ {
		job, err := s.spool.NextDueInbound(ctx, time.Now().Add(24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			return
		}
		s.deliverSink(ctx, job)
	}
}

func TestWSInboundPayloadShapeAndDelivery(t *testing.T) { // G1+K1 形
	hub := &sinkHub{}
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	s, _ := inboundServer(t, []SinkConfig{
		{Name: "prod", HubURL: ts.URL, Channel: "unifiedrobot", Role: "authoritative",
			Filter: "all", Token: "sek-1"},
	}, Whitelist{})
	if err := s.onWSInbound(context.Background(), wsMsg("unifiedrobot", "om_1", "ou_a", "oc_g")); err != nil {
		t.Fatal(err)
	}
	drainSinks(t, s, 4)
	if hub.count() != 1 {
		t.Fatalf("投递数=%d", hub.count())
	}
	p := hub.got[0]
	if p["v"] != float64(1) || p["token"] != "sek-1" || p["msg_id"] != "om_1" ||
		p["chat_type"] != "group" || p["chat_id"] != "oc_g" || p["sender_open_id"] != "ou_a" {
		t.Fatalf("K1 payload 形不符:%v", p)
	}
	if raw, ok := p["raw"].(map[string]any); !ok || raw["text"] != "hi" {
		t.Fatalf("raw 缺失:%v", p["raw"])
	}
	st, _ := s.spool.InboundGlobalStatus(context.Background(), "unifiedrobot\x1fom_1")
	if st != "delivered" {
		t.Fatalf("global=%s", st)
	}
}

func TestWSInboundWALBeforeAck(t *testing.T) { // G2:spool 坏=不 ack(返回 err)
	s, _ := inboundServer(t, []SinkConfig{{Name: "p", HubURL: "http://x", Channel: "c",
		Role: "authoritative", Filter: "all"}}, Whitelist{})
	_ = s.spool.Close() // 模拟磁盘故障
	if err := s.onWSInbound(context.Background(), wsMsg("b", "om_2", "ou", "oc")); err == nil {
		t.Fatal("spool 坏仍 ack=违反 WAL-先于-ack 契约")
	}
}

func TestWSInboundIdempotent(t *testing.T) { // at-least-once 吸收:同 msg 二投=账恰一份
	hub := &sinkHub{}
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	s, _ := inboundServer(t, []SinkConfig{{Name: "p", HubURL: ts.URL, Channel: "c",
		Role: "authoritative", Filter: "all", Token: "k"}}, Whitelist{})
	m := wsMsg("b", "om_3", "ou", "oc")
	_ = s.onWSInbound(context.Background(), m)
	_ = s.onWSInbound(context.Background(), m) // SDK 重推
	drainSinks(t, s, 4)
	if hub.count() != 1 {
		t.Fatalf("重复投递:%d", hub.count())
	}
	if s.metrics.InboundAccepted.Load() != 1 {
		t.Fatalf("accepted=%d", s.metrics.InboundAccepted.Load())
	}
}

func TestSinkCrashWindowRecovery(t *testing.T) { // G2:WAL→sink 间崩溃=重开后续投
	hub := &sinkHub{}
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	pool := pgtest.Pool(t)
	sinks := []SinkConfig{{Name: "p", HubURL: ts.URL, Channel: "c",
		Role: "authoritative", Filter: "all", Token: "k"}}
	s1 := New(Config{ProdURL: "http://127.0.0.1:1", SpoolPool: pool, Sinks: sinks})
	if err := s1.onWSInbound(context.Background(), wsMsg("b", "om_4", "ou", "oc")); err != nil {
		t.Fatal(err)
	}
	// 模拟进程边界：保留同一个 PG 库中的已提交行，重新构造服务对象。

	s2 := New(Config{ProdURL: "http://127.0.0.1:1", SpoolPool: pool, Sinks: sinks})
	drainSinks(t, s2, 4)
	if hub.count() != 1 {
		t.Fatalf("崩溃窗恢复失败:投递数=%d", hub.count())
	}
}

func TestSinkGlobalTerminalAndWaive(t *testing.T) { // G10+T2:一成一败=blocked;waive=with_waiver
	good, bad := &sinkHub{}, &sinkHub{status: 500}
	tsG := httptest.NewServer(good.handler())
	defer tsG.Close()
	tsB := httptest.NewServer(bad.handler())
	defer tsB.Close()
	s, _ := inboundServer(t, []SinkConfig{
		{Name: "authA", HubURL: tsG.URL, Channel: "c", Role: "authoritative", Filter: "all", Token: "k"},
		{Name: "authB", HubURL: tsB.URL, Channel: "c", Role: "authoritative", Filter: "all", Token: "k"},
	}, Whitelist{})
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_5", "ou", "oc"))
	drainSinks(t, s, 12) // authA 一次成;authB 3 次(MaxAttempts)后 dlq
	key := "b\x1fom_5"
	st, _ := s.spool.InboundGlobalStatus(context.Background(), key)
	if st != "blocked_dlq" {
		t.Fatalf("want blocked_dlq got %s", st) // 一权威成+一权威DLQ=全局不成功(C2)
	}
	if n, _ := s.spool.CountBlocked(context.Background()); n != 1 {
		t.Fatalf("blocked 计数=%d", n)
	}
	// waive(T2):blocked→delivered_with_waiver
	if err := s.spool.WaiveSink(context.Background(), key, "authB", "tok-admin", "hub退役"); err != nil {
		t.Fatal(err)
	}
	st, _ = s.spool.InboundGlobalStatus(context.Background(), key)
	if st != "delivered_with_waiver" {
		t.Fatalf("waive 后 global=%s", st)
	}
	if n, _ := s.spool.CountBlocked(context.Background()); n != 0 {
		t.Fatal("waive 后仍 blocked(healthz 应恢复)")
	}
	// 未 dlq 行不可 waive(负例)
	if err := s.spool.WaiveSink(context.Background(), key, "authA", "a", "r"); err == nil {
		t.Fatal("非 dlq 行 waive 应拒")
	}
}

func TestSinkBesteffortBoundedAndNoBlock(t *testing.T) { // G10:besteffort 恒败=dropped 不 block
	good, bad := &sinkHub{}, &sinkHub{status: 500}
	tsG := httptest.NewServer(good.handler())
	defer tsG.Close()
	tsB := httptest.NewServer(bad.handler())
	defer tsB.Close()
	s, _ := inboundServer(t, []SinkConfig{
		{Name: "prod", HubURL: tsG.URL, Channel: "c", Role: "authoritative", Filter: "all", Token: "k"},
		{Name: "test", HubURL: tsB.URL, Channel: "c", Role: "besteffort", Filter: "all", Token: "k"},
	}, Whitelist{})
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_6", "ou", "oc"))
	drainSinks(t, s, 12) // besteffort 2 次(BesteffortMax)后 dropped
	st, _ := s.spool.InboundGlobalStatus(context.Background(), "b\x1fom_6")
	if st != "delivered" { // besteffort 不参与主终态
		t.Fatalf("global=%s", st)
	}
	if s.metrics.BesteffortDropped.Load() != 1 || s.metrics.SinkDLQ.Load() != 0 {
		t.Fatalf("dropped=%d dlq=%d", s.metrics.BesteffortDropped.Load(), s.metrics.SinkDLQ.Load())
	}
}

func TestSinkPoisonNoHOL(t *testing.T) { // G10:毒件立即终态,同 sink 后续消息照走
	hub := &sinkHub{status: 422} // 第一条=毒件
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	s, _ := inboundServer(t, []SinkConfig{{Name: "p", HubURL: ts.URL, Channel: "c",
		Role: "authoritative", Filter: "all", Token: "k"}}, Whitelist{})
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_7", "ou", "oc"))
	drainSinks(t, s, 2) // 毒件→dlq(一次即终态)
	hub.mu.Lock()
	hub.status = 200
	hub.mu.Unlock()
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_8", "ou", "oc"))
	drainSinks(t, s, 2)
	st7, _ := s.spool.InboundGlobalStatus(context.Background(), "b\x1fom_7")
	st8, _ := s.spool.InboundGlobalStatus(context.Background(), "b\x1fom_8")
	if st7 != "blocked_dlq" || st8 != "delivered" {
		t.Fatalf("HOL 判定:om_7=%s om_8=%s", st7, st8) // om_8 不被 om_7 卡
	}
}

func TestSink401AuthKeepsRetrying(t *testing.T) { // K1/G12:401=配置事故,恒重试不丢
	hub := &sinkHub{status: 401}
	ts := httptest.NewServer(hub.handler())
	defer ts.Close()
	s, _ := inboundServer(t, []SinkConfig{{Name: "p", HubURL: ts.URL, Channel: "c",
		Role: "authoritative", Filter: "all", Token: "wrong"}}, Whitelist{})
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_9", "ou", "oc"))
	drainSinks(t, s, 5)
	st, _ := s.spool.InboundGlobalStatus(context.Background(), "b\x1fom_9")
	if st != "pending" { // 不 dlq 不 delivered=还在重试
		t.Fatalf("401 应恒重试,global=%s", st)
	}
	if s.metrics.SinkDLQ.Load() != 0 {
		t.Fatal("401 不应入 DLQ")
	}
}

func TestWSWhitelistFilter(t *testing.T) { // §A:whitelist sink 仅命中者建行
	prod, test := &sinkHub{}, &sinkHub{}
	tsP := httptest.NewServer(prod.handler())
	defer tsP.Close()
	tsT := httptest.NewServer(test.handler())
	defer tsT.Close()
	s, _ := inboundServer(t, []SinkConfig{
		{Name: "prod", HubURL: tsP.URL, Channel: "c", Role: "authoritative", Filter: "all", Token: "k"},
		{Name: "test", HubURL: tsT.URL, Channel: "c", Role: "besteffort", Filter: "whitelist", Token: "k"},
	}, Whitelist{OpenIDs: []string{"ou_vip"}})
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_a", "ou_vip", "oc"))
	_ = s.onWSInbound(context.Background(), wsMsg("b", "om_b", "ou_nobody", "oc"))
	drainSinks(t, s, 8)
	if prod.count() != 2 || test.count() != 1 {
		t.Fatalf("prod=%d(want 2) test=%d(want 1)", prod.count(), test.count())
	}
}
