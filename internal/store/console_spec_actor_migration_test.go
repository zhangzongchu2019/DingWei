package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func migrateConsoleSpecActorFixtureThrough0049(t *testing.T, db *SQLite, ctx context.Context) {
	t.Helper()
	if _, err := db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name >= "0050_" {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		if name == "0044_console_spec_workflow.sql" {
			err = db.applyConsoleSpecWorkflowMigration(ctx, name, string(body), digest)
		} else {
			err = db.applyMigration(ctx, name, string(body), digest)
		}
		if err != nil {
			t.Fatalf("apply pre-0050 migration %s: %v", name, err)
		}
	}
}

func TestConsoleSpecActorMigrationMarksUnattributedRowsUnknown(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "actor-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	migrateConsoleSpecActorFixtureThrough0049(t, db, ctx)
	for _, stmt := range []string{
		`INSERT INTO console_principal(id,provider,provider_issuer,provider_subject,display_name,active) VALUES('owner','test','test','owner','Owner',1)`,
		`INSERT INTO console_agent(id,key_id,session_name,owner_principal,active) VALUES('agent','key','agent-session','owner',1)`,
		`INSERT INTO console_spec(id,principal_id,title,state,current_revision,current_editor_id,archived,task_state,edit_capability_hash,editing_closed,created_at,updated_at) VALUES('s','owner','legacy','editing',1,'owner',0,'splitting','hash',0,'2026-09-27','2026-09-27')`,
		`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_via,created_at) VALUES('s',1,'legacy','[]','[]','digest','owner','principal','2026-09-27')`,
		`INSERT INTO console_spec_review(id,spec_id,revision,stage,verdict,reviewed_by,reviewed_at) VALUES('review','s',1,'human_review','pass','owner','2026-09-27')`,
		`INSERT INTO console_task_proposal(id,schema_version,spec_id,revision,content_sha256,title,acceptance_json,input_anchors_json,review_id,reviewed_by,reviewed_at,created_at) VALUES('proposal',1,'s',1,'digest','proposal','[]','[]','review','owner','2026-09-27','2026-09-27')`,
		`INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,acceptance_json,input_anchors_json,created_by,created_at) VALUES('task','s',1,'digest','task','task','[]','[]','owner','2026-09-27')`,
		`INSERT INTO console_spec_presplit_request(id,spec_id,revision,agent_id,owner_principal,status,deadline_at,created_at) VALUES('presplit','s',1,'agent','owner','pending','2026-09-28','2026-09-27')`,
		`INSERT INTO console_spec_gate_review(id,spec_id,revision,review_agent_id,operator_principal,issues_json,created_at,updated_at) VALUES('gate','s',1,'agent','owner','[]','2026-09-27','2026-09-27')`,
	} {
		if _, err := db.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	body, err := migrationFS.ReadFile("migrations/0050_console_spec_actor_and_derive.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err := db.applyForeignKeyDisabledMigration(ctx, "0050_console_spec_actor_and_derive.sql", string(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := db.db.QueryRowContext(ctx, `SELECT created_by_kind FROM console_spec_revision WHERE spec_id='s' AND revision=1`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "unknown" {
		t.Fatalf("historical creator was inferred: kind=%q want unknown", kind)
	}
	var derived any
	if err := db.db.QueryRowContext(ctx, `SELECT derived_from FROM console_spec WHERE id='s'`).Scan(&derived); err != nil {
		t.Fatal(err)
	}
	if derived != nil {
		t.Fatalf("legacy spec invented derivation: %v", derived)
	}
	for _, table := range []string{"console_spec_review", "console_task_proposal", "console_spec_task", "console_spec_presplit_request", "console_spec_gate_review"} {
		var count int
		if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE spec_id='s' AND revision=1`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("0050 lost legacy child row: table=%s count=%d want 1", table, count)
		}
	}
	rows, err := db.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("0050 left a foreign-key violation")
	}
}
