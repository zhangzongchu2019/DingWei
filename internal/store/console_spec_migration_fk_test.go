package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
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
