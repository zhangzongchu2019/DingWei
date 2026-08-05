package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// TestReplyOriginHubTermination 走真 Hub + 两个真 WS 会话,验证 R4 hop 兜底 / R5 dedup
// 在 RouteEnvelope 层生效:被丢弃的消息不投递,靠有序投递用"后一条先达"证明前一条被丢。
// 同时验证被投递消息带 origin 捕获(origin_addr)+ hop_count 递增。
func TestReplyOriginHubTermination(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", keyID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	developer := dialSession(t, ctx, srv.URL, "developer", keyID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")

	send := func(id, body string, meta map[string]any) {
		env := model.Envelope{ID: id, To: "home#" + keyID, From: "developer#" + keyID, Body: body, Meta: meta}
		if err := writeEnvelope(ctx, developer, env); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}

	// --- T2 / R4:hop_count>=max_hops 的消息被静默丢弃 ---
	send("hop-drop", "SHOULD_DROP_HOP", map[string]any{"hop_count": defaultMaxHops})
	send("after-hop", "AFTER_HOP", nil)
	got := readEnvelope(t, ctx, home)
	if got.Body != "AFTER_HOP" {
		t.Fatalf("R4 hop 兜底失败:期望先收到 AFTER_HOP(hop-drop 应被丢),实收 body=%q id=%s", got.Body, got.ID)
	}
	// 被投递的正常消息应带 origin 捕获 + hop_count=1
	if originAddrOf(got) != "developer#"+keyID {
		t.Fatalf("origin_addr 未捕获:%q", originAddrOf(got))
	}
	if hopCountOf(got) != 1 {
		t.Fatalf("hop_count 期望 1,实 %d", hopCountOf(got))
	}

	// --- T4 / R5:相同 dedup_key 的第二条被丢弃 ---
	send("dk1", "DEDUP_FIRST", map[string]any{"dedup_key": "dk"})
	send("dk2", "DEDUP_SECOND_DROP", map[string]any{"dedup_key": "dk"})
	send("after-dk", "AFTER_DEDUP", nil)
	got = readEnvelope(t, ctx, home)
	if got.Body != "DEDUP_FIRST" {
		t.Fatalf("R5 dedup:期望先收 DEDUP_FIRST,实 %q", got.Body)
	}
	got = readEnvelope(t, ctx, home)
	if got.Body != "AFTER_DEDUP" {
		t.Fatalf("R5 dedup:期望收 AFTER_DEDUP(第二条同 dedup_key 应被丢),实 %q id=%s", got.Body, got.ID)
	}
}
