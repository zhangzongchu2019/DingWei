package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHostedMinimumPlanHTTPOptionsCreateAndQuota(t *testing.T) {
	h, _, _, _, _ := mkHostedHTTP(t)
	code, body, opts := h.do("zzc", "GET", "/api/console/hosted/options", "")
	if code != http.StatusOK {
		t.Fatalf("options: %d %s", code, body)
	}
	var plan map[string]any
	for _, row := range opts["plans"].([]any) {
		p := row.(map[string]any)
		if p["id"] == "1c1.5g40" {
			plan = p
		}
	}
	if plan == nil || plan["mem_gb"] != 1.5 || plan["name"] != "1C/1.5G/40G" || plan["fits"] != true {
		t.Fatalf("minimum option: %+v", plan)
	}
	account := opts["accounts"].([]any)[0].(map[string]any)["id"].(string)
	request, _ := json.Marshal(map[string]any{"name": "Minimum", "node_id": "auto", "cli": "codex", "llm": "gpt", "account_id": account, "plan_id": "1c1.5g40"})
	code, body, _ = h.do("zzc", "POST", "/api/console/hosted/agents", string(request))
	if code != http.StatusAccepted {
		t.Fatalf("actual HTTP create: %d %s", code, body)
	}
	code, body, _ = h.do("zzc", "GET", "/api/console/hosted/options", "")
	if code != http.StatusOK || !strings.Contains(body, `"remaining":{"agents":1,"vcpu":3,"mem_gb":6.5,"disk_gb":984}`) {
		t.Fatalf("minimum quota debit: %d %s", code, body)
	}
	code, body, _ = h.do("admin", "GET", "/api/console/admin/quotas", "")
	if code != http.StatusOK || !strings.Contains(body, `"agents":{"used":1,"limit":2},"vcpu":{"used":1,"limit":4},"mem_gb":{"used":1.5,"limit":8},"disk_gb":{"used":40,"limit":1024}`) {
		t.Fatalf("admin fractional usage: %d %s", code, body)
	}
	// Existing API permissions remain in force; the new plan is no bypass.
	code, _, _ = h.do("", "POST", "/api/console/hosted/agents", string(request))
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous create: %d", code)
	}
}
