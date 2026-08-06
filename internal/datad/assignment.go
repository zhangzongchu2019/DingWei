package datad

// M5 A-lite 最小原子派发层(契约 61e5938 §1-§5):assignment 当代行 partial-unique 当代唯一
// 双角色 + assignment_generation + expected_generation CAS + request_id 首果账 + owner_session
// 单权威 + 全错误码双射。不含竞争认领(B 案单写);与 /pull 白名单共存(§5⑥)。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/jcs"
)

// AssignRole 当代 assignment 角色(§1 CHECK:无 advisor,advisor 只存 dispatch 历史流水)。
const (
	RoleExecutor = "executor"
	RoleReviewer = "reviewer"
)

// A-lite 错误机器码(契约 §3 双射;code→HTTP 由 assignCodeHTTP)。
const (
	ErrHalfAssignment  = "half_assignment"  // 422 双角色缺一
	ErrSelfReview      = "self_review"      // 422 executor==reviewer
	ErrWrongStateA     = "wrong_state"      // 409 task 非可派状态(force 可越)
	ErrStaleGen        = "stale_generation" // 412 expected_generation 不符
	ErrUnknownAssignee = "unknown_assignee" // 422 assignee 不在注册表
	ErrRequeueState    = "requeue_state"    // 409 重派目标在途须先 drain(force 不越)
	ErrStoreDownA      = "store_down"       // 503 fail-closed
	ErrOwnerConflict   = "owner_conflict"   // 409 N-A2 PUT 改 owner≠当代 executor
	ErrRequestConflict = "request_conflict" // 409 N4 同 request_id 换参
)

// assignCodeHTTP 机器码→HTTP 双射(§3 PG 表驱动,此处代码驱动同源)。
var assignCodeHTTP = map[string]int{
	ErrHalfAssignment: 422, ErrSelfReview: 422, ErrWrongStateA: 409, ErrStaleGen: 412,
	ErrUnknownAssignee: 422, ErrRequeueState: 409, ErrStoreDownA: 503,
	ErrOwnerConflict: 409, ErrRequestConflict: 409,
}

// Assignment 当代/历史派发行(§1 assignment 表)。active=true 即当代。
type Assignment struct {
	TaskID     string    `json:"task_id"`
	Role       string    `json:"role"`
	Assignee   string    `json:"assignee"`
	Generation int64     `json:"generation"`
	Active     bool      `json:"active"`
	RequestID  string    `json:"request_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// AssignRequest POST /v1/tasks/{id}/assign 请求体(§2)。
type AssignRequest struct {
	Executor           string `json:"executor"`
	Reviewer           string `json:"reviewer"`
	RequestID          string `json:"request_id"`
	ExpectedGeneration *int64 `json:"expected_generation,omitempty"` // 重派带;首派缺省(N1)
	Force              bool   `json:"force,omitempty"`
	Reason             string `json:"reason,omitempty"` // force ⇒ 必填(N3)
}

// AssignResult 成功首果(§5④原样返 body)。
type AssignResult struct {
	TaskID     string `json:"task_id"`
	Generation int64  `json:"generation"`
	Executor   string `json:"executor"`
	Reviewer   string `json:"reviewer"`
	Status     string `json:"status"`
}

// AssignErr A-lite 错误(机器码+HTTP)=错误单一源 DTO:Mem/PG 产出、HTTP 映射、失败首果
// body 均用它,消除三处各自表示错误语义的分叉(R1②请求与错误双射)。
type AssignErr struct {
	Code string `json:"code"`
	HTTP int    `json:"http"`
	Msg  string `json:"msg"`
}

func (e *AssignErr) Error() string { return e.Code + ": " + e.Msg }

func aerr(code, msg string) *AssignErr {
	h := assignCodeHTTP[code]
	if h == 0 {
		h = 500
	}
	return &AssignErr{Code: code, HTTP: h, Msg: msg}
}

// failFirstBody 失败首果 body=错误 DTO 的 JSON(R1③:重放保真,原样返 Code/HTTP/Msg,不丢 Msg)。
func failFirstBody(e *AssignErr) json.RawMessage {
	b, _ := json.Marshal(e)
	return b
}

// replayErr 从失败首果重建 AssignErr:body 为错误 DTO 则原样返(保真);无 body(旧账)退化
// outcome+http_code。单一源=AssignErr,Mem/PG/HTTP 同套(R1②双射不再各自重构丢字段)。
func replayErr(outcome string, httpCode int, body json.RawMessage) *AssignErr {
	if len(body) > 0 {
		e := &AssignErr{}
		if json.Unmarshal(body, e) == nil && e.Code != "" {
			return e
		}
	}
	return &AssignErr{Code: outcome, HTTP: httpCode, Msg: "重放首果"}
}

// canonicalRequestHash 请求体 canonical(JCS RFC 8785 同款:字段字典序/UTF-8)sha256
// (N4:检测同 request_id 换参;与 B-lite 同规范化,两端一致防误报 409)。
func canonicalRequestHash(req *AssignRequest) string {
	m := map[string]any{"executor": req.Executor, "reviewer": req.Reviewer, "force": req.Force, "reason": req.Reason}
	if req.ExpectedGeneration != nil {
		m["expected_generation"] = *req.ExpectedGeneration
	}
	// 真 RFC 8785 JCS canonical(公共包 internal/jcs,与 B-lite checkpoint id 同一套实现,
	// 由 jcs 包官方向量钉死;不再手搓 key=value 自造格式)。payload 无浮点,canonical 不返错;
	// 防御性:极端返错则退化为空 canonical 的 hash(不 panic,首果账仍幂等一致)。
	buf, err := jcs.Canonicalize(m)
	if err != nil {
		buf = nil
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// FirstResult 首果账行(§1 assign_first_result)。
type FirstResult struct {
	RequestID   string
	Outcome     string // success | <机器码>
	HTTPCode    int
	RequestHash string
	Body        json.RawMessage
	TaskID      string
	CreatedAt   time.Time
}

// validAssignRequest 请求体基础校验(§2 步骤 1 + N3)。
func validAssignRequest(req *AssignRequest) *AssignErr {
	if req.Executor == "" || req.Reviewer == "" {
		return aerr(ErrHalfAssignment, "executor+reviewer 双角色齐(不派半件)")
	}
	if req.Executor == req.Reviewer {
		return aerr(ErrSelfReview, "executor≠reviewer")
	}
	if req.Force && req.Reason == "" {
		return aerr(ErrWrongStateA, "force ⇒ reason 必填(422 越闸留因)") // 用 422 语义:实际 handler 判 reason 空前置
	}
	return nil
}

// assignReadyState task 状态可派判定(§2 步骤 4)。首派=proposed;重派=dispatched/in_progress(须 expected_generation);
// blocked/done/archived/review/waiting_gate=wrong_state(force 可越,但 in_progress 在途=requeue_state force 不越)。
func assignStateGate(status string, force bool, hasExpectedGen bool) (repl bool, e *AssignErr) {
	switch status {
	case "proposed", "dispatched":
		return status == "dispatched", nil // dispatched=重派
	case "in_progress", "review", "waiting_gate":
		// 在途:重派须先 drain,force 不越(N2 特权不跳在途安全)
		return true, aerr(ErrRequeueState, "task 在途("+status+")须先 drain 再重派;force 不越")
	default: // blocked/done/archived
		if force {
			return false, nil // force 越 wrong_state
		}
		return false, aerr(ErrWrongStateA, "task 非可派状态:"+status)
	}
}

// Assigner Store 的 A-lite 派发面(coordinator-only 由 handler 鉴权)。
type Assigner interface {
	// Assign 原子 AssignGeneration(§2 单事务);幂等 request_id 首果账(§3)。
	Assign(ctx context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool) (*AssignResult, *AssignErr)
	// GetFirstResult 首果账查(幂等重放)。
	GetFirstResult(ctx context.Context, requestID string) (*FirstResult, error)
}
