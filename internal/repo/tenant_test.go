package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// 🔴 §22.2a 要求越权负测覆盖伪造企业、跨企业/跨人读与缺 context。
// 本文件在仓储入口层验证双轴必填和触库前拒绝；真实跨行 RLS 由 PG fixture 验。
// ⚠️ 故本文件不得被引用为「三类负测已具备」;它只覆盖「缺 context 默认拒」这一类。
// 前两类随 SP7b 各领域 schema 落地后补,那时才有可读的另一个租户。

// 缺租户上下文默认拒(§22.2c #4 fail-closed):零值一路走到底都必须被拒。
func TestZeroAccessCtxIsRejectedEverywhere(t *testing.T) {
	p := &Pool{} // db 为 nil:若某个入口先碰库再查上下文,这里会 panic 而不是安静通过
	ctx := context.Background()
	var zero AccessCtx

	if _, err := p.Exec(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("Exec 零值上下文应拒,得到 %v", err)
	}
	if _, err := p.Query(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("Query 零值上下文应拒,得到 %v", err)
	}
	if _, err := p.QueryRow(ctx, zero, "SELECT 1"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("QueryRow 零值上下文应拒,得到 %v", err)
	}
	if err := p.Ping(ctx, zero); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("Ping 零值上下文应拒,得到 %v", err)
	}
	called := false
	err := p.InTx(ctx, zero, &sql.TxOptions{}, func(Tx) error { called = true; return nil })
	if !errors.Is(err, ErrNoAccess) {
		t.Fatalf("InTx 零值上下文应拒,得到 %v", err)
	}
	if called {
		t.Fatal("InTx 在上下文被拒后仍执行了回调 —— 拒绝须发生在触库之前")
	}
}

// 拒绝必须是 deny,不得降格成「一次成功的空结果」(§22.2a #2a)。
// QueryRow 之所以返回 (*sql.Row, error) 而非裸 *sql.Row,就是为了这一条。
func TestQueryRowDenyIsNotAnEmptyRow(t *testing.T) {
	row, err := (&Pool{}).QueryRow(context.Background(), AccessCtx{}, "SELECT 1")
	if err == nil {
		t.Fatal("拒绝时不得返回 nil error")
	}
	if row != nil {
		t.Fatal("拒绝时不得同时返回一个可 Scan 的 Row —— 那会让调用方把 deny 当成空结果集")
	}
}

func TestNewAccessCtxRequiresBothAxes(t *testing.T) {
	for _, in := range [][2]string{{"", "p-1"}, {"org-1", ""}, {"", ""}} {
		if _, err := NewAccessCtx(in[0], in[1]); !errors.Is(err, ErrNoAccess) {
			t.Fatalf("企业/主体缺一必须拒: %#v", in)
		}
	}
	tc, err := NewAccessCtx("org-1", "p-1")
	if err != nil {
		t.Fatalf("正常构造失败: %v", err)
	}
	if tc.Org() != "org-1" || tc.Principal() != "p-1" || tc.IsSystem() || tc.IsPending() {
		t.Fatalf("访问上下文属性不对: org=%q principal=%q sys=%v pending=%v", tc.Org(), tc.Principal(), tc.IsSystem(), tc.IsPending())
	}
}

// 🔴 三种上下文必须互不冒充 —— 这是本包最容易出错的地方。
func TestContextKindsDoNotImpersonateEachOther(t *testing.T) {
	var zero AccessCtx
	sys := SystemCtx()
	pend := PendingCtx("datad", "SP7b", "等领域 schema 双轴化")

	if zero.check() == nil {
		t.Fatal("零值必须不可用")
	}
	if sys.check() != nil || pend.check() != nil {
		t.Fatal("SystemCtx 与 PendingCtx 必须可用")
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
		t.Fatal("PendingCtx 不得被当成 SystemCtx —— 会让欠的债看起来不欠")
	}
	if pend.Reason() == "" {
		t.Fatal("pending 必须带理由")
	}
	// 🔴 SA-12a:债的身份必须是结构化的,不能只有一行自由文本 ——
	// 否则闸只能报一个总数,回答不了「datad 归零了没有」。
	if pend.Domain() != "datad" || pend.OwnerSP() != "SP7b" {
		t.Fatalf("pending 必须带结构化的 (domain, ownerSP):得到 (%q,%q)", pend.Domain(), pend.OwnerSP())
	}
	if sys.Domain() != "" || zero.Domain() != "" {
		t.Fatal("只有 pending 才有 domain —— 否则又是一次「取值互相冒充」")
	}
}

// 三项任一为空的 pending 等于没登记,必须退化成【不可用】而非悄悄放行。
// 🔴 三项各钉一条:只测 reason 会让「domain 空着也能过」溜过去,
// 而 domain 空正是 SA-12a 要堵的「无主的债」。
func TestPendingWithoutFullRegistrationIsUnusable(t *testing.T) {
	cases := []struct {
		name             string
		dom, owner, reas string
	}{
		{"缺 domain", "", "SP7b", "r"},
		{"缺 ownerSP", "datad", "", "r"},
		{"缺 reason", "datad", "SP7b", ""},
	}
	for _, c := range cases {
		if PendingCtx(c.dom, c.owner, c.reas).check() == nil {
			t.Errorf("%s 的 pending 必须不可用 —— 否则登记制度形同虚设", c.name)
		}
	}
}
