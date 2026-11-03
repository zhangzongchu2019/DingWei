package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	SessionHistoryEventMaxBytes  int64 = 1_000_000
	SessionHistoryAgentMaxBytes  int64 = 64_000_000
	SessionHistoryGlobalMaxBytes int64 = 2_000_000_000
	SessionHistoryRetain               = 30 * 24 * time.Hour
	SessionHistoryBatch                = 500
	SessionHistoryTxnLimit             = 2 * time.Second
)

//go:embed history_migrations/*.sql
var historyMigrationFS embed.FS

type SessionHistory struct {
	db             *sql.DB
	path           string
	mu             sync.Mutex
	pending        map[historyKey]pendingHistoryGap
	pendingDeletes map[historyDeleteKey]int64
	closeOnce      sync.Once
	closeErr       error
	enabled        atomic.Bool
}

type historyKey struct{ owner, agent, session string }
type historyDeleteKey struct{ owner, agent string }
type pendingHistoryGap struct {
	reason         string
	count          int64
	started, ended int64
}

type SessionHistoryCursor struct {
	Generation string `json:"generation"`
	Seq        int64  `json:"seq"`
}
type SessionHistoryEvent struct {
	Cursor     SessionHistoryCursor `json:"cursor"`
	EventID    string               `json:"event_id"`
	ReceivedAt time.Time            `json:"received_at"`
	Event      json.RawMessage      `json:"event"`
}
type SessionHistoryGap struct {
	AfterSeq   int64      `json:"after_seq"`
	BeforeSeq  *int64     `json:"before_seq,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Reason     string     `json:"reason"`
	LostEvents int64      `json:"lost_events"`
}
type SessionHistoryPage struct {
	Generation string                `json:"generation"`
	HighWater  int64                 `json:"high_water"`
	Events     []SessionHistoryEvent `json:"events"`
	Gaps       []SessionHistoryGap   `json:"gaps"`
	HasOlder   bool                  `json:"has_older"`
	BeforeSeq  *int64                `json:"before_seq,omitempty"`
	GapBefore  bool                  `json:"gap_before"`
}

func OpenSessionHistory(path string) (*SessionHistory, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("history database path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(2000)&_pragma=foreign_keys(ON)", abs))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	h := &SessionHistory{db: db, path: abs, pending: map[historyKey]pendingHistoryGap{}, pendingDeletes: map[historyDeleteKey]int64{}}
	if err = h.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return h, nil
}

func (h *SessionHistory) Path() string {
	if h == nil {
		return ""
	}
	return h.path
}
func (h *SessionHistory) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() { h.closeErr = h.db.Close() })
	return h.closeErr
}

func (h *SessionHistory) migrate(ctx context.Context) error {
	if _, err := h.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS session_history_migrations(version TEXT PRIMARY KEY,digest TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := fs.Glob(historyMigrationFS, "history_migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		body, err := historyMigrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		var prior string
		err = h.db.QueryRowContext(ctx, `SELECT digest FROM session_history_migrations WHERE version=?`, filepath.Base(name)).Scan(&prior)
		if err == nil {
			if prior != digest {
				return fmt.Errorf("history migration %s digest changed", name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO session_history_migrations(version,digest,applied_at) VALUES(?,?,?)`, filepath.Base(name), digest, time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("history migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SetEnabled stores only feature-state metadata while disabled; event rows are never written.
func (h *SessionHistory) SetEnabled(ctx context.Context, enabled bool, now time.Time) error {
	return h.setEnabled(ctx, enabled, now, false)
}

// SetEnabledAtStartup conservatively accounts for the interval since the last
// process updated runtime state. In particular, a previous disabled-state
// write may have failed while the Hub continued with persistence disabled.
func (h *SessionHistory) SetEnabledAtStartup(ctx context.Context, enabled bool, now time.Time) error {
	return h.setEnabled(ctx, enabled, now, true)
}

func (h *SessionHistory) setEnabled(ctx context.Context, enabled bool, now time.Time, startup bool) error {
	if h == nil {
		return nil
	}
	now = now.UTC()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prior, initialized int
	var changed int64
	var disabled sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT enabled,initialized,changed_at_ms,disabled_since_ms FROM session_history_runtime_state WHERE singleton=1`).Scan(&prior, &initialized, &changed, &disabled); err != nil {
		return err
	}
	ms := now.UnixMilli()
	if initialized == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET enabled=?,changed_at_ms=?,disabled_since_ms=?,initialized=1 WHERE singleton=1`, historyBoolInt(enabled), ms, disabledAt(enabled, ms))
	} else if prior == 1 && !enabled {
		_, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET enabled=0,changed_at_ms=?,disabled_since_ms=? WHERE singleton=1`, ms, ms)
	} else if prior == 0 && enabled {
		if disabled.Valid {
			_, err = tx.ExecContext(ctx, `INSERT INTO session_history_gap(gap_id,owner_key,agent_key_id,session_name,generation,after_seq,before_seq,started_at_ms,reason,lost_event_count)
			SELECT lower(hex(randomblob(16))),owner_key,agent_key_id,session_name,generation,newest_seq,NULL,?, 'feature_disabled', 1 FROM session_history_generation`, disabled.Int64)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET enabled=1,changed_at_ms=?,disabled_since_ms=NULL WHERE singleton=1`, ms)
		}
	} else if prior == 1 && enabled && startup {
		_, err = tx.ExecContext(ctx, `INSERT INTO session_history_gap(gap_id,owner_key,agent_key_id,session_name,generation,after_seq,before_seq,started_at_ms,reason,lost_event_count)
		SELECT lower(hex(randomblob(16))),owner_key,agent_key_id,session_name,generation,newest_seq,NULL,?, 'feature_disabled', 1 FROM session_history_generation`, changed)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET enabled=1,changed_at_ms=?,disabled_since_ms=NULL WHERE singleton=1`, ms)
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET changed_at_ms=? WHERE singleton=1`, ms)
	}
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err == nil {
		h.enabled.Store(enabled)
	}
	return err
}

func historyBoolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func disabledAt(v bool, n int64) any {
	if v {
		return nil
	}
	return n
}

// Append writes one complete sanitized event before the caller publishes it to the live ring.
func (h *SessionHistory) Append(ctx context.Context, owner, agent, session, eventID, eventType string, payload []byte, now time.Time) (SessionHistoryCursor, error) {
	if h == nil {
		return SessionHistoryCursor{}, errors.New("history store unavailable")
	}
	if !h.enabled.Load() {
		return SessionHistoryCursor{}, ErrSessionHistoryDisabled
	}
	now = now.UTC()
	if owner == "" || agent == "" || session == "" {
		return SessionHistoryCursor{}, errors.New("history identity unavailable")
	}
	if !json.Valid(payload) {
		key := historyKey{owner, agent, session}
		h.rememberGap(key, "persist_error", now.UTC().UnixMilli())
		_ = h.flushOneGap(context.Background(), key)
		return SessionHistoryCursor{}, errors.New("history payload is not JSON")
	}
	if eventID == "" {
		eventID = randomHistoryID()
	}
	if eventType == "" {
		eventType = "unknown"
	}
	recordBytes := int64(len(payload) + len(owner) + len(agent) + len(session) + len(eventID) + len(eventType) + 96)
	key := historyKey{owner, agent, session}
	if recordBytes > SessionHistoryEventMaxBytes {
		h.rememberGap(key, "oversize", now.UnixMilli())
		_ = h.flushOneGap(context.Background(), key)
		return SessionHistoryCursor{}, fmt.Errorf("%w: %d", ErrSessionHistoryOversize, recordBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, SessionHistoryTxnLimit)
	defer cancel()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, err
	}
	defer tx.Rollback()
	var enabled int
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM session_history_runtime_state WHERE singleton=1`).Scan(&enabled); err != nil {
		return SessionHistoryCursor{}, err
	}
	if enabled != 1 {
		return SessionHistoryCursor{}, errors.New("session history disabled")
	}
	gen, err := ensureHistoryGeneration(ctx, tx, key)
	if err != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, err
	}
	flushedGap, flushErr := h.flushPendingTx(ctx, tx, key, gen, now.UnixMilli())
	if flushErr != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, flushErr
	}
	seq := gen.next
	if _, err = tx.ExecContext(ctx, `UPDATE session_history_gap SET before_seq=?,ended_at_ms=? WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND before_seq IS NULL AND after_seq<?`, seq, now.UnixMilli(), owner, agent, session, gen.value, seq); err != nil {
		return SessionHistoryCursor{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_history_event(owner_key,agent_key_id,session_name,generation,seq,event_id,received_at_ms,event_type,payload_json,record_bytes) VALUES(?,?,?,?,?,?,?,?,?,?)`, owner, agent, session, gen.value, seq, eventID, now.UnixMilli(), eventType, payload, recordBytes); err != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_history_generation SET next_seq=?,newest_seq=?,oldest_seq=CASE WHEN oldest_seq=0 THEN ? ELSE oldest_seq END,retained_bytes=retained_bytes+? WHERE owner_key=? AND agent_key_id=? AND session_name=?`, seq+1, seq, seq, recordBytes, owner, agent, session); err != nil {
		return SessionHistoryCursor{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET retained_bytes=retained_bytes+? WHERE singleton=1`, recordBytes); err != nil {
		return SessionHistoryCursor{}, err
	}
	if err = h.enforceAgentLimitTx(ctx, tx, owner, agent); err == nil {
		err = h.enforceGlobalLimitTx(ctx, tx)
	}
	if err != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, err
	}
	if err = tx.Commit(); err != nil {
		h.rememberGap(key, "persist_error", now.UnixMilli())
		return SessionHistoryCursor{}, err
	}
	h.clearPending(key, flushedGap)
	return SessionHistoryCursor{Generation: gen.value, Seq: seq}, nil
}

type historyGeneration struct {
	value string
	next  int64
}

func ensureHistoryGeneration(ctx context.Context, tx *sql.Tx, k historyKey) (historyGeneration, error) {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO session_history_generation(owner_key,agent_key_id,session_name,generation,next_seq) VALUES(?,?,?,?,1)`, k.owner, k.agent, k.session, randomHistoryID())
	if err != nil {
		return historyGeneration{}, err
	}
	var g historyGeneration
	err = tx.QueryRowContext(ctx, `SELECT generation,next_seq FROM session_history_generation WHERE owner_key=? AND agent_key_id=? AND session_name=?`, k.owner, k.agent, k.session).Scan(&g.value, &g.next)
	return g, err
}
func randomHistoryID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (h *SessionHistory) rememberGap(k historyKey, reason string, at int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.pending[k]
	if p.count == 0 {
		p.reason = reason
		p.started = at
	}
	p.count++
	p.ended = at
	h.pending[k] = p
}
func (h *SessionHistory) clearPending(k historyKey, flushed pendingHistoryGap) {
	if flushed.count == 0 {
		return
	}
	h.mu.Lock()
	if current, ok := h.pending[k]; ok && current == flushed {
		delete(h.pending, k)
	}
	h.mu.Unlock()
}
func (h *SessionHistory) flushOneGap(ctx context.Context, k historyKey) error {
	ctx, cancel := context.WithTimeout(ctx, SessionHistoryTxnLimit)
	defer cancel()
	tx, e := h.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var g historyGeneration
	g, e = ensureHistoryGeneration(ctx, tx, k)
	if e != nil {
		return e
	}
	flushed, flushErr := h.flushPendingTx(ctx, tx, k, g, time.Now().UnixMilli())
	if flushErr != nil {
		return flushErr
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	h.clearPending(k, flushed)
	return nil
}
func (h *SessionHistory) flushPendingTx(ctx context.Context, tx *sql.Tx, k historyKey, g historyGeneration, now int64) (pendingHistoryGap, error) {
	h.mu.Lock()
	p, ok := h.pending[k]
	h.mu.Unlock()
	if !ok {
		return pendingHistoryGap{}, nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO session_history_gap(gap_id,owner_key,agent_key_id,session_name,generation,after_seq,before_seq,started_at_ms,ended_at_ms,reason,lost_event_count) VALUES(?,?,?,?,?,?,NULL,?,?,?,?)`, randomHistoryID(), k.owner, k.agent, k.session, g.value, g.next-1, p.started, now, p.reason, p.count)
	if err != nil {
		return pendingHistoryGap{}, err
	}
	return p, nil
}

type historyRowKey struct {
	owner, agent, session, generation string
	seq, bytes, at                    int64
}

func (h *SessionHistory) deleteExpiredTx(ctx context.Context, tx *sql.Tx, cutoff int64, limit int) (int, error) {
	rows, e := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms FROM session_history_event WHERE received_at_ms<=? ORDER BY received_at_ms,owner_key,agent_key_id,session_name,seq LIMIT ?`, cutoff, limit)
	if e != nil {
		return 0, e
	}
	list, e := scanHistoryRows(rows)
	if e != nil {
		return 0, e
	}
	return len(list), deleteHistoryRows(ctx, tx, list, "expired")
}
func scanHistoryRows(rows *sql.Rows) ([]historyRowKey, error) {
	defer rows.Close()
	var out []historyRowKey
	for rows.Next() {
		var r historyRowKey
		if e := rows.Scan(&r.owner, &r.agent, &r.session, &r.generation, &r.seq, &r.bytes, &r.at); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func oldestRowsForEviction(rows []historyRowKey, bytesNeeded int64) []historyRowKey {
	if bytesNeeded <= 0 {
		return nil
	}
	var selected []historyRowKey
	var freed int64
	for _, row := range rows {
		selected = append(selected, row)
		freed += row.bytes
		if freed >= bytesNeeded {
			break
		}
	}
	return selected
}
func deleteHistoryRows(ctx context.Context, tx *sql.Tx, rows []historyRowKey, reason string) error {
	for _, r := range rows {
		if _, e := tx.ExecContext(ctx, `DELETE FROM session_history_event WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND seq=?`, r.owner, r.agent, r.session, r.generation, r.seq); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE session_history_generation SET retained_bytes=MAX(0,retained_bytes-?),gap_through_seq=MAX(gap_through_seq,?),oldest_seq=COALESCE((SELECT MIN(seq) FROM session_history_event WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=?),0) WHERE owner_key=? AND agent_key_id=? AND session_name=?`, r.bytes, r.seq, r.owner, r.agent, r.session, r.generation, r.owner, r.agent, r.session); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE session_history_runtime_state SET retained_bytes=MAX(0,retained_bytes-?) WHERE singleton=1`, r.bytes); e != nil {
			return e
		}
	}
	// A deletion batch has one bounded, non-content gap marker per affected session.
	groups := map[historyKey][]historyRowKey{}
	for _, r := range rows {
		k := historyKey{r.owner, r.agent, r.session}
		groups[k] = append(groups[k], r)
	}
	for k, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i].seq < g[j].seq })
		for i := 0; i < len(g); {
			j := i + 1
			for j < len(g) && g[j].generation == g[i].generation && g[j].seq == g[j-1].seq+1 {
				j++
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO session_history_gap(gap_id,owner_key,agent_key_id,session_name,generation,after_seq,before_seq,started_at_ms,ended_at_ms,reason,lost_event_count) VALUES(?,?,?,?,?,?,?, ?,?,?,?)`, randomHistoryID(), k.owner, k.agent, k.session, g[i].generation, g[i].seq-1, g[j-1].seq+1, g[i].at, g[j-1].at, reason, int64(j-i)); e != nil {
				return e
			}
			i = j
		}
	}
	return nil
}

func (h *SessionHistory) enforceAgentLimitTx(ctx context.Context, tx *sql.Tx, owner, agent string) error {
	for {
		var n int64
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(retained_bytes),0) FROM session_history_generation WHERE owner_key=? AND agent_key_id=?`, owner, agent).Scan(&n); e != nil {
			return e
		}
		if n <= SessionHistoryAgentMaxBytes {
			return nil
		}
		rows, e := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms FROM session_history_event WHERE owner_key=? AND agent_key_id=? ORDER BY received_at_ms,seq,session_name LIMIT ?`, owner, agent, SessionHistoryBatch)
		if e != nil {
			return e
		}
		batch, e := scanHistoryRows(rows)
		if e != nil {
			return e
		}
		batch = oldestRowsForEviction(batch, n-SessionHistoryAgentMaxBytes)
		if len(batch) == 0 {
			return errors.New("history agent quota counter inconsistent")
		}
		if e = deleteHistoryRows(ctx, tx, batch, "agent_capacity"); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
	}
}
func (h *SessionHistory) enforceGlobalLimitTx(ctx context.Context, tx *sql.Tx) error {
	for {
		var n int64
		if e := tx.QueryRowContext(ctx, `SELECT retained_bytes FROM session_history_runtime_state WHERE singleton=1`).Scan(&n); e != nil {
			return e
		}
		if n <= SessionHistoryGlobalMaxBytes {
			return nil
		}
		rows, e := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms FROM session_history_event ORDER BY received_at_ms,owner_key,agent_key_id,session_name,seq LIMIT ?`, SessionHistoryBatch)
		if e != nil {
			return e
		}
		batch, e := scanHistoryRows(rows)
		if e != nil {
			return e
		}
		batch = oldestRowsForEviction(batch, n-SessionHistoryGlobalMaxBytes)
		if len(batch) == 0 {
			return errors.New("history global quota counter inconsistent")
		}
		if e = deleteHistoryRows(ctx, tx, batch, "global_capacity"); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
	}
}

func (h *SessionHistory) Page(ctx context.Context, owner, agent, session, generation string, before *int64) (SessionHistoryPage, error) {
	if h == nil {
		return SessionHistoryPage{}, errors.New("history store unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, SessionHistoryTxnLimit)
	defer cancel()
	var enabled int
	if e := h.db.QueryRowContext(ctx, `SELECT enabled FROM session_history_runtime_state WHERE singleton=1`).Scan(&enabled); e != nil {
		return SessionHistoryPage{}, e
	}
	if enabled != 1 {
		return SessionHistoryPage{}, ErrSessionHistoryDisabled
	}
	var g historyGeneration
	var high, oldest, gapThrough int64
	e := h.db.QueryRowContext(ctx, `SELECT generation,next_seq-1,oldest_seq,gap_through_seq FROM session_history_generation WHERE owner_key=? AND agent_key_id=? AND session_name=?`, owner, agent, session).Scan(&g.value, &high, &oldest, &gapThrough)
	if errors.Is(e, sql.ErrNoRows) {
		return SessionHistoryPage{Events: []SessionHistoryEvent{}, Gaps: []SessionHistoryGap{}}, nil
	}
	if e != nil {
		return SessionHistoryPage{}, e
	}
	g.next = high + 1
	page := SessionHistoryPage{Generation: g.value, HighWater: high, Events: []SessionHistoryEvent{}, Gaps: []SessionHistoryGap{}}
	if generation != "" && generation != g.value {
		page.GapBefore = true
	}
	query := `SELECT seq,event_id,received_at_ms,payload_json FROM session_history_event WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=?`
	args := []any{owner, agent, session, g.value}
	if before != nil {
		query += ` AND seq<?`
		args = append(args, *before)
	}
	query += ` ORDER BY seq DESC LIMIT 100`
	rows, e := h.db.QueryContext(ctx, query, args...)
	if e != nil {
		return SessionHistoryPage{}, e
	}
	emptyPage, _ := json.Marshal(page)
	pageBytes := len(emptyPage) - 2 // replace the empty events array with encoded events.
	for rows.Next() {
		var ev SessionHistoryEvent
		var ms int64
		var raw []byte
		if e = rows.Scan(&ev.Cursor.Seq, &ev.EventID, &ms, &raw); e != nil {
			rows.Close()
			return SessionHistoryPage{}, e
		}
		ev.Cursor.Generation = g.value
		ev.ReceivedAt = time.UnixMilli(ms).UTC()
		ev.Event = append(json.RawMessage(nil), raw...)
		encoded, _ := json.Marshal(ev)
		separator := 0
		if len(page.Events) > 0 {
			separator = 1
		}
		if pageBytes+separator+len(encoded) > 4<<20 {
			break
		}
		page.Events = append(page.Events, ev)
		pageBytes += separator + len(encoded)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return SessionHistoryPage{}, e
	}
	rows.Close()
	sort.Slice(page.Events, func(i, j int) bool { return page.Events[i].Cursor.Seq < page.Events[j].Cursor.Seq })
	if len(page.Events) > 0 {
		v := page.Events[0].Cursor.Seq
		if v > 1 {
			page.BeforeSeq = &v
		}
	}
	var exists int
	threshold := high - 99
	if len(page.Events) > 0 {
		threshold = page.Events[0].Cursor.Seq
	} else if before != nil {
		threshold = *before
	}
	e = h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM session_history_event WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND seq<?)`, owner, agent, session, g.value, threshold).Scan(&exists)
	if e != nil {
		return SessionHistoryPage{}, e
	}
	page.HasOlder = exists == 1
	if before != nil && oldest > 0 && *before <= oldest {
		page.GapBefore = true
	}
	if gapThrough > 0 && before != nil && *before <= gapThrough+1 {
		page.GapBefore = true
	}
	maxSeq := high + 1
	if len(page.Events) > 0 {
		maxSeq = page.Events[len(page.Events)-1].Cursor.Seq
	}
	gapRows, e := h.db.QueryContext(ctx, `SELECT after_seq,before_seq,started_at_ms,ended_at_ms,reason,lost_event_count FROM session_history_gap WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND (before_seq IS NULL OR before_seq<=?) ORDER BY started_at_ms DESC LIMIT 100`, owner, agent, session, g.value, maxSeq)
	if e != nil {
		return SessionHistoryPage{}, e
	}
	for gapRows.Next() {
		var gap SessionHistoryGap
		var bs, ended sql.NullInt64
		var start int64
		if e = gapRows.Scan(&gap.AfterSeq, &bs, &start, &ended, &gap.Reason, &gap.LostEvents); e != nil {
			gapRows.Close()
			return SessionHistoryPage{}, e
		}
		gap.StartedAt = time.UnixMilli(start).UTC()
		if bs.Valid {
			gap.BeforeSeq = &bs.Int64
		}
		if ended.Valid {
			t := time.UnixMilli(ended.Int64).UTC()
			gap.EndedAt = &t
		}
		page.Gaps = append(page.Gaps, gap)
	}
	if e = gapRows.Err(); e != nil {
		gapRows.Close()
		return SessionHistoryPage{}, e
	}
	gapRows.Close()
	sort.Slice(page.Gaps, func(i, j int) bool { return page.Gaps[i].StartedAt.Before(page.Gaps[j].StartedAt) })
	for len(page.Events) > 0 {
		body, _ := json.Marshal(page)
		if len(body) <= 4<<20 {
			break
		}
		removedSeq := page.Events[0].Cursor.Seq
		page.Events = page.Events[1:]
		v := removedSeq + 1 // exclusive cursor that includes the removed row on the next page
		page.BeforeSeq = &v
		page.HasOlder = true
	}
	if e = ctx.Err(); e != nil {
		return SessionHistoryPage{}, e
	}
	return page, nil
}

var ErrSessionHistoryDisabled = errors.New("session history disabled")
var ErrSessionHistoryOversize = errors.New("session history event exceeds size limit")

// RequestAgentDeletion makes body deletion durable and retryable in the history DB.
func (h *SessionHistory) RequestAgentDeletion(ctx context.Context, owner, agent string, now time.Time) error {
	if h == nil {
		return nil
	}
	requested := now.UTC().UnixMilli()
	_, e := h.db.ExecContext(ctx, `INSERT INTO session_history_delete_queue(owner_key,agent_key_id,requested_at_ms) VALUES(?,?,?) ON CONFLICT(owner_key,agent_key_id) DO UPDATE SET requested_at_ms=excluded.requested_at_ms`, owner, agent, requested)
	if e != nil {
		h.mu.Lock()
		h.pendingDeletes[historyDeleteKey{owner, agent}] = requested
		h.mu.Unlock()
	}
	return e
}

// CleanupBatch runs at most 500 TTL deletions and one bounded queued identity purge.
func (h *SessionHistory) CleanupBatch(ctx context.Context, now time.Time) error {
	if h == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, SessionHistoryTxnLimit)
	defer cancel()
	tx, e := h.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	h.mu.Lock()
	deletes := make(map[historyDeleteKey]int64, len(h.pendingDeletes))
	for k, v := range h.pendingDeletes {
		deletes[k] = v
	}
	h.mu.Unlock()
	keys := make([]historyDeleteKey, 0, len(deletes))
	for k := range deletes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].owner == keys[j].owner {
			return keys[i].agent < keys[j].agent
		}
		return keys[i].owner < keys[j].owner
	})
	if len(keys) > SessionHistoryBatch {
		keys = keys[:SessionHistoryBatch]
	}
	for _, k := range keys {
		if _, e = tx.ExecContext(ctx, `INSERT INTO session_history_delete_queue(owner_key,agent_key_id,requested_at_ms) VALUES(?,?,?) ON CONFLICT(owner_key,agent_key_id) DO UPDATE SET requested_at_ms=MAX(requested_at_ms,excluded.requested_at_ms)`, k.owner, k.agent, deletes[k]); e != nil {
			return e
		}
	}
	var deleted int
	var enabled int
	if e = tx.QueryRowContext(ctx, `SELECT enabled FROM session_history_runtime_state WHERE singleton=1`).Scan(&enabled); e != nil {
		return e
	}
	if enabled == 1 {
		var cleanupErr error
		deleted, cleanupErr = h.deleteExpiredTx(ctx, tx, now.Add(-SessionHistoryRetain).UnixMilli(), SessionHistoryBatch)
		if cleanupErr != nil {
			return cleanupErr
		}
	}
	var owner, agent string
	e = tx.QueryRowContext(ctx, `SELECT owner_key,agent_key_id FROM session_history_delete_queue ORDER BY requested_at_ms LIMIT 1`).Scan(&owner, &agent)
	if e == nil {
		if deleted < SessionHistoryBatch {
			remaining := SessionHistoryBatch - deleted
			rows, er := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms FROM session_history_event WHERE owner_key=? AND agent_key_id=? ORDER BY received_at_ms,session_name,seq LIMIT ?`, owner, agent, remaining)
			if er != nil {
				return er
			}
			batch, er := scanHistoryRows(rows)
			if er != nil {
				return er
			}
			if len(batch) == 0 {
				_, er = tx.ExecContext(ctx, `DELETE FROM session_history_delete_queue WHERE owner_key=? AND agent_key_id=?`, owner, agent)
			} else {
				er = deleteHistoryRows(ctx, tx, batch, "identity_revoked")
				if er == nil {
					if len(batch) < remaining {
						_, er = tx.ExecContext(ctx, `DELETE FROM session_history_delete_queue WHERE owner_key=? AND agent_key_id=?`, owner, agent)
					} else {
						_, er = tx.ExecContext(ctx, `UPDATE session_history_delete_queue SET attempts=attempts+1 WHERE owner_key=? AND agent_key_id=?`, owner, agent)
					}
				}
			}
			if er != nil {
				return er
			}
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	h.mu.Lock()
	for _, k := range keys {
		if h.pendingDeletes[k] == deletes[k] {
			delete(h.pendingDeletes, k)
		}
	}
	h.mu.Unlock()
	return h.flushPending(ctx)
}

func (h *SessionHistory) flushPending(ctx context.Context) error {
	h.mu.Lock()
	keys := make([]historyKey, 0, len(h.pending))
	for k := range h.pending {
		keys = append(keys, k)
	}
	h.mu.Unlock()
	for _, k := range keys {
		if e := h.flushOneGap(ctx, k); e != nil {
			log.Printf("session_history_gap_persist_failed owner=%q agent=%q session=%q error_type=%T", k.owner, k.agent, k.session, e)
			continue
		}
	}
	return nil
}

// HistoryStats is intended for tests/operations and never exposes event content.
func (h *SessionHistory) HistoryStats(ctx context.Context) (int64, int64, error) {
	var rows, bytes int64
	e := h.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(record_bytes),0) FROM session_history_event`).Scan(&rows, &bytes)
	return rows, bytes, e
}
