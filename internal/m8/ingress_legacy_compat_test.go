package m8

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const legacyIngressSource = "2e1e4c9db3c42cf7c987a548fa9214aad67a9f0c"

// Test-only export of immutable Git objects, not a bundled legacy source fork.
func legacyIngressPackage(t *testing.T) string {
	t.Helper()
	root, _ := filepath.Abs("../..")
	cmd := exec.Command("git", "-C", root, "archive", legacyIngressSource, "tools/sessionhelper/sessionhelper")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal("pinned legacy Git object unavailable; fetch the exact approved source before this gate")
	}
	out := t.TempDir()
	r := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		clean := filepath.Clean(h.Name)
		if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
			t.Fatal("unsafe fixture archive path")
		}
		p := filepath.Join(out, clean)
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeXHeader {
			continue
		}
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			t.Fatal("non-regular legacy package member")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(out, "tools/sessionhelper")
}

type legacyIngressChild struct {
	input  io.WriteCloser
	lines  chan string
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func startLegacyIngress(t *testing.T, e *agentEnv, secret string) *legacyIngressChild {
	t.Helper()
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set explicit isolated integration interpreter for real legacy helper")
	}
	root, _ := filepath.Abs("../..")
	home := t.TempDir()
	ipcdir, err := os.MkdirTemp("/tmp", "rif-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 90*time.Second)
	cmd := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "internal/m8/testdata/legacy_ingress_helper.py"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "PYTHONPATH=" + legacyIngressPackage(t), "PYTHONDONTWRITEBYTECODE=1"}
	stderr := newBoundedStderr(childStderrLimit)
	cmd.Stderr = stderr
	logChildStderrOnFailure(t, "legacy ingress helper", stderr)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &legacyIngressChild{in, make(chan string, 64), cmd, cancel}
	go func() {
		defer close(c.lines)
		scan := bufio.NewScanner(out)
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "RIF_") {
				c.lines <- scan.Text()
			}
		}
	}()
	t.Cleanup(func() {
		in.Close()
		cancel()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		os.RemoveAll(ipcdir)
	})
	if err := json.NewEncoder(in).Encode(map[string]any{"home": home, "secret": secret, "base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "ipc": filepath.Join(ipcdir, "ipc.sock")}); err != nil {
		t.Fatal(err)
	}
	c.want(t, "RIF_READY39", 10*time.Second)
	return c
}
func (c *legacyIngressChild) send(t *testing.T, v any) {
	t.Helper()
	if err := json.NewEncoder(c.input).Encode(v); err != nil {
		t.Fatal(err)
	}
}
func (c *legacyIngressChild) want(t *testing.T, want string, limit time.Duration) {
	t.Helper()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case line, ok := <-c.lines:
		if !ok || line != want {
			t.Fatalf("legacy phase got %q want %q", line, want)
		}
	case <-timer.C:
		t.Fatal("legacy helper phase timeout", want)
	}
}
func (c *legacyIngressChild) stop(t *testing.T) {
	c.send(t, map[string]any{"op": "stop"})
	c.want(t, "RIF_STOPPED", 5*time.Second)
	if err := c.cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

type ingressOrderRepo struct {
	store.Repository
	history   *store.SessionHistory
	agent     string
	owner     string
	entered   chan struct{}
	release   chan struct{}
	mu        sync.Mutex
	violation string
	bodySeen  bool
}

func (r *ingressOrderRepo) check(ctx context.Context, expected int64) {
	p, err := r.history.Page(ctx, r.owner, r.agent, "legacy", "", nil)
	if err != nil || p.HighWater != expected {
		r.mu.Lock()
		r.violation = fmt.Sprintf("FIFO barrier history=%d want=%d err=%v", p.HighWater, expected, err)
		r.mu.Unlock()
	}
}
func (r *ingressOrderRepo) ApplySessionDeliveryACK(ctx context.Context, a model.SessionDeliveryACK, now time.Time) (*model.SessionDelivery, bool, error) {
	if a.DeliveryID == "rif-ack-wire" {
		r.check(ctx, 500)
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return r.Repository.ApplySessionDeliveryACK(ctx, a, now)
}
func (r *ingressOrderRepo) GetControlTask(ctx context.Context, id string) (*model.ControlTask, error) {
	if id == "rif-order-barrier" {
		r.check(ctx, 500)
		d, err := r.Repository.GetSessionDelivery(ctx, "rif-ack-wire")
		r.mu.Lock()
		r.bodySeen = true
		if err != nil || d == nil || d.DeliveredAt == nil {
			r.violation = "body overtook durable ACK commit"
		}
		r.mu.Unlock()
		return nil, nil
	}
	return r.Repository.GetControlTask(ctx, id)
}

func TestLegacyIngressBurstKeepsFIFOAndCommittedACK(t *testing.T) {
	e := newAgentEnv(t)
	secret, agent := e.enroll("rif-legacy", "Legacy")
	history, err := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { history.Close() })
	if err := history.SetEnabled(e.ctx, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.hub.SessionHistory = history
	e.hub.SessionHistoryEnabled = true
	repo := &ingressOrderRepo{Repository: e.hub.Repo, history: history, agent: agent, entered: make(chan struct{}), release: make(chan struct{})}
	e.hub.Repo = repo
	logs := &sessionDiagRecorder{changes: make(chan struct{}, 1)}
	e.hub.sessionJournal = logs.write
	child := startLegacyIngress(t, e, secret)
	e.hub.mu.Lock()
	actualClient := e.hub.sessionClients[AgentKeyID("zzc")]["legacy"]
	e.hub.mu.Unlock()
	if actualClient == nil {
		t.Fatal("legacy client missing")
	}
	repo.owner = actualClient.ownerKey
	// A pre-existing sent ledger row, not a new delivery to the synthetic adapter.
	wire, _ := json.Marshal(model.Envelope{ID: "rif-original", Body: "original ledger body", Meta: map[string]any{"type": "delivery", "delivery_id": "rif-ack-wire"}})
	if err := repo.EnqueueSessionDelivery(e.ctx, model.SessionDelivery{ID: "rif-ack-wire", KeyID: AgentKeyID("zzc"), SessionName: "legacy", SourceMessageID: "rif-original", Envelope: string(wire), State: "sent", NextAttemptAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSessionDelivery(e.ctx, "rif-ack-wire", "sent", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err := repo.BindSessionDeliveryACK(e.ctx, "rif-ack-wire", AgentKeyID("zzc"), "legacy", 2, "driver")
	if err != nil || d == nil {
		t.Fatal("missing private ACK ledger")
	}
	released := false
	defer func() {
		if !released {
			close(repo.release)
		}
	}()
	child.send(t, map[string]any{"op": "burst", "ack": map[string]any{"ack_protocol": 2, "processing_mode": "driver", "ack_generation": d.AckGeneration, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano)}})
	child.wantHealthy(t, "RIF_ACK_SENT", 15*time.Second, logs)
	select {
	case <-repo.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("legacy ACK did not reach commit barrier")
	}
	repo.mu.Lock()
	violation := repo.violation
	repo.mu.Unlock()
	if violation != "" {
		t.Fatal(violation)
	}
	select {
	case line := <-child.lines:
		t.Fatal("ACK confirmed before commit", line)
	case <-time.After(120 * time.Millisecond):
	}
	before, _ := repo.GetSessionDelivery(e.ctx, "rif-ack-wire")
	if before.DeliveredAt != nil {
		t.Fatal("blocked ACK appeared committed")
	}
	close(repo.release)
	released = true
	child.want(t, "RIF_ACK_CONFIRM", 5*time.Second)
	waitFor(t, 25*time.Second, "all 1000 legacy events durably consumed", func() bool {
		p, err := history.Page(e.ctx, repo.owner, agent, "legacy", "", nil)
		return err == nil && p.HighWater == 1000
	})
	repo.mu.Lock()
	violation = repo.violation
	bodySeen := repo.bodySeen
	repo.mu.Unlock()
	if violation != "" || !bodySeen {
		t.Fatalf("body/ACK order violation=%q seen=%v", violation, bodySeen)
	}
	// Read all pages, not the bounded in-memory viewer ring; assert IDs and every body.
	seen := map[string]bool{}
	var beforeSeq *int64
	want := 1000
	for {
		page, err := history.Page(e.ctx, repo.owner, agent, "legacy", "", beforeSeq)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Gaps) != 0 || page.GapBefore {
			t.Fatal("legacy burst lost canonical history")
		}
		for i := len(page.Events) - 1; i >= 0; i-- {
			ev := page.Events[i]
			var p struct {
				Sequence int    `json:"sequence"`
				Text     string `json:"text"`
			}
			if json.Unmarshal(ev.Event, &p) != nil || p.Sequence != want || p.Text != strings.Repeat("x", 4096) || seen[ev.EventID] {
				t.Fatalf("canonical event missing/reordered/corrupt at %d", want)
			}
			seen[ev.EventID] = true
			want--
		}
		if !page.HasOlder {
			break
		}
		beforeSeq = page.BeforeSeq
	}
	actualClient.ingress.mu.Lock()
	peakItems, peakBytes := actualClient.ingress.peakItems, actualClient.ingress.peakBytes
	actualClient.ingress.mu.Unlock()
	t.Logf("canonical_history=1000 unique_ids=%d preserved_text4096 ordered_with_ACK_and_body peak_items=%d peak_bytes=%d", len(seen), peakItems, peakBytes)
	if want != 0 || len(seen) != 1000 {
		t.Fatalf("canonical count=%d missing=%d", len(seen), want)
	}
	child.send(t, map[string]any{"op": "state"})
	child.want(t, `RIF_STATE {"alive": true}`, time.Second)
	if logs.has("session_ingress_overflow") {
		t.Fatal("compatible legacy burst overflowed")
	}
	child.stop(t)
}
func TestLegacyIngressReadPauseResumesWithoutDisconnect(t *testing.T) {
	e := newAgentEnv(t)
	secret, _ := e.enroll("rif-pause", "Legacy")
	logs := &sessionDiagRecorder{changes: make(chan struct{}, 1)}
	e.hub.sessionJournal = logs.write
	child := startLegacyIngress(t, e, secret)
	e.hub.mu.Lock()
	c := e.hub.sessionClients[AgentKeyID("zzc")]["legacy"]
	e.hub.mu.Unlock()
	if c == nil {
		t.Fatal("legacy connection absent")
	}
	child.send(t, map[string]any{"op": "pause"})
	child.want(t, "RIF_PAUSED", time.Second)
	body := strings.Repeat("W", 8<<20)
	started := time.Now()
	err := c.write(e.ctx, model.Envelope{ID: "rif-large", From: sessionAddress("workpulse", c.keyID), Body: body, Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true}})
	if err != nil {
		t.Fatalf("3s legacy read pause killed large write: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 2800*time.Millisecond || elapsed > 10*time.Second {
		t.Fatalf("pause not actually exercised: %v", elapsed)
	}
	digest := sha256.Sum256([]byte(body))
	child.want(t, "RIF_LARGE "+hex.EncodeToString(digest[:]), 5*time.Second)
	if err := c.write(e.ctx, model.Envelope{ID: "rif-control", Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true}}); err != nil {
		t.Fatal(err)
	}
	child.want(t, "RIF_SMALL", time.Second)
	child.send(t, map[string]any{"op": "state"})
	child.want(t, `RIF_STATE {"alive": true}`, time.Second)
	if logs.has("session_physical_write_failed") {
		t.Fatal("recovered pause left physical failure")
	}
	t.Logf("actual legacy pause/resume %s body_sha256=%x", elapsed, digest)
	child.stop(t)
}

// A small control message may fit the kernel buffer while the peer is paused;
// receipt after resume, rather than a delayed write, proves this separate case.
func TestLegacyIngressSmallControlReadPause(t *testing.T) {
	e := newAgentEnv(t)
	secret, _ := e.enroll("rif-small-pause", "Legacy")
	logs := &sessionDiagRecorder{}
	e.hub.sessionJournal = logs.write
	child := startLegacyIngress(t, e, secret)
	e.hub.mu.Lock()
	c := e.hub.sessionClients[AgentKeyID("zzc")]["legacy"]
	e.hub.mu.Unlock()
	if c == nil {
		t.Fatal("legacy connection absent")
	}
	child.send(t, map[string]any{"op": "pause"})
	child.want(t, "RIF_PAUSED", time.Second)
	started := time.Now()
	if err := c.write(e.ctx, model.Envelope{ID: "rif-small-paused", Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true}}); err != nil {
		t.Fatal("small control pause disconnected helper", err)
	}
	child.want(t, "RIF_SMALL", 5*time.Second)
	elapsed := time.Since(started)
	if elapsed < 2800*time.Millisecond || elapsed > 5*time.Second {
		t.Fatal("small control receipt did not exercise 3s pause", elapsed)
	}
	child.send(t, map[string]any{"op": "state"})
	child.want(t, `RIF_STATE {"alive": true}`, time.Second)
	if logs.has("session_physical_write_failed") {
		t.Fatal("small control pause caused physical write failure")
	}
	t.Logf("small control receipt after actual read pause %s", elapsed)
	child.stop(t)
}

func (c *legacyIngressChild) wantHealthy(t *testing.T, want string, limit time.Duration, logs *sessionDiagRecorder) {
	t.Helper()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-c.lines:
			if !ok || line != want {
				t.Fatalf("legacy wire phase got %q want %q", line, want)
			}
			return
		case <-logs.changes:
			if logs.has("session_ingress_overflow") {
				t.Fatal("1000-event legacy compatibility window overflowed")
			}
			if logs.has("session_end") {
				t.Fatal("legacy compatibility connection ended before completion")
			}
		case <-timer.C:
			t.Fatal("legacy helper phase timeout", want)
		}
	}
}

// Throttle actual server TCP writes, not a sleeping business-handler mock.
// The same real legacy protocol parser must receive Ping between data frames.
type ingressRateListener struct {
	net.Listener
	enabled *atomic.Bool
}

func (l *ingressRateListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &ingressRateConn{Conn: c, enabled: l.enabled}, nil
}

type ingressRateConn struct {
	net.Conn
	enabled *atomic.Bool
}

func (c *ingressRateConn) Write(p []byte) (int, error) {
	if !c.enabled.Load() {
		return c.Conn.Write(p)
	}
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > 32<<10 {
			n = 32 << 10
		}
		written, err := c.Conn.Write(p[:n])
		total += written
		p = p[written:]
		if err != nil {
			return total, err
		}
		if written == 0 {
			return total, io.ErrShortWrite
		}
		time.Sleep(time.Duration(int64(written) * int64(time.Second) / 1250000))
	}
	return total, nil
}
func TestLegacyIngressLimitedWirePongCompetition(t *testing.T) {
	e := newAgentEnv(t)
	e.srv.Close()
	enabled := &atomic.Bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", e.hub.HandleAgentWS)
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = &ingressRateListener{Listener: srv.Listener, enabled: enabled}
	srv.Start()
	t.Cleanup(srv.Close)
	e.srv = srv
	useTestSessionKeepalive(t, e.hub, 100*time.Millisecond, 5*time.Second)
	logs := &sessionDiagRecorder{}
	e.hub.sessionJournal = logs.write
	e.hub.keepaliveJournal = logs.write
	secret, _ := e.enroll("rif-wire", "Legacy")
	child := startLegacyIngress(t, e, secret)
	e.hub.mu.Lock()
	c := e.hub.sessionClients[AgentKeyID("zzc")]["legacy"]
	e.hub.mu.Unlock()
	if c == nil {
		t.Fatal("legacy peer missing")
	}
	enabled.Store(true)
	child.send(t, map[string]any{"op": "pause"})
	child.want(t, "RIF_PAUSED", time.Second)
	body := strings.Repeat("W界🙂!", (8<<20)/9) + strings.Repeat("W", (8<<20)%9)
	start := time.Now()
	err := c.write(e.ctx, model.Envelope{ID: "rif-wire", Body: body, Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true}})
	if err != nil {
		t.Fatalf("limited-wire data held frame lock and starved healthy Pong: %v", err)
	}
	digest := sha256.Sum256([]byte(body))
	child.want(t, "RIF_LARGE "+hex.EncodeToString(digest[:]), 8*time.Second)
	child.send(t, map[string]any{"op": "frames"})
	select {
	case line := <-child.lines:
		if !strings.HasPrefix(line, "RIF_FRAMES ") {
			t.Fatal("wire measurement missing", line)
		}
		var stats struct {
			Peak  int `json:"peak_data_frame"`
			Pings int `json:"control_pings"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "RIF_FRAMES ")), &stats) != nil || stats.Pings < 1 || stats.Peak > sessionFramePayloadLimit {
			t.Fatal("Ping did not compete with actual limited wire", line)
		}
		t.Logf("wire10Mbps pause3s data8MiB elapsed=%s peak_frame=%d ping_count=%d body_sha256=%x", time.Since(start), stats.Peak, stats.Pings, digest)
	case <-time.After(time.Second):
		t.Fatal("wire measurement timeout")
	}
	child.send(t, map[string]any{"op": "state"})
	child.want(t, `RIF_STATE {"alive": true}`, time.Second)
	if logs.has("session_keepalive_failed") {
		t.Fatal("healthy limited-wire helper failed keepalive")
	}
	child.stop(t)
}
