package m8

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Real helper/outbox/MCP + Hub WS + actual write-slot backpressure. Identity is
// synthetic MemStore; SQLite is private; no deployed service or model is used.
func TestR60OutboundControlBackpressureRecoversWithSendStatus(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set explicit isolated integration interpreter")
	}
	e := newAgentEnv(t)
	repo := &cliReplayCreationRepo{SQLite: e.hub.Repo.(*store.SQLite)}
	e.hub.Repo = repo
	secret, sender := e.enroll("joint-helper", "Desktop")
	peerSecret, target := e.enroll("joint-peer", "Peer")
	group, err := e.svc.CreateGroup(e.ctx, "zzc", sender, "joint")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(e.ctx, "zzc", group.Code, target); err != nil {
		t.Fatal(err)
	}
	peer, status, _ := e.dial(peerSecret, "version=0.2.3.4")
	if peer == nil || status != 101 {
		t.Fatal("joint peer handshake failed")
	}
	defer peer.CloseNow()
	discardInitialAgentNetworkSkill(t, e.ctx, peer)
	e.sendEnv(peer, model.Envelope{ID: "joint-peer-cap", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}})
	waitFor(t, time.Second, "joint peer durable capability", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["peer"]
		e.hub.mu.Unlock()
		return c != nil && e.hub.sessionSupportsDeliveryACK(c)
	})
	root, _ := filepath.Abs("../..")
	ctx, cancel := context.WithTimeout(e.ctx, 35*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "tools/sessionhelper/tests/hosted_body_hub_client.py"))
	stderr := newBoundedStderr(childStderrLimit)
	child.Stderr = stderr
	logChildStderrOnFailure(t, "joint helper", stderr)
	home := t.TempDir()
	child.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper")}
	input, _ := child.StdinPipe()
	output, _ := child.StdoutPipe()
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	if err = json.NewEncoder(input).Encode(map[string]any{"secret": secret, "home": home, "base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "mcp_ipc_sock": shortIPCSocketPath(t), "joint_fault": true}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Phase      string `json:"phase"`
		Outcome    string `json:"outcome"`
		RequestID  string `json:"request_id"`
		SourceID   string `json:"source_message_id"`
		DeliveryID string `json:"delivery_id"`
	}
	scan := bufio.NewScanner(output)
	phase := func(want string) {
		t.Helper()
		for scan.Scan() {
			line := scan.Text()
			if strings.HasPrefix(line, "JOINT_RESULT ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "JOINT_RESULT ")), &result); err != nil {
					t.Fatal("invalid joint result")
				}
			}
			if line == "HOSTED_ERROR" {
				t.Fatalf("joint helper failed during %s (synthetic diagnostic in bounded stderr)", want)
			}
			if line == want {
				return
			}
		}
		t.Fatal("joint helper ended before " + want)
	}
	phase("HOSTED_READY")
	e.hub.mu.Lock()
	source := e.hub.sessionClients[AgentKeyID("zzc")]["desktop"]
	e.hub.mu.Unlock()
	if source == nil || source.writeGate == nil {
		t.Fatal("joint source was not negotiated")
	}
	select {
	case <-source.writeGate:
	case <-ctx.Done():
		t.Fatal("cannot hold real source write slot")
	}
	held := true
	defer func() {
		if held {
			source.writeGate <- struct{}{}
		}
	}()
	failures := func() uint64 {
		for _, v := range obs.Default.Snapshot().WS {
			if v.Kind == "session" {
				return v.OutboundReceiptWriteFail
			}
		}
		return 0
	}
	before := failures()
	started := time.Now()
	io.WriteString(input, "send\n")
	phase("JOINT_SUBMITTED")
	sourceID := result.SourceID
	if sourceID == "" || result.RequestID != "r60-joint-stable" {
		t.Fatal("joint initial caller identity missing")
	}
	waitFor(t, 4500*time.Millisecond, "bounded control write failure", func() bool { return failures() > before })
	if elapsed := time.Since(started); elapsed < 1900*time.Millisecond || elapsed > 4500*time.Millisecond {
		t.Fatalf("control backpressure did not hit the production two-second write limit: %s", elapsed)
	}
	intent, err := repo.GetOutboundIntent(ctx, sender, sourceID)
	if err != nil || intent == nil || repo.created.Load() != 1 {
		t.Fatalf("receipt timeout lost committed responsibility: %v %v", intent, err)
	}
	originalDelivery := intent.DeliveryID
	io.WriteString(input, "query_unknown\n")
	phase("JOINT_UNKNOWN")
	if result.Outcome != "unknown" || result.SourceID != sourceID {
		t.Fatal("MCP status did not expose the original unknown intent")
	}
	source.writeGate <- struct{}{}
	held = false
	io.WriteString(input, "resume\n")
	phase("JOINT_RECOVERED")
	if result.Phase != "hub_durable_queued" || result.SourceID != sourceID || result.DeliveryID != originalDelivery || repo.created.Load() != 1 {
		t.Fatalf("status recovery changed source/delivery or enqueued business twice: %+v created=%d", result, repo.created.Load())
	}
	observed := false
	for !observed {
		got := readEnvelope(t, ctx, peer)
		if got.Body != "DW_HOSTED_JOINT_MARKER" {
			continue
		}
		if got.ID != sourceID || metaString(got.Meta, "delivery_id") != originalDelivery {
			t.Fatal("physical delivery changed its stable identity")
		}
		observed = true
	}
	io.WriteString(input, "stop\n")
	phase("HOSTED_STOPPED")
	if err = child.Wait(); err != nil {
		t.Fatal("joint helper exit failed")
	}
	t.Log("actual Hub write slot held >2s; committed receipt write failed; real helper durable unknown observed through MCP dw_send_status; automatic same-source retry restored the original delivery; one Hub creation; no new caller ID/model retry")
}
