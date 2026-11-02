package m8

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
)

// Registration and handler accounting have distinct lifetimes. Sealing the
// registration gate never waits for a whole websocket conversation.
type hubLifecycle struct {
	once          sync.Once
	registrations lifecycle.Group
	handlers      lifecycle.Group
	background    lifecycle.Group
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	sockets       map[*websocket.Conn]string
	closeOnce     sync.Once
	closeDone     chan struct{}
	closeErr      error
	closeRecords  []WSCloseRecord
}

func (h *Hub) lifetime() *hubLifecycle {
	l := &h.lifecycle
	l.once.Do(func() {
		l.ctx, l.cancel = context.WithCancel(context.Background())
		l.sockets = map[*websocket.Conn]string{}
		l.closeDone = make(chan struct{})
	})
	return l
}
func (h *Hub) beginRegistration(w http.ResponseWriter, kind string) (func(), func(), bool) {
	l := h.lifetime()
	registered, ok := l.registrations.Enter(kind)
	if !ok {
		http.Error(w, "hub draining", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	ended, _ := l.handlers.Enter(kind) // handlers are sealed only after registration joins.
	return registered, ended, true
}
func (h *Hub) streamRequest(w http.ResponseWriter, r *http.Request, kind string) (*http.Request, func(), bool) {
	registered, ended, ok := h.beginRegistration(w, kind)
	if !ok {
		return r, nil, false
	}
	ctx, cancel := context.WithCancel(r.Context())
	l := h.lifetime()
	stop := context.AfterFunc(l.ctx, cancel)
	registered()
	return r.WithContext(ctx), func() { stop(); cancel(); ended() }, true
}
func (h *Hub) trackSocket(conn *websocket.Conn, kind string) func() {
	l := h.lifetime()
	l.mu.Lock()
	l.sockets[conn] = kind
	l.mu.Unlock()
	return func() { l.mu.Lock(); delete(l.sockets, conn); l.mu.Unlock() }
}

// SealRegistrations is nonblocking. WaitRegistrations is bounded by the
// coordinator's one-second access deadline; CloseContext is forbidden on failure.
func (h *Hub) SealRegistrations() { h.lifetime().registrations.Seal() }
func (h *Hub) WaitRegistrations(ctx context.Context) error {
	return h.lifetime().registrations.Wait(ctx)
}
func (h *Hub) LifecycleCounts() map[string]int {
	l := h.lifetime()
	out := l.handlers.Snapshot()
	for k, v := range l.background.Snapshot() {
		out["background_"+k] = v
	}
	for k, v := range l.registrations.Snapshot() {
		out["registration_"+k] = v
	}
	return out
}

// WSCloseRecord retains each close outcome without mixing historical outcomes
// into the live-handler counters used to decide whether storage can be closed.
type WSCloseRecord struct {
	Kind   string `json:"kind"`
	Result string `json:"result"`
	Error  string `json:"error"`
}
type WSCloseSnapshot struct {
	AlreadyClosed int             `json:"already_closed"`
	Failed        int             `json:"failed"`
	Records       []WSCloseRecord `json:"records"`
}

func (h *Hub) WSCloseSnapshot() WSCloseSnapshot {
	l := h.lifetime()
	l.mu.Lock()
	defer l.mu.Unlock()
	out := WSCloseSnapshot{Records: append([]WSCloseRecord{}, l.closeRecords...)}
	for _, r := range out.Records {
		switch r.Result {
		case "already_closed":
			out.AlreadyClosed++
		case "failed":
			out.Failed++
		}
	}
	return out
}

func websocketAlreadyClosed(err error) bool {
	if err == nil {
		return false
	}
	// A timeout/cancellation remains a failure even if an aggregate also wraps a
	// closed sentinel. Never turn a failed write to a live peer into success.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || websocketCloseTimeout(err) {
		return false
	}
	var peerClose websocket.CloseError
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.As(err, &peerClose)
}

// Walk all branches: errors.As alone can stop at a non-timeout net.ErrClosed
// in errors.Join and miss a later timeout that must retain failure precedence.
func websocketCloseTimeout(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if websocketCloseTimeout(child) {
				return true
			}
		}
	}
	return websocketCloseTimeout(errors.Unwrap(err))
}

func (h *Hub) backgroundWork(kind string, fn func()) bool {
	done, ok := h.lifetime().background.Enter(kind)
	if !ok {
		return false
	}
	go func() { defer done(); fn() }()
	return true
}
func (h *Hub) backgroundContext() context.Context { return h.lifetime().ctx }

// CloseContext reports completion and errors instead of treating a five-second
// warning as success. All callers share one close operation and its result.
func (h *Hub) CloseContext(ctx context.Context) error {
	if h == nil {
		return nil
	}
	l := h.lifetime()
	h.SealRegistrations()
	if err := h.WaitRegistrations(ctx); err != nil {
		return err
	}
	l.closeOnce.Do(func() {
		go func() {
			defer close(l.closeDone)
			l.background.Seal()
			l.handlers.Seal()
			// Cancel long HTTP streams, but WS reads remain alive for the close handshake.
			l.cancel()
			var work lifecycle.Group
			var mu sync.Mutex
			var errs []error
			launch := func(kind string, fn func() error) {
				done, _ := work.Enter(kind)
				go func() {
					defer done()
					if err := fn(); err != nil {
						mu.Lock()
						errs = append(errs, err)
						mu.Unlock()
					}
				}()
			}
			launch("mux", func() error { return h.closeConsoleMuxContext(ctx) })
			launch("amr", func() error {
				if h.amr != nil {
					h.amr.close()
				}
				return nil
			})
			launch("cleanup", func() error {
				if h.capabilityPendingCleanupStop != nil {
					close(h.capabilityPendingCleanupStop)
					h.capabilityPendingCleanupWG.Wait()
				}
				return nil
			})
			l.mu.Lock()
			sockets := make(map[*websocket.Conn]string, len(l.sockets))
			for c, kind := range l.sockets {
				sockets[c] = kind
			}
			l.mu.Unlock()
			for conn, kind := range sockets {
				c, socketKind := conn, kind
				launch("ws", func() error {
					err := c.Close(websocket.StatusGoingAway, "hub shutdown")
					record := WSCloseRecord{Kind: socketKind, Result: "closed"}
					if err != nil {
						record.Error = err.Error()
						record.Result = "failed"
						if websocketAlreadyClosed(err) {
							record.Result = "already_closed"
						}
					}
					l.mu.Lock()
					l.closeRecords = append(l.closeRecords, record)
					l.mu.Unlock()
					if err != nil && record.Result == "failed" {
						return fmt.Errorf("websocket close handshake failed: %w", err)
					}
					return nil
				})
			}
			err := work.Wait(ctx)
			if err == nil {
				err = l.handlers.Wait(ctx)
			}
			if err == nil {
				err = l.background.Wait(ctx)
			}
			mu.Lock()
			l.closeErr = errors.Join(append(errs, err)...)
			mu.Unlock()
		}()
	})
	select {
	case <-l.closeDone:
		return l.closeErr
	case <-ctx.Done():
		h.ForceCloseSockets()
		return ctx.Err()
	}
}
func (h *Hub) ForceCloseSockets() {
	l := h.lifetime()
	l.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(l.sockets))
	for c := range l.sockets {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		_ = c.CloseNow()
	}
}

// Close is the legacy test cleanup facade; production uses CloseContext.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	h.SealRegistrations()
	_ = h.WaitRegistrations(ctx)
	h.ForceCloseSockets()
	_ = h.CloseContext(ctx)
}

// TrackLongHandler covers non-session hijacked transports (hosted node WS).
// Their existing request context is canceled and their handler must actually join.
func (h *Hub) TrackLongHandler(kind string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, finish, ok := h.streamRequest(w, r, kind)
		if !ok {
			return
		}
		defer finish()
		next.ServeHTTP(w, r)
	})
}
