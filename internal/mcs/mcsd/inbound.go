// inbound.go —— P3 集成件:调度者收件面(route.Router 接生产收件路径)。
//
// ★shence F1 定性闭合点:route/identity 首件=逻辑 done、"生产 enforce 待集成"——本文件即接线,
// 三条 caller 义务在此兑现(集成门):
//   ①gate-5 LLM 判读输出【不授权】:VerdictLLM 路径零 Board 写;判读结论唯一合法回路=ResolveLLM
//     →重进五闸(gate-3 授权闸),不得直落 guarded 写(A-3);
//   ②Authz 拒空 taskID:board.RoleOfWho 空 taskID 恒非成员(route 侧已防御,caller 侧同样 fail-closed);
//   ③引擎 status/result handler 不得从正文推断 gate 推进:gate 推进(gate_result 落账)唯一路径=
//     router 门方闸过的 MetaOp=="" StatusIngest;engine_op(result/status)只返回授权结论交引擎
//     C1 路由执行,本收件面绝不因正文含"门 PASS"字样入账(否则 MC-a 端到端被绕)。
// 另:★§6 H2/criteria §3.6-B 入口校合法集——非空未知 meta.op → reject(unknown_op),
// 不 fall-through 落 gate-5(route.KnownOp;route 已签逻辑不动,在入口闭合)。
// 审计:一切拒路径落 board_event 不可吞(criteria F/dev55 H3);identity 拒 actor=system:identity-gate
// 哨兵、裸名进 data(dev56③);route 拒 actor=system:route-gate。
package mcsd

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/route"
)

// Inbound 收件面原始输入(传输层已鉴权 principal+结构化 meta;body=原文,首行由本包 canonical 化)。
type Inbound struct {
	Principal string // 传输层鉴权后的 per-session 主体(非信封自述 from,C5 5.0)
	MetaOp    string
	TaskID    string
	Segment   string
	Body      string
}

// Decision 收件面结论(引擎/调用方据此行动;拒路径已落审计账)。
type Decision struct {
	Verdict  route.Verdict
	Gate     int
	EngineOp string // Verdict=engine_op:授权放行的结构化 op(执行走引擎 C1 路由,账随各 guarded 原语)
	Kind     string // Verdict=status_ingest:提取类别(门/PASS/FAIL/里程碑/在轨/阻塞)
	NeedsLLM bool   // Verdict=llm:须调度者 LLM 判读(★义务①:本路径零写,结论走 ResolveLLM 回闸)
	Suspect  bool   // gate-5 自然语言疑似控制(要求结构化重投,MC-B)
	Reason   string // 拒因(unknown_op 等,审计同款)
}

// Receiver 调度者收件面(Router×Board 接线)。GateKeeperOf 门方登记源自 C2.1 派发信封的
// 里程碑分段(P3=显式登记 SetGateKeeper;P4 接真信封解析源)。
type Receiver struct {
	B *board.Board
	R *route.Router

	mu  sync.Mutex
	gks map[string]string // taskID+"\x00"+segment → 门方 principal
}

// NewReceiver 组收件面:Router 的 Authz/RoleOf 接 Board task_role(义务②),门方接登记表(MC-a per-段)。
func NewReceiver(b *board.Board, dispatcherIDs, emergencyIDs map[string]bool, trusted bool) *Receiver {
	rc := &Receiver{B: b, gks: map[string]string{}}
	rc.R = &route.Router{
		DispatcherIDs: dispatcherIDs,
		EmergencyIDs:  emergencyIDs,
		Trusted:       trusted,
		Authz: func(principal, taskID string) bool { // ∈任一 task_role;空 taskID 恒 false(义务②)
			return b.RoleOfWho(context.Background(), taskID, principal) != ""
		},
		RoleOf: func(principal, taskID string) string {
			return b.RoleOfWho(context.Background(), taskID, principal)
		},
		GateKeeperOf: rc.gatekeeperOf,
	}
	return rc
}

// SetGateKeeper 登记某 task 某段的门方 principal(来源=C2.1 派发信封里程碑分段)。
func (rc *Receiver) SetGateKeeper(taskID, segment, principal string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.gks[taskID+"\x00"+segment] = principal
}

func (rc *Receiver) gatekeeperOf(taskID, segment string) string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.gks[taskID+"\x00"+segment] // 未登记="" → route 门 fail-closed
}

// transportHdrRe 通讯传输头:【from→to】(ts) 前缀(C5 5.0a canonical 剥离项)。
// ★必须含 →(收发对):否则会误剥状态首行自身的【门 PASS】/【在轨】括号头。
var transportHdrRe = regexp.MustCompile(`^【[^】]*→[^】]*】\s*(\([^)]*\)\s*)?`)

// CanonicalFirstLine C5 5.0a canonical 规范化首行:跳过空行与 #DWR1 兼容镜像标签行
// (带外 meta 是路由真源,镜像永不作真源,决策B)/剥传输头/剥对称引号与反引号包裹。
func CanonicalFirstLine(body string) string {
	lines := strings.Split(body, "\n")
	i := 0
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || l == "#DWR1" || strings.HasPrefix(l, "#DWR1 ") {
			continue
		}
		break
	}
	if i >= len(lines) {
		return ""
	}
	l := strings.TrimSpace(lines[i])
	l = transportHdrRe.ReplaceAllString(l, "")
	l = strings.Trim(strings.TrimSpace(l), "`\"'")
	return strings.TrimSpace(l)
}

// Handle 单信封过收件面:canonical→五闸→按 verdict 落账/返回。拒路径必落审计事件(F/H3)。
func (rc *Receiver) Handle(ctx context.Context, in Inbound) (Decision, error) {
	// ★H2/§3.6-B 入口校合法集:非空未知 meta.op=reject(unknown_op),不落 gate-5
	if in.MetaOp != "" && !route.KnownOp(in.MetaOp) {
		d := Decision{Verdict: route.VerdictDirectiveRejected, Gate: 2, Reason: "unknown_op"}
		err := rc.B.AuditEvent(ctx, "directive_rejected", in.TaskID, "system:route-gate",
			map[string]any{"principal": in.Principal, "meta_op": in.MetaOp, "reason": "unknown_op"})
		return d, err
	}
	env := route.Envelope{
		Principal: in.Principal, MetaOp: in.MetaOp, TaskID: in.TaskID,
		Segment: in.Segment, FirstLine: CanonicalFirstLine(in.Body),
	}
	res := rc.R.Route(env)
	switch res.Verdict {
	case route.VerdictIdentityRejected:
		// 拒可审计:actor=哨兵(违规者无合法身份可签,dev56③),裸名进 data 非 actor
		err := rc.B.AuditEvent(ctx, "identity_rejected", in.TaskID, "system:identity-gate",
			map[string]any{"name": in.Principal, "source": "inbound"})
		return Decision{Verdict: res.Verdict, Gate: res.Gate}, err

	case route.VerdictDirectiveRejected:
		err := rc.B.AuditEvent(ctx, "directive_rejected", in.TaskID, "system:route-gate",
			map[string]any{"principal": in.Principal, "meta_op": in.MetaOp, "gate": res.Gate})
		return Decision{Verdict: res.Verdict, Gate: res.Gate}, err

	case route.VerdictStatusIngest:
		// ★义务③:gate 推进唯一入账路径(router 已过 per-段门方闸的 MetaOp=="" 首行)。
		kind := "status_note"
		if res.Kind == "门" || res.Kind == "PASS" || res.Kind == "FAIL" {
			kind = "gate_result"
		}
		err := rc.B.AuditEvent(ctx, kind, in.TaskID, in.Principal,
			map[string]any{"kind": res.Kind, "segment": in.Segment, "line": env.FirstLine})
		return Decision{Verdict: res.Verdict, Gate: res.Gate, Kind: res.Kind}, err

	case route.VerdictEngineOp:
		// 授权放行:结构化 op 交引擎 C1 路由执行(其账随各 guarded 原语)。
		// ★义务③:本收件面不看正文,engine result/status 的 handler 亦不得因正文"门 PASS"字样推进 gate。
		return Decision{Verdict: res.Verdict, Gate: res.Gate, EngineOp: in.MetaOp}, nil

	default: // route.VerdictLLM
		// ★义务①:LLM 只判读不授权——本路径零 Board 写;结论必须经 ResolveLLM 重进五闸。
		return Decision{Verdict: res.Verdict, Gate: res.Gate, NeedsLLM: true, Suspect: res.Suspect}, nil
	}
}

// ResolveLLM LLM 判读结论回授权闸(★义务①唯一合法回路,A-3:判读=结构化 op 提案,
// 重进 Handle 全闸链——判读不了闸的结论到不了 guarded 写)。
func (rc *Receiver) ResolveLLM(ctx context.Context, orig Inbound, judgedOp, judgedTaskID string) (Decision, error) {
	in := orig
	in.MetaOp = judgedOp
	if judgedTaskID != "" {
		in.TaskID = judgedTaskID
	}
	return rc.Handle(ctx, in)
}
