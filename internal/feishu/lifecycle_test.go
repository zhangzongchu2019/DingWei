package feishu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type heldSDKClient struct {
	ready   func()
	release chan struct{}
	close   func()
}

func (c *heldSDKClient) Start(context.Context) error { c.ready(); <-c.release; return nil }
func (c *heldSDKClient) Close()                      { c.close() }

func callbackEvent(id string) []byte {
	return []byte(fmt.Sprintf(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"ou_synthetic"}},"message":{"message_id":%q,"chat_type":"p2p","content":"{\"text\":\"synthetic\"}"}}}`, id))
}

func waitIngress(t *testing.T, g *LarkGateway, predicate func(IngressShutdownSnapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !predicate(g.IngressSnapshot()) {
		if time.Now().After(deadline) {
			t.Fatalf("receiver condition timed out: %+v", g.IngressSnapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

// S006's callback extension uses the real event dispatcher and temporary
// SQLite. The complete Hub HTTP/SSE/mux/WS load matrix is recorded separately.
func TestReceiverS006InflightAndLateCallbackNoDB(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "s006.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	g := &LarkGateway{botChannelID: "synthetic", logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var totalDB, afterSealDB, closes atomic.Int64
	entered, releaseCallback := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-releaseCallback:
		default:
			close(releaseCallback)
		}
	}()
	dispatch := make(chan *dispatcher.EventDispatcher, 1)
	releaseSDK := make(chan struct{})
	defer close(releaseSDK)
	g.wsFactory = func(d *dispatcher.EventDispatcher, ready func()) sdkConnection {
		dispatch <- d
		return &heldSDKClient{ready: ready, release: releaseSDK, close: func() {
			if g.IngressSnapshot().Sealed {
				t.Error("S006 Close must precede callback seal")
			}
			closes.Add(1)
		}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startResult := make(chan error, 1)
	go func() {
		startResult <- g.Start(ctx, func(ctx context.Context, m model.Message) error {
			totalDB.Add(1)
			if g.IngressSnapshot().Sealed {
				afterSealDB.Add(1)
			}
			m.Direction, m.Status = model.DirectionIn, "queued"
			if err := db.EnqueueMessage(ctx, m); err != nil {
				return err
			}
			if m.ID == "inflight" {
				close(entered)
				<-releaseCallback
			}
			return nil
		})
	}()
	disp := <-dispatch
	first := make(chan error, 1)
	go func() { _, err := disp.Do(context.Background(), callbackEvent("inflight")); first <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("S006 callback did not reach SQLite")
	}
	cancel() // same cancellation as Hub stop; SDK Start remains held
	if err := <-startResult; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stopCtx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	stopped := make(chan error, 1)
	go func() { stopped <- g.StopIngress(stopCtx) }()
	waitIngress(t, g, func(s IngressShutdownSnapshot) bool { return s.Sealed })
	_, lateErr := disp.Do(context.Background(), callbackEvent("late"))
	s := g.IngressSnapshot()
	if lateErr == nil || s.Rejected != 1 || totalDB.Load() != 1 || afterSealDB.Load() != 0 {
		t.Fatalf("S006_POST_SEAL_DB_ZERO: error=%v rejected=%d total_db=%d after_seal_db=%d", lateErr, s.Rejected, totalDB.Load(), afterSealDB.Load())
	}
	select {
	case err := <-stopped:
		t.Fatalf("S006 returned before admitted callback finished: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(releaseCallback)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := g.StopIngress(stopCtx); err != nil {
		t.Fatal(err)
	}
	s = g.IngressSnapshot()
	if s.Active != 0 || !s.CloseCompleted || closes.Load() != 1 || s.SDKStartNotJoined != 1 {
		t.Fatalf("S006 callback/Close accounting: %+v closes=%d", s, closes.Load())
	}
	t.Logf("S006 callback_extension active=%d rejected=%d total_db=%d after_seal_db=%d close_calls=%d sdk_start_not_joined=%d", s.Active, s.Rejected, totalDB.Load(), afterSealDB.Load(), closes.Load(), s.SDKStartNotJoined)
}

func TestReceiverInflightTimeoutIsVisible(t *testing.T) {
	g := &LarkGateway{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := g.eventDispatcher(func(context.Context, model.Message) error {
			close(entered)
			<-release
			return nil
		}).Do(context.Background(), callbackEvent("held"))
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := g.StopIngress(ctx)
	s := g.IngressSnapshot()
	close(release)
	if callbackErr := <-done; callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) || s.Active != 1 || !s.Sealed {
		t.Fatalf("receiver timeout lost: error=%v snapshot=%+v", err, s)
	}
}

func TestReceiverCannotStartAfterStop(t *testing.T) {
	g := &LarkGateway{}
	if err := g.StopIngress(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.Start(context.Background(), func(context.Context, model.Message) error { return nil }); !errors.Is(err, ErrReceiverDraining) {
		t.Fatalf("stopped receiver started: %v", err)
	}
}

func TestReceiverBlockedCloseIsBounded(t *testing.T) {
	g := &LarkGateway{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	closeEntered, releaseClose, releaseSDK := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(releaseSDK)
	g.wsFactory = func(_ *dispatcher.EventDispatcher, ready func()) sdkConnection {
		return &heldSDKClient{ready: ready, release: releaseSDK, close: func() {
			close(closeEntered)
			<-releaseClose
		}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() { startDone <- g.Start(ctx, func(context.Context, model.Message) error { return nil }) }()
	waitIngress(t, g, func(s IngressShutdownSnapshot) bool { return s.SDKStartNotJoined == 1 })
	stop, done := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer done()
	result := make(chan error, 1)
	go func() { result <- g.StopIngress(stop) }()
	<-closeEntered
	err := <-result
	s := g.IngressSnapshot()
	close(releaseClose)
	cancel()
	<-startDone
	waitIngress(t, g, func(s IngressShutdownSnapshot) bool { return s.CloseCompleted })
	if !errors.Is(err, context.DeadlineExceeded) || s.Sealed || s.CloseCompleted {
		t.Fatalf("blocked SDK Close reported success: error=%v snapshot=%+v", err, s)
	}
}

func TestReceiverMultiGatewayAggregatesSeals(t *testing.T) {
	a, b := &LarkGateway{botChannelID: "a"}, &LarkGateway{botChannelID: "b"}
	m := NewMultiGateway(a, b)
	if err := m.StopIngress(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*LarkGateway{a, b} {
		_, err := g.eventDispatcher(func(context.Context, model.Message) error {
			t.Error("late callback entered business handling")
			return nil
		}).Do(context.Background(), callbackEvent("late"))
		if err == nil {
			t.Fatal("late callback accepted")
		}
	}
	s := m.IngressSnapshot()
	if !s.Sealed || !s.CloseCompleted || s.Active != 0 || s.Rejected != 2 {
		t.Fatalf("multi-gateway shutdown counters: %+v", s)
	}
}
