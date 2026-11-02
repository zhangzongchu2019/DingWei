// Package taskdispatch 定义仓库任务编号锁，不复用旧UUID任务的分配语义。
package taskdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	LeaseDuration = time.Hour
	RenewInterval = 15 * time.Minute
	PollInterval  = time.Minute
	NoticeBudget  = 5 * time.Second
)

var (
	ErrInvalid     = errors.New("r10_invalid")
	ErrForbidden   = errors.New("r10_forbidden")
	ErrConflict    = errors.New("r10_conflict")
	ErrClock       = errors.New("clock_invalid")
	ErrUnavailable = errors.New("r10_unavailable")
	ErrUnknown     = errors.New("r10_outcome_unknown")
	ErrNeedsSync   = errors.New("r10_needs_sync")
	ErrComparison  = errors.New("comparison_unknown")
	ErrNotFound    = errors.New("r10_not_found")
	ErrPRRequired  = errors.New("r10_register_pr_first")
	ErrCompleted   = errors.New("r10_task_completed")
	shaRE          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repositoryRE   = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	taskRE         = regexp.MustCompile(`^T-[0-9]{2,30}$`)
	changeRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	operationRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

// Key不含commit、ref、holder或group，跨版本必须争同一把锁。
type Key struct {
	Provider   string `json:"provider"`
	Instance   string `json:"instance"`
	Repository string `json:"repository"`
	ChangeDir  string `json:"change_dir"`
	TaskID     string `json:"task_id"`
}

func (k Key) Validate() error {
	name := strings.TrimPrefix(k.ChangeDir, "openspec/changes/")
	if k.Provider != "github" || k.Instance != "github.com" || !repositoryRE.MatchString(k.Repository) ||
		!strings.HasPrefix(k.ChangeDir, "openspec/changes/") || !changeRE.MatchString(name) || name == "archive" ||
		path.Clean(k.ChangeDir) != k.ChangeDir || !taskRE.MatchString(k.TaskID) {
		return ErrInvalid
	}
	return nil
}

func (k Key) Digest() string {
	b, _ := json.Marshal(k)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ValidSHA(s string) bool       { return shaRE.MatchString(s) }
func ValidOperation(s string) bool { return operationRE.MatchString(s) }

type Handoff struct {
	Commit          string `json:"commit"`
	Path            string `json:"path"`
	BlobSHA         string `json:"blob_sha"`
	ExecutionCommit string `json:"execution_commit"`
	TaskID          string `json:"task_id"`
	RecoveryState   string `json:"recovery_state"`
}

func (h Handoff) ValidFor(k Key, execution string) bool {
	return ValidSHA(h.Commit) && ValidSHA(h.BlobSHA) && h.ExecutionCommit == execution && h.TaskID == k.TaskID &&
		path.Clean(h.Path) == h.Path && strings.HasPrefix(h.Path, k.ChangeDir+"/") && strings.HasSuffix(h.Path, ".md") &&
		!strings.ContainsAny(h.Path, "\\%\x00\r\n\t?#:")
}

type Lock struct {
	Key
	State              string    `json:"state"`
	Generation         int64     `json:"generation"`
	LeaseToken         string    `json:"lease_token,omitempty"`
	HolderAgentID      string    `json:"holder_agent_id"`
	HolderOwnerLC      string    `json:"holder_owner_lc"`
	AssignedByUserLC   string    `json:"assigned_by_user_id_lc"`
	AuthorizationGroup string    `json:"authorization_group_code"`
	CommitSHA          string    `json:"commit_sha"`
	TasksBlobSHA       string    `json:"tasks_blob_sha"`
	TaskLine           int       `json:"task_line"`
	GrantedAt          time.Time `json:"granted_at_utc"`
	ExpiresAt          time.Time `json:"expires_at_utc"`
	LastRenewedAt      time.Time `json:"last_renewed_at_utc"`
	UpdatedAt          time.Time `json:"updated_at_utc"`
	MonitorRef         string    `json:"monitor_ref"`
	PullRequestID      int64     `json:"pull_request_id"`
	PRDimension        string    `json:"pr_dimension"`
	PRHeadSHA          string    `json:"pr_head_sha"`
	PRRemoteState      string    `json:"pr_remote_state"`
	PRBaseRef          string    `json:"pr_base_ref"`
	PRHeadRef          string    `json:"pr_head_ref"`
	PRUpdatedAt        time.Time `json:"pr_updated_at_utc"`
	ComparisonState    string    `json:"comparison_state"`
	SyncRequired       bool      `json:"sync_required"`
	ObservedCommitSHA  string    `json:"observed_commit_sha"`
	LastAttemptAt      time.Time `json:"last_attempt_at_utc"`
	LastCheckedAt      time.Time `json:"last_checked_at_utc"`
	ComparisonError    string    `json:"comparison_error_class"`
	Handoff            Handoff   `json:"handoff"`
}

func (l Lock) Effective(now time.Time) bool {
	return l.State == "active" && !now.Before(l.GrantedAt) && now.Before(l.ExpiresAt)
}

func (l Lock) Redacted() Lock { l.LeaseToken = ""; return l }

// BindingFact仅保存已有R5核权结果的非秘密字段，用于同一SQLite事务重核绑定未变。
// 身份来源仍是PG当前用户/Agent，不能从console_principal反推主体。
type BindingFact struct {
	Principal string `json:"principal"`
	Provider  string `json:"provider"`
	Instance  string `json:"instance"`
	AccountID string `json:"account_id"`
	Login     string `json:"login"`
}

// Mutation由可信服务组装；HTTP请求不能直接解码到此类型。
type Mutation struct {
	OperationID        string
	Kind               string
	Key                Key
	ExpectedGeneration int64
	ExpectedToken      string
	ActorKind          string
	ActorUserLC        string
	ActorAgentID       string
	ActorOwnerLC       string
	HolderAgentID      string
	HolderOwnerLC      string
	AssignedByUserLC   string
	AuthorizationGroup string
	CommitSHA          string
	TasksBlobSHA       string
	TaskLine           int
	MonitorRef         string
	PullRequestID      int64
	PRDimension        string
	PRHeadSHA          string
	PRRemoteState      string
	PRBaseRef          string
	PRHeadRef          string
	PRUpdatedAt        time.Time
	ComparisonState    string
	ObservedCommitSHA  string
	ComparisonError    string
	Handoff            Handoff
	Bindings           []BindingFact
}

type Result struct {
	OperationID string `json:"operation_id"`
	Outcome     string `json:"outcome"`
	Lock        Lock   `json:"lock"`
	Replay      bool   `json:"replay"`
}

type Operation struct {
	ID           string    `json:"operation_id"`
	Key          Key       `json:"key"`
	Kind         string    `json:"kind"`
	ActorKind    string    `json:"actor_kind"`
	ActorUserLC  string    `json:"actor_user_id_lc"`
	ActorAgentID string    `json:"actor_agent_id"`
	ActorOwnerLC string    `json:"actor_owner_lc"`
	Result       Result    `json:"result"`
	CreatedAt    time.Time `json:"created_at_utc"`
	CompletedAt  time.Time `json:"completed_at_utc"`
}

type Event struct {
	ID           int64     `json:"id"`
	OperationID  string    `json:"operation_id"`
	Key          Key       `json:"key"`
	Generation   int64     `json:"generation"`
	Type         string    `json:"event_type"`
	ActorKind    string    `json:"actor_kind"`
	ActorUserLC  string    `json:"actor_user_id_lc"`
	ActorAgentID string    `json:"actor_agent_id"`
	ActorOwnerLC string    `json:"actor_owner_lc"`
	At           time.Time `json:"at_utc"`
	Detail       string    `json:"detail"`
}

type Notice struct {
	ID               string    `json:"id"`
	OperationID      string    `json:"operation_id"`
	Key              Key       `json:"key"`
	Generation       int64     `json:"generation"`
	RecipientKind    string    `json:"recipient_kind"`
	RecipientID      string    `json:"recipient_id"`
	RecipientOwnerLC string    `json:"recipient_owner_lc"`
	Type             string    `json:"event_type"`
	State            string    `json:"delivery_state"`
	SourceEnvelopeID string    `json:"source_envelope_id"`
	DeliveryID       string    `json:"delivery_id"`
	CreatedAt        time.Time `json:"created_at_utc"`
	CompletedAt      time.Time `json:"completed_at_utc"`
	Reason           string    `json:"reason"`
}

// Repository是新增可选能力，不扩写旧Repository或改变旧派工接口。
type Repository interface {
	TaskDispatchLock(context.Context, Key) (*Lock, error)
	TaskDispatchActive(context.Context) ([]Lock, error)
	TaskDispatchEvents(context.Context, Key, int64) ([]Event, error)
	TaskDispatchNotices(context.Context, string, int) ([]Notice, error)
	TaskDispatchNotice(context.Context, string) (*Notice, error)
	TaskDispatchOperation(context.Context, Key, string) (*Operation, error)
	ApplyTaskDispatch(context.Context, Mutation, func() time.Time, func() error) (Result, error)
	UpdateTaskDispatchNotice(context.Context, string, string, string, string, string, time.Time) (bool, error)
}
