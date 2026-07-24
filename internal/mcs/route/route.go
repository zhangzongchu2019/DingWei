// Package route 实现调度者收件面的五闸路由(契约 C5;正则先行,LLM 兜底,降无谓调用)。
//
// 次序(C5 5.1,逐闸短路):①身份闸(C4 格式)→②控制闸(控制语义×非授权身份)→
// ③结构闸(信封 meta op)→④状态闸(里程碑/门/心跳首行)→⑤LLM 兜底(升调度者判读)。
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
	VerdictDirectiveRejected Verdict = "directive_rejected" // ②
	VerdictEngineOp          Verdict = "engine_op"          // ③ 走引擎既有 op 路由
	VerdictStatusIngest      Verdict = "status_ingest"      // ④ 状态机入账(门/里程碑/心跳)
	VerdictLLM               Verdict = "llm"                // ⑤ 升调度者 LLM 判读
)

// Envelope 收件面输入(只含 meta+canonical 首行;body 不参与路由,C5 5.2)。
type Envelope struct {
	// Principal=传输层鉴权后的 per-session 主体(★C5 5.0 MC-1:【非】信封自述 from,
	// from 在共享队钥下可伪造)。gate 全读此字段。过渡期 hub 未注入=空→高危通道整体关闭(见 Router.Trusted)。
	Principal string
	MetaOp    string // 信封 meta 的 op(可信 control op 仅此结构化字段,C5 5.0a)
	FirstLine string // canonical 规范化首行(剥传输头/镜像标签/引号,C5 5.0a)
	TaskID    string // meta 载明的任务号(空=无)
}

// Result 路由结果(附闸位与提取物,供入账/审计)。
type Result struct {
	Verdict Verdict
	Gate    int    // 命中闸位 1-5
	Kind    string // 状态闸提取:里程碑|门|PASS|FAIL|在轨|阻塞(仅 VerdictStatusIngest)
}

// InTaskRole 授权闸(★C5 gate-3 MC-2):principal 是否 ∈ 目标 task 的 task_role。
// 由调用方注入(读 Board task_role);nil=拒绝一切状态 op(fail-closed)。
type InTaskRole func(principal, taskID string) bool

// Router 五闸路由器。
//   - DispatcherIDs:调度者小组成员(②闸授权+new 建单权);
//   - EmergencyIDs:应急原语合法来源(业主,C6);
//   - Trusted:C5 5.0 principal 是否可信(hub per-session 就位=true;过渡期 false→控制/应急通道整体关闭);
//   - Authz:授权闸(C5 gate-3)。
type Router struct {
	DispatcherIDs map[string]bool
	EmergencyIDs  map[string]bool
	Trusted       bool
	Authz         InTaskRole
	// TesterOf(task)=该 task 指定门方 principal(★dev56② role×op:门 PASS/FAIL 仅 tester 发);空=无。
	TesterOf func(taskID string) string
}

var gateRe = regexp.MustCompile(`^【?(门|PASS|FAIL)`)

func isGate(first string) bool { return gateRe.MatchString(first) }

var (
	// engineOps ③闸:引擎既有 op 集(§6 冻结件)。
	engineOps = map[string]bool{"new": true, "status": true, "result": true, "reject": true}
	// controlRe ②闸:控制语义词(首行或 meta op 表达的增/改/停/暂停/重派/改单)。
	controlRe = regexp.MustCompile(`^【?(停止|暂停|重派|改单|终止|恢复任务|kill|stop|pause|reassign)`)
	// statusRe ④闸:状态/门/心跳首行。
	statusRe = regexp.MustCompile(`^【?(里程碑|门|PASS|FAIL|在轨|阻塞)`)
)

// Route 对单信封执行五闸(纯函数,零 IO;入账/事件由调用方按 Result 落)。
func (r *Router) Route(e Envelope) Result {
	// ① 身份闸:principal 合 C4 格式;★dev56⑤ 外部禁用 system:* 保留前缀(防伪造哨兵)
	if strings.HasPrefix(e.Principal, "system:") || !board.IdentityPattern.MatchString(e.Principal) {
		return Result{Verdict: VerdictIdentityRejected, Gate: 1}
	}
	isControl := controlRe.MatchString(e.FirstLine)
	// 过渡期(principal 不可信):高危通道整体关闭——控制/应急一律拒(★C5 5.0 不带病放行)
	if !r.Trusted && (isControl || r.EmergencyIDs[e.Principal]) {
		return Result{Verdict: VerdictDirectiveRejected, Gate: 2}
	}
	// ② 控制闸:控制语义 × 非调度者 × 非应急来源 → 拒
	if isControl && !r.DispatcherIDs[e.Principal] && !r.EmergencyIDs[e.Principal] {
		return Result{Verdict: VerdictDirectiveRejected, Gate: 2}
	}
	// ③ 授权闸(★MC-2):状态/结果类 op 或状态首行——principal 必须 ∈ 目标 task_role(new=调度者小组)
	statusHit := statusRe.FindStringSubmatch(e.FirstLine) != nil && e.TaskID != ""
	if engineOps[e.MetaOp] || statusHit {
		if e.MetaOp == "new" {
			if !r.DispatcherIDs[e.Principal] {
				return Result{Verdict: VerdictDirectiveRejected, Gate: 3}
			}
		} else if isGate(e.FirstLine) {
			// ★dev56② 门 PASS/FAIL 仅指定门方(tester)可发
			if r.TesterOf == nil || r.TesterOf(e.TaskID) != e.Principal {
				return Result{Verdict: VerdictDirectiveRejected, Gate: 3}
			}
		} else if r.Authz == nil || !r.Authz(e.Principal, e.TaskID) {
			return Result{Verdict: VerdictDirectiveRejected, Gate: 3}
		}
		// ④ 过授权闸→入账
		if engineOps[e.MetaOp] {
			return Result{Verdict: VerdictEngineOp, Gate: 4}
		}
		m := statusRe.FindStringSubmatch(e.FirstLine)
		return Result{Verdict: VerdictStatusIngest, Gate: 4, Kind: m[1]}
	}
	// ⑤ 兜底:升调度者 LLM(只出判读不授权;判读后仍须过授权闸)
	return Result{Verdict: VerdictLLM, Gate: 5}
}
