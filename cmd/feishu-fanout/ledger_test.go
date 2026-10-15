package main

// 本文件钉 R-G2-LEDGER-DOMAIN 在【装配层】的那一半：openSpoolPool 建完池就把 fanout 域的账本
// 施加到这个库上（于是 fanout.OpenSpool 可以只核对不建表），以及两条运维会真遇到的形状：
//
//	① 老库升级：五表由旧版 OpenSpool 自建、账本表还不存在 ⇒ 补记一条 0400，表与数据都不动；
//	② DSN 指错库：FANOUT_PG_DSN 指到 Hub 库 ⇒ 拒绝启动，且不往那个库里建任何 fanout 表。
//
// 🔴 两格都走【产品入口】openSpoolPool，不自己另调一次 OpenPG＋Migrate：
// 夹具另起一条路，证明的就是夹具而不是产品（durability_test.go 文件头注 ① 同一条理由）。

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/fanout"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestOpenSpoolPool_LegacyUnledgaredDB(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	ac, err := repo.NewAccessCtx("org-legacy", "principal-legacy")
	if err != nil {
		t.Fatalf("构造访问上下文失败: %v", err)
	}

	// 造「老 fanout 库」：五表由旧版 OpenSpool 自建（＝直接施加 pgschema.sql），账本表还不存在，
	// 并且表里有一行已发的真数据 —— 它是「升级不得动数据」的探针。
	raw, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("开造库池失败: %v", err)
	}
	if _, err := raw.Exec(ctx, ac, fanout.PGSchemaSQL); err != nil {
		t.Fatalf("造老库（施加五表）失败: %v", err)
	}
	if _, err := raw.Exec(ctx, ac,
		`INSERT INTO outbound(outbound_id, caller_id, request_id, body_hash, body, status,
		   lark_msg_id, created_at, updated_at)
		 VALUES('ob_legacy','caller-legacy','req-legacy','hash-legacy','payload','sent','om_old',100,100)`); err != nil {
		t.Fatalf("塞探针行失败: %v", err)
	}
	if tableInDB(t, raw, "schema_migrations") {
		t.Fatal("造出来的老库竟然已有账本表 —— 本格要的就是「表在、账不在」那个形状")
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("关造库池失败: %v", err)
	}

	pool, rep, err := openSpoolPool(ctx, dsn) // ← 产品入口
	if err != nil {
		t.Fatalf("老 fanout 库上产品入口失败（＝升级起不来）: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	if strings.Join(rep.Applied, ",") != "0400_fanout_baseline.sql" {
		t.Errorf("Applied = %v，要补记 0400 一条（Skipped=%v）", rep.Applied, rep.Skipped)
	}
	// 🔴 数据还在＝表没被重建（0400 的 DDL 全是 CREATE TABLE IF NOT EXISTS）。
	// 只断言「没报错」是不够的：一个先 DROP 再 CREATE 的账本条目同样不报错，却把队列清了。
	var status, msgID string
	row, err := pool.QueryRow(ctx, ac, `SELECT status, lark_msg_id FROM outbound WHERE outbound_id='ob_legacy'`)
	if err != nil {
		t.Fatalf("读探针行失败: %v", err)
	}
	if err := row.Scan(&status, &msgID); err != nil {
		t.Fatalf("扫描探针行失败: %v", err)
	}
	if status != "sent" || msgID != "om_old" {
		t.Errorf("探针行被改动了：status=%q lark_msg_id=%q，要 sent／om_old", status, msgID)
	}
	// 账本到位之后，fanout.New 走的那条（OpenSpool 只核对）必须过。
	if _, err := fanout.OpenSpool(pool); err != nil {
		t.Errorf("账本施加之后 OpenSpool 仍拒: %v", err)
	}
	// 再跑一次产品入口＝空转（fanout 每次重启都走这一遍）。
	pool2, rep2, err := openSpoolPool(ctx, dsn)
	if err != nil {
		t.Fatalf("重跑产品入口失败: %v", err)
	}
	t.Cleanup(func() { _ = pool2.Close() })
	if len(rep2.Applied) != 0 || len(rep2.Skipped) != 1 {
		t.Errorf("重跑 Applied=%v Skipped=%v，要空转一条", rep2.Applied, rep2.Skipped)
	}
}

func TestOpenSpoolPool_RefusesHubDB(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	ac, err := repo.NewAccessCtx("org-misconfig", "principal-misconfig")
	if err != nil {
		t.Fatalf("构造访问上下文失败: %v", err)
	}

	// 造 Hub 库：施加 Hub 域账本（＝Hub 部署干的事）。
	entries, err := migrate.LedgerInDomains(migrate.HubDomains())
	if err != nil {
		t.Fatalf("取 Hub 域条目失败: %v", err)
	}
	raw, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("开造库池失败: %v", err)
	}
	if _, err := migrate.MigrateEntries(ctx, raw, entries, nil); err != nil {
		t.Fatalf("造 Hub 库失败: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("关造库池失败: %v", err)
	}

	// FANOUT_PG_DSN 指到这个库 ⇒ 产品入口必须拒（裁定 R-G2-LEDGER-DOMAIN：两库各自独立）。
	pool, rep, err := openSpoolPool(ctx, dsn)
	if err == nil {
		_ = pool.Close()
		t.Fatalf("fanout 指着 Hub 库竟然起来了（rep=%v）—— 五张 fanout 表会被建进 Hub 库", rep)
	}
	if pool != nil {
		t.Errorf("拒绝时把池交回来了 —— 调用方只会拿到一个已经关掉／没人管的句柄")
	}
	if len(rep.Applied) != 0 || len(rep.Skipped) != 0 {
		t.Errorf("拒绝时带回了读数 Applied=%v Skipped=%v", rep.Applied, rep.Skipped)
	}
	for _, want := range []string{"0001_core_baseline.sql", "0100_identity_baseline.sql",
		"core 域", "identity 域", "fanout", "DSN 指错了库", "不施加任何条目"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("拒绝文案缺 %q（运维要能一眼看出是 FANOUT_PG_DSN 配错了）: %v", want, err)
		}
	}

	// 🔴 拒绝之后还要核库：Hub 库里不得出现任何 fanout 表，账上也不得多出 0400。
	probe, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("开核对池失败: %v", err)
	}
	t.Cleanup(func() { _ = probe.Close() })
	for _, tbl := range []string{"outbound", "inbound_msg", "inbound_sink", "waive_audit", "selfcheck"} {
		if tableInDB(t, probe, tbl) {
			t.Errorf("Hub 库里出现了 fanout 表 %s —— 拒绝必须发生在第一条 DDL 之前", tbl)
		}
	}
	rows, err := probe.Query(ctx, ac, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("读 Hub 库的账失败: %v", err)
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("扫描账失败: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历账失败: %v", err)
	}
	// Compare exactly with the Hub ledger seeded above. Adding a legitimate
	// Hub migration must not look like a fanout mutation, while every added,
	// removed or changed ledger row still fails this equality.
	wantVersions := make([]string, 0, len(entries))
	for _, entry := range entries {
		wantVersions = append(wantVersions, entry.Name)
	}
	sort.Strings(wantVersions)
	if strings.Join(versions, ",") != strings.Join(wantVersions, ",") {
		t.Errorf("Hub 库的账被动了：%v", versions)
	}
}

// tableInDB 表在不在（按 search_path 解析）。
func tableInDB(t *testing.T, pool *repo.Pool, name string) bool {
	t.Helper()
	ac, err := repo.NewAccessCtx("org-probe", "principal-probe")
	if err != nil {
		t.Fatalf("构造访问上下文失败: %v", err)
	}
	row, err := pool.QueryRow(context.Background(), ac, `SELECT to_regclass($1) IS NOT NULL`, name)
	if err != nil {
		t.Fatalf("查表 %s 失败: %v", name, err)
	}
	var ok bool
	if err := row.Scan(&ok); err != nil {
		t.Fatalf("扫描表 %s 的存在性失败: %v", name, err)
	}
	return ok
}
