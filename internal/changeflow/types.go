// Package changeflow implements provider-neutral R1 risk reminders and R8 evidence gates.
// Providers must fence current per-principal authorization through the commit callback.
package changeflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

var (
	ErrInvalid     = errors.New("invalid change evidence")
	ErrForbidden   = errors.New("repository access denied")
	ErrUnavailable = errors.New("provider or evidence unavailable")
	ErrConflict    = errors.New("change operation conflict")
)

type Blocked struct{ Reason string }

func (e *Blocked) Error() string { return e.Reason }
func block(s string) error       { return &Blocked{s} }

var token = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Coordinate struct {
	Provider string `json:"provider"`
	specconfirm.Coordinate
	ChangeID string `json:"change_id"` // stable provider MR/PR identity, not a URL/ref
}

func (c Coordinate) Validate() error {
	if (c.Provider != "github" && c.Provider != "gongfeng") || c.Coordinate.Validate() != nil || !token.MatchString(c.ChangeID) {
		return ErrInvalid
	}
	return nil
}
func (c Coordinate) Key() string { return digest(c) }
func digest(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func reference(v string) bool {
	if len(v) > 2048 || strings.ContainsAny(v, "\r\n\x00") {
		return false
	}
	for _, p := range []string{"https://", "git:", "report:", "log:"} {
		if strings.HasPrefix(v, p) && len(v) > len(p) {
			return true
		}
	}
	return false
}

type Review struct {
	Actor, Commit, Decision, Evidence string
	Valid                             bool
}
type PMResponse struct {
	Point, Commit, Decision, Evidence string
	Valid                             bool
}
type Committee struct {
	Commit, Evidence  string
	Valid, Authorized bool
}
type Task struct {
	ID, Actor, Summary, Commit, Evidence string
	Complete                             bool
}
type Release struct {
	ID, Environment, Commit, Evidence string
	Coordinate                        Coordinate
	Success, Verified                 bool
}

// All Known flags certify complete sets. Nil/empty with Known=false never means zero.
// Snapshot contains source only inside the provider fence; it is never returned to Console/comments.
type Snapshot struct {
	Coordinate                  Coordinate
	Owner, CurrentHead, Version string
	Input                       RiskInput
	Rules                       Rules
	LabelsKnown, ProposalKnown  bool
	Labels                      []string
	ProposalCommittee           bool
	ReviewsKnown                bool
	RequiredReviewers           []string
	Reviews                     []Review
	PMKnown                     bool
	PMPoints                    []string
	Responses                   []PMResponse
	CommitteeKnown              bool
	Committee                   Committee
	TasksKnown                  bool
	Tasks                       []Task
	ScenariosKnown              bool
	Scenarios                   []string
	ApplicabilityKnown          bool
	Applicable                  map[string]bool
	Release                     Release
	MergedAt                    time.Time
	MergeEvidence               string
}

func (s Snapshot) valid(c Coordinate) error {
	if c.Validate() != nil || s.Coordinate != c || !sha.MatchString(s.CurrentHead) || s.Version == "" {
		return ErrUnavailable
	}
	return nil
}
func (s Snapshot) applicable(commit string) bool {
	return sha.MatchString(commit) && (commit == s.Coordinate.Commit || (s.ApplicabilityKnown && s.Applicable[commit]))
}

// Reader performs complete reads and idempotent comment upserts. Unknown writes
// must be reconciled by operation key before retrying; adapters cannot blindly POST.
type Reader interface {
	Read(context.Context, Coordinate) (Snapshot, error)
	UpsertComment(context.Context, Comment) (CommentReceipt, error)
}

// WithAccess must validate controlled repository/instance, current user ACL and
// the requested action (read/detect/ack/result/advance/tool/comment independently).
// It invokes fn synchronously exactly once, preserving authorization/fact fencing
// through the storage transaction. Service-account access is insufficient.
type Provider interface {
	WithAccess(context.Context, string, Coordinate, string, func(Reader) error) error
}
type GitHub struct{}
type GongFeng struct{}

func (GitHub) WithAccess(context.Context, string, Coordinate, string, func(Reader) error) error {
	return ErrUnavailable
}
func (GongFeng) WithAccess(context.Context, string, Coordinate, string, func(Reader) error) error {
	return ErrUnavailable
}

type Hit struct {
	Category string `json:"category"`
	Rule     string `json:"rule"`
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
}
type Acknowledgement struct {
	Actor, Revision string
	At              time.Time
}
type Delivery struct {
	Status, Location, Key string
	At                    time.Time
}
type RiskRevision struct {
	ID, InputDigest, RuleVersion, Owner string
	Hits                                []Hit
	At                                  time.Time
	Acknowledged                        *Acknowledgement
	Console, MR                         Delivery
	Attempts                            []Delivery
}
type Reminder struct {
	ID            string
	Coordinate    Coordinate
	Revisions     []RiskRevision
	MergedAt      time.Time
	MergeEvidence string
}
type Comment struct {
	Coordinate                         Coordinate
	ReminderID, Revision, OperationKey string
	Hits                               []Hit
	RuleVersion, Owner                 string
}
type CommentReceipt struct{ Status, Location string }
type Phase string

const (
	Draft        Phase = "DRAFT"
	ReviewPhase  Phase = "SPEC_REVIEW"
	Approved     Phase = "SPEC_APPROVED"
	Implementing Phase = "IMPLEMENTING"
	Acceptance   Phase = "ACCEPTANCE"
	Archived     Phase = "ARCHIVED"
	Released     Phase = "RELEASED"
)

var phases = []Phase{Draft, ReviewPhase, Approved, Implementing, Acceptance, Archived, Released}

func rank(p Phase) int {
	for i, v := range phases {
		if v == p {
			return i
		}
	}
	return -1
}

type Result struct {
	ID, Scenario, Status, TestedCommit, Evidence, Reason, Supersedes, Actor string
	At                                                                      time.Time
}
type Event struct {
	ID, Kind, Actor string
	From, To        Phase
	Coordinate      Coordinate
	At              time.Time
	Evidence        []string
	FactVersion     string
	Facts           SnapshotView
	ResultRefs      []string
}
type Tool struct {
	ID, Name, Evidence, Actor string
	Success                   bool
	At                        time.Time
}
type Operation struct{ Digest, Kind, ID string }
type State struct {
	Coordinate Coordinate
	Phase      Phase
	Reminder   *Reminder
	Results    []Result
	Events     []Event
	Tools      []Tool
	Operations map[string]Operation
}

func NewState(c Coordinate) State {
	return State{Coordinate: c, Phase: Draft, Operations: map[string]Operation{}}
}

// Transact atomically commits state AND its append-only audit/operation history.
// It must roll back all callback changes on error and serialize/CAS local races.
type Store interface {
	Read(context.Context, Coordinate) (State, error)
	Transact(context.Context, Coordinate, func(*State) error) error
}
type Service struct {
	Store    Store
	Provider Provider
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) ready() error {
	if s == nil || s.Store == nil || s.Provider == nil {
		return ErrUnavailable
	}
	return nil
}
func operation(st *State, actor, id, kind string, payload any) (string, bool, error) {
	if !token.MatchString(id) || len(actor) == 0 || len(actor) > 256 {
		return "", false, ErrInvalid
	}
	key := digest([]string{actor, id, st.Coordinate.Key()})
	d := digest(payload)
	if op, ok := st.Operations[key]; ok {
		if op.Kind != kind || op.Digest != d {
			return "", false, ErrConflict
		}
		return key, true, nil
	}
	if len(st.Operations) >= 2000 {
		return "", false, ErrUnavailable
	}
	if st.Operations == nil {
		st.Operations = map[string]Operation{}
	}
	st.Operations[key] = Operation{Digest: d, Kind: kind, ID: key}
	return key, false, nil
}

// mutate fences ACL and performs a second fact read inside the storage commit
// boundary. No cached snapshot can silently become a current approval.
func (s *Service) mutate(ctx context.Context, actor string, c Coordinate, action string, fn func(*State, Snapshot) error) error {
	if e := s.ready(); e != nil {
		return e
	}
	if c.Validate() != nil || strings.TrimSpace(actor) == "" {
		return ErrInvalid
	}
	return s.Provider.WithAccess(ctx, actor, c, action, func(r Reader) error {
		first, e := r.Read(ctx, c)
		if e != nil {
			return e
		}
		if e = first.valid(c); e != nil {
			return e
		}
		return s.Store.Transact(ctx, c, func(st *State) error {
			if st.Coordinate != c {
				return ErrConflict
			}
			if e = fn(st, first); e != nil {
				return e
			}
			last, e := r.Read(ctx, c)
			if e != nil {
				return e
			}
			if e = last.valid(c); e != nil {
				return e
			}
			if digest(first) != digest(last) {
				return block("提交边界事实已变化，请重新核对")
			}
			return nil
		})
	})
}
