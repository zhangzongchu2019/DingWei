package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func testAMRProcessor() *amrProcessor {
	return &amrProcessor{
		queue:        make(chan amrRouteJob, 8),
		noticeQueue:  make(chan amrNoticeJob, 8),
		turns:        map[amrTurnKey]*amrTurn{},
		completed:    map[amrTurnKey]time.Time{},
		notices:      map[string]time.Time{},
		modes:        map[string]amrMode{"source": amrObserve},
		maxTurns:     8,
		maxBytes:     1024,
		routeBodyMax: 100_000,
		maxCompleted: 32,
		turnTTL:      time.Minute,
		routeTimeout: time.Second,
		maxTargets:   20,
	}
}

func amrEventJSON(t testing.TB, event amrSessionEvent) string {
	t.Helper()
	b, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAMRWaitsForExistingTurnCompletedAndAggregatesFragments(t *testing.T) {
	p := testAMRProcessor()
	base := amrSessionEvent{DeliveryID: "d1", TurnID: "t1"}
	first := base
	first.Kind, first.Text, first.Cursor = "assistant_text", "@target ", "c1"
	p.ingest("key", "source", amrEventJSON(t, first))
	select {
	case <-p.queue:
		t.Fatal("routed before terminal")
	default:
	}
	second := base
	// Codex currently reuses an item-level cursor across multiple deltas; cursor
	// equality therefore must not suppress a distinct fragment.
	second.Kind, second.Text, second.Cursor = "assistant_text", "full body", "c1"
	p.ingest("key", "source", amrEventJSON(t, second))
	done := base
	done.Kind, done.Cursor, done.Data = "state_change", "c3", map[string]any{"change": "turn_completed", "status": "completed"}
	p.ingest("key", "source", amrEventJSON(t, done))
	select {
	case job := <-p.queue:
		if job.body != "@target full body" {
			t.Fatalf("body=%q", job.body)
		}
	default:
		t.Fatal("completed turn was not queued")
	}
	// A replayed/duplicate terminal cannot resurrect the completed turn.
	p.ingest("key", "source", amrEventJSON(t, done))
	select {
	case <-p.queue:
		t.Fatal("duplicate terminal queued a second route")
	default:
	}
}

func TestAMRFragmentClassifierHandlesDuplicateSnapshotAndAppend(t *testing.T) {
	p := testAMRProcessor()
	events := []amrSessionEvent{
		{Kind: "assistant_text", Text: "@target ", Cursor: "1", DeliveryID: "d", TurnID: "t"},
		{Kind: "assistant_text", Text: "@target ", Cursor: "2", DeliveryID: "d", TurnID: "t"},
		{Kind: "assistant_text", Text: "@target full", Cursor: "3", DeliveryID: "d", TurnID: "t"},
		{Kind: "assistant_text", Text: " body", Cursor: "4", DeliveryID: "d", TurnID: "t"},
		{Kind: "state_change", Cursor: "5", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "d", TurnID: "t"},
	}
	for _, event := range events {
		p.ingest("key", "source", amrEventJSON(t, event))
	}
	job := <-p.queue
	if job.body != "@target full body" {
		t.Fatalf("body=%q", job.body)
	}
	stats, active := p.snapshotStats()
	if active != 0 || stats.fragmentDuplicate != 1 || stats.fragmentSnapshot != 1 || stats.fragmentAppend != 2 {
		t.Fatalf("stats=%+v active=%d", stats, active)
	}
}

func TestAMRFailClosedForReplayMissingIdentityAndNoTerminal(t *testing.T) {
	p := testAMRProcessor()
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "@target x", Data: map[string]any{"replay": true}, DeliveryID: "d", TurnID: "t"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "@target x", DeliveryID: "d"}))
	if len(p.turns) != 0 || len(p.queue) != 0 {
		t.Fatalf("invalid event accepted: turns=%d jobs=%d", len(p.turns), len(p.queue))
	}
	// A valid fragment without a terminal remains active and never queues.
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "@target half", DeliveryID: "d2", TurnID: "t2"}))
	if len(p.turns) != 1 || len(p.queue) != 0 {
		t.Fatalf("unterminated turn state: turns=%d jobs=%d", len(p.turns), len(p.queue))
	}
}

func TestAMRIncompleteAndFailedRemainDistinct(t *testing.T) {
	p := testAMRProcessor()
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "empty", TurnID: "t"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "ok", DeliveryID: "failed", TurnID: "t"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "failed"}, DeliveryID: "failed", TurnID: "t"}))
	stats, active := p.snapshotStats()
	if active != 0 || stats.opened != 2 || stats.incomplete != 1 || stats.failed != 1 {
		t.Fatalf("stats=%+v active=%d", stats, active)
	}
	if stats.opened != uint64(active)+stats.queued+stats.closed+stats.incomplete+stats.failed {
		t.Fatalf("top-level conservation failed: stats=%+v active=%d", stats, active)
	}
}

func TestAMRTerminalSuccessIsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want bool
	}{
		{name: "missing outcome", data: map[string]any{}, want: false},
		{name: "claude success", data: map[string]any{"subtype": "success"}, want: true},
		{name: "completed", data: map[string]any{"status": "completed"}, want: true},
		{name: "claude interrupted subtype", data: map[string]any{"subtype": "error_during_execution"}, want: false},
		{name: "explicit error", data: map[string]any{"is_error": true}, want: false},
		{name: "unknown status", data: map[string]any{"status": "future_state"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := amrTerminalSucceeded(tt.data); got != tt.want {
				t.Fatalf("amrTerminalSucceeded(%v)=%t want %t", tt.data, got, tt.want)
			}
		})
	}
}

func TestAMRUnknownTerminalValuesAreCounted(t *testing.T) {
	p := testAMRProcessor()
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "future/state"}, DeliveryID: "d", TurnID: "t"}))
	stats, _ := p.snapshotStats()
	if stats.failed != 1 || stats.terminalUnknown["status=future_state"] != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestAMRTTLClosesOpenCodeLikeTurnAsIncomplete(t *testing.T) {
	p := testAMRProcessor()
	p.turnTTL = time.Second
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "@target unfinished", DeliveryID: "d", TurnID: "t"}))
	p.mu.Lock()
	p.expireLocked(time.Now().UTC().Add(2 * time.Second))
	p.mu.Unlock()
	stats, active := p.snapshotStats()
	if active != 0 || stats.opened != 1 || stats.incomplete != 1 || len(p.queue) != 0 {
		t.Fatalf("stats=%+v active=%d jobs=%d", stats, active, len(p.queue))
	}
}

func TestAMRExpiryUsesBoundedNoticeQueue(t *testing.T) {
	p := testAMRProcessor()
	p.noticeQueue = make(chan amrNoticeJob, 1)
	p.turnTTL = time.Second
	for _, delivery := range []string{"one", "two"} {
		p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "x", DeliveryID: delivery, TurnID: "t"}))
	}
	p.mu.Lock()
	p.expireLocked(time.Now().UTC().Add(2 * time.Second))
	p.mu.Unlock()
	stats, _ := p.snapshotStats()
	if len(p.noticeQueue) != 1 || stats.notificationsSuppressed != 1 {
		t.Fatalf("notice_queue=%d stats=%+v", len(p.noticeQueue), stats)
	}
}

func TestAMRTruncatesAtHeadAndStillQueuesAtTerminal(t *testing.T) {
	p := testAMRProcessor()
	p.maxBytes = 64
	p.routeBodyMax = 128
	text := "@target " + strings.Repeat("x", 200)
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: text, DeliveryID: "d", TurnID: "t"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "d", TurnID: "t"}))
	job := <-p.queue
	if len(job.body) > p.routeBodyMax || !strings.HasPrefix(job.body, "@target ") || !strings.Contains(job.body, "Hub") {
		t.Fatalf("truncated body=%q bytes=%d", job.body, len(job.body))
	}
}

func TestAMRLateAfterTerminalIsCounted(t *testing.T) {
	p := testAMRProcessor()
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "d", TurnID: "t"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "late", DeliveryID: "d", TurnID: "t"}))
	stats, _ := p.snapshotStats()
	if stats.lateAfterTerminal != 1 {
		t.Fatalf("lateAfterTerminal=%d", stats.lateAfterTerminal)
	}
}

func TestAMRPerSessionCapacityEvictsOldest(t *testing.T) {
	p := testAMRProcessor()
	p.maxTurns = 2
	base := time.Now().UTC()
	for i, delivery := range []string{"old", "middle", "new"} {
		p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "x", DeliveryID: delivery, TurnID: "t"}))
		key := amrTurnKey{KeyID: "key", Session: "source", DeliveryID: delivery, TurnID: "t"}
		p.mu.Lock()
		p.turns[key].updated = base.Add(time.Duration(i) * time.Millisecond)
		p.mu.Unlock()
	}
	if len(p.turns) != 2 {
		t.Fatalf("active turns=%d", len(p.turns))
	}
	if _, ok := p.turns[amrTurnKey{KeyID: "key", Session: "source", DeliveryID: "old", TurnID: "t"}]; ok {
		t.Fatal("oldest turn was not evicted")
	}
	stats, active := p.snapshotStats()
	if stats.incomplete != 1 || stats.opened != uint64(active)+stats.queued+stats.closed+stats.incomplete+stats.failed {
		t.Fatalf("stats=%+v active=%d", stats, active)
	}
}

func TestAMRRouteAttemptsEveryTargetAndCountsPartialSuccess(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-amr-multi", Name: "svc-amr-multi", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc-amr-multi", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "owner", DisplayName: "Owner", FeishuOpenID: "ou_owner", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.sessionClients[key.ID] = map[string]*sessionClient{
		"one":   {keyID: key.ID, sessionName: "one"},
		"two":   {keyID: key.ID, sessionName: "two"},
		"three": {keyID: key.ID, sessionName: "three"},
	}
	hub.mu.Unlock()
	p := testAMRProcessor()
	p.hub = hub
	p.modes = map[string]amrMode{"source": amrRoute}
	attempted := 0
	p.routeEnvelope = func(_ context.Context, env model.Envelope) error {
		attempted++
		if strings.HasPrefix(env.To, "two#") {
			return errors.New("offline")
		}
		return nil
	}
	p.mu.Lock()
	p.stats.opened, p.stats.queued = 1, 1
	p.mu.Unlock()
	p.process(amrRouteJob{key: amrTurnKey{KeyID: key.ID, Session: "source", DeliveryID: "d", TurnID: "t"}, body: "@one @two @three hello"})
	stats, active := p.snapshotStats()
	if attempted != 3 || stats.targetsAttempted != 3 || stats.targetsDispatched != 2 || stats.targetsFailed != 1 || stats.closedDispatched != 1 {
		t.Fatalf("attempted=%d stats=%+v", attempted, stats)
	}
	if stats.opened != uint64(active)+stats.queued+stats.closed+stats.incomplete+stats.failed {
		t.Fatalf("top-level conservation failed: stats=%+v active=%d", stats, active)
	}
}

func TestAMRStateConservationAfterPositiveCandidate(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-amr", Name: "svc-amr", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc-amr", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "owner", DisplayName: "Owner", FeishuOpenID: "ou_owner", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.sessionClients[key.ID] = map[string]*sessionClient{"target": {keyID: key.ID, sessionName: "target"}}
	hub.mu.Unlock()
	p := testAMRProcessor()
	p.hub = hub
	job := amrRouteJob{key: amrTurnKey{KeyID: key.ID, Session: "source", DeliveryID: "d", TurnID: "t"}, body: "@target positive control"}
	p.mu.Lock()
	p.stats.opened, p.stats.queued = 1, 1
	p.mu.Unlock()
	p.process(job)
	stats, active := p.snapshotStats()
	if stats.closedCandidateReady != 1 {
		t.Fatalf("positive control did not reach candidate_ready: %+v", stats)
	}
	if stats.opened != uint64(active)+stats.queued+stats.closed+stats.incomplete+stats.failed {
		t.Fatalf("top-level conservation failed: stats=%+v active=%d", stats, active)
	}
	if stats.closed != stats.closedNonCandidate+stats.closedCandidateRejected+stats.closedCandidateReady+stats.closedDispatched {
		t.Fatalf("closed conservation failed: stats=%+v", stats)
	}
}

func TestParseAMRCandidatesUsesWholeTurnAndRejectsInvalidTokens(t *testing.T) {
	got := parseAMRCandidates("@alpha @beta! do the work", "source", 20)
	if len(got) != 2 || got[0].selector != "#alpha" || got[1].selector != "#beta" || got[1].replyPolicy != "expect_reply" || got[0].body != "@alpha @beta! do the work" {
		t.Fatalf("candidates=%+v", got)
	}
	if got := parseAMRCandidates("prefix @alpha body", "source", 20); len(got) != 1 || got[0].selector != "#alpha" || got[0].body != "prefix @alpha body" {
		t.Fatalf("mid-body mention not routed with whole turn: %+v", got)
	}
	if got := parseAMRCandidates("@na$me body", "source", 20); len(got) != 0 {
		t.Fatalf("invalid token partially consumed: %+v", got)
	}
	if got := parseAMRCandidates("@source body", "source", 20); len(got) != 0 {
		t.Fatalf("self mention routed: %+v", got)
	}
}

func TestParseAMRMentionGuardsAndCounters(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		want     int
		found    int
		leading  int
		embedded int
		inCode   int
		escaped  int
	}{
		{name: "email address", text: "zzc@zhangzongchu.com 出现在正文", want: 0},
		{name: "css unresolved candidate", text: "CSS @media screen", want: 1, found: 1, embedded: 1},
		{name: "python decorator candidate", text: "Python @property", want: 1, found: 1, embedded: 1},
		{name: "inline code", text: "引用 `@name` 不投递", want: 0, inCode: 1},
		{name: "fenced code", text: "```\n@name body\n```", want: 0, inCode: 1},
		{name: "escaped", text: "@@opus50ts 字面量", want: 0, escaped: 1},
		{name: "leading", text: "@alpha body", want: 1, found: 1, leading: 1},
		{name: "embedded", text: "请处理 @alpha body", want: 1, found: 1, embedded: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, scan := parseAMRCandidatesDetailed(tt.text, "source", 20)
			if len(got) != tt.want || scan.found != tt.found || scan.leading != tt.leading || scan.embedded != tt.embedded || scan.inCode != tt.inCode || scan.escaped != tt.escaped {
				t.Fatalf("candidates=%+v scan=%+v", got, scan)
			}
		})
	}
}

func TestAMRUnresolvedMentionIsCountedAndNotDispatched(t *testing.T) {
	hub, _, _ := newTestHub(t)
	p := testAMRProcessor()
	p.hub = hub
	for i, body := range []string{"CSS @media screen", "Python @property"} {
		p.stats.opened++
		p.stats.queued++
		p.process(amrRouteJob{key: amrTurnKey{KeyID: "key", Session: "source", DeliveryID: fmt.Sprintf("d%d", i), TurnID: "t"}, body: body})
	}
	stats, _ := p.snapshotStats()
	if stats.mentionUnresolved != 2 || stats.mentionEmbedded != 2 || stats.closedCandidateRejected != 2 || stats.targetsDispatched != 0 {
		t.Fatalf("unresolved mention stats=%+v", stats)
	}
}

func TestLegacyAgentRouteDroppedOnlyForRouteEnabledSession(t *testing.T) {
	p := testAMRProcessor()
	p.modes = map[string]amrMode{"observe": amrObserve, "key/route": amrRoute}
	env := model.Envelope{Meta: map[string]any{"type": "agent_route", "no_mirror": true}}
	if p.shouldDropLegacyAgentRoute("key", "observe", env) {
		t.Fatal("observe mode dropped the legacy comparison path")
	}
	if !p.shouldDropLegacyAgentRoute("key", "route", env) {
		t.Fatal("route mode did not suppress legacy double-send path")
	}
	if p.shouldDropLegacyAgentRoute("key", "route", model.Envelope{Meta: map[string]any{"type": "other"}}) {
		t.Fatal("route mode dropped a non-agent-route envelope")
	}
	if p.shouldDropLegacyAgentRoute("key", "route", model.Envelope{Meta: map[string]any{"type": "agent_route"}}) {
		t.Fatal("route mode dropped an agent_route envelope without the legacy no_mirror marker")
	}
	stats, _ := p.snapshotStats()
	if stats.legacyRouteDropped != 1 {
		t.Fatalf("legacy drops=%d", stats.legacyRouteDropped)
	}
}

func TestDeliveryFailureEnvelopeUsesClientVersionTier(t *testing.T) {
	t.Setenv("WP_AMR_STRUCTURED_FAILURE_MIN_VERSION", "0.1.3.0")
	now := time.Unix(123, 0)
	old := deliveryFailureEnvelope(&sessionClient{keyID: "key", sessionName: "source", version: "0.1.2.9"}, "msg", "#target", "offline", now)
	if old.From != "workpulse#key" || !strings.HasPrefix(old.Body, "【投递失败】") || metaString(old.Meta, "type") != "delivery_failure" || metaBool(old.Meta, "system") || metaBool(old.Meta, "no_mirror") {
		t.Fatalf("old-client failure envelope=%+v", old)
	}
	upgraded := deliveryFailureEnvelope(&sessionClient{keyID: "key", sessionName: "source", version: "0.1.3.0"}, "msg", "#target", "offline", now)
	if metaBool(upgraded.Meta, "system") || metaBool(upgraded.Meta, "no_mirror") || metaString(upgraded.Meta, "type") != "delivery_failure" || metaString(upgraded.Meta, "presentation") != "structured" {
		t.Fatalf("upgraded-client failure envelope=%+v", upgraded)
	}
}

func TestPrepareSessionSelectorRejectsUnknownInsteadOfSynthesizingAddress(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	handled, _, err := hub.prepareSessionSelector(ctx, "key", "source", model.Envelope{To: "#missing", Body: "hello"})
	if !handled || err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("handled=%t err=%v", handled, err)
	}
}

func BenchmarkAMRIngestCompletedCardinality(b *testing.B) {
	for _, size := range []int{0, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("completed_%d", size), func(b *testing.B) {
			p := testAMRProcessor()
			p.maxCompleted = size + b.N + 1
			for i := 0; i < size; i++ {
				p.completed[amrTurnKey{KeyID: "other", Session: "other", DeliveryID: fmt.Sprintf("d%d", i), TurnID: "t"}] = time.Now()
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				delivery := fmt.Sprintf("bench-%d", i)
				p.ingest("key", "source", amrEventJSON(b, amrSessionEvent{Kind: "assistant_text", Text: "x", DeliveryID: delivery, TurnID: "t"}))
				p.mu.Lock()
				delete(p.turns, amrTurnKey{KeyID: "key", Session: "source", DeliveryID: delivery, TurnID: "t"})
				p.mu.Unlock()
			}
		})
	}
}
