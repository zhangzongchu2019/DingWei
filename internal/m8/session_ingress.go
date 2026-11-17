package m8

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	sessionIngressItems       = 2048
	sessionIngressGlobalItems = 65536
	sessionIngressBytes       = 32 << 20
	sessionIngressGlobalBytes = 256 << 20
	sessionWriteLimit         = 2 * time.Second
)

var errSessionQueueOverflow = errors.New("business_queue_overflow")

type sessionFrame struct {
	typ        websocket.MessageType
	data       []byte
	receivedAt time.Time
}
type sessionIngress struct {
	h           *Hub
	c           *sessionClient
	frames      chan sessionFrame
	done        chan struct{}
	cancel      context.CancelFunc
	mu          sync.Mutex
	err         error
	bytes       int
	items       int
	hasActive   bool
	activeAt    time.Time
	active      int
	readAt      time.Time
	workAt      time.Time
	peakItems   int
	peakBytes   int
	limitReason string
}

// This is the connection's only protocol Reader. It never waits for business,
// a database, viewer, outbound write, or a logging sink. Limits include the
// current business item, not just buffered channel entries.
func (h *Hub) startSessionIngress(ctx context.Context, cancel context.CancelFunc, c *sessionClient) *sessionIngress {
	q := &sessionIngress{h: h, c: c, frames: make(chan sessionFrame, sessionIngressItems), done: make(chan struct{}), cancel: cancel}
	c.ingress = q // Publish once before the Reader can emit diagnostics.
	if !h.backgroundWork("session_reader", func() {
		defer close(q.done)
		defer cancel()
		var scratch [8192]byte
		for {
			typ, reader, err := c.conn.Reader(ctx)
			if err != nil {
				q.fail(err)
				return
			}
			if !q.reserve(0) {
				q.overflow()
				return
			}
			var data []byte
			for {
				n, readErr := reader.Read(scratch[:])
				if n > 0 {
					if !q.addBytes(n) {
						q.release(len(data))
						q.overflow()
						return
					}
					data = append(data, scratch[:n]...)
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					q.release(len(data))
					q.fail(readErr)
					return
				}
			}
			c.diag.readFrame(typ, len(data))
			now := h.now().UTC()
			c.recordActivity(now)
			c.stateMu.Lock()
			c.lastReadAt = now
			c.stateMu.Unlock()
			c.scheduleTouch()
			select {
			case q.frames <- sessionFrame{typ: typ, data: data, receivedAt: now}:
			default:
				q.release(len(data))
				q.overflow()
				return
			}
		}
	}) {
		q.fail(context.Canceled)
		close(q.done)
		cancel()
	}
	return q
}
func (q *sessionIngress) reserve(n int) bool {
	q.h.ingressBudgetMu.Lock()
	defer q.h.ingressBudgetMu.Unlock()
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case q.items >= sessionIngressItems:
		q.limitReason = "connection_items"
	case q.h.ingressBudgetItems >= sessionIngressGlobalItems:
		q.limitReason = "global_items"
	case q.bytes+n > sessionIngressBytes:
		q.limitReason = "connection_bytes"
	case q.h.ingressBudgetBytes+n > sessionIngressGlobalBytes:
		q.limitReason = "global_bytes"
	default:
		q.limitReason = ""
	}
	if q.limitReason != "" {
		return false
	}
	q.bytes += n
	q.items++
	q.h.ingressBudgetItems++
	if q.items > q.peakItems {
		q.peakItems = q.items
	}
	q.h.ingressBudgetBytes += n
	if q.bytes > q.peakBytes {
		q.peakBytes = q.bytes
	}
	return true
}
func (q *sessionIngress) release(n int) {
	q.h.ingressBudgetMu.Lock()
	defer q.h.ingressBudgetMu.Unlock()
	q.mu.Lock()
	q.bytes -= n
	q.items--
	q.h.ingressBudgetItems--
	q.h.ingressBudgetBytes -= n
	q.mu.Unlock()
}
func (q *sessionIngress) fail(err error) { q.mu.Lock(); q.err = err; q.mu.Unlock() }
func (q *sessionIngress) next(ctx context.Context) (websocket.MessageType, []byte, error) {
	if q.hasActive {
		q.release(q.active)
		q.active = 0
		q.hasActive = false
	}
	q.mu.Lock()
	q.workAt = time.Time{}
	q.mu.Unlock()
	if ctx.Err() != nil {
		return 0, nil, q.readError(ctx.Err())
	}
	select {
	case <-ctx.Done():
		return 0, nil, q.readError(ctx.Err())
	case f := <-q.frames:
		if ctx.Err() != nil {
			q.release(len(f.data))
			return 0, nil, q.readError(ctx.Err())
		}
		q.active = len(f.data)
		q.hasActive = true
		q.activeAt = f.receivedAt
		q.mu.Lock()
		q.workAt = q.h.now()
		q.mu.Unlock()
		return f.typ, f.data, nil
	}
}
func (q *sessionIngress) joinAndRelease() {
	<-q.done
	if q.hasActive {
		q.release(q.active)
		q.active = 0
		q.hasActive = false
	}
	for {
		select {
		case f := <-q.frames:
			q.release(len(f.data))
		default:
			return
		}
	}
}
func (c *sessionClient) scheduleTouch() {
	if c.touchNotify != nil {
		select {
		case c.touchNotify <- struct{}{}:
		default:
		}
	}
}
func (c *sessionClient) recordPong(now time.Time) {
	c.stateMu.Lock()
	c.lastPongAt = now
	c.stateMu.Unlock()
}
func (h *Hub) runSessionTouch(ctx context.Context, c *sessionClient) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.touchNotify:
			call, cancel := context.WithTimeout(ctx, time.Second)
			h.touchSessionEndpoint(call, c)
			cancel()
		}
	}
}

func (q *sessionIngress) addBytes(n int) bool {
	q.h.ingressBudgetMu.Lock()
	defer q.h.ingressBudgetMu.Unlock()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bytes+n > sessionIngressBytes {
		q.limitReason = "connection_bytes"
		return false
	}
	if q.h.ingressBudgetBytes+n > sessionIngressGlobalBytes {
		q.limitReason = "global_bytes"
		return false
	}
	q.bytes += n
	q.h.ingressBudgetBytes += n
	if q.bytes > q.peakBytes {
		q.peakBytes = q.bytes
	}
	return true
}
func (q *sessionIngress) overflow() {
	q.fail(errSessionQueueOverflow)
	q.c.diag.overflow()
	q.c.obsConn.SetLocalReason("business_queue_overflow")
	_ = q.c.conn.CloseNow()
}

func (q *sessionIngress) readError(fallback error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	return fallback
}
