// Package specconfirm binds committee confirmations to immutable Git coordinates.
// It never resolves a ref, fetches source, or treats a platform role as Git access.
package specconfirm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid confirmation")
	ErrConflict    = errors.New("confirmation operation conflicts with an immutable record")
	ErrForbidden   = errors.New("confirmation access denied")
	ErrUnavailable = errors.New("confirmation authority or repository verification unavailable")
)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var instancePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_/-]{0,199}$`)
var directoryPattern = regexp.MustCompile(`^openspec/changes/([a-z0-9][a-z0-9-]{0,127}|archive/[0-9]{4}-[0-9]{2}-[0-9]{2}-[a-z0-9][a-z0-9-]{0,127})$`)
var operationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type Coordinate struct {
	Instance   string `json:"git_instance"`
	Repository string `json:"repository"`
	ChangeDir  string `json:"change_dir"`
	Commit     string `json:"commit_sha"`
}

func (c Coordinate) Validate() error {
	if !instancePattern.MatchString(c.Instance) || strings.Contains(c.Instance, "..") || strings.HasSuffix(c.Instance, ".") || !repositoryPattern.MatchString(c.Repository) || strings.Contains(c.Repository, "//") || strings.HasSuffix(c.Repository, "/") || !directoryPattern.MatchString(c.ChangeDir) || !shaPattern.MatchString(c.Commit) {
		return ErrInvalid
	}
	return nil
}
func (c Coordinate) SameChange(other Coordinate) bool {
	return c.Instance == other.Instance && c.Repository == other.Repository && c.ChangeDir == other.ChangeDir
}

type Confirmation struct {
	ID          string `json:"id"`
	OperationID string `json:"operation_id"`
	Coordinate
	ConfirmedBy string    `json:"confirmed_by"`
	ConfirmedAt time.Time `json:"confirmed_at"`
	Meaning     string    `json:"meaning"`
}

func OperationKey(actor, operation string) string {
	sum := sha256.Sum256([]byte(actor + "\x00" + operation))
	return hex.EncodeToString(sum[:])
}
func (c Confirmation) Validate() error {
	if c.Coordinate.Validate() != nil || strings.TrimSpace(c.ConfirmedBy) == "" || len(c.ConfirmedBy) > 256 || !operationPattern.MatchString(c.OperationID) || c.ID != OperationKey(c.ConfirmedBy, c.OperationID) || c.ConfirmedAt.IsZero() || strings.TrimSpace(c.Meaning) == "" || len(c.Meaning) > 4096 {
		return ErrInvalid
	}
	return nil
}

// SameFact excludes the server timestamp: an idempotent retry keeps the first one.
func (c Confirmation) SameFact(other Confirmation) bool {
	return c.ID == other.ID && c.OperationID == other.OperationID && c.Coordinate == other.Coordinate && c.ConfirmedBy == other.ConfirmedBy && c.Meaning == other.Meaning
}

type Store interface {
	SaveSpecConfirmation(context.Context, Confirmation) (Confirmation, error)
	ListSpecConfirmations(context.Context, Coordinate) ([]Confirmation, error)
}

// Authority must validate the controlled repository, current read permission,
// and commit/directory existence. Writes additionally require trusted committee
// membership. It must keep authorization valid through fn, not authorize by a
// client-supplied role, Git author, project membership, or service account ACL.
// Missing/unverifiable facts fail closed. Calls are synchronous, exactly once.
type Authority interface {
	WithAccess(context.Context, string, Coordinate, bool, func(context.Context) error) error
}

type Request struct {
	Coordinate
	OperationID string `json:"operation_id"`
	Meaning     string `json:"meaning"`
}

type Status string

const (
	Confirmed Status = "confirmed_version"
	Unchanged Status = "unchanged_since_confirmation"
	Stale     Status = "old_version"
	Unknown   Status = "unknown"
)

type Assessment struct {
	Status        Status `json:"status"`
	FromCommit    string `json:"from_commit"`
	ToCommit      string `json:"to_commit"`
	PolicyVersion string `json:"policy_version"`
	Reason        string `json:"reason"`
}
type View struct {
	Confirmation
	Assessment Assessment `json:"assessment"`
}

// Service is configured at startup and then immutable. Zero value denies access
// and leaves the material-change policy disabled. No environment flag enables it.
type Service struct {
	Store          Store
	Authority      Authority
	Reader         SnapshotReader
	Policy         ChangePolicy
	PolicyApproved bool
	Now            func() time.Time
}

// Enforce the callback contract as well as the authority result: an adapter
// that skips the operation or drops its error must never return false success.
func (s *Service) withAccess(ctx context.Context, actor string, c Coordinate, write bool, fn func(context.Context) error) error {
	calls := 0
	var operationErr error
	err := s.Authority.WithAccess(ctx, actor, c, write, func(ctx context.Context) error {
		calls++
		if calls != 1 {
			return ErrUnavailable
		}
		operationErr = fn(ctx)
		return operationErr
	})
	if err != nil {
		return err
	}
	if calls != 1 {
		return ErrUnavailable
	}
	return operationErr
}

func (s *Service) Confirm(ctx context.Context, actor string, req Request) (out Confirmation, err error) {
	if s == nil || s.Store == nil || s.Authority == nil {
		return out, ErrUnavailable
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	fact := Confirmation{ID: OperationKey(actor, req.OperationID), OperationID: req.OperationID, Coordinate: req.Coordinate, ConfirmedBy: actor, ConfirmedAt: now, Meaning: req.Meaning}
	if fact.Validate() != nil {
		return out, ErrInvalid
	}
	err = s.withAccess(ctx, actor, req.Coordinate, true, func(ctx context.Context) error {
		var e error
		out, e = s.Store.SaveSpecConfirmation(ctx, fact)
		return e
	})
	return out, err
}
func (s *Service) List(ctx context.Context, actor string, current Coordinate) (out []View, err error) {
	if s == nil || s.Store == nil || s.Authority == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(actor) == "" || current.Validate() != nil {
		return nil, ErrInvalid
	}
	out = make([]View, 0)
	err = s.withAccess(ctx, actor, current, false, func(ctx context.Context) error {
		records, e := s.Store.ListSpecConfirmations(ctx, current)
		if e != nil {
			return e
		}
		for _, fact := range records {
			out = append(out, View{Confirmation: fact, Assessment: s.Assess(ctx, fact.Coordinate, current)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Assess never resolves branch names or changes a confirmation. Different SHA
// alone is not evidence of a material modification; disabled/failed comparisons
// are explicitly unknown. It is called only inside the read authorization fence.
func (s *Service) Assess(ctx context.Context, from, to Coordinate) Assessment {
	a := Assessment{Status: Unknown, FromCommit: from.Commit, ToCommit: to.Commit, Reason: "待核对：版本比较策略尚未启用"}
	if from.Validate() != nil || to.Validate() != nil || !from.SameChange(to) {
		a.Reason = "待核对：坐标不属于同一变更"
		return a
	}
	if from.Commit == to.Commit {
		a.Status = Confirmed
		a.PolicyVersion = "exact-commit-v1"
		a.Reason = "仅确认此完整 commit"
		return a
	}
	if s == nil || !s.PolicyApproved || s.Policy == nil || s.Reader == nil {
		return a
	}
	a.PolicyVersion = s.Policy.Version()
	if strings.TrimSpace(a.PolicyVersion) == "" {
		a.Reason = "待核对：策略版本缺失"
		return a
	}
	old, e := s.Reader.ReadSnapshot(ctx, from)
	if e != nil {
		a.Reason = "待核对：原版本无法读取"
		return a
	}
	current, e := s.Reader.ReadSnapshot(ctx, to)
	if e != nil {
		a.Reason = "待核对：当前版本无法读取"
		return a
	}
	if old.Coordinate != from || current.Coordinate != to {
		a.Reason = "待核对：读取版本与请求不符"
		return a
	}
	result, e := s.Policy.Compare(old, current)
	if e != nil {
		a.Reason = "待核对：比较失败"
		return a
	}
	switch result {
	case Material:
		a.Status = Stale
		a.Reason = "针对旧版本：规格内容发生实质修改，需重新确认"
	case NoMaterial:
		a.Status = Unchanged
		a.Reason = "策略已核无实质修改；确认仍仅绑定原 commit"
	default:
		a.Reason = "待核对：比较没有确定结论"
	}
	return a
}
