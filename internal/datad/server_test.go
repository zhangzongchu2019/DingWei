package datad

// P 组核心用例(设计冻结版 §9,MemStore 载体;PG 集成=D4c 就绪后同套跑):
// P2 权限矩阵/P3 CAS/me 别名/P7 审计完整性(含未遂 403)/自报域白名单/乐观锁。

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer() (*Server, *MemStore) {
	ms := NewMemStore()
	s := New(Config{Store: ms, Tokens: map[string]Identity{
		"tok-coord": {Session: "zzc-coordinator-2642", Role: "coordinator"},
		"tok-devo":  {Session: "zzc-devo48-2642", Role: "member"},
		"tok-k3":    {Session: "zzc-claudek3-2642", Role: "member"},
	}})
	return s, ms
}

func call(t *testing.T, s *Server, method, path, token, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

const taskBody = `{"title":"T99","owner_session":"zzc-devo48-2642","status":"in_progress"}`

func TestP2WriteMatrix(t *testing.T) {
	s, ms := testServer()
	// 管理域:member 写=403;coordinator=200
	if w := call(t, s, "PUT", "/v1/tasks/T99", "tok-devo", taskBody, nil); w.Code != 403 {
		t.Fatalf("member put task want 403 got %d", w.Code)
	}
	if w := call(t, s, "PUT", "/v1/tasks/T99", "tok-coord", taskBody, nil); w.Code != 200 {
		t.Fatalf("coord put task got %d: %s", w.Code, w.Body.String())
	}
	if w := call(t, s, "POST", "/v1/dispatch", "tok-devo",
		`{"task_id":"T99","assignee":"x"}`, nil); w.Code != 403 {
		t.Fatalf("member dispatch want 403 got %d", w.Code)
	}
	// 无 token=401
	if w := call(t, s, "GET", "/v1/tasks", "", "", nil); w.Code != 401 {
		t.Fatalf("no token want 401 got %d", w.Code)
	}
	// P7:未遂 403 也入审计
	found := false
	for _, e := range ms.audit {
		if e.Op == "task.put.denied" && e.Actor == "zzc-devo48-2642" {
			found = true
		}
	}
	if !found {
		t.Fatal("未遂 403 未入审计(P7)")
	}
}

func TestSelfReportDomainRowLevel(t *testing.T) {
	s, _ := testServer()
	// 本人行=200;越行=403;me 别名=200
	if w := call(t, s, "PUT", "/v1/sessions/zzc-devo48-2642/state", "tok-devo",
		`{"state":"active"}`, nil); w.Code != 200 {
		t.Fatalf("self state got %d", w.Code)
	}
	if w := call(t, s, "PUT", "/v1/sessions/zzc-claudek3-2642/state", "tok-devo",
		`{"state":"dead"}`, nil); w.Code != 403 {
		t.Fatalf("越行写 want 403 got %d", w.Code)
	}
	if w := call(t, s, "PUT", "/v1/sessions/me/state", "tok-k3",
		`{"state":"idle"}`, nil); w.Code != 200 {
		t.Fatalf("me alias got %d", w.Code)
	}
	// coordinator 也不可代写他人状态(状态=自报语义)
	if w := call(t, s, "PUT", "/v1/sessions/zzc-devo48-2642/state", "tok-coord",
		`{"state":"dead"}`, nil); w.Code != 403 {
		t.Fatalf("coord 代写他人状态 want 403 got %d", w.Code)
	}
	// 心跳同规
	if w := call(t, s, "POST", "/v1/sessions/me/heartbeat", "tok-devo", `{"n":1}`, nil); w.Code != 202 {
		t.Fatalf("heartbeat got %d", w.Code)
	}
	if w := call(t, s, "POST", "/v1/sessions/zzc-claudek3-2642/heartbeat", "tok-devo", `{}`, nil); w.Code != 403 {
		t.Fatalf("越行心跳 want 403 got %d", w.Code)
	}
}

func TestProgressOwnerOnly(t *testing.T) {
	s, _ := testServer()
	call(t, s, "PUT", "/v1/tasks/T99", "tok-coord", taskBody, nil)
	// owner 本人报进度=200,anchor 更新
	if w := call(t, s, "POST", "/v1/tasks/T99/progress", "tok-devo",
		`{"anchor":"abc1234","progress":{"note":"段1毕"}}`, nil); w.Code != 200 {
		t.Fatalf("owner progress got %d: %s", w.Code, w.Body.String())
	}
	// 非 owner member=403
	if w := call(t, s, "POST", "/v1/tasks/T99/progress", "tok-k3",
		`{"anchor":"x"}`, nil); w.Code != 403 {
		t.Fatalf("非owner progress want 403 got %d", w.Code)
	}
	// progress 白名单:status 字段不在 schema=400(防越权改状态)
	if w := call(t, s, "POST", "/v1/tasks/T99/progress", "tok-devo",
		`{"status":"done"}`, nil); w.Code != 400 {
		t.Fatalf("progress 带 status want 400 got %d", w.Code)
	}
	var got Task
	_ = json.Unmarshal(call(t, s, "GET", "/v1/tasks/T99", "tok-devo", "", nil).Body.Bytes(), &got)
	if got.Anchor != "abc1234" || got.Status != "in_progress" {
		t.Fatalf("progress 落账错:%+v", got)
	}
}

func TestP3ConfigCAS(t *testing.T) {
	s, _ := testServer()
	if w := call(t, s, "PUT", "/v1/config/rules.review", "tok-coord", `{"rounds":3}`, nil); w.Code != 200 {
		t.Fatalf("首写 got %d", w.Code)
	}
	// 代次错=412
	if w := call(t, s, "PUT", "/v1/config/rules.review", "tok-coord", `{"rounds":4}`,
		map[string]string{"If-Match": "9"}); w.Code != 412 {
		t.Fatalf("CAS want 412 got %d", w.Code)
	}
	if w := call(t, s, "PUT", "/v1/config/rules.review", "tok-coord", `{"rounds":4}`,
		map[string]string{"If-Match": "1"}); w.Code != 200 {
		t.Fatalf("CAS 正确代次 got %d", w.Code)
	}
	// 历史代次可查
	w := call(t, s, "GET", "/v1/config/rules.review?generation=1", "tok-devo", "", nil)
	var e ConfigEntry
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.Generation != 1 || !strings.Contains(string(e.Value), "3") {
		t.Fatalf("历史代次:%+v", e)
	}
	// member 写 config=403
	if w := call(t, s, "PUT", "/v1/config/x", "tok-devo", `{}`, nil); w.Code != 403 {
		t.Fatalf("member config want 403 got %d", w.Code)
	}
	// GET /v1/config 列表(project_context 用):全员读,每 key 最新代次
	call(t, s, "PUT", "/v1/config/rules.dispatch", "tok-coord", `{"owner":"coordinator"}`, nil)
	wl := call(t, s, "GET", "/v1/config", "tok-devo", "", nil)
	var list []ConfigEntry
	_ = json.Unmarshal(wl.Body.Bytes(), &list)
	if wl.Code != 200 || len(list) != 2 { // rules.review(gen2)+rules.dispatch(gen1)
		t.Fatalf("list config: code=%d n=%d", wl.Code, len(list))
	}
	for _, e := range list {
		if e.Key == "rules.review" && e.Generation != 2 {
			t.Fatalf("list 应返最新代次,rules.review gen=%d", e.Generation)
		}
	}
}

func TestTaskOptimisticLock(t *testing.T) {
	s, _ := testServer()
	w := call(t, s, "PUT", "/v1/tasks/T88", "tok-coord", taskBody, nil)
	var t1 Task
	_ = json.Unmarshal(w.Body.Bytes(), &t1)
	// 不带 If-Match 改已存在件=409
	if w := call(t, s, "PUT", "/v1/tasks/T88", "tok-coord", taskBody, nil); w.Code != 409 {
		t.Fatalf("无 If-Match want 409 got %d", w.Code)
	}
	// 带过期 If-Match=409
	stale := t1.UpdatedAt.Add(-time.Second).Format(time.RFC3339Nano)
	if w := call(t, s, "PUT", "/v1/tasks/T88", "tok-coord", taskBody,
		map[string]string{"If-Match": stale}); w.Code != 409 {
		t.Fatalf("过期锁 want 409 got %d", w.Code)
	}
	if w := call(t, s, "PUT", "/v1/tasks/T88", "tok-coord", taskBody,
		map[string]string{"If-Match": t1.UpdatedAt.Format(time.RFC3339Nano)}); w.Code != 200 {
		t.Fatalf("正确锁 got %d: %s", w.Code, w.Body.String())
	}
}

func TestSkillFlowAndAuditRead(t *testing.T) {
	s, _ := testServer()
	// 提案=全员;status 强制 proposed(即使伪造 approved)
	if w := call(t, s, "POST", "/v1/skills", "tok-devo",
		`{"name":"grep-discipline","version":"1.0","status":"approved","content":"...","sha256":"aa"}`,
		nil); w.Code != 201 {
		t.Fatalf("propose got %d: %s", w.Code, w.Body.String())
	}
	var sk []Skill
	_ = json.Unmarshal(call(t, s, "GET", "/v1/skills", "tok-k3", "", nil).Body.Bytes(), &sk)
	if len(sk) != 1 || sk[0].Status != "proposed" {
		t.Fatalf("提案态未强制:%+v", sk)
	}
	// 批准=coordinator only
	if w := call(t, s, "POST", "/v1/skills/grep-discipline/1.0/approve", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member approve want 403 got %d", w.Code)
	}
	if w := call(t, s, "POST", "/v1/skills/grep-discipline/1.0/approve", "tok-coord", "", nil); w.Code != 204 {
		t.Fatalf("coord approve got %d", w.Code)
	}
	// audit 读=coordinator only
	if w := call(t, s, "GET", "/v1/audit", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member audit want 403 got %d", w.Code)
	}
	w := call(t, s, "GET", "/v1/audit", "tok-coord", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "skill.approved") {
		t.Fatalf("audit 缺记录:%d %s", w.Code, w.Body.String())
	}
}

func TestMeAliasInQuery(t *testing.T) {
	s, _ := testServer()
	call(t, s, "PUT", "/v1/tasks/T77", "tok-coord", taskBody, nil)
	w := call(t, s, "GET", "/v1/tasks?owner=me", "tok-devo", "", nil)
	var tasks []Task
	_ = json.Unmarshal(w.Body.Bytes(), &tasks)
	if len(tasks) != 1 || tasks[0].ID != "T77" {
		t.Fatalf("owner=me 解析错:%+v", tasks)
	}
	if w := call(t, s, "GET", "/v1/tasks?owner=me", "tok-k3", "", nil); strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatalf("k3 的 me 不应命中 devo48 件:%s", w.Body.String())
	}
}
