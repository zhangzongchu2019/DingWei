package migrate

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 0108 is older than the deployed 0110/0111/0112/0113 numbers, but the ledger is a set of
// applied entries, not a high-water mark. Existing entries must not replay.
func TestConsent0108AppliesAfter0110And0111OnlyOnce(t *testing.T) {
	ctx := context.Background()
	p := testSchema(t, pgtest.DSN(t))
	entries, err := LedgerInDomains(HubDomains())
	if err != nil {
		t.Fatal(err)
	}
	var deployed []Entry
	for _, entry := range entries {
		if entry.Name != "0108_extseat_short_name.sql" {
			deployed = append(deployed, entry)
		}
	}

	if _, err = MigrateEntries(ctx, p, deployed, nil); err != nil {
		t.Fatal(err)
	}
	before := ledgerRows(t, p)

	if len(before) != 18 || before["0110_extseat_expect_reply.sql"].digest == "" || before["0111_extseat_feishu_copy.sql"].digest == "" || before["0112_np_agent_display_name.sql"].digest == "" || before["0113_extseat_groups.sql"].digest == "" || before["0114_agent_feishu_summary_copy.sql"].digest == "" || before["0115_mcpgw_dcr.sql"].digest == "" {
		t.Fatalf("R57_LATE_0108_DEPLOYED_BASELINE %v", keysOf(before))
	}
	report, err := Migrate(ctx, p, HubDomains())
	if err != nil || strings.Join(report.Applied, ",") != "0108_extseat_short_name.sql" || len(report.Skipped) != 18 {
		t.Fatalf("R57_LATE_0108_ONLY_PENDING report=%+v err=%v", report, err)
	}
	after := ledgerRows(t, p)

	if len(after) != 19 || after["0108_extseat_short_name.sql"].digest == "" {
		t.Fatalf("R57_LATE_0108_LEDGER %v", keysOf(after))
	}
	for name, old := range before {
		got := after[name]
		if got.digest != old.digest || !got.appliedAt.Equal(old.appliedAt) {
			t.Fatalf("R57_LATE_0108_REPLAYED_EXISTING %s", name)
		}
	}
	repeated, err := Migrate(ctx, p, HubDomains())
	if err != nil || len(repeated.Applied) != 0 || len(repeated.Skipped) != 19 {
		t.Fatalf("R57_LATE_0108_RESTART report=%+v err=%v", repeated, err)
	}
}

func TestExternalDisplayNameMigrationBackfillsWithoutChangingAddress(t *testing.T) {
	ctx := context.Background()
	p := testSchema(t, pgtest.DSN(t))
	entries, err := LedgerInDomains(HubDomains())
	if err != nil {
		t.Fatal(err)
	}
	var beforeDisplayName []Entry
	var displayNameEntry Entry
	for _, entry := range entries {
		if entry.Name == "0112_np_agent_display_name.sql" {
			displayNameEntry = entry
			continue
		}
		beforeDisplayName = append(beforeDisplayName, entry)
	}
	if displayNameEntry.Name == "" {
		t.Fatal("0112 display-name migration missing from Hub ledger")
	}

	if _, err = MigrateEntries(ctx, p, beforeDisplayName, nil); err != nil {
		t.Fatal(err)
	}
	ac := testCtx(t)
	_, err = p.Exec(ctx, ac, `
INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at)
VALUES('owner','owner','own','own','member','active',now());
INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at)
VALUES('ext-with-name','owner','alice_AG7','alice_ag7','external','active',now()),
      ('ext-without-name','owner','bob_AG8','bob_ag8','external','active',now()),
      ('ext-overlong','owner','carol_AG9','carol_ag9','external','active',now());
INSERT INTO np_external_principal(id,provider,tenant_key,issuer,subject,display_name,owner_lc,created_at)
VALUES('p1','feishu','tenant','app','subject-1','张伟','owner',now()),
      ('p2','feishu','tenant','app','subject-2','','owner',now()),
      ('p3','feishu','tenant','app','subject-3',repeat('x',321),'owner',now());
INSERT INTO np_extseat(agent_id,principal_id,short_name,ag_code,ag_generation,created_at)
VALUES('ext-with-name','p1','alice','AG7',1,now()),('ext-without-name','p2','bob','AG8',1,now()),('ext-overlong','p3','carol','AG9',1,now());`)
	if err != nil {
		t.Fatal(err)
	}
	var principalName string
	principalRow, err := p.QueryRow(ctx, ac, `SELECT display_name FROM np_external_principal WHERE id='p1'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := principalRow.Scan(&principalName); err != nil || principalName != "张伟" {
		t.Fatalf("migration fixture principal display_name=%q err=%v", principalName, err)
	}

	if _, err = MigrateEntries(ctx, p, []Entry{displayNameEntry}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := p.Query(ctx, ac, `SELECT id,name,COALESCE(display_name,'') FROM np_agent ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := [][3]string{{"ext-overlong", "carol_AG9", ""}, {"ext-with-name", "alice_AG7", "张伟"}, {"ext-without-name", "bob_AG8", ""}}
	for _, expected := range want {
		if !rows.Next() {
			t.Fatalf("missing expected backfill row %v", expected)
		}
		var got [3]string
		if err := rows.Scan(&got[0], &got[1], &got[2]); err != nil {
			t.Fatal(err)
		}
		if got != expected {
			t.Fatalf("EXTSEAT_DISPLAY_BACKFILL_ADDRESS_INVARIANT row=%v want=%v", got, expected)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected extra agent row")
	}
}

func TestConsentMigrationLedgerPG(t *testing.T) {
	ctx := context.Background()
	p := testSchema(t, pgtest.DSN(t))
	ac := testCtx(t)
	entries, e := LedgerInDomains(HubDomains())
	if e != nil {
		t.Fatal(e)
	}
	var previous []Entry
	for _, entry := range entries {
		if entry.Name != "0108_extseat_short_name.sql" {
			previous = append(previous, entry)
		}
	}

	if _, e = MigrateEntries(ctx, p, previous, nil); e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, ac, `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at) VALUES('own','own','ow','ow','member','active',now()); INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('ext','own','bot_AG1','bot_ag1','external','active',now()); INSERT INTO np_external_principal(id,provider,tenant_key,issuer,subject,display_name,owner_lc,created_at) VALUES('p','feishu','t','app','s','name','own',now()); INSERT INTO np_extseat(agent_id,principal_id,short_name,ag_code,ag_generation,created_at) VALUES('ext','p','bot','AG1',1,now())`)
	if e != nil {
		t.Fatal(e)
	}

	if _, e = p.Exec(ctx, ac, `UPDATE np_extseat SET short_name='abcdefgh261109060000'`); e == nil {
		t.Fatal("ZC_LEDGER_OLD_CHECK")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := Migrate(ctx, p, HubDomains()); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	row, e := p.QueryRow(ctx, ac, `SELECT short_name FROM np_extseat WHERE agent_id='ext'`)
	if e != nil {
		t.Fatal(e)
	}
	var name string
	if e = row.Scan(&name); e != nil || name != "bot" {
		t.Fatalf("ZC_LEDGER_OLD_ROW %s %v", name, e)
	}

	if _, e = p.Exec(ctx, ac, `UPDATE np_extseat SET short_name='abcdefgh261109060000'`); e != nil {
		t.Fatalf("ZC_LEDGER_LONG_REJECTED %v", e)
	}

	if _, e = p.Exec(ctx, ac, `UPDATE np_extseat SET short_name='abcdefghijklmnopqrstu'`); e == nil {
		t.Fatal("ZC_LEDGER_OVER20_ACCEPTED")
	}
	rep, e := Migrate(ctx, p, HubDomains())
	if e != nil || len(rep.Applied) != 0 {
		t.Fatalf("ZC_LEDGER_REPLAY %v %+v", e, rep)
	}
}
