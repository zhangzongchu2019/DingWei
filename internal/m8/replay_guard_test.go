package m8

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// R-V2-0105 件三：旧 helper 事件桥整份重放缓冲时，Hub 不得把已完成回合重开、重路由；
// R5 去重滑动续期并封顶；入站 last_seen 落库节流（要点第七、十至十三节）。

func replayGuardProcessor() *amrProcessor {
	p := testAMRProcessor()
	p.turnTTL = 5 * time.Minute
	p.completedTTL = 24 * time.Hour
	p.maxCompleted = 4096
	p.maxCompletedTotal = 100000
	return p
}

func completeTurn(t *testing.T, p *amrProcessor, session, delivery, turn string) {
	t.Helper()
	p.ingest("key", session, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "body", Cursor: "c1", DeliveryID: delivery, TurnID: turn}))
	p.ingest("key", session, amrEventJSON(t, amrSessionEvent{Kind: "state_change", Cursor: "c2", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: delivery, TurnID: turn}))
	for len(p.queue) > 0 {
		<-p.queue
	}
}

func TestReplayedTerminalAfterTwelveMinutesIsNotReopened(t *testing.T) {
	p := replayGuardProcessor()
	completeTurn(t, p, "src", "d1", "t1")
	opened := p.stats.opened
	for _, after := range []time.Duration{12 * time.Minute, time.Hour, 23 * time.Hour} {
		p.mu.Lock()
		p.expireLocked(time.Now().UTC().Add(after))
		p.mu.Unlock()
		completeTurn(t, p, "src", "d1", "t1") // 旧桥整份重放：同一回合的正文与终态再来一遍
		if p.stats.opened != opened {
			t.Fatalf("after %s the completed turn was reopened (opened %d -> %d) — 会再次回路由", after, opened, p.stats.opened)
		}
	}
}

func TestReplayedTerminalBeyondOldPerSessionCapIsNotReopened(t *testing.T) {
	p := replayGuardProcessor()
	for i := 0; i < 600; i++ { // 超过旧上限 64 与 512
		completeTurn(t, p, "src", fmt.Sprintf("d%03d", i), "t")
	}
	opened := p.stats.opened
	completeTurn(t, p, "src", "d000", "t") // 最早的那个回合
	if p.stats.opened != opened {
		t.Fatalf("earliest of 600 completed turns was reopened; per-session cap too small")
	}
}

func TestCompletedTotalCapEvictsOldestFirst(t *testing.T) {
	p := replayGuardProcessor()
	p.maxCompletedTotal = 5
	for i := 0; i < 7; i++ {
		completeTurn(t, p, fmt.Sprintf("s%d", i), "d", "t")
		time.Sleep(time.Millisecond)
	}
	if len(p.completed) != 5 {
		t.Fatalf("completed=%d want 5", len(p.completed))
	}
	for i, want := range []bool{false, false, true, true, true, true, true} {
		_, ok := p.completed[amrTurnKey{KeyID: "key", Session: fmt.Sprintf("s%d", i), DeliveryID: "d", TurnID: "t"}]
		if ok != want {
			t.Fatalf("session s%d present=%v want %v (oldest first out)", i, ok, want)
		}
	}
}

func TestIgnoredCompletedLogIsAggregatedPerSession(t *testing.T) {
	p := replayGuardProcessor()
	completeTurn(t, p, "src", "d1", "t1")
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	for i := 0; i < 1000; i++ {
		p.ingest("key", "src", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "x", Cursor: "c1", DeliveryID: "d1", TurnID: "t1"}))
	}
	lines := strings.Count(buf.String(), "reason=completed_turn")
	if lines < 1 || lines > 2 {
		t.Fatalf("1000 replayed events produced %d log lines, want 1–2 aggregated lines:\n%s", lines, buf.String())
	}
	if !strings.Contains(buf.String(), "aggregated=") {
		t.Fatalf("aggregated count missing: %s", buf.String())
	}
}

func TestReplyDedupSlidingBlocksPeriodicResends(t *testing.T) {
	d := newReplyDedup(24 * time.Hour)
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return at }
	if d.SeenOrMark("reply-origin:d:t") {
		t.Fatal("first sighting must pass")
	}
	for i := 0; i < 200; i++ { // 每 720 s 一次，持续 40 小时：滑动续期下永远在窗内
		at = at.Add(720 * time.Second)
		if !d.SeenOrMark("reply-origin:d:t") {
			t.Fatalf("periodic resend #%d at +%s passed the dedup", i+1, time.Duration(i+1)*720*time.Second)
		}
	}
	at = at.Add(25 * time.Hour)
	if d.SeenOrMark("reply-origin:d:t") {
		t.Fatal("after a gap longer than the window it is a first sighting again")
	}
}

func TestReplyDedupCapEvictsOldest(t *testing.T) {
	d := newReplyDedup(24 * time.Hour)
	d.max = 100
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return at }
	for i := 0; i < 150; i++ {
		at = at.Add(time.Second)
		d.SeenOrMark(fmt.Sprintf("k%03d", i))
	}
	if n := d.size(); n > 100 {
		t.Fatalf("size=%d over cap 100", n)
	}
	d.mu.Lock()
	_, oldest := d.seen["k000"]
	_, newest := d.seen["k149"]
	d.mu.Unlock()
	if oldest || !newest {
		t.Fatalf("eviction order wrong: oldest present=%v newest present=%v", oldest, newest)
	}
}

func TestReplayGuardRaisesCompletedTTLToDedupWindow(t *testing.T) {
	t.Setenv("WP_AMR_COMPLETED_TTL", "1m")
	t.Setenv("REPLY_ORIGIN_DEDUP_TTL", "10m")
	h, _, _ := newTestHub(t)
	if h.amr.completedTTL != 10*time.Minute {
		t.Fatalf("completedTTL=%s want raised to dedup window 10m", h.amr.completedTTL)
	}
}

func TestReplayGuardDefaults(t *testing.T) {
	h, _, _ := newTestHub(t)
	if h.amr.completedTTL != 24*time.Hour || h.amr.maxCompleted != 4096 || h.amr.maxCompletedTotal != 100000 {
		t.Fatalf("amr defaults ttl=%s per=%d total=%d", h.amr.completedTTL, h.amr.maxCompleted, h.amr.maxCompletedTotal)
	}
	if h.replyDedup.ttl != 24*time.Hour || h.replyDedup.max != 200000 || h.sessionTouchMinInterval != 5*time.Second {
		t.Fatalf("dedup ttl=%s max=%d touch=%s", h.replyDedup.ttl, h.replyDedup.max, h.sessionTouchMinInterval)
	}
}

type countingTouchRepo struct {
	store.Repository
	touches atomic.Int64
}

func (r *countingTouchRepo) TouchSessionEndpoint(ctx context.Context, keyID, sessionName string, at time.Time) error {
	r.touches.Add(1)
	return r.Repository.TouchSessionEndpoint(ctx, keyID, sessionName, at)
}

func TestSessionTouchIsThrottledPerConnection(t *testing.T) {
	h, _, ctx := newTestHub(t)
	repo := &countingTouchRepo{Repository: h.Repo}
	h.Repo = repo
	c := &sessionClient{keyID: "k", sessionName: "s"}
	for i := 0; i < 1000; i++ { // 洪流：1000 条入站消息
		h.touchSessionEndpoint(ctx, c)
	}
	if n := repo.touches.Load(); n < 1 || n > 9 {
		t.Fatalf("1000 inbound messages produced %d last_seen writes, want single digits", n)
	}
	c.stateMu.Lock()
	c.lastTouchWritten = c.lastTouchWritten.Add(-6 * time.Second) // 5 s 已过
	c.stateMu.Unlock()
	before := repo.touches.Load()
	h.touchSessionEndpoint(ctx, c)
	if repo.touches.Load() != before+1 {
		t.Fatal("after the interval the next message must write again")
	}
}

func TestCompletedPerSessionCapEvictsExactlyTheOldestInOnePass(t *testing.T) {
	// 复核要求：上限放大到 4096 后，超限淘汰不得每条重扫整表；4096＋1 条只淘汰最旧的 1 条。
	p := replayGuardProcessor()
	base := time.Now().UTC()
	p.mu.Lock()
	for i := 0; i <= 4096; i++ {
		p.setCompletedLocked(amrTurnKey{KeyID: "key", Session: "src", DeliveryID: fmt.Sprintf("d%05d", i), TurnID: "t"}, base.Add(time.Duration(i)*time.Millisecond))
	}
	start := time.Now()
	p.capCompletedLocked()
	elapsed := time.Since(start)
	_, oldest := p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: "d00000", TurnID: "t"}]
	_, second := p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: "d00001", TurnID: "t"}]
	n := len(p.completed)
	p.mu.Unlock()
	if n != 4096 || oldest || !second {
		t.Fatalf("len=%d oldest present=%v second present=%v; want 4096, oldest evicted only", n, oldest, second)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cap took %s", elapsed)
	}
}

func TestCompletedPerSessionCapLargeOverflowIsBatched(t *testing.T) {
	p := replayGuardProcessor()
	p.maxCompleted = 100
	base := time.Now().UTC()
	p.mu.Lock()
	for i := 0; i < 5000; i++ {
		p.setCompletedLocked(amrTurnKey{KeyID: "key", Session: "src", DeliveryID: fmt.Sprintf("d%05d", i), TurnID: "t"}, base.Add(time.Duration(i)*time.Millisecond))
	}
	start := time.Now()
	p.capCompletedLocked()
	elapsed := time.Since(start)
	n := len(p.completed)
	_, newest := p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: "d04999", TurnID: "t"}]
	p.mu.Unlock()
	if n != 100 || !newest {
		t.Fatalf("len=%d newest present=%v", n, newest)
	}
	// 原「每淘汰一条重扫整表」为 O(N×超出)＝约 5000×4900 次迭代；批量为 O(N log N)。
	if elapsed > 300*time.Millisecond {
		t.Fatalf("batch eviction too slow: %s", elapsed)
	}
}

// ── R-V2-0105 件三补件（P23-REVIEW-FIX／LOG-THROTTLE-DETAIL／LOG-FLUSH-SWEEP／R5-SWEEP）──

func TestSessionTouchFixedWindowEveryThreeSeconds(t *testing.T) {
	h, _, ctx := newTestHub(t)
	repo := &countingTouchRepo{Repository: h.Repo}
	h.Repo = repo
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cur := base
	h.NowFunc = func() time.Time { return cur }
	c := &sessionClient{keyID: "k", sessionName: "s"}
	var lastWrite time.Time
	for sec := 0; sec <= 30; sec += 3 {
		cur = base.Add(time.Duration(sec) * time.Second)
		before := repo.touches.Load()
		h.touchSessionEndpoint(ctx, c)
		if repo.touches.Load() > before {
			lastWrite = cur
		}
	}
	if n := repo.touches.Load(); n < 5 {
		t.Fatalf("every 3 s for 30 s produced %d writes, want >= 5 (fixed window: time updates only on write)", n)
	}
	if cur.Sub(lastWrite) > 5*time.Second {
		t.Fatalf("last write at +%s, more than 5 s before the end", lastWrite.Sub(base))
	}
}

var aggregatedRe = regexp.MustCompile(`aggregated=(\d+)`)

func sumAggregated(logs string) (sum, lines int) {
	for _, m := range aggregatedRe.FindAllStringSubmatch(logs, -1) {
		n, _ := strconv.Atoi(m[1])
		sum += n
		lines++
	}
	return
}

func TestIgnoredLogCountsSumToTotalAcrossWindowsAndFlushOnSweep(t *testing.T) {
	p := replayGuardProcessor()
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cur := base
	p.clock = func() time.Time { return cur }
	completeTurn(t, p, "src", "d1", "t1")
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	for i := 0; i < 1000; i++ { // 跨 10 s 边界（共 15 s）
		cur = base.Add(time.Duration(i) * 15 * time.Millisecond)
		p.ingest("key", "src", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "x", Cursor: "c1", DeliveryID: "d1", TurnID: "t1"}))
	}
	p.sweep(base.Add(40 * time.Second)) // 窗口已过、之后再无事件：须靠 sweep 补出汇总
	sum, lines := sumAggregated(buf.String())
	if sum != 1000 {
		t.Fatalf("aggregated counts sum to %d, want 1000:\n%s", sum, buf.String())
	}
	if lines > 9 {
		t.Fatalf("%d log lines for 1000 events", lines)
	}
	if !strings.Contains(buf.String(), "flush=window") {
		t.Fatalf("sweep flush line missing:\n%s", buf.String())
	}
}

func TestIgnoredTableIsBoundedAndCleanedWhenIdleOrClosed(t *testing.T) {
	p := replayGuardProcessor()
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cur := base
	p.clock = func() time.Time { return cur }
	for i := 0; i < 50; i++ {
		s := fmt.Sprintf("s%02d", i)
		completeTurn(t, p, s, "d", "t")
		p.ingest("key", s, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "x", Cursor: "c", DeliveryID: "d", TurnID: "t"}))
		p.ingest("key", s, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "y", Cursor: "c", DeliveryID: "d", TurnID: "t"}))
	}
	if len(p.ignoredCompleted) != 50 {
		t.Fatalf("table=%d want 50", len(p.ignoredCompleted))
	}
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	p.sessionClosed("key", "s00") // 断开：补汇总并删表项
	if !strings.Contains(buf.String(), "session=s00 aggregated=1 flush=session_closed") {
		t.Fatalf("close flush missing:\n%s", buf.String())
	}
	p.sweep(base.Add(11 * time.Second)) // 窗后：冲刷余量
	p.sweep(base.Add(22 * time.Second)) // 再一窗无事件：空闲清理
	if len(p.ignoredCompleted) != 0 || len(p.ignoredCompletedLog) != 0 {
		t.Fatalf("idle table not cleaned: %d/%d", len(p.ignoredCompleted), len(p.ignoredCompletedLog))
	}
	if sum, _ := sumAggregated(buf.String()); sum != 50 {
		t.Fatalf("flushed counts sum %d, want 50 (one pending per session)", sum)
	}
}

func TestOtherAMRReasonsAreNotAggregated(t *testing.T) {
	p := replayGuardProcessor()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	for i := 0; i < 5; i++ {
		p.ingest("key", "src", "{not json")
	}
	if n := strings.Count(buf.String(), "reason=invalid_json"); n != 5 {
		t.Fatalf("invalid_json logged %d times, want 5 (only completed_turn is aggregated)", n)
	}
}

func TestReplyDedupPassesOnceAfterWindowThenBlocksAgain(t *testing.T) {
	d := newReplyDedup(time.Hour)
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return at }
	d.SeenOrMark("k")
	at = at.Add(2 * time.Hour)
	if d.SeenOrMark("k") {
		t.Fatal("first sighting after the window must pass")
	}
	d.mu.Lock()
	refreshed := d.seen["k"] == at.UnixNano()
	d.mu.Unlock()
	if !refreshed {
		t.Fatal("timestamp not refreshed on the pass")
	}
	at = at.Add(10 * time.Minute)
	if !d.SeenOrMark("k") {
		t.Fatal("next resend inside the new window must be blocked")
	}
}

func TestReplyDedupOverCapDropsAllExpiredFirst(t *testing.T) {
	d := newReplyDedup(time.Hour)
	d.max = 100
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return at }
	for i := 0; i < 60; i++ {
		d.SeenOrMark(fmt.Sprintf("old%02d", i))
	}
	at = at.Add(2 * time.Hour) // 60 条已过期
	for i := 0; i < 41; i++ {
		d.SeenOrMark(fmt.Sprintf("new%02d", i)) // 第 101 条触发超限
	}
	if n := d.size(); n != 41 {
		t.Fatalf("size=%d want 41: expired entries must all go before any unexpired one", n)
	}
}

func TestAMRSweepAlsoSweepsReplyDedup(t *testing.T) {
	h, _, _ := newTestHub(t)
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	h.replyDedup.now = func() time.Time { return at }
	h.replyDedup.SeenOrMark("expire-me")
	at = at.Add(23 * time.Hour)
	h.replyDedup.SeenOrMark("keep-me")
	at = at.Add(2 * time.Hour) // expire-me 越过 24 h 窗
	h.amr.sweep(at)
	h.replyDedup.mu.Lock()
	_, gone := h.replyDedup.seen["expire-me"]
	_, kept := h.replyDedup.seen["keep-me"]
	h.replyDedup.mu.Unlock()
	if gone || !kept {
		t.Fatalf("AMR sweep must call replyDedup.Sweep: expired present=%v unexpired present=%v", gone, kept)
	}
}

func TestCompletedCountsResyncAfterDirectMapWrites(t *testing.T) {
	// 有人绕过 setCompletedLocked 直接写 map（老测试如此）⇒ 计数自我校正，上限仍生效。
	p := replayGuardProcessor()
	p.maxCompleted = 10
	p.mu.Lock()
	p.setCompletedLocked(amrTurnKey{KeyID: "key", Session: "src", DeliveryID: "seed", TurnID: "t"}, time.Now().Add(-time.Hour)) // 计数表非空
	for i := 0; i < 15; i++ {
		p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: fmt.Sprintf("d%02d", i), TurnID: "t"}] = time.Now().Add(time.Duration(i) * time.Millisecond)
	}
	p.capCompletedLocked()
	n, tracked := len(p.completed), p.completedTracked
	p.mu.Unlock()
	_, seed := p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: "seed", TurnID: "t"}]
	if n != 10 || tracked != 10 || seed {
		t.Fatalf("len=%d tracked=%d seed present=%v; want 10/10 and the oldest (seed) evicted", n, tracked, seed)
	}
}

func TestCompletedCapScanAt100kEntries(t *testing.T) {
	p := replayGuardProcessor()
	base := time.Now().UTC()
	p.mu.Lock()
	for s := 0; s < 50; s++ {
		for i := 0; i < 2000; i++ {
			p.setCompletedLocked(amrTurnKey{KeyID: "key", Session: fmt.Sprintf("s%02d", s), DeliveryID: fmt.Sprintf("d%05d", i), TurnID: "t"}, base.Add(time.Duration(i)*time.Millisecond))
		}
	}
	start := time.Now()
	p.capCompletedLocked()
	elapsed := time.Since(start)
	p.mu.Unlock()
	t.Logf("capCompletedLocked over 100000 entries (none over cap): %s", elapsed)
	if elapsed > 3*time.Millisecond {
		t.Fatalf("one cap pass over 100k entries took %s", elapsed)
	}
}

func TestSessionDisconnectFlushesIgnoredLogCounts(t *testing.T) {
	// 接线：Hub 会话收尾调用 amr.sessionClosed（真 WS 连接断开）。
	hub, db, ctx := newTestHub(t)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	name := "u1-flusher-" + keyTail(key.ID)
	conn := dialSession(t, ctx, srv.URL, "flusher", key.ID, secret)
	waitSessionOnline(t, hub, key.ID, name)
	hub.amr.ingest(key.ID, name, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "body", Cursor: "c1", DeliveryID: "d2", TurnID: "t2"}))
	hub.amr.ingest(key.ID, name, amrEventJSON(t, amrSessionEvent{Kind: "state_change", Cursor: "c2", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: "d2", TurnID: "t2"}))
	hub.amr.ingest(key.ID, name, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "late1", Cursor: "c1", DeliveryID: "d2", TurnID: "t2"}))
	var buf safeBuffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	hub.amr.ingest(key.ID, name, amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: "late2", Cursor: "c1", DeliveryID: "d2", TurnID: "t2"}))
	conn.Close(websocket.StatusNormalClosure, "bye")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), "flush=session_closed") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "session="+name+" aggregated=1 flush=session_closed") {
		t.Fatalf("disconnect did not flush pending ignored count:\n%s", buf.String())
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
