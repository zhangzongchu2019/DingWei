package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const sessionHistoryPerfBudget = 10 * time.Millisecond
const sessionHistoryPerfTimerCoverageFloor = time.Millisecond

func sessionHistoryPerfOverBudget(additional time.Duration) bool {
	return additional > sessionHistoryPerfBudget
}

func sessionHistoryPerfTimerMissedPersistence(enabledP99 time.Duration) bool {
	return enabledP99 < sessionHistoryPerfTimerCoverageFloor
}

func TestSessionHistoryPerformanceGuardsAreLive(t *testing.T) {
	if sessionHistoryPerfOverBudget(sessionHistoryPerfBudget) {
		t.Fatal("exact performance budget must pass")
	}
	if !sessionHistoryPerfOverBudget(sessionHistoryPerfBudget + time.Nanosecond) {
		t.Fatal("over-budget performance sample was not rejected")
	}
	if sessionHistoryPerfTimerMissedPersistence(sessionHistoryPerfTimerCoverageFloor) {
		t.Fatal("timer coverage floor boundary must pass")
	}
	if !sessionHistoryPerfTimerMissedPersistence(0) {
		t.Fatal("timer that excludes the durable write was not rejected")
	}
}

func TestSessionHistoryDisabledPageHasStableResponse(t *testing.T) {
	h := New(nil, ConsoleAuthMode{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/view/work/history", nil)
	r.SetPathValue("sessionName", "work")
	h.HandleSessionHistory(w, r)
	if w.Code != 503 || w.Body.String() != `{"code":"history_feature_disabled","message":"未启用"}` {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSessionHistoryRouteRequiresAgentOwner(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.consoleAuthEnabled = true
	now := time.Now()
	for _, principal := range []string{"owner", "delegate"} {
		if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: principal, Provider: "feishu", ProviderIssuer: "app", ProviderSubject: principal, DisplayName: principal, Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	agent := model.ConsoleAgent{ID: "agent", KeyID: "key", SessionName: "work", OwnerPrincipal: "owner", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "owner", AgentID: agent.ID, Permission: "view", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "delegate", AgentID: agent.ID, Permission: "view", Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, principal := range []string{"owner", "delegate"} {
		raw := "token-" + principal
		if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "session-" + principal, PrincipalID: principal, TokenHash: hashConsoleToken(raw), ExpiresAt: now.Add(time.Hour)}, now); err != nil {
			t.Fatal(err)
		}
	}
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.SetEnabled(ctx, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := history.Append(ctx, "owner", "key", "work", "event", "assistant_text", []byte(`{"kind":"assistant_text"}`), now); err != nil {
		t.Fatal(err)
	}
	if err := history.SetEnabled(ctx, false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := history.SetEnabled(ctx, true, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	hub.SessionHistory, hub.SessionHistoryEnabled = history, true
	wrapped := hub.RequireConsoleAgent("view", hub.HandleSessionHistory)
	for _, tc := range []struct {
		principal string
		want      int
	}{{"owner", 200}, {"delegate", 404}} {
		req := httptest.NewRequest("GET", "/view/work/history", nil)
		req.SetPathValue("sessionName", "work")
		req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: "token-" + tc.principal})
		w := httptest.NewRecorder()
		wrapped(w, req)
		if w.Code != tc.want {
			t.Errorf("principal=%s status=%d body=%s", tc.principal, w.Code, w.Body.String())
		}
		if tc.principal == "owner" && !strings.Contains(w.Body.String(), `"reason":"feature_disabled"`) {
			t.Errorf("history API omitted feature-disabled gap: %s", w.Body.String())
		}
	}
}

func TestSessionHistoryDisabledKeepsEventRingPayloadAndSkipsWrites(t *testing.T) {
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	h := New(nil, ConsoleAuthMode{})
	h.SessionHistory = history
	body := `{"kind":"assistant_text","text":"hello"}`
	c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
	if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: "event-1", Body: body, Meta: map[string]any{"type": terminalSessionEventType}}) {
		t.Fatal("event rejected")
	}
	st := h.terminals[terminalKey("key", "work")]
	if st == nil || len(st.eventRing) != 1 || st.eventRing[0] != body {
		t.Fatalf("off payload changed: %#v", st)
	}
	rows, _, err := history.HistoryStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("disabled writes=%d", rows)
	}
}

func TestSessionHistoryDisabledDirectPublishAndSSEMatchR49Bodies(t *testing.T) {
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	h := New(nil, ConsoleAuthMode{})
	h.SessionHistory = history // default-off state
	first, second := `{"kind":"assistant_text","text":"ingress"}`, `{"kind":"tool_call","text":"publish"}`
	c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
	if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: "e1", Body: first, Meta: map[string]any{"type": terminalSessionEventType}}) {
		t.Fatal("session event rejected")
	}
	h.publishSessionEvent("key", "work", second)
	st := h.terminals[terminalKey("key", "work")]
	if len(st.eventRing) != 2 || st.eventRing[0] != first || st.eventRing[1] != second {
		t.Fatalf("ring=%q", st.eventRing)
	}
	h.sessionClients = map[string]map[string]*sessionClient{"key": {"work": c}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/internal/view/work/events", nil).WithContext(ctx)
	req.SetPathValue("session", "work")
	w := httptest.NewRecorder()
	h.HandleInternalViewEvents(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "data: "+first+"\n\n") || !strings.Contains(w.Body.String(), "data: "+second+"\n\n") {
		t.Fatalf("SSE code=%d body=%q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"_history"`) || strings.Contains(w.Body.String(), `"_history_gap"`) {
		t.Fatalf("disabled SSE changed r49 event payload: %q", w.Body.String())
	}
	rows, _, err := history.HistoryStats(context.Background())
	if err != nil || rows != 0 {
		t.Fatalf("disabled history rows=%d err=%v", rows, err)
	}
}

func TestSessionHistoryUnavailableDirectPublishAndSSEMatchR49Bodies(t *testing.T) {
	h := New(nil, ConsoleAuthMode{})
	first, second := `{"kind":"assistant_text","text":"ingress"}`, `{"kind":"tool_call","text":"publish"}`
	c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
	if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: "e1", Body: first, Meta: map[string]any{"type": terminalSessionEventType}}) {
		t.Fatal("session event rejected")
	}
	h.publishSessionEvent("key", "work", second)
	st := h.terminals[terminalKey("key", "work")]
	if len(st.eventRing) != 2 || st.eventRing[0] != first || st.eventRing[1] != second {
		t.Fatalf("ring=%q", st.eventRing)
	}
	h.sessionClients = map[string]map[string]*sessionClient{"key": {"work": c}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/internal/view/work/events", nil).WithContext(ctx)
	req.SetPathValue("session", "work")
	w := httptest.NewRecorder()
	h.HandleInternalViewEvents(w, req)
	var gotEvents []string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			gotEvents = append(gotEvents, strings.TrimPrefix(line, "data: "))
		}
	}
	wantEvents := []string{first, second}
	if w.Code != http.StatusOK || strings.Join(gotEvents, "\x00") != strings.Join(wantEvents, "\x00") {
		t.Fatalf("unavailable-store event payloads differ from r49: code=%d got=%q want=%q", w.Code, gotEvents, wantEvents)
	}
}

func TestSessionHistoryEnabledPersistsBeforePublishingCursor(t *testing.T) {
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.SetEnabled(context.Background(), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	h := New(nil, ConsoleAuthMode{})
	h.SessionHistory, h.SessionHistoryEnabled = history, true
	body := `{"kind":"assistant_text","text":"hello"}`
	c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
	if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: "event-1", Body: body, Meta: map[string]any{"type": terminalSessionEventType}}) {
		t.Fatal("event rejected")
	}
	st := h.terminals[terminalKey("key", "work")]
	if len(st.eventRing) != 1 || !strings.Contains(st.eventRing[0], `"_history"`) {
		t.Fatalf("live event lacks cursor: %q", st.eventRing)
	}
	page, err := history.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].EventID != "event-1" {
		t.Fatalf("history page=%+v", page)
	}
	var live map[string]any
	if err := json.Unmarshal([]byte(st.eventRing[0]), &live); err != nil {
		t.Fatal(err)
	}
	if live["_history"] == nil {
		t.Fatal("persisted event was published without history cursor")
	}
}

func TestSessionHistoryConcurrentGroupCursorMatchesLiveEvents(t *testing.T) {
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.SetEnabled(context.Background(), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	h := New(nil, ConsoleAuthMode{})
	h.SessionHistory, h.SessionHistoryEnabled = history, true
	const count = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
			if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: fmt.Sprintf("live-%02d", i), Body: `{"kind":"assistant_text"}`, Meta: map[string]any{"type": terminalSessionEventType}}) {
				t.Errorf("event %d rejected", i)
			}
		}()
	}
	close(start)
	wg.Wait()
	page, err := history.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil || len(page.Events) != count {
		t.Fatalf("history count=%d err=%v", len(page.Events), err)
	}
	stored := make(map[string]store.SessionHistoryCursor, count)
	for _, event := range page.Events {
		stored[event.EventID] = event.Cursor
	}
	h.mu.Lock()
	state := h.terminals[terminalKey("key", "work")]
	live := append([]string(nil), state.eventRing...)
	h.mu.Unlock()
	if len(live) != count {
		t.Fatalf("live event count=%d", len(live))
	}
	for _, raw := range live {
		var event struct {
			History struct {
				Generation string `json:"generation"`
				Seq        int64  `json:"seq"`
				EventID    string `json:"event_id"`
			} `json:"_history"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		cursor, ok := stored[event.History.EventID]
		if !ok || cursor.Generation != event.History.Generation || cursor.Seq != event.History.Seq {
			t.Errorf("live cursor differs from committed row: live=%+v stored=%+v exists=%t", event.History, cursor, ok)
		}
	}
}

func TestSessionHistoryTwentyAgentPublishP99(t *testing.T) {
	if os.Getenv("DW_SESSION_HISTORY_PERF") != "1" {
		t.Skip("set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate")
	}
	const agents, pushes = 20, 25
	measure := func(enabled bool) []time.Duration {
		h := New(nil, ConsoleAuthMode{})
		if enabled {
			history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = history.Close() })
			if err := history.SetEnabled(context.Background(), true, time.Now()); err != nil {
				t.Fatal(err)
			}
			h.SessionHistory, h.SessionHistoryEnabled = history, true
		}
		latencies := make([]time.Duration, agents*pushes)
		var wg sync.WaitGroup
		for a := 0; a < agents; a++ {
			a := a
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := &sessionClient{keyID: AgentKeyID("owner"), ownerKey: "owner", sessionName: fmt.Sprintf("agent-%02d", a), metricsAgentID: fmt.Sprintf("agent-%02d", a)}
				for n := 0; n < pushes; n++ {
					start := time.Now()
					h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: fmt.Sprintf("%d-%d", a, n), Body: `{"kind":"assistant_text","text":"performance probe"}`, Meta: map[string]any{"type": terminalSessionEventType}})
					latencies[a*pushes+n] = time.Since(start)
				}
			}()
		}
		wg.Wait()
		return latencies
	}
	baseline, enabled := measure(false), measure(true)
	pairedAdded := make([]time.Duration, len(baseline))
	for i := range pairedAdded {
		pairedAdded[i] = enabled[i] - baseline[i]
	}
	percentile := func(values []time.Duration) time.Duration {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		return values[(len(values)*99+99)/100-1]
	}
	baselineP99, enabledP99, additionalP99 := percentile(baseline), percentile(enabled), percentile(pairedAdded)
	t.Logf("agents=%d pushes_per_agent=%d total=%d baseline_p99=%s enabled_p99=%s paired_additional_p99=%s threshold=%s", agents, pushes, len(baseline), baselineP99, enabledP99, additionalP99, 10*time.Millisecond)
	if sessionHistoryPerfTimerMissedPersistence(enabledP99) {
		t.Errorf("enabled p99 %s is too small to include the synchronous durable append in the timed publish path", enabledP99)
	}
	if sessionHistoryPerfOverBudget(additionalP99) {
		t.Errorf("session history publish-path overhead p99 %s exceeds %s threshold", additionalP99, sessionHistoryPerfBudget)
	}
}

func TestSessionHistoryOversizeStillPublishesAndMarksGap(t *testing.T) {
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.SetEnabled(context.Background(), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	h := New(nil, ConsoleAuthMode{})
	h.SessionHistory, h.SessionHistoryEnabled = history, true
	c := &sessionClient{keyID: "key", ownerKey: "owner", sessionName: "work", metricsAgentID: "agent"}
	body := `{"kind":"assistant_text","text":"` + strings.Repeat("x", int(store.SessionHistoryEventMaxBytes)) + `"}`
	if !h.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: "oversize", Body: body, Meta: map[string]any{"type": terminalSessionEventType}}) {
		t.Fatal("oversize event was rejected from live path")
	}
	st := h.terminals[terminalKey("key", "work")]
	if len(st.eventRing) != 1 || !strings.Contains(st.eventRing[0], `"_history_gap"`) {
		t.Fatal("oversize live event was truncated or missing gap marker")
	}
	page, err := history.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 0 || len(page.Gaps) != 1 || page.Gaps[0].Reason != "oversize" {
		t.Fatalf("oversize gap=%+v", page.Gaps)
	}
}
