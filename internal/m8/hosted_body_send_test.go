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
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Synthetic in-memory identity/Hub and real Python helper/socket/shim. No PG/model/host.
func TestHostedBodySendAcrossRealHub(t *testing.T) { runHostedBodySendAcrossRealHub(t, false) }
func TestHostedBodySendSameRequestIDAcrossCLIProcesses(t *testing.T) {
	runHostedBodySendAcrossRealHub(t, true)
}

type cliReplayCreationRepo struct {
	*store.SQLite
	created atomic.Int32
}

func (r *cliReplayCreationRepo) CommitOutboundSessionDelivery(ctx context.Context, v model.OutboundIntent, d model.SessionDelivery) (*model.OutboundIntent, bool, error) {
	saved, created, err := r.SQLite.CommitOutboundSessionDelivery(ctx, v, d)
	if err == nil && created {
		r.created.Add(1)
	}
	return saved, created, err
}
func runHostedBodySendAcrossRealHub(t *testing.T, cliRepeat bool) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set explicit isolated integration interpreter")
	}
	e := newAgentEnv(t)
	counter := &cliReplayCreationRepo{SQLite: e.hub.Repo.(*store.SQLite)}
	if cliRepeat {
		e.hub.Repo = counter
	}
	secret, sourceID := e.enroll("hosted-device", "Desktop")
	peerSecret, peerID := e.enroll("hosted-peer", "Peer")
	group, err := e.svc.CreateGroup(e.ctx, "zzc", sourceID, "h1group")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.JoinGroup(e.ctx, "zzc", group.Code, peerID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetGroupLeaderOnly(e.ctx, "zzc", group.Code, true); err != nil {
		t.Fatal(err)
	}
	peer, status, _ := e.dial(peerSecret, "version=0.2.3.4")
	if peer == nil || status != 101 {
		t.Fatal("synthetic peer handshake failed")
	}
	defer peer.Close(websocket.StatusNormalClosure, "done")
	discardInitialAgentNetworkSkill(t, e.ctx, peer)
	e.sendEnv(peer, model.Envelope{ID: "peer-durable-capabilities", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}})
	waitFor(t, time.Second, "peer durable queue negotiation", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["peer"]
		e.hub.mu.Unlock()
		return c != nil && e.hub.sessionSupportsDeliveryACK(c)
	})
	root, _ := filepath.Abs("../..")
	home := t.TempDir()
	ipcSocket := shortIPCSocketPath(t)
	ctx, cancel := context.WithTimeout(e.ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "tools/sessionhelper/tests/hosted_body_hub_client.py"))
	stderr := newBoundedStderr(childStderrLimit)
	cmd.Stderr = stderr
	logChildStderrOnFailure(t, "hosted body helper child", stderr)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper")}
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	if err := json.NewEncoder(input).Encode(map[string]any{"secret": secret, "home": home, "base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "mcp_ipc_sock": ipcSocket, "cli_repeat": cliRepeat}); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(output)
	var repeatResult struct {
		SourceID   string `json:"source_message_id"`
		DeliveryID string `json:"delivery_id"`
		Calls      int    `json:"process_send_calls"`
	}
	phase := func(want string) {
		t.Helper()
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "CLI_REPEAT_RESULT ") {
				if json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "CLI_REPEAT_RESULT ")), &repeatResult) != nil {
					t.Fatal("bad CLI repeat result")
				}
			}
			if scan.Text() == "HOSTED_ERROR" {
				t.Fatal("synthetic child failed; no raw secret output")
			}
			if scan.Text() == want {
				return
			}
		}
		t.Fatal("synthetic child ended before phase " + want)
	}
	phase("HOSTED_READY")
	io.WriteString(input, "send\n")
	if cliRepeat {
		phase("CLI_REPEAT_RESOLVED")
		if repeatResult.Calls != 2 || repeatResult.SourceID == "" || repeatResult.DeliveryID == "" {
			t.Fatal("two independent CLI sends did not preserve IDs", repeatResult)
		}
		intent, err := counter.GetOutboundIntent(e.ctx, sourceID, repeatResult.SourceID)
		if err != nil || intent == nil || intent.DeliveryID != repeatResult.DeliveryID || counter.created.Load() != 1 {
			t.Fatalf("same request ID created more than one Hub delivery: created=%d intent=%+v err=%v", counter.created.Load(), intent, err)
		}
		seen := 0
		observe, done := context.WithTimeout(ctx, 350*time.Millisecond)
		defer done()
		for {
			_, raw, err := peer.Read(observe)
			if err != nil {
				if observe.Err() != context.DeadlineExceeded {
					t.Fatal("peer closed before duplicate observation", err)
				}
				break
			}
			var got model.Envelope
			if json.Unmarshal(raw, &got) == nil && got.Body == "DW_HOSTED_SEND_MARKER" {
				seen++
				if got.ID != repeatResult.SourceID || metaString(got.Meta, "delivery_id") != repeatResult.DeliveryID {
					t.Fatal("different physical delivery identity")
				}
			}
		}
		if seen != 1 || counter.created.Load() != 1 {
			t.Fatalf("two CLI invocations produced %d wire deliveries / %d committed creations", seen, counter.created.Load())
		}
		io.WriteString(input, "stop\n")
		phase("HOSTED_STOPPED")
		if err := cmd.Wait(); err != nil {
			t.Fatal("CLI child failed")
		}
		t.Log("two installed send.py processes with identical --request-id; real IPC/helper/WS/Hub; exactly one committed Hub delivery and one observed delivery frame; same source/delivery restored")
		return
	}
	phase("HOSTED_RESOLVED")
	for {
		got := readEnvelope(t, ctx, peer)
		if got.Body == "DW_HOSTED_SEND_MARKER" {
			if got.To != "peer#u_zzc" || metaString(got.Meta, "reply_policy") != "no_reply" {
				t.Fatal("routing/meta changed")
			}
			break
		}
	}
	if err := e.svc.LeaveGroup(e.ctx, "zzc", group.Code, peerID); err != nil {
		t.Fatal(err)
	}
	io.WriteString(input, "send\n")
	phase("HOSTED_RESOLVED")
	// Keep the authentic peer connection; removed membership must prevent actual delivery.
	observe, done := context.WithTimeout(ctx, 300*time.Millisecond)
	defer done()
	for {
		_, raw, err := peer.Read(observe)
		if err != nil {
			if observe.Err() != context.DeadlineExceeded {
				t.Fatal("peer lost before rejection observation")
			}
			break
		}
		var got model.Envelope
		if json.Unmarshal(raw, &got) == nil && got.Body == "DW_HOSTED_SEND_MARKER" {
			t.Fatal("restricted sender bypassed group removal or resent")
		}
	}
	io.WriteString(input, "stop\n")
	phase("HOSTED_STOPPED")
	if err := cmd.Wait(); err != nil {
		t.Fatal("synthetic child exit failed")
	}
	t.Log("restricted MCP body uses actual enrolled Hub routing; group removal blocks delivery; durable acceptance and explicit group rejection are distinct; local responsibility is not delivery proof")
}
