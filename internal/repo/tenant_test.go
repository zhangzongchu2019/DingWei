package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// 🔴 §22.2c #5 要求越权负测三类:伪造 tenant / 跨租户读 / 缺 context。
// 本轮只能验第三类,前两类【现在验不了】—— 领域表还没有租户列,
// 「跨租户读」在数据模型上尚不存在,造不出真负例。
// ⚠️ 故本文件不得被引用为「三类负测已具备」;它只覆盖「缺 context 默认拒」这一类。
// 前两类随 SP7b 各领域 schema 落地后补,那时才有可读的另一个租户。

// 缺租户上下文默认拒(§22.2c #4 fail-closed):零值一路走到底都必须被拒。
func TestZeroTenantCtxIsRejectedEverywhere(t *testing.T) {
	p := &Pool{} // db 为 nil:若某个入口先碰库再查上下文,这里会 panic 而不是安静通过
	ctx := context.Background()
	var zero TenantCtx

	if _, err := p.Exec(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("Exec 零值上下文应拒,得到 %v", err)
	}
	if _, err := p.Query(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("Query 零值上下文应拒,得到 %v", err)
	}
	if _, err := p.QueryRow(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("QueryRow 零值上下文应拒,得到 %v", err)
	}
	if err := p.Ping(ctx, zero); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("Ping 零值上下文应拒,得到 %v", err)
	}
	called := false
	err := p.InTx(ctx, zero, &sql.TxOptions{}, func(Tx) error { called = true; return nil })
	if !errors.Is(err, ErrNoTenant) {
		t.Fatalf("InTx 零值上下文应拒,得到 %v", err)
	}
	if called {
		t.Fatal("InTx 在上下文被拒后仍执行了回调 —— 拒绝须发生在触库之前")
	}
}

// 拒绝必须是 deny,不得降格成「一次成功的空结果」(§22.2a #2a)。
// QueryRow 之所以返回 (*sql.Row, error) 而非裸 *sql.Row,就是为了这一条。
func TestQueryRowDenyIsNotAnEmptyRow(t *testing.T) {
	row, err := (&Pool{}).QueryRow(context.Background(), TenantCtx{}, "SELECT 1")
	if err == nil {
		t.Fatal("拒绝时不得返回 nil error")
	}
	if row != nil {
		t.Fatal("拒绝时不得同时返回一个可 Scan 的 Row —— 那会让调用方把 deny 当成空结果集")
	}
}

func TestNewTenantCtx(t *testing.T) {
	if _, err := NewTenantCtx(""); !errors.Is(err, ErrNoTenant) {
		t.Fatal("空租户号必须拒,不接受稍后再填")
	}
	tc, err := NewTenantCtx("t-1")
	if err != nil {
		t.Fatalf("正常构造失败: %v", err)
	}
	if tc.ID() != "t-1" || tc.IsSystem() || tc.IsPending() {
		t.Fatalf("租户上下文属性不对: id=%q sys=%v pending=%v", tc.ID(), tc.IsSystem(), tc.IsPending())
	}
}

// 🔴 三种上下文必须互不冒充 —— 这是本包最容易出错的地方。
func TestContextKindsDoNotImpersonateEachOther(t *testing.T) {
	var zero TenantCtx
	sys := SystemCtx()
	pend := PendingTenantCtx("等 SP7b 的 datad 领域 schema")

	if zero.check() == nil {
		t.Fatal("零值必须不可用")
	}
	if sys.check() != nil || pend.check() != nil {
		t.Fatal("SystemCtx 与 PendingTenantCtx 必须可用")
	}
	// 零值不得被当成系统上下文:否则「忘了传」会伪装成「与租户无关」。
	if zero.IsSystem() {
		t.Fatal("零值不得表现为系统上下文")
	}
	// 系统上下文不得被当成 pending,反之亦然:一个是本就无租户,一个是欠租户。
	if sys.IsPending() {
		t.Fatal("SystemCtx 不得被当成 pending —— 会把「无租户语义」冒充成「已收编」")
	}
	if pend.IsSystem() {
		t.Fatal("PendingTenantCtx 不得被当成 SystemCtx —— 会让欠的债看起来不欠")
	}
	if pend.Reason() == "" {
		t.Fatal("pending 必须带理由")
	}
}

// 空理由的 pending 等于没登记,必须退化成【不可用】而非悄悄放行。
func TestPendingWithoutReasonIsUnusable(t *testing.T) {
	if PendingTenantCtx("").check() == nil {
		t.Fatal("空理由的 pending 必须不可用 —— 否则登记制度形同虚设")
	}
}
