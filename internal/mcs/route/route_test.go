package route

import "testing"

// P3 首件:五闸路由(契约 C5 v1.5)dev 侧表驱动(存在性层;tester 另写独立 gate 负例=验收层)。
// 角色:tester=执行席+seg1 门方;impl=执行席;devo48=designer(task_role 成员、非执行席);qa=seg2 门方(★MC-a per-段)。
func newRouter(trusted bool) *Router {
	role := map[string]map[string]string{
		"t1": {
			"zzc-tester-2642": "tester",
			"zzc-impl-2642":   "implementer",
			"zzc-devo48-2642": "designer",
			"zzc-qa-2642":     "qa_owner",
		},
	}
	return &Router{
		DispatcherIDs: map[string]bool{"zzc-dispatch-2642": true},
		EmergencyIDs:  map[string]bool{"zzc-owner-2642": true},
		Trusted:       trusted,
		Authz:         func(p, task string) bool { _, ok := role[task][p]; return ok },
		RoleOf:        func(p, task string) string { return role[task][p] },
		// ★dev56 MC-a 门方 per-段:seg1 门方=tester、seg2 门方=qa(非硬编码 tester)。
		GateKeeperOf: func(task, seg string) string {
			if task != "t1" {
				return ""
			}
			switch seg {
			case "", "seg1":
				return "zzc-tester-2642"
			case "seg2":
				return "zzc-qa-2642"
			}
			return ""
		},
	}
}

func TestFiveGates_V15(t *testing.T) {
	r := newRouter(true)
	cases := []struct {
		name string
		env  Envelope
		want Verdict
		gate int
	}{
		// ① 身份闸
		{"裸名principal拒", Envelope{Principal: "developer-out"}, VerdictIdentityRejected, 1},
		{"大写非法", Envelope{Principal: "ZZC-x-2642", MetaOp: "new"}, VerdictIdentityRejected, 1},
		{"外部伪造system哨兵拒(dev56⑤)", Envelope{Principal: "system:identity-gate", MetaOp: "new"}, VerdictIdentityRejected, 1},
		// ② 控制闸(★MC-B 仅 meta.op)
		{"非授权控制op拒(meta.op=stop)", Envelope{Principal: "zzc-dev55-2642", MetaOp: "stop", FirstLine: "停止 t1"}, VerdictDirectiveRejected, 2},
		{"调度者控制op(stop)放行→引擎", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "stop"}, VerdictEngineOp, 2},
		{"业主应急控制op放行→引擎", Envelope{Principal: "zzc-owner-2642", MetaOp: "reassign"}, VerdictEngineOp, 2},
		// ③ 授权闸 role×op 矩阵
		{"非执行席发result拒(designer)", Envelope{Principal: "zzc-devo48-2642", MetaOp: "result", TaskID: "t1"}, VerdictDirectiveRejected, 3},
		{"执行席(tester)发result过", Envelope{Principal: "zzc-tester-2642", MetaOp: "result", TaskID: "t1"}, VerdictEngineOp, 4},
		{"执行席(impl)发result过", Envelope{Principal: "zzc-impl-2642", MetaOp: "result", TaskID: "t1"}, VerdictEngineOp, 4},
		{"非调度者建单拒", Envelope{Principal: "zzc-tester-2642", MetaOp: "new", TaskID: "t2"}, VerdictDirectiveRejected, 3},
		{"调度者建单过", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "new", TaskID: "t2"}, VerdictEngineOp, 4},
		{"status=任一task_role成员过", Envelope{Principal: "zzc-devo48-2642", MetaOp: "status", TaskID: "t1"}, VerdictEngineOp, 4},
		// ★shence P1:收口 close 必过 gate-3,执行席不得绕(核心安全洞)
		{"★P1 执行席发收口close拒(调度者唯一关闭权,不绕gate-4)", Envelope{Principal: "zzc-impl-2642", MetaOp: "close", TaskID: "t1"}, VerdictDirectiveRejected, 3},
		{"★P1 调度者发收口close过", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "close", TaskID: "t1"}, VerdictEngineOp, 4},
		{"★P1 非成员发收口close拒(非gate-5绕过)", Envelope{Principal: "zzc-dev55-2642", MetaOp: "close", TaskID: "t1"}, VerdictDirectiveRejected, 3},
		// ★dev56 MC-a:门方 per-段
		{"seg1门方(tester)发门PASS入账", Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "seg1"}, VerdictStatusIngest, 4},
		{"★MC-a seg2门方是qa非tester:tester发seg2门拒", Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "seg2"}, VerdictDirectiveRejected, 3},
		{"★MC-a seg2门方qa发seg2门PASS过", Envelope{Principal: "zzc-qa-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "seg2"}, VerdictStatusIngest, 4},
		{"非门方task_role成员发门PASS拒(dev56②)", Envelope{Principal: "zzc-devo48-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "seg1"}, VerdictDirectiveRejected, 3},
		{"结构化gate_result由门方发过", Envelope{Principal: "zzc-tester-2642", MetaOp: "gate_result", TaskID: "t1", Segment: "seg1"}, VerdictEngineOp, 4},
		{"★gate_result非门方拒(不绕gate-4)", Envelope{Principal: "zzc-impl-2642", MetaOp: "gate_result", TaskID: "t1", Segment: "seg1"}, VerdictDirectiveRejected, 3},
		// ⑤ 兜底 + MC-B
		{"普通讨论升LLM", Envelope{Principal: "zzc-shenceclaude-2642", FirstLine: "有个疑问"}, VerdictLLM, 5},
		{"★MC-B:status含暂停字样不误拒", Envelope{Principal: "zzc-tester-2642", MetaOp: "status", FirstLine: "阻塞：需暂停部署，等待审批", TaskID: "t1"}, VerdictEngineOp, 4},
		{"★MC-B:自然语言停止无op→LLM不拒", Envelope{Principal: "zzc-dev55-2642", FirstLine: "我觉得该停止这个方案"}, VerdictLLM, 5},
	}
	for _, c := range cases {
		got := r.Route(c.env)
		if got.Verdict != c.want || got.Gate != c.gate {
			t.Errorf("%s: got %s@%d want %s@%d", c.name, got.Verdict, got.Gate, c.want, c.gate)
		}
	}
}

// ★item① 过渡窗(principal 不可信):一切 mutating(含收口/门/status/new/result+控制+应急)fail-closed;仅只读放行。
func TestTransitionWindow_AllMutatingFailClosed(t *testing.T) {
	r := newRouter(false) // hub 未就位
	rejects := []struct {
		name string
		env  Envelope
	}{
		{"过渡期控制拒", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "stop"}},
		{"过渡期应急拒", Envelope{Principal: "zzc-owner-2642", MetaOp: "reassign"}},
		{"★item① 过渡期status入账拒(不再放行)", Envelope{Principal: "zzc-tester-2642", MetaOp: "status", TaskID: "t1"}},
		{"★item① 过渡期门PASS入账拒", Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "seg1"}},
		{"★item① 过渡期new拒", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "new", TaskID: "t1"}},
		{"★item① 过渡期收口close拒", Envelope{Principal: "zzc-dispatch-2642", MetaOp: "close", TaskID: "t1"}},
		{"★item① 过渡期result拒", Envelope{Principal: "zzc-tester-2642", MetaOp: "result", TaskID: "t1"}},
	}
	for _, c := range rejects {
		if got := r.Route(c.env); got.Verdict != VerdictDirectiveRejected {
			t.Errorf("%s: 过渡期应 fail-closed 拒,got %+v", c.name, got)
		}
	}
	// ★G 放行边界正例(防自锁 DoS):过渡期纯只读(无 mutating op/无状态首行)仍可用→落 gate-5 判读(非权威)。
	if got := r.Route(Envelope{Principal: "zzc-tester-2642", FirstLine: "查询一下当前进度"}); got.Verdict != VerdictLLM {
		t.Errorf("★G 过渡期只读应放行(gate-5判读):got %+v", got)
	}
}

// ★B default-deny:未知 op / 越表 (角色,op) → 拒(fail-closed,非 fall-through 放行)。
func TestDefaultDeny(t *testing.T) {
	r := newRouter(true)
	// Authz=nil → status fail-closed
	rNil := &Router{DispatcherIDs: map[string]bool{}, Trusted: true}
	if got := rNil.Route(Envelope{Principal: "zzc-x-2642", MetaOp: "status", TaskID: "t1"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("Authz nil 应 fail-closed:%+v", got)
	}
	// GateKeeperOf=nil → 门 fail-closed
	rNoGk := &Router{DispatcherIDs: map[string]bool{}, Trusted: true, Authz: func(string, string) bool { return true }}
	if got := rNoGk.Route(Envelope{Principal: "zzc-x-2642", FirstLine: "【门 PASS】", TaskID: "t1"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("GateKeeperOf nil 门应 fail-closed:%+v", got)
	}
	// 未知段(GateKeeperOf 返 "")→ 门 fail-closed
	if got := r.Route(Envelope{Principal: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1", Segment: "unknown"}); got.Verdict != VerdictDirectiveRejected {
		t.Fatalf("未知段门应 fail-closed:%+v", got)
	}
}
