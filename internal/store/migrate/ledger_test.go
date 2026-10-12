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

	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/fanout"
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

	// 🔴 走【Hub 装配点】的口径（HubDomains()），不是全账本：本格钉的是「Hub 真会施加的那一份
	// 就够身份域起来」，而 fanout 的 0400 按裁定 R-G2-LEDGER-DOMAIN 不进 Hub 库。
	pool := testSchema(t, dsn)
	rep, err := Migrate(ctx, pool, HubDomains())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// 🔴 这份名单写死是有意的（与上面「缺表名单」同理）：它钉的是【Hub 域里到底有哪几条】，
	// 引 LedgerInDomains() 去比就变成自己比自己。加一条 Hub 域条目 ⇒ 改这里，红一次是应当的。
	if strings.Join(rep.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0300_console_transport_slot.sql" {
		t.Errorf("Applied = %v，要 Hub 域四条按字典序（0400 属 fanout 域，不得进 Hub 库）", rep.Applied)
	}
	if err := identity.NewPGStore(pool).CheckSchema(ctx); err != nil {
		t.Fatalf("跑完账本 CheckSchema 仍失败: %v", err)
	}
	// 🔴 反过来也要钉：CheckSchema 通过【不需要】fanout 的表。少了这半句，
	// 「Hub 域就够」与「其实还悄悄建了 fanout 五表」两种世界在本格里长得一样。
	for _, tbl := range []string{"outbound", "inbound_msg", "inbound_sink", "waive_audit", "selfcheck"} {
		if tableExists(t, pool, tbl) {
			t.Errorf("Hub 库里出现了 fanout 域的表 %s —— HubDomains() 不含 fanout（裁定 R-G2-LEDGER-DOMAIN）", tbl)
		}
	}
	// 账本自己也记了账（Hub 域四条，digest 与内嵌正文一致）。
	rows := ledgerRows(t, pool)
	entries, err := LedgerInDomains(HubDomains())
	if err != nil {
		t.Fatalf("LedgerInDomains: %v", err)
	}
	if len(rows) != len(entries) {
		t.Fatalf("账上 %d 条，Hub 域账本 %d 条", len(rows), len(entries))
	}
	for _, e := range entries {
		if rows[e.Name].digest != Digest(e.Body) {
			t.Errorf("%s: 账内 digest 与正文不符", e.Name)
		}
	}
	// 重启空转：再跑一次，什么都不动。
	rep2, err := Migrate(ctx, pool, HubDomains())
	if err != nil {
		t.Fatalf("重跑 Migrate: %v", err)
	}
	if len(rep2.Applied) != 0 || len(rep2.Skipped) != 5 {
		t.Errorf("重跑 Applied=%v Skipped=%v，要空转四条", rep2.Applied, rep2.Skipped)
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
	if _, err := Migrate(ctx, pool, HubDomains()); err != nil {
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
//	空库经【各域自己的幂等 SchemaDDL】建出的形状 ≡ 空库经【账本 Migrate】建出的形状
//	（Migrate 把 0001＋0100＋全部后续增量按字典序依次施加 ⇒ 就是「双轨指纹」那一句的全称）。
//
// 为什么必须有：账本条目是【冻结副本】，各域的 SchemaDDL 还在那儿继续改。两边漂移的后果正是
// 「代码改了、库里没改」——测试库（走 SchemaDDL）绿，部署库（走账本）行为不同。
// 有了这条对照，漂移在 CI 上就红，不靠人记。
// 🔴 比的是【形状】不是字节：将来身份域改 DDL 时，正确做法是【新增一条】增量条目（0101_…），
// 冻结的 0100 一个字都不改 —— 那时字节必然不同，而形状仍须相同。故本格不做字节比对。
//
// 🔴 本格是【源 ↔ 账本】漂移的唯一判据（组长 2026-10-02T01:31:16Z 改的口径）：
//
//	字节那一轨（下面 TestFrozenBodyMatchesSelfReportedSHA／TestAppliedEntriesAreImmutable）
//	只管「已施加的条目不可变」，🔴 不再管「冻结副本与源逐字相同」—— 那条判据撑不到有增量那天。
//	⇒ 源 pgschema.sql 加了一列而没有对应的增量条目，必须由本格红出来（反向对照见交付件 K6）。
func TestLedgerShapeMatchesDomainDDL(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()

	// 轨 A：各域自己的幂等 DDL（identity.PGSchemaSQL ＋ fanout.PGSchemaSQL）。
	// 身份域的 pgschema.sql 不建 audit_log（头注写明由账本保证先存在），故那 8 张只有 np_* ——
	// 这正是「core 的表归 core 的条目」的分工。
	// 🔴 张数写死是有意的（它是「账本漏建」的判据）：身份域 G3 终版 800f24d8 是 8 张
	// （原 6 张 ＋ 编组 np_group／np_group_member），fanout 是 5 张。任一域再增表 ⇒ 改这里，
	// 并在同一批里重冻对应基线或新增一条增量（口径页 §五）；只改一处就会让本格或双轨比对红，
	// 红比默默漂走好。
	// 🔴 新增一个域进账本时，本格的轨 A 必须【同时】加它的 SchemaDDL：只加条目不加轨 A，
	// 双轨比对会因为「B 多出几张表」而红（表集合那一句），这是设计好的 fail-closed，不是误报。
	// 🔴 施加顺序照号段（0100 identity → 0400 fanout），与账本同序：fanout 五表无外键指向
	// np_*／audit_log，顺序其实不影响结果，但让轨 A 与轨 B 同序，红的时候两边可逐条对照。
	trackA := testSchema(t, dsn)
	if _, err := trackA.Exec(ctx, testCtx(t), identity.PGSchemaSQL); err != nil {
		t.Fatalf("轨 A 建表失败（identity）: %v", err)
	}
	if _, err := trackA.Exec(ctx, testCtx(t), fanout.PGSchemaSQL); err != nil {
		t.Fatalf("轨 A 建表失败（fanout）: %v", err)
	}
	if _, err := trackA.Exec(ctx, testCtx(t), consolemux.PGSchemaSQL); err != nil {
		t.Fatalf("track A mux schema: %v", err)
	}
	aTables := baseTables(t, trackA, schemaOf(t, trackA))
	// 13 ＝ 身份域 8 ＋ fanout 域 5（outbound／inbound_msg／inbound_sink／waive_audit／selfcheck，
	// R-G2-FANOUT-HARDEN ① 把五表收口进 internal/fanout/pgschema.sql 后才有这一项）。
	if len(aTables) != 15 {
		t.Fatalf("轨 A 建出 %d 张表（%v），要 15 张＝身份域 9（G3 终版 800f24d8：原 6 张＋编组两张＋设备认领一张）"+
			"＋ mux 域 1（0300）＋ fanout 域 5（0400）", len(aTables), aTables)
	}

	// 轨 B：账本。🔴 本格用【全账本】（Ledger()＋MigrateEntries），不是某个装配点的域子集：
	// 它比的是「各域源 DDL ↔ 各自的冻结副本」两轨的形状，本来就跨域；按域取子集会让
	// fanout 那半边形不出来（轨 A 建了 13 张，轨 B 只建 10 张 ⇒ 表集合那一句红，却是本格自己造的红）。
	// 🔵 生产装配点没有「全账本」这个口径（Migrate 必须点名域）——本格是账本自身的性质，不是装配。
	all, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	trackB := testSchema(t, dsn)
	repB, err := MigrateEntries(ctx, trackB, all, nil)
	if err != nil {
		t.Fatalf("轨 B MigrateEntries: %v", err)
	}
	// 🔴 「全部后续增量依次施加」这半句要自己证，不能靠 runner 的实现替本格担保：
	// 将来漏挂一条增量（例如新文件没被 embed 进来）时，本格会比出形状差，却说不出是「少跑了一条」。
	// 把两个读数并排，红的时候能直接指到条目名。
	var wantApplied []string
	for _, e := range all {
		wantApplied = append(wantApplied, e.Name)
	}
	if strings.Join(repB.Applied, ",") != strings.Join(wantApplied, ",") {
		t.Fatalf("轨 B 只施加了 %v，而账本里是 %v —— 必须一条不落、按字典序依次施加", repB.Applied, wantApplied)
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
		t.Errorf("两轨形状不一致（各域 SchemaDDL 已走在账本前面）：修法是【新增一条增量条目】"+
			"（号段见口径页 §五），🔴 不是回去改已冻结的条目正文。\n"+
			"── 轨 A（各域 SchemaDDL）──\n%s\n── 轨 B（账本，只取同名表）──\n%s", a, b)
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

// ── 字节那一轨：判据是「已施加条目不可变」，🔴 不是「与源逐字相同」 ──────────────
//
// 🔴 口径改动（组长 2026-10-02T01:31:16Z）：这一轨原先钉的是「0100 的冻结正文与
// internal/identity/pgschema.sql 逐字相同」。那条判据【撑不到有增量那天】——
// 身份域改自己的 pgschema.sql（临时库用的幂等 DDL）之后，正确做法是新增 0101 写增量、
// 0100 一个字都不动；那一刻「逐字相同」必然不成立，而两边其实都是对的。
// 它还会把「源里只改了一个注释」判成红，逼人回去改一条【已经不可变】的冻结正文。
// ⇒ 现在两轨各判各的：源 ↔ 账本的漂移归形状轨（TestLedgerShapeMatchesDomainDDL），
//
//	字节轨只钉「已施加的条目不可变」，分两格：
//	· 不需库的一格钉【冻结正文没被手改过】（头注自报的 sha256 ＝ 冻结正文的 sha256）；
//	· 需库的一格钉【账实相符，且改一个字就被 runner 拒绝】。
//
// frozenBodyMarker 是 0100 里「头注到此为止」的那一行。🔴 它与条目文件里的那行必须逐字相同
// （含末尾那一串制表符样式的横线）；改标记行会让本格与重冻脚本两头都对不上，故这里写死。
const frozenBodyMarker = "-- ── 以下为冻结正文 ──────────────────────────────────────────────────────\n"

// frozenIdentityEntry／frozenFanoutEntry 是两条【冻结副本】条目的名字。写死而不按前缀找：
// 改名（＝改账本主键）本身就是一件必须红出来、必须人来确认的事。
// 🔴 0001_core_baseline.sql 【不在】这份名单里：它是手写的基线快照，不是某个源文件的逐字副本，
// 文件里没有 frozenBodyMarker 那一行（splitFrozen 会 Fatalf）。名单只收「冻结自某个源」的条目。
const (
	frozenIdentityEntry = "0100_identity_baseline.sql"
	frozenFanoutEntry   = "0400_fanout_baseline.sql"
)

// frozenEntry 一条冻结条目的自报坐标：源文件路径与冻结时的提交号。
// 🔴 提交号写死在这里而不是去问 git：本格不依赖仓状态（CI 上可能是浅克隆／tarball），
// 且「这条冻结自哪一版」必须是条目自己能被复核的事实，不是运行期查出来的。
type frozenEntry struct{ name, source, commit string }

var frozenEntries = []frozenEntry{
	{name: frozenIdentityEntry, source: "internal/identity/pgschema.sql", commit: "800f24d8"},
	// R-G2-FANOUT-HARDEN ①：fanout 五表收口进 pgschema.sql 的那一版（时间列已改 BIGINT）。
	{name: frozenFanoutEntry, source: "internal/fanout/pgschema.sql", commit: "0228e0d4"},
}

// splitFrozen 把一条冻结条目切成「头注（含标记行）」与「冻结正文」。
func splitFrozen(t *testing.T, body string) (head, frozen string) {
	t.Helper()
	i := strings.Index(body, frozenBodyMarker)
	if i < 0 {
		t.Fatalf("找不到冻结正文标记行 —— 头注或标记行被改动过；本格的判据依赖它定位正文起点")
	}
	return body[:i+len(frozenBodyMarker)], body[i+len(frozenBodyMarker):]
}

// TestFrozenBodyMatchesSelfReportedSHA 钉「冻结正文没被手改过」，🔴 不需要库。
//
// 头注那一行自报的是「冻结当时那份源文件的 sha256」，而冻结正文就是那份源的【逐字副本】
// ⇒ 同一个数也就是「冻结正文的 sha256」。本格按后一种读法用它，于是：
//
//	· 源前行（加了 0101 之后）⇒ 本格照绿（这正是旧判据做不到的）；
//	· 有人手改冻结正文一个注释字 ⇒ 本格红（旧 K5 那一刀的检出力留在 CI 上，
//	  不必等到部署那天由 runner 拒绝启动才被发现）。
//
// 🔴 顺带钉头注里的两处坐标（源文件路径、冻结时的提交号）：那两行字若不在，后来者就无法
// 自己复核「这条冻结自哪一份源」，而本格的那个数也就成了无来历的魔数。
func TestFrozenBodyMatchesSelfReportedSHA(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	byName := make(map[string]string, len(entries))
	for _, e := range entries {
		byName[e.Name] = e.Body
	}
	for _, fe := range frozenEntries {
		t.Run(fe.name, func(t *testing.T) {
			body := byName[fe.name]
			if body == "" {
				t.Fatalf("账本里没有 %s（重冻时改过文件名？号段规则见口径页 §五）", fe.name)
			}
			head, frozen := splitFrozen(t, body)

			want := sha256Hex(frozen)
			if !strings.Contains(head, want) {
				t.Errorf("头注自报的 sha256 与【冻结正文】的真值不符：真值 %s（头注里找不到）—— "+
					"要么正文被手改过（🔴 已施加条目不可变，要改就新增一条增量），要么那行坐标是装饰", want)
			}
			for _, coord := range []string{fe.source, fe.commit} {
				if !strings.Contains(head, coord) {
					t.Errorf("头注缺坐标 %q —— 后来者无法自己复核这条冻结自哪一份源", coord)
				}
			}
			// 🔴 本格【不引】各域的 PGSchemaSQL：这不是偷懒，是判据本身 ——
			// 冻结正文与当前的源是否逐字相同，从今往后不是任何一格的判据（理由见本节头注）。
			// 源与账本的漂移由形状轨判；本格的读数只来自条目文件自己。
		})
	}
	// 🔴 反向对照（名单本身也要被守）：账本里凡是带 frozenBodyMarker 的条目，都必须在
	// frozenEntries 名单里。否则「新增一条冻结条目而忘了登记坐标」会让本格静默少判一条 ——
	// 判据少一半就是没判（同 scripts/repo-boundary-gate.sh 头注 ①②「闸有几半」那条教训）。
	for _, e := range entries {
		if !strings.Contains(e.Body, frozenBodyMarker) {
			continue
		}
		found := false
		for _, fe := range frozenEntries {
			if fe.name == e.Name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("条目 %s 带冻结正文标记却不在 frozenEntries 名单里 —— "+
				"它的自报 sha256 与坐标没人核；补进名单（改名＝改账本主键，须人来确认）", e.Name)
		}
	}
}

// TestAppliedEntriesAreImmutable 钉字节轨的另一半：【账实相符，且改一个字就被拒】。
//
// 判据（组长 2026-10-02T01:31:16Z）：各条目 digest 与账本记录一致；改已施加条目一字 ⇒ 红。
// 🔴 与 migrate_test.go 的 TestMigrateEntries_RefusesChangedEntry 的分工：那一格用合成条目
// 钉 runner 的语义，本格用【真账本的真条目】钉同一件事 —— 合成条目对不上真文件时
// （例如 0100 的头注里就藏着不可改的坐标），只有本格能发现。
func TestAppliedEntriesAreImmutable(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	dsn := pgtest.DSN(t)
	pool := testSchema(t, dsn)
	ctx := context.Background()

	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	// 🔴 全账本口径（同双轨形状格的理由）：本格钉的是「任何已施加条目都不可变」，
	// 与条目属哪个域无关；域子集会让 fanout 那条不在场，于是它的不可变性没人守。
	if _, err := MigrateEntries(ctx, pool, entries, nil); err != nil {
		t.Fatalf("MigrateEntries: %v", err)
	}

	// ① 账实相符：账上每条 digest 与当前正文逐个相符（不是抽一条看看）。
	rows := ledgerRows(t, pool)
	if len(rows) != len(entries) {
		t.Fatalf("账上 %d 条，账本 %d 条", len(rows), len(entries))
	}
	for _, e := range entries {
		r, ok := rows[e.Name]
		if !ok {
			t.Errorf("%s 没记进账", e.Name)
			continue
		}
		if r.digest != Digest(e.Body) {
			t.Errorf("%s: 账内 digest %s ≠ 正文 digest %s", e.Name, r.digest, Digest(e.Body))
		}
	}

	// ② 改已施加条目【一个注释字】⇒ 拒绝。这一刀就是旧 K5（「表结构」→「表结枸」）：
	// 形状不变、digest 必变 ⇒ 形状轨看不见，只有字节轨能挡。
	idx := -1
	for i, e := range entries {
		if e.Name == frozenIdentityEntry {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("账本里没有 %s", frozenIdentityEntry)
	}
	const from, to = "表结构", "表结枸"
	if !strings.Contains(entries[idx].Body, from) {
		t.Fatalf("找不到要改的那处注释字 %q —— 变异打不上去，本格什么也没测", from)
	}
	tampered := make([]Entry, len(entries))
	copy(tampered, entries)
	tampered[idx].Body = strings.Replace(tampered[idx].Body, from, to, 1)

	rep, err := MigrateEntries(ctx, pool, tampered, nil)
	if err == nil {
		t.Fatalf("已施加条目被改了一个注释字却没拒绝 —— 库里是按旧内容迁的，继续跑等于拿新账认旧事实")
	}
	for _, want := range []string{frozenIdentityEntry, "已应用的迁移内容已变更", "拒绝启动",
		Digest(entries[idx].Body), Digest(tampered[idx].Body)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("拒绝文案缺 %q（两个 digest 必须都在，否则运维比不出变了什么）: %v", want, err)
		}
	}
	if len(rep.Applied) != 0 {
		t.Errorf("拒绝的那次竟然执行了 %v", rep.Applied)
	}
	// ③ 拒绝不得把账写脏：还是那三条、digest 还是原来那三个。
	after := ledgerRows(t, pool)
	if len(after) != len(rows) {
		t.Fatalf("账被写脏了：原 %d 条、现 %d 条", len(rows), len(after))
	}
	for name, r := range rows {
		if after[name].digest != r.digest {
			t.Errorf("%s 的账内 digest 被改动了（%s → %s）", name, r.digest, after[name].digest)
		}
	}
}

// sha256Hex 与 Digest 同算法，单列一个名字是为了让上面那格读起来是「冻结正文的 sha256」，
// 而不是「账本 digest」（两者恰好同算法，但语义不同：一个指头注自报的那份冻结内容，
// 一个指 runner 记账用的条目全文——含头注）。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
