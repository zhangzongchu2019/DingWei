package main

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// 本文件钉的是【装配点】那一层的判据：Migrate 出错 ⇒ Hub 入口与 bootstrap-admin 都必须拒绝启动，
// 且不在半套表上写任何东西。
//
// 🔴 出处：复验件 runbooks/REVIEW-G2-MIGRATE-LEDGER-tosonnet55tc2k-20261001.md 附录 A.2
// （复验方的变异 C2「装配点忽略 Migrate 的错」在本席原 15 格上【全绿】⇒ 那是真缺口：
// 这一层原本没有任何用例，只有作者自述）。组长裁定 ruling-2026-10-01T170223Z 第②件要求收进来。
// 与附录原格的差别只有两处，都是为了不给「恒拒也绿」留口子：
//   ① 补一格【正向对照】（空库经 Hub 入口自己迁出表、不建号、重启空转账不增）——
//      只测拒绝的格子，会因为「入口本来就起不来」而假绿；
//   ② 拒绝那两格断言错误里点出【是哪一条条目】，不接受任何错误都算过。

// ledgerProbeExec / ledgerProbeCount 是用例自己的探针（走 repo.Pool，不引第二套建库路径）。
func ledgerProbeExec(t *testing.T, dsn, q string, args ...any) {
	t.Helper()
	tc, err := repo.NewAccessCtx("test", "migrate-refuse")
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.OpenPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.Exec(context.Background(), tc, q, args...); err != nil {
		t.Fatalf("探针执行失败 %q: %v", q, err)
	}
}

func ledgerProbeCount(t *testing.T, dsn, q string, args ...any) int {
	t.Helper()
	tc, err := repo.NewAccessCtx("test", "migrate-refuse")
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.OpenPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	row, err := p.QueryRow(context.Background(), tc, q, args...)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 正向对照：空库经 Hub 入口自己迁出表，不建任何账号；重启再开一次是空转（账不增）。
func TestEmptyDBHubEntryMigratesAndCreatesNoAccount(t *testing.T) {
	dsn := pgSchemaDSN(t, false)
	st, _, closeFn, err := openNewPlatformStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("空库上 openNewPlatformStore: %v", err)
	}
	if st == nil {
		t.Fatal("存储为 nil 而错误也为 nil —— 装配点交回了一个不能用的东西")
	}
	closeFn()
	if n := ledgerProbeCount(t, dsn, `SELECT count(*) FROM np_user`); n != 0 {
		t.Errorf("np_user 有 %d 行：Hub 入口不该建号（建号只归 bootstrap-admin）", n)
	}
	if n := ledgerProbeCount(t, dsn, `SELECT count(*) FROM schema_migrations`); n != 13 {
		t.Fatalf("账上 %d 条，要 13 条（含 0108 外部短名）；此前12条（含 0104 np_session.created_at、0105 extseat、0107 mcpgw_oauth）", n)
	}
	// 重启：仍成功，且账不增（digest 记账只跑一次那条语义在装配点上也要成立）。
	_, _, c2, err := openNewPlatformStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("重启 openNewPlatformStore: %v", err)
	}
	c2()
	if n := ledgerProbeCount(t, dsn, `SELECT count(*) FROM schema_migrations`); n != 13 {
		t.Errorf("重启后账上 %d 条，要仍是 11 条", n)
	}
}

// C2 主格：账内 digest 被篡改 ⇒ Hub 入口拒绝、bootstrap-admin 拒绝且 stdout 为空、不建号。
func TestMigrateFailureRefusesHubEntryAndBootstrap(t *testing.T) {
	dsn := pgSchemaDSN(t, false)
	// 先正常迁一遍（这一段同时是本格的正向对照：同一个入口在账没被改时必须能起）。
	_, _, closeFn, err := openNewPlatformStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("首次 openNewPlatformStore: %v", err)
	}
	closeFn()

	ledgerProbeExec(t, dsn, `UPDATE schema_migrations SET digest = 'deadbeef' WHERE version = '0100_identity_baseline.sql'`)

	_, _, _, err = openNewPlatformStore(context.Background(), dsn)
	if err == nil {
		t.Fatal("账被篡改后 Hub 入口竟然起来了 —— 那正是「启动成功冒充 schema 如预期」")
	}
	if !strings.Contains(err.Error(), "0100_identity_baseline.sql") {
		t.Errorf("拒绝的理由没点出是哪一条条目: %v", err)
	}
	out, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs")
	if err == nil {
		t.Fatalf("bootstrap-admin 竟然成功，stdout=%q", out)
	}
	if out != "" {
		t.Errorf("拒绝时 stdout 必须为空（打了东西就意味着调用方以为成了）: %q", out)
	}
	if n := ledgerProbeCount(t, dsn, `SELECT count(*) FROM np_user`); n != 0 {
		t.Errorf("被拒之后 np_user 有 %d 行 —— 在半套账上写了东西", n)
	}
}

// 账表形状被破坏（缺 digest／applied_at 列）⇒ 拒绝启动，不是静默继续、也不是把表盖掉。
func TestBrokenLedgerTableRefuses(t *testing.T) {
	dsn := pgSchemaDSN(t, false)
	ledgerProbeExec(t, dsn, `CREATE TABLE schema_migrations(version text PRIMARY KEY)`)

	if _, _, _, err := openNewPlatformStore(context.Background(), dsn); err == nil {
		t.Fatal("账表缺列却照样起来 —— runner 把「读不出账」当成了「账是空的」")
	}
	if n := ledgerProbeCount(t, dsn,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'np_user'`); n != 0 {
		t.Error("被拒之后 np_user 竟然建出来了：拒绝发生在建表之后，等于半迁移")
	}
}
