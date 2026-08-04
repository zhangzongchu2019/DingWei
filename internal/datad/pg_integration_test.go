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
}
