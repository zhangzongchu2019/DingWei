package extwire

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeTables map[string]bool

func (f fakeTables) TableExists(_ context.Context, name string) (bool, error) {
	if name == "boom" {
		return false, errors.New("db down")
	}
	return f[name], nil
}

func TestCheckSchemaAllPresent(t *testing.T) {
	all := fakeTables{}
	for _, n := range RequiredTables {
		all[n] = true
	}
	if err := CheckSchema(context.Background(), all); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSchemaReportsEveryMissingTable(t *testing.T) {
	some := fakeTables{}
	for i, n := range RequiredTables {
		if i%2 == 0 {
			some[n] = true
		}
	}
	err := CheckSchema(context.Background(), some)
	if err == nil {
		t.Fatal("missing tables must fail closed")
	}
	for i, n := range RequiredTables {
		if i%2 == 1 && !strings.Contains(err.Error(), n) {
			t.Fatalf("error must name missing table %s: %v", n, err)
		}
	}
	if CheckSchema(context.Background(), nil) == nil {
		t.Fatal("nil checker must fail closed")
	}
}

func TestRequiredTablesMatch0105(t *testing.T) {
	want := []string{"np_external_principal", "np_extseat", "np_extseat_token", "np_extseat_inbox", "np_extseat_send_key", "np_extseat_outbox"}
	if strings.Join(RequiredTables, ",") != strings.Join(want, ",") {
		t.Fatalf("RequiredTables drifted from 0105: %v", RequiredTables)
	}
}
