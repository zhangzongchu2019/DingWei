package migrate

import (
	"context"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestGroupCommunicationMigrationKeepsOldCatalogCompatible(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)
	entries, err := LedgerInDomains(HubDomains())
	if err != nil {
		t.Fatal(err)
	}
	old := make([]Entry, 0, len(entries)-1)
	found := false
	for _, entry := range entries {
		if entry.Name == "0101_group_communication.sql" {
			found = true
			continue
		}
		old = append(old, entry)
	}
	if !found {
		t.Fatal("new group migration missing from Hub catalog")
	}
	pool := testSchema(t, pgtest.DSN(t))
	ctx := context.Background()
	if _, err := migrateEntries(ctx, pool, old, nil, HubDomains()); err != nil {
		t.Fatal(err)
	}
	up, err := migrateEntries(ctx, pool, entries, nil, HubDomains())
	if err != nil || len(up.Applied) != 1 || up.Applied[0] != "0101_group_communication.sql" {
		t.Fatalf("upgrade did not apply only additive group migration: %+v %v", up, err)
	}
	var kind, nullable, defaultValue string
	row, err := pool.QueryRow(ctx, testCtx(t), `SELECT data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'np_group' AND column_name = 'leader_only'`)
	if err != nil {
		t.Fatal(err)
	}
	err = row.Scan(&kind, &nullable, &defaultValue)
	if err != nil || kind != "boolean" || nullable != "NO" || defaultValue != "false" {
		t.Fatalf("unexpected additive column: %q %q %q %v", kind, nullable, defaultValue, err)
	}
	back, err := migrateEntries(ctx, pool, old, nil, HubDomains())
	if err != nil || len(back.Applied) != 0 || len(back.Skipped) != len(old) {
		t.Fatalf("previous catalog cannot reuse upgraded schema: %+v %v", back, err)
	}
	var recorded int
	row, err = pool.QueryRow(ctx, testCtx(t), `SELECT count(*) FROM schema_migrations WHERE version = '0101_group_communication.sql'`)
	if err != nil {
		t.Fatal(err)
	}
	err = row.Scan(&recorded)
	if err != nil || recorded != 1 {
		t.Fatalf("previous catalog changed new migration record: count=%d error=%v", recorded, err)
	}
	// This proves only schema/catalog compatibility. An old Hub does not enforce
	// leader_only; deployment rollback must refuse downgrade if any group uses it.
}
