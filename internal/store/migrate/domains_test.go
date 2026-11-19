package migrate

// 本文件钉 R-G2-LEDGER-DOMAIN（裁定 RULING-REGISTER 2026-10-02T11:06:25Z）在账本这一侧的四件事：
//
//	① 号段 → 域的映射按口径页 §五第二版逐段对得上，判不出域即拒（不给默认值）；
//	② Hub 侧只施加 Hub 域，🔴 不含 fanout 的 0400（否则下一次 Hub 部署把 fanout 五表建进生产 Hub 库）；
//	③ fanout 侧只施加 fanout 域（这样账本的 digest 保护真的到达 fanout 库）；
//	④ 库里已记着【别的域】的账 ⇒ 拒绝启动、一条 DDL 都不发（DSN 指错库时在动手之前出声）。
//
// 🔴 生产 Hub 库现状（只施加过 0001＋0100）那一格是【升级路径】：新代码跑上去必须是
// 「不报错、无新条目、表清单一字不动」——那才是"可以部署"的意思。

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// coreEntry 是 core 域基线条目的名字。🔴 写死（与 ledger_test.go 的 frozen* 两条同理）：
// 引 Ledger() 去挑就变成自己比自己，「账本里多了一条没人认领的」再也判不出来。
const coreEntry = "0001_core_baseline.sql"

// fanoutTables 是 0400 建的那五张表。🔴 写死而不引 fanout 包：本格要判的正是
// 「Hub 库里【不该】出现它们」，引过来就等于让被判的东西自己报名单。
var fanoutTables = []string{"outbound", "inbound_msg", "inbound_sink", "waive_audit", "selfcheck"}

// hubTables 抽三张 Hub 侧的表当探针（audit_log 属 core 0001，np_user／np_group 属 identity 0100）。
var hubTables = []string{"audit_log", "np_user", "np_group"}

func TestDomainOf_NumberRangeMapping(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	// 逐段对：每段的【下界、中间、上界】都要判对，只判一个代表值会漏掉边界写错（hi/lo 互换、差一）。
	ok := []struct {
		name string
		want Domain
	}{
		{"0001_core_baseline.sql", DomainCore},
		{"0050_core_something.sql", DomainCore},
		{"0099_core_last.sql", DomainCore},
		{frozenIdentityEntry, DomainIdentity},
		{"0150_identity_something.sql", DomainIdentity},
		{"0199_identity_last.sql", DomainIdentity},
		{"0200_hosted_baseline.sql", DomainHosted},
		{"0250_hosted_something.sql", DomainHosted},
		{"0299_hosted_last.sql", DomainHosted},
		{"0300_view_baseline.sql", DomainView},
		{"0350_view_something.sql", DomainView},
		{"0399_view_last.sql", DomainView},
		{frozenFanoutEntry, DomainFanout},
		{"0401_fanout_add_index.sql", DomainFanout},
		{"0450_fanout_something.sql", DomainFanout},
		{"0499_fanout_last.sql", DomainFanout},
		{"0900_cross_cleanup.sql", DomainCross},
		{"0950_cross_something.sql", DomainCross},
		{"9999_cross_last.sql", DomainCross},
	}
	for _, tc := range ok {
		got, err := DomainOf(tc.name)
		if err != nil {
			t.Errorf("DomainOf(%s) 报错: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DomainOf(%s) = %q，要 %q（号段→域映射见口径页 §五）", tc.name, got, tc.want)
		}
	}

	bad := []struct {
		name string
		want string // 拒绝文案里必须有的字
	}{
		{"0000_zero.sql", "未分配域"},
		{"0500_unassigned.sql", "未分配域"}, // 0500-0899 现在没有域：新域要先登记，不许悄悄落进既有域
		{"0899_unassigned.sql", "未分配域"},
		{"abcd_not_numbered.sql", "前四位不是数字"}, // 第五位是下划线、前四位不是数字 ⇒ 走到数字那一步
		{"1_x.sql", "四位号段"},
		{"0400fanout_missing_underscore.sql", "四位号段"},
		{"0400.sql", "四位号段"},
		{"", "四位号段"},
	}
	for _, tc := range bad {
		got, err := DomainOf(tc.name)
		if err == nil {
			t.Errorf("DomainOf(%q) = %q，竟然没拒 —— 判不出域还给个默认值，等于让写错号的条目悄悄进了某个库",
				tc.name, got)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("DomainOf(%q) 的拒绝文案缺 %q: %v", tc.name, tc.want, err)
		}
	}
	// 未分配号段那条还要指出【去哪儿登记】，否则运维只能翻代码。
	// 🔴 先判 err 再读文案：nil 上调 Error() 会 panic，把整包的读数（含上面那些 t.Errorf）一起带走 ——
	// 一个会 panic 的用例比一个会红的用例差，红的时候只剩一条"panic"看不出判据。
	_, err := DomainOf("0500_unassigned.sql")
	if err == nil {
		t.Fatal("未分配号段竟然没拒（上面那半段已报过读数，这里不再重复）")
	}
	for _, want := range []string{"口径页 §五", "domains.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("未分配号段的拒绝文案缺 %q: %v", want, err)
		}
	}
}

func TestLedgerInDomains_PartitionsRealLedger(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	// 🔴 名单写死：本格钉的是【真账本里现在有哪几条、各属哪个域】，引 Ledger() 去比就是自己比自己。
	hub := names(mustLedgerIn(t, HubDomains()))

	if strings.Join(hub, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0106_tempgroup.sql,0107_mcpgw_oauth.sql,0108_extseat_short_name.sql,0110_extseat_expect_reply.sql,0111_extseat_feishu_copy.sql,0112_np_agent_display_name.sql,0113_extseat_groups.sql,0114_agent_feishu_summary_copy.sql,0115_mcpgw_dcr.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
		t.Errorf("HubDomains 取到 %v，要 %s（core＋identity，🔴 不含 fanout 的 %s）",
			hub, coreEntry+","+frozenIdentityEntry, frozenFanoutEntry)
	}
	fan := names(mustLedgerIn(t, FanoutDomains()))

	if strings.Join(fan, ",") != frozenFanoutEntry {
		t.Errorf("FanoutDomains 取到 %v，要只有 %s", fan, frozenFanoutEntry)
	}

	// 两半互斥、且合起来恰好是全账本：没有「没人认领」的条目（那种条目在任何库上都不会被施加，
	// 于是它的表谁都不建 —— 静默漏建，比报错难查得多）。
	all := names(mustLedger(t))
	claim := map[string]Domain{}
	for _, d := range []Domain{DomainCore, DomainIdentity, DomainHosted, DomainView, DomainFanout, DomainCross} {
		for _, n := range names(mustLedgerIn(t, []Domain{d})) {
			if prev, dup := claim[n]; dup {
				t.Errorf("条目 %s 同时落进 %s 与 %s 两个域 —— 号段不得重叠", n, prev, d)
			}
			claim[n] = d
		}
	}
	for _, n := range all {

		if _, ok := claim[n]; !ok {
			t.Errorf("条目 %s 不属于任何已登记域 —— 它会被所有装配点跳过（静默漏建）", n)
		}
	}

	if len(claim) != len(all) {
		t.Errorf("按域取子集合起来 %d 条，全账本 %d 条", len(claim), len(all))
	}

	// cross 当前没有条目，仍允许显式查询并返回空集；hosted 与 view 的既有条目另按名单核对。
	for _, d := range []Domain{DomainCross} {
		if got := names(mustLedgerIn(t, []Domain{d})); len(got) != 0 {
			t.Errorf("域 %s 现在该是空的，却取到 %v", d, got)
		}
	}
	if got := names(mustLedgerIn(t, []Domain{DomainHosted})); strings.Join(got, ",") != "0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql" {
		t.Fatalf("hosted ledger ownership: %v", got)
	}
}

// TestMigrate_RejectsBadDomainSetBeforeTouchingDB 钉「域集合的校验先于任何库上动作」。
//
// 🔴 pool 传 nil：若校验发生在碰库之后，"传错域"就会先在库上留下痕迹（建了账表、甚至建了表）
// 才报错 —— 那时错误文案说的是域，库里多的东西却没人解释。
func TestMigrate_RejectsBadDomainSetBeforeTouchingDB(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	for _, tc := range []struct {
		name    string
		domains []Domain
		want    string
	}{
		{"空集合", nil, "域集合为空"},
		{"空切片", []Domain{}, "域集合为空"},
		{"未知域", []Domain{Domain("bogus")}, "未知域"},
		{"自造域名", []Domain{Domain("FANOUT")}, "未知域"}, // 大小写不同的同名域不是同一个域
		{"重复域", []Domain{DomainFanout, DomainFanout}, "出现两次"},
	} {
		rep, err := Migrate(context.Background(), nil, tc.domains)
		if err == nil {
			t.Errorf("%s: 竟然没拒（rep=%v）", tc.name, rep)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: 拒绝文案缺 %q: %v", tc.name, tc.want, err)
		}

		if len(rep.Applied) != 0 || len(rep.Skipped) != 0 {
			t.Errorf("%s: 拒了却带回了读数 %v", tc.name, rep)
		}
	}
	// LedgerInDomains 同一口径（它是 Migrate 的第一步，两处判据不得一宽一严）。
	for _, domains := range [][]Domain{nil, {}, {Domain("bogus")}, {DomainFanout, DomainFanout}} {

		if _, err := LedgerInDomains(domains); err == nil {
			t.Errorf("LedgerInDomains(%v) 竟然没拒", domains)
		}
	}
}

// TestMigrate_DomainScopingOnRealLedger 是②③④三件事在真账本＋真 PG 上的读数。
//
// 🔴 一个内嵌 PG、每格一个 schema（本包既有夹具口径）：一格一个库会把预算全花在起停上。
// 🔴 本格【不得】t.Parallel()（pgtest 的残留快照按本格起停前后判增量）。
func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	dsn := pgtest.DSN(t)
	ctx := context.Background()

	// ② Hub 侧：只施加 Hub 域，fanout 五表一张都不出现。
	// 🔴 反向对照（交付件 K1）：给 HubDomains() 加上 DomainFanout ⇒ 本格必红（Applied 多出 0400、
	// 五表出现在 Hub 库里）——那正是裁定要拦的形状。
	t.Run("Hub 侧只施加 Hub 域", func(t *testing.T) {
		pool := testSchema(t, dsn)
		rep, err := Migrate(ctx, pool, HubDomains())
		if err != nil {
			t.Fatalf("Migrate(HubDomains): %v", err)
		}

		if strings.Join(rep.Applied, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0106_tempgroup.sql,0107_mcpgw_oauth.sql,0108_extseat_short_name.sql,0110_extseat_expect_reply.sql,0111_extseat_feishu_copy.sql,0112_np_agent_display_name.sql,0113_extseat_groups.sql,0114_agent_feishu_summary_copy.sql,0115_mcpgw_dcr.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
			t.Errorf("Applied = %v，要 Hub 域迁移按字典序全部应用", rep.Applied)
		}
		for _, tbl := range append(hubTables, r31HostedTableNames()...) {
			if !tableExists(t, pool, tbl) {
				t.Errorf("Hub 库缺自己的表 %s", tbl)
			}
		}
		for _, tbl := range fanoutTables {
			if tableExists(t, pool, tbl) {
				t.Errorf("Hub 库里出现了 fanout 域的表 %s —— HubDomains() 不含 fanout（裁定 R-G2-LEDGER-DOMAIN）", tbl)
			}
		}
		rows := ledgerRows(t, pool)

		if len(rows) != 19 || rows["0103_agent_last_source_r31.sql"].digest == "" || rows["0200_hosted_r30_baseline.sql"].digest == "" || rows["0201_hosted_node_access_r31.sql"].digest == "" || rows["0102_device_claim.sql"].digest == "" || rows[coreEntry].digest == "" || rows[frozenIdentityEntry].digest == "" || rows["0101_group_communication.sql"].digest == "" || rows["0112_np_agent_display_name.sql"].digest == "" || rows["0113_extseat_groups.sql"].digest == "" {
			t.Errorf("账上读到 %v，要 Hub 域十九条（含 %s 与 %s）", keysOf(rows), coreEntry, frozenIdentityEntry)
		}

		if _, ok := rows[frozenFanoutEntry]; ok {
			t.Errorf("Hub 库的账上记了 %s —— 那条属 fanout 域", frozenFanoutEntry)
		}
	})

	// ③ fanout 侧：只施加 fanout 域。这样账本的 digest 保护才真的到达 fanout 库
	// （在此之前 fanout 库上根本没有 schema_migrations，谁改了表都没人知道）。
	// 🔴 反向对照（交付件 K2）：FanoutDomains() 改成返回 HubDomains() ⇒ 本格必红。
	t.Run("fanout 侧只施加 fanout 域", func(t *testing.T) {
		pool := testSchema(t, dsn)
		rep, err := Migrate(ctx, pool, FanoutDomains())
		if err != nil {
			t.Fatalf("Migrate(FanoutDomains): %v", err)
		}

		if strings.Join(rep.Applied, ",") != frozenFanoutEntry {
			t.Errorf("Applied = %v，要只有 %s", rep.Applied, frozenFanoutEntry)
		}
		for _, tbl := range fanoutTables {
			if !tableExists(t, pool, tbl) {
				t.Errorf("fanout 库缺自己的表 %s（0400 该建齐五张）", tbl)
			}
		}
		for _, tbl := range hubTables {
			if tableExists(t, pool, tbl) {
				t.Errorf("fanout 库里出现了 Hub 域的表 %s —— FanoutDomains() 只含 fanout", tbl)
			}
		}
		rows := ledgerRows(t, pool)

		if len(rows) != 1 || rows[frozenFanoutEntry].digest == "" {
			t.Errorf("账上读到 %v，要恰好 %s 一条", keysOf(rows), frozenFanoutEntry)
		}
		// 重启空转：再跑一次不动库（fanout 每次启动都会走这一遍）。
		rep2, err := Migrate(ctx, pool, FanoutDomains())
		if err != nil {
			t.Fatalf("重跑 Migrate(FanoutDomains): %v", err)
		}

		if len(rep2.Applied) != 0 || len(rep2.Skipped) != 1 {
			t.Errorf("重跑 Applied=%v Skipped=%v，要空转一条", rep2.Applied, rep2.Skipped)
		}
	})

	// ④ 既有 Hub（0001＋0100）升级：只应用列出的后续 Hub 迁移，既有表与账不重写。
	// 本格明确核准新增迁移范围；不允许新增其它域的表或改写旧账。
	t.Run("既有 Hub 升级应用全部后续 Hub 迁移", func(t *testing.T) {
		pool := testSchema(t, dsn)
		// 造出生产现状：只施加 0001＋0100（按名字从真账本里挑，不自己写 DDL）。
		prod := pickEntries(t, coreEntry, frozenIdentityEntry)

		if _, err := MigrateEntries(ctx, pool, prod, nil); err != nil {
			t.Fatalf("造生产 Hub 现状失败: %v", err)
		}
		schema := schemaOf(t, pool)
		before := baseTables(t, pool, schema)
		beforeRows := ledgerRows(t, pool)

		rep, err := Migrate(ctx, pool, HubDomains())
		if err != nil {
			t.Fatalf("在生产 Hub 现状上跑 Hub 侧 Migrate 竟然报错（部署会起不来）: %v", err)
		}

		if strings.Join(rep.Applied, ",") != "0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0106_tempgroup.sql,0107_mcpgw_oauth.sql,0108_extseat_short_name.sql,0110_extseat_expect_reply.sql,0111_extseat_feishu_copy.sql,0112_np_agent_display_name.sql,0113_extseat_groups.sql,0114_agent_feishu_summary_copy.sql,0115_mcpgw_dcr.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
			t.Errorf("Applied=%v, want all pending Hub migrations including session creation time", rep.Applied)
		}

		if strings.Join(rep.Skipped, ",") != coreEntry+","+frozenIdentityEntry {
			t.Errorf("Skipped = %v，要恰好那两条（空转）", rep.Skipped)
		}
		after := baseTables(t, pool, schema)
		// 0102 adds only the device-claim table, preserving every old table.
		filtered := make([]string, 0, len(after))
		for _, table := range after {
			if !strings.HasPrefix(table, "np_temp_group") && table != "mcpgw_oauth" && table != "np_device_claim" && table != "np_external_principal" && table != "np_extseat" && table != "np_extseat_token" && table != "np_extseat_inbox" && table != "np_extseat_outbox" && table != "np_extseat_send_key" && table != "np_extseat_feishu_copy" && table != "np_agent_feishu_copy_outbox" && table != "mcpgw_dcr_client" && table != "mcpgw_dcr_binding" && table != "mcpgw_dcr_bucket" && table != "console_transport_slot" && !strings.HasPrefix(table, "g4_") {
				filtered = append(filtered, table)
			}
		}

		if len(after) != len(before)+31 || strings.Join(before, ",") != strings.Join(filtered, ",") {
			t.Errorf("表清单动了：\n迁前 %v\n迁后 %v", before, after)
		}
		afterRows := ledgerRows(t, pool)

		if len(afterRows) != len(beforeRows)+17 {
			t.Errorf("账多了条目：迁前 %v、迁后 %v", keysOf(beforeRows), keysOf(afterRows))
		}
		for name, r := range beforeRows {
			if afterRows[name].digest != r.digest || !afterRows[name].appliedAt.Equal(r.appliedAt) {
				t.Errorf("条目 %s 的账被动过了（digest %s→%s，applied_at %v→%v）",
					name, r.digest, afterRows[name].digest, r.appliedAt, afterRows[name].appliedAt)
			}
		}
		for _, tbl := range fanoutTables {
			if tableExists(t, pool, tbl) {
				t.Errorf("生产 Hub 现状上跑出了 fanout 表 %s", tbl)
			}
		}
	})

	// ④ 指错库：fanout 库上跑 Hub 侧 Migrate ⇒ 拒绝，且【一条 DDL 都不发】。
	// 🔴 反向对照（交付件 K3）：去掉 migrateEntries 里的隔离核对 ⇒ 本格必红
	// （Hub 的三条会被施加进 fanout 库，两个服务共一本账，裁定取的乙就名存实亡）。
	t.Run("fanout 库上跑 Hub 侧 Migrate ⇒ 拒绝且不动库", func(t *testing.T) {
		pool := testSchema(t, dsn)

		if _, err := Migrate(ctx, pool, FanoutDomains()); err != nil {
			t.Fatalf("先造 fanout 库失败: %v", err)
		}
		schema := schemaOf(t, pool)
		before := baseTables(t, pool, schema)

		rep, err := Migrate(ctx, pool, HubDomains())
		if err == nil {
			t.Fatalf("竟然没拒（rep=%v）—— Hub 的条目会被施加进 fanout 的库", rep)
		}
		for _, want := range []string{frozenFanoutEntry, "fanout 域", "core", "identity",
			"拒绝启动", "不施加任何条目", "R-G2-LEDGER-DOMAIN"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("拒绝文案缺 %q（运维要能照文案判出是 DSN 指错了库）: %v", want, err)
			}
		}

		if len(rep.Applied) != 0 || len(rep.Skipped) != 0 {
			t.Errorf("拒了却带回读数 Applied=%v Skipped=%v", rep.Applied, rep.Skipped)
		}
		for _, tbl := range hubTables {
			if tableExists(t, pool, tbl) {
				t.Errorf("拒绝之后库里竟然多了 Hub 的表 %s —— 核对必须在第一条 DDL 之前", tbl)
			}
		}
		after := baseTables(t, pool, schema)

		if strings.Join(before, ",") != strings.Join(after, ",") {
			t.Errorf("表清单动了：\n拒前 %v\n拒后 %v", before, after)
		}
		rows := ledgerRows(t, pool)

		if len(rows) != 1 {
			t.Errorf("账上 %v，要还是 fanout 那一条（拒绝不得写账）", keysOf(rows))
		}
	})

	// ④ 反方向：Hub 库上跑 fanout 侧 Migrate ⇒ 同样拒（FANOUT_PG_DSN 指到 Hub 库就是这一格）。
	t.Run("Hub 库上跑 fanout 侧 Migrate ⇒ 拒绝且不动库", func(t *testing.T) {
		pool := testSchema(t, dsn)

		if _, err := Migrate(ctx, pool, HubDomains()); err != nil {
			t.Fatalf("先造 Hub 库失败: %v", err)
		}
		before := ledgerRows(t, pool)

		rep, err := Migrate(ctx, pool, FanoutDomains())
		if err == nil {
			t.Fatalf("竟然没拒（rep=%v）—— fanout 的五表会被建进 Hub 库", rep)
		}
		for _, want := range []string{coreEntry, frozenIdentityEntry, "core 域", "identity 域", "fanout"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("拒绝文案缺 %q: %v", want, err)
			}
		}
		for _, tbl := range fanoutTables {
			if tableExists(t, pool, tbl) {
				t.Errorf("拒绝之后库里竟然多了 fanout 表 %s", tbl)
			}
		}
		after := ledgerRows(t, pool)

		if len(after) != len(before) {
			t.Errorf("账被动了：拒前 %v、拒后 %v", keysOf(before), keysOf(after))
		}
	})
}

// mustLedgerIn / mustLedger 取条目（失败即 Fatal）。
func mustLedgerIn(t *testing.T, domains []Domain) []Entry {
	t.Helper()
	entries, err := LedgerInDomains(domains)
	if err != nil {
		t.Fatalf("LedgerInDomains(%v): %v", domains, err)
	}
	return entries
}

func mustLedger(t *testing.T) []Entry {
	t.Helper()
	entries, err := Ledger()
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	return entries
}

// pickEntries 按名字从真账本里挑条目（挑不到即 Fatal：名字写错了本格什么也没测）。
func pickEntries(t *testing.T, want ...string) []Entry {
	t.Helper()
	all := mustLedger(t)
	byName := map[string]Entry{}
	for _, e := range all {
		byName[e.Name] = e
	}
	out := make([]Entry, 0, len(want))
	for _, n := range want {
		e, ok := byName[n]
		if !ok {
			t.Fatalf("真账本里没有条目 %s（有：%v）", n, names(all))
		}
		out = append(out, e)
	}
	return out
}

func keysOf(rows map[string]ledgerRow) []string {
	out := make([]string, 0, len(rows))
	for k := range rows {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
