package store

import (
	"context"
	"database/sql"
	"strings"
)

// QuestionContext is an internal authorization candidate. It has references only,
// not a question body or a grant to read another task or an entire spec.
type QuestionContext struct {
	ID, SpecID, Owner, Editor, SHA, TaskID string
	Revision                               int
	Archived                               bool
	Task                                   *TaskFlowState
}
type QuestionPolicy struct {
	SpecID     string `json:"spec_id"`
	PMUser     string `json:"pm_user"`
	LeaderUser string `json:"leader_user"`
	Version    int    `json:"version"`
	UpdatedBy  string `json:"updated_by"`
	UpdatedAt  string `json:"updated_at"`
}
type PilotQuestion struct {
	ID                     string             `json:"id"`
	SpecID                 string             `json:"spec_id"`
	Revision               int                `json:"revision"`
	SHA                    string             `json:"content_sha256"`
	TaskID                 string             `json:"task_id,omitempty"`
	BlockerID              string             `json:"blocker_id,omitempty"`
	AssignmentVersion      int                `json:"assignment_version,omitempty"`
	Generation             int                `json:"generation"`
	Version                int                `json:"version"`
	Kind                   string             `json:"kind"`
	Title                  string             `json:"title"`
	ObservedFact           string             `json:"observed_fact"`
	Question               string             `json:"question"`
	ExpectedAnswer         string             `json:"expected_answer"`
	InputAnchors           string             `json:"input_anchors"`
	CreatedBy              string             `json:"created_by"`
	PMUser                 string             `json:"pm_user"`
	Responsible            string             `json:"responsible_principal"`
	LeaderUser             string             `json:"leader_user,omitempty"`
	Mode                   string             `json:"mode"`
	State                  string             `json:"state"`
	CreatedAt              string             `json:"created_at"`
	GenerationStartedAt    string             `json:"generation_started_at"`
	DueAt                  string             `json:"due_at"`
	FirstSubstantiveAt     string             `json:"first_substantive_answer_at,omitempty"`
	AnsweredAt             string             `json:"answered_at,omitempty"`
	ResolvedAt             string             `json:"resolved_at,omitempty"`
	PolicyVersion          int                `json:"policy_version"`
	BindingCurrent         bool               `json:"binding_current"`
	RecheckState           string             `json:"recheck_state"`
	TaskStatus             string             `json:"task_status,omitempty"`
	Notifications          []QuestionDelivery `json:"notifications,omitempty"`
	CanAnswer              bool               `json:"can_answer"`
	CanManage              bool               `json:"can_manage"`
	CanConfigureRecipients bool               `json:"can_configure_recipients"`
	AssignmentOptions      []string           `json:"assignment_options,omitempty"`
	CanAssign              bool               `json:"can_assign"`
	CanDecideChange        bool               `json:"can_decide_change"`
	ClarificationRequired  bool               `json:"clarification_required"`
}
type QuestionDelivery struct {
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	Recipient   string `json:"recipient_user"`
	AttemptedAt string `json:"attempted_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
}
type QuestionAnswer struct {
	Authority   string `json:"authority"`
	Sequence    int64  `json:"sequence"`
	Generation  int    `json:"generation"`
	Actor       string `json:"actor"`
	EventID     string `json:"event_id"`
	Kind        string `json:"answer_kind"`
	Body        string `json:"body"`
	Evidence    string `json:"evidence_ref"`
	ScopeChange bool   `json:"scope_change"`
	CreatedAt   string `json:"created_at"`
}
type QuestionChange struct {
	Sequence    int64  `json:"sequence"`
	ID          string `json:"id"`
	State       string `json:"state"`
	Revision    int    `json:"revision"`
	NewRevision int    `json:"new_revision"`
	NewSpecID   string `json:"new_spec_id,omitempty"`
	Reason      string `json:"reason"`
}
type QuestionUpdate struct {
	ClarificationKind    string   `json:"clarification_kind,omitempty"`
	RequestID            string   `json:"request_id"`
	ExpectedVersion      int      `json:"expected_version"`
	Operation            string   `json:"operation"`
	TaskID               string   `json:"task_id,omitempty"`
	Kind                 string   `json:"kind,omitempty"`
	Title                string   `json:"title,omitempty"`
	ObservedFact         string   `json:"observed_fact,omitempty"`
	Question             string   `json:"question,omitempty"`
	ExpectedAnswer       string   `json:"expected_answer,omitempty"`
	InputAnchors         string   `json:"input_anchors,omitempty"`
	EntryRefs            []string `json:"entry_refs,omitempty"`
	Responsible          string   `json:"responsible_principal,omitempty"`
	ProductionOrSecurity bool     `json:"production_or_security,omitempty"`
	EventID              string   `json:"event_id,omitempty"`
	AnswerKind           string   `json:"answer_kind,omitempty"`
	Body                 string   `json:"body,omitempty"`
	Evidence             string   `json:"evidence_ref,omitempty"`
	ScopeChange          bool     `json:"scope_change,omitempty"`
	PMUser               string   `json:"pm_user,omitempty"`
	LeaderUser           string   `json:"leader_user,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	ChangeID             string   `json:"change_id,omitempty"`
	NewRevision          int      `json:"new_revision,omitempty"`
	NewSpecID            string   `json:"new_spec_id,omitempty"`
}
type QuestionResult struct {
	Question            PilotQuestion `json:"question"`
	Outcome             string        `json:"outcome"`
	Replayed            bool          `json:"replayed"`
	ExecutionAuthorized bool          `json:"execution_authorized"`
}

const questionCols = `id,spec_id,revision,content_sha256,task_id,blocker_id,assignment_version,generation,version,kind,title,observed_fact,question_text,expected_answer,input_anchors,created_by,pm_user,responsible_principal,leader_user,mode,state,created_at,generation_started_at,due_at,first_substantive_answer_at,answered_at,resolved_at,policy_version`

func scanQuestion(row rowScanner) (PilotQuestion, error) {
	var q PilotQuestion
	var first, answered, resolved sql.NullString
	err := row.Scan(&q.ID, &q.SpecID, &q.Revision, &q.SHA, &q.TaskID, &q.BlockerID, &q.AssignmentVersion, &q.Generation, &q.Version, &q.Kind, &q.Title, &q.ObservedFact, &q.Question, &q.ExpectedAnswer, &q.InputAnchors, &q.CreatedBy, &q.PMUser, &q.Responsible, &q.LeaderUser, &q.Mode, &q.State, &q.CreatedAt, &q.GenerationStartedAt, &q.DueAt, &first, &answered, &resolved, &q.PolicyVersion)
	q.FirstSubstantiveAt = first.String
	q.AnsweredAt = answered.String
	q.ResolvedAt = resolved.String
	return q, err
}
func questionTerminal(s string) bool { return s == "resolved" || s == "cancelled" || s == "superseded" }
func questionReader(a BlockerActor, c QuestionContext, q PilotQuestion) bool {
	if a.UserID != "" {
		return a.UserID == c.Owner || a.UserID == c.Editor || a.UserID == q.PMUser || "user:"+a.UserID == q.Responsible
	}
	return a.AgentID != "" && c.Task != nil && (a.AgentID == c.Task.Developer || a.AgentID == c.Task.Reviewer || q.Responsible == a.Key())
}
func questionManager(a BlockerActor, c QuestionContext) bool {
	return a.UserID != "" && (a.UserID == c.Owner || a.UserID == c.Editor)
}
func questionAnswerer(a BlockerActor, c QuestionContext, q PilotQuestion) bool {
	return a.UserID != "" && a.UserID == q.PMUser
}
func validQuestionKind(s string) bool {
	return s == "requirement_unclear" || s == "change" || s == "technical" || s == "resource"
}
func validQuestionUser(s string) bool       { return s == "" || validBlockerPrincipal("user:"+s) }
func questionNonempty(s string, n int) bool { return strings.TrimSpace(s) != "" && len(s) <= n }
func taskQuestionChangesClear(ctx context.Context, q taskQuery, task string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pilot_question_change p JOIN console_spec_task t ON t.spec_id=p.spec_id AND t.revision=p.revision WHERE t.id=? AND p.state<>'rejected'`, task).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrTaskFlowConflict
	}
	return nil
}
