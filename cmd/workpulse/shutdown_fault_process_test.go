package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// These routes exist only when the isolated overlay calls this setup. The
// production executable has neither these routes nor an environment bypass.
func hlcFaultSetup(h *m8.Hub, st *store.SQLite, public, internal *http.ServeMux, kind string) feishu.Receiver {
	var active, committed atomic.Int64
	release := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		delay := 2 * time.Second
		if kind == "S013" {
			delay = 8300 * time.Millisecond
		}
		time.Sleep(delay)
		close(release)
		signal.Stop(signals)
	}()
	slow := func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		if kind == "S003" {
			<-r.Context().Done()
			return
		}
		<-release
		if err := st.UpsertAppConfig(r.Context(), "hlc-fault-"+r.PathValue("id"), `{"committed":true}`); err != nil {
			http.Error(w, "transaction failed", 500)
			return
		}
		committed.Add(1)
		w.WriteHeader(204)
	}
	public.HandleFunc("GET /hlc/slow/{id}", slow)
	internal.HandleFunc("GET /hlc/slow/{id}", slow)
	stuck := func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			if _, err := io.WriteString(w, ": fixture-heartbeat\n\n"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}
	public.HandleFunc("GET /hlc/stuck", stuck)
	internal.Handle("GET /hlc/stuck", h.TrackLongHandler("internal_sse", http.HandlerFunc(stuck)))
	public.HandleFunc("GET /hlc/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"active": active.Load(), "committed": committed.Load(), "hub": h.LifecycleCounts()})
	})
	return nil
}

type hlcWatchObservation struct {
	At    time.Time `json:"at_utc"`
	Value string    `json:"value"`
}

// An independent supervisor keeps checking throughout deactivation. It does not
// claim to reproduce systemd259, which disables watchdog checks while stopping.
func TestHLCFaultProcessMatrix(t *testing.T) {
	if os.Getenv("HLC_FAULT_ENABLED") != "1" {
		t.Skip("isolated fault/watchdog runner tests/hlc/fault_matrix.py")
	}
	kind := os.Getenv("HLC_FAULT_CASE")
	out := os.Getenv("HLC_MATRIX_EVIDENCE")
	root := t.TempDir()
	os.MkdirAll(out, 0755)
	pub, intl := hlcFreeAddress(t), hlcFreeAddress(t)
	sock := filepath.Join(root, "notify.sock")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer notify.Close()
	log := &hlcLockedBuffer{}
	cmd := exec.Command("bash", "-c", `ulimit -c 0; exec "$@"`, "hlc-isolated", os.Args[0], "-test.run=^TestHLCLoadProcessChild$", "-test.count=1")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TZ=UTC", "HLC_LOAD_CHILD=1", "NOTIFY_SOCKET=" + sock, "WP_DATA_DIR=" + root, "WP_DB_PATH=" + filepath.Join(root, "hub.db"), "WP_ADDR=" + pub, "WP_INTERNAL_ADDR=" + intl, "FEISHU_TRANSPORT=fake", "WP_CONSOLE_AUTH_ENABLED=false", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(root, "no-team"), "WP_SCHEDULE_PERSONAL_DIR=" + root, "GOTRACEBACK=crash"}
	if kind == "S013" {
		cmd.Env = append(cmd.Env, "WATCHDOG_USEC=12000000")
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		os.WriteFile(filepath.Join(out, kind+"-process.log"), log.Bytes(), 0644)
	})
	notify.SetReadDeadline(time.Now().Add(10 * time.Second))
	buffer := make([]byte, 1024)
	for {
		n, _, err := notify.ReadFromUnix(buffer)
		if err != nil {
			t.Fatalf("ready failed: %v log=%s", err, log.Bytes())
		}
		if strings.Contains(string(buffer[:n]), "READY=1") {
			break
		}
	}
	var mu sync.Mutex
	observations := []hlcWatchObservation{}
	lastWatch := time.Now()
	aborted := false
	stopping := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			notify.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := notify.ReadFromUnix(buffer)
			if err == nil && strings.Contains(string(buffer[:n]), "WATCHDOG=1") {
				mu.Lock()
				lastWatch = time.Now()
				observations = append(observations, hlcWatchObservation{lastWatch.UTC(), "WATCHDOG=1"})
				mu.Unlock()
			}
			select {
			case <-stopping:
				return
			default:
			}
			mu.Lock()
			expired := kind == "S013" && time.Since(lastWatch) > 12*time.Second && !aborted
			if expired {
				aborted = true
			}
			mu.Unlock()
			if expired {
				_ = cmd.Process.Signal(syscall.SIGABRT)
			}
		}
	}()
	responses := make(chan int, 2)
	if kind == "S002" || kind == "S003" || kind == "S013" || kind == "S010" {
		for i, addr := range []string{pub, intl} {
			go func(i int, addr string) {
				res, err := http.Get(fmt.Sprintf("http://%s/hlc/slow/%d", addr, i))
				if err != nil {
					responses <- 0
					return
				}
				res.Body.Close()
				responses <- res.StatusCode
			}(i, addr)
		}
	}
	if kind == "S007" {
		for _, addr := range []string{pub, intl} {
			res, err := http.Get("http://" + addr + "/hlc/stuck")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { res.Body.Close() })
		}
	}
	if kind != "S012" {
		hlcUntil(t, "two finite or held requests", func() bool {
			res, err := http.Get("http://" + pub + "/hlc/status")
			if err != nil {
				return false
			}
			defer res.Body.Close()
			var s map[string]any
			json.NewDecoder(res.Body).Decode(&s)
			return s["active"] == float64(2)
		})
	}
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if kind == "S010" {
		go func() { time.Sleep(100 * time.Millisecond); _ = cmd.Process.Signal(syscall.SIGTERM) }()
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var processErr error
	select {
	case processErr = <-waited:
	case <-time.After(16 * time.Second):
		t.Fatal("FAULT_PROCESS_TOTAL_BOUND")
	}
	ended := time.Now()
	close(stopping)
	<-readerDone
	report := shutdownReport{}
	scan := bufio.NewScanner(bytes.NewReader(log.Bytes()))
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var row struct {
			Shutdown *shutdownReport `json:"shutdown"`
		}
		if json.Unmarshal(scan.Bytes(), &row) == nil && row.Shutdown != nil {
			report = *row.Shutdown
		}
	}
	code := cmd.ProcessState.ExitCode()
	signalName := ""
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		signalName = ws.Signal().String()
	}
	mu.Lock()
	trace := append([]hlcWatchObservation(nil), observations...)
	wasAborted := aborted
	mu.Unlock()
	maxGap := time.Duration(0)
	previous := start
	for _, o := range trace {
		if o.At.After(start) {
			if gap := o.At.Sub(previous); gap > maxGap {
				maxGap = gap
			}
			previous = o.At
		}
	}
	if gap := ended.Sub(previous); gap > maxGap {
		maxGap = gap
	}
	result := map[string]any{"case": kind, "signal_utc": start.UTC(), "exit_utc": ended.UTC(), "elapsed_ms": ended.Sub(start).Milliseconds(), "process_exit_code": code, "process_error": fmt.Sprint(processErr), "signal": signalName, "report": report, "watchdog_trace": trace, "watchdog_max_gap_ms": maxGap.Milliseconds(), "supervisor_sent_sigabrt": wasAborted, "ports": []string{pub, intl}, "temporary_home": root}
	b, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile(filepath.Join(out, kind+"-result.json"), b, 0644)
	t.Logf("%s process_exit=%d elapsed_ms=%d stages=%+v supervisor_abort=%t", kind, code, ended.Sub(start).Milliseconds(), report.Stages, wasAborted)
	if os.Getenv("HLC_WATCHDOG_MUTANT") == "1" {
		if !wasAborted || signalName != "aborted" {
			t.Fatal("S013_WITHHOLD_MUST_SIGABRT")
		}
		t.Fatal("S013_CONTINUOUS_WATCHDOG: mutant interrupted by real supervisor SIGABRT")
	}
	want := 0
	if kind == "S003" || kind == "S007" {
		want = 2
	}
	if kind == "S007" {
		snap := report.Stages[1].Snapshot.(map[string]any)
		hub := snap["hub"].(map[string]any)
		httpCounts := snap["http_overlaps_streams"].(map[string]any)
		if hub["internal_sse"] != float64(1) || httpCounts["public_http"] != float64(1) || httpCounts["internal_http"] != float64(1) {
			t.Fatalf("S007_REMAINING_CATEGORIES: %v", snap)
		}
	}
	if kind == "S012" {
		want = 1
	}
	if code != want || report.ExitCode != want || len(report.Stages) != 5 || ended.Sub(start) > 15500*time.Millisecond {
		t.Fatal("FAULT_PROCESS_EXIT_OR_BUDGET")
	}
	if want == 2 {
		if !report.Stages[1].Timeout || !report.Stages[1].ForcedClose {
			t.Fatal("FAULT_DRAIN_TIMEOUT_NOT_RECORDED")
		}
	}
	if kind == "S012" {
		snapshot := report.Snapshot.(map[string]any)
		servers := snapshot["servers"].(map[string]any)
		pubResult, pok := servers["public"].(map[string]any)
		intResult, iok := servers["internal"].(map[string]any)
		if !pok || !iok || pubResult["error"] != "nil" || intResult["error"] != "hlc injected internal close failure" {
			t.Fatalf("S012_BOTH_SERVER_RESULTS: %v", servers)
		}
	}
	if kind == "S002" {
		for i := 0; i < 2; i++ {
			if status := <-responses; status != 204 {
				t.Fatal("S002_TRANSACTION_INCOMPLETE")
			}
		}
		db, err := store.OpenSQLite(filepath.Join(root, "hub.db"))
		if err != nil {
			t.Fatal(err)
		}
		configs, err := db.ListAppConfig(context.Background())
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		committed := 0
		for _, c := range configs {
			if strings.HasPrefix(c.Key, "hlc-fault-") {
				committed++
			}
		}
		if committed != 2 {
			t.Fatalf("S002_DURABLE_COMMITS: got%d", committed)
		}
	}
	if kind == "S013" {
		if wasAborted || ended.Sub(start) < 13*time.Second || maxGap > 4*time.Second || len(trace) < 4 {
			t.Fatal("S013_CONTINUOUS_WATCHDOG")
		}
	}
}

func TestHLCBindFailure(t *testing.T) {
	if os.Getenv("HLC_FAULT_ENABLED") != "1" {
		t.Skip("isolated startup failure runner")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHLCProcessChild$", "-test.count=1")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TZ=UTC", "HLC_PROCESS_CHILD=1", "WP_DATA_DIR=" + root, "WP_DB_PATH=" + filepath.Join(root, "hub.db"), "WP_ADDR=" + ln.Addr().String(), "WP_INTERNAL_ADDR=" + hlcFreeAddress(t), "FEISHU_TRANSPORT=fake", "WP_CONSOLE_AUTH_ENABLED=false", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(root, "none"), "WP_SCHEDULE_PERSONAL_DIR=" + root}
	started := time.Now()
	raw, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	out := os.Getenv("HLC_MATRIX_EVIDENCE")
	os.MkdirAll(out, 0755)
	os.WriteFile(filepath.Join(out, "S010-bind-process.log"), raw, 0644)
	b, _ := json.MarshalIndent(map[string]any{"exit_code": cmd.ProcessState.ExitCode(), "elapsed_ms": elapsed.Milliseconds(), "error": fmt.Sprint(err)}, "", "  ")
	os.WriteFile(filepath.Join(out, "S010-bind-result.json"), b, 0644)
	if err == nil || cmd.ProcessState.ExitCode() != 1 || elapsed > 2*time.Second {
		t.Fatalf("S010_BIND_FAILURE: err=%v elapsed=%v", err, elapsed)
	}
	t.Logf("S010 bind_failure actual_exit=1 elapsed_ms=%d", elapsed.Milliseconds())
}
