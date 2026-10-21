package store

import (
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
)

var ErrClarificationUpgradeRequired = fmt.Errorf("clarification requires typed anchored tools: %w", ErrTaskFlowPermission)

// ClarificationPolicy grants only discussion rights. It does not grant access to
// a whole spec, permit editing final content, or authorize execution/signoff.
type ClarificationPolicy struct {
	SpecID     string `json:"spec_id"`
	PMUser     string `json:"pm_user"`
	DeputyUser string `json:"deputy_user"`
	Version    int    `json:"version"`
	UpdatedBy  string `json:"updated_by"`
	UpdatedAt  string `json:"updated_at"`
}

type ClarificationPolicyUpdate struct {
	ExpectedVersion int    `json:"expected_version"`
	PMUser          string `json:"pm_user"`
	DeputyUser      string `json:"deputy_user"`
}

// No actor, provider identity, transport, or authority is accepted from clients.
type ClarificationUpdate struct {
	RequestID string   `json:"request_id"`
	Files     []string `json:"file_ids,omitempty"`
	Kind      string   `json:"kind"`
	EntryRefs []string `json:"entry_refs"`
	Body      string   `json:"body"`
	Evidence  string   `json:"evidence_ref"`
	Corrects  int64    `json:"corrects,omitempty"`
	Confirms  int64    `json:"confirms,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type ClarificationPost struct {
	Images        []clarificationimage.File `json:"images,omitempty"`
	SpecID        string                    `json:"spec_id"`
	Floor         int64                     `json:"floor"`
	Revision      int                       `json:"revision"`
	SHA           string                    `json:"content_sha256"`
	QuestionID    string                    `json:"question_id"`
	Actor         string                    `json:"actor"`
	EventID       string                    `json:"event_id"`
	Role          string                    `json:"role"`
	Kind          string                    `json:"kind"`
	EntryRefs     []string                  `json:"entry_refs"`
	Body          string                    `json:"body"`
	Evidence      string                    `json:"evidence_ref"`
	Corrects      int64                     `json:"corrects,omitempty"`
	Confirms      int64                     `json:"confirms,omitempty"`
	Reason        string                    `json:"reason,omitempty"`
	Source        string                    `json:"source"`
	PreviousFloor int64                     `json:"previous_floor,omitempty"`
	PolicyVersion int                       `json:"policy_version"`
	CreatedAt     string                    `json:"created_at"`
}

type ClarificationResult struct {
	Post                ClarificationPost `json:"post"`
	Replayed            bool              `json:"replayed"`
	ExecutionAuthorized bool              `json:"execution_authorized"`
}

type ClarificationThread struct {
	Posts               []ClarificationPost  `json:"posts"`
	NextBefore          int64                `json:"next_before,omitempty"`
	ExecutionAuthorized bool                 `json:"execution_authorized"`
	Context             ClarificationContext `json:"context"`
}

// This is the necessary question excerpt, not a grant to its private D3
// history, observed facts/attachments, or the entire spec/task.
type ClarificationContext struct {
	Actor            string   `json:"actor"`
	SpecID           string   `json:"spec_id"`
	Revision         int      `json:"revision"`
	SHA              string   `json:"content_sha256"`
	QuestionID       string   `json:"question_id"`
	Title            string   `json:"title"`
	Question         string   `json:"question"`
	ExpectedAnswer   string   `json:"expected_answer"`
	Role             string   `json:"role"`
	EntryRefs        []string `json:"entry_refs"`
	AnchorsTruncated bool     `json:"anchors_truncated"`
	Archived         bool     `json:"archived"`
	PolicyVersion    int      `json:"policy_version"`
}
