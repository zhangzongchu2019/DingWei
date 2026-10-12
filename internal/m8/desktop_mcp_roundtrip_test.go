package m8

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Opt-in because ordinary Go-only unit runs need not have Python dependencies.
// scripts/test-desktop-hub-roundtrip.sh requires and sets the interpreter.
func TestDesktopMCPRoundTripAcrossRealHub(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate")
	}
	for _, mode := range []string{"legacy", "enrolled_leader_only", "enrolled_group_removed"} {
		t.Run(mode, func(t *testing.T) { desktopMCPRoundTrip(t, python, mode) })
	}
}

func desktopMCPRoundTrip(t *testing.T, python, mode string) {
	var hub *Hub
	var baseCtx context.Context
	var srv *httptest.Server
	var peer *websocket.Conn
	var sourceSecret, keyID string
	var removePeer func() error
	if mode == "legacy" {
		hub, _, baseCtx = newTestHub(t)
		if err := hub.UpsertService(baseCtx, model.RegisteredService{ID: "desktop-integration", Name: "fixture", DeliveryType: "ws", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		secret, key, err := hub.IssueAPIKey(baseCtx, "desktop-integration", "synthetic-only")
		if err != nil {
			t.Fatal(err)
		}
		sourceSecret, keyID = secret, key.ID
		mux := http.NewServeMux()
		mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
		srv = httptest.NewServer(mux)
		defer srv.Close()
		peer = dialSession(t, baseCtx, srv.URL, "peer", keyID, sourceSecret)
	} else {
		// The same actual /ws/agent path as the new platform, using only
		// synthetic in-memory identities and business-layer approval.
		e := newAgentEnv(t)
		hub, baseCtx, srv, keyID = e.hub, e.ctx, e.srv, "u_zzc"
		var desktopID string
		sourceSecret, desktopID = e.enroll("desktop-device", "Desktop")
		peerSecret, peerID := e.enroll("peer-device", "Peer")
		group, err := e.svc.CreateGroup(baseCtx, "zzc", desktopID, "mcpgroup")
		if err != nil {
			t.Fatal(err)
		}
		if err := e.svc.JoinGroup(baseCtx, "zzc", group.Code, peerID); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.SetGroupLeaderOnly(baseCtx, "zzc", group.Code, true); err != nil {
			t.Fatal(err)
		}
		removePeer = func() error { return e.svc.LeaveGroup(baseCtx, "zzc", group.Code, peerID) }
		var status int
		peer, status, _ = e.dial(peerSecret, "version=0.2.2.2")
		if peer == nil || status != http.StatusSwitchingProtocols {
			t.Fatal("synthetic approved peer handshake failed")
		}
		discardInitialAgentNetworkSkill(t, baseCtx, peer)
	}
	db := hub.Repo
	ctx, cancel := context.WithTimeout(baseCtx, 40*time.Second)
	defer cancel()
	defer peer.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, peer, model.Envelope{ID: "peer-caps", From: sessionAddress("peer", keyID), To: sessionAddress("workpulse", keyID), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "peer delivery capability", func() bool {
		hub.mu.Lock()
		c := hub.sessionClients[keyID]["peer"]
		hub.mu.Unlock()
		return c != nil && hub.sessionSupportsDeliveryACK(c)
	})
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cmd := exec.CommandContext(ctx, python, "-W", "error::RuntimeWarning", filepath.Join(root, "tools/sessionhelper/tests/desktop_hub_roundtrip_client.py"))
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || name == "PYTHONPATH" || strings.HasPrefix(name, "SH_") || strings.HasPrefix(name, "DW_") || strings.HasPrefix(name, "CODEX_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "OPENCODE_") || strings.HasPrefix(name, "DINGWEI_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "PYTHONPATH="+filepath.Join(root, "tools/sessionhelper"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Child exceptions are reduced to a type name; never echo bootstrap bytes.
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Signal(os.Interrupt)
			force := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
			_ = cmd.Wait()
			force.Stop()
		}
	}()
	encoder := json.NewEncoder(input)
	if err := encoder.Encode(map[string]string{"ws_base": "ws" + strings.TrimPrefix(srv.URL, "http"), "key_id": keyID, "secret": sourceSecret, "state": home, "mode": mode}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(output)
	readPhase := func(prefix string) string {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "BRIDGE_ERROR ") {
				t.Fatal("cross-runtime child failed; no raw child log emitted")
			}
			if strings.HasPrefix(line, prefix) {
				return strings.TrimPrefix(line, prefix)
			}
		}
		t.Fatal("cross-runtime child ended before expected phase")
		return ""
	}
	readPhase("BRIDGE_WAIT_READY ")
	question := model.Envelope{ID: "desktop-question", From: sessionAddress("peer", keyID), To: sessionAddress("desktop", keyID), Body: "DW_SYNTHETIC_QUESTION", Meta: map[string]any{"kind": "content", "reply_policy": "expect_reply"}}
	if err := writeEnvelope(ctx, peer, question); err != nil {
		t.Fatal(err)
	}
	readPhase("BRIDGE_INPUT_READY ")
	if mode == "enrolled_group_removed" {
		if err := removePeer(); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(map[string]string{"operation": "reply"}); err != nil {
		t.Fatal(err)
	}
	if mode == "enrolled_group_removed" {
		var rejection struct {
			State         string `json:"state"`
			Unread        int    `json:"unread"`
			RepeatRefused bool   `json:"repeat_refused"`
		}
		if err := json.Unmarshal([]byte(readPhase("BRIDGE_REJECTED ")), &rejection); err != nil {
			t.Fatal(err)
		}
		if rejection.State != "reply_rejected" || rejection.Unread != 1 || !rejection.RepeatRefused {
			t.Fatal("group rejection falsely completed or allowed retry")
		}
		assertNoDesktopReply(t, ctx, peer)
		if err := encoder.Encode(map[string]string{"operation": "stop"}); err != nil {
			t.Fatal(err)
		}
		readPhase("BRIDGE_CLEAN ")
		if err := cmd.Wait(); err != nil {
			t.Fatal("rejection child exited unsuccessfully")
		}
		t.Log("actual enrolled group removal rejects MCP reply, retains unread, refuses resend: PASS")
		return
	}
	var result struct {
		DeliveryID        string `json:"delivery_id"`
		SourceMessageID   string `json:"source_message_id"`
		InboundDeliveryID string `json:"inbound_delivery_id"`
		Stage             string `json:"stage"`
		UnreadAfter       int    `json:"unread_after"`
		DuplicateCached   bool   `json:"duplicate_cached"`
	}
	if err := json.Unmarshal([]byte(readPhase("BRIDGE_RESULT ")), &result); err != nil {
		t.Fatal("invalid cross-runtime result")
	}
	if result.Stage != "hub_durable_queued" || result.UnreadAfter != 0 || !result.DuplicateCached {
		t.Fatal("reply completion/cache contract failed")
	}
	d, err := db.GetSessionDelivery(ctx, result.DeliveryID)
	if err != nil || d == nil || d.SourceMessageID != result.SourceMessageID || d.SessionName != "peer" {
		t.Fatal("client receipt has no corresponding actual queue commit")
	}
	var delivered model.Envelope
	if err := json.Unmarshal([]byte(d.Envelope), &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.Body != "DW_SYNTHETIC_ANSWER" || metaString(delivered.Meta, "in_reply_to") != question.ID || delivered.To != question.From {
		t.Fatal("queue lost reply body/original target/correlation")
	}
	if _, leaked := delivered.Meta[outboundReceiptNonceKey]; leaked {
		t.Fatal("sender nonce reached recipient queue")
	}
	waitFor(t, time.Second, "desktop receive receipt", func() bool {
		inbound, err := db.GetSessionDelivery(ctx, result.InboundDeliveryID)
		return err == nil && inbound != nil && inbound.SourceMessageID == question.ID && inbound.State == "processed"
	})
	for {
		frame := readEnvelope(t, ctx, peer)
		if frame.ID == result.SourceMessageID {
			if err := writeEnvelope(ctx, peer, model.Envelope{ID: "reply-processed", From: question.From, To: sessionAddress("workpulse", keyID), Meta: map[string]any{"type": "delivery_ack", "delivery_id": result.DeliveryID, "state": "processed"}}); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	assertNoDesktopReply(t, ctx, peer)
	if err := encoder.Encode(map[string]string{"operation": "stop"}); err != nil {
		t.Fatal(err)
	}
	readPhase("BRIDGE_CLEAN ")
	if err := cmd.Wait(); err != nil {
		t.Fatal("cross-runtime child exited unsuccessfully")
	}
	t.Log("real Hub/SQLite + actual helper receive worker + MCP stdio wait/reply: PASS; synthetic identities, no model or desktop UI")
}

func assertNoDesktopReply(t *testing.T, ctx context.Context, peer *websocket.Conn) {
	t.Helper()
	readCtx, stopRead := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stopRead()
	for {
		_, raw, err := peer.Read(readCtx)
		if err != nil {
			if readCtx.Err() != context.DeadlineExceeded {
				t.Fatal("peer failed before duplicate observation completed")
			}
			break
		}
		var extra model.Envelope
		if json.Unmarshal(raw, &extra) == nil && extra.Body == "DW_SYNTHETIC_ANSWER" {
			t.Fatal("cached confirmation transmitted another reply")
		}
	}
}
