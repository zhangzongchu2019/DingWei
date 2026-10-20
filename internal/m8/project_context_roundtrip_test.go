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

func TestProjectContextAcrossRealHTTPHelperIPCMCP(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set")
	}
	e, _, _ := r32FlowEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent/console/project-context", e.hub.HandleProjectContext)
	server := httptest.NewServer(mux)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "fixture.json")
	body, _ := json.Marshal(map[string]string{"origin": server.URL, "credential": e.zKimiCred})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-B", filepath.Join(root, "tests/pilot-project-context/local_context_roundtrip.py"), path)
	// The probe creates its own isolated HOME before constructing the real helper.
	cmd.Env = []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "PYTHONPATH=" + filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8", "HOME=" + t.TempDir()}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "LOCAL_CONTEXT_ROUNDTRIP_PASS") {
		t.Fatalf("PC_CROSS_RUNTIME_PRODUCTION_WIRING_FAILED: %v %s", err, output)
	}
	t.Log("real local Go Hub HTTP, SessionHelper constructor/IPC, actual shim stdio, dedicated credential and bounded projection: PASS; no live Hub or AI")
}
