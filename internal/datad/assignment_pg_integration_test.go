package datad

// A-lite PG 集成测试(R2-5/R2-7/N-A3):真 PG 上证 Mem 载体盖不住的语义面——
//   ① DB 真并发唯一(advisory 串行+partial-unique 硬门);
//   ② 三角色 ACL(assign 唯一写路径的 DB 强制:app/gc 角色直写=permission denied);
//   ③ 首果 GC 域(仅 closed+超期;越界负例);
//   ④ 连接池 role 不泄漏(独立池 current_user 恒定)。
// 门控:DATAD_TEST_PG_DSN(app 角色)+DATAD_TEST_PG_ASSIGN_DSN(assign 角色)
//   +DATAD_TEST_PG_GC_DSN(gc 角色)+DATAD_TEST_PG_SETUP_DSN(超级用户,GC 回溯造账用)。
// 无 DSN=显式 Skip(如实标注,不假绿)。⚠铁律:必须指独立测试库(dingwei_test 谱系),
//   永不指生产库(08-04 T-ITEST 教训)。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type pgWorld struct {
	p       *PGStore
	appDB   *sql.DB // datad_user 池(直连,ACL 负例用)
	setupDB *sql.DB // 超级用户(可选:回溯造账/清场)
	tok     map[string]bool
	haveACL bool // 三角色 DSN 齐=可跑 ACL/泄漏面
	haveSup bool
	assign  string
	gc      string
}

func pgWorldUp(t *testing.T) *pgWorld {
	t.Helper()
	dsn := os.Getenv("DATAD_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DATAD_TEST_PG_DSN 未设:PG 集成候真 PG(核心逻辑 Mem 同套已盖;真并发/ACL/GC 仅此处证)")
	}
	assignDSN := os.Getenv("DATAD_TEST_PG_ASSIGN_DSN")
	gcDSN := os.Getenv("DATAD_TEST_PG_GC_DSN")
	ctx := context.Background()
	p, err := OpenPGPools(ctx, dsn, assignDSN, gcDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	w := &pgWorld{p: p, tok: map[string]bool{sExec: true, sRev: true, sThird: true},
		haveACL: assignDSN != "" && gcDSN != "", assign: assignDSN, gc: gcDSN}
	w.appDB, err = sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.appDB.Close() })
	if sup := os.Getenv("DATAD_TEST_PG_SETUP_DSN"); sup != "" {
		w.setupDB, err = sql.Open("pgx", sup)
		if err != nil {
			t.Fatal(err)
		}
		w.haveSup = true
		t.Cleanup(func() { w.setupDB.Close() })
	}
	return w
}

// newTask 造一件 proposed task(经 app 池 PutTask=生产同路径)。
func (w *pgWorld) newTask(t *testing.T, suffix string) string {
	t.Helper()
	id := fmt.Sprintf("T-R3-%d-%s", time.Now().UnixNano(), suffix)
	if _, err := w.p.PutTask(context.Background(),
		Task{ID: id, Title: "r3", OwnerSession: sCoord, Status: "proposed"}, time.Time{}, true); err != nil {
		t.Fatal(err)
	}
	return id
}

// pgUID 跨轮唯一 UUID 形 request_id(atomic:B4 并发测多 goroutine 调用)。
var pgUIDSeq atomic.Int64

func pgUID() string {
	n := pgUIDSeq.Add(1)
	return fmt.Sprintf("%08x-0000-4000-8000-%012d", time.Now().UnixNano()&0xffffffff, n)
}

func isPGDenied(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "42501" // insufficient_privilege
	}
	return err != nil && strings.Contains(err.Error(), "permission denied")
}

// TestPGAssignFull:A1/A3/A4三型/A5/A9/A13/audit 事务面在真 PG 走一遍(SQL 断言行级终态)。
func TestPGAssignFull(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()

	// A1 半件拒+成功首派+owner/status(SQL 断言)
	tid := w.newTask(t, "a1")
	if _, e := w.p.Assign(ctx, tid, &AssignRequest{Executor: sExec, RequestID: pgUID()}, w.tok, sCoord); e == nil || e.Code != ErrHalfAssignment {
		t.Fatalf("A1 半件: %v", e)
	}
	rid1 := pgUID()
	r1, e := w.p.Assign(ctx, tid, req(sExec, sRev, rid1), w.tok, sCoord)
	if e != nil || r1.Generation != 1 {
		t.Fatalf("A1 首派: %v %+v", e, r1)
	}
	var owner, status string
	if err := w.appDB.QueryRow(`SELECT owner_session,status FROM task WHERE id=$1`, tid).Scan(&owner, &status); err != nil || owner != sExec || status != "dispatched" {
		t.Fatalf("A1 owner/status SQL: %v %s %s", err, owner, status)
	}
	// audit 事务面:成功行 op=assign,actor=coordinator,detail 完整(jsonb 字段级断言:
	// PG jsonb 存储会规范化空白/键序,不做子串比对)。
	var agen int
	var aexec, arev, aoutcome string
	if err := w.appDB.QueryRow(`SELECT (detail->>'generation')::int, detail->>'executor', detail->>'reviewer', detail->>'outcome'
	    FROM audit WHERE op='assign' AND actor=$1 AND detail->>'request_id'=$2`, sCoord, rid1).Scan(&agen, &aexec, &arev, &aoutcome); err != nil {
		t.Fatalf("A1 audit(事务内成功行): %v", err)
	}
	if agen != 1 || aexec != sExec || arev != sRev || aoutcome != "success" {
		t.Fatalf("audit detail 字段: gen=%d exec=%s rev=%s outcome=%s", agen, aexec, arev, aoutcome)
	}

	// A3 重派 gen+1 旧代保留(SQL 行数)
	g1 := int64(1)
	if _, e := w.p.Assign(ctx, tid, &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: pgUID(), ExpectedGeneration: &g1}, w.tok, sCoord); e != nil {
		t.Fatalf("A3 重派: %v", e)
	}
	var total, active int
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1`, tid).Scan(&total)
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1 AND active`, tid).Scan(&active)
	if total != 4 || active != 2 {
		t.Fatalf("A3 应 4 行历史/2 行当代,got %d/%d", total, active)
	}

	// A4 三型+A9(独立 task)
	tid2 := w.newTask(t, "a4")
	badRID := pgUID()
	if _, e := w.p.Assign(ctx, tid2, req(sExec, sExec, badRID), w.tok, sCoord); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("A4 首错: %v", e)
	}
	okRID := pgUID()
	if _, e := w.p.Assign(ctx, tid2, req(sExec, sRev, okRID), w.tok, sCoord); e != nil {
		t.Fatalf("A4 成功: %v", e)
	}
	if _, e := w.p.Assign(ctx, tid2, req(sExec, sExec, badRID), w.tok, sCoord); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("A4 第三型(条件变仍原样): %v", e)
	}
	r, e := w.p.Assign(ctx, tid2, req(sExec, sRev, okRID), w.tok, sCoord)
	if e != nil || r.Generation != 1 {
		t.Fatalf("A4 成功重放: %v %+v", e, r)
	}
	if _, e := w.p.Assign(ctx, tid2, req(sThird, sRev, okRID), w.tok, sCoord); e == nil || e.Code != ErrRequestConflict {
		t.Fatalf("A9 换参: %v", e)
	}

	// A5 首派带 expected=invalid_request(v1.8r2);重派错 gen=412
	tid3 := w.newTask(t, "a5")
	g0 := int64(0)
	if _, e := w.p.Assign(ctx, tid3, &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: pgUID(), ExpectedGeneration: &g0}, w.tok, sCoord); e == nil || e.Code != ErrInvalidRequest || e.Sub != SubExpectedGenOnFirst {
		t.Fatalf("A5 首派带 expected 应 invalid_request: %v", e)
	}
	w.p.Assign(ctx, tid3, req(sExec, sRev, pgUID()), w.tok, sCoord)
	bad := int64(9)
	if _, e := w.p.Assign(ctx, tid3, &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: pgUID(), ExpectedGeneration: &bad}, w.tok, sCoord); e == nil || e.Code != ErrStaleGen || e.HTTP != 412 {
		t.Fatalf("A5 CAS: %v", e)
	}

	// A13 三子因(PG 路径)
	tid4 := w.newTask(t, "a13")
	if _, e := w.p.Assign(ctx, tid4, req(sExec, sRev, "not-a-uuid"), w.tok, sCoord); e == nil || e.Sub != SubRequestIDMalformed {
		t.Fatalf("A13①: %v", e)
	}
	var cnt int
	w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id='not-a-uuid'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatal("A13① malformed request_id 不应入账")
	}
	neg := int64(-1)
	nrid := pgUID()
	if _, e := w.p.Assign(ctx, tid4, &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: nrid, ExpectedGeneration: &neg}, w.tok, sCoord); e == nil || e.Sub != SubNegativeGeneration {
		t.Fatalf("A13②: %v", e)
	}
	var frBody string
	if err := w.appDB.QueryRow(`SELECT body::text FROM assign_first_result WHERE request_id=$1`, nrid).Scan(&frBody); err != nil || !strings.Contains(frBody, SubNegativeGeneration) {
		t.Fatalf("A13② 应入账带 sub: %v %s", err, frBody)
	}
	frid := pgUID()
	if _, e := w.p.Assign(ctx, tid4, &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: frid, Force: true}, w.tok, sCoord); e == nil || e.Sub != SubForceReasonMissing {
		t.Fatalf("A13③: %v", e)
	}
	// 失败首果与 audit 同事务:两者都在
	w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id=$1`, frid).Scan(&cnt)
	if cnt != 1 {
		t.Fatal("A13③ 失败首果应落账")
	}
	w.appDB.QueryRow(`SELECT count(*) FROM audit WHERE op=$1 AND detail->>'request_id'=$2`, "assign."+ErrInvalidRequest, frid).Scan(&cnt)
	if cnt != 1 {
		t.Fatal("A13③ 失败 audit 应与首果同事务落")
	}
	// A13⑤ 大小写幂等(M1,PG TEXT 主键=威胁本体):大写形重放=同一把键单行,不重执行
	tid5 := w.newTask(t, "m1case")
	base := pgUID()
	r1, e1 := w.p.Assign(ctx, tid5, req(sExec, sRev, base), w.tok, sCoord)
	if e1 != nil {
		t.Fatalf("A13⑤ 首派: %v", e1)
	}
	r2, e2 := w.p.Assign(ctx, tid5, req(sExec, sRev, strings.ToUpper(base)), w.tok, sCoord)
	if e2 != nil || r2.Generation != r1.Generation {
		t.Fatalf("A13⑤ 大写形重放应原样返: %v %+v vs %+v", e2, r2, r1)
	}
	w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE lower(request_id)=lower($1)`, base).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("A13⑤ 首果账应单行(小写规范键),got %d", cnt)
	}
	var storedKey string
	w.appDB.QueryRow(`SELECT request_id FROM assign_first_result WHERE lower(request_id)=lower($1)`, base).Scan(&storedKey)
	if storedKey != strings.ToLower(base) {
		t.Fatalf("A13⑤ 入库键应小写形: %s", storedKey)
	}
}

// TestPGA2ConcurrentUnique:K=8 真并发首派(各自连接,advisory 串行+partial-unique 兜底)——
// 恰一赢家 gen1;每 loser=stale_generation/412+首果账;SQL 终态行级断言(R2-5 PG 层原证)。
func TestPGA2ConcurrentUnique(t *testing.T) {
	w := pgWorldUp(t)
	tid := w.newTask(t, "a2k")
	const K = 8
	rids := make([]string, K)
	for i := range rids {
		rids[i] = pgUID()
	}
	res := make([]*AssignResult, K)
	errs := make([]*AssignErr, K)
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i], errs[i] = w.p.Assign(context.Background(), tid, req(sExec, sRev, rids[i]), w.tok, sCoord)
		}(i)
	}
	wg.Wait()
	winners := 0
	for i := 0; i < K; i++ {
		if errs[i] == nil {
			winners++
			if res[i].Generation != 1 {
				t.Fatalf("赢家应 gen1: %+v", res[i])
			}
			continue
		}
		e := errs[i]
		if e.Code != ErrStaleGen || e.HTTP != 412 {
			t.Fatalf("loser %s 应 stale_generation/412: %+v", rids[i], e)
		}
		fr, ferr := w.p.GetFirstResult(context.Background(), rids[i])
		if ferr != nil || fr == nil || fr.Outcome != ErrStaleGen {
			t.Fatalf("loser %s 首果: %v %+v", rids[i], ferr, fr)
		}
		// jsonb 规范化空白/键序≠字节保真;DTO 字段级同值断言(HTTP 层字节一致性由
		// 单一 DTO 再 marshal 保证,server_test TestHTTPAssignDTOMatchesFirstResult 逐字节证)。
		var stored AssignErr
		if err := json.Unmarshal(fr.Body, &stored); err != nil || stored.Code != e.Code || stored.HTTP != e.HTTP || stored.Msg != e.Msg || stored.Sub != e.Sub {
			t.Fatalf("loser %s 首果 body DTO 不同值: %v %+v vs %+v", rids[i], err, stored, e)
		}
	}
	if winners != 1 {
		t.Fatalf("恰一赢家,got %d", winners)
	}
	var active, gens, exN, rvN int
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1 AND active`, tid).Scan(&active)
	w.appDB.QueryRow(`SELECT count(DISTINCT generation) FROM assignment WHERE task_id=$1 AND active`, tid).Scan(&gens)
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1 AND active AND role='executor' AND assignee=$2`, tid, sExec).Scan(&exN)
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1 AND active AND role='reviewer' AND assignee=$2`, tid, sRev).Scan(&rvN)
	if active != 2 || gens != 1 || exN != 1 || rvN != 1 {
		t.Fatalf("终态 active=%d gens=%d ex=%d rv=%d(应 2/1/1/1)", active, gens, exN, rvN)
	}
	// partial-unique 硬门直证:即便 assign 角色直 INSERT 第二行 active executor 也被索引拒。
	_, err := w.p.assignDB.Exec(`INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES($1,'executor','evil',99,true,$2)`, tid, pgUID())
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "23505" {
		t.Fatalf("partial-unique 应拒第二 active executor(23505),got %v", err)
	}
}

// TestPGACLThreeRoles:N-A3 addendum 负向原证——app/gc 角色直写 assignment/首果=42501;
// assign 角色无 DELETE 首果;audit 不可改。三角色 DSN 未齐=显式 Skip(不假绿)。
func TestPGACLThreeRoles(t *testing.T) {
	w := pgWorldUp(t)
	if !w.haveACL {
		t.Skip("DATAD_TEST_PG_ASSIGN_DSN/GC_DSN 未设:ACL 负向面需三角色部署(evidence 脚本跑)")
	}
	tid := w.newTask(t, "acl")
	rid := pgUID()
	if _, e := w.p.Assign(context.Background(), tid, req(sExec, sRev, rid), w.tok, sCoord); e != nil {
		t.Fatalf("assign 角色正路径应成功: %v", e)
	}
	// app 角色(datad_user)四负例
	for name, q := range map[string]string{
		"app INSERT assignment":   `INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES('` + tid + `','executor','x',9,false,'x')`,
		"app UPDATE assignment":   `UPDATE assignment SET active=false WHERE task_id='` + tid + `'`,
		"app INSERT first_result": `INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id) VALUES('x','y',1,'h','{}','t')`,
		"app UPDATE first_result": `UPDATE assign_first_result SET outcome='tamper' WHERE request_id='` + rid + `'`,
		"app DELETE first_result": `DELETE FROM assign_first_result WHERE request_id='` + rid + `'`,
		"app UPDATE audit":        `UPDATE audit SET actor='tamper' WHERE actor='` + sCoord + `'`,
	} {
		if _, err := w.appDB.Exec(q); !isPGDenied(err) {
			t.Errorf("%s 应 permission denied,got %v", name, err)
		}
	}
	// gc 角色负例:写 assignment/改首果=拒(删除首果=允许面,GC 测试单证)
	for name, q := range map[string]string{
		"gc INSERT assignment":   `INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES('` + tid + `','executor','x',9,false,'x')`,
		"gc UPDATE assignment":   `UPDATE assignment SET active=false WHERE task_id='` + tid + `'`,
		"gc UPDATE first_result": `UPDATE assign_first_result SET outcome='tamper' WHERE request_id='` + rid + `'`,
		"gc INSERT first_result": `INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id) VALUES('x2','y',1,'h','{}','t')`,
	} {
		if _, err := w.p.gcDB.Exec(q); !isPGDenied(err) {
			t.Errorf("%s 应 permission denied,got %v", name, err)
		}
	}
	// assign 角色:无 DELETE 首果(GC 权限不在 assign)
	if _, err := w.p.assignDB.Exec(`DELETE FROM assign_first_result WHERE request_id='` + rid + `'`); !isPGDenied(err) {
		t.Errorf("assign DELETE first_result 应 denied,got %v", err)
	}
	if _, err := w.p.assignDB.Exec(`UPDATE audit SET actor='tamper' WHERE detail->>'request_id'='` + rid + `'`); !isPGDenied(err) {
		t.Errorf("assign UPDATE audit 应 denied,got %v", err)
	}
}

// TestPGRoleNoLeak:独立池 current_user 恒为本池角色;多轮借还连接后不串线(addendum 泄漏面)。
func TestPGRoleNoLeak(t *testing.T) {
	w := pgWorldUp(t)
	if !w.haveACL {
		t.Skip("三角色 DSN 未齐")
	}
	// 先走一次 assign 事务(把 assign 池连接用过一轮)
	tid := w.newTask(t, "leak")
	w.p.Assign(context.Background(), tid, req(sExec, sRev, pgUID()), w.tok, sCoord)
	for i := 0; i < 5; i++ {
		var u string
		w.appDB.QueryRow(`SELECT current_user`).Scan(&u)
		if u == "datad_assign" || u == "datad_assign_gc" {
			t.Fatalf("app 池串到 %s(role 泄漏)", u)
		}
		w.p.assignDB.QueryRow(`SELECT current_user`).Scan(&u)
		if u != "datad_assign" {
			t.Fatalf("assign 池 current_user=%s(应 datad_assign)", u)
		}
		w.p.gcDB.QueryRow(`SELECT current_user`).Scan(&u)
		if u != "datad_assign_gc" {
			t.Fatalf("gc 池 current_user=%s(应 datad_assign_gc)", u)
		}
	}
}

// TestPGGCFirstResults:GC 仅删 closed+超期;open+超期/closed+新 皆留;需超级用户回溯 created_at。
func TestPGGCFirstResults(t *testing.T) {
	w := pgWorldUp(t)
	if !w.haveSup {
		t.Skip("DATAD_TEST_PG_SETUP_DSN 未设:GC 回溯造账需超级用户(evidence 脚本跑)")
	}
	ctx := context.Background()
	mk := func(suffix, status string, old bool) (string, string) {
		tid := w.newTask(t, "gc"+suffix)
		rid := pgUID()
		if _, e := w.p.Assign(ctx, tid, req(sExec, sRev, rid), w.tok, sCoord); e != nil {
			t.Fatal(e)
		}
		if status != "dispatched" {
			if _, err := w.setupDB.Exec(`UPDATE task SET status=$2 WHERE id=$1`, tid, status); err != nil {
				t.Fatal(err)
			}
		}
		if old {
			if _, err := w.setupDB.Exec(`UPDATE assign_first_result SET created_at=now()-interval '40 days' WHERE request_id=$1`, rid); err != nil {
				t.Fatal(err)
			}
		}
		return tid, rid
	}
	_, ridDel := mk("1", "done", true)        // closed+超期 → 删
	_, ridOpen := mk("2", "dispatched", true) // open+超期 → 留
	_, ridNew := mk("3", "archived", false)   // closed+新 → 留
	n, err := w.p.GCFirstResults(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if n < 1 {
		t.Fatalf("GC 应至少删 1 行,got %d", n)
	}
	check := func(rid string, want int, why string) {
		var c int
		w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id=$1`, rid).Scan(&c)
		if c != want {
			t.Fatalf("%s: rid=%s 应 %d 行,got %d", why, rid, want, c)
		}
	}
	check(ridDel, 0, "closed+超期应删")
	check(ridOpen, 1, "open+超期须留(重放安全)")
	check(ridNew, 1, "closed+新须留(保留窗内)")
}

// TestPGMigrateOnly:evidence 脚本引导用——仅触发 SchemaDDL 幂等迁移(grants.sql 执行前置)。
func TestPGMigrateOnly(t *testing.T) {
	dsn := os.Getenv("DATAD_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DATAD_TEST_PG_DSN 未设")
	}
	p, err := OpenPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
}

// cycle2 B3(PG 层):跨 task 同 request_id=稳定 request_conflict;audit 同事务;目标 task 零污染。
func TestPGB3CrossTask(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()
	ta := w.newTask(t, "b3a")
	tb := w.newTask(t, "b3b")
	rid := pgUID()
	if _, e := w.p.Assign(ctx, ta, req(sExec, sRev, rid), w.tok, sCoord); e != nil {
		t.Fatal(e)
	}
	if _, e := w.p.Assign(ctx, tb, req(sExec, sRev, rid), w.tok, sCoord); e == nil || e.Code != ErrRequestConflict {
		t.Fatalf("跨 task 应 request_conflict: %v", e)
	}
	var cnt int
	w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1`, tb).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("tb 不得被误派,assignment=%d", cnt)
	}
	var status string
	w.appDB.QueryRow(`SELECT status FROM task WHERE id=$1`, tb).Scan(&status)
	if status != "proposed" {
		t.Fatalf("tb status 应原样 proposed: %s", status)
	}
	w.appDB.QueryRow(`SELECT count(*) FROM audit WHERE op=$1 AND detail->>'request_id'=$2 AND target=$3`,
		"assign."+ErrRequestConflict, rid, tb).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("跨 task conflict audit 应落(同事务): %d", cnt)
	}
	// 原 task 重放仍通
	if r, e := w.p.Assign(ctx, ta, req(sExec, sRev, rid), w.tok, sCoord); e != nil || r.Generation != 1 {
		t.Fatalf("原 task 重放: %v %+v", e, r)
	}
}

// cycle2 B4(PG 层):PUT 与 Assign 并发交错——不变量=任意时刻 owner_session==当代 executor;
// 冲突 PUT 稳定拒且零改 task。K 轮随机交错+每轮不变量断言。
func TestPGB4PutAssignInterleave(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()
	tid := w.newTask(t, "b4")
	// 首派 gen1:executor=sExec
	if _, e := w.p.Assign(ctx, tid, req(sExec, sRev, pgUID()), w.tok, sCoord); e != nil {
		t.Fatal(e)
	}
	execs := []string{sExec, sThird}
	const K = 12
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				// 重派翻代(读现代次→CAS;竞争败=412 正常)
				var g sql.NullInt64
				w.appDB.QueryRow(`SELECT generation FROM assignment WHERE task_id=$1 AND role='executor' AND active`, tid).Scan(&g)
				if g.Valid {
					ng := execs[(i/2)%2]
					rv := execs[1-(i/2)%2]
					w.p.Assign(ctx, tid, &AssignRequest{Executor: ng, Reviewer: rv, RequestID: pgUID(), ExpectedGeneration: &g.Int64}, w.tok, sCoord)
				}
			} else {
				// 并发 PUT:owner 写当前读到的 executor(合法)或对手(应被拒)
				cur, err := w.p.GetTask(ctx, tid)
				if err != nil {
					return
				}
				tt := *cur
				tt.OwnerSession = execs[i%len(execs)]
				w.p.PutTask(ctx, tt, cur.UpdatedAt, false) // owner_conflict/optimistic conflict 都是合法拒
			}
		}(i)
	}
	wg.Wait()
	// 终态不变量:owner_session == 当代 active executor(B4 核心)
	var owner, activeExec string
	if err := w.appDB.QueryRow(`SELECT owner_session FROM task WHERE id=$1`, tid).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := w.appDB.QueryRow(`SELECT assignee FROM assignment WHERE task_id=$1 AND role='executor' AND active`, tid).Scan(&activeExec); err != nil {
		t.Fatal(err)
	}
	if owner != activeExec {
		t.Fatalf("B4 不变量破:owner_session=%s ≠ 当代 executor=%s", owner, activeExec)
	}
	// 定向负例:共锁下改 owner≠executor 稳定拒且零改
	cur, _ := w.p.GetTask(ctx, tid)
	bad := *cur
	if activeExec == sExec {
		bad.OwnerSession = sThird
	} else {
		bad.OwnerSession = sExec
	}
	if _, err := w.p.PutTask(ctx, bad, cur.UpdatedAt, false); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("owner≠executor 应 owner_conflict: %v", err)
	}
	after, _ := w.p.GetTask(ctx, tid)
	if after.OwnerSession != owner || !after.UpdatedAt.Equal(cur.UpdatedAt) {
		t.Fatalf("冲突 PUT 不得改 task: before=%s after=%s", owner, after.OwnerSession)
	}
	// force 路径同拒
	if _, err := w.p.PutTask(ctx, bad, time.Time{}, true); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("force PUT 亦应 owner_conflict: %v", err)
	}
}

// cycle2 C1:异 task 同 request_id 真并发——恰一提交,败方**首次即稳定 request_conflict/409**
// (绝不 store_down);同 body 与异 body 两型;多轮防调度侥幸;败方 task/assignment 零污染。
func TestPGC1CrossTaskConcurrentSameRID(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()
	for round := 0; round < 6; round++ {
		sameBody := round%2 == 0
		ta := w.newTask(t, fmt.Sprintf("c1a%d", round))
		tb := w.newTask(t, fmt.Sprintf("c1b%d", round))
		rid := pgUID()
		reqA := req(sExec, sRev, rid)
		reqB := req(sExec, sRev, rid)
		if !sameBody {
			reqB = req(sThird, sRev, rid) // 异 body(同 rid)
		}
		var ra, rb *AssignResult
		var ea, eb *AssignErr
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; ra, ea = w.p.Assign(ctx, ta, reqA, w.tok, sCoord) }()
		go func() { defer wg.Done(); <-start; rb, eb = w.p.Assign(ctx, tb, reqB, w.tok, sCoord) }()
		close(start)
		wg.Wait()
		okN := 0
		for i, e := range []*AssignErr{ea, eb} {
			if e == nil {
				okN++
				continue
			}
			// 败方判据:首次即 request_conflict/409;store_down=C1 复发即败
			if e.Code == ErrStoreDownA {
				t.Fatalf("round%d(sameBody=%v) 败方首响 store_down(C1 未闭): %+v", round, sameBody, e)
			}
			if e.Code != ErrRequestConflict || e.HTTP != 409 {
				t.Fatalf("round%d 败方应 request_conflict/409,got %+v (i=%d)", round, e, i)
			}
		}
		if okN != 1 {
			t.Fatalf("round%d 应恰一成功,got %d (ea=%v eb=%v ra=%v rb=%v)", round, okN, ea, eb, ra, rb)
		}
		// 败方 task 零污染+首果恰一行
		loser := tb
		if ea != nil {
			loser = ta
		}
		var cnt int
		w.appDB.QueryRow(`SELECT count(*) FROM assignment WHERE task_id=$1`, loser).Scan(&cnt)
		if cnt != 0 {
			t.Fatalf("round%d 败方 task 有 assignment=%d", round, cnt)
		}
		var status string
		w.appDB.QueryRow(`SELECT status FROM task WHERE id=$1`, loser).Scan(&status)
		if status != "proposed" {
			t.Fatalf("round%d 败方 status=%s", round, status)
		}
		w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id=$1`, rid).Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("round%d 首果应恰一行,got %d", round, cnt)
		}
	}
}

// cycle2 C1 附:赢家响应丢失重试——两边各自重试,结果稳定(赢家 task 原样返首果/败方 task
// 稳定 409),首果恒一份。
func TestPGC1ResponseLossRetry(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()
	ta := w.newTask(t, "c1ra")
	tb := w.newTask(t, "c1rb")
	rid := pgUID()
	r1, e1 := w.p.Assign(ctx, ta, req(sExec, sRev, rid), w.tok, sCoord)
	if e1 != nil {
		t.Fatal(e1)
	}
	if _, e := w.p.Assign(ctx, tb, req(sExec, sRev, rid), w.tok, sCoord); e == nil || e.Code != ErrRequestConflict {
		t.Fatalf("tb 首次: %v", e)
	}
	// 模拟响应丢失:两边各自重试×3,结果逐次稳定
	for i := 0; i < 3; i++ {
		r2, e2 := w.p.Assign(ctx, ta, req(sExec, sRev, rid), w.tok, sCoord)
		if e2 != nil || r2.Generation != r1.Generation || r2.Executor != r1.Executor || r2.TaskID != ta {
			t.Fatalf("赢家重试#%d 应原样首果: %v %+v", i, e2, r2)
		}
		if _, e := w.p.Assign(ctx, tb, req(sExec, sRev, rid), w.tok, sCoord); e == nil || e.Code != ErrRequestConflict || e.HTTP != 409 {
			t.Fatalf("败方重试#%d 应稳定 409: %v", i, e)
		}
	}
	var cnt int
	w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id=$1`, rid).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("首果恒一份,got %d", cnt)
	}
}

// cycle2 C1 附:交叉组合无死锁——(TA,r1)(TA,r2)(TB,r1)(TB,r2)混打 K 轮,固定锁序下全部完成
// (超时=死锁即败),终态不变量保持。
func TestPGC1LockOrderNoDeadlock(t *testing.T) {
	w := pgWorldUp(t)
	ctx := context.Background()
	ta := w.newTask(t, "dla")
	tb := w.newTask(t, "dlb")
	r1, r2 := pgUID(), pgUID()
	combos := []struct {
		task string
		rid  string
	}{{ta, r1}, {ta, r2}, {tb, r1}, {tb, r2}}
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for k := 0; k < 4; k++ {
			for _, c := range combos {
				wg.Add(1)
				go func(task, rid string) {
					defer wg.Done()
					w.p.Assign(ctx, task, req(sExec, sRev, rid), w.tok, sCoord)
				}(c.task, c.rid)
			}
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("交叉组合 30s 未完成=疑似死锁(固定锁序失效)")
	}
	// 每 rid 首果恰一行;每 task owner==当代 executor
	for _, rid := range []string{r1, r2} {
		var cnt int
		w.appDB.QueryRow(`SELECT count(*) FROM assign_first_result WHERE request_id=$1`, rid).Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("rid=%s 首果应恰一行,got %d", rid, cnt)
		}
	}
	for _, task := range []string{ta, tb} {
		var owner string
		var execA sql.NullString
		w.appDB.QueryRow(`SELECT owner_session FROM task WHERE id=$1`, task).Scan(&owner)
		w.appDB.QueryRow(`SELECT assignee FROM assignment WHERE task_id=$1 AND role='executor' AND active`, task).Scan(&execA)
		if execA.Valid && owner != execA.String {
			t.Fatalf("task=%s owner=%s ≠ executor=%s", task, owner, execA.String)
		}
	}
}
