package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const approvedClaudeModel = "claude-opus-5-5"

func claudeModelOrch(t *testing.T) (*Orchestrator, *fakeIdentity, *fakeGateway, *Store, string) {
	t.Helper()
	o, idp, gw, st := mkOrch(t)
	// Only synthetic test authorization. Production combos.json remains false.
	o.combos = fakeCombos{m: map[string]bool{"claude|claude|token_file": true, "codex|gpt|token_file": true}}
	a, err := o.pool.RegisterOwned(context.Background(), "zzc", "claude-fixture", "token_file", "claude", "claude", `{"fixture":"not-a-real-credential"}`)
	if err != nil {
		t.Fatal(err)
	}
	return o, idp, gw, st, a.ID
}

func TestClaudeModelCreatePersistsAndDispatchesApprovedValue(t *testing.T) {
	parallelHostedTest(t)
	o, _, gw, st, account := claudeModelOrch(t)
	ctx := context.Background()
	if err := st.setR31LegacyOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":16384,"models":["claude-opus-5-5"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.setR31LegacyOnline(ctx, "nd_full", json.RawMessage(`{"mem_free_mb":32768,"models":["gpt-6.1-sol"]}`)); err != nil {
		t.Fatal(err)
	}
	result, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "ClaudeModel", CLI: "claude", LLM: "claude", AccountID: account, Plan: "2c4g", Model: approvedClaudeModel})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := st.GetSeat(ctx, result.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if seat.CLI != "claude" || seat.Model != approvedClaudeModel || seat.NodeID != "nd_free" {
		t.Fatalf("persisted model/selected node: %+v", seat)
	}
	o.RunJob(ctx, result.JobID)
	var dispatched bool
	for _, cmd := range gw.sent {
		if cmd.Type == "create_seat" {
			var params struct {
				CLI   string `json:"cli"`
				Model string `json:"model"`
			}
			if err := json.Unmarshal(cmd.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.CLI != "claude" || params.Model != approvedClaudeModel {
				t.Fatalf("job lost model: %+v", params)
			}
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatal("job never dispatched create")
	}

}

func TestClaudeModelCreateRejectsUnapprovedAndMalformedBeforeIssue(t *testing.T) {
	parallelHostedTest(t)
	o, idp, gw, st, account := claudeModelOrch(t)
	ctx := context.Background()
	if err := st.setR31LegacyOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":16384,"models":["claude-opus-5-5","--help"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"claude-not-approved", "--help", "claude/op us", "claude-opus-5-5\n", strings.Repeat("x", 65)} {
		_, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "InvalidClaude", CLI: "claude", LLM: "claude", AccountID: account, Plan: "2c4g", Model: model})
		if !errors.Is(err, ErrModelNotAvailable) {
			t.Fatalf("unapproved/malformed model=%q err=%v", model, err)
		}
	}
	if len(idp.issued) != 0 || len(gw.sent) != 0 {
		t.Fatal("model refusal performed issuance/node work")
	}
	// Empty node list is not approval, even for a syntactically valid name.
	if err := st.setR31LegacyOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "NoListClaude", CLI: "claude", LLM: "claude", AccountID: account, Plan: "2c4g", Model: approvedClaudeModel}); !errors.Is(err, ErrModelNotAvailable) {
		t.Fatalf("empty list: %v", err)
	}
}

func TestClaudeEmptyModelPreservesDefaultAndUnverifiedComboClosed(t *testing.T) {
	parallelHostedTest(t)
	o, _, _, st, account := claudeModelOrch(t)
	ctx := context.Background()
	o.combos = fakeCombos{m: map[string]bool{"claude|claude|token_file": false}}
	req := CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "DefaultClaude", CLI: "claude", LLM: "claude", AccountID: account, Plan: "2c4g", Model: approvedClaudeModel}
	if _, err := o.Create(ctx, req); !errors.Is(err, ErrComboNotVerified) {
		t.Fatalf("model must not open unverified combo: %v", err)
	}
	o.combos = fakeCombos{m: map[string]bool{"claude|claude|token_file": true}}
	req.Model = ""
	out, err := o.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	seat, err := st.GetSeat(ctx, out.AgentID)
	if err != nil || seat.Model != "" {
		t.Fatalf("empty must remain empty: %+v %v", seat, err)
	}
}
