package m8

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
}
