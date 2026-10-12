package m8

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func exerciseCostSeedProduct(t *testing.T, h *Hub, base, token, state, keyPath string) {
	t.Helper()
	status := func() int {
		r, _ := http.NewRequest("GET", base+"/view/agent04.owner/status", nil)
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if status() != 503 {
		t.Fatal("unseeded history silently exposed")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	path := "/internal/console/cost-seed-authority"
	body := []byte(`{"target":"target-b","full_names":["agent04.owner"]}`)
	req, _ := http.NewRequest("POST", base+path, bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, _ := muxID()
	req.Header.Set("X-DW-VS-Target", "target-b")
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(bytes.TrimSpace(key), vsToHubDirection, "POST", path, ts, nonce, body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var authority struct {
		Agents []map[string]string `json:"agents"`
	}
	json.NewDecoder(res.Body).Decode(&authority)
	res.Body.Close()
	if res.StatusCode != 200 || len(authority.Agents) != 1 {
		t.Fatal("signed seed authority refused")
	}
	row := map[string]any{}
	for k, v := range authority.Agents[0] {
		row[k] = v
	}
	row["legacy_name"] = "old-agent04"
	row["coverage"] = "all_completed_through_H"
	mapping := map[string]any{"version": "cost-seed-v1", "target": "target-b", "bindings": []any{row}}
	encoded, _ := json.Marshal(mapping)
	file := filepath.Join(t.TempDir(), "approved-map.json")
	os.WriteFile(file, encoded, 0600)
	old, _ := os.ReadFile(state)
	oldSHA := sha256.Sum256(old)
	mapSHA := sha256.Sum256(encoded)
	for i := 0; i < 2; i++ {
		cmd := exec.Command("python3", "migrate_mux_cost.py", "--legacy-state", state, "--legacy-sha256", hex.EncodeToString(oldSHA[:]), "--mapping", file, "--mapping-sha256", hex.EncodeToString(mapSHA[:]), "--projection", state+".mux-cost.db", "--authority-url", base, "--target", "target-b", "--service-key-file", keyPath, "--consumers-stopped")
		cmd.Dir = "../../view-service"
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("controlled synthetic seed: %v %s", err, out)
		}
		t.Logf("E2_SYNTHETIC_CONTROLLED_SEED iteration=%d summary=%s", i, string(out))
	}
	if status() != 200 {
		t.Fatal("seeded public status unavailable")
	}
	// Real Python VS + public authorized proxy: subject generation failure may
	// not reach the legacy account. Check fixed error and private source unchanged.
	routing := *loadViewRouting()
	copiedTargets := map[string]ViewTarget{}
	for k, v := range routing.Targets {
		copiedTargets[k] = v
	}
	routing.Targets = copiedTargets
	_, target := routing.resolve("agent04.owner", "owner")
	forward := target.HubToVSSecretFile
	original, err := os.ReadFile(forward)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"missing", "short", "bad-signature"} {
		os.Remove(forward)
		if fault == "short" {
			os.WriteFile(forward, []byte("short"), 0600)
		}
		if fault == "bad-signature" {
			os.WriteFile(forward, original, 0600) // VS retains its original key.
			wrong := filepath.Join(t.TempDir(), "wrong-forward")
			os.WriteFile(wrong, bytes.Repeat([]byte("z"), 32), 0600)
			changed := target
			changed.HubToVSSecretFile = wrong
			routing.Targets["target-b"] = changed
			raw, _ := json.Marshal(routing)
			os.WriteFile(os.Getenv("VIEW_ROUTING_CONFIG"), raw, 0600)
			resetViewRoutingCacheForTest()
		}
		for _, ep := range []string{"status", "events"} {
			r, _ := http.NewRequest("GET", base+"/view/agent04.owner/"+ep, nil)
			r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
			for _, name := range []string{"X-DW-Mux-Subject", "X-DW-VS-Target", "X-DW-Timestamp", "X-DW-Nonce", "X-DW-Signature"} {
				r.Header.Set(name, "forged")
			}
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 503 || !strings.Contains(string(data), "projection_subject_unavailable") {
				t.Fatalf("true subject failure %s/%s: %d", fault, ep, resp.StatusCode)
			}
		}
		unchanged, _ := os.ReadFile(state)
		if !bytes.Equal(unchanged, old) {
			t.Fatal("subject failure changed legacy private state")
		}
	}
	os.WriteFile(forward, original, 0600)
	routing.Targets["target-b"] = target
	encodedRouting, _ := json.Marshal(routing)
	os.WriteFile(os.Getenv("VIEW_ROUTING_CONFIG"), encodedRouting, 0600)
	resetViewRoutingCacheForTest()
	if status() != 200 {
		t.Fatal("restored subject did not recover true VS projection")
	}
	t.Log("E1_TRUE_PUBLIC_PG_PYTHON_VS missing/short/bad-signature events+status fixed503, forged browser subject stripped, legacy source unchanged, restored200 PASS")

	t.Log("E2_TRUE_PG_SIGNED_AUTHORITY_AND_OFFLINE_TOOL public unseeded503/seed/repeat seeded200 PASS; historical source is synthetic and coverage explicitly known")
}
