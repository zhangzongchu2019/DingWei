package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleSpecActorMigrationMarksUnattributedRowsUnknown(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "actor-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "owner", Provider: "test", ProviderIssuer: "test", ProviderSubject: "owner", DisplayName: "Owner", Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO console_spec(id,principal_id,title,state,current_revision,current_editor_id,archived,task_state,edit_capability_hash,editing_closed,created_at,updated_at) VALUES('s','owner','legacy','editing',1,'owner',0,'splitting','hash',0,'2026-09-27','2026-09-27')`,
		`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_via,created_at) VALUES('s',1,'legacy','[]','[]','digest','owner','principal','2026-09-27')`,
	} {
		if _, err := db.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
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
	rows, err := db.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("0050 left a foreign-key violation")
	}
}
