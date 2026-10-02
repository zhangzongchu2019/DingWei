package fanout

// 段1 测试:托管出站(设计 v2.3 §C/§K2/§K3)。盖 G3(202/幂等/409/状态机/status)、
// G7(多 bot 选路)、G10 出站侧(DLQ)、G13(重启后同 request_id 同 outbound_id 恰一发)。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

const testUUID = "9b2f8a4e-1c3d-5e6f-8a9b-0c1d2e3f4a5b" // 合法 v5 形(第13位=5,第17位=8)

type failSender struct {
	mu    sync.Mutex
	fails int // 前 N 次失败
	calls int
}

func (f *failSender) Send(ctx context.Context, out feishu.OutMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.fails {
		return "", errors.New("lark down")
	}
	return fmt.Sprintf("om_real_%d", f.calls), nil
}

func custodyServer(t *testing.T, sender Sender, bots map[string]Sender, maxAttempts int) (*Server, string) {
	t.Helper()
	pool := spoolTestPool(t)
	s := New(Config{
		ProdURL: "http://127.0.0.1:1", TestURL: "",
		Sender: sender, Bots: bots, SpoolPool: pool, MaxAttempts: maxAttempts,
		Callers: map[string]CallerConfig{
			"tok-prod": {Name: "prod-hub", TestSuffix: false},
			"tok-test": {Name: "test-hub", TestSuffix: true},
		},
	})
	if s.spool == nil {
		t.Fatal("spool 未打开")
	}
	return s, "pgtest"
}

func postCustody(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/outbound", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func custodyBody(requestID, bot, text string) string {
	b, _ := json.Marshal(map[string]any{
		"v": 1, "request_id": requestID, "bot_channel": bot,
		"to_id": "oc_1", "to_type": "group", "text": text,
	})
	return string(b)
}

// drainOnce 手动驱动 worker 一轮(测试不开 200ms ticker,直接取件投递)。
func drainOnce(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	for {
		job, err := s.spool.NextDue(ctx, time.Now().Add(time.Hour)) // 越过退避窗取件
		if err != nil {
			t.Fatalf("NextDue: %v", err)
		}
		if job == nil {
			return
		}
		s.deliverCustody(ctx, job)
	}
}

func TestCustodyAcceptAndSend(t *testing.T) {
	rec := &sendRec{}
	s, _ := custodyServer(t, rec, nil, 8)
	w := postCustody(t, s, "tok-prod", custodyBody(testUUID, "unifiedrobot", "hello"))
	if w.Code != 202 {
		t.Fatalf("want 202 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id := resp["outbound_id"]
	if id == "" {
		t.Fatal("无 outbound_id")
	}
	drainOnce(t, s)
	// K3 status=sent+lark_msg_id
	req := httptest.NewRequest("GET", "/outbound/"+id, nil)
	req.Header.Set("Authorization", "Bearer tok-prod")
	sw := httptest.NewRecorder()
	s.ServeHTTP(sw, req)
	var st map[string]any
	_ = json.Unmarshal(sw.Body.Bytes(), &st)
	if st["status"] != "sent" || st["lark_msg_id"] != "msg-1" {
		t.Fatalf("status=%v", st)
	}
	if len(rec.outs) != 1 || rec.outs[0].Text != "hello" { // prod caller 无后缀
		t.Fatalf("outs=%v", rec.outs)
	}
}

func TestCustodyIdempotentReplayAnd409(t *testing.T) {
	rec := &sendRec{}
	s, _ := custodyServer(t, rec, nil, 8)
	body := custodyBody(testUUID, "b", "x")
	w1 := postCustody(t, s, "tok-prod", body)
	w2 := postCustody(t, s, "tok-prod", body) // G3:响应丢失重试=同请求重放
	if w1.Code != 202 || w2.Code != 200 {
		t.Fatalf("codes %d/%d", w1.Code, w2.Code)
	}
	var r1, r2 map[string]string
	_ = json.Unmarshal(w1.Body.Bytes(), &r1)
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)
	if r1["outbound_id"] != r2["outbound_id"] {
		t.Fatalf("重放 outbound_id 不同:%v vs %v", r1, r2)
	}
	drainOnce(t, s)
	if len(rec.outs) != 1 {
		t.Fatalf("重放导致双发:%d", len(rec.outs)) // 飞书恰一发
	}
	// 同 request_id 异 body=409
	w3 := postCustody(t, s, "tok-prod", custodyBody(testUUID, "b", "DIFFERENT"))
	if w3.Code != 409 {
		t.Fatalf("want 409 got %d", w3.Code)
	}
}

func TestCustodyValidation(t *testing.T) {
	s, _ := custodyServer(t, &sendRec{}, nil, 8)
	cases := []struct {
		name, token, body string
		want              int
	}{
		{"401错token", "tok-bad", custodyBody(testUUID, "b", "x"), 401},
		{"400非UUIDv5", "tok-prod", custodyBody("not-a-uuid", "b", "x"), 400},
		{"400 v2", "tok-prod", `{"v":2,"request_id":"` + testUUID + `","bot_channel":"b","text":"x"}`, 400},
		{"400未知字段", "tok-prod", `{"v":1,"request_id":"` + testUUID + `","bot_channel":"b","text":"x","hax":1}`, 400},
		{"422空件", "tok-prod", `{"v":1,"request_id":"` + testUUID + `","bot_channel":"b"}`, 422},
	}
	for _, c := range cases {
		if w := postCustody(t, s, c.token, c.body); w.Code != c.want {
			t.Errorf("%s: want %d got %d (%s)", c.name, c.want, w.Code, w.Body.String())
		}
	}
}

func TestCustodyMultiBotRouting(t *testing.T) { // G7
	a, b := &sendRec{}, &sendRec{}
	s, _ := custodyServer(t, nil, map[string]Sender{"botA": a, "botB": b}, 8)
	postCustody(t, s, "tok-prod", custodyBody(testUUID, "botB", "to-b"))
	if w := postCustody(t, s, "tok-prod",
		custodyBody("9b2f8a4e-1c3d-5e6f-8a9b-0c1d2e3f4a5c", "botX", "x")); w.Code != 404 {
		t.Fatalf("未知bot want 404 got %d", w.Code)
	}
	drainOnce(t, s)
	if len(a.outs) != 0 || len(b.outs) != 1 || b.outs[0].BotChannelID != "botB" {
		t.Fatalf("选路错:a=%d b=%d", len(a.outs), len(b.outs))
	}
}

func TestCustodyCallerSuffix(t *testing.T) { // §C 指定来源:test caller 加后缀,幂等
	rec := &sendRec{}
	s, _ := custodyServer(t, rec, nil, 8)
	postCustody(t, s, "tok-test", custodyBody(testUUID, "b", "hi"))
	drainOnce(t, s)
	if len(rec.outs) != 1 || rec.outs[0].Text != "hi"+TestEnvSuffix {
		t.Fatalf("后缀缺失:%v", rec.outs)
	}
}

func TestCustodyDLQ(t *testing.T) { // G10 出站侧:终败入 DLQ+计数
	fs := &failSender{fails: 99}
	s, _ := custodyServer(t, fs, nil, 2)
	w := postCustody(t, s, "tok-prod", custodyBody(testUUID, "b", "x"))
	var r map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	drainOnce(t, s) // attempt1→retry
	drainOnce(t, s) // attempt2→DLQ(max=2)
	req := httptest.NewRequest("GET", "/outbound/"+r["outbound_id"], nil)
	req.Header.Set("Authorization", "Bearer tok-prod")
	sw := httptest.NewRecorder()
	s.ServeHTTP(sw, req)
	var st map[string]any
	_ = json.Unmarshal(sw.Body.Bytes(), &st)
	if st["status"] != "dlq" || st["error"] == nil {
		t.Fatalf("want dlq got %v", st)
	}
	if s.metrics.OutboundDLQ.Load() != 1 {
		t.Fatal("DLQ 计数缺失")
	}
}

func TestCustodyRestartRecovery(t *testing.T) { // G13:代理已WAL+响应丢→重启→同id恰一发
	pool := spoolTestPool(t)
	rec1 := &sendRec{}
	s1 := New(Config{ProdURL: "http://127.0.0.1:1", Sender: rec1, SpoolPool: pool,
		Callers: map[string]CallerConfig{"tok-prod": {Name: "prod-hub"}}})
	w := postCustody(t, s1, "tok-prod", custodyBody(testUUID, "b", "x"))
	if w.Code != 202 {
		t.Fatalf("want 202 got %d", w.Code)
	}
	var r1 map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &r1)
	// 模拟崩溃:未投递即关闭(响应已 202=已落盘,Hub 视角可能没收到→会重试)

	rec2 := &sendRec{}
	s2 := New(Config{ProdURL: "http://127.0.0.1:1", Sender: rec2, SpoolPool: pool,
		Callers: map[string]CallerConfig{"tok-prod": {Name: "prod-hub"}}})
	if _, err := s2.spool.RecoverSending(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Hub 重启后按 §C1 确定性派生=同 request_id 重试
	w2 := postCustody(t, s2, "tok-prod", custodyBody(testUUID, "b", "x"))
	var r2 map[string]string
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)
	if w2.Code != 200 || r2["outbound_id"] != r1["outbound_id"] {
		t.Fatalf("重启后幂等破:code=%d %v vs %v", w2.Code, r2, r1)
	}
	drainOnce(t, s2)
	if len(rec2.outs) != 1 {
		t.Fatalf("飞书应恰一发,got %d", len(rec2.outs))
	}
}

func TestLegacyOutboundStillWorks(t *testing.T) { // 无 v=v1 立发路零回归
	rec := &sendRec{}
	s := New(Config{ProdURL: "http://127.0.0.1:1", Sender: rec, OutboundToken: "old-tok"})
	body, _ := json.Marshal(feishu.OutMessage{BotChannelID: "b", ToID: "oc_1", Text: "hi"})
	req := httptest.NewRequest("POST", "/outbound", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer old-tok")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("legacy want 200 got %d: %s", w.Code, w.Body.String())
	}
	if len(rec.outs) != 1 || rec.outs[0].Text != "hi"+TestEnvSuffix { // legacy=无条件后缀(v1 语义)
		t.Fatalf("legacy 行为变:%v", rec.outs)
	}
}
