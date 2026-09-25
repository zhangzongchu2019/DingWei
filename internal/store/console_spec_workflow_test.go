package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestMigration0044PreservesEveryLegacyStateAndDropsClaimedBy(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.db.ExecContext(ctx, `CREATE TABLE console_principal(id TEXT PRIMARY KEY,provider TEXT,provider_issuer TEXT,provider_subject TEXT,display_name TEXT NOT NULL DEFAULT '',active INTEGER);CREATE TABLE console_spec(id TEXT PRIMARY KEY,principal_id TEXT NOT NULL,title TEXT NOT NULL,state TEXT NOT NULL,current_revision INTEGER NOT NULL,claimed_by TEXT,edit_capability_hash TEXT NOT NULL DEFAULT '',editing_closed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `INSERT INTO console_principal VALUES('owner','f','i','s','甲',1)`); err != nil {
		t.Fatal(err)
	}
	states := []string{"draft", "hard_gate", "human_review", "needs_improvement", "claimable", "in_progress", "archived"}
	for i, state := range states {
		if _, err = db.db.ExecContext(ctx, `INSERT INTO console_spec VALUES(?,?,?, ?,1,'legacy','','0',?,?)`, fmt.Sprintf("s%d", i), "owner", state, state, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	body, err := migrationFS.ReadFile("migrations/0044_console_spec_workflow.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err = db.preflightConsolePrincipalDisplayNames(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.applyMigration(ctx, "0044_console_spec_workflow.sql", string(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_spec`).Scan(&count); err != nil || count != 7 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
	rows, err := db.db.QueryContext(ctx, `PRAGMA table_info(console_spec)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "claimed_by" {
			t.Fatal("claimed_by survived migration")
		}
	}
	var archived int
	if err = db.db.QueryRowContext(ctx, `SELECT archived FROM console_spec WHERE id='s6'`).Scan(&archived); err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	expected := map[string]string{
		"draft": "editing", "needs_improvement": "editing",
		"hard_gate": "review", "human_review": "review",
		"claimable": "final", "in_progress": "final", "archived": "final",
	}
	for i, legacy := range states {
		var state, gotLegacy string
		if err = db.db.QueryRowContext(ctx, `SELECT state,legacy_state FROM console_spec WHERE id=?`, fmt.Sprintf("s%d", i)).Scan(&state, &gotLegacy); err != nil {
			t.Fatal(err)
		}
		if state != expected[legacy] || gotLegacy != legacy {
			t.Fatalf("legacy=%s state=%s recorded=%s", legacy, state, gotLegacy)
		}
		if legacy == "claimable" || legacy == "in_progress" || legacy == "archived" {
			revision := model.ConsoleSpecRevision{SpecID: fmt.Sprintf("s%d", i), Revision: 2, CreatedBy: "owner"}
			if _, writeErr := db.AppendConsoleSpecRevision(ctx, "owner", revision.SpecID, 1, revision, time.Now()); !errors.Is(writeErr, ErrConsoleSpecFinal) {
				t.Fatalf("legacy final state %s remained writable: %v", legacy, writeErr)
			}
		}
	}
}

func TestMigration0044RewritesOnlyLegacyOwnerDefaultsDeterministically(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "legacy-owner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.db.ExecContext(ctx, `
		CREATE TABLE console_principal(id TEXT PRIMARY KEY,provider TEXT,provider_issuer TEXT,provider_subject TEXT,display_name TEXT NOT NULL,active INTEGER);
		CREATE TABLE console_principal_role(principal_id TEXT,role TEXT,owner_key TEXT,active INTEGER);
		CREATE TABLE console_spec(id TEXT PRIMARY KEY,principal_id TEXT NOT NULL,title TEXT NOT NULL,state TEXT NOT NULL,current_revision INTEGER NOT NULL,claimed_by TEXT,edit_capability_hash TEXT NOT NULL DEFAULT '',editing_closed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
		INSERT INTO console_principal VALUES('b','f','i','b','Owner',0),('a','f','i','a','Owner',1),('custom','f','i','c','业主自起名',1);
		INSERT INTO console_principal_role VALUES('a','owner','team',1),('b','owner','team',1),('custom','owner','other',1);
		INSERT INTO console_spec VALUES('s','a','s','draft',1,NULL,'',0,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	body, err := migrationFS.ReadFile("migrations/0044_console_spec_workflow.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err = db.applyConsoleSpecWorkflowMigration(ctx, "0044_console_spec_workflow.sql", string(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	rows, err := db.db.QueryContext(ctx, `SELECT id,display_name FROM console_principal ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			t.Fatal(err)
		}
		got[id] = name
	}
	want := map[string]string{"a": "team", "b": "team-2", "custom": "业主自起名"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("names=%v want=%v", got, want)
	}
}

func TestMigration0044NonDefaultDuplicateRollsBackOwnerRewrite(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.db.ExecContext(ctx, `
		CREATE TABLE console_principal(id TEXT PRIMARY KEY,provider TEXT,provider_issuer TEXT,provider_subject TEXT,display_name TEXT NOT NULL,active INTEGER);
		CREATE TABLE console_principal_role(principal_id TEXT,role TEXT,owner_key TEXT,active INTEGER);
		CREATE TABLE console_spec(id TEXT PRIMARY KEY,principal_id TEXT NOT NULL,title TEXT NOT NULL,state TEXT NOT NULL,current_revision INTEGER NOT NULL,claimed_by TEXT,edit_capability_hash TEXT NOT NULL DEFAULT '',editing_closed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
		INSERT INTO console_principal VALUES('owner','f','i','o','Owner',1),('x','f','i','x','自起名',1),('y','f','i','y','自起名',0);
		INSERT INTO console_principal_role VALUES('owner','owner','team',1),('x','owner','x',1),('y','owner','y',1);
		INSERT INTO console_spec VALUES('s','owner','s','draft',1,NULL,'',0,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	body, err := migrationFS.ReadFile("migrations/0044_console_spec_workflow.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	err = db.applyConsoleSpecWorkflowMigration(ctx, "0044_console_spec_workflow.sql", string(body), hex.EncodeToString(sum[:]))
	if err == nil || !strings.Contains(err.Error(), "自起名(count=2") {
		t.Fatalf("migration error=%v", err)
	}
	var name string
	if err = db.db.QueryRowContext(ctx, `SELECT display_name FROM console_principal WHERE id='owner'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Owner" {
		t.Fatalf("partial rewrite survived rollback: %q", name)
	}
}

func TestMigration0044RejectsAndListsDuplicateOrEmptyShortNames(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.db.ExecContext(ctx, `CREATE TABLE console_principal(id TEXT PRIMARY KEY,display_name TEXT,active INTEGER);INSERT INTO console_principal VALUES('a','',1),('b','same',1),('c','same',0),('d',char(9)||char(10),0)`)
	if err != nil {
		t.Fatal(err)
	}
	err = db.preflightConsolePrincipalDisplayNames(ctx)
	if err == nil || !strings.Contains(err.Error(), "<empty>") || !strings.Contains(err.Error(), "<blank-whitespace>") || !strings.Contains(err.Error(), "same(count=2") || !strings.Contains(err.Error(), "ids=b,c") {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestConsoleSpecTwelveStepFlowSingleEditorFinalAndOwnerOnlyTasks(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	createSpec(t, db, "flow", "Flow", now)
	revise := func(actor string, expected int) {
		r := specRevision("flow", expected+1, fmt.Sprintf("%s-%d", actor, expected+1))
		r.CreatedBy = actor
		if _, err := db.AppendConsoleSpecRevision(ctx, actor, "flow", expected, r, now.Add(time.Duration(expected)*time.Minute)); err != nil {
			t.Fatalf("revise %s/%d: %v", actor, expected, err)
		}
	}
	revise("owner-a", 1)
	if err := db.TransferConsoleSpec(ctx, "flow", "owner-a", "乙", "editing", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendConsoleSpecRevision(ctx, "owner-a", "flow", 2, specRevision("flow", 3, "stolen"), now); err != ErrConsoleSpecEditDenied {
		t.Fatalf("non-holder err=%v", err)
	}
	revise("owner-b", 2)
	if err := db.TransferConsoleSpec(ctx, "flow", "owner-b", "甲", "editing", now); err != nil {
		t.Fatal(err)
	}
	revise("owner-a", 3)
	if err := db.TransferConsoleSpec(ctx, "flow", "owner-a", "丙", "review", now); err != nil {
		t.Fatal(err)
	}
	revise("manager", 4)
	if err := db.TransferConsoleSpec(ctx, "flow", "manager", "甲", "editing", now); err != nil {
		t.Fatal(err)
	}
	revise("owner-a", 5)
	if err := db.TransferConsoleSpec(ctx, "flow", "owner-a", "丙", "review", now); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(ctx, "flow", "manager", now); err != nil {
		t.Fatal(err)
	}
	finalized, err := db.ConsoleSpec(ctx, "owner-a", "flow", false)
	if err != nil {
		t.Fatal(err)
	}
	if finalized == nil || finalized.State != "final" || finalized.CurrentEditorID != "" || finalized.CurrentEditorShort != "甲" {
		t.Fatalf("finalized spec must automatically return to its creator for task splitting: %+v", finalized)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE console_principal SET display_name='' WHERE id='owner-a'`); err != nil {
		t.Fatal(err)
	}
	fallback, err := db.ConsoleSpec(ctx, "owner-a", "flow", false)
	if err != nil {
		t.Fatal(err)
	}
	if fallback == nil || fallback.OwnerShortName != "owner-a" || fallback.CurrentEditorShort != "owner-a" {
		t.Fatalf("empty human name must fall back to the non-empty principal short identifier: %+v", fallback)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE console_principal SET display_name='甲' WHERE id='owner-a'`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"owner-a", "owner-b", "manager"} {
		r := specRevision("flow", 7, "final-change")
		r.CreatedBy = actor
		if _, err := db.AppendConsoleSpecRevision(ctx, actor, "flow", 6, r, now); !errors.Is(err, ErrConsoleSpecFinal) {
			t.Fatalf("final actor=%s err=%v", actor, err)
		}
	}
	p := model.ConsoleTaskProposal{ID: "task", SpecID: "flow", Title: "T", TaskContent: "T", Status: "planned", DependsOnJSON: "[]", ReviewedBy: "owner-a", CreatedAt: now, ReviewedAt: now}
	if err := db.CreateConsoleTaskProposal(ctx, p, "owner-b", false); err == nil {
		t.Fatal("non-owner split accepted")
	}
	if err := db.CreateConsoleTaskProposal(ctx, p, "owner-a", false); err != nil {
		t.Fatal(err)
	}
	if err := db.CloseConsoleSpecTaskSplit(ctx, "flow", "owner-a", now); err != nil {
		t.Fatal(err)
	}
	p.ID = "task2"
	if err := db.CreateConsoleTaskProposal(ctx, p, "owner-a", false); err == nil {
		t.Fatal("closed split accepted")
	}
}

func TestConsoleSpecEditorsAndTransfersRequireActiveOwnerRole(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "nonowner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "no", DisplayName: "非业主", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "inactive-owner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "io", DisplayName: "停用业主", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipalRole(ctx, model.ConsolePrincipalRole{PrincipalID: "inactive-owner", Role: "owner", OwnerKey: "tenant-a", Active: false}); err != nil {
		t.Fatal(err)
	}
	editors, err := db.ListActiveConsolePrincipals(ctx)
	if err != nil {
		t.Fatalf("owner candidate list returned an error: %v", err)
	}
	got := make(map[string]bool, len(editors))
	for _, editor := range editors {
		got[editor.ID] = true
		if editor.ShortName == "" || editor.DisplayName == "" {
			t.Fatalf("owner candidate must carry both short and human-readable names: %+v", editor)
		}
		if editor.ID == "nonowner" || editor.ID == "inactive-owner" {
			t.Fatalf("non-owner leaked into editor candidates: %+v", editors)
		}
	}
	if len(editors) != 3 || !got["owner-a"] || !got["owner-b"] || !got["manager"] {
		t.Fatalf("owner candidate ids=%v want exactly owner-a,owner-b,manager", got)
	}
	if err := db.UpsertConsolePrincipalRole(ctx, model.ConsolePrincipalRole{PrincipalID: "owner-a", Role: "owner", OwnerKey: "tenant-b", Active: true}); err != nil {
		t.Fatal(err)
	}
	editors, err = db.ListActiveConsolePrincipals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, editor := range editors {
		if editor.ID == "owner-a" && editor.ShortName != "" {
			t.Fatalf("multi-scope owner must visibly fall back to its human name, not an arbitrary scope: %+v", editor)
		}
	}
	createSpec(t, db, "owner-only-target", "owner-only-target", time.Now())
	if err := db.TransferConsoleSpec(ctx, "owner-only-target", "owner-a", "非业主", "editing", time.Now()); !errors.Is(err, ErrConsoleSpecPermission) {
		t.Fatalf("active non-owner became editor: %v", err)
	}
	if err := db.TransferConsoleSpec(ctx, "owner-only-target", "owner-a", "乙", "editing", time.Now()); err != nil {
		t.Fatalf("active owner transfer failed: %v", err)
	}
}
