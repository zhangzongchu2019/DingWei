package main

// 0.2.1.101 账号包 HTTP 面（R-V2-ACCOUNT-PER-USER）：P1／P2／P3'／P5／P6／P7／P12／P13、管理 A1–A4、刷新凭据路由。
// 编号对方案件 PLAN-R-V2-ACCOUNT-PER-USER-opus50tc2k.md。seed-account（已删）原测的「应答不回显、库里是密文、
// 坏输入不落库」三性质迁到这里：P1（不回显＋密文）、P7（坏输入不落库）、P12（全链路字符串扫描）。

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

type acctRig struct {
	t    *testing.T
	mux  *http.ServeMux
	st   *hosted.Store
	gw   *fakeGW
	pool *hosted.PoolService
}

func mkAcctRig(t *testing.T) *acctRig {
	t.Helper()
	deps, st, _, gw, pool := mkHostedDeps(t)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	return &acctRig{t: t, mux: mux, st: st, gw: gw, pool: pool}
}

// req origin: "" ⇒ 同源（testRouteOrigin）；"-" ⇒ 不带 Origin；其余原样。
func (r *acctRig) req(actor, method, path, body, origin string) (int, string) {
	r.t.Helper()
	q := httptest.NewRequest(method, path, strings.NewReader(body))
	q.Header.Set("X-Test-Actor", actor)
	switch origin {
	case "":
		q.Header.Set("Origin", testRouteOrigin)
	case "-":
	default:
		q.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, q)
	return rec.Code, rec.Body.String()
}

func (r *acctRig) register(actor, name, kind, secret string) (int, map[string]any) {
	r.t.Helper()
	b, _ := json.Marshal(map[string]string{"name": name, "kind": kind, "cli": "codex", "llm": "gpt", "secret": secret})
	code, body := r.req(actor, "POST", "/api/console/llm-accounts", string(b), "")
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return code, m
}

func (r *acctRig) list(actor, path string) []map[string]any {
	r.t.Helper()
	code, body := r.req(actor, "GET", path, "", "")
	if code != 200 {
		r.t.Fatalf("GET %s as %s: %d %s", path, actor, code, body)
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *acctRig) dbHits(needle string) int {
	r.t.Helper()
	n, err := r.st.SecretPlaintextHits(context.Background(), needle)
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

const tokSecret = `{"access_token":"tok-ZZ-PLAINTEXT-MARKER-1"}`

// P1＋P12：登记 ⇒ 201、归属调用者；应答、库、审计、日志都不出现密钥原文（换密同样）。
func TestAccountRegisterNoEchoAndEncrypted(t *testing.T) {
	r := mkAcctRig(t)
	var logBuf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prevOut)

	b, _ := json.Marshal(map[string]string{"name": "mine", "kind": "token_file", "cli": "codex", "llm": "gpt", "secret": tokSecret})
	code, body := r.req("bob", "POST", "/api/console/llm-accounts", string(b), "")
	if code != http.StatusCreated || strings.Contains(body, "PLAINTEXT-MARKER") {
		t.Fatalf("登记: %d %s", code, body)
	}
	var a map[string]any
	_ = json.Unmarshal([]byte(body), &a)
	id, _ := a["id"].(string)
	acct, err := r.st.GetAccount(context.Background(), id)
	if err != nil || acct.OwnerLC != "bob" {
		t.Fatalf("归属应为调用者 bob: %+v %v", acct, err)
	}
	// 换密（api_key 后四位只给属主）
	b2, _ := json.Marshal(map[string]string{"secret": `{"access_token":"tok-ZZ-PLAINTEXT-MARKER-2"}`})
	code, body = r.req("bob", "PUT", "/api/console/llm-accounts/"+id+"/secret", string(b2), "")
	if code != 200 || strings.Contains(body, "PLAINTEXT-MARKER") || !strings.Contains(body, `"in_use":0`) {
		t.Fatalf("换密: %d %s", code, body)
	}
	for _, needle := range []string{"PLAINTEXT-MARKER-1", "PLAINTEXT-MARKER-2"} {
		if n := r.dbHits(needle); n != 0 {
			t.Fatalf("库内出现明文 %s ×%d", needle, n)
		}
		if strings.Contains(logBuf.String(), needle) {
			t.Fatalf("日志出现明文 %s", needle)
		}
	}
	for _, it := range r.list("bob", "/api/console/llm-accounts") {
		if raw, _ := json.Marshal(it); bytes.Contains(raw, []byte("PLAINTEXT")) {
			t.Fatalf("列表出现明文: %s", raw)
		}
	}
}

// P2＋P3'：越权矩阵——bob 对 zzc 的账号：列表不可见、改密／停用／启用 404、建席引用 account_not_allowed、刷新 zzc 席位 not_owner；库不变。
func TestAccountCrossUserMatrix(t *testing.T) {
	r := mkAcctRig(t)
	ctx := context.Background()
	zzc, err := r.st.ListUsableAccounts(ctx, "zzc", "", "") // 装配时属主 zzc 已登记一枚
	if err != nil || len(zzc) != 1 {
		t.Fatalf("zzc 账号: %+v %v", zzc, err)
	}
	id := zzc[0].ID
	for _, it := range r.list("bob", "/api/console/llm-accounts") {
		if it["id"] == id {
			t.Fatal("他人账号不得出现在 bob 的列表")
		}
	}
	code, body := r.req("bob", "GET", "/api/console/hosted/options", "", "")
	if code != 200 || strings.Contains(body, id) {
		t.Fatalf("bob 的 options 不得含 zzc 账号: %d %s", code, body)
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/console/llm-accounts/" + id + "/secret", `{"secret":"{\"a\":1}"}`},
		{"POST", "/api/console/llm-accounts/" + id + "/disable", ""},
		{"POST", "/api/console/llm-accounts/" + id + "/enable", ""},
		{"PUT", "/api/console/llm-accounts/acct_nosuch/secret", `{"secret":"{\"a\":1}"}`}, // 不存在同码
	} {
		code, body := r.req("bob", c.method, c.path, c.body, "")
		if code != http.StatusNotFound || !strings.Contains(body, `"not_found"`) {
			t.Fatalf("%s %s as bob: %d %s, want 404 not_found", c.method, c.path, code, body)
		}
	}
	code, body = r.req("bob", "POST", "/api/console/hosted/agents",
		`{"name":"steal","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+id+`","plan_id":"2c4g"}`, "")
	if code != http.StatusBadRequest || !strings.Contains(body, "account_not_allowed") {
		t.Fatalf("bob 用 zzc 账号建席: %d %s", code, body)
	}
	code, body = r.req("bob", "POST", "/api/console/hosted/agents/ag_any/refresh_cred", "", "")
	if code != http.StatusForbidden || !strings.Contains(body, "not_owner") {
		t.Fatalf("bob 刷新他人席位: %d %s", code, body)
	}
	if a, _ := r.st.GetAccount(ctx, id); a.State == "disabled" || a.OwnerLC != "zzc" {
		t.Fatalf("库不得被改: %+v", a)
	}
}

// P5＋P13（HTTP）：同用户重名 409 account_name_taken；他人同名可建；第 21 个 409 account_limit。
func TestAccountNameAndLimitOverHTTP(t *testing.T) {
	r := mkAcctRig(t)
	if code, _ := r.register("bob", "team-codex", "token_file", `{"a":1}`); code != http.StatusCreated { // zzc 已有同名
		t.Fatalf("他人同名应可建: %d", code)
	}
	if code, m := r.register("bob", "team-codex", "token_file", `{"a":1}`); code != http.StatusConflict || m["error"] != "account_name_taken" {
		t.Fatalf("本人重名: %d %v", code, m)
	}
	for i := 1; i < hosted.MaxAccountsPerOwner; i++ {
		if code, m := r.register("bob", "k"+string(rune('A'+i)), "token_file", `{"a":1}`); code != http.StatusCreated {
			t.Fatalf("第 %d 个: %d %v", i+1, code, m)
		}
	}
	if code, m := r.register("bob", "overflow", "token_file", `{"a":1}`); code != http.StatusConflict || m["error"] != "account_limit" {
		t.Fatalf("第 21 个: %d %v", code, m)
	}
}

// P6：写接口 Origin 守卫——无／错 Origin ⇒ 403 origin_mismatch、零副作用（建席、生命周期含 refresh_cred、配额 PUT、账号各写口）。
func TestHostedWriteRoutesRequireOrigin(t *testing.T) {
	r := mkAcctRig(t)
	ctx := context.Background()
	acct := r.list("zzc", "/api/console/llm-accounts")[0]["id"].(string)
	writes := []struct{ actor, method, path, body string }{
		{"zzc", "POST", "/api/console/hosted/agents", `{"name":"o1","node_id":"auto","cli":"codex","llm":"gpt","account_id":"` + acct + `","plan_id":"2c4g"}`},
		{"zzc", "POST", "/api/console/hosted/agents/ag_x/refresh_cred", ""},
		{"zzc", "POST", "/api/console/hosted/agents/ag_x/restart", ""},
		{"admin", "PUT", "/api/console/admin/quotas/zzc", `{"agents_limit":9,"vcpu_limit":9,"mem_gb_limit":9,"disk_gb_limit":9}`},
		{"zzc", "POST", "/api/console/llm-accounts", `{"name":"o2","kind":"token_file","cli":"codex","llm":"gpt","secret":"{}"}`},
		{"zzc", "PUT", "/api/console/llm-accounts/" + acct + "/secret", `{"secret":"{}"}`},
		{"zzc", "POST", "/api/console/llm-accounts/" + acct + "/disable", ""},
		{"admin", "POST", "/api/console/admin/llm-accounts/" + acct + "/disable", ""},
	}
	for _, w := range writes {
		for _, origin := range []string{"-", "https://evil.test"} {
			code, body := r.req(w.actor, w.method, w.path, w.body, origin)
			if code != http.StatusForbidden || !strings.Contains(body, "origin_mismatch") {
				t.Fatalf("%s %s Origin=%q: %d %s, want 403 origin_mismatch", w.method, w.path, origin, code, body)
			}
		}
	}
	if ts := r.gw.types(); len(ts) != 0 {
		t.Fatalf("Origin 拒绝不得发节点指令: %v", ts)
	}
	if l, _ := r.st.ListOwnedAccounts(ctx, "zzc"); len(l) != 1 || l[0].State == "disabled" {
		t.Fatalf("库不得被改: %+v", l)
	}
	if q, _ := r.st.GetQuota(ctx, "zzc"); q.AgentsLimit == 9 {
		t.Fatal("配额不得被改")
	}
}

// P7：联动表外 ⇒ 400 combo_incompatible；token_file 非 JSON ⇒ 400 params_invalid；超限 ⇒ 413 too_large；均不落库、不回显。
func TestAccountRegisterValidationOverHTTP(t *testing.T) {
	r := mkAcctRig(t)
	const mark = "BAD-INPUT-MARKER"
	cases := []struct {
		body    string
		code    int
		errCode string
	}{
		{`{"name":"x1","kind":"api_key","cli":"claude","llm":"gpt","secret":"` + mark + `"}`, 400, "combo_incompatible"},
		{`{"name":"x2","kind":"token_file","cli":"codex","llm":"gpt","secret":"not json ` + mark + `"}`, 400, "params_invalid"},
		{`{"name":"x3","kind":"token_file","cli":"codex","llm":"gpt","secret":"{\"x\":\"` + strings.Repeat("a", hosted.MaxSecretBytes+8192) + `\"}"}`, 413, "too_large"},
	}
	for _, c := range cases {
		code, body := r.req("bob", "POST", "/api/console/llm-accounts", c.body, "")
		if code != c.code || !strings.Contains(body, c.errCode) || strings.Contains(body, mark) {
			t.Fatalf("want %d %s, got %d %.200s", c.code, c.errCode, code, body)
		}
	}
	if l := r.list("bob", "/api/console/llm-accounts"); len(l) != 0 {
		t.Fatalf("坏输入不得落库: %+v", l)
	}
	if n := r.dbHits(mark); n != 0 {
		t.Fatalf("坏输入明文进库 ×%d", n)
	}
}

// A1–A4：管理列表含他人元数据（属主、在用数、状态）、不含 last4 与任何密钥字段；无管理换密路由；
// 管理停用受引用守卫；普通用户调管理路由 403 forbidden_role。
func TestAdminAccountRoutes(t *testing.T) {
	r := mkAcctRig(t)
	const adminViewSecret = "sk-adminview-9876"
	// api_key 不在本装配的联动表里——直接走服务层以属主 bob 登记（HTTP 登记的联动校验另有 P7）。
	// 合法元数据也可含 9876（这里固定在名称里，账号 ID 仍可随机），不能扫描裸后四位判泄漏。
	if _, err := r.pool.RegisterOwned(context.Background(), "bob", "bob-api-9876", "api_key", "codex", "gpt", adminViewSecret); err != nil {
		t.Fatalf("seed: %v", err)
	}
	lst := r.list("admin", "/api/console/admin/llm-accounts")
	if len(lst) != 2 {
		t.Fatalf("管理列表应含全部 2 个: %+v", lst)
	}
	for _, it := range lst {
		raw, _ := json.Marshal(it)
		if _, has := it["last4"]; has || bytes.Contains(raw, []byte(adminViewSecret)) || bytes.Contains(raw, []byte("sk-")) {
			t.Fatalf("管理列表不得含 last4／密钥片段: %s", raw)
		}
		if it["owner"] == nil || it["state"] == nil || it["in_use"] == nil {
			t.Fatalf("管理列表缺元数据: %s", raw)
		}
	}
	// A1'：属主本人列表仍有 api_key 后四位
	own := r.list("bob", "/api/console/llm-accounts")
	if own[0]["last4"] != "9876" {
		t.Fatalf("属主本人应见后四位: %+v", own)
	}
	// A2：无管理换密路由
	if code, _ := r.req("admin", "PUT", "/api/console/admin/llm-accounts/"+own[0]["id"].(string)+"/secret", `{"secret":"x"}`, ""); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("管理换密路由不得存在: %d", code)
	}
	// A3：管理停用——zzc 的号被席位引用时拒；bob 的号无引用可停可启
	zzcID := ""
	for _, it := range lst {
		if it["owner"] == "zzc" {
			zzcID = it["id"].(string)
		}
	}
	if code, body := r.req("zzc", "POST", "/api/console/hosted/agents", `{"name":"a3","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+zzcID+`","plan_id":"2c4g"}`, ""); code != http.StatusAccepted {
		t.Fatalf("seed seat: %d %s", code, body)
	}
	if code, body := r.req("admin", "POST", "/api/console/admin/llm-accounts/"+zzcID+"/disable", "", ""); code != http.StatusConflict || !strings.Contains(body, "account_in_use") {
		t.Fatalf("管理停用在用账号: %d %s", code, body)
	}
	bobID := own[0]["id"].(string)
	for _, op := range []string{"disable", "enable"} {
		if code, body := r.req("admin", "POST", "/api/console/admin/llm-accounts/"+bobID+"/"+op, "", ""); code != http.StatusNoContent {
			t.Fatalf("管理 %s: %d %s", op, code, body)
		}
	}
	// A4：普通用户调管理路由 ⇒ 403 forbidden_role
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/console/admin/llm-accounts"},
		{"POST", "/api/console/admin/llm-accounts/" + bobID + "/disable"},
	} {
		if code, body := r.req("bob", c.method, c.path, "", ""); code != http.StatusForbidden || !strings.Contains(body, "forbidden_role") {
			t.Fatalf("%s %s as bob: %d %s", c.method, c.path, code, body)
		}
	}
}

// 刷新凭据路由：属主 202＋job_id，节点收到 refresh_cred_seat（参数里恰一次明文）；未知 op 仍 404。
func TestRefreshCredRoute(t *testing.T) {
	r := mkAcctRig(t)
	acct := r.list("zzc", "/api/console/llm-accounts")[0]["id"].(string)
	code, body := r.req("zzc", "POST", "/api/console/hosted/agents",
		`{"name":"rf1","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+acct+`","plan_id":"2c4g"}`, "")
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	agentID, _ := m["job_id"].(string) // 新建 job_id＝agent_id（编排层口径）
	waitSeatState(t, r.st, agentID, "online")
	code, body = r.req("zzc", "POST", "/api/console/hosted/agents/"+agentID+"/refresh_cred", "", "")
	if code != http.StatusAccepted || !strings.Contains(body, "job_id") {
		t.Fatalf("refresh: %d %s", code, body)
	}
	waitNodeCmd(t, r.gw, "refresh_cred_seat")
	if code, _ := r.req("zzc", "POST", "/api/console/hosted/agents/"+agentID+"/bogus_op", "", ""); code != http.StatusNotFound {
		t.Fatalf("未知 op 应 404: %d", code)
	}
}

func waitSeatState(t *testing.T, st *hosted.Store, agentID, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		seat, err := st.GetSeat(context.Background(), agentID)
		if err == nil && seat.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("seat %s state=%s err=%v, want %s", agentID, seat.State, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitNodeCmd(t *testing.T, gw *fakeGW, typ string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, x := range gw.types() {
			if x == typ {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("node command %s never sent: %v", typ, gw.types())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
