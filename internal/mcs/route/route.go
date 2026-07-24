// Package route 实现调度者收件面的五闸路由(契约 C5;正则先行,LLM 兜底,降无谓调用)。
//
// 次序(C5 5.1,逐闸短路):①身份闸(C4 格式)→②控制闸(控制语义×非授权身份)→
// ③结构闸(信封 meta op)→④状态闸(里程碑/门/心跳首行)→⑤LLM 兜底(升调度者判读)。
// 规则只作用于 meta+首行(不信 body 自称,§6 冻结件同构);规则表版本化,改=改契约须重两签。
package route

import (
	"regexp"

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

// Envelope 收件面输入(只含 meta+首行;body 不参与路由,C5 5.2)。
type Envelope struct {
	From      string // 发件身份(传输层 principal 绑定,非 body 自称)
	MetaOp    string // 信封 meta 的 op(空=无)
	FirstLine string // 正文首行
	TaskID    string // meta 载明的任务号(空=无)
}

// Result 路由结果(附闸位与提取物,供入账/审计)。
type Result struct {
	Verdict Verdict
	Gate    int    // 命中闸位 1-5
	Kind    string // 状态闸提取:里程碑|门|PASS|FAIL|在轨|阻塞(仅 VerdictStatusIngest)
}

// Router 五闸路由器。DispatcherIDs=调度者小组成员身份集(②闸授权名单);
// EmergencyIDs=应急原语合法来源(业主,C3 3.2——②闸唯一例外)。
type Router struct {
	DispatcherIDs map[string]bool
	EmergencyIDs  map[string]bool
}

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
	// ① 身份闸:发件身份必须合 C4 格式
	if !board.IdentityPattern.MatchString(e.From) {
		return Result{Verdict: VerdictIdentityRejected, Gate: 1}
	}
	// ② 控制闸:控制语义 × 非调度者 × 非应急来源 → 拒
	if controlRe.MatchString(e.FirstLine) {
		if !r.DispatcherIDs[e.From] && !r.EmergencyIDs[e.From] {
			return Result{Verdict: VerdictDirectiveRejected, Gate: 2}
		}
		// 调度者/应急来源的控制指令继续向下(结构/状态/兜底按内容归位)
	}
	// ③ 结构闸:meta op 命中引擎集
	if engineOps[e.MetaOp] {
		return Result{Verdict: VerdictEngineOp, Gate: 3}
	}
	// ④ 状态闸:状态首行 ∧ 载明任务号
	if m := statusRe.FindStringSubmatch(e.FirstLine); m != nil && e.TaskID != "" {
		return Result{Verdict: VerdictStatusIngest, Gate: 4, Kind: m[1]}
	}
	// ⑤ 兜底:升调度者 LLM(判读结论由调用方落 route_llm 事件)
	return Result{Verdict: VerdictLLM, Gate: 5}
}
