package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// Extra faults are test-only routes installed by the runner overlay.
// Both extra handlers deliberately ignore cancellation and remain blocked until
// process exit; forced network closure still reaches their clients. No product shutdown logic is replaced.
func hlcFullFaultSetup(h *m8.Hub, st *store.SQLite, public, internal *http.ServeMux) feishu.Receiver {
	receiver := hlcLoadSetup(h, st, public, internal)
	var publicActive, sseActive atomic.Int64
	baseFinal := hlcFinalFixture
	hlcFinalFixture = func() map[string]any {
		result := baseFinal()
		result["extra_public_http_active"] = publicActive.Load()
		result["extra_internal_sse_active"] = sseActive.Load()
		return result
	}
	public.HandleFunc("GET /hlc/full-stuck-http", func(w http.ResponseWriter, r *http.Request) {
		publicActive.Add(1)
		defer publicActive.Add(-1)
		<-make(chan struct{})
	})
	internal.Handle("GET /hlc/full-stuck-sse", h.TrackLongHandler("internal_sse", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseActive.Add(1)
		defer sseActive.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		// Fault work remains blocked even after the network is force closed.
		<-make(chan struct{})
	})))
	public.HandleFunc("GET /hlc/full-fault-status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"public_http": publicActive.Load(), "internal_sse": sseActive.Load()})
	})
	return receiver
}

func hlcS007FullStop(t *testing.T, c *hlcChild, out string) (shutdownReport, time.Time, time.Time) {
	t.Helper()
	tag := "S007-full"
	start := time.Now()
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var processErr error
	select {
	case processErr = <-c.done:
	case <-time.After(16 * time.Second):
		t.Fatal("HLC real process exceeded 15s plus 0.5s measurement tolerance")
	}
	ended := time.Now()
	raw := c.log.Bytes()
	os.WriteFile(filepath.Join(out, tag+"-process.log"), raw, 0644)
	report := shutdownReport{}
	var finalFixture map[string]any
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var row struct {
			Shutdown *shutdownReport `json:"shutdown"`
			Fixture  map[string]any  `json:"hlc_fixture_final"`
		}
		if json.Unmarshal(scan.Bytes(), &row) == nil {
			if row.Shutdown != nil {
				report = *row.Shutdown
			}
			if row.Fixture != nil {
				finalFixture = row.Fixture
			}
		}
	}
	actualExit := 0
	if processErr != nil {
		if e, ok := processErr.(*exec.ExitError); ok {
			actualExit = e.ExitCode()
		} else {
			t.Fatal(processErr)
		}
	}
	result := map[string]any{"signal_utc": start.UTC(), "exit_utc": ended.UTC(), "stop_ms": ended.Sub(start).Milliseconds(), "process_error": fmt.Sprint(processErr), "process_exit_code": actualExit, "report": report, "fixture": finalFixture}
	b, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile(filepath.Join(out, tag+"-shutdown.json"), b, 0644)
	t.Logf("%s stop_ms=%d process_error=%v report_exit=%d stages=%+v", tag, ended.Sub(start).Milliseconds(), processErr, report.ExitCode, report.Stages)
	if finalFixture == nil {
		t.Fatal("missing fixture DB call counts")
	}
	callback := finalFixture["callback"].(map[string]any)
	if callback["late_db_calls"] != float64(0) {
		t.Fatalf("S006_POST_SEAL_DB_ZERO: %v", callback)
	}
	expectedExit := 2
	if actualExit != expectedExit || report.ExitCode != expectedExit || len(report.Stages) != 5 {
		t.Fatalf("%s S007_FULL_EXIT_CODE: process=%v report=%+v", tag, processErr, report)
	}
	if ended.Sub(start) > 15*time.Second {
		t.Fatal("stop total budget exceeded")
	}

	snap := report.Snapshot.(map[string]any)
	servers := snap["servers"].(map[string]any)
	closed := []time.Time{}
	for _, name := range []string{"public", "internal"} {
		entry := servers[name].(map[string]any)
		when, err := time.Parse(time.RFC3339Nano, entry["listener_closed_utc"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if when.Sub(start) > time.Second {
			t.Fatalf("listener/server %s=%v", name, entry)
		}
		closed = append(closed, when)
	}
	diff := closed[0].Sub(closed[1])
	if diff < 0 {
		diff = -diff
	}
	if diff > 100*time.Millisecond {
		t.Fatalf("LISTENER_DELTA: %v", diff)
	}

	if report.ExitClass != "timeout" || report.ElapsedMS > 15000 || !report.Stages[1].Timeout || !report.Stages[1].ForcedClose {
		t.Fatalf("S007_FULL_TIMEOUT_CLASS: %+v", report)
	}
	drain := report.Stages[1].Snapshot.(map[string]any)
	remainingHub := drain["hub"].(map[string]any)
	remainingHTTP := drain["http_overlaps_streams"].(map[string]any)
	if remainingHub["internal_sse"] != float64(1) || remainingHTTP["public_http"] != float64(1) || remainingHTTP["internal_http"] != float64(1) {
		t.Fatalf("S007_FULL_REMAINING: %+v", drain)
	}
	for kind, n := range remainingHub {
		if kind != "internal_sse" && n != float64(0) {
			t.Fatalf("S007_FULL_NORMAL_HUB_NOT_DRAINED: %s=%v", kind, n)
		}
	}
	return report, start, ended
}

func TestHLCS007Full(t *testing.T) {
	if os.Getenv("HLC_S007_FULL_ENABLED") != "1" {
		t.Skip("isolated full mixed fault: tests/hlc/s007_full.py")
	}
	ctx := context.Background()
	root := t.TempDir()
	out := os.Getenv("HLC_MATRIX_EVIDENCE")
	if os.Getenv("HLC_REPEAT") != "" && os.Getenv("HLC_REPEAT") != "1" {
		out = filepath.Join(out, fmt.Sprintf("run-%d", time.Now().UnixNano()))
		t.Setenv("HLC_MATRIX_EVIDENCE", out)
	}
	os.MkdirAll(out, 0755)
	dsn := pgtest.DSN(t)
	hlcSeed(t, dsn)
	public, internal := hlcFreeAddress(t), hlcFreeAddress(t)
	pgURL, parseErr := url.Parse(dsn)
	if parseErr != nil {
		t.Fatal("invalid private PG fixture DSN")
	}
	resource := fmt.Sprintf("RESOURCE tmp=%s sqlite=%s public=%s internal=%s pg=%s database=%s", root, filepath.Join(root, "hub.db"), public, internal, pgURL.Host, pgURL.Path)
	t.Log(resource)
	os.WriteFile(filepath.Join(out, "RESOURCES.txt"), []byte(resource+"\n"), 0644)
	routing := hlcRoutingAndVS(t, root, internal)
	t.Logf("RESOURCE home=%s sqlite=%s public=%s internal=%s pg=private-pgtest notify=%s", root, filepath.Join(root, "hub.db"), public, internal, root)
	c := hlcStart(t, root, dsn, routing, public, internal)
	agents := make([]*hlcAgent, 20)
	seed := time.Now().UnixNano()
	random := rand.New(rand.NewSource(seed))
	t.Logf("fixture_random_seed=%d", seed)
	for _, i := range random.Perm(len(agents)) {
		agents[i] = hlcDialAgent(t, c, i)
		hlcPublish(t, agents[i], i, fmt.Sprintf("hlc-before-%02d", i))
	}
	for i := 0; i < 20; i++ {
		i := i
		hlcUntil(t, fmt.Sprintf("terminal source %d", i), func() bool {
			client := &http.Client{Timeout: time.Second}
			res, e := client.Get("http://" + internal + "/internal/view/" + hlcFull(i) + "/events")
			if e != nil {
				return false
			}
			res.Body.Close()
			return res.StatusCode == 200
		})
	}

	streams := []*hlcStream{}
	for i := 0; i < 100; i++ {
		streams = append(streams, hlcOpenSSE(t, "http://"+internal+"/internal/view/"+hlcFull(i%20)+"/events", ""))
	}
	muxes := []*hlcStream{}
	for owner := 0; owner < 5; owner++ {
		muxes = append(muxes, hlcOpenMux(t, c, owner, nil))
	}
	// Exactly 20 mux/bridge handlers: five public mux writers + fifteen signed VS bridges.
	hlcUntil(t, "100 SSE + 5 mux + 15 bridge + 20 session WS", func() bool {
		s := hlcReadStatus(t, c)
		h := s["hub"].(map[string]any)
		return h["internal_sse"] == float64(100) && h["public_mux"] == float64(5) && h["internal_bridge"] == float64(15) && h["session_ws"] == float64(20)
	})
	baseline := hlcReadStatus(t, c)
	b, _ := json.MarshalIndent(baseline, "", "  ")
	os.WriteFile(filepath.Join(out, os.Getenv("HLC_CASE")+"-load.json"), b, 0644)
	// Keep a real viewer WS for S011's third push channel, beyond the twenty helpers.
	viewCtx, viewCancel := context.WithTimeout(ctx, 3*time.Second)
	defer viewCancel()
	viewer, _, err := websocket.Dial(viewCtx, "ws://"+public+"/ws/view/"+hlcFull(0), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://" + public}, "Cookie": []string{httpapi.SessionCookie + "=" + hlcToken(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	viewDone := make(chan struct{})
	go func() {
		defer close(viewDone)
		for {
			if _, _, err := viewer.Read(ctx); err != nil {
				return
			}
		}
	}()
	defer viewer.CloseNow()
	pending := make(chan int, 101)
	for _, i := range random.Perm(100) {
		go func(i int) {
			addr := public
			if i >= 50 {
				addr = internal
			}
			res, err := http.Get(fmt.Sprintf("http://%s/hlc/slow/%03d", addr, i))
			if err != nil {
				pending <- 0
				return
			}
			res.Body.Close()
			pending <- res.StatusCode
		}(i)
	}
	go func() {
		res, err := http.Post("http://"+public+"/hlc/callback", "application/json", nil)
		if err != nil {
			pending <- 0
			return
		}
		res.Body.Close()
		pending <- res.StatusCode
	}()
	hlcUntil(t, "100 slow HTTP + inflight callback", func() bool {
		s := hlcReadStatus(t, c)
		return s["http_active"] == float64(100) && s["ingress"].(map[string]any)["active_callbacks"] == float64(1)
	})

	extraStream := hlcOpenSSE(t, "http://"+internal+"/hlc/full-stuck-sse", "")
	extraPending := make(chan int, 1)
	go func() {
		res, err := http.Get("http://" + public + "/hlc/full-stuck-http")
		if err != nil {
			extraPending <- 0
			return
		}
		res.Body.Close()
		extraPending <- res.StatusCode
	}()
	hlcUntil(t, "two additional uncooperative requests", func() bool {
		res, err := http.Get("http://" + public + "/hlc/full-fault-status")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		var state map[string]any
		if json.NewDecoder(res.Body).Decode(&state) != nil {
			return false
		}
		return state["public_http"] == float64(1) && state["internal_sse"] == float64(1)
	})
	initial := hlcReadStatus(t, c)
	hubInitial := initial["hub"].(map[string]any)
	if initial["http_active"] != float64(100) || initial["ingress"].(map[string]any)["active_callbacks"] != float64(1) || hubInitial["internal_sse"] != float64(101) || hubInitial["public_mux"] != float64(5) || hubInitial["internal_bridge"] != float64(15) || hubInitial["session_ws"] != float64(20) {
		t.Fatalf("S007_FULL_INITIAL_LOAD: %v", initial)
	}
	rawInitial, _ := json.MarshalIndent(initial, "", "  ")
	os.WriteFile(filepath.Join(out, "S007-full-initial.json"), rawInitial, 0644)
	report, stopped, exited := hlcS007FullStop(t, c, out)
	select {
	case status := <-extraPending:
		if status != 0 {
			t.Fatalf("S007_FULL_EXTRA_HTTP: got %d, want forced disconnect", status)
		}
	case <-time.After(time.Second):
		t.Fatal("S007_FULL_EXTRA_HTTP: not force disconnected")
	}

	for i := 0; i < 101; i++ {
		select {
		case status := <-pending:
			if status != 204 {
				t.Fatalf("finite work response=%d", status)
			}
		case <-time.After(time.Second):
			t.Fatal("finite work failed to return")
		}
	}
	for _, s := range append(append(streams, muxes...), extraStream) {
		select {
		case <-s.done:
		case <-time.After(time.Second):
			t.Fatal("stream not ended by shutdown")
		}
	}
	for _, a := range agents {
		select {
		case <-a.done:
		case <-time.After(time.Second):
			t.Fatal("helper WS did not close")
		}
	}
	select {
	case <-viewDone:
	case <-time.After(time.Second):
		t.Fatal("viewer WS not closed")
	}
	snapshot := report.Snapshot.(map[string]any)
	ingress := snapshot["feishu_ingress"].(map[string]any)
	if ingress["active_callbacks"] != float64(0) || ingress["rejected_after_seal"] != float64(1) {
		t.Fatalf("callback final counts=%v", ingress)
	}
	// Reopen the actual old SQLite only after waitpid. Verify all accepted HTTP commits survive.
	db, err := store.OpenSQLite(filepath.Join(root, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	configs, err := db.ListAppConfig(ctx)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, v := range configs {
		if strings.HasPrefix(v.Key, "hlc-slow-") {
			commits++
		}
	}
	if commits != 100 {
		t.Fatalf("durable HTTP commits=%d want100", commits)
	}

	final := map[string]any{"durable_http_commits": commits, "normal_http_responses": 100, "normal_callback_responses": 1, "normal_internal_sse_closed": 100, "normal_mux_closed": 5, "normal_bridge_closed": 15, "normal_helper_ws_closed": 20, "viewer_ws_closed": true, "late_db_calls": 0, "inflight_callbacks": ingress["active_callbacks"], "rejected_after_seal": ingress["rejected_after_seal"], "signal_utc": stopped.UTC(), "exit_utc": exited.UTC(), "stop_ms": exited.Sub(stopped).Milliseconds(), "stages": report.Stages}
	rawFinal, _ := json.MarshalIndent(final, "", "  ")
	os.WriteFile(filepath.Join(out, "S007-full-normal-completion.json"), rawFinal, 0644)
	t.Logf("S007_FULL_NORMAL_COMPLETION: durable_http=%d normal_callback=1 late_db=0 helper_ws=20 sse=100 mux=5 bridge=15", commits)
}
