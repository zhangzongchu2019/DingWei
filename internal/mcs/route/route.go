// Package route 实现调度者收件面的五闸路由(契约 C5 v1.5;正则先行,LLM 兜底,降无谓调用)。
//
// 次序(C5 5.1,逐闸短路):①身份闸(C4 格式)→②控制闸(meta.op∈ControlOps)→
// ③授权闸(role×op 矩阵,状态 op 必过、含收口/门,★shence P1 不绕 gate-4)→④状态入账→⑤LLM 兜底。
// 规则只作用于 meta+首行(不信 body 自称,§6 冻结件同构);规则表版本化,改=改契约须重两签。
package route

import (
	"regexp"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
)

// Verdict 路由结论。
type Verdict string

const (
	VerdictIdentityRejected  Verdict = "identity_rejected"  // ①
	VerdictDirectiveRejected Verdict = "directive_rejected" // ②/③
	VerdictEngineOp          Verdict = "engine_op"          // ③过闸→走引擎既有 op 路由
	VerdictStatusIngest      Verdict = "status_ingest"      // ④状态机入账(门/里程碑/心跳)
	VerdictLLM               Verdict = "llm"                // ⑤升调度者 LLM 判读
)

// Envelope 收件面输入(只含 meta+canonical 首行;body 不参与路由,C5 5.2)。
type Envelope struct {
	// Principal=传输层鉴权后的 per-session 主体(★C5 5.0 MC-1:【非】信封自述 from,
	// from 在共享队钥下可伪造)。gate 全读此字段。过渡期 hub 未注入=空→整体关闭(见 Router.Trusted)。
	Principal string
	MetaOp    string // 信封 meta 的 op(可信 control/state op 仅此结构化字段,C5 5.0a)
	FirstLine string // canonical 规范化首行(剥传输头/镜像标签/引号,C5 5.0a)
	TaskID    string // meta 载明的任务号(空=无)
	// Segment=门 PASS/FAIL 定位的里程碑段(★dev56 MC-a:门方 per-段,非硬编码 tester);空=无段。
	Segment string
}

// Result 路由结果(附闸位与提取物,供入账/审计)。
type Result struct {
	Verdict Verdict
	Gate    int    // 命中闸位 1-5
	Kind    string // 状态闸提取(仅 VerdictStatusIngest)
	Suspect bool   // ★MC-B:gate-5 自然语言疑似控制(要求结构化重投,不执行不误拒)
}

// InTaskRole ∈任一 task_role(status/心跳用);nil=拒。
type InTaskRole func(principal, taskID string) bool

// Router 五闸路由器(契约 C5 v1.5)。
type Router struct {
	DispatcherIDs map[string]bool // 调度者小组(new/收口/reject 授权;②控制闸放行)
	EmergencyIDs  map[string]bool // 应急原语合法来源(业主,C6)
	Trusted       bool            // C5 5.0 principal 是否可信(hub per-session 就位=true;过渡期 false)
	Authz         InTaskRole      // ∈任一 task_role(status/心跳)
	// GateKeeperOf(task,segment)=该段 C2.1 指定门方 principal(★dev56 MC-a:per-段、角色不硬编码为 tester);空=无。
	GateKeeperOf func(taskID, segment string) string
	// RoleOf(principal,task)=principal 在 task 的角色(implementer/tester/designer/qa_owner);""=非成员。
	// result=执行席(implementer/tester)靠此区分于"任一 task_role"(status)。
	RoleOf func(principal, taskID string) string
}

var (
	// stateOps ③闸 op 集(★shence P1:5.1a 矩阵除控制行外全部状态 op——含 gate_result 门/close 收口,
	// 防其绕过授权闸落 gate-4 引擎直路。收口=调度者唯一关闭权、执行席不得绕)。
	stateOps = map[string]bool{
		"new": true, "status": true, "result": true, "reject": true,
		"gate_result": true, "close": true,
	}
	// controlOps ★dev56 MC-B:控制判定仅认结构化 meta.op,不认自然语言正文(防误拒合法 status)。
	controlOps = map[string]bool{"stop": true, "pause": true, "reassign": true, "amend": true, "resume": true, "kill": true}
	// suspectRe:自然语言疑似控制词——★MC-B 仅用于 gate-5 标记,【不】用于 gate-2 拒收。
	suspectRe = regexp.MustCompile(`(停止|暂停|重派|改单|终止|kill|stop|pause)`)
	// statusRe ④闸:状态/门/心跳首行。
	statusRe = regexp.MustCompile(`^【?(里程碑|门|PASS|FAIL|在轨|阻塞)`)
	// gateRe:门 PASS/FAIL 首行(门方 per-段判定)。
	gateRe = regexp.MustCompile(`^【?(门|PASS|FAIL)`)
)

func isExecSeat(role string) bool { return role == "implementer" || role == "tester" }

// Route 对单信封执行五闸(纯函数,零 IO;入账/事件由调用方按 Result 落)。
func (r *Router) Route(e Envelope) Result {
	// ① 身份闸:principal 合 C4 格式;★dev56⑤ 外部禁用 system:* 保留前缀(防伪造哨兵)。
	if strings.HasPrefix(e.Principal, "system:") || !board.IdentityPattern.MatchString(e.Principal) {
		return Result{Verdict: VerdictIdentityRejected, Gate: 1}
	}

	isControl := controlOps[e.MetaOp] // ★MC-B:仅结构化 op 触发控制闸
	isGateLine := gateRe.MatchString(e.FirstLine)
	statusHit := statusRe.FindStringSubmatch(e.FirstLine) != nil && e.TaskID != ""
	// ③闸触发:结构化 state op ∈ stateOps,或门首行,或状态首行(★shence P1:收口/门在 stateOps 必进本闸)。
	isState := stateOps[e.MetaOp] || isGateLine || statusHit
	isEmergency := r.EmergencyIDs[e.Principal]

	// ★item① 过渡窗(principal 不可信):一切 mutating(控制+状态+应急)fail-closed 不入账;仅只读放行。
	// (收口/门/status/new/result/reject 及 control/emergency 全拒;纯只读落 gate-5 判读、非权威。)
	if !r.Trusted && (isControl || isState || isEmergency) {
		return Result{Verdict: VerdictDirectiveRejected, Gate: 2}
	}

	// ② 控制闸(★dev56 MC-B 仅 meta.op∈ControlOps 触发):放行仅【调度者小组】∨【业主集∧走 C6】。
	if isControl {
		if !r.DispatcherIDs[e.Principal] && !isEmergency {
			return Result{Verdict: VerdictDirectiveRejected, Gate: 2}
		}
		return Result{Verdict: VerdictEngineOp, Gate: 2} // 控制 op 放行→引擎控制路由
	}

	// ③ 授权闸(★MC-2+dev56②+shence P1):状态 op 按 5.1a role×op 矩阵逐 (角色,op) 判、越表 fail-closed。
	if isState {
		if !r.authorizeState(e, isGateLine) {
			return Result{Verdict: VerdictDirectiveRejected, Gate: 3}
		}
		// ④ 过授权闸→入账。有结构化 meta.op→引擎既有 op 路由(C1);纯状态/门首行(无 meta.op)→状态机入账(门/心跳→TaskLedger)。
		if e.MetaOp == "" {
			if m := statusRe.FindStringSubmatch(e.FirstLine); m != nil {
				return Result{Verdict: VerdictStatusIngest, Gate: 4, Kind: m[1]}
			}
		}
		return Result{Verdict: VerdictEngineOp, Gate: 4}
	}

	// ⑤ 兜底:升调度者 LLM(只出判读不授权;判读后仍须过授权闸)。
	// ★MC-B:自然语言疑似控制→标记 suspect(要求结构化重投),既不执行也不误拒。
	return Result{Verdict: VerdictLLM, Gate: 5, Suspect: suspectRe.MatchString(e.FirstLine)}
}

// authorizeState 按 C5 5.1a role×op 矩阵逐 (角色,op) 判(★MC-2+dev56②+MC-a+shence P1)。
// 越表 fail-closed(default-deny);"∈任一 task_role"不替代本矩阵。
func (r *Router) authorizeState(e Envelope, isGateLine bool) bool {
	// 门 PASS/FAIL(结构化 gate_result 或门首行):仅该段 C2.1 指定门方(★dev56 MC-a:per-段、非硬编码 tester)。
	if e.MetaOp == "gate_result" || isGateLine {
		if r.GateKeeperOf == nil {
			return false // fail-closed
		}
		gk := r.GateKeeperOf(e.TaskID, e.Segment)
		return gk != "" && gk == e.Principal
	}
	switch e.MetaOp {
	case "new": // 建单=调度者小组
		return r.DispatcherIDs[e.Principal]
	case "close": // ★shence P1 收口=调度者唯一关闭权(执行席不得自行收口)
		return r.DispatcherIDs[e.Principal]
	case "result": // 产出结果=执行席(implementer/tester)
		return r.RoleOf != nil && isExecSeat(r.RoleOf(e.Principal, e.TaskID))
	case "reject": // 调度者小组 或 该段指定门方
		if r.DispatcherIDs[e.Principal] {
			return true
		}
		return r.GateKeeperOf != nil && r.GateKeeperOf(e.TaskID, e.Segment) == e.Principal
	case "status": // status=任一 task_role 成员
		return r.Authz != nil && r.Authz(e.Principal, e.TaskID)
	case "": // 无 meta.op 的状态首行(心跳/在轨/阻塞)=任一 task_role
		return r.Authz != nil && r.Authz(e.Principal, e.TaskID)
	}
	return false // 越表 fail-closed(★B default-deny)
}
