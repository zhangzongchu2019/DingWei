package m8

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-0105 件一 Hub 侧：会话事件逐事件序号＋纪元；SSE 带 id 行；after／Last-Event-ID 只补缺口；
// 纪元变化或溢出先发 replay_gap 再整份重放；不带 after 时 data 行与现行逐字相同。

type sseFrame struct {
	ID, Event, Data string
}

func seqTestHub(t *testing.T) (*Hub, *httptest.Server, *sessionClient) {
	t.Helper()
	hub, _, _ := newTestHub(t)
	c := &sessionClient{keyID: "key", sessionName: "s1", connSeq: 1}
	hub.mu.Lock()
	hub.sessionClients["key"] = map[string]*sessionClient{"s1": c}
	hub.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/view/{session}/events", hub.HandleInternalViewEvents)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, srv, c
}

func publishN(hub *Hub, c *sessionClient, from, to int) {
	for i := from; i <= to; i++ {
		hub.handleSessionEventEnvelope(c, model.Envelope{Meta: map[string]any{"type": terminalSessionEventType},
			Body: fmt.Sprintf(`{"kind":"assistant_text","text":"t%d","delivery_id":"d","turn_id":"x"}`, i)})
	}
}

// readSSE 读到 want 个带 data 的帧为止（或超时）。
func readSSE(t *testing.T, url string, header http.Header, want int) []sseFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var frames []sseFrame
	cur := sseFrame{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.Data != "" {
				frames = append(frames, cur)
				if len(frames) >= want {
					return frames
				}
			}
			cur = sseFrame{}
		case strings.HasPrefix(line, "id: "):
			cur.ID = line[4:]
		case strings.HasPrefix(line, "event: "):
			cur.Event = line[7:]
		case strings.HasPrefix(line, "data: "):
			cur.Data = line[6:]
		}
	}
	return frames
}

func epochOf(t *testing.T, id string) string {
	t.Helper()
	e, _, ok := parseSSEAfter(id)
	if !ok {
		t.Fatalf("bad id %q", id)
	}
	return e
}

func TestSessionEventSSECarriesMonotonicIDsAndUnchangedData(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, 3)
	frames := readSSE(t, srv.URL+"/internal/view/s1/events", nil, 3)
	if len(frames) != 3 {
		t.Fatalf("frames=%+v", frames)
	}
	epoch := epochOf(t, frames[0].ID)
	for i, f := range frames {
		want := fmt.Sprintf(`{"kind":"assistant_text","text":"t%d","delivery_id":"d","turn_id":"x"}`, i+1)
		if f.Data != want || f.ID != fmt.Sprintf("%s-%d", epoch, i+1) || f.Event != "" {
			t.Fatalf("frame %d = %+v (data must be byte-identical to the old format; id epoch-seq)", i, f)
		}
	}
}

func TestSSEAfterSameEpochReplaysOnlyNewer(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, 3)
	epoch := epochOf(t, readSSE(t, srv.URL+"/internal/view/s1/events", nil, 1)[0].ID)
	frames := readSSE(t, srv.URL+"/internal/view/s1/events?after="+epoch+"-2", nil, 1)
	if len(frames) != 1 || frames[0].ID != epoch+"-3" || frames[0].Event != "" {
		t.Fatalf("after=%s-2 frames=%+v", epoch, frames)
	}
	// Last-Event-ID 头作后备（query 优先）
	frames = readSSE(t, srv.URL+"/internal/view/s1/events", http.Header{"Last-Event-Id": {epoch + "-2"}}, 1)
	if len(frames) != 1 || frames[0].ID != epoch+"-3" {
		t.Fatalf("Last-Event-ID frames=%+v", frames)
	}
}

func TestSSEAfterOtherEpochSendsGapThenFullReplay(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, 2)
	frames := readSSE(t, srv.URL+"/internal/view/s1/events?after=deadbeef-7", nil, 3)
	if len(frames) != 3 || frames[0].Event != "replay_gap" || !strings.Contains(frames[0].Data, `"epoch_changed"`) {
		t.Fatalf("frames=%+v", frames)
	}
	if !strings.HasSuffix(frames[1].ID, "-1") || !strings.HasSuffix(frames[2].ID, "-2") {
		t.Fatalf("full replay expected after gap: %+v", frames)
	}
}

func TestSSEAfterOlderThanRingSendsOverflowGap(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, terminalEventRingCap+10)
	fresh := readSSE(t, srv.URL+"/internal/view/s1/events", nil, 2)
	if fresh[0].Event != "replay_gap" || !strings.Contains(fresh[0].Data, `"overflow"`) {
		t.Fatal("fresh truncated history must be visible", fresh[0])
	}
	epoch := epochOf(t, fresh[1].ID)
	frames := readSSE(t, srv.URL+"/internal/view/s1/events?after="+epoch+"-1", nil, 2)
	if len(frames) != 2 || frames[0].Event != "replay_gap" || !strings.Contains(frames[0].Data, `"overflow"`) {
		t.Fatalf("frames=%+v", frames)
	}
	if frames[1].ID != fmt.Sprintf("%s-%d", epoch, 11) {
		t.Fatalf("ring oldest should be seq 11, got %+v", frames[1])
	}
}

func TestSSEInvalidAfterIsIgnored(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, 2)
	for _, bad := range []string{"x", "ABC-1", "abc-", "abc-1-2", strings.Repeat("a", 40) + "-1"} {
		frames := readSSE(t, srv.URL+"/internal/view/s1/events?after="+bad, nil, 2)
		if len(frames) != 2 || frames[0].Event != "" || !strings.HasSuffix(frames[0].ID, "-1") {
			t.Fatalf("after=%q frames=%+v", bad, frames)
		}
	}
}

func TestTerminalRebuildChangesEpochAndRestartsSeq(t *testing.T) {
	hub, srv, c := seqTestHub(t)
	publishN(hub, c, 1, 2)
	oldEpoch := epochOf(t, readSSE(t, srv.URL+"/internal/view/s1/events", nil, 1)[0].ID)
	hub.mu.Lock()
	hub.closeTerminalLocked("key", "s1") // helper 断线：状态被删，下次新建（hub.go 会话收尾路径）
	hub.mu.Unlock()
	publishN(hub, c, 3, 3)
	frames := readSSE(t, srv.URL+"/internal/view/s1/events?after="+oldEpoch+"-2", nil, 2)
	if len(frames) != 2 || frames[0].Event != "replay_gap" || !strings.Contains(frames[0].Data, `"epoch_changed"`) {
		t.Fatalf("rebuilt terminal must announce an epoch change: %+v", frames)
	}
	newEpoch := epochOf(t, frames[1].ID)
	if newEpoch == oldEpoch || frames[1].ID != newEpoch+"-1" || !strings.Contains(frames[1].Data, `"t3"`) {
		t.Fatalf("new event after rebuild must carry a new epoch and seq 1, got %+v (old epoch %s)", frames[1], oldEpoch)
	}
}
