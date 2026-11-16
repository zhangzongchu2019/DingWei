package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type keepaliveGatedRepo struct {
	*store.SQLite
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *keepaliveGatedRepo) GetControlTask(ctx context.Context, id string) (*model.ControlTask, error) {
	if id == "keepalive-business-gate" {
		r.once.Do(func() { close(r.entered) })
		select {
		case <-r.release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.SQLite.GetControlTask(ctx, id)
}
func TestSessionIngressLongOutputDoesNotStarvePong(t *testing.T) {
	for _, slow := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "slow_business"}[slow], func(t *testing.T) {
			h, db, ctx := newTestHub(t)
			useTestSessionKeepalive(t, h, 50*time.Millisecond, 500*time.Millisecond)
			gated := &keepaliveGatedRepo{SQLite: db, entered: make(chan struct{}), release: make(chan struct{})}
			h.Repo = gated
			if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc", Name: "svc", DeliveryType: "ws", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			secret, key, err := h.IssueAPIKey(ctx, "svc", "person")
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
			srv := httptest.NewServer(mux)
			defer srv.Close()
			peer := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
			defer peer.CloseNow()
			defer close(gated.release)
			h.mu.Lock()
			c := h.sessionClients[key.ID]["developer"]
			h.mu.Unlock()
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				for {
					if _, _, err := peer.Read(ctx); err != nil {
						return
					}
				}
			}()
			start := time.Now()
			sendKeepaliveFixture(t, ctx, peer, model.Envelope{ID: "output-prefix", From: sessionAddress("developer", key.ID), Body: strings.Repeat("x", 8192), Meta: map[string]any{"type": "terminal_output"}})
			if slow {
				sendKeepaliveFixture(t, ctx, peer, model.Envelope{ID: "gate", From: sessionAddress("developer", key.ID), Meta: map[string]any{"control_task_id": "keepalive-business-gate"}})
				select {
				case <-gated.entered:
				case <-time.After(time.Second):
					t.Fatal("business did not enter controlled gate")
				}
			}
			for i := 0; i < 63; i++ {
				sendKeepaliveFixture(t, ctx, peer, model.Envelope{ID: "output", From: sessionAddress("developer", key.ID), Body: strings.Repeat("x", 8192), Meta: map[string]any{"type": "terminal_output"}})
			}
			observe := 200 * time.Millisecond
			if slow {
				observe = 6100 * time.Millisecond
			}
			timer := time.NewTimer(observe)
			defer timer.Stop()
			select {
			case <-readDone:
				t.Fatal("healthy Pong connection closed while business was blocked")
			case <-timer.C:
			}
			c.stateMu.Lock()
			pong := c.lastPongAt
			c.stateMu.Unlock()
			if !pong.After(start) {
				t.Fatal("Reader failed to consume Pong during long output/business gate")
			}
			if !h.sessionOnline(key.ID, "developer") {
				t.Fatal("long business falsely marked Agent offline")
			}
			if c.ingress == nil {
				t.Fatal("session has no independent Reader")
			}
			c.ingress.mu.Lock()
			bytes := c.ingress.bytes
			c.ingress.mu.Unlock()
			if bytes > sessionIngressBytes {
				t.Fatalf("queue byte budget exceeded: %d", bytes)
			}
			if slow {
				stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				err := h.CloseContext(stopCtx)
				stop()
				if err != nil {
					t.Fatal("Hub did not join blocked business/Reader on shutdown", err)
				}
				h.ingressBudgetMu.Lock()
				left := h.ingressBudgetBytes
				h.ingressBudgetMu.Unlock()
				if left != 0 {
					t.Fatal("shutdown retained ingress budget", left)
				}
			}
		})
	}
}
func TestSessionIngressQueueBudgetAndRelease(t *testing.T) {
	h, _, _ := newTestHub(t)
	q := &sessionIngress{h: h, frames: make(chan sessionFrame, sessionIngressItems), done: make(chan struct{})}
	if !q.reserve(sessionIngressBytes) {
		t.Fatal("exact byte budget rejected")
	}
	if q.reserve(1) {
		t.Fatal("byte budget exceeded")
	}
	q.release(sessionIngressBytes)
	h.ingressBudgetMu.Lock()
	h.ingressBudgetBytes = sessionIngressGlobalBytes
	h.ingressBudgetMu.Unlock()
	if q.reserve(1) {
		t.Fatal("global budget exceeded")
	}
	h.ingressBudgetMu.Lock()
	h.ingressBudgetBytes = 0
	h.ingressBudgetMu.Unlock()
	if !q.reserve(16) {
		t.Fatal("budget not reusable")
	}
	q.frames <- sessionFrame{data: make([]byte, 16)}
	close(q.done)
	q.joinAndRelease()
	if h.ingressBudgetBytes != 0 || q.bytes != 0 {
		t.Fatal("connection close leaked global budget")
	}
}
func TestSessionKeepaliveConfigurationAndFailureClassification(t *testing.T) {
	h, _, _ := newTestHub(t)
	for _, v := range [][2]string{{"", ""}, {"10s", "1s"}, {"60s", "14s"}} {
		if err := h.ConfigureSessionKeepalive(v[0], v[1]); err != nil {
			t.Fatal(v, err)
		}
	}
	for _, v := range [][2]string{{"-1s", ""}, {"1s", ""}, {"60s", "15s"}, {"x", ""}, {"", "31s"}, {"", "-1s"}, {"9999999999999999999h", ""}} {
		if h.ConfigureSessionKeepalive(v[0], v[1]) == nil {
			t.Fatal("invalid config accepted", v)
		}
	}
	if keepaliveErrorClass(errors.New("raw private exception"), context.DeadlineExceeded) != "deadline" || keepaliveErrorClass(context.Canceled, nil) != "cancelled" || keepaliveErrorClass(websocket.CloseError{Code: 1000}, nil) != "closed" {
		t.Fatal("failure classes collapsed")
	}
}
func TestSessionKeepaliveJournalBoundedAndPrivate(t *testing.T) {
	h, _, _ := newTestHub(t)
	reg := obs.New(nil)
	c := &sessionClient{sessionName: "fixture", connSeq: 7, obsConn: reg.OpenWS("session")}
	var rows [][]byte
	h.keepaliveJournal = func(b []byte) bool { rows = append(rows, append([]byte(nil), b...)); return false }
	h.recordKeepaliveFailure(c, time.Second, time.Second, time.Second, "deadline")
	if len(rows) != 1 || strings.Contains(string(rows[0]), "private") || !strings.Contains(string(rows[0]), `"reader_age_ms":"unknown"`) {
		t.Fatal("journal missing or fabricated", string(rows[0]))
	}
	for _, v := range reg.Snapshot().WS {
		if v.Kind == "session" && v.DiagnosticDrop != 1 {
			t.Fatal("missing dropped diagnostic count", v)
		}
	}
}

func sendKeepaliveFixture(t *testing.T, ctx context.Context, c *websocket.Conn, e model.Envelope) {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func TestSessionIngressOverflowClosesWithoutBlockingOrAck(t *testing.T) {
	h, _, ctx := newTestHub(t)
	done := make(chan error, 1)
	reg := obs.New(nil)
	allCtx, stopAll := context.WithCancel(ctx)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		readerCtx, cancel := context.WithCancel(allCtx)
		defer cancel()
		c := &sessionClient{conn: conn, obsConn: reg.OpenWS("session")}
		q := h.startSessionIngress(readerCtx, cancel, c)
		<-q.done
		q.mu.Lock()
		cause := q.err
		q.mu.Unlock()
		q.joinAndRelease()
		c.obsConn.Closed(nil)
		done <- cause
	}))
	defer srv.Close()
	defer stopAll()
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.CloseNow()
	for i := 0; i < sessionIngressItems+1; i++ {
		if peer.Write(ctx, websocket.MessageText, []byte("bounded reliable business")) != nil {
			break
		}
	}
	select {
	case e := <-done:
		if !errors.Is(e, errSessionQueueOverflow) {
			t.Fatal("wrong overflow reason", e)
		}
	case <-time.After(time.Second):
		t.Fatal("full queue blocked Reader")
	}
	if h.ingressBudgetBytes != 0 {
		t.Fatal("overflow leaked bytes", h.ingressBudgetBytes)
	}
	for _, v := range reg.Snapshot().WS {
		if v.Kind == "session" && (v.PingFail != 0 || v.Closed["business_queue_overflow"] != 1) {
			t.Fatal("overload misclassified as keepalive", v)
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, _, err = peer.Read(readCtx); err == nil {
		t.Fatal("memory queue produced fake business acceptance")
	}
}
func TestSessionKeepaliveTimeoutJournalOnceAndCancellationIsNotFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "planned_cancel"}[cancelled], func(t *testing.T) {
			h, _, ctx := newTestHub(t)
			useTestSessionKeepalive(t, h, 20*time.Millisecond, 80*time.Millisecond)
			reg := obs.New(nil)
			logs := make(chan []byte, 2)
			h.keepaliveJournal = func(b []byte) bool { logs <- append([]byte(nil), b...); return true }
			done := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				c := &sessionClient{conn: conn, obsConn: reg.OpenWS("session"), sessionName: "fixture", connSeq: 9}
				defer c.obsConn.Closed(nil)
				call, cancel := context.WithCancel(ctx)
				defer cancel()
				if cancelled {
					cancel()
				}
				h.watchSessionKeepalive(call, c)
				close(done)
			}))
			defer srv.Close()
			peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.CloseNow()
			// No peer Reader: the socket is connected but no Pong can be consumed.
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("keepalive close exceeded failure budget")
			}
			want := 1
			if cancelled {
				want = 0
			}
			if len(logs) != want {
				t.Fatal("failure journal count", len(logs), want)
			}
			if !cancelled {
				if raw := <-logs; !strings.Contains(string(raw), `"error_class":"deadline"`) {
					t.Fatal("timeout classified incorrectly", string(raw))
				}
			}
			for _, v := range reg.Snapshot().WS {
				if v.Kind == "session" && v.PingFail != uint64(want) {
					t.Fatal("planned cancellation counted as ping failure", v)
				}
			}
		})
	}
}

func TestSessionKeepaliveCancellationDuringActualPing(t *testing.T) {
	h, _, ctx := newTestHub(t)
	useTestSessionKeepalive(t, h, 20*time.Millisecond, 5*time.Second)
	reg := obs.New(nil)
	logs := make(chan []byte, 4)
	h.keepaliveJournal = func(b []byte) bool { logs <- append([]byte(nil), b...); return true }
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	done, pingSeen := make(chan struct{}), make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		c := &sessionClient{conn: conn, obsConn: reg.OpenWS("session"), sessionName: "fixture", connSeq: 9}
		defer c.obsConn.Closed(nil)
		h.watchSessionKeepalive(call, c)
		close(done)
	}))
	defer srv.Close()
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		OnPingReceived: func(context.Context, []byte) bool {
			once.Do(func() { close(pingSeen) })
			return false // Deliberately withhold Pong: Ping is really in flight.
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.CloseNow()
	readDone := make(chan struct{})
	go func() { defer close(readDone); _, _, _ = peer.Read(ctx) }()
	select {
	case <-pingSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("peer never observed an actual Ping")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight Ping ignored planned cancellation")
	}
	if len(logs) != 0 {
		t.Fatalf("planned in-flight cancellation wrote failure journal: %s", <-logs)
	}
	for _, v := range reg.Snapshot().WS {
		if v.Kind == "session" && (v.PingFail != 0 || v.Closed["keepalive_failed"] != 0) {
			t.Fatalf("planned in-flight cancellation was classified as keepalive failure: %+v", v)
		}
	}
	_ = peer.CloseNow()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("peer Reader did not stop")
	}
}

func TestSessionKeepaliveJournalUsesBudgetCountAndInjectedClock(t *testing.T) {
	now := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)
	h := &Hub{NowFunc: func() time.Time { return now }}
	ctx := context.Background()
	c := &sessionClient{sessionName: "fixture", lastReadAt: now.Add(-300 * time.Millisecond), lastPongAt: now.Add(-200 * time.Millisecond)}
	done := make(chan struct{})
	close(done)
	q := &sessionIngress{h: h, c: c, frames: make(chan sessionFrame, 1), done: done}
	c.ingress = q
	if !q.reserve(3) {
		t.Fatal("could not reserve fixture")
	}
	q.frames <- sessionFrame{typ: websocket.MessageText, data: []byte("abc"), receivedAt: now}
	if _, _, err := q.next(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.joinAndRelease()
	var raw []byte
	h.keepaliveJournal = func(b []byte) bool { raw = append([]byte(nil), b...); return true }
	h.recordKeepaliveFailure(c, time.Second, time.Second, 13*time.Millisecond, "deadline")
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]float64{"queue_items": 1, "queue_bytes": 3, "reader_age_ms": 300, "pong_age_ms": 200, "worker_age_ms": 0, "elapsed_ms": 13} {
		if event[field] != want {
			t.Fatalf("diagnostic %s=%v, want %v (active item and one injected clock)", field, event[field], want)
		}
	}
	if event["utc"] != now.Format(time.RFC3339Nano) {
		t.Fatal("journal ignored injected clock", event["utc"])
	}
}

func TestSessionIngressReadFailurePreservesTransportCause(t *testing.T) {
	h, _, _ := newTestHub(t)
	q := &sessionIngress{h: h, frames: make(chan sessionFrame, 1)}
	q.fail(io.EOF)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := q.next(ctx); !errors.Is(err, io.EOF) {
		t.Fatal("reader EOF was relabelled as request cancellation", err)
	}
}

// Contend the production write slot, rather than fabricating a write result.
// No control frame is written on timeout; a committed ACK must remain committed.
func TestSessionWriteDefaultControlFailuresAreObservableAndPreserveCommit(t *testing.T) {
	counter := func() obs.WSView {
		for _, v := range obs.Default.Snapshot().WS {
			if v.Kind == "session" {
				return v
			}
		}
		t.Fatal("session observation missing")
		return obs.WSView{}
	}
	for _, kind := range []string{"outbound_receipt", "delivery_ack_confirm"} {
		t.Run(kind, func(t *testing.T) {
			f := newACKV2Fixture(t, true)
			before := counter()
			// The fixture has already initialized the real connection's write slot.
			<-f.client.writeGate
			held := true
			defer func() {
				if held {
					f.client.writeGate <- struct{}{}
				}
			}()
			started := time.Now()
			if kind == "outbound_receipt" {
				f.hub.writeOutboundReceipt(f.ctx, f.client, "stable-source", strings.Repeat("a", 32), "queued", "ack-wire")
			} else {
				f.hub.handleDeliveryACKV2(f.ctx, f.client, f.ack("queued", "", "", f.stamp), f.record())
			}
			if elapsed := time.Since(started); elapsed < 1900*time.Millisecond || elapsed > 4*time.Second {
				t.Fatalf("control write did not respect total two-second limit: %s", elapsed)
			}
			after := counter()
			if kind == "outbound_receipt" {
				if after.OutboundReceiptWriteFail != before.OutboundReceiptWriteFail+1 || after.DeliveryACKConfirmWriteFail != before.DeliveryACKConfirmWriteFail {
					t.Fatalf("outbound receipt failure not separately observable: before=%+v after=%+v", before, after)
				}
				if d := f.record(); d.ID != "ack-wire" || d.State != "sent" {
					t.Fatalf("receipt failure changed durable delivery: %+v", d)
				}
			} else {
				if after.DeliveryACKConfirmWriteFail != before.DeliveryACKConfirmWriteFail+1 || after.OutboundReceiptWriteFail != before.OutboundReceiptWriteFail {
					t.Fatalf("ACK confirm failure not separately observable: before=%+v after=%+v", before, after)
				}
				if d := f.record(); d.ID != "ack-wire" || d.State != "delivered" {
					t.Fatalf("confirm failure rolled back committed ACK: %+v", d)
				}
			}
			if after.DiagnosticDrop != before.DiagnosticDrop {
				t.Fatal("control failure polluted diagnostic drop counter")
			}
			f.client.writeGate <- struct{}{}
			held = false
			if kind == "outbound_receipt" {
				f.hub.writeOutboundReceipt(f.ctx, f.client, "stable-source", strings.Repeat("a", 32), "queued", "ack-wire")
				got := f.readEnvUntil(f.conn, time.Second, func(e model.Envelope) bool { return metaString(e.Meta, "type") == "outbound_receipt" })
				if got == nil || metaString(got.Meta, "delivery_id") != "ack-wire" || metaString(got.Meta, "source_message_id") != "stable-source" {
					t.Fatalf("original receipt not restored: %+v", got)
				}
			} else {
				f.sendAndConfirm("queued", "", "", f.stamp)
			}
			final := counter()
			if final.OutboundReceiptWriteFail != after.OutboundReceiptWriteFail || final.DeliveryACKConfirmWriteFail != after.DeliveryACKConfirmWriteFail {
				t.Fatal("successful control write counted as failure")
			}
		})
	}
}
