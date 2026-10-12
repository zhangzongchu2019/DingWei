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
	t.Log("E2_TRUE_PG_SIGNED_AUTHORITY_AND_OFFLINE_TOOL public unseeded503/seed/repeat seeded200 PASS; historical source is synthetic and coverage explicitly known")
}
