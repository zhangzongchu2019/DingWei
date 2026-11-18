package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyProof() HistoryInputProvenance {
	return HistoryInputProvenance{SourceMessageID: "source", Kind: "agent", DisplayName: "timer", TrustDecision: "trusted", AttestedBy: "hub"}
}
func historyEcho(delivery, origin, turn, lifecycle string) []byte {
	b, _ := json.Marshal(map[string]any{"kind": "user_input", "delivery_id": delivery, "origin_delivery_id": origin, "turn_id": turn, "text": "synthetic", "data": map[string]any{"lifecycle": lifecycle, "source": "agent"}})
	return b
}
func rememberProof(t *testing.T, h *SessionHistory, owner, agent, session, delivery string, now time.Time) {
	t.Helper()
	if e := h.RememberInputProvenance(context.Background(), owner, agent, session, delivery, historyProof(), now); e != nil {
		t.Fatal(e)
	}
}
func proofCount(t *testing.T, h *SessionHistory) int {
	t.Helper()
	var n int
	if e := h.db.QueryRow(`SELECT count(*) FROM session_history_provenance`).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func assertHistoryAccounting(t *testing.T, h *SessionHistory) {
	t.Helper()
	var actual, global, generation int64
	if e := h.db.QueryRow(`SELECT (SELECT COALESCE(SUM(record_bytes),0) FROM session_history_event)+(SELECT COALESCE(SUM(record_bytes),0) FROM session_history_provenance),(SELECT retained_bytes FROM session_history_runtime_state WHERE singleton=1),(SELECT COALESCE(SUM(retained_bytes),0) FROM session_history_generation)`).Scan(&actual, &global, &generation); e != nil {
		t.Fatal(e)
	}
	if actual != global || actual != generation || actual < 0 {
		t.Fatalf("history budget drift actual=%d global=%d generations=%d", actual, global, generation)
	}
}
func hasHistoryProof(t *testing.T, h *SessionHistory, owner, agent, session string) bool {
	t.Helper()
	p, e := h.Page(context.Background(), owner, agent, session, "", nil)
	if e != nil || len(p.Events) != 1 {
		t.Fatalf("page=%+v err=%v", p, e)
	}
	var event map[string]any
	if e = json.Unmarshal(p.Events[0].Event, &event); e != nil {
		t.Fatal(e)
	}
	_, ok := event["provenance"]
	return ok
}

func TestHistoryProvenanceExactAssociationAndFailClosed(t *testing.T) {
	for _, test := range []string{"match", "owner", "agent", "session", "generation", "delivery", "origin", "queued", "turn", "conflict", "expired", "missing", "client_claim", "kind", "stored_trust", "stored_attestation", "other_delivery"} {
		t.Run(test, func(t *testing.T) {
			h := newHistoryTest(t)
			ctx := context.Background()
			now := time.Unix(100, 0)
			owner, agent, session := "o", "a", "s"
			if test != "missing" && test != "client_claim" {
				rememberProof(t, h, owner, agent, session, "d", now)
			}
			switch test {
			case "owner":
				owner = "other"
			case "agent":
				agent = "other"
			case "session":
				session = "other"
			case "generation":
				if _, e := h.db.Exec(`UPDATE session_history_provenance SET generation='different-generation'`); e != nil {
					t.Fatal(e)
				}
			case "conflict":
				p := historyProof()
				p.SourceMessageID = "different"
				if e := h.RememberInputProvenance(ctx, "o", "a", "s", "d", p, now); e != nil {
					t.Fatal(e)
				}
				rememberProof(t, h, "o", "a", "s", "d", now)
			case "stored_trust":
				if _, e := h.db.Exec(`UPDATE session_history_provenance SET trust_decision='unknown'`); e != nil {
					t.Fatal(e)
				}
			case "stored_attestation":
				if _, e := h.db.Exec(`UPDATE session_history_provenance SET attested_by='client'`); e != nil {
					t.Fatal(e)
				}
			case "expired":
				now = now.Add(SessionHistoryRetain)
			}
			if test == "owner" || test == "agent" || test == "session" {
				// Deliberately reuse a generation string across identities: each
				// of the five SQL predicates must carry its own security weight.
				var g string
				if e := h.db.QueryRow(`SELECT generation FROM session_history_provenance LIMIT 1`).Scan(&g); e != nil {
					t.Fatal(e)
				}
				if _, e := h.db.Exec(`INSERT INTO session_history_generation(owner_key,agent_key_id,session_name,generation,next_seq) VALUES(?,?,?,?,1)`, owner, agent, session, g); e != nil {
					t.Fatal(e)
				}
			}

			d, o, turn, life := "d", "d", "t", "admitted"
			switch test {
			case "delivery":
				d = "other"
			case "origin":
				o = "other"
			case "other_delivery":
				d = "other"
				o = "other"
			case "queued":
				life = "queued"
			case "turn":
				turn = ""
			}
			payload := historyEcho(d, o, turn, life)
			if test == "kind" {
				payload = []byte(strings.Replace(string(payload), `"kind":"user_input"`, `"kind":"assistant_text"`, 1))
			}
			if test == "client_claim" {
				var event map[string]any
				_ = json.Unmarshal(payload, &event)
				event["provenance"] = map[string]string{"kind": "agent", "trust_decision": "trusted", "attested_by": "hub"}
				payload, _ = json.Marshal(event)
			}
			if _, e := h.Append(ctx, owner, agent, session, "event", "session_event", payload, now); e != nil {
				t.Fatal(e)
			}
			if has := hasHistoryProof(t, h, owner, agent, session); has != (test == "match") {
				t.Fatalf("association %s proof=%v", test, has)
			}
			if test == "match" && proofCount(t, h) != 0 {
				t.Fatal("sidecar was not consumed")
			}
			assertHistoryAccounting(t, h)
		})
	}
}

func TestHistoryProvenanceIdempotencyTrustAndBounds(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	now := time.Unix(100, 0)
	rememberProof(t, h, "o", "a", "s", "d", now)
	rememberProof(t, h, "o", "a", "s", "d", now.Add(time.Second))
	if proofCount(t, h) != 1 {
		t.Fatal("replay duplicated sidecar")
	}
	assertHistoryAccounting(t, h)
	for _, which := range []string{"trust", "attested", "kind", "display", "source", "size"} {
		t.Run(which, func(t *testing.T) {
			p := historyProof()
			switch which {
			case "trust":
				p.TrustDecision = "unknown"
			case "attested":
				p.AttestedBy = "client"
			case "kind":
				p.Kind = "guess"
			case "display":
				p.DisplayName = ""
			case "source":
				p.SourceMessageID = ""
			case "size":
				p.DisplayName = strings.Repeat("x", 4096)
			}
			if e := h.RememberInputProvenance(ctx, "o", "a", "s", "invalid-"+which, p, now); e == nil {
				t.Fatal("untrusted/oversize sidecar accepted")
			}
		})
	}
	if proofCount(t, h) != 1 {
		t.Fatal("rejected proof changed rows")
	}
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint("quota-", global), func(t *testing.T) {
			hh := newHistoryTest(t)
			rememberProof(t, hh, "o", "a", "s", "old", now)
			query := `UPDATE session_history_generation SET retained_bytes=?`
			cap := SessionHistoryAgentMaxBytes
			if global {
				query = `UPDATE session_history_runtime_state SET retained_bytes=?`
				cap = SessionHistoryGlobalMaxBytes
			}
			if _, e := hh.db.Exec(query, cap); e != nil {
				t.Fatal(e)
			}
			if e := hh.RememberInputProvenance(ctx, "o", "a", "s", "new", historyProof(), now); e == nil {
				t.Fatal("quota allowed sidecar")
			}
		})
	}
}

func TestHistoryProvenanceAppendRollbackAndOversize(t *testing.T) {
	for _, which := range []string{"insert_failure", "oversize"} {
		t.Run(which, func(t *testing.T) {
			h := newHistoryTest(t)
			ctx := context.Background()
			now := time.Unix(100, 0)
			rememberProof(t, h, "o", "a", "s", "d", now)
			payload := historyEcho("d", "d", "t", "admitted")
			if which == "insert_failure" {
				if _, e := h.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON session_history_event BEGIN SELECT RAISE(ABORT,'fixture insert failure'); END`); e != nil {
					t.Fatal(e)
				}
			} else {
				var event map[string]any
				_ = json.Unmarshal(payload, &event)
				event["text"] = ""
				base, _ := json.Marshal(event)
				overhead := int64(len("o") + len("a") + len("s") + len("event") + len("session_event") + 96)
				event["text"] = strings.Repeat("x", int(SessionHistoryEventMaxBytes-overhead)-len(base)-20)
				payload, _ = json.Marshal(event)
			}
			_, e := h.Append(ctx, "o", "a", "s", "event", "session_event", payload, now)
			if e == nil {
				t.Fatal("failed event committed")
			}
			if which == "oversize" && !errors.Is(e, ErrSessionHistoryOversize) {
				t.Fatal(e)
			}
			if proofCount(t, h) != 1 {
				t.Fatal("failed append consumed sidecar")
			}
			n, _, _ := h.HistoryStats(ctx)
			if n != 0 {
				t.Fatal("failed append wrote duplicate event")
			}
			assertHistoryAccounting(t, h)
			if which == "oversize" {
				if e := h.flushOneGap(ctx, historyKey{"o", "a", "s"}); e != nil {
					t.Fatal(e)
				}
				page, e := h.Page(ctx, "o", "a", "s", "", nil)
				if e != nil {
					t.Fatal(e)
				}
				if len(page.Gaps) != 1 || page.Gaps[0].Reason != "oversize" {
					t.Fatalf("enhanced oversize lost existing gap semantics: %+v", page.Gaps)
				}
			}
			if which == "insert_failure" {
				_, _ = h.db.Exec(`DROP TRIGGER fail_history`)
			}
			if _, e = h.Append(ctx, "o", "a", "s", "event", "session_event", historyEcho("d", "d", "t", "admitted"), now); e != nil {
				t.Fatal(e)
			}
			if !hasHistoryProof(t, h, "o", "a", "s") || proofCount(t, h) != 0 {
				t.Fatal("retry lost proof")
			}
			assertHistoryAccounting(t, h)
		})
	}
}

func TestHistoryProvenanceLifecycleAndEviction(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0)
	for _, which := range []string{"ttl", "disable", "delete", "generation", "agent_capacity", "global_capacity"} {
		t.Run(which, func(t *testing.T) {
			h := newHistoryTest(t)
			rememberProof(t, h, "o", "a", "s", "d", now)
			switch which {
			case "ttl":
				if e := h.CleanupBatch(ctx, now.Add(SessionHistoryRetain)); e != nil {
					t.Fatal(e)
				}
			case "disable":
				if e := h.SetEnabled(ctx, false, now); e != nil {
					t.Fatal(e)
				}
			case "delete":
				if e := h.RequestAgentDeletion(ctx, "o", "a", now); e != nil {
					t.Fatal(e)
				}
				if e := h.CleanupBatch(ctx, now); e != nil {
					t.Fatal(e)
				}
			case "generation":
				if _, e := h.db.Exec(`UPDATE session_history_generation SET generation='other'`); e != nil {
					t.Fatal(e)
				}
			default:
				// Inflate only the sidecar's accounting, with matching counters, to exercise
				// real eviction without a multi-GB fixture. The row remains an actual sidecar.
				tx, e := h.db.BeginTx(ctx, nil)
				if e != nil {
					t.Fatal(e)
				}
				cap := SessionHistoryAgentMaxBytes
				if which == "global_capacity" {
					cap = SessionHistoryGlobalMaxBytes
				}
				// Drop CHECK via a test-local pragma; restore immediately after the fixture.
				_, e = tx.Exec(`PRAGMA ignore_check_constraints=ON`)
				if e != nil {
					t.Fatal(e)
				}
				_, e = tx.Exec(`UPDATE session_history_provenance SET record_bytes=?`, cap+1)
				if e != nil {
					t.Fatal(e)
				}
				_, _ = tx.Exec(`UPDATE session_history_generation SET retained_bytes=?`, cap+1)
				_, _ = tx.Exec(`UPDATE session_history_runtime_state SET retained_bytes=?`, cap+1)
				if which == "agent_capacity" {
					e = h.enforceAgentLimitTx(ctx, tx, "o", "a")
				} else {
					e = h.enforceGlobalLimitTx(ctx, tx)
				}
				if e != nil {
					t.Fatal(e)
				}
				_, _ = tx.Exec(`PRAGMA ignore_check_constraints=OFF`)
				if e = tx.Commit(); e != nil {
					t.Fatal(e)
				}
			}
			if proofCount(t, h) != 0 {
				t.Fatalf("%s left orphan sidecar", which)
			}
			var gaps int
			if e := h.db.QueryRow(`SELECT count(*) FROM session_history_gap`).Scan(&gaps); e != nil {
				t.Fatal(e)
			}
			if gaps != 0 {
				t.Fatalf("sidecar-only %s invented a body gap", which)
			}
			assertHistoryAccounting(t, h)
		})
	}
}

func TestHistoryProvenanceMigrationDigestAndNonconsecutiveNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	h, e := OpenSessionHistory(path)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = h.db.QueryRow(`SELECT count(*) FROM session_history_migrations WHERE version IN ('0001_session_history.sql','0078_history_provenance.sql')`).Scan(&n); e != nil || n != 2 {
		t.Fatalf("nonconsecutive migration rejected n=%d err=%v", n, e)
	}
	if _, e = h.db.Exec(`UPDATE session_history_migrations SET digest='wrong' WHERE version='0078_history_provenance.sql'`); e != nil {
		t.Fatal(e)
	}
	_ = h.Close()
	if hh, e := OpenSessionHistory(path); e == nil {
		_ = hh.Close()
		t.Fatal("digest mutation accepted")
	}
}

func TestHistoryProvenanceLegacyUpgradeAndUnmatchedBytes(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0)
	path := filepath.Join(t.TempDir(), "history.db")
	h, e := OpenSessionHistory(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.SetEnabled(ctx, true, now); e != nil {
		t.Fatal(e)
	}
	legacy := []byte(`{ "kind" : "user_input", "text":"legacy", "delivery_id":"d", "origin_delivery_id":"d", "turn_id":"t", "data":{"lifecycle":"admitted"} }`)
	if _, e = h.Append(ctx, "o", "a", "s", "legacy", "session_event", legacy, now); e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{`DROP TRIGGER history_provenance_generation_change`, `DROP TRIGGER history_provenance_generation_delete`, `DROP TABLE session_history_provenance`, `DELETE FROM session_history_migrations WHERE version='0078_history_provenance.sql'`} {
		if _, e = h.db.Exec(sql); e != nil {
			t.Fatal(e)
		}
	}
	_ = h.Close()
	h, e = OpenSessionHistory(path)
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	if e = h.SetEnabledAtStartup(ctx, true, now); e != nil {
		t.Fatal(e)
	}
	page, e := h.Page(ctx, "o", "a", "s", "", nil)
	if e != nil || len(page.Events) != 1 {
		t.Fatalf("upgrade page %v %v", page, e)
	}
	if string(page.Events[0].Event) != string(legacy) {
		t.Fatalf("legacy/unmatched JSON changed: %s", page.Events[0].Event)
	}
	if proofCount(t, h) != 0 {
		t.Fatal("upgrade invented proof")
	}
	assertHistoryAccounting(t, h)
	// An echo committed before its evidence is never retroactively changed.
	rememberProof(t, h, "o", "a", "s", "d", now)
	page, e = h.Page(ctx, "o", "a", "s", "", nil)
	if e != nil || strings.Contains(string(page.Events[0].Event), "provenance") {
		t.Fatal("late proof rewrote legacy event")
	}
}

func TestHistoryProvenanceEventEvictionAlsoCleansQueuedEvidence(t *testing.T) {
	h := newHistoryTest(t)
	ctx := context.Background()
	now := time.Unix(100, 0)
	rememberProof(t, h, "o", "a", "s", "d", now)
	if _, e := h.Append(ctx, "o", "a", "s", "queued", "session_event", historyEcho("d", "d", "", "queued"), now); e != nil {
		t.Fatal(e)
	}
	if proofCount(t, h) != 1 {
		t.Fatal("queued echo consumed evidence")
	}
	tx, e := h.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms FROM session_history_event`)
	if e != nil {
		t.Fatal(e)
	}
	batch, e := scanHistoryRows(rows)
	if e != nil {
		t.Fatal(e)
	}
	if e = deleteHistoryRows(ctx, tx, batch, "agent_capacity"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if proofCount(t, h) != 0 {
		t.Fatal("evicted queued event left its evidence")
	}
	assertHistoryAccounting(t, h)
}
