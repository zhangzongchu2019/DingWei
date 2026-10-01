package main

// T-G4-34 · V6 集成测试套件（组长令 bd773565a0be：本席单独写；真机部分 prodops 代跑 106、
// tosonnet55tc2k 判读）。逐条挂判据编号 B1–B9、B11；B10 业主亲验不占代码；
// B12 干净容器验通＝real-machine 脚本（scripts/ 下，prodops 代跑），不在本文件。
//
// 驱动方式：全部经 console 路由真 HTTP 入口（hosted_routes.go）＋pgtest 真库＋假身份/假网关；
// 故障注入走 fakeGW.failOn；反向对照＝判据若被删则对应格必红（断言写的就是判据本身）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

type integRig struct {
	deps hostedDeps
	st   *hosted.Store
	idp  *fakeIDP
	gw   *fakeGW
	mux  *http.ServeMux
	pool *hosted.PoolService
}

func mkRig(t *testing.T) *integRig {
	t.Helper()
	deps, st, idp, gw, poolSvc := mkHostedDeps(t)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	return &integRig{deps: deps, st: st, idp: idp, gw: gw, mux: mux, pool: poolSvc}
}

func (r *integRig) call(actor, method, path, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Test-Actor", actor)
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	var v map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	return rec.Code, v
}

// createAndDrive 创建并等待执行链走完（路由异步起 RunJob；假网关全成 ⇒ online）。
func (r *integRig) createAndDrive(t *testing.T, name, acctID string) string {
	t.Helper()
	code, v := r.call("zzc", "POST", "/api/console/hosted/agents",
		fmt.Sprintf(`{"name":%q,"node_id":"auto","cli":"codex","llm":"gpt","account_id":%q,"plan_id":"2c4g"}`, name, acctID))
	if code != 202 {
		t.Fatalf("create %s: %d %v", name, code, v)
	}
	jobID := v["job_id"].(string)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, jv := r.call("zzc", "GET", "/api/console/hosted/jobs/"+jobID, "")
		if jv["stage"] == "online" || jv["stage"] == "failed" {
			return jobID
		}
		if time.Now().After(deadline) {
			t.Fatalf("执行链超时: %v", jv)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *integRig) usableAcct(t *testing.T) string {
	t.Helper()
	accts, err := r.st.ListUsableAccounts(context.Background(), "zzc", "", "")
	if err != nil || len(accts) == 0 {
		t.Fatalf("无可用户账号: %v", err)
	}
	return accts[0].ID
}

// B1：蒙版阶段真实（queued→online 沿状态机读面）；online 后 Agent 在业主清单（idp 名下）且席位 online。
func TestB1StagesRealAndAgentListed(t *testing.T) {
	r := mkRig(t)
	acctID := r.usableAcct(t)
	jobID := r.createAndDrive(t, "b1w", acctID)
	_, jv := r.call("zzc", "GET", "/api/console/hosted/jobs/"+jobID, "")
	if jv["stage"] != "online" || jv["agent_id"] != jobID {
		t.Fatalf("B1 online 格: %v", jv)
	}
	seat, _ := r.st.GetSeat(context.Background(), jobID)
	if seat.State != "online" {
		t.Fatalf("B1 席位应 online: %+v", seat)
	}
	ids, _ := r.idp.ListHostedAgentIDsByOwner(context.Background(), "zzc")
	found := false
	for _, id := range ids {
		if id == jobID {
			found = true
		}
	}
	if !found {
		t.Fatalf("B1 业主清单应含该 Agent: %v", ids)
	}
}

// B2：全程无人登录节点——节点动作全部来自 Hub 经网关下发的指令，且序列恰为 pull→create。
func TestB2AllNodeActionsFromHub(t *testing.T) {
	r := mkRig(t)
	r.createAndDrive(t, "b2w", r.usableAcct(t))
	if len(r.gw.sent) != 2 || r.gw.sent[0].Type != "pull_image" || r.gw.sent[1].Type != "create_seat" {
		t.Fatalf("B2 指令序列: %+v", r.gw.sent)
	}
}

// B3：拉镜像失败 ⇒ 蒙版 failed/image_pull_failed；Retire 吊销（配额归还）；账号回池；无后续指令。
func TestB3PullFailureCleanup(t *testing.T) {
	r := mkRig(t)
	r.gw.failOn = "pull_image"
	acctID := r.usableAcct(t)
	jobID := r.createAndDrive(t, "b3w", acctID)
	_, jv := r.call("zzc", "GET", "/api/console/hosted/jobs/"+jobID, "")
	if jv["stage"] != "failed" || jv["reason"] != "image_pull_failed" {
		t.Fatalf("B3 蒙版: %v", jv)
	}
	if !r.idp.retired[jobID] {
		t.Fatal("B3 必须 Retire 吊销（配额归还与释放名字的同一动作）")
	}
	ids, _ := r.idp.ListHostedAgentIDsByOwner(context.Background(), "zzc")
	for _, id := range ids {
		if id == jobID {
			t.Fatalf("B3 吊销后不得在名下（配额统计源）: %v", ids)
		}
	}
	a, _ := r.st.GetAccount(context.Background(), acctID)
	if a.State != "available" || a.BoundAgentID.Valid {
		t.Fatalf("B3 账号应回池: %+v", a)
	}
	for _, c := range r.gw.sent {
		if c.Type == "create_seat" {
			t.Fatalf("B3 拉镜像失败后不得起容器: %+v", r.gw.sent)
		}
	}
	// 配额归还的可观察面：options 剩余回到满格
	_, ov := r.call("zzc", "GET", "/api/console/hosted/options", "")
	quota := ov["quota"].(map[string]any)
	if quota["agents"].(float64) != 2 {
		t.Fatalf("B3 配额应归还: %v", quota)
	}
}

// B4/B5：联动与套餐置灰由 options 驱动＋服务端兜底（不兼容组合与超额创建被拒）。
func TestB4B5ComboAndPlanGuards(t *testing.T) {
	r := mkRig(t)
	acctID := r.usableAcct(t)
	// B4：不兼容组合（不在验通表）⇒ combo_incompatible
	code, v := r.call("zzc", "POST", "/api/console/hosted/agents",
		fmt.Sprintf(`{"name":"b4w","node_id":"auto","cli":"claude","llm":"gpt","account_id":%q,"plan_id":"2c4g"}`, acctID))
	if code != 400 || v["error"] != "combo_incompatible" {
		t.Fatalf("B4: %d %v", code, v)
	}
	// B5：套餐超剩余额度——占满两席后第三席 quota_exceeded 且带 remaining
	r.createAndDrive(t, "b5a", acctID)
	// 第二枚账号给第二席
	a2, err := r.pool.Register(context.Background(), "admin", true, "team-codex-b5", "token_file", "codex", "gpt", "sk-y")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.pool.Grant(context.Background(), "admin", true, a2.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	r.createAndDrive(t, "b5b", a2.ID)
	// 第三席：agents 上限 2 ⇒ quota_exceeded 带 remaining.agents=0
	code, v = r.call("zzc", "POST", "/api/console/hosted/agents",
		fmt.Sprintf(`{"name":"b5c","node_id":"auto","cli":"codex","llm":"gpt","account_id":%q,"plan_id":"2c4g"}`, a2.ID))
	if code != 409 || v["error"] != "quota_exceeded" {
		t.Fatalf("B5: %d %v", code, v)
	}
	rem := v["remaining"].(map[string]any)
	if rem["agents"].(float64) != 0 {
		t.Fatalf("B5 remaining: %v", rem)
	}
	// 且 options 里两档 fits 都该 false（agents 剩 0）
	_, ov := r.call("zzc", "GET", "/api/console/hosted/options", "")
	for _, p := range ov["plans"].([]any) {
		if p.(map[string]any)["fits"].(bool) {
			t.Fatalf("B5 满额后套餐应全置灰: %v", ov["plans"])
		}
	}
}

// B9：自动落位选有空量的节点；全满 ⇒ node_no_capacity 不进无限排队。
func TestB9AutoNodeCapacity(t *testing.T) {
	r := mkRig(t)
	// 把唯一节点打成满（余量 0）
	if err := r.st.SetNodeOnline(context.Background(), "nd_rt", json.RawMessage(`{"cpu_total":16,"mem_free_mb":0,"disk_free_mb":0}`)); err != nil {
		t.Fatal(err)
	}
	code, v := r.call("zzc", "POST", "/api/console/hosted/agents",
		fmt.Sprintf(`{"name":"b9w","node_id":"auto","cli":"codex","llm":"gpt","account_id":%q,"plan_id":"2c4g"}`, r.usableAcct(t)))
	if v["error"] != "node_no_capacity" {
		t.Fatalf("B9 全满应拒: %d %v", code, v)
	}
	if len(r.gw.sent) != 0 {
		t.Fatalf("B9 全满不得发任何节点指令: %+v", r.gw.sent)
	}
}

// B7：凭据暴露面——options/accounts 无 secret；库中无明文；create_seat 参数里凭据只在
// credential_file／token_file 两个键下（不经 env——节点侧 spec 闸另有 TestCreateSeatSecuritySpecAndTokenFile）。
func TestB7CredentialExposureSurface(t *testing.T) {
	r := mkRig(t)
	acctID := r.usableAcct(t)
	r.createAndDrive(t, "b7w", acctID)
	// ① options 应答不含密钥明文
	_, ov := r.call("zzc", "GET", "/api/console/hosted/options", "")
	if strings.Contains(fmt.Sprintf("%v", ov), "sk-x") {
		t.Fatalf("B7 options 不得含明文: %v", ov)
	}
	// ② 库中无明文（secret_enc 是密文）
	n, err := r.st.SecretPlaintextHits(context.Background(), "sk-x")
	if err != nil || n != 0 {
		t.Fatalf("B7 库中明文命中 %d err=%v", n, err)
	}
	// ③ 下发参数：明文凭据只出现在 credential_file.content；令牌只在 token_file
	for _, c := range r.gw.sent {
		raw := string(c.Params)
		if c.Type == "create_seat" {
			if !strings.Contains(raw, `"credential_file":{"content":"sk-x"}`) {
				t.Fatalf("B7 凭据件应在 credential_file: %s", raw)
			}
			if !strings.Contains(raw, `"token_file":{`) {
				t.Fatalf("B7 令牌件应在 token_file: %s", raw)
			}
		} else if strings.Contains(raw, "sk-x") {
			t.Fatalf("B7 非 create_seat 指令不得含凭据: %s", raw)
		}
	}
}

// B8：每次解密下发恰好一条审计（逐字段：操作人/动作/目标账号）。
func TestB8DecryptAuditExactlyOnce(t *testing.T) {
	r := mkRig(t)
	acctID := r.usableAcct(t)
	r.createAndDrive(t, "b8w", acctID)
	n, err := r.st.CountAudit(context.Background(), "account.decrypt_issue", acctID)
	if err != nil || n != 1 {
		t.Fatalf("B8 解密审计应恰好一条: n=%d err=%v", n, err)
	}
}

// B11：创建全链路无任何登录类指令——网关收到的指令类型白名单只有 pull_image/create_seat，
// 且参数里无 login/device-auth 字样（凭据件只由平台写入，丙1 教训）。
func TestB11NoLoginCommandInChain(t *testing.T) {
	r := mkRig(t)
	r.createAndDrive(t, "b11w", r.usableAcct(t))
	for _, c := range r.gw.sent {
		if c.Type != "pull_image" && c.Type != "create_seat" {
			t.Fatalf("B11 链路只允许 pull/create: %s", c.Type)
		}
		low := strings.ToLower(string(c.Params))
		for _, bad := range []string{"login", "device-auth", "device_auth", "auth login"} {
			if strings.Contains(low, bad) {
				t.Fatalf("B11 链路不得含登录类操作 %q: %s", bad, c.Params)
			}
		}
	}
}

// B6：名称无效 ⇒ name_invalid 服务端拒（本层 fakeIDP 与 identity.ValidAgentName 同形状校验；
// 钉的是「G3 拒绝透传为契约机器码」的路由行为——真校验的权威性在 G3 侧用例）。
func TestB6NameInvalidRejected(t *testing.T) {
	r := mkRig(t)
	code, v := r.call("zzc", "POST", "/api/console/hosted/agents",
		fmt.Sprintf(`{"name":"bad name!","node_id":"auto","cli":"codex","llm":"gpt","account_id":%q,"plan_id":"2c4g"}`, r.usableAcct(t)))
	if code != 400 || v["error"] != "name_invalid" {
		t.Fatalf("B6: %d %v", code, v)
	}
}
