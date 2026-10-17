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

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Synthetic in-memory identity/Hub and real Python helper/socket/shim. No PG/model/host.
func TestHostedBodySendAcrossRealHub(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set explicit isolated integration interpreter")
	}
	e := newAgentEnv(t)
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
	root, _ := filepath.Abs("../..")
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(e.ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "tools/sessionhelper/tests/hosted_body_hub_client.py"))
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
	if err := json.NewEncoder(input).Encode(map[string]string{"secret": secret, "home": home, "base": "ws" + strings.TrimPrefix(e.srv.URL, "http")}); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(output)
	phase := func(want string) {
		t.Helper()
		for scan.Scan() {
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
	phase("HOSTED_SENT")
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
	phase("HOSTED_SENT")
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
	t.Log("restricted MCP body uses actual enrolled Hub routing; group removal blocks delivery; socket receipt is not delivery proof")
}
