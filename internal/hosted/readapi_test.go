package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// T-G4-35 返修（台账 lead-g4-t35-review、lead-g4-joint-run-1 第 1 条）：options 读模型按契约第一节【序列化后】核对。
// 原用例只断言 Go 字段，quota 少一层 remaining 时页面读到 undefined 而用例照绿 —— 这里一律经 json.Marshal 再比。

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %#v", v)
	}
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func optionsJSON(t *testing.T, o *Orchestrator, owner string) (string, map[string]any) {
	t.Helper()
	v, err := o.Options(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return string(raw), m
}

// 契约第一节逐字段：五个顶层键；quota 只有 remaining 一层；各列表元素的键集合与契约相同（不多不少）。
func TestOptionsJSONMatchesContract(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	o.combos = fakeCombos{m: map[string]bool{"codex|gpt|token_file": true, "claude|glm|api_key": false}}
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"cpu_total":16,"mem_free_mb":15360,"disk_free_mb":102400}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeState(ctx, "nd_full", "offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.pool.Register(ctx, "admin", true, "team-glm", "api_key", "claude", "glm", "sk-abcd9z8y"); err != nil {
		t.Fatal(err)
	}
	lst, _ := o.pool.ListAdmin(ctx, "admin", true)
	for _, a := range lst {
		if a.Name == "team-glm" {
			if err := o.pool.Grant(ctx, "admin", true, a.ID, "zzc"); err != nil {
				t.Fatal(err)
			}
		}
	}

	raw, m := optionsJSON(t, o, "zzc")
	if got, want := keysOf(t, m), []string{"accounts", "combos", "models", "nodes", "plans", "quota"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level keys %v, want %v", got, want)
	}
	// quota：契约原文 quota: {remaining:{agents, vcpu, mem_gb, disk_gb}}；配额 2/4/8G/1T（R-G4-DISK-TIER 后磁盘档定 8192），已占一席 2C/4G/8G。
	if !strings.Contains(raw, `"quota":{"remaining":{"agents":1,"vcpu":2,"mem_gb":4,"disk_gb":1016}}`) {
		t.Fatalf("quota not in contract shape: %s", raw)
	}
	elemKeys := map[string][]string{
		"nodes":    {"free_disk_gb", "free_mem_gb", "free_vcpu", "id", "name", "online", "pool"},
		"combos":   {"cli", "cred_kind", "llm", "verified"},
		"accounts": {"id", "kind", "last4", "llm", "name"},
		"plans":    {"fits", "id", "mem_gb", "name", "vcpu"},
	}
	for list, want := range elemKeys {
		items := m[list].([]any)
		if len(items) == 0 {
			t.Fatalf("%s empty in fixture; cannot check element shape: %s", list, raw)
		}
		for _, it := range items {
			if got := keysOf(t, it); !reflect.DeepEqual(got, want) {
				t.Errorf("%s element keys %v, want %v", list, got, want)
			}
		}
	}
	// 离线节点下发 online=false（原用例两枚都在线，这一格存活）。
	online := map[string]bool{}
	for _, n := range m["nodes"].([]any) {
		nm := n.(map[string]any)
		online[nm["id"].(string)] = nm["online"].(bool)
	}
	if online["nd_full"] || !online["nd_free"] {
		t.Errorf("node online flags %v, want nd_full=false nd_free=true", online)
	}
	// 未验通组合下发 verified=false（原用例只有一条且验通，这一格存活）。
	verified := map[string]bool{}
	for _, c := range m["combos"].([]any) {
		cm := c.(map[string]any)
		verified[cm["cli"].(string)+"|"+cm["llm"].(string)] = cm["verified"].(bool)
	}
	if !verified["codex|gpt"] || verified["claude|glm"] {
		t.Errorf("combo verified flags %v, want codex|gpt=true claude|glm=false", verified)
	}
	// 账号永不带密钥；api_key 类出后四位。
	if strings.Contains(raw, "sk-abcd9z8y") || !strings.Contains(raw, `"last4":"9z8y"`) {
		t.Errorf("account secret leaked or last4 missing: %s", raw)
	}
}

// 空列表序列化为 []，不是 null。
func TestOptionsEmptyListsAreArrays(t *testing.T) {
	svc, s := mkPoolSvc(t)
	o := NewOrchestrator(s, svc, &fakeIdentity{}, &fakeGateway{}, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	raw, _ := optionsJSON(t, o, "nobody")
	for _, k := range []string{"nodes", "combos", "accounts"} {
		if !strings.Contains(raw, `"`+k+`":[]`) {
			t.Errorf("%s must serialize as [] when empty: %s", k, raw)
		}
	}
	if strings.Contains(raw, "null") {
		t.Errorf("no null allowed in options: %s", raw)
	}
}

// fits 的四道闸各自独立起作用（原用例里 4c8g 置灰其实是内存闸抓的，agents 闸与 vcpu 闸被遮住）。
func TestOptionsPlanFitsEachGate(t *testing.T) {
	ctx := context.Background()
	fitsOf := func(t *testing.T, q Quota, seats []string) map[string]bool {
		t.Helper()
		o, idp, _, s := mkOrch(t)
		if err := s.PutQuota(ctx, q, "admin"); err != nil {
			t.Fatal(err)
		}
		for _, id := range seats {
			if err := s.CreateSeat(ctx, Seat{AgentID: id, NodeID: "nd_free", CLI: "codex", LLM: "gpt",
				CredKind: "token_file", Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
				t.Fatal(err)
			}
			idp.agentIDs = append(idp.agentIDs, id)
		}
		v, err := o.Options(ctx, "zzc")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range v.Plans {
			out[p.ID] = p.Fits
		}
		return out
	}
	t.Run("agents left 0 greys both plans", func(t *testing.T) {
		// vCPU、内存、磁盘都宽裕，只差 Agent 数。
		got := fitsOf(t, Quota{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 64, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, []string{"ag_a"})
		if got["2c4g"] || got["4c8g"] {
			t.Errorf("fits %v, want both false when no agent slot left", got)
		}
	})
	t.Run("memory short but vcpu ample", func(t *testing.T) {
		// 剩 6G 内存、vCPU 64：2C/4G 能放，4C/8G 不能 —— 只能是内存闸在起作用（tosonnet50ts 复验存活刀）。
		got := fitsOf(t, Quota{UserLC: "zzc", AgentsLimit: 8, VCPULimit: 64, MemMBLimit: 6144, DiskMBLimit: 1 << 20}, nil)
		if !got["2c4g"] || got["4c8g"] {
			t.Errorf("fits %v, want 2c4g=true 4c8g=false (memory gate)", got)
		}
	})
	t.Run("disk short but vcpu and memory ample", func(t *testing.T) {
		// R-G4-DISK-TIER 后两档磁盘是真档位（8192／16384），磁盘闸直接用真档位证伪（原临时覆盖撤除）。
		// 剩 15G 磁盘、vCPU 与内存都宽裕：8G 能放，16G 不能 —— 只能是磁盘闸在起作用。
		got := fitsOf(t, Quota{UserLC: "zzc", AgentsLimit: 8, VCPULimit: 64, MemMBLimit: 1 << 20, DiskMBLimit: 15360}, nil)
		if !got["2c4g"] || got["4c8g"] {
			t.Errorf("fits %v, want 2c4g=true 4c8g=false (disk gate)", got)
		}
	})
	t.Run("vcpu short but memory ample", func(t *testing.T) {
		// 剩 3 个 vCPU、内存 1TB：2C 能放，4C 不能 —— 只能是 vCPU 闸在起作用。
		got := fitsOf(t, Quota{UserLC: "zzc", AgentsLimit: 8, VCPULimit: 3, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, nil)
		if !got["2c4g"] || got["4c8g"] {
			t.Errorf("fits %v, want 2c4g=true 4c8g=false (vcpu gate)", got)
		}
	})
}

// jobs.reason 只出契约第四节「进度」的七个机器码；状态机里记的其他东西一律收成 start_failed。
func TestJobReasonOnlyContractCodes(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	cases := map[string]string{
		"queue_timeout": "queue_timeout", "pull_timeout": "pull_timeout", "start_timeout": "start_timeout",
		"image_pull_failed": "image_pull_failed", "start_failed": "start_failed",
		"credential_inject_failed": "credential_inject_failed", "node_offline": "node_offline",
		"":                         "start_failed",
		"backup_failed: disk full": "start_failed",
		"hosted: invalid state":    "start_failed",
		"upgrade":                  "start_failed",
	}
	i := 0
	for detail, want := range cases {
		i++
		id := "ag_r" + string(rune('a'+i))
		if err := s.CreateSeat(ctx, Seat{AgentID: id, NodeID: "nd_free", CLI: "codex", LLM: "gpt",
			CredKind: "token_file", Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSeatState(ctx, id, "error", detail, "fail", nil); err != nil {
			t.Fatal(err)
		}
		jv, err := o.JobProgress(ctx, id)
		if err != nil || jv.Stage != "failed" || jv.Reason != want || jv.AgentID != "" {
			t.Errorf("detail %q: %+v %v, want failed/%s without agent_id", detail, jv, err, want)
		}
	}
}

// 组长裁定 lead-g4-joint-run-1 第 2 条：jobs 归属按席位 created_by——创建失败后 Agent 已 Retire（身份侧不再列出它），
// 业主仍须读到 {stage:failed, reason}；别人与不存在同答 not_owner。
func TestJobProgressForOwnerBySeat(t *testing.T) {
	o, idp, gw, _ := mkOrch(t)
	ctx := context.Background()
	gw.failOn = "pull_image"
	out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	o.RunJob(ctx, out.AgentID)
	if len(idp.retired) != 1 || idp.retired[0] != out.AgentID {
		t.Fatalf("fixture: failed job must have retired its agent: %v", idp.retired)
	}
	jv, err := o.JobProgressFor(ctx, out.JobID, "zzc")
	if err != nil || jv.Stage != "failed" || jv.Reason != "image_pull_failed" || jv.AgentID != "" {
		t.Fatalf("owner reads own failed job: %+v %v", jv, err)
	}
	for _, c := range []struct{ job, actor string }{{out.JobID, "eve"}, {out.JobID, ""}, {"ag_nosuch", "zzc"}} {
		if _, err := o.JobProgressFor(ctx, c.job, c.actor); !errors.Is(err, ErrNotOwner) {
			t.Errorf("job %s as %q: %v, want not_owner", c.job, c.actor, err)
		}
	}
}

// 包 B2：options.models＝在线节点上报清单的并集；节点下线即从并集消失（修订 4）。
func TestOptionsModelsUnionOnlineOnly(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":16384,"models":["gpt-6.1-sol","gpt-5.5"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, "nd_full", json.RawMessage(`{"mem_free_mb":1024,"models":["gpt-4o"]}`)); err != nil {
		t.Fatal(err)
	}
	_, m := optionsJSON(t, o, "zzc")
	models := m["models"].([]any)
	if len(models) != 3 {
		t.Fatalf("两在线节点并集应 3 个: %v", models)
	}
	// 下线 nd_full ⇒ gpt-4o 消失
	if err := s.SetNodeState(ctx, "nd_full", "offline"); err != nil {
		t.Fatal(err)
	}
	_, m2 := optionsJSON(t, o, "zzc")
	models2 := m2["models"].([]any)
	for _, mm := range models2 {
		if mm == "gpt-4o" {
			t.Fatalf("下线节点的模型应消失: %v", models2)
		}
	}
	if len(models2) != 2 {
		t.Fatalf("并集应剩 2: %v", models2)
	}
}
