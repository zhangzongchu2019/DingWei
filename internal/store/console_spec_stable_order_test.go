package store

import (
	"context"
	"testing"
	"time"
)

func TestConsoleSpecTransferKeepsCreatedOrderPosition(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	createSpec(t, db, "spec-b", "B", now.Add(time.Minute))
	// Make the old updated_at order disagree with the stable created_at order.
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec SET updated_at=? WHERE id='spec-a'`, now.Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	position := func(id string) int {
		items, err := db.ListConsoleSpecs(ctx, "owner-a", false)
		if err != nil {
			t.Fatal(err)
		}
		for i, item := range items {
			if item.ID == id {
				return i
			}
		}
		t.Fatalf("spec %s missing from list", id)
		return -1
	}
	before := position("spec-b")
	if err := db.TransferConsoleSpec(ctx, "spec-b", "owner-a", "乙", "review", now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after := position("spec-b")
	if before != after {
		t.Fatalf("transferred spec moved under the user's pointer: before=%d after=%d", before, after)
	}
}
