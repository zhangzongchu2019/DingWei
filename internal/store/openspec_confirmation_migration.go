package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// A legacy database can lose its ledger while retaining the new immutable
// table. Verify every table/index/trigger definition before backfilling: neither
// replay CREATE nor IF NOT EXISTS may conceal a missing protection trigger.
func (s *SQLite) migrateOpenSpecConfirmation(ctx context.Context, name, body, digest string) error {
	expectedDB, err := repo.OpenSQLite(ctx, ":memory:", repo.MaxOpenConns(1))
	if err != nil {
		return err
	}
	defer expectedDB.Close()
	// This isolated in-memory catalog contains schema only, never tenant records.
	access := repo.SystemCtx()
	if _, err = expectedDB.Exec(ctx, access, body); err != nil {
		return err
	}
	rows, err := expectedDB.Query(ctx, access, `SELECT type,name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	type object struct{ kind, name, ddl string }
	var objects []object
	for rows.Next() {
		var o object
		if err = rows.Scan(&o.kind, &o.name, &o.ddl); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	present := 0
	for _, o := range objects {
		var kind, ddl string
		err = s.db.QueryRowContext(ctx, `SELECT type,sql FROM sqlite_master WHERE name=?`, o.name).Scan(&kind, &ddl)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if kind != o.kind || strings.Join(strings.Fields(ddl), " ") != strings.Join(strings.Fields(o.ddl), " ") {
			return fmt.Errorf("migrate %s: immutable confirmation schema mismatch: %s", name, o.name)
		}
		present++
	}
	if present == 0 {
		return s.applyMigration(ctx, name, body, digest)
	}
	if present != len(objects) {
		return fmt.Errorf("migrate %s: immutable confirmation schema partially present; refusing ledger backfill", name)
	}
	return s.recordMigration(ctx, name, digest)
}
