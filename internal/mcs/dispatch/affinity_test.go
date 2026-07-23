package dispatch

import (
	"context"
	"reflect"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// ---- M3:亲和螺丝(W1' 已终选 A;M1-View §3b / V11 / V44) ----

// V11:修订亲和两级链——链①(原 worker idle 派回)命中;原 worker 死后提示作废,
// 修订节点静默走链②(Board 重建),零等待零新状态,正确性不依赖亲和。
func TestV11_RevisionAffinityChainDegradesSilently(t *testing.T) {
	d, drv, b, ctx := rig(t)
	if _, _, err := d.DispatchOne(ctx, "n1", 1); err != nil { // n1→w0(origin 记 w0)
		t.Fatal(err)
	}
	s, _ := b.GetNode(ctx, "n1")
	b.Processing(ctx, "n1", 1, s.DeliveryID.String, d.ID)
	b.Terminal(ctx, "n1", 1, s.DeliveryID.String, true, "ref", d.ID)
	d.P.Release("w0")
	// 修订节点继承原产出 node 的 origin(上层建图时调用)
	if err := b.AddNode(ctx, "rev1", "t1", 3, "ready", nil); err != nil {
		t.Fatal(err)
	}
	d.SetOriginHint("rev1", d.OriginOf("n1"))
	if d.OriginOf("rev1") != "w0" {
		t.Fatalf("origin 继承失败:%s", d.OriginOf("rev1"))
	}
	// 链① 命中
	wid, _, err := d.DispatchOne(ctx, "rev1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid != "w0" {
		t.Fatalf("链① 应派回 w0,got %s", wid)
	}
	sRev, _ := b.GetNode(ctx, "rev1")
	b.Processing(ctx, "rev1", 1, sRev.DeliveryID.String, d.ID)
	b.Terminal(ctx, "rev1", 1, sRev.DeliveryID.String, true, "", d.ID)
	d.P.Release("w0")
	// w0 死→reap;第二个修订节点 prefer w0→静默落链②,零等待
	drv.spawned[0].mu.Lock()
	drv.spawned[0].dead = true
	drv.spawned[0].mu.Unlock()
	d.P.Reap(ctx)
	if err := b.AddNode(ctx, "rev2", "t1", 4, "ready", nil); err != nil {
		t.Fatal(err)
	}
	d.SetOriginHint("rev2", "w0")
	wid2, _, err := d.DispatchOne(ctx, "rev2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid2 == "" || wid2 == "w0" {
		t.Fatalf("原 worker 死应静默落链②得新 worker,got %q", wid2)
	}
	sr, _ := b.GetNode(ctx, "rev2")
	if sr.Status != "dispatched" {
		t.Fatalf("链② 派发未成:%+v", sr)
	}
}

// V44(负测,结构性断言):派发路径零会话文件依赖——
// ①Worker 契约面(池对 driver 的全部要求)不含任何 resume/会话读取方法;
// ②Envelope(投递载体)不含任何会话文件字段。
// fenced session handoff=未来 B 窗(M1-view 非规范段);未实现该协议前出现 resume 面=违规。
func TestV44_NoSessionResumeSurfaceInDispatchPath(t *testing.T) {
	wt := reflect.TypeOf((*pool.Worker)(nil)).Elem()
	allowed := map[string]bool{"ID": true, "Health": true, "Caps": true, "Deliver": true, "Stop": true}
	for i := 0; i < wt.NumMethod(); i++ {
		name := wt.Method(i).Name
		if !allowed[name] {
			t.Fatalf("Worker 契约出现派发外方法 %q——V44:未实现 fenced handoff 前派发路径不得有 resume/会话面", name)
		}
	}
	et := reflect.TypeOf(pool.Envelope{})
	for i := 0; i < et.NumField(); i++ {
		name := et.Field(i).Name
		switch name {
		case "NodeID", "Generation", "RID", "Payload":
		default:
			t.Fatalf("Envelope 出现未知字段 %q(不得携带会话文件/句柄)", name)
		}
	}
}

// A案正确性底线:亲和提示整体丢失(进程重启内存清零)后,一切照常从 Board 重建=链②。
func TestA_OriginLossIsHarmless(t *testing.T) {
	d, _, b, ctx := rig(t)
	if err := b.AddNode(ctx, "rev1", "t1", 3, "ready", nil); err != nil {
		t.Fatal(err)
	}
	d.SetOriginHint("rev1", "w0")
	// 模拟重启:提示全清
	d.mu.Lock()
	d.origin = map[string]string{}
	d.mu.Unlock()
	wid, _, err := d.DispatchOne(ctx, "rev1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid == "" {
		t.Fatal("提示丢失不得影响派发")
	}
	s, _ := b.GetNode(ctx, "rev1")
	if s.Status != "dispatched" {
		t.Fatalf("链② 未成:%+v", s)
	}
}
