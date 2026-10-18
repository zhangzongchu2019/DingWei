package nodeagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeModelThroughActualCreateSpec(t *testing.T) {
	for _, proxy := range []string{"http://172.30.68.1:11080"} {
		t.Run(proxy, func(t *testing.T) {
			fake := newFakeDocker(t)
			d := fake.ops(t.TempDir())
			d.seatProxy = proxy
			d.seatCodexModel = "gpt-codex-default"
			d.seatModels = []string{"gpt-codex-default", "claude-opus-5-5"}
			params := json.RawMessage(`{"seat_id":"ag_claude","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"token_file","cli":"claude","model":"claude-opus-5-5"}`)
			if _, err := d.create(context.Background(), params); err != nil {
				t.Fatal(err)
			}
			if len(fake.specs) != 1 {
				t.Fatalf("specs=%d", len(fake.specs))
			}
			var env []string
			for _, v := range fake.specs[0]["Env"].([]any) {
				env = append(env, v.(string))
			}
			joined := "\n" + strings.Join(env, "\n") + "\n"
			if !strings.Contains(joined, "\nCLAUDE_MODEL=claude-opus-5-5\n") || !strings.Contains(joined, `CLAUDE_MODELS=["gpt-codex-default","claude-opus-5-5"]`) {
				t.Fatalf("missing approved model/list: %v", env)
			}
			if strings.Contains(joined, "CODEX_MODEL=") {
				t.Fatal("Claude inherited Codex model/default")
			}
		})
	}
}

func TestClaudeNodeRejectsOutsideListAndOptionBeforeEngine(t *testing.T) {
	for _, model := range []string{"claude-not-approved", "--help", "claude/op us"} {
		t.Run(model, func(t *testing.T) {
			fake := newFakeDocker(t)
			d := fake.ops(t.TempDir())
			d.seatModels = []string{"claude-opus-5-5", "--help"}
			params, _ := json.Marshal(map[string]any{"seat_id": "ag_badclaude", "image": goodImage, "resources": map[string]any{"memory_mb": 4096, "cpu_shares": 2048, "disk_mb": 8192}, "cred_kind": "token_file", "cli": "claude", "model": model})
			if _, err := d.create(context.Background(), params); err == nil {
				t.Fatal("invalid/unapproved accepted")
			}
			if len(fake.calls) != 0 {
				t.Fatalf("Engine touched before model denial: %v", fake.calls)
			}
		})
	}
}

func TestClaudeEmptyModelDoesNotInheritCodexDefault(t *testing.T) {
	fake := newFakeDocker(t)
	d := fake.ops(t.TempDir())
	d.seatCodexModel = "gpt-codex-default"
	env := strings.Join(d.seatEnv("token_file", "claude", ""), "\n")
	for _, key := range []string{"CODEX_MODEL=", "CLAUDE_MODEL=", "CLAUDE_MODELS="} {
		if strings.Contains(env, key) {
			t.Fatalf("empty Claude changed behavior: %s", env)
		}
	}
	d.seatProxy = ""
	d.seatModels = []string{"claude-opus-5-5"}
	withoutProxy := strings.Join(d.seatEnv("token_file", "claude", "claude-opus-5-5"), "\n")
	if !strings.Contains(withoutProxy, "CLAUDE_MODEL=claude-opus-5-5") {
		t.Fatal("env construction lost Claude model before proxy early return")
	}
	d.seatProxy = "http://172.30.68.1:11080"
	// Keep the original Codex value/default semantics under the same policy.
	codex := strings.Join(d.seatEnv("token_file", "codex", ""), "\n")
	if !strings.Contains(codex, "CODEX_MODEL=gpt-codex-default") || strings.Contains(codex, "CLAUDE_MODEL=") {
		t.Fatalf("Codex changed: %s", codex)
	}
}

func TestClaudeUpgradeKeepsRecordedApprovedModel(t *testing.T) {
	fake := newFakeDocker(t)
	d := fake.ops(t.TempDir())
	d.seatModels = []string{"claude-opus-5-5"}
	params := json.RawMessage(`{"seat_id":"ag_claudeup","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8192},"cred_kind":"token_file","cli":"claude","model":"claude-opus-5-5"}`)
	if _, err := d.create(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if _, err := d.upgrade(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if len(fake.specs) != 2 {
		t.Fatalf("upgrade did not create candidate: %d", len(fake.specs))
	}
	var selected bool
	for _, v := range fake.specs[1]["Env"].([]any) {
		if v == "CLAUDE_MODEL=claude-opus-5-5" {
			selected = true
		}
	}
	if !selected {
		t.Fatal("upgrade lost recorded Claude model")
	}
	// Membership is rechecked before a new Claude runtime can be built. Codex's
	// existing recorded-model behavior and tests are untouched.
	d.seatModels = []string{"claude-other-approved"}
	calls := len(fake.calls)
	if _, err := d.upgrade(context.Background(), params); err == nil {
		t.Fatal("removed Claude model accepted at upgrade")
	}
	if len(fake.calls) != calls {
		t.Fatal("Engine touched after model was removed")
	}
}
