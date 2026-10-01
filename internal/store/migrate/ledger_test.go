package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 本文件钉的是【账本真的能撑起第一版部署】这一串事实，而不是 runner 的内部语义
// （那在 migrate_test.go）。三件事：
//
//	① 空库跑一遍 Migrate ⇒ 身份域的 CheckSchema 通过（Hub 与 bootstrap-admin 起得来的前提）
//	② 账本建出的 audit_log 收得下身份域的审计写入（复验 🟡④ 的回归：形状不对时是【静默全丢】）
//	③ 双轨一致（口径页 §六）：空库经各域 SchemaDDL 建出的形状 ≡ 空库经账本 Migrate 建出的形状

func TestMigrate_RealLedgerSatisfiesIdentityCheckSchema(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()

	// 反向对照：不跑账本，CheckSchema 必须报缺表（否则①的「通过」说明不了任何事）。
	empty := testSchema(t, dsn)
	if err := identity.NewPGStore(empty).CheckSchema(ctx); err == nil {
		t.Fatalf("空库上 CheckSchema 竟然通过 —— 那道「缺表即拒」的判据是死的")
	} else {
		// 🔴 这张名单＝identity.pgRequiredTables 的逐字副本（G3 终版 800f24d8；编组两张表自 41feec50 起）。
		// 它【故意】写死而不引那个变量：引了就会跟着一起变，于是「账本漏建一张表」再也判不出来。
		// 代价是 G3 增表时要同步改这里 —— 漂移由下面那条双轨指纹格与 CheckSchema 一起兜住。
		for _, tbl := range []string{"np_user", "np_identity", "np_registration_code", "np_session", "np_agent", "np_enroll_application", "np_group", "np_group_member", "audit_log"} {
			if !strings.Contains(err.Error(), tbl) {
				t.Errorf("缺表错误没点出 %s: %v", tbl, err)
			}
		}
	}

	pool := testSchema(t, dsn)
	rep, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if strings.Join(rep.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql" {
		t.Errorf("Applied = %v，要两条按字典序", rep.Applied)
	}
	if err := identity.NewPGStore(pool).CheckSchema(ctx); err != nil {
		t.Fatalf("跑完账本 CheckSchema 仍失败: %v", err)
	}
	// 账本自己也记了账（两条，digest 与内嵌正文一致）。
	rows := ledgerRows(t, pool)
	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	if len(rows) != len(entries) {
		t.Fatalf("账上 %d 条，账本 %d 条", len(rows), len(entries))
	}
	for _, e := range entries {
		if rows[e.Name].digest != Digest(e.Body) {
			t.Errorf("%s: 账内 digest 与正文不符", e.Name)
		}
	}
	// 重启空转：再跑一次，什么都不动。
	rep2, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("重跑 Migrate: %v", err)
	}
	if len(rep2.Applied) != 0 || len(rep2.Skipped) != 2 {
		t.Errorf("重跑 Applied=%v Skipped=%v，要空转两条", rep2.Applied, rep2.Skipped)
	}
}

// TestMigrate_AuditLogAcceptsIdentityWrites 是复验 🟡④ 的回归格。
//
// 原缺陷链（本席探针实跑过）：SQLite 侧 0001 的 audit_log 是 `id TEXT PRIMARY KEY`（无默认）＋
// `ts TEXT NOT NULL`，而身份域的 AppendAudit 不给 id、ts 传 time.Time，且 service.go 把
// 审计错误丢弃 ⇒ 直译那张表到 PG 的后果是【审计静默全丢】，启动照样成功。
// 故本格不用「表建出来了」充数，而是走身份域真实的写入路径，再把行读回来逐字段核。
func TestMigrate_AuditLogAcceptsIdentityWrites(t *testing.T) {
	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	at := time.Date(2026, 10, 1, 15, 30, 0, 0, time.UTC)
	store := identity.NewPGStore(pool)
	if err := store.AppendAudit(ctx, identity.AuditEvent{
		Actor: "boss", Action: "agent.revoke", Target: "agt_1", Detail: "reason=test", At: at,
	}); err != nil {
		t.Fatalf("AppendAudit 失败（正是 🟡④ 的形状冲突）: %v", err)
	}

	var (
		id                int64
		actor, action     string
		target, afterJSON string
		ts                time.Time
	)
	row, err := pool.QueryRow(ctx, testCtx(t),
		`SELECT id, actor, action, target, after_json, ts FROM audit_log`)
	if err != nil {
		t.Fatalf("查审计行失败: %v", err)
	}
	if err := row.Scan(&id, &actor, &action, &target, &afterJSON, &ts); err != nil {
		t.Fatalf("扫描审计行失败: %v", err)
	}
	if id == 0 {
		t.Errorf("id 没有默认值生成（读到 %d）—— 写入方不给 id，靠的就是 bigserial 默认", id)
	}
	if actor != "boss" || action != "agent.revoke" || target != "agt_1" {
		t.Errorf("审计字段串位: actor=%q action=%q target=%q", actor, action, target)
	}
	if !ts.Equal(at) {
		t.Errorf("ts = %v，要 %v（timestamptz 才收得下 time.Time）", ts, at)
	}
	var detail map[string]string
	if err := json.Unmarshal([]byte(afterJSON), &detail); err != nil {
		t.Fatalf("after_json 不是 JSON: %v（%q）", err, afterJSON)
	}
	if detail["detail"] != "reason=test" {
		t.Errorf("after_json.detail = %q", detail["detail"])
	}

	// action 保持 NOT NULL（与 SQLite 侧 0001_init.sql:207 一致）：写入方恒传字符串，
	// 但「传了 NULL」必须被库挡住，不能悄悄变成一条没有动作的审计。
	if _, err := pool.Exec(ctx, testCtx(t),
		`INSERT INTO audit_log(actor, action, ts) VALUES($1, NULL, $2)`, "x", at); err == nil {
		t.Errorf("action 可以是 NULL —— 与 SQLite 侧的 NOT NULL 不一致")
	}
}

// TestLedgerShapeMatchesDomainDDL 是口径页 §六 的双轨一致判据：
//
//	空库经【各域自己的幂等 SchemaDDL】建出的形状 ≡ 空库经【账本 Migrate】建出的形状。
//
// 为什么必须有：账本条目是【冻结副本】，各域的 SchemaDDL 还在那儿继续改。两边漂移的后果正是
// 「代码改了、库里没改」——测试库（走 SchemaDDL）绿，部署库（走账本）行为不同。
// 有了这条对照，漂移在 CI 上就红，不靠人记。
// 🔴 比的是【形状】不是字节：将来身份域改 DDL 时，正确做法是【新增一条】增量条目（0101_…），
// 冻结的 0100 一个字都不改 —— 那时字节必然不同，而形状仍须相同。故本格不做字节比对。
func TestLedgerShapeMatchesDomainDDL(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()

	// 轨 A：各域自己的幂等 DDL。身份域的 pgschema.sql 不建 audit_log（头注写明由账本保证先存在），
	// 故这一轨只有 np_* 那几张 —— 这正是「core 的表归 core 的条目」的分工。
	// 🔴 张数写死是有意的（它是「账本漏建」的判据）：G3 终版 800f24d8 是 8 张
	// （原 6 张 ＋ 编组 np_group／np_group_member）。身份域再增表 ⇒ 改这里，并在同一批里
	// 重冻 0100 或新增 0101（口径页 §五）；只改一处就会让本格或双轨比对红，红比默默漂走好。
	trackA := testSchema(t, dsn)
	if _, err := trackA.Exec(ctx, testCtx(t), identity.PGSchemaSQL); err != nil {
		t.Fatalf("轨 A 建表失败: %v", err)
	}
	aTables := baseTables(t, trackA, schemaOf(t, trackA))
	if len(aTables) != 8 {
		t.Fatalf("轨 A 建出 %d 张表（%v），要身份域那 8 张（G3 终版 800f24d8：原 6 张＋编组两张）", len(aTables), aTables)
	}

	// 轨 B：账本。
	trackB := testSchema(t, dsn)
	if _, err := Migrate(ctx, trackB); err != nil {
		t.Fatalf("轨 B Migrate: %v", err)
	}
	bTables := baseTables(t, trackB, schemaOf(t, trackB))

	// 表集合：B ＝ A ∪ {audit_log, schema_migrations}。多出来的两张都得有来历，
	// 少一张就是账本漏建（那正是「Hub 起不来」的成因）。
	want := append(append([]string{}, aTables...), "audit_log", "schema_migrations")
	sort.Strings(want)
	if strings.Join(bTables, ",") != strings.Join(want, ",") {
		t.Fatalf("轨 B 的表集合 = %v，要 %v", bTables, want)
	}

	only := map[string]bool{}
	for _, tb := range aTables {
		only[tb] = true
	}
	a := shapeDump(t, trackA, schemaOf(t, trackA), nil)
	b := shapeDump(t, trackB, schemaOf(t, trackB), only)
	if a != b {
		t.Errorf("两轨形状不一致（冻结副本已与各域 DDL 漂移）:\n── 轨 A（各域 SchemaDDL）──\n%s\n── 轨 B（账本，只取同名表）──\n%s", a, b)
	}
}

// schemaOf 取池连的是哪个 schema（用例里每个池一个独立 schema）。
func schemaOf(t *testing.T, pool *repo.Pool) string {
	t.Helper()
	row, err := pool.QueryRow(context.Background(), testCtx(t), `SELECT current_schema()`)
	if err != nil {
		t.Fatalf("查 current_schema 失败: %v", err)
	}
	var s string
	if err := row.Scan(&s); err != nil {
		t.Fatalf("扫描 current_schema 失败: %v", err)
	}
	return s
}

// baseTables 该 schema 里的普通表（序列、视图不算）。
func baseTables(t *testing.T, pool *repo.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), testCtx(t),
		`SELECT table_name FROM information_schema.tables
		  WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		t.Fatalf("查表清单失败: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("扫描表名失败: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历表清单失败: %v", err)
	}
	return out
}

// shapeDump 把一个 schema 的【形状】规范化成可比的文本：列（类型/可空/默认）＋索引＋约束。
//
// only 非 nil 时只取名单里的表（用来把轨 B 裁到与轨 A 同一组表）。
// 🔴 默认值与索引定义里会带 schema 名（serial 的 nextval('s.seq')、indexdef 的 ON s.t），
// 两轨的 schema 名不同 ⇒ 原样比会【恒不一致】，把真漂移淹掉。故统一替换成占位符再比。
func shapeDump(t *testing.T, pool *repo.Pool, schema string, only map[string]bool) string {
	t.Helper()
	ctx := context.Background()
	tc := testCtx(t)
	norm := func(s string) string { return strings.ReplaceAll(s, schema, "@SCH@") }

	var lines []string
	collect := func(prefix, query string, args ...any) {
		rows, err := pool.Query(ctx, tc, query, args...)
		if err != nil {
			t.Fatalf("取形状失败（%s）: %v", prefix, err)
		}
		defer rows.Close()
		for rows.Next() {
			var tbl, line string
			if err := rows.Scan(&tbl, &line); err != nil {
				t.Fatalf("扫描形状失败（%s）: %v", prefix, err)
			}
			if only != nil && !only[tbl] {
				continue
			}
			lines = append(lines, prefix+"|"+norm(line))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("遍历形状失败（%s）: %v", prefix, err)
		}
	}

	collect("C", `SELECT table_name,
	        column_name||' '||data_type||' null='||is_nullable||' default='||coalesce(column_default,'-')
	      FROM information_schema.columns WHERE table_schema = $1 ORDER BY table_name, ordinal_position`, schema)
	collect("I", `SELECT tablename, indexname||' '||indexdef
	      FROM pg_indexes WHERE schemaname = $1 ORDER BY tablename, indexname`, schema)
	collect("K", `SELECT tc.table_name,
	        tc.constraint_name||' '||tc.constraint_type||' '||coalesce(cc.check_clause,'-')
	      FROM information_schema.table_constraints tc
	      LEFT JOIN information_schema.check_constraints cc
	             ON cc.constraint_schema = tc.constraint_schema
	            AND cc.constraint_name  = tc.constraint_name
	     WHERE tc.table_schema = $1 ORDER BY tc.table_name, tc.constraint_name`, schema)

	if len(lines) == 0 {
		t.Fatalf("形状取空了（schema=%s）—— 查询本身坏了，不是「两轨都为空所以相等」", schema)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// ── 冻结副本的字节身份（不需库） ───────────────────────────────────────────────
//
// frozenBodyMarker 是 0100 里「头注到此为止」的那一行。🔴 它与条目文件里的那行必须逐字相同
// （含末尾那一串制表符样式的横线）；改标记行会让本格与重冻脚本两头都对不上，故这里写死。
const frozenBodyMarker = "-- ── 以下为冻结正文 ──────────────────────────────────────────────────────\n"

// TestFrozenIdentityCopyEqualsSource 钉「0100 是 internal/identity/pgschema.sql 的【逐字】副本」。
//
// 🔴 为什么要有这一格（它下面那条双轨指纹格管不了这件事）：
//
//	双轨比的是【形状】，故意不比字节 —— 因为将来正确的增量做法是新增 0101，那时字节必然不同。
//	但"重冻"这一步是【字节级】动作：手改过正文、或源已变而副本没跟上，形状可能仍然一致
//	（例如只改了注释、或改了源里一段还没建表的部分），于是漂移会一直藏到部署那天。
//	⇒ 两格分工：本格判「副本＝当次冻结的那份源」，双轨格判「账本建出的形状＝各域 DDL 建出的形状」。
//
// 🔴 顺带钉头注里自报的源 sha256：那行字若不与真值一致，它就是装饰，而读它的人会拿它当坐标。
func TestFrozenIdentityCopyEqualsSource(t *testing.T) {
	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	const name = "0100_identity_baseline.sql"
	var body string
	for _, e := range entries {
		if e.Name == name {
			body = e.Body
		}
	}
	if body == "" {
		t.Fatalf("账本里没有 %s（重冻时改过文件名？号段规则见口径页 §五）", name)
	}

	i := strings.Index(body, frozenBodyMarker)
	if i < 0 {
		t.Fatalf("找不到冻结正文标记行 —— 头注或标记行被改动过；本格的判据依赖它定位正文起点")
	}
	head, got := body[:i+len(frozenBodyMarker)], body[i+len(frozenBodyMarker):]
	if got != identity.PGSchemaSQL {
		t.Errorf("冻结副本与 internal/identity/pgschema.sql 不逐字相同（副本 %d 字节、源 %d 字节）—— "+
			"重冻时手改过正文，或源已变而副本没跟上（源变 ⇒ 新增 0101，不改本条）", len(got), len(identity.PGSchemaSQL))
	}

	wantSHA := sha256Hex(identity.PGSchemaSQL)
	if !strings.Contains(head, wantSHA) {
		t.Errorf("头注自报的源 sha256 与真值不符：真值 %s（头注里找不到）—— 那行坐标是假的", wantSHA)
	}
	if !strings.Contains(head, "internal/identity/pgschema.sql") {
		t.Error("头注没写明冻结自哪个文件 —— 后来者无法自己复核这一格")
	}
}

// sha256Hex 与 Digest 同算法，单列一个名字是为了让上面那格读起来是「源文件的 sha256」，
// 而不是「账本 digest」（两者恰好同算法，但语义不同：一个指文件，一个指条目正文）。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
