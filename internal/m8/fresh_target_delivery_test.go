package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestFreshTargetAdmissionQueuesBeforeCapabilityAndPromotesExactlyOnce(t *testing.T) {
	h, db, ctx := newTestHub(t)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc-fresh-target", Name: "fresh-target", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "svc-fresh-target", "person")
	if err != nil {
		t.Fatal(err)
	}
	const session = "fresh"
	registered := make(chan struct{})
	release := make(chan struct{})
	h.sessionBeforePublishHook = func(keyID, sessionName string) {
		if keyID == key.ID && sessionName == session {
			close(registered)
			<-release
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	type dialResult struct {
		conn *websocket.Conn
		err  error
	}
	dialed := make(chan dialResult, 1)
	go func() {
		conn := dialSessionWithHeaderRaw(t, ctx, srv.URL, session, key.ID, secret, nil)
		dialed <- dialResult{conn: conn}
	}()
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("fresh target did not reach registered-but-unpublished window")
	}
	keyID := key.ID
	env := model.Envelope{ID: "fresh-before-caps", From: "source#source-key#UnifiedRobot", To: session + "#" + keyID, Body: "deliver once", TS: time.Now().Unix()}
	if err := h.routeToSession(ctx, keyID, session, env, false); err != nil {
		t.Fatalf("registered-but-unpublished fresh target must queue: %v", err)
	}
	// Current and rollback-era claim filters both exclude capability_pending.
	if preCap, err := db.ClaimSessionDelivery(ctx, keyID, session, time.Now().UTC()); err != nil || preCap != nil {
		t.Fatalf("capability_pending must not be claimed before capability: %+v err=%v", preCap, err)
	}
	close(release)
	target := (<-dialed).conn
	defer target.Close(websocket.StatusNormalClosure, "done")
	_ = readEnvelopeIncludingSystem(t, ctx, target)
	if err := writeEnvelope(ctx, target, model.Envelope{ID: "caps-fresh", To: "workpulse#" + keyID, Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, target)
	if got.ID != env.ID || got.Body != env.Body || metaString(got.Meta, "type") != "delivery" || metaString(got.Meta, "delivery_id") == "" {
		t.Fatalf("fresh target delivery=%+v", got)
	}
	if err := writeEnvelope(ctx, target, model.Envelope{ID: "ack-fresh", To: "workpulse#" + keyID, Meta: map[string]any{"type": "delivery_ack", "delivery_id": metaString(got.Meta, "delivery_id"), "state": "processed"}}); err != nil {
		t.Fatal(err)
	}
	deliveryID := metaString(got.Meta, "delivery_id")
	waitFor(t, time.Second, "fresh target delivery processed once", func() bool {
		d, _ := db.GetSessionDelivery(ctx, deliveryID)
		return d != nil && d.State == "processed" && d.Attempts == 1
	})
}

func TestFreshTargetAdmissionDoesNotAdmitOrdinaryOfflineEndpoint(t *testing.T) {
	h, db, ctx := newTestHub(t)
	const keyID, session = "fresh-key", "offline"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: keyID, SessionName: session, LastSeenAt: time.Now().UTC(), Active: false}); err != nil {
		t.Fatal(err)
	}
	err := h.routeToSession(context.Background(), keyID, session, model.Envelope{ID: "ordinary-offline", From: "source#source-key#UnifiedRobot", To: session + "#" + keyID, Body: "must fail", TS: time.Now().Unix()}, false)
	if err == nil {
		t.Fatal("ordinary offline endpoint unexpectedly admitted")
	}
}

func TestFreshTargetRouteSeesCapabilityResolvedBeforeItsPendingInsert(t *testing.T) {
	for _, tc := range []struct {
		name        string
		deliveryACK bool
	}{
		{name: "ack_true_queues_and_delivers_once", deliveryACK: true},
		{name: "ack_false_fails_without_stranded_pending_row", deliveryACK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db, ctx := newTestHub(t)
			if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc-fresh-race", Name: "fresh-race", DeliveryType: "ws", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			secret, key, err := h.IssueAPIKey(ctx, "svc-fresh-race", "person")
			if err != nil {
				t.Fatal(err)
			}
			const session = "fresh-race"
			registered := make(chan struct{})
			releasePublish := make(chan struct{})
			h.sessionBeforePublishHook = func(keyID, sessionName string) {
				if keyID == key.ID && sessionName == session {
					close(registered)
					<-releasePublish
				}
			}
			routeAtInsertWindow := make(chan struct{})
			releaseRoute := make(chan struct{})
			h.freshTargetBeforePendingInsertHook = func(keyID, sessionName string) {
				if keyID == key.ID && sessionName == session {
					close(routeAtInsertWindow)
					<-releaseRoute
				}
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
			srv := httptest.NewServer(mux)
			defer srv.Close()
			type dialResult struct {
				conn *websocket.Conn
				err  error
			}
			dialed := make(chan dialResult, 1)
			go func() {
				conn := dialSessionWithHeaderRaw(t, ctx, srv.URL, session, key.ID, secret, nil)
				dialed <- dialResult{conn: conn}
			}()
			select {
			case <-registered:
			case <-time.After(time.Second):
				t.Fatal("fresh target did not reach registered-but-unpublished window")
			}
			env := model.Envelope{ID: "late-resolve-" + tc.name, From: "source#source-key#UnifiedRobot", To: session + "#" + key.ID, Body: "deliver after resolve", TS: time.Now().Unix()}
			routed := make(chan error, 1)
			go func() { routed <- h.routeToSession(ctx, key.ID, session, env, false) }()
			select {
			case <-routeAtInsertWindow:
			case <-time.After(time.Second):
				t.Fatal("route did not reach pre-insert window")
			}

			// Let the endpoint publish and report its capability while the router is
			// still paused just before INSERT. The admission mutex must make the
			// router observe this result rather than write a late pending row.
			close(releasePublish)
			var target *websocket.Conn
			select {
			case got := <-dialed:
				if got.err != nil {
					t.Fatal(got.err)
				}
				target = got.conn
			case <-time.After(time.Second):
				t.Fatal("target did not complete websocket dial")
			}
			defer target.Close(websocket.StatusNormalClosure, "done")
			_ = readEnvelopeIncludingSystem(t, ctx, target)
			if err := writeEnvelope(ctx, target, model.Envelope{ID: "caps-late-" + tc.name, To: "workpulse#" + key.ID, Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": tc.deliveryACK}}); err != nil {
				t.Fatal(err)
			}
			admission := h.capabilityPendingAdmission(key.ID, session)
			if admission == nil {
				t.Fatal("fresh admission disappeared before capability resolution")
			}
			waitFor(t, time.Second, "capability resolution before route insert", func() bool {
				admission.mu.Lock()
				defer admission.mu.Unlock()
				return admission.resolved && admission.deliveryACK == tc.deliveryACK
			})
			close(releaseRoute)

			if tc.deliveryACK {
				if err := <-routed; err != nil {
					t.Fatalf("ACK=true route after resolved capability: %v", err)
				}
				got := readEnvelope(t, ctx, target)
				if got.ID != env.ID || metaString(got.Meta, "type") != "delivery" {
					t.Fatalf("ACK=true delivered envelope=%+v", got)
				}
				if metaString(got.Meta, "delivery_id") == "" {
					t.Fatal("ACK=true delivery missing delivery_id")
				}
			} else {
				if err := <-routed; err == nil {
					t.Fatal("ACK=false route unexpectedly succeeded")
				}
				// A second resolver pass must find nothing. Without the admission
				// serialization, the late INSERT leaves exactly one pending row here.
				dead, err := db.ResolveCapabilityPendingSessionDeliveries(ctx, key.ID, session, false, time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				if len(dead) != 0 {
					t.Fatalf("ACK=false left %d stranded capability_pending rows", len(dead))
				}
			}

		})
	}
}

func TestCapabilityPendingCleanupPeriodicSweepExpiresNeverReconnectedDelivery(t *testing.T) {
	h, db, ctx := newTestHub(t)
	now := time.Now().UTC()
	delivery := model.SessionDelivery{
		ID:              "dw-dlv-periodic-expiry",
		KeyID:           "fresh-key",
		SessionName:     "never-reconnects",
		Envelope:        `{}`,
		SourceMessageID: "periodic-expiry-source",
		SourceType:      "session_inbound",
		Provenance:      capabilityPendingProvenance,
		State:           capabilityPendingState,
		CreatedAt:       now.Add(-capabilityPendingTTL - time.Second),
	}
	if err := db.EnqueueSessionDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.runCapabilityPendingCleanup(ctx, stop, ticks)
	}()
	// The loop's startup sweep ran before this test inserted the row; this tick
	// proves the periodic path, which is the only path for a target that never
	// reconnects and therefore never re-reports a capability.
	ticks <- now
	waitFor(t, time.Second, "periodic cleanup expires never-reconnected delivery", func() bool {
		got, _ := db.GetSessionDelivery(ctx, delivery.ID)
		return got != nil && got.State == "dead" && got.LastError == "delivery_capability_unconfirmed_expired"
	})
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("periodic cleanup loop did not stop")
	}
}

func TestFreshTargetFailureNotifiesActualOnlineSender(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause string
	}{
		{name: "ack_false", cause: "delivery_ack_unsupported"},
		{name: "expiry", cause: "delivery_capability_unconfirmed_expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db, ctx := newTestHub(t)
			if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc-fresh-notice", Name: "fresh-notice", DeliveryType: "ws", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			secret, key, err := h.IssueAPIKey(ctx, "svc-fresh-notice", "person")
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
			srv := httptest.NewServer(mux)
			defer srv.Close()
			source := dialSessionWithHeaderRaw(t, ctx, srv.URL, "source", key.ID, secret, nil)
			defer source.Close(websocket.StatusNormalClosure, "done")
			waitSessionOnline(t, h, key.ID, "source")
			original := model.Envelope{ID: "fresh-failure-" + tc.name, From: sessionAddress("source", key.ID), To: sessionAddress("target", key.ID), Body: "please deliver", TS: time.Now().Unix()}

			if tc.name == "ack_false" {
				registered := make(chan struct{})
				release := make(chan struct{})
				h.sessionBeforePublishHook = func(keyID, sessionName string) {
					if keyID == key.ID && sessionName == "target" {
						close(registered)
						<-release
					}
				}
				dialed := make(chan *websocket.Conn, 1)
				go func() { dialed <- dialSessionWithHeaderRaw(t, ctx, srv.URL, "target", key.ID, secret, nil) }()
				select {
				case <-registered:
				case <-time.After(time.Second):
					t.Fatal("target did not reach admission window")
				}
				if err := h.routeToSession(ctx, key.ID, "target", original, false); err != nil {
					t.Fatalf("fresh target route: %v", err)
				}
				close(release)
				target := <-dialed
				defer target.Close(websocket.StatusNormalClosure, "done")
				_ = readEnvelopeIncludingSystem(t, ctx, target)
				if err := writeEnvelope(ctx, target, model.Envelope{ID: "caps-noack", To: "workpulse#" + key.ID, Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": false}}); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := json.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "dw-dlv-expiry-notice", KeyID: key.ID, SessionName: "target", Envelope: string(raw), SourceMessageID: original.ID, SourceType: "session_inbound", Provenance: capabilityPendingProvenance, State: capabilityPendingState, CreatedAt: time.Now().UTC().Add(-capabilityPendingTTL - time.Second)}); err != nil {
					t.Fatal(err)
				}
				h.expireCapabilityPendingDeliveries(ctx, time.Now().UTC())
			}

			notice := readEnvelopeOfType(t, ctx, source, "delivery_status")
			if metaString(notice.Meta, "reason_code") != tc.cause || metaString(notice.Meta, "delivery_id") == "" {
				t.Fatalf("sender failure notice=%+v", notice)
			}
		})
	}
}

func TestFreshTargetStartupSweepExpiresPreseededPendingDelivery(t *testing.T) {
	h, db, ctx := newTestHub(t)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc-fresh-startup", Name: "fresh-startup", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "svc-fresh-startup", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	source := dialSessionWithHeaderRaw(t, ctx, srv.URL, "source", key.ID, secret, nil)
	defer source.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, h, key.ID, "source")

	// New starts the same one-shot sweep before a process can accept a source
	// websocket. Stop that empty initial worker, re-arm its once guard, then
	// exercise the exact startup-sweep function with a real online source. The
	// separate periodic test covers a source that never reconnects.
	close(h.capabilityPendingCleanupStop)
	h.capabilityPendingCleanupStop = nil
	h.capabilityPendingCleanupWG.Wait()
	h.capabilityPendingCleanupOnce = sync.Once{}

	original := model.Envelope{ID: "fresh-startup-original", From: sessionAddress("source", key.ID), To: sessionAddress("never", key.ID), Body: "must report expiry", TS: time.Now().Unix()}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	delivery := model.SessionDelivery{ID: "dw-dlv-preseeded", KeyID: key.ID, SessionName: "never", Envelope: string(raw), SourceMessageID: original.ID, SourceType: "session_inbound", Provenance: capabilityPendingProvenance, State: capabilityPendingState, CreatedAt: time.Now().UTC().Add(-capabilityPendingTTL - time.Second)}
	if err := db.EnqueueSessionDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	h.startCapabilityPendingCleanup()
	got, err := db.GetSessionDelivery(ctx, delivery.ID)
	if err != nil || got == nil || got.State != "dead" || got.LastError != "delivery_capability_unconfirmed_expired" {
		t.Fatalf("startup sweep delivery=%+v err=%v", got, err)
	}
	notice := readEnvelopeOfType(t, ctx, source, "delivery_status")
	if metaString(notice.Meta, "reason_code") != "delivery_capability_unconfirmed_expired" || metaString(notice.Meta, "delivery_id") != delivery.ID {
		t.Fatalf("startup sweep sender failure notice=%+v", notice)
	}
}

func TestHubCloseWaitsForCapabilityPendingCleanup(t *testing.T) {
	h, _, _ := newTestHub(t)
	reached := make(chan struct{})
	release := make(chan struct{})
	h.capabilityPendingCleanupBeforeExit = func() {
		close(reached)
		<-release
	}
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("cleanup goroutine did not receive Close stop")
	}
	select {
	case <-closed:
		close(release)
		t.Fatal("Hub.Close returned before capability cleanup exited")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Hub.Close did not wait for capability cleanup exit")
	}
}

func TestFreshTargetStartupSweepFailureDoesNotBlockHubConstruction(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "fresh-startup-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// A closed repository makes the synchronous startup sweep fail. New must
	// only log that operational failure, not prevent the Hub from starting.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	h := New(db, ConsoleAuthDisabled())
	if elapsed := time.Since(started); elapsed > time.Second {
		h.Close()
		t.Fatalf("New blocked on failed capability-pending startup sweep for %s", elapsed)
	}
	h.Close()
}

type blockingCapabilityPendingSweepRepo struct {
	*store.SQLite
	entered chan struct{}
}

func (r *blockingCapabilityPendingSweepRepo) ExpireCapabilityPendingSessionDeliveries(ctx context.Context, _ time.Time) ([]model.SessionDelivery, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestFreshTargetStartupSweepTimeoutDoesNotBlockHubConstruction(t *testing.T) {
	base, err := store.OpenSQLite(filepath.Join(t.TempDir(), "fresh-startup-timeout.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if err := base.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo := &blockingCapabilityPendingSweepRepo{SQLite: base, entered: make(chan struct{}, 1)}
	oldTimeout := capabilityPendingStartupSweepTimeout
	capabilityPendingStartupSweepTimeout = 20 * time.Millisecond
	t.Cleanup(func() { capabilityPendingStartupSweepTimeout = oldTimeout })

	started := time.Now()
	h := New(repo, ConsoleAuthDisabled())
	elapsed := time.Since(started)
	defer h.Close()
	select {
	case <-repo.entered:
	default:
		t.Fatal("startup sweep did not enter repository")
	}
	if elapsed > time.Second {
		t.Fatalf("New blocked on capability-pending startup sweep for %s", elapsed)
	}
}

func TestFreshTargetCleanupIsNoopWithoutRepository(t *testing.T) {
	h := New(nil, ConsoleAuthDisabled())
	h.startCapabilityPendingCleanup()
	h.Close()
}
