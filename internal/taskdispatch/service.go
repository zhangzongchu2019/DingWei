package taskdispatch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

func Enabled() bool { return os.Getenv("DW_TASK_DISPATCH_LOCK_ENABLED") == "1" }

// Actor is assembled from existing Console/Agent authentication, never JSON.
type Actor struct{ UserLC, AgentID string }
type Request struct {
	OperationID   string  `json:"operation_id"`
	Kind          string  `json:"kind"`
	Key           Key     `json:"key"`
	Generation    int64   `json:"generation"`
	Token         string  `json:"lease_token"`
	Group         string  `json:"authorization_group_code"`
	HolderAgentID string  `json:"holder_agent_id"`
	Commit        string  `json:"commit_sha"`
	MonitorRef    string  `json:"monitor_ref"`
	PullRequestID int64   `json:"pull_request_id"`
	Handoff       Handoff `json:"handoff"`
}
type Service struct {
	Repo     Repository
	Identity identity.Store
	Reader   *specread.Service
	Now      func() time.Time
	// Prepare projects current users into the existing R5 principal bridge before
	// acquiring identity locks. It must not run inside the MemStore callback.
	Prepare      func(context.Context, string) error
	AuditFailure func(context.Context, string, string)
}

func coordinate(k Key, commit, path string) specread.Coordinate {
	return specread.Coordinate{Provider: k.Provider, Instance: k.Instance, Repository: k.Repository, Commit: commit, Path: path}
}
func (s *Service) ready() bool {
	return Enabled() && s != nil && s.Repo != nil && s.Identity != nil && s.Reader != nil && s.Now != nil
}
func (s *Service) actorOwner(ctx context.Context, a Actor) (string, error) {
	if (a.UserLC == "") == (a.AgentID == "") {
		return "", ErrForbidden
	}
	owner := identity.Lower(a.UserLC)
	if a.AgentID != "" {
		agent, e := s.Identity.GetAgent(ctx, a.AgentID)
		if e != nil || agent == nil || agent.State != identity.AgentActive {
			return "", ErrForbidden
		}
		owner = agent.OwnerLC
	}
	user, e := s.Identity.GetUser(ctx, owner)
	if e != nil || user == nil || user.State != identity.UserActive {
		return "", ErrForbidden
	}
	return owner, nil
}
func (s *Service) binding(ctx context.Context, owner string, k Key) (BindingFact, error) {
	b, _, e := s.Reader.AuthorizeRead(ctx, owner, coordinate(k, "", ""))
	if e != nil {
		return BindingFact{}, ErrForbidden
	}
	return BindingFact{Principal: b.Principal, Provider: b.Provider, Instance: b.Instance, AccountID: b.AccountID, Login: b.Login}, nil
}
func (s *Service) fixedTask(ctx context.Context, owner string, k Key, commit string) (int, string, error) {
	if !ValidSHA(commit) {
		return 0, "", ErrInvalid
	}
	item, e := s.Reader.Read(ctx, owner, coordinate(k, commit, k.ChangeDir+"/tasks.md"), "")
	if e != nil {
		return 0, "", ErrComparison
	}
	task, e := SelectTask(item.Text, k.TaskID)
	if e != nil {
		return 0, "", e
	}
	blob := ""
	for _, entry := range item.Entries {
		if entry.Path == item.Path {
			blob = entry.SHA
		}
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(item.Text), item.Text)))
	if hex.EncodeToString(sum[:]) != blob {
		return 0, "", ErrComparison
	}
	return task.Line, blob, nil
}
func (s *Service) handoff(ctx context.Context, owner string, k Key, h Handoff, execution string) error {
	if !h.ValidFor(k, execution) || (h.RecoveryState != "stopped" && h.RecoveryState != "unknown") {
		return ErrInvalid
	}
	item, e := s.Reader.Read(ctx, owner, coordinate(k, h.Commit, h.Path), "")
	if e != nil {
		return ErrComparison
	}
	sha := ""
	for _, entry := range item.Entries {
		if entry.Path == h.Path {
			sha = entry.SHA
		}
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(item.Text), item.Text)))
	if sha != h.BlobSHA || hex.EncodeToString(sum[:]) != sha || !strings.Contains(item.Text, k.TaskID) || !strings.Contains(item.Text, execution) {
		return ErrInvalid
	}
	return nil
}
func (s *Service) Execute(ctx context.Context, actor Actor, r Request) (result Result, err error) {
	if !s.ready() {
		return result, ErrUnavailable
	}
	if !ValidOperation(r.OperationID) {
		return result, ErrInvalid
	}
	defer func() {
		if err != nil && s.AuditFailure != nil {
			s.AuditFailure(ctx, r.OperationID, errClass(err))
		}
	}()
	if r.Key.Validate() != nil {
		return result, ErrInvalid
	}
	if r.Kind == "observe" || r.Kind == "expire" {
		return result, ErrForbidden
	}
	owner, err := s.actorOwner(ctx, actor)
	if err != nil {
		return result, err
	}
	existing, err := s.Repo.TaskDispatchLock(ctx, r.Key)
	if err != nil {
		return result, err
	}
	replayOp, opErr := s.Repo.TaskDispatchOperation(ctx, r.Key, r.OperationID)
	if opErr != nil {
		return result, opErr
	}
	group := r.Group
	ids := []string{}
	expectedOwners := map[string]string{}
	if existing != nil {
		group = existing.AuthorizationGroup
		if r.Kind != "grant" {
			ids = append(ids, existing.HolderAgentID)
			expectedOwners[existing.HolderAgentID] = existing.HolderOwnerLC
		}
	}
	if actor.AgentID != "" {
		ids = append(ids, actor.AgentID)
		expectedOwners[actor.AgentID] = owner
	}
	if r.Kind == "grant" || r.Kind == "handoff" {
		if actor.AgentID != "" || r.HolderAgentID == "" {
			return result, ErrForbidden
		}
		ids = append(ids, r.HolderAgentID)
	}
	if len(ids) == 0 || group == "" {
		return result, ErrForbidden
	}
	for _, id := range ids {
		agent, e := s.Identity.GetAgent(ctx, id)
		if e != nil || agent == nil || agent.State != identity.AgentActive {
			return result, ErrForbidden
		}
		if expected := expectedOwners[id]; expected != "" && expected != agent.OwnerLC {
			return result, ErrForbidden
		}
		expectedOwners[id] = agent.OwnerLC
	}
	subjects := []string{owner}
	seenAgent := map[string]bool{}
	for _, id := range ids {
		if !seenAgent[id] {
			subjects = append(subjects, expectedOwners[id])
			seenAgent[id] = true
		}
	}
	owners := map[string]bool{owner: true}
	for _, o := range expectedOwners {
		owners[o] = true
	}
	for o := range owners {
		if s.Prepare != nil {
			if e := s.Prepare(ctx, o); e != nil {
				return result, ErrUnavailable
			}
		}
	}
	fence, ok := s.Identity.(identity.TaskAuthorizationStore)
	if !ok {
		return result, ErrUnavailable
	}
	callbackCompleted := false
	err = fence.WithTaskAuthorization(ctx, owner, group, ids, func(agents []identity.Agent, check func() error) error {
		for _, a := range agents {
			if expectedOwners[a.ID] != a.OwnerLC {
				return ErrForbidden
			}
		}
		m := Mutation{OperationID: r.OperationID, Kind: r.Kind, Key: r.Key, ExpectedGeneration: r.Generation, ExpectedToken: r.Token, ActorKind: "user", ActorUserLC: owner, AuthorizationGroup: group, Handoff: r.Handoff}
		if actor.AgentID != "" {
			m.ActorKind = "agent"
			m.ActorUserLC = ""
			m.ActorAgentID = actor.AgentID
			m.ActorOwnerLC = owner
		}
		if existing != nil {
			m.HolderAgentID = existing.HolderAgentID
			m.HolderOwnerLC = existing.HolderOwnerLC
			m.CommitSHA = existing.CommitSHA
			m.TasksBlobSHA = existing.TasksBlobSHA
			m.TaskLine = existing.TaskLine
			m.MonitorRef = existing.MonitorRef
			m.PullRequestID = existing.PullRequestID
			m.AssignedByUserLC = existing.AssignedByUserLC
			m.PRRemoteState = existing.PRRemoteState
			m.PRBaseRef = existing.PRBaseRef
			m.PRHeadRef = existing.PRHeadRef
		}
		if r.Kind == "grant" || r.Kind == "handoff" {
			m.HolderAgentID = r.HolderAgentID
			m.HolderOwnerLC = expectedOwners[r.HolderAgentID]
			m.CommitSHA = r.Commit
			m.MonitorRef = r.MonitorRef
			m.PullRequestID = r.PullRequestID
			m.AssignedByUserLC = owner
			if strings.TrimSpace(m.MonitorRef) == "" {
				return ErrInvalid
			}
		}
		if r.Kind == "register_pr" {
			m.PullRequestID = r.PullRequestID
		}
		// Both owners are checked independently, even when provider credentials have
		// broader access. Identity/ACL uncertainty never produces a grant.
		for _, o := range subjects {
			b, e := s.binding(ctx, o, r.Key)
			if e != nil {
				return e
			}
			m.Bindings = append(m.Bindings, b)
			if r.Kind == "grant" || r.Kind == "handoff" || r.Kind == "sync" {
				commit := m.CommitSHA
				if r.Kind == "sync" {
					commit = r.Commit
				}
				line, blob, e := s.fixedTask(ctx, o, r.Key, commit)
				if e != nil {
					return e
				}
				m.TaskLine = line
				m.TasksBlobSHA = blob
			}
			if (r.Kind == "release" || r.Kind == "handoff" || (r.Kind == "grant" && existing != nil && replayOp == nil)) && existing != nil {
				if e := s.handoff(ctx, o, r.Key, r.Handoff, existing.CommitSHA); e != nil {
					return e
				}
			}
		}
		if r.Kind == "release" {
			m.PRDimension = existing.PRDimension
			m.ComparisonState = existing.ComparisonState
		} else {
			if r.Kind == "sync" {
				m.CommitSHA = r.Commit
			}
			readOwner := m.HolderOwnerLC
			if readOwner == "" {
				return ErrForbidden
			}
			s.observe(ctx, readOwner, &m, nil)
			// Register only a known same-repository PR. Existing PR failures instead
			// persist unknown and block all protected actions.
			if r.Kind == "register_pr" && m.PRDimension != "readable" {
				return ErrComparison
			}
		}
		var e error
		result, e = s.Repo.ApplyTaskDispatch(ctx, m, s.Now, check)
		callbackCompleted = true
		return e
	})
	if err != nil && callbackCompleted && result.Outcome == "success" {
		return Result{OperationID: r.OperationID, Outcome: "unknown"}, ErrUnknown
	}
	return result, err
}
func errClass(e error) string {
	for _, safe := range []error{ErrInvalid, ErrForbidden, ErrConflict, ErrClock, ErrUnavailable, ErrUnknown, ErrNeedsSync, ErrComparison, ErrNotFound, ErrCompleted, ErrPRRequired} {
		if errors.Is(e, safe) {
			return safe.Error()
		}
	}
	return "r10_unavailable"
}
