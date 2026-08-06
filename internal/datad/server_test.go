package datad

// P 组核心用例(设计冻结版 §9,MemStore 载体;PG 集成=D4c 就绪后同套跑):
// P2 权限矩阵/P3 CAS/me 别名/P7 审计完整性(含未遂 403)/自报域白名单/乐观锁。

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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

func TestReadonlyDispatchEndpoint(t *testing.T) {
	s, ms := testServer()
	call(t, s, "PUT", "/v1/tasks/T99", "tok-coord", taskBody, nil)
	// T98 也须真实建件:PG 有 dispatch.task_id FK,只在 Mem 绿=假绿(shenceclaude MC1)
	call(t, s, "PUT", "/v1/tasks/T98", "tok-coord",
		`{"title":"T98","owner_session":"zzc-devo48-2642","status":"in_progress"}`, nil)
	// member 读=403 且未遂入审计;coordinator=200
	if w := call(t, s, "GET", "/v1/dispatch", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member dispatch read want 403 got %d", w.Code)
	}
	found := false
	for _, e := range ms.audit {
		if e.Op == "dispatch.read.denied" && e.Actor == "zzc-devo48-2642" {
			found = true
		}
	}
	if !found {
		t.Fatal("dispatch.read 未遂 403 未入审计(P7)")
	}
	for _, b := range []string{
		`{"task_id":"T99","assignee":"zzc-devo48-2642","role":"executor"}`,
		`{"task_id":"T99","assignee":"zzc-claudek3-2642","role":"reviewer"}`,
		`{"task_id":"T98","assignee":"zzc-devo48-2642","role":"executor","round":2}`,
	} {
		if w := call(t, s, "POST", "/v1/dispatch", "tok-coord", b, nil); w.Code != 201 {
			t.Fatalf("append got %d: %s", w.Code, w.Body.String())
		}
	}
	var rows []DispatchRow
	_ = json.Unmarshal(call(t, s, "GET", "/v1/dispatch", "tok-coord", "", nil).Body.Bytes(), &rows)
	if len(rows) != 3 || rows[0].ID != 1 || rows[2].ID != 3 || rows[0].CreatedAt.IsZero() {
		t.Fatalf("全量读:%+v", rows)
	}
	// 增量:since_id=2 只返 id=3
	_ = json.Unmarshal(call(t, s, "GET", "/v1/dispatch?since_id=2", "tok-coord", "", nil).Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].ID != 3 || rows[0].Round != 2 {
		t.Fatalf("since_id 增量:%+v", rows)
	}
	// task_id 过滤
	_ = json.Unmarshal(call(t, s, "GET", "/v1/dispatch?task_id=T99", "tok-coord", "", nil).Body.Bytes(), &rows)
	if len(rows) != 2 || rows[1].Role != "reviewer" {
		t.Fatalf("task_id 过滤:%+v", rows)
	}
	// member 自报域(Q1):assignee=me=200 只见本人行;越权 assignee=403
	w := call(t, s, "GET", "/v1/dispatch?assignee=me", "tok-devo", "", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &rows)
	if w.Code != 200 || len(rows) != 2 {
		t.Fatalf("member assignee=me: code=%d %+v", w.Code, rows)
	}
	for _, r := range rows {
		if r.Assignee != "zzc-devo48-2642" {
			t.Fatalf("member 见他人行:%+v", r)
		}
	}
	if w := call(t, s, "GET", "/v1/dispatch?assignee=zzc-claudek3-2642", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member 越权 assignee want 403 got %d", w.Code)
	}
}

func TestLatestHeartbeatsEndpoint(t *testing.T) {
	s, _ := testServer()
	// member 读=403
	if w := call(t, s, "GET", "/v1/heartbeats/latest", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member heartbeat read want 403 got %d", w.Code)
	}
	// 两会话各两跳,取每会话最新
	call(t, s, "POST", "/v1/sessions/me/heartbeat", "tok-devo", `{"n":1}`, nil)
	call(t, s, "POST", "/v1/sessions/me/heartbeat", "tok-k3", `{"n":1}`, nil)
	time.Sleep(2 * time.Millisecond)
	call(t, s, "POST", "/v1/sessions/me/heartbeat", "tok-devo", `{"n":2}`, nil)
	var hs []HeartbeatLatest
	w := call(t, s, "GET", "/v1/heartbeats/latest", "tok-coord", "", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &hs)
	if w.Code != 200 || len(hs) != 2 {
		t.Fatalf("latest heartbeats: code=%d %+v", w.Code, hs)
	}
	// session 升序:claudek3 < devo48;devo48 的 at=第二跳(晚于 claudek3 的唯一跳)
	if hs[0].Session != "zzc-claudek3-2642" || hs[1].Session != "zzc-devo48-2642" ||
		!hs[1].At.After(hs[0].At) {
		t.Fatalf("每会话最新/排序错:%+v", hs)
	}
}

func TestPullModeAndGate(t *testing.T) {
	s, ms := testServer()
	mk := func(id string) {
		call(t, s, "PUT", "/v1/tasks/"+id, "tok-coord",
			`{"title":"`+id+`","owner_session":"zzc-devo48-2642","status":"dispatched"}`, nil)
	}
	mk("P1")
	mk("P2")
	// 拉取 P1=200 → in_progress
	w := call(t, s, "POST", "/v1/tasks/P1/pull", "tok-devo", `{"to":"in_progress"}`, nil)
	var got Task
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.Status != "in_progress" {
		t.Fatalf("pull P1: %d %+v", w.Code, got)
	}
	// 每次一项闸:名下已有 in_progress 再拉 P2=409
	if w := call(t, s, "POST", "/v1/tasks/P2/pull", "tok-devo", `{"to":"in_progress"}`, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "pull_gate") {
		t.Fatalf("pull gate want 409 pull_gate got %d %s", w.Code, w.Body.String())
	}
	// 非 owner=403(coordinator 也不走此端点,越闸走 PUT)+未遂入审计(CR n2)
	if w := call(t, s, "POST", "/v1/tasks/P1/pull", "tok-k3", `{"to":"review"}`, nil); w.Code != 403 {
		t.Fatalf("非owner pull want 403 got %d", w.Code)
	}
	denied := false
	for _, e := range ms.audit {
		if e.Op == "task.pull.denied" && e.Actor == "zzc-claudek3-2642" {
			denied = true
		}
	}
	if !denied {
		t.Fatal("非owner pull 未遂 403 未入审计(P7)")
	}
	// 交审 P1 释放闸→拉 P2=200
	if w := call(t, s, "POST", "/v1/tasks/P1/pull", "tok-devo", `{"to":"review"}`, nil); w.Code != 200 {
		t.Fatalf("submit review: %d %s", w.Code, w.Body.String())
	}
	if w := call(t, s, "POST", "/v1/tasks/P2/pull", "tok-devo", `{"to":"in_progress"}`, nil); w.Code != 200 {
		t.Fatalf("闸未释放: %d %s", w.Code, w.Body.String())
	}
	// 错误态:review 件再拉=409 wrong_state;非法 to=422
	if w := call(t, s, "POST", "/v1/tasks/P1/pull", "tok-devo", `{"to":"in_progress"}`, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "wrong_state") {
		t.Fatalf("wrong_state want 409 got %d %s", w.Code, w.Body.String())
	}
	if w := call(t, s, "POST", "/v1/tasks/P1/pull", "tok-devo", `{"to":"done"}`, nil); w.Code != 422 {
		t.Fatalf("非法 to want 422 got %d", w.Code)
	}
	// audit 落账
	found := false
	for _, e := range ms.audit {
		if e.Op == "task.pull.in_progress" && e.Actor == "zzc-devo48-2642" {
			found = true
		}
	}
	if !found {
		t.Fatal("pull 未入审计")
	}
}

func TestPullGateConcurrent(t *testing.T) {
	// 招牌属性证据位(CR N1):同 owner 并发拉 K 件,恰 1 成功其余 pull_gate。
	s, _ := testServer()
	const k = 8
	for i := 0; i < k; i++ {
		call(t, s, "PUT", "/v1/tasks/C"+strconv.Itoa(i), "tok-coord",
			`{"title":"c","owner_session":"zzc-devo48-2642","status":"dispatched"}`, nil)
	}
	codes := make(chan int, k)
	var wg sync.WaitGroup
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w := call(t, s, "POST", "/v1/tasks/C"+strconv.Itoa(n)+"/pull", "tok-devo",
				`{"to":"in_progress"}`, nil)
			codes <- w.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	ok, gated, other := 0, 0, 0
	for c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			gated++
		default:
			other++
		}
	}
	if ok != 1 || gated != k-1 || other != 0 {
		t.Fatalf("并发闸:want 1×200+%d×409 got %d×200/%d×409/%d other", k-1, ok, gated, other)
	}
}

func TestPullLagEndpoint(t *testing.T) {
	s, _ := testServer()
	call(t, s, "PUT", "/v1/tasks/L1", "tok-coord",
		`{"title":"L1","owner_session":"zzc-devo48-2642","status":"dispatched"}`, nil)
	call(t, s, "PUT", "/v1/tasks/L2", "tok-coord",
		`{"title":"L2","owner_session":"zzc-claudek3-2642","status":"dispatched"}`, nil)
	call(t, s, "PUT", "/v1/tasks/L3", "tok-coord",
		`{"title":"L3","owner_session":"zzc-devo48-2642","status":"in_progress"}`, nil)
	// 派发台账行=权威 since 时点
	call(t, s, "POST", "/v1/dispatch", "tok-coord",
		`{"task_id":"L1","assignee":"zzc-devo48-2642","role":"executor"}`, nil)
	// progress 污染 updated_at(晚于派发时点),钉死 since 不退化取 updated_at(CR N3)
	time.Sleep(2 * time.Millisecond)
	call(t, s, "POST", "/v1/tasks/L1/progress", "tok-devo", `{"anchor":"lagx"}`, nil)
	// member=403
	if w := call(t, s, "GET", "/v1/tasks/pull-lag", "tok-devo", "", nil); w.Code != 403 {
		t.Fatalf("member pull-lag want 403 got %d", w.Code)
	}
	w := call(t, s, "GET", "/v1/tasks/pull-lag", "tok-coord", "", nil)
	var rows []PullLagRow
	_ = json.Unmarshal(w.Body.Bytes(), &rows)
	if w.Code != 200 || len(rows) != 2 { // 仅 dispatched:L1/L2;L3 不入
		t.Fatalf("pull-lag: code=%d %+v", w.Code, rows)
	}
	for _, r := range rows {
		if r.TaskID == "L3" {
			t.Fatalf("in_progress 件不应入监控:%+v", rows)
		}
		if r.Since.IsZero() || r.AgeSeconds < 0 {
			t.Fatalf("since/age 缺:%+v", r)
		}
	}
	if !rows[0].Since.Before(rows[1].Since) && !rows[0].Since.Equal(rows[1].Since) {
		t.Fatalf("未按最久在前排序:%+v", rows)
	}
	// N3 钉死:L1 的 since=dispatch.created_at,非被 progress 抬高的 task.updated_at
	var ds []DispatchRow
	_ = json.Unmarshal(call(t, s, "GET", "/v1/dispatch?task_id=L1", "tok-coord", "", nil).Body.Bytes(), &ds)
	var l1t Task
	_ = json.Unmarshal(call(t, s, "GET", "/v1/tasks/L1", "tok-coord", "", nil).Body.Bytes(), &l1t)
	for _, r := range rows {
		if r.TaskID == "L1" {
			if len(ds) != 1 || !r.Since.Equal(ds[0].CreatedAt) {
				t.Fatalf("since 应=dispatch.created_at: since=%v dispatch=%+v", r.Since, ds)
			}
			if r.Since.Equal(l1t.UpdatedAt) || !l1t.UpdatedAt.After(r.Since) {
				t.Fatalf("since 退化取了 updated_at: since=%v updated_at=%v", r.Since, l1t.UpdatedAt)
			}
		}
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
