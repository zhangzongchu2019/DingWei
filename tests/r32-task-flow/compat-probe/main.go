// The same probe is compiled against exact r31.1 and R32 store libraries.
// It operates only on a caller-supplied isolated SQLite file.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: probe seed|inspect isolated-db")
	}
	ctx := context.Background()
	s, err := store.OpenSQLite(os.Args[2])
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		return err
	}
	if os.Args[1] == "seed" {
		now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
		if err = s.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "fixture", Provider: "fixture", ProviderIssuer: "fixture", ProviderSubject: "fixture", DisplayName: "Fixture", Active: true}); err != nil {
			return err
		}
		if err = s.CreateConsoleSpec(ctx, model.ConsoleSpec{ID: "compat-spec", PrincipalID: "fixture", Title: "Compatibility"}, model.ConsoleSpecRevision{SpecID: "compat-spec", Revision: 1, ContentMarkdown: "requirements", ContentSHA256: strings.Repeat("a", 64), AcceptanceJSON: `[{"id":"A1","text":"preserved"}]`, InputAnchorsJSON: `[]`, CreatedBy: "fixture"}, now); err != nil {
			return err
		}
		raw, e := sql.Open("sqlite", os.Args[2])
		if e != nil {
			return e
		}
		defer raw.Close()
		if _, e = raw.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL WHERE id='compat-spec'`); e != nil {
			return e
		}
		if err = s.CreateConsoleTaskProposal(ctx, model.ConsoleTaskProposal{ID: "compat-task", SpecID: "compat-spec", Title: "Preserve", TaskContent: "preserved-content", DefinitionOfDone: "preserved-dod", Status: "planned", DependsOnJSON: `[]`, CreatedAt: now, ReviewedBy: "fixture"}, "fixture", false); err != nil {
			return err
		}
	}
	tasks, err := s.ListConsoleTaskProposals(ctx, "compat-spec")
	if err != nil {
		return err
	}
	if len(tasks) != 1 || tasks[0].ID != "compat-task" || tasks[0].TaskContent != "preserved-content" || tasks[0].DefinitionOfDone != "preserved-dod" {
		return fmt.Errorf("legacy task read or data preservation failed: %d", len(tasks))
	}
	raw, err := sql.Open("sqlite", os.Args[2])
	if err != nil {
		return err
	}
	defer raw.Close()
	var ledger int
	if err = raw.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='0055_console_task_flow_r32.sql'`).Scan(&ledger); err != nil {
		return err
	}
	var principal int
	if err = raw.QueryRowContext(ctx, `SELECT count(*) FROM console_principal WHERE id='fixture'`).Scan(&principal); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"tasks": len(tasks), "principal_rows": principal, "r32_ledger_rows": ledger, "task_content_preserved": true, "dod_preserved": true})
}
