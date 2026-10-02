package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 组长取甲：helper 回「failed、可重试」时 Hub 按指数退避重投，连续到限转 dead，并给发送方一条可见的投递失败通知
// （原因码 retry_exhausted＋最后一次失败原因）。此格把上限设为 3、退避设为毫秒级，只验状态机与通知。
func TestRetryableFailedDeliveryCapsAndNotifiesSender(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.deliveryFailedBackoffBase, hub.deliveryFailedBackoffCap, hub.deliveryFailedMax = 20*time.Millisecond, 40*time.Millisecond, 3
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSessionWithHeaderRaw(t, ctx, srv.URL, "home", key.ID, secret, nil)
	defer home.Close(websocket.StatusNormalClosure, "done")
	src := dialSessionWithHeaderRaw(t, ctx, srv.URL, "src", key.ID, secret, nil)
	defer src.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "caps", To: "workpulse#" + key.ID,
		Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "会话 home 在线且支持 delivery_ack", func() bool {
		hub.mu.Lock()
		c := hub.sessionClients[key.ID]["home"]
		hub.mu.Unlock()
		return c != nil && hub.sessionSupportsDeliveryACK(c)
	})
	if err := hub.RouteEnvelope(ctx, model.Envelope{ID: "fc-1", To: "home#" + key.ID, From: "src#" + key.ID,
		Body: "task", TS: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	for i := 1; i <= 3; i++ {
		var got model.Envelope
		for {
			got = readEnvelope(t, ctx, home)
			if metaString(got.Meta, "type") == "delivery" {
				break
			}
		}
		if deliveryID == "" {
			deliveryID = metaString(got.Meta, "delivery_id")
		} else if metaString(got.Meta, "delivery_id") != deliveryID {
			t.Fatalf("redelivery %d carries another delivery id: %+v", i, got.Meta)
		}
		if err := writeEnvelope(ctx, home, model.Envelope{ID: "ack-fail", To: "workpulse#" + key.ID,
			Meta: map[string]any{"type": "delivery_ack", "delivery_id": deliveryID, "state": "failed", "reason": "cli_unavailable", "retryable": true}}); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			waitFor(t, time.Second, "可重试 failed 后回到 queued", func() bool {
				d, _ := db.GetSessionDelivery(ctx, deliveryID)
				return d != nil && (d.State == "queued" || d.State == "sent")
			})
		}
	}
	waitFor(t, 2*time.Second, "连续 3 次可重试 failed 后转 dead、last_error=cli_unavailable", func() bool {
		d, _ := db.GetSessionDelivery(ctx, deliveryID)
		return d != nil && d.State == "dead" && d.LastError == "cli_unavailable"
	})
	for {
		env := readEnvelope(t, ctx, src)
		if metaString(env.Meta, "type") != "delivery_status" {
			continue
		}
		if metaString(env.Meta, "reason_code") != "retry_exhausted" || metaString(env.Meta, "last_error") != "cli_unavailable" ||
			metaString(env.Meta, "delivery_id") != deliveryID {
			t.Fatalf("sender notice meta=%+v body=%q", env.Meta, env.Body)
		}
		break
	}
	// CLI 恢复后，新信照常投递（到限只作用于那一条）
	if err := hub.RouteEnvelope(ctx, model.Envelope{ID: "fc-2", To: "home#" + key.ID, From: "src#" + key.ID,
		Body: "task after recovery", TS: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	var fresh model.Envelope
	for {
		fresh = readEnvelope(t, ctx, home)
		if metaString(fresh.Meta, "type") == "delivery" && metaString(fresh.Meta, "delivery_id") != deliveryID {
			break
		}
	}
	newID := metaString(fresh.Meta, "delivery_id")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "ack-ok", To: "workpulse#" + key.ID,
		Meta: map[string]any{"type": "delivery_ack", "delivery_id": newID, "state": "processed"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "恢复后的新投递 processed", func() bool {
		d, _ := db.GetSessionDelivery(ctx, newID)
		return d != nil && d.State == "processed"
	})
}

// 默认值钉住（零值 Hub 即生产值）：2 秒起、封顶 300 秒、连续 30 次转 dead。期望值写字面量。
func TestDeliveryFailedRetryDefaults(t *testing.T) {
	var h Hub
	base, maxDelay, maxFailed := h.deliveryFailedRetryParams()
	if base != 2*time.Second || maxDelay != 300*time.Second || maxFailed != 30 {
		t.Fatalf("defaults = %s/%s/%d, want 2s/300s/30", base, maxDelay, maxFailed)
	}
}
