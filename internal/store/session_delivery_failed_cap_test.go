package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 组长取甲：可重试 failed 改指数退避（2 秒起翻倍、封顶 300 秒），连续 30 次转 dead；injected／processed 清零。
func newFailedCapDB(t *testing.T) (*SQLite, context.Context, string) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "failed-cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "dw-dlv_fc", KeyID: "k", SessionName: "seat",
		Envelope: `{"id":"m1"}`, SourceMessageID: "m1", SourceType: "session", State: "queued", NextAttemptAt: now}); err != nil {
		t.Fatal(err)
	}
	return db, ctx, "dw-dlv_fc"
}

func TestFailSessionDeliveryRetryableBackoffSequenceAndCap(t *testing.T) {
	db, ctx, id := newFailedCapDB(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	want := []time.Duration{2, 4, 8, 16, 32, 64, 128, 256, 300, 300}
	for i, w := range want {
		dead, failed, next, err := db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
		if err != nil || dead || failed != i+1 {
			t.Fatalf("failure %d: dead=%v failed=%d err=%v", i+1, dead, failed, err)
		}
		if got := next.Sub(now); got != w*time.Second {
			t.Fatalf("failure %d: delay=%s want %s", i+1, got, w*time.Second)
		}
		d, _ := db.GetSessionDelivery(ctx, id)
		if d.State != "queued" || d.LastError != "cli_unavailable" || !d.NextAttemptAt.Equal(next) {
			t.Fatalf("failure %d: record %+v", i+1, d)
		}
	}
}

func TestFailSessionDeliveryRetryableTurnsDeadAtMax(t *testing.T) {
	db, ctx, id := newFailedCapDB(t)
	now := time.Now().UTC()
	for i := 1; i <= 30; i++ {
		dead, failed, _, err := db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
		if err != nil || failed != i || dead != (i == 30) {
			t.Fatalf("failure %d: dead=%v failed=%d err=%v", i, dead, failed, err)
		}
	}
	d, _ := db.GetSessionDelivery(ctx, id)
	if d.State != "dead" || d.LastError != "cli_unavailable" {
		t.Fatalf("after 30 failures: %+v", d)
	}
	// 终态之后不再动
	if dead, failed, _, err := db.FailSessionDeliveryRetryable(ctx, id, "x", now, 2*time.Second, 300*time.Second, 30); err != nil || dead || failed != 0 {
		t.Fatalf("terminal delivery must be left alone: dead=%v failed=%d err=%v", dead, failed, err)
	}
}

func TestFailSessionDeliveryRetryableCountResetsOnSuccess(t *testing.T) {
	db, ctx, id := newFailedCapDB(t)
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_, _, _, _ = db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
	}
	if err := db.UpdateSessionDelivery(ctx, id, "injected", "", now); err != nil {
		t.Fatal(err)
	}
	_, failed, next, err := db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
	if err != nil || failed != 1 || next.Sub(now) != 2*time.Second {
		t.Fatalf("after a success the count restarts: failed=%d delay=%s err=%v", failed, next.Sub(now), err)
	}
}

// 复验 tosonnet55tc2k：连续次数不因会话断开重连（租约释放）而清零——只有 injected／processed 清零。
func TestFailSessionDeliveryRetryableCountSurvivesReconnect(t *testing.T) {
	db, ctx, id := newFailedCapDB(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_, _, _, _ = db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
	}
	claimed, err := db.ClaimSessionDelivery(ctx, "k", "seat", now.Add(time.Hour)) // 重投发出（sent）
	if err != nil || claimed == nil || claimed.ID != id {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := db.ReleaseSessionDeliveryLeases(ctx, "k", "seat", "session_disconnected", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, failed, next, err := db.FailSessionDeliveryRetryable(ctx, id, "cli_unavailable", now, 2*time.Second, 300*time.Second, 30)
	if err != nil || failed != 6 || next.Sub(now) != 64*time.Second {
		t.Fatalf("after reconnect the count must continue: failed=%d delay=%s err=%v", failed, next.Sub(now), err)
	}
}
