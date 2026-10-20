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

func TestPilotTemplateAcrossRealHTTPHelperIPCMCP(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set locked helper Python; canonical runs loopback integration")
	}
	e, v := templateHTTPFixture(t)
	dev, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent/console/spec-templates", e.hub.RequireAgentBlocker(e.hub.HandleTemplateDraft))
	mux.HandleFunc("GET /api/agent/console/spec-templates/{draftID}", e.hub.RequireAgentBlocker(e.hub.HandleTemplateDraft))
	mux.HandleFunc("POST /api/agent/console/spec-templates/{draftID}", e.hub.RequireAgentBlocker(e.hub.HandleTemplateDraft))
	server := httptest.NewServer(mux)
	defer server.Close()
	root, _ := filepath.Abs("../..")
	data, _ := json.Marshal(map[string]any{"origin": server.URL, "credential": e.zKimiCred, "agent_id": dev.ID, "draft_id": v.ID})
	config := filepath.Join(t.TempDir(), "fixture.json")
	if err = os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-B", filepath.Join(root, "tests/pilot-spec-template/local_roundtrip.py"), config)
	cmd.Env = []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "LOCAL_TEMPLATE_ROUNDTRIP_PASS") {
		t.Fatalf("TEMPLATE_PRODUCTION_IPC_MCP_UNWIRED: %v %s", err, output)
	}
	ref, _ := e.db.TemplateReferences(e.ctx, v.ID)
	if ref.State != "pending" {
		t.Fatal("TEMPLATE_MCP_IMPLICIT_CONFIRM_OR_EXECUTE")
	}
}
