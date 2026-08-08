package feishu

// T17 §C1/§K2:request_id 派生确定性+UUIDv5 形;ProxyGateway v:1 托管+resolve 透传(G8)。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var reV5 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestDeriveRequestIDDeterministicV5(t *testing.T) {
	a := DeriveRequestID("reply", "msg-123", "0")
	b := DeriveRequestID("reply", "msg-123", "0")
	if a != b {
		t.Fatalf("非确定性:%s vs %s", a, b) // 重试/重启恒同=托管幂等根基
	}
	if !reV5.MatchString(a) {
		t.Fatalf("非 UUIDv5 形(§K2 正则):%s", a)
	}
	if DeriveRequestID("reply", "msg-124", "0") == a || DeriveRequestID("reply", "msg-123", "1") == a ||
		DeriveRequestID("schedule", "msg-123", "0") == a {
		t.Fatal("不同输入撞值")
	}
	// 长度前缀防拼接歧义:("ab","c") ≠ ("a","bc")
	if DeriveRequestID("reply", "ab", "c") == DeriveRequestID("reply", "a", "bc") {
		t.Fatal("拼接歧义(须长度前缀)")
	}
}

func TestProxyGatewaySendCustodyAndLegacy(t *testing.T) {
	var lastBody map[string]any
	var lastAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		lastBody = nil
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		if _, ok := lastBody["v"]; ok {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"outbound_id": "ob_x1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"msg_id": "om_legacy"})
	}))
	defer ts.Close()
	g := NewProxyGateway(ts.URL+"/outbound", "tok")
	// v:1 托管:RequestID 在=custody 契约,返回 outbound_id 作凭证
	rid := DeriveRequestID("reply", "m1", "0")
	id, err := g.Send(context.Background(), OutMessage{
		BotChannelID: "b", ToID: "oc_1", ToType: "group", Text: "hi", RequestID: rid})
	if err != nil || id != "ob_x1" {
		t.Fatalf("custody: id=%q err=%v", id, err)
	}
	if lastBody["v"] != float64(1) || lastBody["request_id"] != rid || lastAuth != "Bearer tok" {
		t.Fatalf("custody 请求形:%v auth=%s", lastBody, lastAuth)
	}
	// legacy:无 RequestID=旧协议零回归
	id, err = g.Send(context.Background(), OutMessage{BotChannelID: "b", ToID: "oc_1", Text: "hi"})
	if err != nil || id != "om_legacy" {
		t.Fatalf("legacy: id=%q err=%v", id, err)
	}
	if _, ok := lastBody["v"]; ok {
		t.Fatal("legacy 不应带 v")
	}
}

func TestProxyGatewayResolvePassthrough(t *testing.T) { // G8
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resolve" {
			w.WriteHeader(500)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["name"] == "张三" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "ou_zhang"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	g := NewProxyGateway(ts.URL+"/outbound", "tok")
	id, err := g.ResolveNameToID(context.Background(), "b", "张三", "personal")
	if err != nil || id != "ou_zhang" {
		t.Fatalf("resolve: %q %v", id, err)
	}
	if _, err := g.ResolveNameToID(context.Background(), "b", "无人", "personal"); err == nil {
		t.Fatal("404 应报错")
	}
}
