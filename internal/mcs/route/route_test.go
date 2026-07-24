package route

import "testing"

// P3 预制:五闸路由(契约 C5 v0.2)表驱动。principal=鉴权主体;Trusted=hub 就位;Authz=task_role 授权闸。
func newRouter(trusted bool) *Router {
	roles := map[string]map[string]bool{
		"t1": {"zzc-tester-2642": true, "zzc-devo48-2642": true},
	}
	return &Router{
		DispatcherIDs: map[string]bool{"zzc-dispatch-2642": true},
		EmergencyIDs:  map[string]bool{"zzc-owner-2642": true},
		Trusted:       trusted,
		Authz:         func(p, task string) bool { return roles[task][p] },
	}
}

func TestFiveGates_V02(t *testing.T) {
	r := newRouter(true)
	cases := []struct {
		name string
		env  Envelope
		want Verdict
		gate int
	}{
		// ① 身份闸(读 principal)
		{"裸名principal拒", Envelope{Principal: "developer-out"}, VerdictIdentityRejected, 1},
		{"大写非法", Envelope{Principal: "ZZC-x-2642", MetaOp: "new"}, VerdictIdentityRejected, 1},
		// ② 控制闸
		{"非授权停任务拒", Envelope{Principal: "zzc-dev55-2642", FirstLine: "停止 t1"}, VerdictDirectiveRejected, 2},
		{"调度者控制→无op无task落兜底", Envelope{Principal: "zzc-dispatch-2642", FirstLine: "停止"}, VerdictLLM, 5},
		// ③ 授权闸(MC-2)
		{"非task_role发result拒", Envelope{Principal: "zzc-dev56-2642", MetaOp: "result", TaskID: "t1"}, VerdictDirectiveRejected, 3},
		{"task_role成员发result过", Envelope{Principal: "zzc-tester-2642", MetaOp: "result", TaskID: "t1"}, VerdictEngineOp, 4},
		{"非调度者建单拒", Envelope{Principal: "zzc-tester-2642", MetaOp: "new", TaskID: "t2"}, VerdictDirectiveRejected, 3},
		{"调度者建单过", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "new", TaskID: "t2"}, VerdictEngineOp, 4},
		{"task_role成员发门PASS入账", Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1"}, VerdictStatusIngest, 4},
		{"非task_role伪造门拒(MC-2核心)", Envelope{Principal: "zzc-dev55-2642", FirstLine: "【门 PASS】", TaskID: "t1"}, VerdictDirectiveRejected, 3},
		// ⑤ 兜底
		{"普通讨论升LLM", Envelope{Principal: "zzc-shenceclaude-2642", FirstLine: "有个疑问"}, VerdictLLM, 5},
		{"状态词无task→兜底", Envelope{Principal: "zzc-devo48-2642", FirstLine: "PASS 了"}, VerdictLLM, 5},
	}
	for _, c := range cases {
		got := r.Route(c.env)
		if got.Verdict != c.want || got.Gate != c.gate {
			t.Errorf("%s: got %s@%d want %s@%d", c.name, got.Verdict, got.Gate, c.want, c.gate)
		}
	}
}

// 过渡期(principal 不可信):控制/应急通道整体关闭(★C5 5.0 不带病放行)。
func TestTransition_HighRiskChannelsClosed(t *testing.T) {
	r := newRouter(false) // hub 未就位
	// 调度者控制指令也拒(过渡期无可信身份)
	if got := r.Route(Envelope{Principal: "zzc-dispatch-2642", FirstLine: "停止 t1"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("过渡期控制应拒:%+v", got)
	}
	// 业主应急也拒
	if got := r.Route(Envelope{Principal: "zzc-owner-2642", FirstLine: "停止 t1"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("过渡期应急应拒:%+v", got)
	}
	// 但只读/状态入账仍放行(授权闸过)
	if got := r.Route(Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1"}); got.Verdict != VerdictStatusIngest {
		t.Fatalf("过渡期状态入账应放行:%+v", got)
	}
}

// Authz=nil → 状态 op fail-closed(拒)。
func TestAuthzNilFailClosed(t *testing.T) {
	r := &Router{DispatcherIDs: map[string]bool{}, Trusted: true, Authz: nil}
	if got := r.Route(Envelope{Principal: "zzc-x-2642", MetaOp: "result", TaskID: "t1"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("Authz nil 应 fail-closed:%+v", got)
	}
}
