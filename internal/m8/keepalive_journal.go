package m8

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coder/websocket"
)

func keepaliveErrorClass(err, pingContextErr error) string {
	if errors.Is(pingContextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if websocket.CloseStatus(err) != -1 {
		return "closed"
	}
	return "other"
}
func ageMillis(now, then time.Time) any {
	if then.IsZero() {
		return "unknown"
	}
	return now.Sub(then).Milliseconds()
}
func (h *Hub) recordKeepaliveFailure(c *sessionClient, interval, timeout, elapsed time.Duration, class string) {
	now := h.now()
	c.stateMu.Lock()
	read, pong := c.lastReadAt, c.lastPongAt
	c.stateMu.Unlock()
	data := map[string]any{"event": "session_keepalive_failed", "utc": now.UTC().Format(time.RFC3339Nano), "worker_phase": "unknown", "session": c.sessionName, "conn_seq": c.connSeq, "interval_ms": interval.Milliseconds(), "timeout_ms": timeout.Milliseconds(), "elapsed_ms": elapsed.Milliseconds(), "error_class": class, "reader_age_ms": ageMillis(now, read), "pong_age_ms": ageMillis(now, pong), "worker_age_ms": "unknown", "queue_items": 0, "queue_bytes": 0}
	if q := c.ingress; q != nil {
		q.mu.Lock()
		data["queue_bytes"] = q.bytes
		data["queue_items"] = q.items
		data["worker_age_ms"] = ageMillis(now, q.workAt)
		data["worker_phase"] = "processing"
		if q.workAt.IsZero() {
			data["worker_phase"] = "idle"
		}
		q.mu.Unlock()
	}
	// JSON escapes session names; no envelope, credentials, URL, or raw error.
	raw, _ := json.Marshal(data)
	raw = append(raw, '\n')
	write := writeKeepaliveJournal
	if h.keepaliveJournal != nil {
		write = h.keepaliveJournal
	}
	if !write(raw) {
		c.obsConn.DiagnosticDrop()
	}
}
