package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigration0044PreservesPopulatedSpecForeignKeyGraph(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "prod-shape.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.ExecContext(ctx, `
CREATE TABLE console_principal(id TEXT PRIMARY KEY,provider TEXT,provider_issuer TEXT,provider_subject TEXT,display_name TEXT NOT NULL,active INTEGER);
CREATE TABLE console_principal_role(principal_id TEXT,role TEXT,owner_key TEXT,active INTEGER);
CREATE TABLE console_spec(id TEXT PRIMARY KEY,principal_id TEXT NOT NULL REFERENCES console_principal(id),title TEXT NOT NULL,state TEXT NOT NULL,current_revision INTEGER NOT NULL,claimed_by TEXT,edit_capability_hash TEXT NOT NULL DEFAULT '',editing_closed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE console_spec_revision(spec_id TEXT NOT NULL REFERENCES console_spec(id),revision INTEGER NOT NULL,content_markdown TEXT NOT NULL,acceptance_json TEXT NOT NULL,input_anchors_json TEXT NOT NULL,content_sha256 TEXT NOT NULL,created_by TEXT NOT NULL REFERENCES console_principal(id),created_via TEXT NOT NULL DEFAULT 'principal',supersedes_revision INTEGER,created_at TEXT NOT NULL,PRIMARY KEY(spec_id,revision));
CREATE TABLE console_spec_review(id TEXT PRIMARY KEY,spec_id TEXT NOT NULL,revision INTEGER NOT NULL,reviewed_by TEXT NOT NULL REFERENCES console_principal(id),FOREIGN KEY(spec_id,revision) REFERENCES console_spec_revision(spec_id,revision));
CREATE TABLE console_task_proposal(id TEXT PRIMARY KEY,spec_id TEXT NOT NULL,revision INTEGER NOT NULL,review_id TEXT NOT NULL REFERENCES console_spec_review(id),FOREIGN KEY(spec_id,revision) REFERENCES console_spec_revision(spec_id,revision));
CREATE TABLE console_spec_event(id TEXT PRIMARY KEY,spec_id TEXT NOT NULL REFERENCES console_spec(id),revision INTEGER NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,detail TEXT NOT NULL DEFAULT '',at TEXT NOT NULL);
CREATE TABLE console_spec_task(id TEXT PRIMARY KEY,spec_id TEXT NOT NULL,revision INTEGER NOT NULL,created_by TEXT NOT NULL REFERENCES console_principal(id),FOREIGN KEY(spec_id,revision) REFERENCES console_spec_revision(spec_id,revision));
INSERT INTO console_principal VALUES('owner-entry-a','feishu','app','oa','Owner',1);
INSERT INTO console_principal_role VALUES('owner-entry-a','owner','zzc',1);
INSERT INTO console_spec VALUES('spec-e05','owner-entry-a','prod','archived',1,NULL,'',0,'2026-09-22T00:00:00Z','2026-09-22T00:00:00Z');
INSERT INTO console_spec_revision VALUES('spec-e05',1,'# prod','[]','[]','sha','owner-entry-a','principal',NULL,'2026-09-22T00:00:00Z');
INSERT INTO console_spec_review VALUES('review-1','spec-e05',1,'owner-entry-a');
INSERT INTO console_task_proposal VALUES('proposal-1','spec-e05',1,'review-1');
INSERT INTO console_spec_event VALUES('e1','spec-e05',1,'owner-entry-a','create','','2026-09-22T00:00:00Z'),('e2','spec-e05',1,'owner-entry-a','archive','','2026-09-22T00:00:01Z');
INSERT INTO console_spec_task VALUES('task-1','spec-e05',1,'owner-entry-a');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	assertFKCount := func(want int) {
		t.Helper()
		var got int
		if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&got); err != nil || got != want {
			t.Fatalf("foreign_key_check count=%d want=%d err=%v", got, want, err)
		}
	}
	assertFKCount(0)
	body, err := migrationFS.ReadFile("migrations/0044_console_spec_workflow.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err = db.applyConsoleSpecWorkflowMigration(ctx, "0044_console_spec_workflow.sql", string(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("0044 populated child graph: %v", err)
	}
	assertFKCount(0)
	for table, want := range map[string]int{"console_spec": 1, "console_spec_revision": 1, "console_spec_review": 1, "console_task_proposal": 1, "console_spec_event": 2, "console_spec_task": 1} {
		var got int
		if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
	var enabled int
	if err = db.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign_keys=%d err=%v", enabled, err)
	}
}

func TestMigration0044RejectsDanglingChildAndRestoresForeignKeys(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "dangling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.ExecContext(ctx, `
CREATE TABLE console_principal(id TEXT PRIMARY KEY,provider TEXT,provider_issuer TEXT,provider_subject TEXT,display_name TEXT NOT NULL,active INTEGER);
CREATE TABLE console_principal_role(principal_id TEXT,role TEXT,owner_key TEXT,active INTEGER);
CREATE TABLE console_spec(id TEXT PRIMARY KEY,principal_id TEXT NOT NULL REFERENCES console_principal(id),title TEXT NOT NULL,state TEXT NOT NULL,current_revision INTEGER NOT NULL,claimed_by TEXT,edit_capability_hash TEXT NOT NULL DEFAULT '',editing_closed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE console_spec_event(id TEXT PRIMARY KEY,spec_id TEXT NOT NULL REFERENCES console_spec(id),revision INTEGER NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,detail TEXT NOT NULL DEFAULT '',at TEXT NOT NULL);
INSERT INTO console_principal VALUES('owner-entry-a','feishu','app','oa','Owner',1);
INSERT INTO console_principal_role VALUES('owner-entry-a','owner','zzc',1);
INSERT INTO console_spec VALUES('spec-e05','owner-entry-a','prod','archived',1,NULL,'',0,'2026-09-22T00:00:00Z','2026-09-22T00:00:00Z');`)
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
	body = append(body, []byte(`
INSERT INTO console_spec_event(id,spec_id,revision,actor,action,detail,at)
VALUES('dangling','missing-spec',1,'owner-entry-a','probe','','2026-09-22T00:00:01Z');
`)...)
	sum := sha256.Sum256(body)
	err = db.applyConsoleSpecWorkflowMigration(ctx, "0044_console_spec_workflow.sql", string(body), hex.EncodeToString(sum[:]))
	if err == nil || !strings.Contains(err.Error(), "pre-commit foreign_key_check failed") {
		t.Errorf("dangling child migration err=%v", err)
	}
	var enabled int
	if err = db.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign_keys after rejected migration=%d err=%v", enabled, err)
	}
	var recorded int
	if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='0044_console_spec_workflow.sql'`).Scan(&recorded); err != nil || recorded != 0 {
		t.Errorf("migration ledger rows=%d err=%v", recorded, err)
	}
	for table, want := range map[string]int{
		"console_principal":      1,
		"console_principal_role": 1,
		"console_spec":           1,
		"console_spec_event":     0,
	} {
		var got int
		if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil || got != want {
			t.Errorf("rollback %s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
	var principalID, state string
	var claimedBy *string
	if err = db.db.QueryRowContext(ctx, `SELECT principal_id,state,claimed_by FROM console_spec WHERE id='spec-e05'`).Scan(&principalID, &state, &claimedBy); err != nil {
		t.Errorf("rollback legacy spec row: %v", err)
	} else if principalID != "owner-entry-a" || state != "archived" || claimedBy != nil {
		t.Errorf("rollback legacy spec=(principal=%q state=%q claimed_by=%v)", principalID, state, claimedBy)
	}
	var claimedColumn int
	if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('console_spec') WHERE name='claimed_by'`).Scan(&claimedColumn); err != nil || claimedColumn != 1 {
		t.Errorf("rollback legacy schema claimed_by columns=%d err=%v", claimedColumn, err)
	}
	var v2Tables int
	if err = db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='console_spec_v2'`).Scan(&v2Tables); err != nil || v2Tables != 0 {
		t.Errorf("rollback console_spec_v2 tables=%d err=%v", v2Tables, err)
	}
}
