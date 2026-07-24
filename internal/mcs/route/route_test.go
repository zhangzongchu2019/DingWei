package route

import "testing"

// P3 预制:五闸路由(契约 C5)表驱动测试。契约未两签前=预制自测;两签后升正式门用例。
func TestFiveGates(t *testing.T) {
	r := &Router{
		DispatcherIDs: map[string]bool{"zzc-dispatch-2642": true},
		EmergencyIDs:  map[string]bool{"zzc-owner-2642": true},
	}
	cases := []struct {
		name string
		env  Envelope
		want Verdict
		gate int
	}{
		// ① 身份闸
		{"裸名拒收", Envelope{From: "developer-out", FirstLine: "随便"}, VerdictIdentityRejected, 1},
		{"note类裸名拒收", Envelope{From: "mynote", MetaOp: "new"}, VerdictIdentityRejected, 1},
		{"大写非法", Envelope{From: "ZZC-dev55-2642", MetaOp: "new"}, VerdictIdentityRejected, 1},
		// ② 控制闸
		{"非授权者停任务=拒", Envelope{From: "zzc-dev55-2642", FirstLine: "停止 t1"}, VerdictDirectiveRejected, 2},
		{"非授权者reassign=拒", Envelope{From: "zzc-dev56-2642", FirstLine: "reassign n3"}, VerdictDirectiveRejected, 2},
		{"调度者停任务=放行落兜底", Envelope{From: "zzc-dispatch-2642", FirstLine: "停止 t1"}, VerdictLLM, 5},
		{"业主应急=放行(唯一例外通道)", Envelope{From: "zzc-owner-2642", FirstLine: "停止 t1"}, VerdictLLM, 5},
		// ③ 结构闸
		{"op=new 直走引擎", Envelope{From: "zzc-dev55-2642", MetaOp: "new", FirstLine: "任务描述"}, VerdictEngineOp, 3},
		{"op=result 直走引擎", Envelope{From: "zzc-dev56-2642", MetaOp: "result"}, VerdictEngineOp, 3},
		{"未知op不走结构闸", Envelope{From: "zzc-dev56-2642", MetaOp: "hack"}, VerdictLLM, 5},
		// ④ 状态闸
		{"门结果入账", Envelope{From: "zzc-tester-2642", FirstLine: "【门 PASS】", TaskID: "t1"}, VerdictStatusIngest, 4},
		{"里程碑入账", Envelope{From: "zzc-devo48-2642", FirstLine: "里程碑:M2 交件", TaskID: "t1"}, VerdictStatusIngest, 4},
		{"心跳在轨入账", Envelope{From: "zzc-devo48-2642", FirstLine: "在轨", TaskID: "t1"}, VerdictStatusIngest, 4},
		{"状态词但无任务号→兜底", Envelope{From: "zzc-devo48-2642", FirstLine: "PASS 了"}, VerdictLLM, 5},
		// ⑤ 兜底
		{"普通讨论升LLM", Envelope{From: "zzc-shenceclaude-2642", FirstLine: "这个设计我有个疑问"}, VerdictLLM, 5},
	}
	for _, c := range cases {
		got := r.Route(c.env)
		if got.Verdict != c.want || got.Gate != c.gate {
			t.Errorf("%s: got %s@gate%d want %s@gate%d", c.name, got.Verdict, got.Gate, c.want, c.gate)
		}
	}
}

// 状态闸提取物(Kind)正确,供入账分类。
func TestStatusKindExtraction(t *testing.T) {
	r := &Router{}
	got := r.Route(Envelope{From: "zzc-tester-2642", FirstLine: "【FAIL】用例x", TaskID: "t9"})
	if got.Verdict != VerdictStatusIngest || got.Kind != "FAIL" {
		t.Fatalf("%+v", got)
	}
}
