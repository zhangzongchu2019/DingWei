package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func openMigrated(t *testing.T, path string) *SQLite {
	t.Helper()
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

// SA-13 ① 逐启动重放 ⇒ 管理员改动被静默回滚。
//
// 🔴 这条是本修复的骨：修之前它实测失败（0025 的无条件 UPDATE 每次重启把
// l1_command_roster 的 pattern 盖回去，并顺带盖一个新 updated_at）。
func TestMigrationLedger_RestartDoesNotRevertAdminEdit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "revert.db")
	s := openMigrated(t, path)

	const edited = "#管理员改过的"
	if _, err := s.db.ExecContext(ctx,
		`UPDATE l1_decision_rule SET pattern=? WHERE id='l1_command_roster'`, edited); err != nil {
		t.Fatalf("模拟管理员改规则: %v", err)
	}

	// 重启 = 再跑一次 Migrate
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("第二次 Migrate: %v", err)
	}

	var got string
	if err := s.db.QueryRowContext(ctx,
		`SELECT pattern FROM l1_decision_rule WHERE id='l1_command_roster'`).Scan(&got); err != nil {
		t.Fatalf("回读: %v", err)
	}
	if got != edited {
		t.Fatalf("重启把管理员改动静默回滚了：期望 %q，实得 %q", edited, got)
	}
}

// 迁移只跑一次：账里每个 version 恰好一行，且行数 == 迁移文件数。
func TestMigrationLedger_AppliedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "once.db"))
	for i := 0; i < 3; i++ {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("第 %d 次 Migrate: %v", i+2, err)
		}
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(entries) {
		t.Fatalf("账内 %d 行，迁移文件 %d 个 —— 不是一一对应", n, len(entries))
	}
	var dup int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (SELECT version FROM schema_migrations GROUP BY version HAVING COUNT(*)>1)`).Scan(&dup); err != nil {
		t.Fatal(err)
	}
	if dup != 0 {
		t.Fatalf("账内有 %d 个 version 重复记账", dup)
	}
}

// SA-13 ②：已应用的迁移内容被改过 ⇒ 拒绝启动，不得拿新账认旧事实。
func TestMigrationLedger_DigestMismatchRefusesStart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "digest.db")
	s := openMigrated(t, path)

	if _, err := s.db.ExecContext(ctx,
		`UPDATE schema_migrations SET digest='deadbeef' WHERE version='0001_init.sql'`); err != nil {
		t.Fatal(err)
	}
	err := s.Migrate(ctx)
	if err == nil {
		t.Fatal("已应用迁移的 digest 变了，Migrate 仍然放行 —— 拒绝启动这条没生效")
	}
	if !strings.Contains(err.Error(), "拒绝启动") {
		t.Fatalf("错误信息没说清是拒绝启动：%v", err)
	}
}

// SA-13 ②：不再通配吞 duplicate column —— 列已存在时必须让启动失败，
// 而不是被当成「迁移已应用」然后进程照常起来。
//
// ⚠️ 本反测的第一版【通过了，但不是因为它测的那件事】：当时用一张手搓的最小
// session_endpoint 建库，结果在 0011 就因 "no such column: active" 挂了，
// 断言只写了「err != nil」于是照样绿。⇒ 反测必须断言【失败在预期的那一处】。
func TestMigrationLedger_DuplicateColumnFailsInsteadOfBeingSwallowed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dup.db")
	s := openMigrated(t, path)

	// 0032 就一句：ALTER TABLE session_endpoint ADD COLUMN delivery_ack_v1。
	// 把它的账删掉但把列留着 ⇒ 下次启动会重跑它，撞上 duplicate column。
	// 这正是旧实现会吞掉、然后照常启动的那一类。
	const target = "0032_session_endpoint_delivery_ack.sql"
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM schema_migrations WHERE version=?`, target); err != nil {
		t.Fatal(err)
	}

	err := s.Migrate(ctx)
	if err == nil {
		t.Fatal("duplicate column 被吞了 —— Migrate 成功返回，进程会带着未落成的 schema 起来")
	}
	if !strings.Contains(err.Error(), target) {
		t.Fatalf("失败的不是预期那一处迁移（本反测就白做了）：%v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		t.Fatalf("失败原因不是 duplicate column（本反测就白做了）：%v", err)
	}
	t.Logf("如期失败在 %s：%v", target, err)
}

// legacy 库补账：账为空但库里已有业务表 ⇒ 核验后只写账、不重放数据语句。
func TestMigrationLedger_LegacyBackfillDoesNotReplayData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// 先正常迁到最新，再把账整个删掉，伪造成「一个在跑但没有账的旧库」。
	s := openMigrated(t, path)
	const edited = "#legacy 库里被改过的"
	if _, err := s.db.ExecContext(ctx,
		`UPDATE l1_decision_rule SET pattern=? WHERE id='l1_command_roster'`, edited); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("legacy 补账失败: %v", err)
	}

	var got string
	if err := s.db.QueryRowContext(ctx,
		`SELECT pattern FROM l1_decision_rule WHERE id='l1_command_roster'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != edited {
		t.Fatalf("legacy 补账重放了数据语句：期望保持 %q，实得 %q", edited, got)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("legacy 补账没写账")
	}
}
