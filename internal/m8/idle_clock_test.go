package m8

import (
	"sync"
	"testing"
	"time"
)

// idleClock 会话空闲判定的假时钟（组长 R-M8-TESTSIDE 取甲）：挂到 Hub.NowFunc 与 Hub.IdleTickerFunc。
// 时间只在 advance 时前进；巡检的 tick 由 fire 手动送出，通道无缓冲 ——
// fire 返回即表示巡检已收下这一拍；confirmAlive 再送一拍，送得进去即表示上一拍判完、巡检仍在跑（没有因空闲关连接）。
type idleClock struct {
	mu    sync.Mutex
	now   time.Time
	ticks chan time.Time // 本格只有一个会话、一个巡检
	armed chan struct{}
	once  sync.Once
}

func installIdleClock(t *testing.T, hub *Hub) *idleClock {
	t.Helper()
	c := &idleClock{now: time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC), ticks: make(chan time.Time), armed: make(chan struct{})}
	hub.NowFunc = c.Now
	hub.IdleTickerFunc = func(time.Duration) (<-chan time.Time, func()) {
		c.once.Do(func() { close(c.armed) })
		return c.ticks, func() {}
	}
	return c
}

func (c *idleClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *idleClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// fire 把当前假时刻作为一拍送给巡检；巡检不在收（已退出）⇒ 失败。等待上限只在出错时才会走到。
func (c *idleClock) fire(t *testing.T, what string) {
	t.Helper()
	select {
	case <-c.armed:
	case <-time.After(envelopeReadTimeout):
		t.Fatalf("idle watchdog never started (%s)", what)
	}
	select {
	case c.ticks <- c.Now():
	case <-time.After(envelopeReadTimeout):
		t.Fatalf("idle watchdog is not running any more (%s)", what)
	}
}

// confirmAlive 上一拍判完后巡检仍在跑（没有因空闲关掉连接）。
func (c *idleClock) confirmAlive(t *testing.T, what string) {
	t.Helper()
	c.fire(t, what)
}
