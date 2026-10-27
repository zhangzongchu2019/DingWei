package m8

import (
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

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestBlockerAcrossRealHTTPHelperIPCMCP(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set locked helper Python; canonical runs this local integration")
	}
	e, _, state := r32FlowEnv(t)
	mux := http.NewServeMux()
	for _, method := range []string{"GET", "POST"} {
		for _, suffix := range []string{"", "/{blockerID}"} {
			mux.HandleFunc(method+" /api/agent/console/tasks/{taskID}/blockers"+suffix, e.hub.RequireAgentBlocker(e.hub.HandleTaskBlockers))
		}
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"origin": server.URL, "credential": e.zKimiCred, "agent_id": state.Developer, "task_id": state.ID, "task_version": state.Version})
	config := filepath.Join(t.TempDir(), "fixture.json")
	if err = os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-B", filepath.Join(root, "tests/pilot-blocker-wake/local_roundtrip.py"), config)
	cmd.Env = []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "LOCAL_BLOCKER_ROUNDTRIP_PASS") {
		t.Fatalf("BLOCKER_PRODUCTION_IPC_MCP_UNWIRED: %v %s", err, output)
	}
	list, err := e.db.ListConsoleTasksForAgent(e.ctx, state.Developer)
	if err != nil || len(list) != 1 || list[0].Status != "planned" {
		t.Fatal("BLOCKER_REAL_MCP_IMPLICIT_START", list, err)
	}
	t.Log("five actual MCP calls through helper/HTTP; current task remains planned; no live Hub, CLI, AI or business execution")
	t.Run("missing_identity_ack_returns_to_hub", testBlockerMissingIdentityACKRoundtrip)
}

func testBlockerMissingIdentityACKRoundtrip(t *testing.T) {
	e, _, _ := blockerReadyFixture(t)
	record := &blockerFinishWindow{SQLite: e.db}
	e.hub.Repo = record
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"origin": e.agentEnv.srv.URL, "credential": e.zKimiCred})
	config := filepath.Join(t.TempDir(), "local-fixture.json")
	if err = os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	cmd := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "tests/pilot-blocker-wake/missing_identity_ack.py"), config)
	cmd.Env = []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"}
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() { output, err := cmd.CombinedOutput(); done <- result{output, err} }()
	waitFor(t, 5*time.Second, "actual Python helper wake capabilities", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
		e.hub.mu.Unlock()
		if c == nil {
			return false
		}
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		return c.deliveryACKV1 && c.blockerNoticeV1 && c.blockerWakeV1
	})
	e.hub.dispatchBlockerWakes(ctx)
	r := <-done
	if r.err != nil {
		t.Fatalf("actual helper ACK roundtrip: %v %s", r.err, r.output)
	}
	var ack model.Envelope
	for _, line := range strings.Split(string(r.output), "\n") {
		if strings.HasPrefix(line, "ACK_RESULT ") {
			if err = json.Unmarshal([]byte(strings.TrimPrefix(line, "ACK_RESULT ")), &ack); err != nil {
				t.Fatal(err)
			}
		}
	}
	if metaString(ack.Meta, "type") != "delivery_ack" || metaString(ack.Meta, "state") != "processed" || metaString(ack.Meta, "reason") != "current_agent_identity_unavailable" {
		t.Fatalf("actual helper ACK reason lost: %+v", ack.Meta)
	}
	id := metaString(ack.Meta, "delivery_id")
	waitFor(t, time.Second, "Hub consumed actual legacy receipt ACK", func() bool {
		d, _ := e.db.GetSessionDelivery(e.ctx, id)
		return d != nil && d.State == "legacy_received" && d.ProcessedAt == nil && d.InjectedAt == nil && d.AckProtocol == 1
	})
	d, _ := e.db.GetSessionDelivery(e.ctx, id)
	if d.LastError != "" {
		t.Fatalf("processed branch changed reason storage: %q", d.LastError)
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if len(record.ids) != 1 {
		t.Fatalf("notice-only ACK caused another delivery: %v", record.ids)
	}
	t.Log("actual helper -> WS -> Hub: reason arrives, legacy receipt only, no model timestamps or extra delivery")
}
