package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"strings"
	"sync"
	"testing"
	"time"
)

type sessionDiagRecorder struct {
	mu      sync.Mutex
	rows    []map[string]any
	changes chan struct{}
}

func (r *sessionDiagRecorder) write(raw []byte) bool {
	var row map[string]any
	if len(raw) > 4096 || json.Unmarshal(raw, &row) != nil {
		return false
	}
	r.mu.Lock()
	r.rows = append(r.rows, row)
	select {
	case r.changes <- struct{}{}:
	default:
	}
	r.mu.Unlock()
	return true
}
func (r *sessionDiagRecorder) has(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row["event"] == event {
			return true
		}
	}
	return false
}
func (r *sessionDiagRecorder) one(t *testing.T, event string) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var found map[string]any
	for _, row := range r.rows {
		if row["event"] == event {
			if found != nil {
				t.Fatal("duplicate diagnostic", event)
			}
			found = row
		}
	}
	if found == nil {
		t.Fatal("diagnostic missing", event)
	}
	return found
}
func TestSessionDiagnosticsRealPeerEndAndPrivacy(t *testing.T) {
	e := newAgentEnv(t)
	logs := &sessionDiagRecorder{}
	e.hub.sessionJournal = logs.write
	conn, _, _ := e.dial(e.cred, "version=0.2.3.39")
	if conn == nil {
		t.Fatal("local peer absent")
	}
	discardInitialAgentNetworkSkill(t, e.ctx, conn)
	conn.Close(websocket.StatusNormalClosure, "private-close-text-must-not-log")
	waitFor(t, time.Second, "peer end diagnostic", func() bool { return logs.has("session_end") })
	row := logs.one(t, "session_end")
	if row["reason"] != "peer_normal" || row["peer_close_code"] != float64(1000) || row["phase"] != "online" {
		t.Fatalf("peer end cause lost: %+v", row)
	}
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), "private-close-text") || strings.Contains(string(raw), e.cred) {
		t.Fatal("private detail leaked")
	}
}
func TestSessionDiagnosticsOverflowAndPhysicalWriteCauses(t *testing.T) {
	h, _, _ := newTestHub(t)
	logs := &sessionDiagRecorder{}
	h.sessionJournal = logs.write
	d := newSessionDiagnostics(h, "fixture")
	c := &sessionClient{diag: d}
	d.client = c
	c.ingress = &sessionIngress{h: h, c: c, limitReason: "global_items", items: 3, bytes: 7, peakItems: 9, peakBytes: 19}
	d.overflow()
	d.overflow()
	d.end()
	d.end()
	row := logs.one(t, "session_ingress_overflow")
	if row["reason"] != "business_queue_overflow" || row["limit"] != "global_items" || row["queue_peak_items"] != float64(9) || row["queue_peak_bytes"] != float64(19) {
		t.Fatalf("overflow cause/occupancy lost: %+v", row)
	}
	if logs.one(t, "session_end")["reason"] != "business_queue_overflow" {
		t.Fatal("overflow lost end cause")
	}
	d = newSessionDiagnostics(h, "writer")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	d.physicalStart(123, sessionDataWriteLimit)
	d.writeFailure(context.Background(), ctx, errors.New("private-error-url-token"))
	d.writeFailure(context.Background(), ctx, errors.New("duplicate"))
	d.end()
	row = logs.one(t, "session_physical_write_failed")
	if row["reason"] != "physical_write_timeout" || row["error_class"] != "deadline" || row["write_bytes"] != float64(123) || row["write_budget_ms"] != float64(20000) {
		t.Fatalf("physical write cause lost: %+v", row)
	}
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), "private-error") {
		t.Fatal("raw write exception leaked")
	}
}
func TestSessionWritePolicyKeepsControlAndDataBounds(t *testing.T) {
	for _, tc := range []struct {
		kind string
		size int
		want time.Duration
	}{{"outbound_receipt", 64 << 10, 5 * time.Second}, {"outbound_receipt", (64 << 10) + 1, 20 * time.Second}, {"session_heartbeat_result_v1", 32, 5 * time.Second}, {"business", 32, 20 * time.Second}, {"", 8 << 20, 20 * time.Second}} {
		if got := sessionPhysicalWriteBudget(model.Envelope{Meta: map[string]any{"type": tc.kind}}, tc.size); got != tc.want {
			t.Fatalf("write class %q size%d =%s want%s", tc.kind, tc.size, got, tc.want)
		}
	}
	if sessionDiagReason("raw-untrusted-reason") != "other" {
		t.Fatal("diagnostic reason is not a fixed enum")
	}
}
func TestSessionIngressGlobalDescriptorBudgetAndRelease(t *testing.T) {
	h, _, _ := newTestHub(t)
	queues := make([]*sessionIngress, 32)
	for i := range queues {
		q := &sessionIngress{h: h}
		queues[i] = q
		for n := 0; n < 2048; n++ {
			if !q.reserve(0) {
				t.Fatal("approved descriptor window rejected", i, n)
			}
		}
		if q.reserve(0) {
			t.Fatal("per-connection item budget unbounded")
		}
	}
	q := &sessionIngress{h: h}
	if q.reserve(0) || q.limitReason != "global_items" {
		t.Fatal("global descriptor budget unbounded")
	}
	for _, q := range queues {
		for n := 0; n < 2048; n++ {
			q.release(0)
		}
	}
	if h.ingressBudgetItems != 0 || h.ingressBudgetBytes != 0 {
		t.Fatal("global descriptor budget leaked")
	}
	if !q.reserve(1) {
		t.Fatal("released descriptor budget not reusable")
	}
	q.release(1)
}

func TestSessionDiagnosticsRateAndLineBounds(t *testing.T) {
	h, _, _ := newTestHub(t)
	at := time.Now()
	h.NowFunc = func() time.Time { return at }
	calls := 0
	h.sessionJournal = func(raw []byte) bool {
		calls++
		if len(raw) > 4096 {
			t.Fatal("oversize diagnostic reached sink")
		}
		return true
	}
	reg := obs.New(nil)
	c := &sessionClient{obsConn: reg.OpenWS("session")}
	for i := 0; i < 201; i++ {
		d := newSessionDiagnostics(h, "fixture")
		d.client = c
		d.end()
	}
	if calls != 200 {
		t.Fatal("journal rate not bounded", calls)
	}
	for _, v := range reg.Snapshot().WS {
		if v.Kind == "session" && v.DiagnosticDrop != 1 {
			t.Fatal("rate drop not visible", v.DiagnosticDrop)
		}
	}
	at = at.Add(time.Second)
	d := newSessionDiagnostics(h, "fixture")
	d.end()
	if calls != 201 {
		t.Fatal("journal budget did not refill")
	}
	// A malicious test hook cannot make the real writer emit an unbounded line.
	d = newSessionDiagnostics(h, "fixture")
	d.client = c
	d.emit(map[string]any{"event": "session_end", "reason": strings.Repeat("x", 5000)})
	if calls != 201 {
		t.Fatal("oversize line not rejected before sink")
	}
}
func TestSessionDiagnosticsRevokeAndShutdownReasons(t *testing.T) {
	for _, reason := range []string{"revoked", "hub_shutdown"} {
		t.Run(reason, func(t *testing.T) {
			e := newAgentEnv(t)
			logs := &sessionDiagRecorder{}
			e.hub.sessionJournal = logs.write
			conn, _, _ := e.dial(e.cred, "version=0.2.3.39")
			if conn == nil {
				t.Fatal("missing test peer")
			}
			defer conn.CloseNow()
			discardInitialAgentNetworkSkill(t, e.ctx, conn)
			go func() {
				for {
					if _, _, err := conn.Read(e.ctx); err != nil {
						return
					}
				}
			}()
			if reason == "revoked" {
				if _, err := e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
					t.Fatal(err)
				}
			} else {
				ctx, cancel := context.WithTimeout(e.ctx, 2*time.Second)
				defer cancel()
				if err := e.hub.CloseContext(ctx); err != nil {
					t.Fatal(err)
				}
			}
			waitFor(t, time.Second, "lifecycle reason", func() bool { return logs.has("session_end") })
			row := logs.one(t, "session_end")
			if row["reason"] != reason {
				t.Fatalf("lifecycle reason lost: %+v", row)
			}
		})
	}
}

func TestSessionIngressReadByteBoundsAndCancelledDrain(t *testing.T) {
	h, _, _ := newTestHub(t)
	q := &sessionIngress{h: h, frames: make(chan sessionFrame, sessionIngressItems), done: make(chan struct{})}
	if !q.reserve(0) || !q.addBytes(32<<20) {
		t.Fatal("exact read-byte bound rejected")
	}
	if q.addBytes(1) || q.limitReason != "connection_bytes" {
		t.Fatal("read path bypassed connection byte limit")
	}
	q.release(32 << 20)
	h.ingressBudgetBytes = 256 << 20
	if !q.reserve(0) {
		t.Fatal("zero-byte descriptor at global byte boundary rejected")
	}
	if q.addBytes(1) || q.limitReason != "global_bytes" {
		t.Fatal("read path bypassed global byte limit")
	}
	q.release(0)
	h.ingressBudgetBytes = 0
	if !q.reserve(10) {
		t.Fatal("active reservation")
	}
	q.frames <- sessionFrame{data: make([]byte, 10)}
	if _, _, err := q.next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !q.reserve(20) {
		t.Fatal("pending reservation")
	}
	q.frames <- sessionFrame{data: make([]byte, 20)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := q.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled worker continued", err)
	}
	close(q.done)
	q.joinAndRelease()
	if h.ingressBudgetItems != 0 || h.ingressBudgetBytes != 0 || q.items != 0 || q.bytes != 0 || len(q.frames) != 0 {
		t.Fatal("active/pending cancellation leaked budget or references")
	}
}
