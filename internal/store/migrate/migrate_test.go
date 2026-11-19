package migrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// ── 共用夹具 ─────────────────────────────────────────────────────────────
//
// 🔴 为什么用「一个临时库 ＋ 每格一个 schema」而不是每格起一个库：
// 起一次内嵌 PG 是秒级的，一格一个库会让本包的用例把时间全花在起停上；
// schema 之间互不可见，而账本只在 search_path 里建表、不碰别的 schema，隔离性对这些格子够用。
// （cmd/workpulse/bootstrap_admin_test.go 的 pgSchemaDSN 是同一个办法。）
// 🔴 本包用例【不得】t.Parallel()：pgtest 的残留快照按「本格起停前后」判增量，
// 并行跑会互相把对方的进程／日志读成自己的残留（口径页与 pgtest 头注同一条纪律）。

// testSchema 在同一个临时库里开一个独立 schema，返回连到它的池（含 t.Cleanup 关池）。
func testSchema(t *testing.T, dsn string) *repo.Pool {
	t.Helper()
	ctx := context.Background()
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("开管理池失败: %v", err)
	}
	defer admin.Close()
	schema := uniqueSchema()
	if _, err := admin.Exec(ctx, testCtx(t), `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("建 schema 失败: %v", err)
	}
	pool, err := repo.OpenPG(ctx, withSearchPath(dsn, schema))
	if err != nil {
		t.Fatalf("开 schema 池失败: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("关池失败: %v", err)
		}
	})
	return pool
}

// testCtx 用例里的访问上下文。
//
// 🔴 故意【不用】repo.SystemCtx()：闸按文本清点 SystemCtx 调用点（每处都是评审面），
// 用例里的建表／查询不该混进那个数——生产侧唯一一处 SystemCtx 在 MigrateEntries 里。
func testCtx(t *testing.T) repo.AccessCtx {
	t.Helper()
	ctx, err := repo.NewAccessCtx("test", "migrate")
	if err != nil {
		t.Fatalf("构造访问上下文失败: %v", err)
	}
	return ctx
}

func uniqueSchema() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "m_" + hex.EncodeToString(b)
}

func withSearchPath(dsn, schema string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + schema
}

func mustExec(t *testing.T, pool *repo.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), testCtx(t), query, args...); err != nil {
		t.Fatalf("执行失败 %q: %v", query, err)
	}
}

// tableExists 表在不在（按 search_path 解析，故判的是本格 schema 里的那张）。
func tableExists(t *testing.T, pool *repo.Pool, name string) bool {
	t.Helper()
	row, err := pool.QueryRow(context.Background(), testCtx(t), `SELECT to_regclass($1) IS NOT NULL`, name)
	if err != nil {
		t.Fatalf("查表失败: %v", err)
	}
	var ok bool
	if err := row.Scan(&ok); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	return ok
}

// ledgerRows 读账本表：version → (digest, applied_at)。
func ledgerRows(t *testing.T, pool *repo.Pool) map[string]ledgerRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), testCtx(t),
		`SELECT version, digest, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("读账失败: %v", err)
	}
	defer rows.Close()
	out := map[string]ledgerRow{}
	for rows.Next() {
		var r ledgerRow
		if err := rows.Scan(&r.version, &r.digest, &r.appliedAt); err != nil {
			t.Fatalf("扫描账失败: %v", err)
		}
		out[r.version] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历账失败: %v", err)
	}
	return out
}

type ledgerRow struct {
	version   string
	digest    string
	appliedAt time.Time
}

// resetRegistryForTest 清掉 Register 的全局登记（用例之间不许互相留状态）。
func resetRegistryForTest() {
	mu.Lock()
	registered = nil
	mu.Unlock()
}

// ── 账本内容（不需库） ─────────────────────────────────────────────────────

func TestLedger_EmbeddedEntries(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	// 🔴 这份名单写死而不引 embed 去比：本格钉的就是「内嵌进二进制的账本是这几条、这个序」。
	// 加一条条目 ⇒ 改这里（红一次是应当的：账本主键集合变了，必须有人看过一眼）。
	want := []string{"0001_core_baseline.sql", "0100_identity_baseline.sql", "0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0106_tempgroup.sql", "0107_mcpgw_oauth.sql", "0108_extseat_short_name.sql", "0110_extseat_expect_reply.sql", "0111_extseat_feishu_copy.sql", "0112_np_agent_display_name.sql", "0200_hosted_r30_baseline.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql"}
	if len(entries) != len(want) {
		t.Fatalf("账本条数 = %d（%v），要 %d", len(entries), names(entries), len(want))
	}
	for i, e := range entries {
		if e.Name != want[i] {
			t.Errorf("第 %d 条 = %s，要 %s（顺序＝文件名字典序，是语义的一部分）", i, e.Name, want[i])
		}
		if strings.TrimSpace(e.Body) == "" {
			t.Errorf("%s: 正文为空 —— 内嵌没打进二进制", e.Name)
		}
		if got := Digest(e.Body); len(got) != 64 {
			t.Errorf("%s: digest %q 不是 sha256 hex", e.Name, got)
		}
	}
	// 顺序依赖必须成立：身份域复用 audit_log 而自己不建（0100 头注同一句），
	// 故 0001 必须排在 0100 前面。这条断言钉的是【依赖】，不是文件名的巧合。
	if !strings.Contains(entries[0].Body, "audit_log") {
		t.Errorf("0001 里没有 audit_log —— 0100 依赖它先存在")
	}
	if strings.Contains(entries[1].Body, "CREATE TABLE IF NOT EXISTS audit_log") {
		t.Errorf("0100 里出现了建 audit_log —— 身份域不建这张表（复用 core 的）")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	Register(Entry{Name: "9001_test_a.sql", Body: "CREATE TABLE IF NOT EXISTS t_a(a int);"})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("撞名没有 panic —— 两个域用同一个号就会互相盖账")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "被登记两次") {
			t.Errorf("panic 文案 %q 没点出撞名", msg)
		}
	}()
	Register(Entry{Name: "9001_test_a.sql", Body: "CREATE TABLE IF NOT EXISTS t_b(b int);"})
}

func TestRegister_EmptyNameOrBodyPanics(t *testing.T) {
	for _, tc := range []struct {
		what string
		e    Entry
	}{
		{"缺名字", Entry{Name: "  ", Body: "CREATE TABLE IF NOT EXISTS t(a int);"}},
		// 🔴 只有【空白】才算空。「只有注释」不在这一格判：判它要解析 SQL（块注释、字符串里的
		// 减号都是坑），而这类条目真出现了会被双轨指纹用例抓住（域 DDL 改了、账本形状没改）。
		{"正文只有空白", Entry{Name: "9002_test_empty.sql", Body: "  \n\t \n"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			resetRegistryForTest()
			t.Cleanup(resetRegistryForTest)
			defer func() {
				if recover() == nil {
					t.Fatalf("Register(%s) 没有 panic", tc.what)
				}
			}()
			Register(tc.e)
		})
	}
}

func TestLedger_RegisterCollidingWithEmbeddedIsError(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	// 与内嵌条目撞名：🔴 不用 panic 表达（那要读 embed，I/O 错不该 panic），改成 Ledger 返回错误。
	Register(Entry{Name: "0001_core_baseline.sql", Body: "CREATE TABLE IF NOT EXISTS t_x(a int);"})
	if _, err := Ledger(); err == nil {
		t.Fatalf("Register 与内嵌条目撞名，Ledger 却没报错 —— 同一段 DDL 会以两个来源各记一本账")
	} else if !strings.Contains(err.Error(), "撞名") {
		t.Errorf("错误文案没点出撞名: %v", err)
	}
}

func TestLedger_RegisteredEntriesSortWithEmbedded(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	Register(Entry{Name: "0200_hosted_test.sql", Body: "CREATE TABLE IF NOT EXISTS t_h(a int);"})
	Register(Entry{Name: "0050_core_test.sql", Body: "CREATE TABLE IF NOT EXISTS t_c(a int);"})
	// 🔴 0900 这条是随 0400（fanout 基线）进账本后补的：原先登记的两条分别落在内嵌条目的
	// 【中间】与【末尾之后】，0400 一加进来就没有任何东西排在它后面了 —— 少了「末位之后也要排对」
	// 这一半，排序格对「新条目该落哪儿」的判别力就缺了一角（判据有几半，格就得有几半）。
	Register(Entry{Name: "0900_cross_test.sql", Body: "CREATE TABLE IF NOT EXISTS t_x(a int);"})
	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	got := names(entries)
	want := []string{"0001_core_baseline.sql", "0050_core_test.sql", "0100_identity_baseline.sql",
		"0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0106_tempgroup.sql", "0107_mcpgw_oauth.sql", "0108_extseat_short_name.sql", "0110_extseat_expect_reply.sql", "0111_extseat_feishu_copy.sql", "0112_np_agent_display_name.sql", "0200_hosted_r30_baseline.sql", "0200_hosted_test.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql", "0900_cross_test.sql"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("账本顺序 = %v，要 %v（字典序，与来源无关）", got, want)
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

// ── 应用语义（要库） ───────────────────────────────────────────────────────

// twoEntries 里 0002 依赖 0001 建的表：顺序错了就建不出索引 ⇒ 「显式排序」不是纸面承诺。
func twoEntries() []Entry {
	return []Entry{
		{Name: "0002_test_index.sql", Body: "CREATE INDEX IF NOT EXISTS t_i ON t_first(a);"},
		{Name: "0001_test_first.sql", Body: "CREATE TABLE IF NOT EXISTS t_first(a int);\nCREATE TABLE IF NOT EXISTS t_second(b int);"},
	}
}

func TestMigrateEntries_AppliesSortedOnceAndRecordsClock(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()

	first := &clock.Fake{T: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)}
	rep, err := MigrateEntries(ctx, pool, twoEntries(), first)
	if err != nil {
		t.Fatalf("首次 Migrate: %v", err)
	}
	// 给的顺序是 0002 在前，而 0002 依赖 0001 的表 ⇒ 没排序就必然失败；这里成功即证明排了。
	if strings.Join(rep.Applied, ",") != "0001_test_first.sql,0002_test_index.sql" {
		t.Errorf("Applied = %v，要按字典序两条", rep.Applied)
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("首次跑就有 Skipped = %v", rep.Skipped)
	}
	if !tableExists(t, pool, "t_first") || !tableExists(t, pool, "t_second") {
		t.Errorf("一次 Exec 里的多条语句没有全部生效（无参 ⇒ 简单协议才允许多语句）")
	}

	rows := ledgerRows(t, pool)
	if len(rows) != 2 {
		t.Fatalf("账上 %d 条，要 2 条", len(rows))
	}
	for _, e := range twoEntries() {
		r, ok := rows[e.Name]
		if !ok {
			t.Fatalf("账上缺 %s", e.Name)
		}
		if r.digest != Digest(e.Body) {
			t.Errorf("%s: 账内 digest %s ≠ 正文 digest %s", e.Name, r.digest, Digest(e.Body))
		}
		if !r.appliedAt.Equal(first.T) {
			t.Errorf("%s: applied_at = %v，要注入时钟的 %v（时间由接口传入，便于断言）", e.Name, r.appliedAt, first.T)
		}
	}

	// 第二次：换个时钟，什么都不该动 —— 账上的 applied_at 仍是第一次那个时刻。
	second := &clock.Fake{T: first.T.Add(9 * time.Hour)}
	rep2, err := MigrateEntries(ctx, pool, twoEntries(), second)
	if err != nil {
		t.Fatalf("重跑 Migrate: %v", err)
	}
	if len(rep2.Applied) != 0 {
		t.Errorf("重跑又执行了 %v —— 一次性变更变成了每次重启执行一次（SQLite 侧事故 ①）", rep2.Applied)
	}
	if strings.Join(rep2.Skipped, ",") != "0001_test_first.sql,0002_test_index.sql" {
		t.Errorf("Skipped = %v，要两条", rep2.Skipped)
	}
	for name, want := range ledgerRows(t, pool) {
		if !want.appliedAt.Equal(first.T) {
			t.Errorf("%s: 重跑后 applied_at 变成 %v —— 账被盖过了", name, want.appliedAt)
		}
	}
}

func TestMigrateEntries_RefusesChangedEntry(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()
	clk := &clock.Fake{T: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)}

	entries := twoEntries()
	if _, err := MigrateEntries(ctx, pool, entries, clk); err != nil {
		t.Fatalf("首次 Migrate: %v", err)
	}

	// 把已应用的 0001 正文改掉（多建一张表），并在后面再排一条新的：
	// 拒绝必须发生在【动手之前】，故新表与后面那条都不该出现。
	changed := []Entry{
		{Name: "0001_test_first.sql", Body: "CREATE TABLE IF NOT EXISTS t_first(a int);\nCREATE TABLE IF NOT EXISTS t_second(b int);\nCREATE TABLE IF NOT EXISTS t_tamper(c int);"},
		{Name: "0002_test_index.sql", Body: "CREATE INDEX IF NOT EXISTS t_i ON t_first(a);"},
		{Name: "0003_test_later.sql", Body: "CREATE TABLE IF NOT EXISTS t_later(d int);"},
	}
	rep, err := MigrateEntries(ctx, pool, changed, clk)
	if err == nil {
		t.Fatalf("已应用条目内容变了却没拒绝 —— 库里是按旧内容迁的，继续跑等于拿新账认旧事实")
	}
	for _, want := range []string{"0001_test_first.sql", "已应用的迁移内容已变更", "拒绝启动"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文案缺 %q: %v", want, err)
		}
	}
	// 错误里必须【同时】给出两个 digest，否则运维只能看到「变了」却比不出变了什么。
	// 🔴 取的是 0001 那条的原文（twoEntries() 里它排在切片第二位，别按位置取错）。
	original := ""
	for _, e := range entries {
		if e.Name == "0001_test_first.sql" {
			original = e.Body
		}
	}
	if !strings.Contains(err.Error(), Digest(original)) || !strings.Contains(err.Error(), Digest(changed[0].Body)) {
		t.Errorf("错误里没同时给出账内与当前两个 digest，运维无从比对: %v", err)
	}
	if len(rep.Applied) != 0 {
		t.Errorf("拒绝的那次竟然执行了 %v", rep.Applied)
	}
	if tableExists(t, pool, "t_tamper") {
		t.Errorf("改过的正文被执行了 —— 拒绝必须发生在动手之前")
	}
	if tableExists(t, pool, "t_later") {
		t.Errorf("撞在变更条目之后的 0003 也被应用了 —— 拒绝就该停下，不继续迁后面的")
	}
	if len(ledgerRows(t, pool)) != 2 {
		t.Errorf("账被写脏了: %v", ledgerRows(t, pool))
	}
}

func TestMigrateEntries_FailedEntryLeavesNoTrace(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()
	clk := &clock.Fake{T: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)}

	entries := []Entry{
		{Name: "0001_ok.sql", Body: "CREATE TABLE IF NOT EXISTS t_ok(a int);"},
		// 前半段建两张表、后半段炸：PG 的 DDL 是事务性的 ⇒ 这一条要么全成、要么一点没发生。
		{Name: "0002_bad.sql", Body: "CREATE TABLE IF NOT EXISTS t_half1(a int);\nCREATE TABLE IF NOT EXISTS t_half2(b int);\nSELECT 1/0;"},
	}
	rep, err := MigrateEntries(ctx, pool, entries, clk)
	if err == nil {
		t.Fatalf("坏条目没有报错")
	}
	if !strings.Contains(err.Error(), "0002_bad.sql") {
		t.Errorf("错误没点出是哪一条: %v", err)
	}
	if strings.Join(rep.Applied, ",") != "0001_ok.sql" {
		t.Errorf("Applied = %v，要只有前一条（出错时 Report 反映出错之前已做的事）", rep.Applied)
	}
	if tableExists(t, pool, "t_half1") || tableExists(t, pool, "t_half2") {
		t.Errorf("失败条目留下了半套表 —— DDL 与账必须同事务，回滚要一起回")
	}
	rows := ledgerRows(t, pool)
	if _, ok := rows["0002_bad.sql"]; ok {
		t.Errorf("失败条目却记了账 —— 「已应用」发给了一个没做成的版本")
	}
	if _, ok := rows["0001_ok.sql"]; !ok {
		t.Errorf("成功那条没记账: %v", rows)
	}
	if !tableExists(t, pool, "t_ok") {
		t.Errorf("成功那条的表不在")
	}

	// 修好之后重跑：只补上那一条，成功那条不重跑。
	fixed := []Entry{entries[0], {Name: "0002_bad.sql", Body: "CREATE TABLE IF NOT EXISTS t_half1(a int);"}}
	rep2, err := MigrateEntries(ctx, pool, fixed, clk)
	if err != nil {
		t.Fatalf("修好后重跑: %v", err)
	}
	if strings.Join(rep2.Applied, ",") != "0002_bad.sql" || strings.Join(rep2.Skipped, ",") != "0001_ok.sql" {
		t.Errorf("Applied=%v Skipped=%v，要只补 0002、0001 跳过", rep2.Applied, rep2.Skipped)
	}
}

func TestMigrateEntries_RejectsBadInputBeforeTouchingDB(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()
	clk := &clock.Fake{T: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)}

	for _, tc := range []struct {
		what    string
		entries []Entry
		want    string
	}{
		{"缺名字", []Entry{{Name: " ", Body: "CREATE TABLE IF NOT EXISTS t(a int);"}}, "缺名字"},
		{"名字不带 .sql", []Entry{{Name: "0001_test", Body: "CREATE TABLE IF NOT EXISTS t(a int);"}}, ".sql"},
		{"正文为空", []Entry{{Name: "0001_test.sql", Body: "   \n"}}, "正文为空"},
		{"同批撞名", []Entry{
			{Name: "0001_test.sql", Body: "CREATE TABLE IF NOT EXISTS t(a int);"},
			{Name: "0001_test.sql", Body: "CREATE TABLE IF NOT EXISTS t2(a int);"},
		}, "出现两次"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			_, err := MigrateEntries(ctx, pool, tc.entries, clk)
			if err == nil {
				t.Fatalf("%s 没被拒", tc.what)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误文案缺 %q: %v", tc.want, err)
			}
			// 🔴 先全验再动手：验不过就连账本表都不该建，否则「库被碰过但什么都没迁」这种状态
			// 会让人以为迁移跑了一半。
			if tableExists(t, pool, "schema_migrations") {
				t.Errorf("%s 被拒之前已经建了账本表", tc.what)
			}
		})
	}
}

func TestMigrateEntries_RejectsZeroClock(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)

	_, err := MigrateEntries(context.Background(), pool, twoEntries(), &clock.Fake{})
	if err == nil {
		t.Fatalf("零时刻没被拒 —— applied_at 是「库什么时候真的应用了这条」的唯一记录")
	}
	if !strings.Contains(err.Error(), "零时刻") {
		t.Errorf("错误文案没点出零时刻: %v", err)
	}
	if tableExists(t, pool, "t_first") {
		t.Errorf("拒绝之前已经建了表")
	}
}

func TestMigrateEntries_NilClockUsesReal(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)

	before := time.Now().Add(-time.Minute)
	if _, err := MigrateEntries(context.Background(), pool, twoEntries(), nil); err != nil {
		t.Fatalf("nil 时钟: %v", err)
	}
	for name, r := range ledgerRows(t, pool) {
		if r.appliedAt.Before(before) || r.appliedAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("%s: applied_at = %v，不像真实时钟盖的", name, r.appliedAt)
		}
	}
}

func TestMigrateEntries_NilPoolIsError(t *testing.T) {
	if _, err := MigrateEntries(context.Background(), nil, twoEntries(), clock.Real{}); err == nil {
		t.Fatalf("缺连接池没被拒")
	}
}

// ── 事务边界与并发启动（组长裁定 ruling-2026-10-01T170223Z 的 ①③ 两件） ─────────
//
// 🔴 这两格的出处都是【复验方抓到、本席原用例抓不到】的变异，不是补覆盖率：
//   ① A6（DDL 挪到事务外、只在事务里记账）⇒ 本席原 15 格全绿；
//   ③ 并发启动 ⇒ 原实现明写「不做跨进程互斥」，6 路并发 1 成 5 败（撞唯一键），
//      而那在运维眼里长得跟「库坏了」一样。
// 两格的用例形状取自复验件 REVIEW-G2-MIGRATE-LEDGER-tosonnet55tc2k-20261001.md 附录 A.1，
// 本席在此基础上各加了断言（见各格注里「比复验方原格多钉的」）。

// TestMigrateEntries_LedgerInsertFailureRollsBackDDL 钉「DDL 与记账是同一个事务」。
//
// 办法（复验方 A.1 的办法，照用）：预建一张同名同形、但带 CHECK 的账表，让第二条的记账必败；
// 账表 DDL 是 IF NOT EXISTS ⇒ runner 会沿用这张预建表。
// 断言第二条建的表【不存在】：留着它就等于「库里有这条建的表、账上没这条」，
// 下次启动会再跑一遍这条，遇到非幂等 DDL（ALTER／CREATE INDEX 无 IF NOT EXISTS）就起不来。
//
// 比复验方原格多钉的两处：
//
//	· 第一条的表【必须还在】且账上有它 —— 否则本格会因为「整个 Migrate 什么都没做」而假绿；
//	· 出错时交回的 Report 要点名第一条已应用（排障要知道卡在哪条之后）。
func TestMigrateEntries_LedgerInsertFailureRollsBackDDL(t *testing.T) {
	ctx := context.Background()
	pool := testSchema(t, pgtest.DSN(t))
	mustExec(t, pool, `CREATE TABLE schema_migrations(
		version    text        PRIMARY KEY,
		digest     text        NOT NULL,
		applied_at timestamptz NOT NULL,
		CHECK (version <> '0002_zz.sql'))`)

	entries := []Entry{
		{Name: "0001_zz.sql", Body: `CREATE TABLE zz_a(x int)`},
		{Name: "0002_zz.sql", Body: `CREATE TABLE zz_b(x int)`},
	}
	rep, err := MigrateEntries(ctx, pool, entries, nil)
	if err == nil {
		t.Fatalf("第二条的记账被 CHECK 挡住，Migrate 却报成功（rep=%v）", rep)
	}
	if !strings.Contains(err.Error(), "0002_zz.sql") {
		t.Errorf("错误没点出是哪一条: %v", err)
	}
	if tableExists(t, pool, "zz_b") {
		t.Error("zz_b 还在 —— DDL 与记账不是同一个事务（记账失败而表留下了）")
	}
	if !tableExists(t, pool, "zz_a") {
		t.Error("zz_a 不在 —— 第一条本该已【各自一个事务】提交；本格要判的是第二条回滚，不是全批没跑")
	}
	if strings.Join(rep.Applied, ",") != "0001_zz.sql" {
		t.Errorf("出错时交回的 Applied = %v，要 [0001_zz.sql]（反映出错之前已做的事）", rep.Applied)
	}
	rows := ledgerRows(t, pool)
	if len(rows) != 1 {
		t.Fatalf("账上 %d 条，要 1 条", len(rows))
	}
	if rows["0001_zz.sql"].digest != Digest(entries[0].Body) {
		t.Error("第一条的 digest 与正文不符")
	}
}

// sameSchemaPools 在【同一个 schema】上开 n 个池。
//
// 🔴 为什么不给 n 个 goroutine 共用一个池：真并发启动是 n 个【进程】，各有各的连接池；
// 共用一个池时，锁可能恰好落在同一条连接上（于是看起来"串行得很干净"），
// 那会把「跨会话串行化」这半判据偷偷换成「同会话内排队」。
func sameSchemaPools(t *testing.T, dsn string, n int) []*repo.Pool {
	t.Helper()
	ctx := context.Background()
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("开管理池失败: %v", err)
	}
	defer func() { _ = admin.Close() }()
	schema := uniqueSchema()
	if _, err := admin.Exec(ctx, testCtx(t), `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("建 schema 失败: %v", err)
	}
	out := make([]*repo.Pool, n)
	for i := range out {
		p, err := repo.OpenPG(ctx, withSearchPath(dsn, schema))
		if err != nil {
			t.Fatalf("开第 %d 个池失败: %v", i, err)
		}
		out[i] = p
	}
	t.Cleanup(func() {
		for _, p := range out {
			_ = p.Close()
		}
	})
	return out
}

// TestMigrate_ConcurrentRunnersAllSucceed 钉「多进程同时启动不再互相拒启」。
//
// 判据（组长裁定 ruling-2026-10-01T170223Z 第③件）：6 路并发【全成功】、账上每条恰好一次。
// 🔴 反向对照＝撤掉 takeLock 那两句（ensureLedgerTable 与 applyOne 里各一处）⇒ 本格必红：
// 撤锁后两处并发 CREATE TABLE IF NOT EXISTS 撞 pg_type_typname_nsp_index、
// 或两处并发记账撞 schema_migrations 主键，6 路里只有 1 路能成（复验方在未加锁的版本上实测 1 成 5 败）。
//
// 🔴 断言的是「每条条目被应用【恰好一次】」，不是「某一路 applied=2」：
// 锁是【条目粒度】的，A 应用 0001、B 应用 0100 这种交错是允许的终态（头注已写明不保证公平），
// 断言"某一路独揽全部条目"会把一个正确实现判成红。
//
// 🔵 本格用【全账本】口径（Ledger()＋MigrateEntries）：并发串行化是 runner 的性质，与条目属哪个域无关；
// 生产装配点经 Migrate 点名域（domains.go），域过滤另有本格（TestMigrate_DomainScoping…）。
func TestMigrate_ConcurrentRunnersAllSucceed(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	const n = 6
	pools := sameSchemaPools(t, dsn, n)

	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	if len(entries) != 17 {
		t.Fatalf("账本 %d 条（%v），本格按 17 条写断言（变了就改本格，别改断言数字糊过去）",
			len(entries), names(entries))
	}

	reps := make([]Report, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{}) // 起跑屏障：6 路真同时进去，不靠"碰巧撞上"
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			reps[i], errs[i] = MigrateEntries(ctx, pools[i], entries, nil)
		}(i)
	}
	close(start)
	wg.Wait()

	appliedCount := map[string]int{}
	for i, err := range errs {
		if err != nil {
			t.Errorf("第 %d 路并发迁移失败（并发启动不该互相拒启）: %v", i, err)
			continue
		}
		for _, name := range reps[i].Applied {
			appliedCount[name]++
		}
		for _, name := range reps[i].Applied {
			if appliedCount[name] > 1 {
				t.Errorf("条目 %s 被 %d 路都报成 applied —— 每条只该被应用一次", name, appliedCount[name])
			}
		}
	}
	for _, e := range entries {
		if appliedCount[e.Name] != 1 {
			t.Errorf("条目 %s 被应用了 %d 次，要恰好 1 次（各路 Applied=%v）", e.Name, appliedCount[e.Name], reps)
		}
	}

	// 终态：账 17 条、digest 与正文一致、身份域 CheckSchema 过（＝Hub 真起得来）。
	probe := pools[0]
	rows := ledgerRows(t, probe)
	if len(rows) != 17 {
		t.Fatalf("账上 %d 条，要 17 条", len(rows))
	}
	for _, e := range entries {
		if rows[e.Name].digest != Digest(e.Body) {
			t.Errorf("%s: 账内 digest 与正文不符", e.Name)
		}
	}
	if err := identity.NewPGStore(probe).CheckSchema(ctx); err != nil {
		t.Errorf("并发迁完 CheckSchema 仍失败: %v", err)
	}
	// 事后再迁一次：空转（不重复应用、不报错）。
	rep, err := MigrateEntries(ctx, probe, entries, nil)
	if err != nil {
		t.Fatalf("并发之后重跑 MigrateEntries: %v", err)
	}
	if len(rep.Applied) != 0 || len(rep.Skipped) != 17 {
		t.Errorf("重跑 Applied=%v Skipped=%v，要空转十七条", rep.Applied, rep.Skipped)
	}
}
