package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

type claudeModelCombosRT struct{}

func (claudeModelCombosRT) Lookup(cli, llm, kind string) (bool, bool) {
	if cli == "claude" && llm == "claude" && kind == "token_file" {
		return true, true
	}
	return fakeCombosRT{}.Lookup(cli, llm, kind)
}
func (claudeModelCombosRT) List() []hosted.Combo {
	return append(fakeCombosRT{}.List(), hosted.Combo{CLI: "claude", LLM: "claude", CredKind: "token_file", Verified: true})
}

func TestHostedClaudeModelActualHTTP(t *testing.T) {
	deps, st, idp, gw, _ := mkHostedDeps(t)
	ctx := context.Background()
	// Synthetic qualification only; neither actual combo definition is modified.
	deps.orch = hosted.NewOrchestrator(st, deps.pool, idp, gw, claudeModelCombosRT{}, fakeImagesRT{})
	created := make(chan struct{})
	gw.onCreated = func(id string) { _ = deps.orch.MarkOnline(ctx, id); close(created) }

	if err := r31LegacySetOnline(st, ctx, "nd_rt", json.RawMessage(`{"cpu_total":16,"mem_free_mb":16384,"disk_free_mb":102400,"models":["claude-opus-5-5"]}`)); err != nil {
		t.Fatal(err)
	}
	acct, err := deps.pool.RegisterOwned(ctx, "zzc", "ClaudeFixture", "token_file", "claude", "claude", `{"fixture":"no-real-credential"}`)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	h := hostedHTTP{t: t, mux: mux}
	for _, model := range []string{"claude-not-approved", "--help"} {
		raw, _ := json.Marshal(map[string]any{"name": "BadClaude", "node_id": "auto", "cli": "claude", "llm": "claude", "account_id": acct.ID, "plan_id": "2c4g", "model": model})
		code, body, result := h.do("zzc", "POST", "/api/console/hosted/agents", string(raw))
		if code != 422 || result["error"] != "model_not_available" || strings.Contains(body, model) {
			t.Fatalf("HTTP model denial %d %s", code, body)
		}
	}
	if len(idp.ids) != 0 || len(gw.sent) != 0 {
		t.Fatal("HTTP denial issued identity/node command")
	}
	raw, _ := json.Marshal(map[string]any{"name": "Opus55", "node_id": "auto", "cli": "claude", "llm": "claude", "account_id": acct.ID, "plan_id": "2c4g", "model": "claude-opus-5-5"})
	code, body, result := h.do("zzc", "POST", "/api/console/hosted/agents", string(raw))
	if code != 202 {
		t.Fatalf("HTTP Claude create %d %s", code, body)
	}
	job, _ := result["job_id"].(string)
	seat, err := st.GetSeat(ctx, job)
	if err != nil || seat.Model != "claude-opus-5-5" {
		t.Fatalf("HTTP lost persisted model: %+v %v", seat, err)
	}
	select {
	case <-created:
	case <-time.After(10 * time.Second):
		t.Fatal("actual HTTP async job did not create")
	}
	gw.mu.Lock()
	sent := append([]hosted.NodeCommand(nil), gw.sent...)
	gw.mu.Unlock()
	var found bool
	for _, cmd := range sent {
		if cmd.Type == "create_seat" {
			var p map[string]any
			_ = json.Unmarshal(cmd.Params, &p)
			if p["cli"] == "claude" && p["model"] == "claude-opus-5-5" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("HTTP job/node chain lost model")
	}
	if code, body, _ := h.do("zzc", "GET", "/api/console/hosted/jobs/"+job, ""); code != 200 {
		t.Fatalf("HTTP progress failed: %d %s", code, body)
	}
}
