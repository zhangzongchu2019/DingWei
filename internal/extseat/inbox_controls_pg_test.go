package extseat

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func (f *fixture) inboxBytes(t *testing.T) string {
	t.Helper()
	row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT COALESCE(json_agg(x ORDER BY x.id)::text,'[]') FROM (SELECT * FROM np_extseat_inbox) x`)
	if e != nil {
		t.Fatal(e)
	}
	var v string
	if e = row.Scan(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCheckClearReadOnlyScopePG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, a := f.issue(t)
	for _, id := range []string{"one", "two"} {
		if _, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, id, "中😀"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := f.s.Wait(f.ctx, a, 0); e != nil {
		t.Fatal(e)
	} // one leased, the other queued
	// Seed outgoing data as a control direction.
	if _, e := f.s.Send(f.ctx, a, "helper.ho", "outgoing", "outgoing"); e != nil {
		t.Fatal(e)
	}
	before := f.inboxBytes(t)
	touches := f.touches(t)
	got, e := f.s.Check(f.ctx, a)
	if e != nil || len(got) != 2 || got[0].BodyBytes != 7 || got[0].From != "helper.ho" || f.inboxBytes(t) != before || f.touches(t) != touches {
		t.Fatalf("MCP_CHECK_READ_ONLY got=%+v e=%v", got, e)
	}
	if _, e = f.s.Clear(f.ctx, a, false); !errors.Is(e, extshared.ErrNotPermitted) || f.inboxBytes(t) != before {
		t.Fatalf("MCP_CLEAR_CONFIRM e=%v", e)
	}
	ids, e := f.s.Clear(f.ctx, a, true)
	if e != nil || len(ids) != 2 || f.touches(t) != touches {
		t.Fatalf("MCP_CLEAR_CONFIRMED e=%v ids=%v", e, ids)
	}
	ids, e = f.s.Clear(f.ctx, a, true)
	if e != nil || len(ids) != 0 {
		t.Fatalf("MCP_CLEAR_IDEMPOTENT e=%v ids=%v", e, ids)
	}
	after, e := f.s.Check(f.ctx, a)
	if e != nil || len(after) != 0 {
		t.Fatalf("MCP_CLEAR_CONFIRMED pending=%v e=%v", after, e)
	}
	var n int
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_outbox WHERE state='queued' AND body='outgoing'`)
	if e = row.Scan(&n); e != nil || n != 1 {
		t.Fatalf("MCP_CLEAR_OUTGOING_UNCHANGED %d %v", n, e)
	}
}
func TestSendWaitNewReplyAndReadOnlyPollPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, a := f.issue(t)
	if _, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "older", "old"); e != nil {
		t.Fatal(e)
	}
	f.clock.add(time.Second)
	receipt, e := f.s.Send(f.ctx, a, "helper.ho", "request", "request", true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `CREATE TABLE poll_write_counts(n bigint);INSERT INTO poll_write_counts VALUES(0);CREATE FUNCTION count_poll_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE poll_write_counts SET n=n+1;RETURN NULL;END $$;CREATE TRIGGER token_poll_write AFTER INSERT OR UPDATE OR DELETE ON np_extseat_token FOR EACH STATEMENT EXECUTE FUNCTION count_poll_write();CREATE TRIGGER seat_poll_write AFTER INSERT OR UPDATE OR DELETE ON np_extseat FOR EACH STATEMENT EXECUTE FUNCTION count_poll_write();CREATE TRIGGER inbox_poll_write AFTER INSERT OR UPDATE OR DELETE ON np_extseat_inbox FOR EACH STATEMENT EXECUTE FUNCTION count_poll_write();CREATE TRIGGER group_poll_write AFTER INSERT OR UPDATE OR DELETE ON test_groups FOR EACH STATEMENT EXECUTE FUNCTION count_poll_write();`); e != nil {
		t.Fatal(e)
	}
	before := f.inboxBytes(t)
	touches := f.touches(t)
	ds, e := f.s.WaitReply(f.ctx, a, receipt.CreatedAt, 2100*time.Millisecond)
	if e != nil || len(ds) != 0 || before != f.inboxBytes(t) || touches != f.touches(t) {
		t.Fatalf("MCP_SENDWAIT_READ_ONLY_IDLE ds=%v e=%v", ds, e)
	}
	var writes int
	countRow, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT n FROM poll_write_counts`)
	if countErr := countRow.Scan(&writes); countErr != nil || writes != 0 {
		t.Fatalf("MCP_SENDWAIT_READ_ONLY_IDLE writes=%d err=%v", writes, countErr)
	}
	if _, e = f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "fresh", "new"); e != nil {
		t.Fatal(e)
	}
	ds, e = f.s.WaitReply(f.ctx, a, receipt.CreatedAt, time.Second)
	if e != nil || len(ds) != 1 || ds[0].Body != "new" {
		t.Fatalf("MCP_SENDWAIT_NEW_REPLY ds=%v e=%v", ds, e)
	}
	if e = f.s.Ack(f.ctx, a, ds[0].ID, int(ds[0].Generation)); e != nil {
		t.Fatal(e)
	}
	retried, e := f.s.Send(f.ctx, a, "helper.ho", "request", "request", true)
	if e != nil || retried.DeliveryID != receipt.DeliveryID || !retried.CreatedAt.Equal(receipt.CreatedAt) {
		t.Fatalf("MCP_SENDWAIT_IDEMPOTENT e=%v", e)
	}
	var outboxes int
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_outbox`)
	if e = row.Scan(&outboxes); e != nil || outboxes != 1 {
		t.Fatalf("MCP_SENDWAIT_IDEMPOTENT rows=%d e=%v", outboxes, e)
	}
	// Cancelled calls must not lease a waiting old message.
	c, cancel := context.WithCancel(f.ctx)
	cancel()
	before = f.inboxBytes(t)
	if _, e = f.s.WaitReply(c, a, receipt.CreatedAt, time.Second); !errors.Is(e, context.Canceled) || before != f.inboxBytes(t) {
		t.Fatalf("MCP_SENDWAIT_CANCEL e=%v", e)
	}
	if e = f.s.Revoke(f.ctx, a.AgentID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Check(f.ctx, a); !errors.Is(e, extshared.ErrTokenRevoked) {
		t.Fatalf("MCP_SCOPE_REVOKED_CHECK e=%v", e)
	}
}
