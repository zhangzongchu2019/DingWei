package datad

// M5 A-lite 最小原子派发层(契约 61e5938 §1-§5):assignment 当代行 partial-unique 当代唯一
// 双角色 + assignment_generation + expected_generation CAS + request_id 首果账 + owner_session
// 单权威 + 全错误码双射。不含竞争认领(B 案单写);与 /pull 白名单共存(§5⑥)。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
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
	ErrInvalidRequest  = "invalid_request"  // 422 请求形非法(契约 delta v1.8:request_id 缺失/非 UUID 形、负 expected_generation、force 空 reason)
)

// assignCodeHTTP 机器码→HTTP 双射(§3 PG 表驱动,此处代码驱动同源)。
var assignCodeHTTP = map[string]int{
	ErrHalfAssignment: 422, ErrSelfReview: 422, ErrWrongStateA: 409, ErrStaleGen: 412,
	ErrUnknownAssignee: 422, ErrRequeueState: 409, ErrStoreDownA: 503,
	ErrOwnerConflict: 409, ErrRequestConflict: 409, ErrInvalidRequest: 422,
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

// invalid_request 子因(契约 v1.8 D1:单码盖多输入条件,区分力由 sub 补,A13 逐子因验收)。
const (
	SubRequestIDMalformed = "request_id_malformed" // 不入首果账(D4:幂等键非法,PRIMARY KEY 不可满足)
	SubNegativeGeneration = "negative_generation"
	SubForceReasonMissing = "force_reason_missing"
)

// AssignErr A-lite 错误(机器码+HTTP+子因)=错误单一源 DTO:Mem/PG 产出、HTTP 映射、失败首果
// body 均用它,消除三处各自表示错误语义的分叉(R1②请求与错误双射)。
type AssignErr struct {
	Code string `json:"code"`
	HTTP int    `json:"http"`
	Msg  string `json:"msg"`
	Sub  string `json:"sub,omitempty"` // invalid_request 子因(v1.8 D1),其余码空
}

func (e *AssignErr) Error() string { return e.Code + ": " + e.Msg }

func aerr(code, msg string) *AssignErr {
	h := assignCodeHTTP[code]
	if h == 0 {
		h = 500
	}
	return &AssignErr{Code: code, HTTP: h, Msg: msg}
}

// aerrSub invalid_request 带子因(v1.8 D1)。
func aerrSub(code, sub, msg string) *AssignErr {
	e := aerr(code, msg)
	e.Sub = sub
	return e
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

// canonicalRequestHash 完整请求体 canonical(jcs 整数子集,与 B-lite 同实现)sha256。
// R2-1 修:①覆盖完整请求体含 request_id(缺则同 id 异参可漏检);②fail-closed——canonical
// 错误(invalid UTF-8 等)返 invalid_request,绝不吞错 hash 空字节(吞错=多个坏请求同 hash
// =幂等账串号)。归一化形:全字段恒在(force/reason 含零值),expected_generation 仅非 nil
// 时在场——首写与重放同一函数计算,自身一致。payload 为类型化 struct 无浮点。
func canonicalRequestHash(req *AssignRequest) (string, *AssignErr) {
	m := map[string]any{
		"request_id": req.RequestID,
		"executor":   req.Executor, "reviewer": req.Reviewer,
		"force": req.Force, "reason": req.Reason,
	}
	if req.ExpectedGeneration != nil {
		m["expected_generation"] = *req.ExpectedGeneration
	}
	buf, err := jcs.Canonicalize(m)
	if err != nil {
		return "", aerr(ErrInvalidRequest, "请求体无法 canonical 化: "+err.Error())
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// uuidRe request_id 冻结格式=RFC 4122 UUID 形(契约 §1"调用方 UUID"机械化,delta v1.8③;
// 大小写不敏感)。
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validRequestID request_id 形校验。非法=invalid_request(422);此变体**不入首果账**
// (幂等键自身非法无法记账,delta v1.8④)。
func validRequestID(id string) *AssignErr {
	if id == "" {
		return aerrSub(ErrInvalidRequest, SubRequestIDMalformed, "request_id 必填(UUID 形)")
	}
	if !uuidRe.MatchString(id) {
		return aerrSub(ErrInvalidRequest, SubRequestIDMalformed, "request_id 非 UUID 形(RFC 4122)")
	}
	return nil
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

// validAssignRequest 请求体基础校验(§2 步骤 1 + N3 + delta v1.8)。request_id 形校验不在
// 此(validRequestID,先于首果账查且不记账);此处各错均先记首果再返(§2 步骤 5)。
func validAssignRequest(req *AssignRequest) *AssignErr {
	if req.Executor == "" || req.Reviewer == "" {
		return aerr(ErrHalfAssignment, "executor+reviewer 双角色齐(不派半件)")
	}
	if req.Executor == req.Reviewer {
		return aerr(ErrSelfReview, "executor≠reviewer")
	}
	if req.Force && req.Reason == "" {
		return aerrSub(ErrInvalidRequest, SubForceReasonMissing, "force ⇒ reason 必填(N3 越闸必留因;v1.8 D2,HTTP 逐字 422)")
	}
	if req.ExpectedGeneration != nil && *req.ExpectedGeneration < 0 {
		return aerrSub(ErrInvalidRequest, SubNegativeGeneration, "expected_generation 不得为负(v1.8 D1)")
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

// assignAuditDetail 审计 detail(R2-4:完整绑定请求与结果;成功/业务失败与 assignment+
// 首果同原子边界落账,由 Mem 锁内/PG 事务内写,handler 只记认证 denied)。
type assignAuditDetail struct {
	TaskID     string `json:"task_id"`
	Generation *int64 `json:"generation,omitempty"` // 成功=新代;失败=nil
	Executor   string `json:"executor"`
	Reviewer   string `json:"reviewer"`
	RequestID  string `json:"request_id"`
	Force      bool   `json:"force"`
	Reason     string `json:"reason,omitempty"`
	Outcome    string `json:"outcome"`       // success | <机器码>
	Sub        string `json:"sub,omitempty"` // invalid_request 子因(v1.8 D1)
}

func assignAuditRow(actor, taskID string, req *AssignRequest, gen *int64, outcome, sub string) AuditEntry {
	d := assignAuditDetail{TaskID: taskID, Generation: gen, Executor: req.Executor,
		Reviewer: req.Reviewer, RequestID: req.RequestID, Force: req.Force, Reason: req.Reason, Outcome: outcome, Sub: sub}
	raw, _ := json.Marshal(d)
	op := "assign"
	if outcome != "success" {
		op = "assign." + outcome
	}
	return AuditEntry{Actor: actor, Op: op, Target: taskID, Detail: raw, At: time.Now().UTC()}
}

// Assigner Store 的 A-lite 派发面(coordinator-only 由 handler 鉴权)。
type Assigner interface {
	// Assign 原子 AssignGeneration(§2 单事务);幂等 request_id 首果账(§3)。
	// actor=真实调用方 Identity.Session(R2-4:audit 真身份,与业务同原子边界落账)。
	Assign(ctx context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool, actor string) (*AssignResult, *AssignErr)
	// GetFirstResult 首果账查(幂等重放)。
	GetFirstResult(ctx context.Context, requestID string) (*FirstResult, error)
}
