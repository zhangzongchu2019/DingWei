// Package coordinatereview implements R6. Persistent records contain coordinates
// and review facts, never repository text or transferable credentials.
package coordinatereview

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
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

var (
	ErrInvalid     = errors.New("invalid coordinate review")
	ErrForbidden   = errors.New("coordinate review access denied")
	ErrUnavailable = errors.New("coordinate review dependencies unavailable")
	ErrConflict    = errors.New("coordinate review conflict")
)
var token = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var operation = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type Coordinate struct {
	Provider string `json:"provider"`
	specconfirm.Coordinate
}

func (c Coordinate) Validate() error {
	if c.Coordinate.Validate() != nil || (c.Provider != "github" && c.Provider != "gongfeng") {
		return ErrInvalid
	}
	if c.Provider == "github" && (c.Instance != "github.com" || !regexp.MustCompile(`^[1-9][0-9]{0,19}$`).MatchString(c.Repository)) {
		return ErrInvalid
	}
	if c.Provider == "gongfeng" && c.Instance != "git.code.tencent.com" {
		return ErrInvalid
	}
	return nil
}
func key(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ID(actor, op string) string  { return "coordreview_" + key([]string{actor, op}) }
func AgentActor(id string) string { return "agent:" + id }
func validEvidence(s string) bool {
	return len(s) > 5 && len(s) <= 2048 && !strings.ContainsAny(s, "\x00\r\n") && (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "git:") || strings.HasPrefix(s, "report:") || strings.HasPrefix(s, "log:"))
}

type Result struct {
	Operation string    `json:"operation_id"`
	Commit    string    `json:"commit_sha"`
	Reviewer  string    `json:"reviewer"`
	Verdict   string    `json:"verdict"`
	Evidence  string    `json:"evidence"`
	At        time.Time `json:"received_at"`
}
type Notice struct {
	Slot    int       `json:"slot"`
	At      time.Time `json:"attempted_at"`
	State   string    `json:"state"` // attempting is unknown after a crash: never resend under a fresh identity.
	Receipt string    `json:"receipt,omitempty"`
}
type Comparison struct {
	Current    Coordinate             `json:"current"`
	Assessment specconfirm.Assessment `json:"assessment"`
	At         time.Time              `json:"at"`
}
type Alert struct {
	Current  Coordinate `json:"current"`
	Dispatch string     `json:"dispatch"`
}
type Record struct {
	Unable      time.Time    `json:"unable_at"`
	CheckedAt   time.Time    `json:"checked_at"`
	EventID     string       `json:"event_id"`
	Alerts      []Alert      `json:"alerts"`
	ReadStage   int          `json:"read_stage"`
	ID          string       `json:"id"`
	Operation   string       `json:"operation_id"`
	Coordinate  Coordinate   `json:"coordinate"`
	AssignedBy  string       `json:"assigned_by"`
	AgentID     string       `json:"agent_id"`
	TrackRef    string       `json:"track_ref"`
	Created     time.Time    `json:"created_at"`
	Opened      time.Time    `json:"opened_at,omitempty"`
	Completed   time.Time    `json:"completed_at,omitempty"`
	Cancelled   time.Time    `json:"cancelled_at,omitempty"`
	Superseded  time.Time    `json:"superseded_at,omitempty"`
	Dispatch    string       `json:"dispatch"`
	Results     []Result     `json:"results"`
	Comparisons []Comparison `json:"comparisons"`
	Notices     []Notice     `json:"notices"`
}

func (r Record) Validate() error {
	if !operation.MatchString(r.EventID) || r.Coordinate.Validate() != nil || !token.MatchString(r.AgentID) || !operation.MatchString(r.Operation) || r.AssignedBy == "" || strings.HasPrefix(r.AssignedBy, "agent:") || len(r.AssignedBy) > 200 || r.ID != ID(r.AssignedBy, r.Operation) || r.Created.IsZero() {
		return ErrInvalid
	}
	if len(r.TrackRef) > 256 || strings.ContainsAny(r.TrackRef, "\x00\r\n\\%?#") || strings.Contains(r.TrackRef, "..") || strings.HasPrefix(r.TrackRef, "/") {
		return ErrInvalid
	}
	return nil
}
func (r Record) Pending() bool {
	return r.Cancelled.IsZero() && r.Completed.IsZero() && r.Superseded.IsZero() && r.Unable.IsZero()
}
func (r Record) Unread() bool { return r.Pending() && r.Opened.IsZero() }
func (r Record) SameAssignment(o Record) bool {
	return r.EventID == o.EventID && r.ID == o.ID && r.Operation == o.Operation && r.Coordinate == o.Coordinate && r.AssignedBy == o.AssignedBy && r.AgentID == o.AgentID && r.TrackRef == o.TrackRef
}

// Store serializes mutations and preserves append-only histories. fn must not
// recursively read the store. It may perform final external/identity fences.
type Store interface {
	BeginCoordinateReview(context.Context) (func(), error)
	CreateCoordinateReview(context.Context, Record, func() error) (Record, error)
	CoordinateReview(context.Context, string) (Record, error)
	CoordinateReviewIDs(context.Context, string, int) ([]string, error)
	MutateCoordinateReview(context.Context, string, func(*Record) error) (Record, error)
}
type Access interface {
	// Must invoke fn synchronously exactly once, retaining current identity and
	// repository authorization through its final check callback.
	WithReviewAccess(context.Context, string, Record, func(func() error) error) error
}
type Reader interface {
	Read(context.Context, string, Coordinate, string) (specread.Result, error)
	Snapshot(context.Context, string, Coordinate) (specconfirm.Snapshot, error)
	Resolve(context.Context, string, Coordinate, string) (Coordinate, error)
}
type Create struct {
	EventID    string     `json:"event_id,omitempty"`
	Coordinate Coordinate `json:"coordinate"`
	AgentID    string     `json:"agent_id"`
	Operation  string     `json:"operation_id"`
	TrackRef   string     `json:"track_ref"`
}

type NoticeAccess interface {
	WithReviewNotice(context.Context, Record, func(func() error, string, string) error) error
}
