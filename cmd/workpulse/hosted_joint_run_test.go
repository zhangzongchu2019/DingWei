package main

// 托管区联调七处（组长裁定 lead-g4-joint-run-1）与 T-G4-35 返修在 HTTP 层的判据：一律经真 handler、真 pgtest 库。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

type hostedHTTP struct {
	t   *testing.T
	mux *http.ServeMux
}

func (h hostedHTTP) do(actor, method, path, body string) (int, string, map[string]any) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		req.Header.Set("X-Test-Actor", actor)
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", testRouteOrigin) // 0.2.1.101：hosted 写路由加 Origin 守卫
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, rec.Body.String(), m
}

func mkHostedHTTP(t *testing.T) (hostedHTTP, hostedDeps, *hosted.Store, *fakeIDP, *fakeGW) {
	t.Helper()
	deps, st, idp, gw, _ := mkHostedDeps(t)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	return hostedHTTP{t: t, mux: mux}, deps, st, idp, gw
}

// 建一席并跑完执行链（假网关全成 ⇒ online）。
func onlineSeat(t *testing.T, deps hostedDeps, st *hosted.Store) string {
	t.Helper()
	ctx := context.Background()
	accts, err := st.ListUsableAccounts(ctx, "zzc", "", "")
	if err != nil || len(accts) == 0 {
		t.Fatalf("no usable account: %v", err)
	}
	out, err := deps.orch.Create(ctx, hosted.CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: accts[0].ID, Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	deps.orch.RunJob(ctx, out.AgentID)
	return out.AgentID
}

// The hosted owner fence uniformly refuses unresolved sessions before business handlers.
func TestHostedAuthCodes(t *testing.T) {
	h, _, _, _, _ := mkHostedHTTP(t)
	for _, path := range []string{"/api/console/hosted/options", "/api/console/hosted/jobs/x", "/api/console/admin/quotas"} {
		if st, _, m := h.do("", "GET", path, ""); st != 403 || m["error"] != "forbidden" {
			t.Errorf("GET %s no session: %d %v", path, st, m)
		}
		if st, _, m := h.do("must-change", "GET", path, ""); st != 403 || m["error"] != "forbidden" {
			t.Errorf("GET %s must-change: %d %v", path, st, m)
		}
	}
}

// 第 2 条：创建失败（Agent 已 Retire）的任务，业主仍读到 {stage:failed, reason}；别人、不存在同答 403 not_owner。
func TestHostedJobOwnerBySeat(t *testing.T) {
	h, deps, st, idp, gw := mkHostedHTTP(t)
	gw.failOn = "pull_image"
	ctx := context.Background()
	accts, _ := st.ListUsableAccounts(ctx, "zzc", "", "")
	out, err := deps.orch.Create(ctx, hosted.CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: accts[0].ID, Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	deps.orch.RunJob(ctx, out.AgentID)
	if !idp.retired[out.AgentID] {
		t.Fatal("fixture: failed job must have retired its agent")
	}
	deps.ownsAgent = func(*http.Request, string, string) bool { return false } // 身份侧已不认这个 Agent
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	h.mux = mux
	if code, body, _ := h.do("zzc", "GET", "/api/console/hosted/jobs/"+out.JobID, ""); code != 200 || strings.TrimSpace(body) != `{"stage":"failed","reason":"image_pull_failed"}` {
		t.Fatalf("owner: %d %s", code, body)
	}
	for _, c := range []struct{ actor, job string }{{"eve", out.JobID}, {"zzc", "ag_nosuch"}} {
		if code, _, m := h.do(c.actor, "GET", "/api/console/hosted/jobs/"+c.job, ""); code != 403 || m["error"] != "not_owner" {
			t.Errorf("%s reading %s: %d %v", c.actor, c.job, code, m)
		}
	}
}

// 第 3 条：每个可能到达 handler 的错误都译成契约机器码；Go 错误文本一个字也不出；未列出的错误 ⇒ 500 internal_error。
func TestHostedErrorsAreMachineCodes(t *testing.T) {
	for _, m := range hostedErrorCodes {
		rec := httptest.NewRecorder()
		wrapped := fmt.Errorf("layer: %w", m.err)
		writeHostedError(rec, wrapped)
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != m.status || got["error"] != m.code || len(got) != 1 {
			t.Errorf("%v: %d %s, want %d {error:%s}", m.err, rec.Code, rec.Body, m.status, m.code)
		}
		if strings.Contains(rec.Body.String(), "layer") || (m.err.Error() != m.code && strings.Contains(rec.Body.String(), m.err.Error())) {
			t.Errorf("%v: Go error text leaked: %s", m.err, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	writeHostedError(rec, errors.New("pq: connection refused at 10.0.0.1"))
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":"internal_error"}` {
		t.Errorf("unknown error: %d %s", rec.Code, rec.Body)
	}
}

// 第 3 条在真路由上：名称不合规（G3 适配器透传的 identity.ErrNameInvalid）⇒ 400 name_invalid。
func TestHostedCreateNameInvalidCode(t *testing.T) {
	h, _, st, _, _ := mkHostedHTTP(t)
	accts, _ := st.ListUsableAccounts(context.Background(), "zzc", "", "")
	code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents",
		`{"name":"bad name!","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+accts[0].ID+`","plan_id":"2c4g"}`)
	if code != 400 || m["error"] != "name_invalid" || strings.Contains(body, "identity:") {
		t.Fatalf("%d %s", code, body)
	}
}

// 第 4 条：销毁时备份不可用 ⇒ 入队成功，job 最终 failed/backup_failed；席位不动。
func TestHostedDestroyBackupFailed(t *testing.T) {
	h, deps, st, _, _ := mkHostedHTTP(t)
	id := onlineSeat(t, deps, st)
	code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents/"+id+"/destroy", `{"confirm":true}`)
	jobID, _ := m["job_id"].(string)
	if code != http.StatusAccepted || jobID == "" {
		t.Fatalf("enqueue: %d %s", code, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		code, body, m = h.do("zzc", "GET", "/api/console/hosted/jobs/"+jobID, "")
		if m["stage"] == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not fail: %d %s", code, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code != http.StatusOK || m["reason"] != "backup_failed" {
		t.Fatalf("job result: %d %s", code, body)
	}
	// 组长取甲（2026-10-02）：在线销毁先停容器再备份；备份不成 ⇒ 不吊销、不删，席位如实记 error／backup_failed。
	if seat, _ := st.GetSeat(context.Background(), id); seat.State != "error" || seat.StateDetail != "backup_failed" {
		t.Fatalf("seat must show backup_failed, nothing deleted: %+v", seat)
	}
}

// 第 5 条：GET admin/quotas 契约形状（GB）逐字；PUT 路径里的用户 ID 不分大小写落到同一行。
func TestHostedAdminQuotasShape(t *testing.T) {
	h, deps, st, _, _ := mkHostedHTTP(t)
	onlineSeat(t, deps, st) // 已用一席 2C/4G/8G（R-G4-DISK-TIER 后 2c4g 磁盘档 8192MB）
	code, body, _ := h.do("admin", "GET", "/api/console/admin/quotas", "")
	want := `[{"user":{"id":"Zzc","short_name":"zz"},"agents":{"used":1,"limit":2},"vcpu":{"used":2,"limit":4},"mem_gb":{"used":4,"limit":8},"disk_gb":{"used":8,"limit":1024}}]`
	if code != 200 || strings.TrimSpace(body) != want {
		t.Fatalf("GET quotas: %d\n got %s\nwant %s", code, body, want)
	}
	if code, body, _ := h.do("admin", "PUT", "/api/console/admin/quotas/ZZC", `{"agents_limit":5,"vcpu_limit":6,"mem_gb_limit":12,"disk_gb_limit":20}`); code != 200 {
		t.Fatalf("PUT: %d %s", code, body)
	}
	_, body, _ = h.do("admin", "GET", "/api/console/admin/quotas", "")
	if !strings.Contains(body, `"agents":{"used":1,"limit":5},"vcpu":{"used":2,"limit":6},"mem_gb":{"used":4,"limit":12},"disk_gb":{"used":8,"limit":20}`) {
		t.Fatalf("PUT with upper-case ID must land on the lower-case row: %s", body)
	}
}

// 第 6 条：两个码的分界走真路由，两种都带 remaining。
func TestHostedQuotaCodesOverHTTP(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		quota hosted.Quota
		plan  string
		want  string
		rem   string
	}{
		{"plan too big", hosted.Quota{UserLC: "zzc", AgentsLimit: 2, VCPULimit: 3, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, "4c8g",
			"plan_exceeds_quota", `"remaining":{"agents":2,"disk_gb":1024,"mem_gb":1024,"vcpu":3}`},
		{"no agent slot", hosted.Quota{UserLC: "zzc", AgentsLimit: 0, VCPULimit: 64, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, "2c4g",
			"quota_exceeded", `"remaining":{"agents":0,"disk_gb":1024,"mem_gb":1024,"vcpu":64}`},
	} {
		h, _, st, _, _ := mkHostedHTTP(t)
		if err := st.PutQuota(ctx, c.quota, "admin"); err != nil {
			t.Fatal(err)
		}
		accts, _ := st.ListUsableAccounts(ctx, "zzc", "", "")
		code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents",
			`{"name":"w9","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+accts[0].ID+`","plan_id":"`+c.plan+`"}`)
		if code != 409 || m["error"] != c.want || !strings.Contains(body, c.rem) {
			t.Errorf("%s: %d %s, want 409 %s with %s", c.name, code, body, c.want, c.rem)
		}
	}
}

// 第 7 条在真路由上：节点发不到 ⇒ 入队 202，jobs 最终给出 node_offline，席位状态不变。
func TestHostedLifecycleNodeDown(t *testing.T) {
	h, deps, st, _, gw := mkHostedHTTP(t)
	id := onlineSeat(t, deps, st)
	gw.downOn = "stop_seat"
	code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents/"+id+"/disable", `{"confirm":true}`)
	jobID, _ := m["job_id"].(string)
	if code != http.StatusAccepted || jobID == "" {
		t.Fatalf("enqueue: %d %s", code, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		code, body, m = h.do("zzc", "GET", "/api/console/hosted/jobs/"+jobID, "")
		if m["stage"] == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not fail: %d %s", code, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code != http.StatusOK || m["reason"] != "node_offline" {
		t.Fatalf("job result: %d %s", code, body)
	}
	if seat, _ := st.GetSeat(context.Background(), id); seat.State != "online" {
		t.Fatalf("seat state must not change: %+v", seat)
	}
}
