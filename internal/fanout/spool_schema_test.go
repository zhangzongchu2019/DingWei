package fanout

// 本文件钉的是 R-G2-LEDGER-DOMAIN（裁定 RULING-REGISTER 2026-10-02T11:06:25Z）在【本包】的那一半：
// OpenSpool 不再自建表，只核对五表在不在（只读）；建表归迁移账本条目 0400，由装配层
// cmd/feishu-fanout 的 openSpoolPool 施加。
//
// 🔴 另一半（「装配层真的施加了 fanout 域、且只施加 fanout 域」）在 cmd/feishu-fanout 的用例里钉，
// 不在这里：本包用例【不得】import internal/store/migrate —— 那个包的用例 import 了本包
// （双轨形状格要读 fanout.PGSchemaSQL 当轨 A），同包用例反向 import 会成环。

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// spoolTestPool 起一个临时 PG 并施加 fanout 五表，返回连到它的池。
//
// 🔴 本包用例一律经它拿池：OpenSpool 已经不建表了，直接 pgtest.Pool(t) 拿到的是空库，
// 装配出来的 Server 会走「spool 未开 ⇒ 托管路 503」那条路 —— 那形状与「用例忘了建表」
// 长得一模一样，绿得毫无意义。
func spoolTestPool(t *testing.T) *repo.Pool {
	t.Helper()
	pool := pgtest.Pool(t)
	applySpoolSchema(t, pool)
	return pool
}

// applySpoolSchema 施加五表（字节＝pgschema.sql，即账本 0400 的冻结源）。
//
// 🔴 用【源字节】而不是账本条目：本包 import 不了 migrate（成环，见文件头注），
// 而两者同源这件事由 internal/store/migrate 的双轨形状格守着（漂移即红）。
func applySpoolSchema(t *testing.T, pool *repo.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), testAccess(t), PGSchemaSQL); err != nil {
		t.Fatalf("施加 fanout 五表失败（pgschema.sql）: %v", err)
	}
}

// testAccess 用例里的访问上下文。
//
// 🔴 故意不用系统上下文：边界闸按文本清点那类调用点（每处都是评审面，口径含 _test.go），
// 用例里的建表／查询不该混进那个数 —— 与 internal/store/migrate 的 testCtx 同一条理由。
func testAccess(t *testing.T) repo.AccessCtx {
	t.Helper()
	ac, err := repo.NewAccessCtx("test", "fanout")
	if err != nil {
		t.Fatalf("构造访问上下文失败: %v", err)
	}
	return ac
}

func tableExists(t *testing.T, pool *repo.Pool, name string) bool {
	t.Helper()
	row, err := pool.QueryRow(context.Background(), testAccess(t), `SELECT to_regclass($1) IS NOT NULL`, name)
	if err != nil {
		t.Fatalf("查表 %s 在不在失败: %v", name, err)
	}
	var ok bool
	if err := row.Scan(&ok); err != nil {
		t.Fatalf("扫描表 %s 的存在性失败: %v", name, err)
	}
	return ok
}

// TestOpenSpool_RefusesUnledgaredDBWithoutCreatingAnything 钉「OpenSpool 只核对、不自建」。
//
// 判据两条，缺一条本格就冒充得过：
//
//	① 空库上 OpenSpool 必须拒，且点名缺哪几张表、指出建表归账本 0400（运维据此知道去查装配层）；
//	② 拒的时候【一张表都没建】。只断言 ①，「先建表再返回一个错」的实现照样绿 ——
//	   而那正是本格要拦的形状（表建出来了，账本的 digest 保护就成了摆设）。
//
// 🔴 反向对照（交付件 K4）：把 OpenSpool 里的 CheckSchema 换回 Exec(PGSchemaSQL) ⇒ ①②同时红。
func TestOpenSpool_RefusesUnledgaredDBWithoutCreatingAnything(t *testing.T) {
	pool := pgtest.Pool(t) // 空库：不经账本、也不经 applySpoolSchema
	ctx := context.Background()

	_, err := OpenSpool(pool)
	if err == nil {
		t.Fatal("空库上 OpenSpool 竟然成功 —— 它已经不该自建表了（建表归账本 0400，装配层施加）")
	}
	for _, tbl := range spoolRequiredTables {
		if !strings.Contains(err.Error(), tbl) {
			t.Errorf("拒绝文案没点出缺表 %s（运维要能照文案去查）: %v", tbl, err)
		}
	}
	for _, want := range []string{"0400_fanout_baseline", "openSpoolPool", "R-G2-LEDGER-DOMAIN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("拒绝文案缺 %q（要指到建表的真源与裁定，而不是只说「缺表」）: %v", want, err)
		}
	}
	for _, tbl := range spoolRequiredTables {
		if tableExists(t, pool, tbl) {
			t.Errorf("OpenSpool 拒了却把表 %s 建出来了 —— 核对必须是只读的", tbl)
		}
	}

	// CheckSchema 单独再核一次：同样只读、同样拒（OpenSpool 只是它的一层包装）。
	if err := CheckSchema(ctx, pool); err == nil {
		t.Errorf("空库上 CheckSchema 竟然通过")
	}
	for _, tbl := range spoolRequiredTables {
		if tableExists(t, pool, tbl) {
			t.Errorf("CheckSchema 竟然建出了表 %s —— 它的名字里不该有任何 DDL", tbl)
		}
	}

	// 施加账本的源字节之后，两个入口都过（＝装配层先 Migrate、再 OpenSpool 的生产顺序）。
	applySpoolSchema(t, pool)
	if err := CheckSchema(ctx, pool); err != nil {
		t.Errorf("五表齐了 CheckSchema 仍拒: %v", err)
	}
	if _, err := OpenSpool(pool); err != nil {
		t.Errorf("五表齐了 OpenSpool 仍拒: %v", err)
	}
	// 🔴 少一张也算缺：把 selfcheck drop 掉（pre-arm③ 的靶表，原先由 admin.go 懒建 ——
	// 收口进 pgschema.sql 之后没人再懒建它，漏了就是漏了）。
	if _, err := pool.Exec(ctx, testAccess(t), `DROP TABLE selfcheck`); err != nil {
		t.Fatalf("drop selfcheck 失败: %v", err)
	}
	err = CheckSchema(ctx, pool)
	if err == nil {
		t.Fatal("少一张表 CheckSchema 竟然通过 —— 「五表都在」这条判据是死的")
	}
	if !strings.Contains(err.Error(), "selfcheck") {
		t.Errorf("拒绝文案没点出少的是 selfcheck: %v", err)
	}
	if strings.Contains(err.Error(), "outbound") {
		t.Errorf("拒绝文案把在场的 outbound 也报成缺（运维会去查一张好表）: %v", err)
	}
}

// TestOpenSpool_NilPoolIsError 保住原有的那条：不给池就是错，不是「spool 静默不开」。
func TestOpenSpool_NilPoolIsError(t *testing.T) {
	if _, err := OpenSpool(nil); err == nil {
		t.Error("OpenSpool(nil) 竟然成功")
	}
	if err := CheckSchema(context.Background(), nil); err == nil {
		t.Error("CheckSchema(nil) 竟然成功")
	}
}
