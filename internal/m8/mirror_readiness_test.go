package m8

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Block the server AFTER the guide bytes reach the client but BEFORE Write
// returns. This is the real socket boundary, not an artificial database sleep.
type mirrorGuideBarrier struct {
	net.Conn
	reached, release chan struct{}
	once             sync.Once
}

func (c *mirrorGuideBarrier) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && bytes.Contains(p, []byte(agentNetworkSkillPushType)) {
		c.once.Do(func() { close(c.reached); <-c.release })
	}
	return n, err
}

type mirrorHijackWriter struct {
	http.ResponseWriter
	reached, release chan struct{}
}

func (w mirrorHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, e := w.ResponseWriter.(http.Hijacker).Hijack()
	if e != nil {
		return c, b, e
	}
	wrapped := &mirrorGuideBarrier{Conn: c, reached: w.reached, release: w.release}
	return wrapped, bufio.NewReadWriter(b.Reader, bufio.NewWriter(wrapped)), nil
}
func awaitSessionRuntime(t *testing.T, h *Hub, key, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !h.sessionOnline(key, name) {
		select {
		case <-ctx.Done():
			t.Fatal("runtime readiness did not arrive within unchanged 2s deadline")
		case <-tick.C:
		}
	}
}
func TestMirrorRegistrationRuntimeBarrier(t *testing.T) {
	h, db, ctx := newTestHub(t)
	if e := h.UpsertService(ctx, model.RegisteredService{ID: "mirror-barrier", Name: "mirror-barrier", DeliveryType: "ws", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	secret, key, e := h.IssueAPIKey(ctx, "mirror-barrier", "person")
	if e != nil {
		t.Fatal(e)
	}
	if e = h.BindAccount(ctx, key.ID, "dev:personal:alice"); e != nil {
		t.Fatal(e)
	}
	h.RegisterBot("dev", "UnifiedRobot")
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(mirrorHijackWriter{w, reached, release}, r)
	}))
	defer srv.Close()
	start := time.Now()
	// 本格刻意保持在发布前，不使用等待发布的正常建连夹具。
	c := dialSessionWithHeaderRaw(t, ctx, srv.URL, "home", key.ID, secret, nil)
	discardInitialAgentNetworkSkill(t, ctx, c)
	defer c.Close(websocket.StatusNormalClosure, "done")
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("guide barrier not reached")
	}
	ep, e := db.ListSessionEndpoints(ctx)
	if e != nil || len(ep) != 1 || !ep[0].Active || h.sessionOnline(key.ID, "home") {
		t.Fatal("expected persisted registration, received guide, runtime NOT ready")
	}
	t.Logf("trace +%s registration_persisted=true guide_received=true runtime_ready=false", time.Since(start))
	msg := model.Message{ID: "barrier-on", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}
	result, e := h.Dispatch(ctx, msg, "mirror on home")
	if e != nil || !result.Matched || !strings.Contains(result.Reply, "离线") {
		t.Fatalf("before-ready must disclose saved/offline: %+v err=%v", result, e)
	}
	ep, e = db.ListSessionEndpoints(ctx)
	if e != nil || !ep[0].MirrorEnabled {
		t.Fatal("pre-ready command was not persisted")
	}
	t.Logf("trace +%s before_ready command=on persisted=true reply=offline no_runtime_delivery", time.Since(start))
	unlock()
	awaitSessionRuntime(t, h, key.ID, "home")
	t.Logf("trace +%s runtime_ready=true", time.Since(start))
	for _, action := range []string{"on", "off"} {
		msg.ID = "ready-" + action
		result, e = h.Dispatch(ctx, msg, "mirror "+action+" home")
		if e != nil || strings.Contains(result.Reply, "离线") {
			t.Fatalf("ready command %+v err=%v", result, e)
		}
		env := readEnvelope(t, ctx, c)
		if env.Meta["type"] != "mirror_control" || env.Meta["enabled"] != (action == "on") {
			t.Fatalf("ready control %+v", env)
		}
		t.Logf("trace +%s after_ready command=%s socket_control_received=true", time.Since(start), action)
	}
}
