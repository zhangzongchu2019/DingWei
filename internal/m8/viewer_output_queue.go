package m8

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const viewerWriteLimit = time.Second
const viewerOutputByteLimit = 1 << 20
const viewerOutputItemLimit = 64

type viewerOutputQueue struct {
	mu      sync.Mutex
	bytes   int
	stopped bool
	frames  chan string
	cancel  context.CancelFunc
	done    chan struct{}
	conn    *websocket.Conn
	viewer  *terminalViewer
}

// Called under h.mu before publishing a real viewer. The worker captures the
// connection so reconnect cannot inherit an old queue. Virtual viewers have no
// output worker. Overflow closes this viewer; the terminal buffer remains the
// visible reconnect/replay boundary, never an Agent-session close.
func (h *Hub) startViewerOutput(ctx context.Context, v *terminalViewer) {
	if v.conn == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	stopHub := context.AfterFunc(h.backgroundContext(), cancel)
	q := &viewerOutputQueue{frames: make(chan string, viewerOutputItemLimit), cancel: cancel, done: make(chan struct{}), conn: v.conn, viewer: v}
	v.output = q
	if !h.backgroundWork("viewer_output", func() {
		defer close(q.done)
		defer stopHub()
		defer cancel()
		defer func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.stopped = true
			for {
				select {
				case <-q.frames:
					continue
				default:
					q.bytes = 0
					return
				}
			}
		}()
		// Local copy avoids racing the terminal handler's disconnect/grace pointer.
		writer := &terminalViewer{conn: q.conn, mu: v.mu}
		for {
			select {
			case <-ctx.Done():
				return
			case body := <-q.frames:
				q.mu.Lock()
				stopped := q.stopped
				q.mu.Unlock()
				if stopped {
					return
				}
				err := terminalWrite(ctx, writer, map[string]any{"type": "output", "data": body})
				q.mu.Lock()
				q.bytes -= len(body)
				q.mu.Unlock()
				if err != nil {
					q.viewer.outputGap.Store(true)
					_ = q.conn.CloseNow()
					return
				}
			}
		}
	}) {
		stopHub()
		cancel()
		q.mu.Lock()
		q.stopped = true
		q.mu.Unlock()
		v.outputGap.Store(true)
		_ = q.conn.CloseNow()
		close(q.done)
	}
}

// Offline controls retain the legacy status and normal-close protocol. One
// owned cleanup per existing viewer keeps slow peers out of Agent teardown;
// terminalWrite has its total 1s budget, Close has the library's 5s+5s bound.
// The lifecycle group tracks these cleanups and still reports its own shutdown
// deadline, rather than bypassing it or pretending the handshake completed.
func (h *Hub) closeOfflineTerminalViewers(viewers []*terminalViewer) {
	for _, viewer := range viewers {
		if viewer == nil {
			continue
		}
		v := viewer
		h.mu.Lock()
		conn := v.conn
		q := v.output
		writeSlot := v.mu
		h.mu.Unlock()
		if q != nil {
			conn = q.conn
			q.mu.Lock()
			q.stopped = true
			if q.bytes != 0 {
				v.outputGap.Store(true)
			}
			q.mu.Unlock()
		}
		if conn == nil {
			continue
		}
		writer := &terminalViewer{conn: conn, mu: writeSlot}
		if !h.backgroundWork("viewer_offline", func() {
			defer conn.CloseNow()
			if q != nil {
				defer q.cancel()
			}
			if err := terminalWrite(context.Background(), writer, map[string]any{"type": "status", "readonly": true, "message": "会话已离线"}); err != nil {
				v.outputGap.Store(true)
			}
			_ = conn.Close(websocket.StatusNormalClosure, "session offline")
		}) {
			// Shutdown already sealed admission: never start unowned cleanup.
			v.outputGap.Store(true)
			if q != nil {
				q.cancel()
			}
			_ = conn.CloseNow()
		}
	}
}
func (q *viewerOutputQueue) enqueue(body string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	if len(body)+q.bytes > viewerOutputByteLimit {
		q.stopped = true
		q.viewer.outputGap.Store(true)
		q.cancel()
		_ = q.conn.CloseNow()
		return
	}
	select {
	case q.frames <- body:
		q.bytes += len(body)
	default:
		q.stopped = true
		q.viewer.outputGap.Store(true)
		q.cancel()
		_ = q.conn.CloseNow()
	}
}
