package m8

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
		p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: fmt.Sprintf("d%05d", i), TurnID: "t"}] = base.Add(time.Duration(i) * time.Millisecond)
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
		p.completed[amrTurnKey{KeyID: "key", Session: "src", DeliveryID: fmt.Sprintf("d%05d", i), TurnID: "t"}] = base.Add(time.Duration(i) * time.Millisecond)
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
