package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionHistoryConcurrentAppendGroupCommitAssignsCursors(t *testing.T) {
	h := newHistoryTest(t)
	const count = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := h.Append(context.Background(), "owner", "agent", "work",
				fmt.Sprintf("event-%02d", i), "assistant_text", []byte(`{"kind":"assistant_text"}`), time.Unix(10, 0))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if commits := h.appendBatches.Load(); commits == 0 || commits >= count {
		t.Fatalf("concurrent appends were not group committed: commits=%d appends=%d", commits, count)
	}
	page, err := h.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil || len(page.Events) != count {
		t.Fatalf("group page count=%d err=%v", len(page.Events), err)
	}
	seen := map[string]bool{}
	for i, event := range page.Events {
		if event.Cursor.Seq != int64(i+1) || seen[event.EventID] {
			t.Fatalf("non-monotonic/duplicate cursor at index=%d event=%+v", i, event)
		}
		seen[event.EventID] = true
	}
	for i := 0; i < count; i++ {
		if !seen[fmt.Sprintf("event-%02d", i)] {
			t.Fatalf("missing event-%02d", i)
		}
	}
}

func TestSessionHistoryGroupCommitFallbackIsolatesDuplicateEvent(t *testing.T) {
	h := newHistoryTest(t)
	start := make(chan struct{})
	ids := []string{"same-id", "same-id", "valid-id"}
	var wg sync.WaitGroup
	errs := make(chan error, len(ids))
	for _, eventID := range ids {
		eventID := eventID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := h.Append(context.Background(), "owner", "agent", "work", eventID, "assistant_text", []byte(`{"kind":"assistant_text"}`), time.Unix(10, 0))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	failures := 0
	for err := range errs {
		if err != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("duplicate event failure count=%d; want 1", failures)
	}
	page, err := h.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("unrelated events were lost after batch rollback: count=%d err=%v", len(page.Events), err)
	}
	seen := map[string]bool{}
	for _, event := range page.Events {
		seen[event.EventID] = true
	}
	if !seen["same-id"] || !seen["valid-id"] {
		t.Fatalf("surviving events=%v", seen)
	}
}

func newHistoryTest(t *testing.T) *SessionHistory {
	t.Helper()
	h, err := OpenSessionHistory(filepath.Join(t.TempDir(), "workpulse-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if err := h.SetEnabled(context.Background(), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSessionHistoryPersistsStableCursorAndOwnerScopedPage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	h, err := OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabled(ctx, true, time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	c1, err := h.Append(ctx, "owner-a", "agent-a", "work", "evt-1", "assistant_text", json.RawMessage(`{"kind":"assistant_text","text":"one"}`), time.Unix(11, 0))
	if err != nil {
		t.Fatal(err)
	}
	if c1.Seq != 1 || c1.Generation == "" {
		t.Fatalf("cursor=%+v", c1)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h2, err := OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()
	page, err := h2.Page(ctx, "owner-a", "agent-a", "work", c1.Generation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].EventID != "evt-1" || page.Events[0].Cursor != c1 {
		t.Fatalf("page=%+v", page)
	}
	other, err := h2.Page(ctx, "owner-b", "agent-a", "work", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Events) != 0 {
		t.Fatalf("cross-owner page leaked: %+v", other)
	}
}

func TestSessionHistoryMigrationLedgerIsSeparateFromPrimaryStore(t *testing.T) {
	primary, ctx := newTestSQLite(t)
	var count int
	if err := primary.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'session_history_%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("primary store contains R15 tables: %d", count)
	}
	history, err := OpenSessionHistory(filepath.Join(t.TempDir(), "workpulse-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.db.QueryRowContext(ctx, `SELECT count(*) FROM session_history_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("history migrations=%d err=%v", count, err)
	}
}

func TestSessionHistoryDisabledDoesNotWriteAndReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	h, err := OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.SetEnabled(ctx, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	_, err = h.Append(ctx, "o", "a", "s", "e", "assistant_text", []byte(`{"kind":"assistant_text"}`), time.Now())
	if err == nil {
		t.Fatal("Append accepted while disabled")
	}
	_, err = h.Page(ctx, "o", "a", "s", "", nil)
	if !errors.Is(err, ErrSessionHistoryDisabled) {
		t.Fatalf("Page error=%v", err)
	}
	rows, _, err := h.HistoryStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("disabled event rows=%d", rows)
	}
}

func TestSessionHistoryOversizeRecordsGapWithoutTruncating(t *testing.T) {
	h := newHistoryTest(t)
	payload, _ := json.Marshal(map[string]string{"text": string(make([]byte, SessionHistoryEventMaxBytes))})
	_, err := h.Append(context.Background(), "o", "a", "s", "big", "assistant_text", payload, time.Now())
	if err == nil {
		t.Fatal("oversize payload accepted")
	}
	page, err := h.Page(context.Background(), "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 0 || len(page.Gaps) == 0 || page.Gaps[0].Reason != "oversize" {
		t.Fatalf("oversize page=%+v", page)
	}
}

func TestSessionHistoryExactRecordByteBoundary(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	// The fixed record accounting is payload + 5 identity/type bytes + 96 metadata bytes.
	boundary := []byte(`"` + strings.Repeat("x", 999_897) + `"`)
	if len(boundary)+5+96 != int(SessionHistoryEventMaxBytes) {
		t.Fatalf("test fixture bytes=%d", len(boundary)+101)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "e", "t", boundary, time.Now()); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
	tooLarge := []byte(`"` + strings.Repeat("x", 999_898) + `"`)
	if _, err := h.Append(ctx, "o", "a", "s", "e2", "t", tooLarge, time.Now()); !errors.Is(err, ErrSessionHistoryOversize) {
		t.Fatalf("limit+1 error=%v", err)
	}
}

func TestSessionHistoryAgentQuotaEvictsOldestBeforeNewRow(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	now := time.Unix(10_000, 0).UTC()
	if _, err := h.Append(ctx, "owner", "agent", "session", "old", "assistant_text", []byte(`{"text":"old"}`), now); err != nil {
		t.Fatal(err)
	}
	newPayload := []byte(`{"text":"new"}`)
	newBytes := int64(len(newPayload) + len("owner") + len("agent") + len("session") + len("new") + len("assistant_text") + 96)
	oldBytes := SessionHistoryAgentMaxBytes - newBytes + 1
	if _, err := h.db.ExecContext(ctx, `UPDATE session_history_event SET record_bytes=? WHERE event_id='old'`, oldBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE session_history_generation SET retained_bytes=? WHERE owner_key='owner' AND agent_key_id='agent' AND session_name='session'`, oldBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE session_history_runtime_state SET retained_bytes=? WHERE singleton=1`, oldBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "owner", "agent", "session", "new", "assistant_text", newPayload, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var rows int
	var gotBytes int64
	if err := h.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(record_bytes),0) FROM session_history_event WHERE owner_key='owner' AND agent_key_id='agent'`).Scan(&rows, &gotBytes); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || gotBytes != newBytes {
		t.Fatalf("agent quota rows=%d retained_bytes=%d want one new row %d bytes", rows, gotBytes, newBytes)
	}
}

func TestSessionHistoryGlobalQuotaEvictsOldestAcrossAgents(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	now := time.Unix(20_000, 0).UTC()
	const agents = 33
	perAgentBytes := SessionHistoryAgentMaxBytes - 1_000
	for i := 0; i < agents; i++ {
		agent := fmt.Sprintf("agent-%02d", i)
		eventID := fmt.Sprintf("old-%02d", i)
		if _, err := h.Append(ctx, "owner", agent, "session", eventID, "assistant_text", []byte(`{"text":"old"}`), now); err != nil {
			t.Fatal(err)
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE session_history_event SET record_bytes=? WHERE owner_key='owner' AND agent_key_id=?`, perAgentBytes, agent); err != nil {
			t.Fatal(err)
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE session_history_generation SET retained_bytes=? WHERE owner_key='owner' AND agent_key_id=?`, perAgentBytes, agent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE session_history_runtime_state SET retained_bytes=? WHERE singleton=1`, perAgentBytes*agents); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "owner", "agent-new", "session", "new", "assistant_text", []byte(`{"text":"new"}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var rows, oldestRows, gaps int
	var retained int64
	if err := h.db.QueryRowContext(ctx, `SELECT count(*),SUM(CASE WHEN agent_key_id IN ('agent-00','agent-01') THEN 1 ELSE 0 END) FROM session_history_event`).Scan(&rows, &oldestRows); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRowContext(ctx, `SELECT retained_bytes FROM session_history_runtime_state WHERE singleton=1`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM session_history_gap WHERE reason='global_capacity'`).Scan(&gaps); err != nil {
		t.Fatal(err)
	}
	if rows != agents-1 || oldestRows != 0 || retained > SessionHistoryGlobalMaxBytes || gaps != 2 {
		t.Fatalf("global quota rows=%d oldest_rows=%d retained=%d gaps=%d", rows, oldestRows, retained, gaps)
	}
}

func TestSessionHistoryRetentionBoundaryUsesVirtualTime(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	start := time.Unix(100, 0).UTC()
	if _, err := h.Append(ctx, "o", "a", "s", "old", "t", []byte(`{"n":1}`), start); err != nil {
		t.Fatal(err)
	}
	if err := h.CleanupBatch(ctx, start.Add(SessionHistoryRetain-time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	rows, _, err := h.HistoryStats(ctx)
	if err != nil || rows != 1 {
		t.Fatalf("before ttl rows=%d err=%v", rows, err)
	}
	if err := h.CleanupBatch(ctx, start.Add(SessionHistoryRetain)); err != nil {
		t.Fatal(err)
	}
	rows, _, err = h.HistoryStats(ctx)
	if err != nil || rows != 0 {
		t.Fatalf("at ttl rows=%d err=%v", rows, err)
	}
}

func TestSessionHistoryDisabledPausesTTLCleanupUntilReenabled(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := h.SetEnabled(ctx, true, base); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "owner", "agent", "session", "event", "assistant_text", []byte(`{"text":"old"}`), base); err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabled(ctx, false, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.CleanupBatch(ctx, base.Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _, err := h.HistoryStats(ctx)
	if err != nil || rows != 1 {
		t.Fatalf("disabled TTL cleanup rows=%d err=%v", rows, err)
	}
	if err := h.SetEnabled(ctx, true, base.Add(32*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.CleanupBatch(ctx, base.Add(32*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _, err = h.HistoryStats(ctx)
	if err != nil || rows != 0 {
		t.Fatalf("enabled TTL cleanup rows=%d err=%v", rows, err)
	}
}

func TestSessionHistoryOffIntervalBecomesGapOnReenable(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	start := time.Unix(1_000, 0).UTC()
	if _, err := h.Append(ctx, "o", "a", "s", "before", "t", []byte(`{"n":1}`), start); err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabled(ctx, false, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "off", "t", []byte(`{"n":2}`), start.Add(2*time.Minute)); !errors.Is(err, ErrSessionHistoryDisabled) {
		t.Fatalf("off append error=%v", err)
	}
	if err := h.SetEnabled(ctx, true, start.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "after", "t", []byte(`{"n":3}`), start.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	page, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || len(page.Gaps) != 1 || page.Gaps[0].Reason != "feature_disabled" {
		t.Fatalf("toggle page=%+v", page)
	}
}

func TestSessionHistoryStartupDoesNotInferGapWhenDisableWriteFailed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	h, err := OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	start := time.Unix(2_000, 0).UTC()
	if err := h.SetEnabledAtStartup(ctx, true, start); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "before", "t", []byte(`{"n":1}`), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `CREATE TRIGGER reject_disable BEFORE UPDATE OF enabled ON session_history_runtime_state WHEN NEW.enabled=0 BEGIN SELECT RAISE(ABORT,'simulated readonly state'); END`); err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabledAtStartup(ctx, false, start.Add(10*time.Second)); err == nil {
		t.Fatal("expected disabled-state write failure")
	}
	if _, err := h.db.ExecContext(ctx, `DROP TRIGGER reject_disable`); err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabledAtStartup(ctx, true, start.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Gaps) != 0 {
		t.Fatalf("failed disable write cannot be inferred as a confirmed disabled interval: %+v", page.Gaps)
	}
}

func TestSessionHistoryConsecutiveEnabledStartupDoesNotCreateGap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	start := time.Unix(2_000, 0).UTC()
	h, err := OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabledAtStartup(ctx, true, start); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "before", "t", []byte(`{"n":1}`), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.SetEnabledAtStartup(ctx, true, start.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || len(page.Gaps) != 0 {
		t.Fatalf("ordinary enabled restart must remain continuous: %+v", page)
	}
}

func TestSessionHistoryStartupOpenOffOnCreatesExactlyOneGap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	start := time.Unix(3_000, 0).UTC()
	h, err := OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabledAtStartup(ctx, true, start); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(ctx, "o", "a", "s", "before", "t", []byte(`{"n":1}`), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetEnabledAtStartup(ctx, false, start.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.SetEnabledAtStartup(ctx, true, start.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || len(page.Gaps) != 1 || page.Gaps[0].Reason != "feature_disabled" || !page.Gaps[0].StartedAt.Equal(start.Add(10*time.Second)) {
		t.Fatalf("open-off-on startup should retain history and create exactly one bounded gap: %+v", page)
	}
}

func TestSessionHistoryDeletionQueuePurgesAndRetries(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	if _, err := h.Append(ctx, "old-owner", "agent", "s", "event", "t", []byte(`{"secret":"body"}`), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := h.RequestAgentDeletion(ctx, "old-owner", "agent", time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := h.CleanupBatch(ctx, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	rows, _, err := h.HistoryStats(ctx)
	if err != nil || rows != 0 {
		t.Fatalf("purge rows=%d err=%v", rows, err)
	}
	if err := h.CleanupBatch(ctx, time.Unix(4, 0)); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM session_history_delete_queue`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("queue rows=%d err=%v", remaining, err)
	}
}

func TestSessionHistoryPaginationUsesExclusiveCursorWithoutDuplicates(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	for i := 0; i < 205; i++ {
		payload, _ := json.Marshal(map[string]int{"n": i})
		if _, err := h.Append(ctx, "o", "a", "s", fmt.Sprintf("event-%03d", i), "t", payload, time.Unix(int64(i+1), 0)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	page1, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Events) != 100 || !page1.HasOlder || page1.Events[0].Cursor.Seq != 106 || page1.Events[99].Cursor.Seq != 205 {
		t.Fatalf("page1 range/count: %d %d..%d", len(page1.Events), page1.Events[0].Cursor.Seq, page1.Events[len(page1.Events)-1].Cursor.Seq)
	}
	page2, err := h.Page(ctx, "o", "a", "s", page1.Generation, page1.BeforeSeq)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 100 || page2.Events[0].Cursor.Seq != 6 || page2.Events[99].Cursor.Seq != 105 {
		t.Fatalf("page2 range/count: %d %d..%d", len(page2.Events), page2.Events[0].Cursor.Seq, page2.Events[len(page2.Events)-1].Cursor.Seq)
	}
	page3, err := h.Page(ctx, "o", "a", "s", page2.Generation, page2.BeforeSeq)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3.Events) != 5 || page3.HasOlder || page3.Events[0].Cursor.Seq != 1 || page3.Events[4].Cursor.Seq != 5 {
		t.Fatalf("page3=%+v", page3)
	}
}

func TestSessionHistoryPageStopsAtFourMiBAndKeepsCursorExclusive(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	text := strings.Repeat("x", 60_000)
	for i := 0; i < 100; i++ {
		payload, _ := json.Marshal(map[string]string{"text": text})
		if _, err := h.Append(ctx, "o", "a", "s", fmt.Sprintf("large-%03d", i), "t", payload, time.Unix(int64(i+1), 0)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := h.Page(ctx, "o", "a", "s", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 4<<20 || len(page.Events) == 0 || len(page.Events) >= 100 || !page.HasOlder || page.BeforeSeq == nil {
		t.Fatalf("bytes=%d events=%d older=%t before=%v", len(body), len(page.Events), page.HasOlder, page.BeforeSeq)
	}
	older, err := h.Page(ctx, "o", "a", "s", page.Generation, page.BeforeSeq)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Events) == 0 || older.Events[len(older.Events)-1].Cursor.Seq+1 != page.Events[0].Cursor.Seq {
		t.Fatalf("cursor skip/overlap: older last=%d current first=%d", older.Events[len(older.Events)-1].Cursor.Seq, page.Events[0].Cursor.Seq)
	}
}
