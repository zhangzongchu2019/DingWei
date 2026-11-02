package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Run explicitly in its own test process: the pinned SDK intentionally retains
// process-owned goroutines. It must not contaminate other goroutine assertions.
func TestReceiverPinnedSDKProcess(t *testing.T) {
	if os.Getenv("HLC_PINNED_SDK_PROCESS") != "1" {
		t.Skip("process-reaped SDK fixture: run alone with HLC_PINNED_SDK_PROCESS=1")
	}
	peerClosed := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			defer close(peerClosed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"URL": "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?device_id=synthetic&service_id=1",
		}})
	}))
	defer server.Close()
	g := &LarkGateway{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	g.wsFactory = func(d *dispatcher.EventDispatcher, ready func()) sdkConnection {
		return larkws.NewClient("synthetic-app", "synthetic-not-a-credential",
			larkws.WithDomain(server.URL), larkws.WithLogLevel(larkcore.LogLevelError),
			larkws.WithEventHandler(d), larkws.WithOnReady(ready), larkws.WithOnReconnected(ready))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- g.Start(ctx, func(context.Context, model.Message) error { return nil }) }()
	readyDeadline := time.Now().Add(3 * time.Second)
	for {
		g.receiver.mu.Lock()
		ready := g.receiver.ready
		g.receiver.mu.Unlock()
		if ready != nil {
			select {
			case <-ready:
				goto connected
			default:
			}
		}
		if time.Now().After(readyDeadline) {
			t.Fatal("pinned SDK did not connect to loopback fixture")
		}
		time.Sleep(time.Millisecond)
	}
connected:
	cancel()
	<-result
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := g.StopIngress(stop); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peerClosed:
	case <-stop.Done():
		t.Fatal("SDK Close did not close actual WebSocket")
	}
	stacks := make([]byte, 1<<20)
	stacks = stacks[:runtime.Stack(stacks, true)]
	// Count an actual stack frame, not a ping-loop's "created by ...Start"
	// attribution line, which refers to the same function a second time.
	frames := bytes.Count(stacks, []byte("\ngithub.com/larksuite/oapi-sdk-go/v3/ws.(*Client).Start("))
	s := g.IngressSnapshot()
	if frames != 1 || s.SDKStartNotJoined != 1 || s.Active != 0 || !s.CloseCompleted {
		t.Fatalf("SDK_PROCESS_BOUNDARY: snapshot=%+v actual_sdk_start_frames=%d", s, frames)
	}
	t.Logf("pinned_sdk=v3.9.7 actual_websocket_closed=true active_callbacks=%d sdk_start_frames_excluded=%d; process reaps SDK residuals, not all SDK goroutines joined", s.Active, frames)
}
