package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"io"
	"sync"
	"time"
)

// Per-connection bounded metadata only. Never store payload, URL, raw errors or
// message IDs. End may race a writer: report in-flight rather than invent cause.
type sessionDiagnostics struct {
	h               *Hub
	client          *sessionClient
	session         string
	seq             int
	mu              sync.Mutex
	firstReason     string
	phase           string
	peerCode        int
	waiters         int
	physical        bool
	writeBytes      int
	writeLimit      time.Duration
	writeAt         time.Time
	lastWriteReason string
	lastFrameType   string
	lastFrameBytes  int
	lastWorkerType  string
	endOnce         sync.Once
	overflowOnce    sync.Once
	writeOnce       sync.Once
}

func newSessionDiagnostics(h *Hub, name string) *sessionDiagnostics {
	if len(name) > 128 {
		name = name[:128]
	}
	return &sessionDiagnostics{h: h, session: name, phase: "registration", lastWriteReason: "none", lastFrameType: "unknown", lastWorkerType: "unknown"}
}
func (d *sessionDiagnostics) reason(r string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.firstReason == "" {
		d.firstReason = sessionDiagReason(r)
	}
}
func (d *sessionDiagnostics) online() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.phase = "online"
	d.mu.Unlock()
}
func (d *sessionDiagnostics) waiting(n int) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.waiters += n
	d.mu.Unlock()
}
func (d *sessionDiagnostics) physicalStart(size int, limit time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.physical = true
	d.writeBytes = size
	d.writeLimit = limit
	d.writeAt = d.h.now()
	d.mu.Unlock()
}
func (d *sessionDiagnostics) physicalEnd() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.physical = false
	d.mu.Unlock()
}
func sessionErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, io.EOF):
		return "eof"
	case websocket.CloseStatus(err) != -1:
		return "peer_close"
	default:
		return "other"
	}
}
func (d *sessionDiagnostics) cancellationReason(ctx context.Context) string {
	if d.h.lifetime().ctx.Err() != nil {
		return "hub_shutdown"
	}
	if ctx.Err() != nil {
		return "request_cancelled"
	}
	return "reader_error"
}
func (d *sessionDiagnostics) readFailure(ctx context.Context, err error) {
	if d == nil {
		return
	}
	reason := "reader_error"
	code := websocket.CloseStatus(err)
	switch {
	case code == 1000:
		reason = "peer_normal"
	case code == 1001:
		reason = "peer_going_away"
	case code != -1:
		reason = "peer_other_code"
	case errors.Is(err, errSessionQueueOverflow):
		reason = "business_queue_overflow"
	case errors.Is(err, io.EOF):
		reason = "reader_eof"
	case ctx.Err() != nil || d.h.lifetime().ctx.Err() != nil:
		reason = d.cancellationReason(ctx)
	}
	d.mu.Lock()
	if code != -1 {
		d.peerCode = int(code)
	}
	if d.firstReason == "" {
		d.firstReason = sessionDiagReason(reason)
	}
	d.mu.Unlock()
}
func (d *sessionDiagnostics) snapshot(event string) map[string]any {
	now := d.h.now()
	d.mu.Lock()
	reason := d.firstReason
	if reason == "" {
		reason = "handler_returned"
	}
	code := any("unknown")
	if d.peerCode != 0 {
		code = d.peerCode
	}
	data := map[string]any{"event": event, "utc": now.UTC().Format(time.RFC3339Nano), "session": d.session, "conn_seq": d.seq, "reason": reason, "phase": d.phase, "peer_close_code": code, "waiting_writers": d.waiters, "physical_write_active": d.physical, "write_bytes": d.writeBytes, "write_budget_ms": d.writeLimit.Milliseconds(), "write_age_ms": ageMillis(now, d.writeAt), "queue_items": 0, "queue_bytes": 0, "queue_peak_items": 0, "queue_peak_bytes": 0, "limit": "none", "reader_age_ms": "unknown", "pong_age_ms": "unknown", "worker_age_ms": "unknown"}
	data["physical_write_reason"] = d.lastWriteReason
	data["reader_frame_type"] = d.lastFrameType
	data["reader_frame_bytes"] = d.lastFrameBytes
	data["last_worker_type"] = d.lastWorkerType
	d.mu.Unlock()
	if c := d.client; c != nil {
		c.stateMu.Lock()
		data["reader_age_ms"] = ageMillis(now, c.lastReadAt)
		data["pong_age_ms"] = ageMillis(now, c.lastPongAt)
		c.stateMu.Unlock()
		if q := c.ingress; q != nil {
			q.mu.Lock()
			data["queue_items"] = q.items
			data["queue_bytes"] = q.bytes
			data["queue_peak_items"] = q.peakItems
			data["queue_peak_bytes"] = q.peakBytes
			data["worker_age_ms"] = ageMillis(now, q.workAt)
			if q.limitReason != "" {
				data["limit"] = q.limitReason
			}
			q.mu.Unlock()
		}
	}
	return data
}

// Bound output work across reconnect storms as well as once-per-connection.
const sessionJournalPerSecond = 200

func (h *Hub) takeSessionJournal() bool {
	now := h.now()
	h.sessionJournalMu.Lock()
	defer h.sessionJournalMu.Unlock()
	if h.sessionJournalWindow.IsZero() || now.Before(h.sessionJournalWindow) || now.Sub(h.sessionJournalWindow) >= time.Second {
		h.sessionJournalWindow = now
		h.sessionJournalCount = 0
	}
	if h.sessionJournalCount >= sessionJournalPerSecond {
		return false
	}
	h.sessionJournalCount++
	return true
}
func (d *sessionDiagnostics) dropped() {
	if d.client != nil {
		d.client.obsConn.DiagnosticDrop()
	}
}
func (d *sessionDiagnostics) emit(data map[string]any) {
	if !d.h.takeSessionJournal() {
		d.dropped()
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	write := writeKeepaliveJournal
	if d.h.sessionJournal != nil {
		write = d.h.sessionJournal
	}
	if len(raw) > 4096 || !write(raw) {
		d.dropped()
	}
}
func (d *sessionDiagnostics) end() {
	if d == nil {
		return
	}
	d.endOnce.Do(func() { d.emit(d.snapshot("session_end")) })
}
func (d *sessionDiagnostics) overflow() {
	if d == nil {
		return
	}
	d.reason("business_queue_overflow")
	d.overflowOnce.Do(func() { d.emit(d.snapshot("session_ingress_overflow")) })
}
func (d *sessionDiagnostics) writeFailure(parent, physical context.Context, err error) {
	if d == nil {
		return
	}
	class := sessionErrorClass(err)
	if physical.Err() != nil {
		class = sessionErrorClass(physical.Err())
	}
	reason := "physical_write_error"
	switch {
	case parent.Err() != nil:
		reason = d.cancellationReason(parent)
	case errors.Is(physical.Err(), context.DeadlineExceeded):
		reason = "physical_write_timeout"
	case errors.Is(physical.Err(), context.Canceled):
		reason = "physical_write_cancelled"
	}
	d.reason(reason)
	d.mu.Lock()
	d.lastWriteReason = reason
	d.mu.Unlock()
	d.writeOnce.Do(func() {
		data := d.snapshot("session_physical_write_failed")
		data["first_observed_reason"] = data["reason"]
		data["reason"] = reason
		data["error_class"] = class
		d.emit(data)
	})
}

func sessionDiagReason(reason string) string {
	switch reason {
	case "registration_failed", "device_rejected", "replaced", "revoked", "idle_timeout", "keepalive_failed", "business_queue_overflow", "physical_write_error", "physical_write_timeout", "physical_write_cancelled", "hub_shutdown", "request_cancelled", "reader_error", "reader_eof", "peer_normal", "peer_going_away", "peer_other_code", "handler_returned":
		return reason
	default:
		return "other"
	}
}
func (d *sessionDiagnostics) readFrame(typ websocket.MessageType, n int) {
	if d == nil {
		return
	}
	kind := "other"
	if typ == websocket.MessageText {
		kind = "text"
	} else if typ == websocket.MessageBinary {
		kind = "binary"
	}
	d.mu.Lock()
	d.lastFrameType = kind
	d.lastFrameBytes = n
	d.mu.Unlock()
}
func (d *sessionDiagnostics) worker(kind string) {
	if d == nil {
		return
	}
	switch kind {
	case "terminal_output", "session_event", "session_capabilities", "delivery_ack", "delivery_ack_v2", "outbound_status_v1", "delivery_status_query_v1", "session_heartbeat_v1":
	default:
		kind = "other"
	}
	d.mu.Lock()
	d.lastWorkerType = kind
	d.mu.Unlock()
}
