package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

func TestIPCSendBodyAMRUsesSharedParserAndIndentEscape(t *testing.T) {
	top, looked := parseAMRCandidatesDetailed("发给主目标\n@alpha @beta 正文", "source", 20)
	if !looked || len(top) != 2 || top[0].selector != "#alpha" || top[1].selector != "#beta" || top[0].body != "正文" {
		t.Fatalf("top-level candidates=%+v looked=%t", top, looked)
	}
	indented, looked := parseAMRCandidatesDetailed("发给主目标\n @alpha @beta 正文", "source", 20)
	if looked || len(indented) != 0 {
		t.Fatalf("indented escape parsed candidates=%+v looked=%t", indented, looked)
	}
}

func TestIPCSendBodyAMRUsesConfiguredTargetLimit(t *testing.T) {
	h := &Hub{amr: &amrProcessor{maxTargets: 1}}
	limit := h.ipcBodyAMRMaxTargets()
	if limit != 1 {
		t.Fatalf("limit=%d want=1", limit)
	}
	candidates, looked := parseAMRCandidatesDetailed("@alpha @beta 正文", "source", limit)
	if !looked || len(candidates) != 0 {
		t.Fatalf("configured limit not enforced candidates=%+v looked=%t", candidates, looked)
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

// F8/T-040 阳性对照:光有 late_after_terminal 计数不够 —— G2-2 要的是「哪几条」。
// 本例故意让同一个已完成回合再收两条事件:一条计数(assistant_text 非空),一条不计数(state_change),
// 断言两条日志的 counted_late 取值**相反**。只断言"日志里有 counted_late"是空断言:
// 恒真的字段名证明不了它区分得开,必须让两个分支同时在场并给出不同值。
func TestAMRLateAfterTerminalLogCarriesAttribution(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	const secret = "此正文不得出现在日志里"
	p := testAMRProcessor()
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "d1", TurnID: "t1"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: secret, DeliveryID: "d1", TurnID: "t1"}))
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "tool_use"}, DeliveryID: "d1", TurnID: "t1"}))

	var counted, uncounted []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(line, "reason=completed_turn") {
			continue
		}
		switch {
		case strings.Contains(line, "counted_late=true"):
			counted = append(counted, line)
		case strings.Contains(line, "counted_late=false"):
			uncounted = append(uncounted, line)
		default:
			t.Fatalf("ignored 日志缺 counted_late 判别位: %q", line)
		}
	}
	if len(counted) != 1 || len(uncounted) != 1 {
		t.Fatalf("两分支须各出现一次才算区分得开: counted=%d uncounted=%d 全文=%q", len(counted), len(uncounted), buf.String())
	}
	stats, _ := p.snapshotStats()
	if stats.lateAfterTerminal != uint64(len(counted)) {
		t.Fatalf("日志与计数对不上: late_after_terminal=%d counted_late=true 行数=%d", stats.lateAfterTerminal, len(counted))
	}
	for _, field := range []string{"session=source", "delivery_id=d1", "turn_id=t1", "kind=assistant_text"} {
		if !strings.Contains(counted[0], field) {
			t.Fatalf("计数行缺归因字段 %s: %q", field, counted[0])
		}
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("正文泄漏进日志: %q", buf.String())
	}
	if !strings.Contains(counted[0], fmt.Sprintf("text_len=%d", len(secret))) {
		t.Fatalf("计数行应只记 text_len 不记正文: %q", counted[0])
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
	var routed []model.Envelope
	p.routeEnvelope = func(_ context.Context, env model.Envelope) error {
		attempted++
		routed = append(routed, env)
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
	for _, env := range routed {
		targets, ok := env.Meta["route_targets"].([]string)
		if !ok || len(targets) != 3 || targets[0] != "one#"+key.ID || targets[1] != "two#"+key.ID || targets[2] != "three#"+key.ID {
			t.Fatalf("route_targets=%#v", env.Meta["route_targets"])
		}
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
	if len(got) != 2 || got[0].selector != "#alpha" || got[1].selector != "#beta" || got[1].replyPolicy != "expect_reply" || got[0].body != "do the work" {
		t.Fatalf("candidates=%+v", got)
	}
	if got := parseAMRCandidates("prefix @alpha body", "source", 20); len(got) != 0 {
		t.Fatalf("mid-body mention routed: %+v", got)
	}
	if got := parseAMRCandidates("@na$me body", "source", 20); len(got) != 0 {
		t.Fatalf("invalid token partially consumed: %+v", got)
	}
	if got := parseAMRCandidates("@source body", "source", 20); len(got) != 0 {
		t.Fatalf("self mention routed: %+v", got)
	}
}

func TestT082AMRRoutesFirstEligibleLineWithHeartbeatDeltas(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantTargets      int
		wantBody         string
		wantNonCandidate bool
	}{
		{name: "first character regression", body: "@target direct", wantTargets: 1, wantBody: "direct"},
		{name: "later column zero", body: "先说明再动手\n@target routed\ncontext", wantTargets: 1, wantBody: "routed\ncontext"},
		{name: "space indentation escape", body: "说明\n @target example", wantNonCandidate: true},
		{name: "tab indentation escape", body: "说明\n\t@target example", wantNonCandidate: true},
		{name: "unicode indentation escape", body: "说明\n\u3000@target example", wantNonCandidate: true},
		{name: "markdown heading continues", body: "## 标题\n@target after heading", wantTargets: 1, wantBody: "after heading"},
		{name: "decorated markdown heading continues", body: "**## 标题**\n@target after decoration", wantTargets: 1, wantBody: "after decoration"},
		{name: "quote does not route", body: "> @target quotation", wantNonCandidate: true},
		{name: "only first directive routes", body: "@one first\n@two second", wantTargets: 1, wantBody: "first\n@two second"},
		{name: "no line head mention", body: "plain @target reference\nmore text", wantNonCandidate: true},
		{name: "first line keeps trim space", body: " \t@target trimmed", wantTargets: 1, wantBody: "trimmed"},
		{name: "first line zero width is not trimmed", body: "\u200b@target hidden", wantNonCandidate: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub, db, ctx := newTestHub(t)
			if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-t082", Name: "svc-t082", DeliveryType: "ws", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			_, key, err := hub.IssueAPIKey(ctx, "svc-t082", "owner")
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
				"target": {keyID: key.ID, sessionName: "target"},
				"one":    {keyID: key.ID, sessionName: "one"},
				"two":    {keyID: key.ID, sessionName: "two"},
			}
			hub.mu.Unlock()
			p := testAMRProcessor()
			p.hub = hub
			p.modes = map[string]amrMode{"source": amrRoute}
			var routed []model.Envelope
			p.routeEnvelope = func(_ context.Context, env model.Envelope) error {
				routed = append(routed, env)
				return nil
			}
			p.mu.Lock()
			p.stats.opened, p.stats.queued = 1, 1
			p.mu.Unlock()
			p.process(amrRouteJob{key: amrTurnKey{KeyID: key.ID, Session: "source", DeliveryID: "d", TurnID: "t"}, body: tc.body})
			stats, _ := p.snapshotStats()
			if len(routed) != tc.wantTargets || stats.targetsDispatched != uint64(tc.wantTargets) || stats.closedDispatched != boolUint64(tc.wantTargets > 0) {
				t.Fatalf("heartbeat delta dispatched=%d targets_dispatched=%d routed=%d", stats.closedDispatched, stats.targetsDispatched, len(routed))
			}
			if tc.wantTargets > 0 && routed[0].Body != tc.wantBody {
				t.Fatalf("body=%q want=%q", routed[0].Body, tc.wantBody)
			}
			if tc.wantNonCandidate && (stats.closedNonCandidate != 1 || stats.notificationsSent != 0 || stats.notificationsSuppressed != 0) {
				t.Fatalf("non-candidate was not silent: %+v", stats)
			}
		})
	}
}

func boolUint64(v bool) uint64 {
	if v {
		return 1
	}
	return 0
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

// 业主 2026-09-10 裁定:@ 提及要「尽可能可靠投递,只要客户端 inbox 收到就是送达」。
// 本组用例把该裁定钉在 reliablePrimaryEnvelope 上,并守住三条边界:
//
//	① agent_route 必须可靠 —— 否则又回到"直写成功无痕、离线即丢"
//	② no_mirror 不再影响可靠性 —— 它只管镜不镜像
//	③ system 仍必须不可靠 —— 控制面信封不该进持久队列要 ACK
//
// 只断言 ① 是不够的:把整个判定改成恒真也能让 ① 过,②③ 才是防止那种"修法"的护栏。
func TestReliableEnvelopeDecouplesMirrorFromReliability(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"AMR 路由信封(no_mirror 且 agent_route)", map[string]any{"type": "agent_route", "no_mirror": true}, true},
		{"普通消息", map[string]any{}, true},
		{"仅 no_mirror 的普通消息", map[string]any{"no_mirror": true}, true},
		{"控制面 system", map[string]any{"type": "terminal_resize", "system": true, "no_mirror": true}, false},
		{"仅 system", map[string]any{"system": true}, false},
		{"其它已知类型不得被顺带放行", map[string]any{"type": "delivery_ack"}, false},
	}
	for _, tc := range cases {
		env := model.Envelope{ID: "e1", Meta: tc.meta}
		if got := reliablePrimaryEnvelope(env); got != tc.want {
			t.Errorf("%s: reliablePrimaryEnvelope=%v want %v", tc.name, got, tc.want)
		}
	}
	// 空 ID 仍不可靠(原有不变量,顺带守住)
	if reliablePrimaryEnvelope(model.Envelope{Meta: map[string]any{}}) {
		t.Errorf("空 ID 信封不得判为可靠")
	}
}

// AMR 构造的信封必须真的能通过可靠判定 —— 上面那组测的是判定函数,
// 这一格测的是**实际发出的那个信封形状**,两者对不上时前者会绿而线上仍坏。
func TestAMRRoutedEnvelopeIsReliable(t *testing.T) {
	env := model.Envelope{
		ID: "amr-abc",
		Meta: map[string]any{
			"type": "agent_route", "no_mirror": true,
			"reply_policy": "no_reply", "source_delivery_id": "d1", "source_turn_id": "t1",
		},
	}
	if !reliablePrimaryEnvelope(env) {
		t.Fatalf("AMR 路由信封被判为不可靠 —— 将退回直写无痕、离线即丢")
	}
}

// G2-1 阳性对照(T-019):生产上 fragment_snapshot 若为 0,必须先证明该分支**能**计数,
// 否则分不清「累计式不存在」与「累计式发生了但没被识别」—— 后者会让 AMR 的聚合逻辑
// 建立在一个未经验证的假设上(P3:claudeDriver 跨帧文本是互斥还是累计)。
//
// 三分支同时断言,不只测 snapshot:只测一格无法证明分类器**分得开**,
// 而"分得开"才是这批计数可用作占比结论的前提。
func TestAMRFragmentBranchesAreDistinguishable(t *testing.T) {
	p := testAMRProcessor()
	frag := func() (a, s, d uint64) {
		st, _ := p.snapshotStats()
		return st.fragmentAppend, st.fragmentSnapshot, st.fragmentDuplicate
	}
	send := func(text string) {
		p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{
			Kind: "assistant_text", Text: text, DeliveryID: "d-frag", TurnID: "t-frag"}))
	}

	send("abc") // 首帧:走 append
	a1, s1, d1 := frag()
	if a1 != 1 || s1 != 0 || d1 != 0 {
		t.Fatalf("首帧应记 append: append=%d snapshot=%d dup=%d", a1, s1, d1)
	}

	send("abc") // 与已存全文相同:dup
	a2, s2, d2 := frag()
	if d2 != d1+1 || a2 != a1 || s2 != s1 {
		t.Fatalf("重复帧应只增 dup: append=%d snapshot=%d dup=%d", a2, s2, d2)
	}

	send("abcdef") // 以已存全文为前缀且更长:snapshot(累计式)
	a3, s3, d3 := frag()
	if s3 != s2+1 || a3 != a2 || d3 != d2 {
		t.Fatalf("前缀扩展帧应只增 snapshot: append=%d snapshot=%d dup=%d", a3, s3, d3)
	}

	send("xyz") // 既不相同也非前缀:append(互斥式)
	a4, s4, d4 := frag()
	if a4 != a3+1 || s4 != s3 || d4 != d3 {
		t.Fatalf("互斥帧应只增 append: append=%d snapshot=%d dup=%d", a4, s4, d4)
	}

	// 结论性断言:三格各自都被触发过,分类器确实分得开。
	// 生产上任一格为 0,即可解读为「该形态在真实流量中未发生」,而非「未被识别」。
	if a4 == 0 || s4 == 0 || d4 == 0 {
		t.Fatalf("三分支须各自至少触发一次才构成阳性对照: append=%d snapshot=%d dup=%d", a4, s4, d4)
	}
}

// T-046(业主裁定:未解决不随本版本发布):安静的回合不得被丢弃,只要来源会话还连着。
// 三格合起来才算数 —— 只测「安静不丢」会让「永不回收」也通过。
func TestAMRQuietTurnSurvivesWhileSourceConnected(t *testing.T) {
	p := testAMRProcessor()
	p.turnTTL = time.Second
	// hub 为 nil ⇒ sourceStillConnectedLocked 返回 false,模拟「来源已断」
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{
		Kind: "assistant_text", Text: "@target 半句", DeliveryID: "d1", TurnID: "t1"}))
	p.mu.Lock()
	p.expireLocked(time.Now().UTC().Add(2 * time.Second))
	gone := len(p.turns)
	p.mu.Unlock()
	if gone != 0 {
		t.Fatalf("来源已断开时,安静超 TTL 的回合应被丢弃,实存 %d", gone)
	}
}

func TestAMRQuietTurnKeptWhenSourceStillConnected(t *testing.T) {
	p := testAMRProcessor()
	p.turnTTL = time.Second
	p.maxTurnAge = time.Hour
	hub := &Hub{sessionClients: map[string]map[string]*sessionClient{
		"key": {"source": {sessionName: "source", keyID: "key"}}}}
	p.hub = hub
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{
		Kind: "assistant_text", Text: "@target 等长命令中", DeliveryID: "d1", TurnID: "t1"}))
	p.mu.Lock()
	p.expireLocked(time.Now().UTC().Add(10 * time.Minute)) // 安静 10 分钟,远超 TTL
	kept := len(p.turns)
	p.mu.Unlock()
	if kept != 1 {
		t.Fatalf("来源仍在线时,仅仅安静不得丢弃该回合(这正是 T-046),实存 %d", kept)
	}
}

func TestAMRQuietTurnStillBoundedByMaxAge(t *testing.T) {
	p := testAMRProcessor()
	p.turnTTL = time.Second
	p.maxTurnAge = time.Minute // 绝对年龄上限
	hub := &Hub{sessionClients: map[string]map[string]*sessionClient{
		"key": {"source": {sessionName: "source", keyID: "key"}}}}
	p.hub = hub
	p.ingest("key", "source", amrEventJSON(t, amrSessionEvent{
		Kind: "assistant_text", Text: "@target 连接假活", DeliveryID: "d1", TurnID: "t1"}))
	p.mu.Lock()
	p.expireLocked(time.Now().UTC().Add(2 * time.Minute)) // 超过 maxTurnAge
	kept := len(p.turns)
	p.mu.Unlock()
	if kept != 0 {
		t.Fatalf("超过绝对年龄上限仍须回收,否则连接假活会导致永不回收,实存 %d", kept)
	}
}
