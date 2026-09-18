package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// D1 滑动续期的三道守卫,逐条阴性对照。
// 🔴 这三条都是「少了也不会有任何测试变红」的那类,所以必须显式测。
func TestTouchConsoleSessionGuards(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	open := func(t *testing.T) *SQLite {
		t.Helper()
		db, err := OpenSQLite(filepath.Join(t.TempDir(), "touch.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "p", Provider: "feishu", ProviderIssuer: "i", ProviderSubject: "s", Active: true}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	stored := func(t *testing.T, db *SQLite) string {
		t.Helper()
		var v string
		if err := db.db.QueryRow(`SELECT expires_at FROM console_session WHERE id='sid'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	create := func(t *testing.T, db *SQLite, expires time.Time) {
		t.Helper()
		if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "sid", PrincipalID: "p", TokenHash: "h", ExpiresAt: expires}, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("正例:向后延", func(t *testing.T) {
		db := open(t)
		create(t, db, now.Add(time.Hour))
		want := now.Add(12 * time.Hour)
		if err := db.TouchConsoleSession(ctx, "sid", want, now); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, db); got != want.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("应延到 %v, 实际 %s", want, got)
		}
	})

	t.Run("阴性:已过期的不得被续活", func(t *testing.T) {
		db := open(t)
		expired := now.Add(-time.Minute)
		create(t, db, expired)
		if err := db.TouchConsoleSession(ctx, "sid", now.Add(12*time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, db); got != expired.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("🔴 过期会话被续活了: %s", got)
		}
	})

	t.Run("阴性:已吊销的不得被续活", func(t *testing.T) {
		db := open(t)
		expires := now.Add(time.Hour)
		create(t, db, expires)
		if err := db.RevokeConsoleSession(ctx, "sid", now); err != nil {
			t.Fatal(err)
		}
		if err := db.TouchConsoleSession(ctx, "sid", now.Add(12*time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, db); got != expires.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("🔴 已吊销会话被续活了: %s", got)
		}
	})

	t.Run("阴性:只延不缩(并发下晚到的旧值不得把窗口拉回来)", func(t *testing.T) {
		db := open(t)
		far := now.Add(10 * time.Hour)
		create(t, db, far)
		if err := db.TouchConsoleSession(ctx, "sid", now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, db); got != far.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("🔴 过期时刻被缩短了: %s", got)
		}
	})

	t.Run("阴性:新值不晚于 now 直接拒", func(t *testing.T) {
		db := open(t)
		create(t, db, now.Add(time.Hour))
		if err := db.TouchConsoleSession(ctx, "sid", now, now); err == nil {
			t.Fatal("新过期时刻不晚于 now 应报错")
		}
	})

	t.Run("ResolveConsoleSession 必须带回 CreatedAt(绝对上限起算点)", func(t *testing.T) {
		db := open(t)
		create(t, db, now.Add(time.Hour))
		got, err := db.ResolveConsoleSession(ctx, "h", now)
		if err != nil || got == nil {
			t.Fatalf("resolve 失败 %v %v", got, err)
		}
		if !got.CreatedAt.Equal(now.Add(-time.Hour)) {
			t.Fatalf("CreatedAt 应为建会话时刻, 实际 %v", got.CreatedAt)
		}
	})
}
