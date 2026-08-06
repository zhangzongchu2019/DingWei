package datad

// PG 集成测试(DATAD_TEST_PG_DSN 门控;prodops 实例就绪后 CI/真机跑):
// 同一套核心流对真 PG 走一遍——建表幂等/task 乐观锁/config CAS/自报域/审计。
// 无 DSN=显式 Skip(如实标注,不假绿)。
// ⚠铁律(08-04 教训:曾用生产 DSN 跑本测留 T-ITEST 残留污染生产账):
//   DATAD_TEST_PG_DSN 必须指【独立测试库 dingwei_test】,【永不指生产库 dingwei】。
//   本测会写 T-ITEST-* 件+config/session 行,指生产=污染真账。prodops 已建 dingwei_test 隔离。

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestPGIntegration(t *testing.T) {
	dsn := os.Getenv("DATAD_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DATAD_TEST_PG_DSN 未设:PG 集成候 prodops 实例(D4c),核心逻辑已由 MemStore 同套用例盖")
	}
	ctx := context.Background()
	pg, err := OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	// 二次 Open=建表幂等
	pg2, err := OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("幂等迁移: %v", err)
	}
	pg2.Close()

	id := "T-ITEST-" + time.Now().UTC().Format("150405")
	tk, err := pg.PutTask(ctx, Task{ID: id, Title: "itest", OwnerSession: "zzc-devo48-2642",
		Status: "in_progress"}, time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	// 乐观锁:错时间戳=409
	if _, err := pg.PutTask(ctx, *tk, tk.UpdatedAt.Add(-time.Second), false); err != ErrConflict {
		t.Fatalf("want ErrConflict got %v", err)
	}
	// progress jsonb_set
	out, err := pg.UpdateProgress(ctx, id, TaskProgress{Anchor: "abc123",
		Progress: json.RawMessage(`{"note":"pg itest"}`)})
	if err != nil || out.Anchor != "abc123" {
		t.Fatalf("progress: %v %+v", err, out)
	}
	// config CAS
	key := "itest." + id
	if _, err := pg.PutConfig(ctx, key, json.RawMessage(`{"v":1}`), 0, "itest"); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.PutConfig(ctx, key, json.RawMessage(`{"v":2}`), 0, "itest"); err != ErrCASFailed {
		t.Fatalf("CAS want ErrCASFailed got %v", err)
	}
	// 自报域+audit
	if err := pg.PutSessionState(ctx, SessionState{Session: "zzc-devo48-2642", State: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := pg.AppendHeartbeat(ctx, "zzc-devo48-2642", json.RawMessage(`{"n":1}`), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := pg.AppendAudit(ctx, AuditEntry{Actor: "itest", Op: "itest.run", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, err := pg.ListAudit(ctx, time.Now().Add(-time.Minute), 10)
	if err != nil || len(got) == 0 {
		t.Fatalf("audit 回读: %v n=%d", err, len(got))
	}

	// 只读端点往返(shenceclaude MC2:PG SQL 须真跑,FK/advisory 锁/DISTINCT 语义 Mem 盖不住)
	for i, d := range []Dispatch{
		{TaskID: id, Assignee: "zzc-devo48-2642", Role: "executor", Round: 1},
		{TaskID: id, Assignee: "zzc-claudek3-2642", Role: "reviewer", Round: 1},
	} {
		if err := pg.AppendDispatch(ctx, d); err != nil {
			t.Fatalf("dispatch append #%d: %v", i, err)
		}
	}
	rows, err := pg.ListDispatches(ctx, id, 0, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("dispatch 回读: %v n=%d", err, len(rows))
	}
	if rows[0].ID >= rows[1].ID || rows[0].CreatedAt.IsZero() || rows[1].Role != "reviewer" {
		t.Fatalf("dispatch 行序/生成列: %+v", rows)
	}
	// since_id 增量:下界不含
	inc, err := pg.ListDispatches(ctx, id, rows[0].ID, 0)
	if err != nil || len(inc) != 1 || inc[0].ID != rows[1].ID {
		t.Fatalf("since_id 增量: %v %+v", err, inc)
	}
	// FK:不存在的 task=拒
	if err := pg.AppendDispatch(ctx, Dispatch{TaskID: id + "-nofk", Assignee: "x", Role: "executor"}); err == nil {
		t.Fatal("dispatch FK 未生效:不存在 task 应拒")
	}
	// heartbeat 第二跳晚于第一跳→latest 取新
	hbSession := "zzc-itest-" + id
	if err := pg.AppendHeartbeat(ctx, hbSession, nil, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	second := time.Now().UTC().Truncate(time.Microsecond) // timestamptz=微秒精度,纳秒对拍假红
	if err := pg.AppendHeartbeat(ctx, hbSession, nil, second); err != nil {
		t.Fatal(err)
	}
	hs, err := pg.LatestHeartbeats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hs {
		if h.Session == hbSession {
			found = true
			if !h.At.Equal(second) {
				t.Fatalf("latest 未取最新跳: got %v want %v", h.At, second)
			}
		}
	}
	if !found {
		t.Fatalf("latest 缺会话 %s: %+v", hbSession, hs)
	}
}
