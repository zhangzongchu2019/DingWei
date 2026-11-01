//go:build r6_compat

package m8

import (
	"context"
	"encoding/json"
	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestR6HelperHubCompatibility(t *testing.T) {
	python := os.Getenv("DW_R6_COMPAT_PYTHON")
	legacy := os.Getenv("DW_R6_COMPAT_LEGACY_TREE")
	if python == "" || legacy == "" {
		t.Fatal("set locked DW_R6_COMPAT_PYTHON and fixed r46 DW_R6_COMPAT_LEGACY_TREE; run tests/r6-compat/run.py")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	for _, cell := range []string{"new-helper-new-off", "old-helper-new-on", "new-helper-new-on"} {
		t.Run(cell, func(t *testing.T) {
			env, _, q := r6Fixture(t)
			h := env.hub
			now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			start := now
			h.CoordinateReviews.Now = func() time.Time { return now }
			sent := 0
			if e := env.svc.Store.CreateRegistrationCode(env.ctx, "zzc", "r6-compat-test-digest", now.Add(24*time.Hour)); e != nil {
				t.Fatal(e)
			}
			if _, e := env.svc.Store.RedeemRegistrationCode(env.ctx, "r6-compat-test-digest", identity.Identity{Provider: "feishu", Issuer: "test-bot", Subject: "test-reviewer"}, now); e != nil {
				t.Fatal(e)
			}
			h.CoordinateReviewSender = func(_ context.Context, issuer, subject, id, body string) (string, error) {
				var fields map[string]any
				if issuer != "test-bot" || subject != "test-reviewer" || json.Unmarshal([]byte(body), &fields) != nil || len(fields) != 5 {
					t.Fatal("R6_COMPAT_PRIVATE_NOTICE")
				}
				sent++
				return "receipt", nil
			}
			if cell == "new-helper-new-off" {
				t.Setenv("DW_OPENSPEC_REVIEW_ENABLED", "")
				h.ConfigureCoordinateReviews(nil)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
			mux.HandleFunc("GET /api/agent/console/tasks", h.RequireAgentTaskFlow(h.HandleAgentTaskFlow))
			mux.HandleFunc("GET /api/agent/console/coordinate-reviews/{requestID}", h.RequireAgentTaskFlow(h.HandleAgentCoordinateReviews))
			mux.HandleFunc("POST /api/agent/console/coordinate-reviews/{requestID}/{action}", h.RequireAgentTaskFlow(h.HandleAgentCoordinateReviews))
			mux.HandleFunc("POST /api/console/coordinate-reviews", h.RequireConsolePrincipal(h.HandleCoordinateReviews))
			id := "coordreview_" + strings.Repeat("a", 64)
			mux.HandleFunc("POST /fixture/tick", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Minutes int `json:"minutes"`
				}
				if json.NewDecoder(r.Body).Decode(&in) != nil {
					w.WriteHeader(400)
					return
				}
				now = start.Add(time.Duration(in.Minutes) * time.Minute)
				for i := 0; i < 3; i++ {
					h.processCoordinateReviews(r.Context())
				}
				q, e := h.CoordinateReviews.Get(r.Context(), "zzc", id)
				if e != nil {
					t.Error(e)
					w.WriteHeader(500)
					return
				}
				minutes := []float64{}
				for _, n := range q.Notices {
					minutes = append(minutes, n.At.Sub(start).Minutes())
				}
				taskJSON(w, 200, map[string]any{"notices": len(q.Notices), "notice_minutes": minutes, "sent": sent, "opened": !q.Opened.IsZero(), "completed": !q.Completed.IsZero(), "dispatch": q.Dispatch})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			h.PublicOrigin = server.URL
			req, _ := http.NewRequest("POST", server.URL+"/api/console/coordinate-reviews", strings.NewReader(r6JSON(q)))
			req.Header.Set("Origin", server.URL)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: env.tokZZC})
			resp, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if cell == "new-helper-new-off" {
				if resp.StatusCode != 503 {
					t.Fatal("R6_COMPAT_DARK_CREATE", resp.StatusCode)
				}
			} else {
				var out struct {
					Request cr.Record `json:"request"`
				}
				if resp.StatusCode != 200 || json.Unmarshal(raw, &out) != nil {
					t.Fatal("R6_COMPAT_CREATE", resp.StatusCode, string(raw))
				}
				id = out.Request.ID
			}
			helperRoot := os.Getenv("DW_R6_COMPAT_HELPER_TREE")
			if helperRoot == "" {
				helperRoot = filepath.Join(root, "tools/sessionhelper")
			}
			if cell == "old-helper-new-on" {
				helperRoot = filepath.Join(legacy, "tools/sessionhelper")
			}
			config := filepath.Join(t.TempDir(), "fixture.json")
			body, _ := json.Marshal(map[string]string{"case": cell, "origin": server.URL, "credential": env.zKimiCred, "agent_id": q.AgentID, "request_id": id, "create_status": strconv.Itoa(resp.StatusCode)})
			if e = os.WriteFile(config, body, 0600); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(python, "-B", filepath.Join(root, "tests/r6-compat/probe.py"), config)
			cmd.Env = []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "PYTHONPATH=" + helperRoot, "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"}
			output, e := cmd.CombinedOutput()
			if e != nil || !strings.Contains(string(output), "R6_COMPAT_PASS") {
				t.Fatalf("R6_COMPAT_CELL_FAILED %s: %v %s", cell, e, output)
			}
			t.Log(string(output))
		})
	}
}
