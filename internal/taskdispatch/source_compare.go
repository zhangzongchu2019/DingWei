package taskdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

type pollFact struct {
	SHA         string
	PR          specread.PullRequest
	PRDimension string
	Err         error
}

func (s *Service) poll(ctx context.Context, owner string, m Mutation) pollFact {
	fact := pollFact{PRDimension: "pr_not_opened"}
	resolved, e := s.Reader.Read(ctx, owner, coordinate(m.Key, "", ""), m.MonitorRef)
	if e != nil {
		fact.Err = e
		return fact
	}
	fact.SHA = resolved.Commit
	if m.PullRequestID > 0 {
		fact.PR, e = s.Reader.ReadPullRequest(ctx, owner, coordinate(m.Key, "", ""), m.PullRequestID)
		if e != nil {
			fact.PRDimension = "comparison_unknown"
			fact.Err = e
			return fact
		}
		fact.PRDimension = "readable"
	}
	return fact
}
func (s *Service) observe(ctx context.Context, owner string, m *Mutation, shared *pollFact) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	m.ComparisonState = "comparison_unknown"
	m.ComparisonError = "comparison_unknown"
	fact := pollFact{}
	if shared == nil {
		fact = s.poll(ctx, owner, *m)
	} else {
		fact = *shared
	}
	priorState, priorBase, priorHead := m.PRRemoteState, m.PRBaseRef, m.PRHeadRef
	m.PRDimension = fact.PRDimension
	m.PRHeadSHA = fact.PR.HeadSHA
	m.PRRemoteState = fact.PR.State
	m.PRBaseRef = fact.PR.BaseRef
	m.PRHeadRef = fact.PR.HeadRef
	m.PRUpdatedAt = fact.PR.UpdatedAt
	if m.PullRequestID == 0 {
		m.PRDimension = "pr_not_opened"
	} else if fact.Err != nil {
		m.PRDimension = "comparison_unknown"
	}
	if fact.Err != nil || !ValidSHA(fact.SHA) {
		return
	}
	// No identity or remote ACL cache: a shared poll only shares the source fact.
	binding, e := s.binding(ctx, owner, m.Key)
	if e != nil {
		return
	}
	m.Bindings = append(m.Bindings, binding)
	base, e := s.Reader.ReadDispatchSnapshot(ctx, owner, coordinate(m.Key, m.CommitSHA, ""), m.Key.ChangeDir)
	if e != nil {
		return
	}
	target, e := s.targetSnapshot(ctx, owner, m.Key, fact.SHA)
	if e != nil {
		return
	}
	policy := specconfirm.ConservativePolicyA{}
	difference, e := policy.Compare(base, target)
	if e != nil || difference == specconfirm.Indeterminate {
		return
	}
	if m.PullRequestID > 0 {
		pr, e := s.targetSnapshot(ctx, owner, m.Key, fact.PR.HeadSHA)
		if e != nil {
			return
		}
		d, e := policy.Compare(base, pr)
		if e != nil || d == specconfirm.Indeterminate {
			return
		}
		if d == specconfirm.Material {
			difference = d
		}
	}
	m.ComparisonState = "clean"
	if difference == specconfirm.Material || (m.PullRequestID > 0 && priorState != "" && (priorState != m.PRRemoteState || priorBase != m.PRBaseRef || priorHead != m.PRHeadRef)) {
		m.ComparisonState = "needs_sync"
	}
	m.ObservedCommitSHA = fact.SHA
	m.ComparisonError = ""
}
func (s *Service) targetSnapshot(ctx context.Context, owner string, k Key, sha string) (specconfirm.Snapshot, error) {
	snap, e := s.Reader.ReadDispatchSnapshot(ctx, owner, coordinate(k, sha, ""), k.ChangeDir)
	if e == nil {
		return snap, nil
	}
	// Only exact content-identical dated archives can be exempted by Policy A.
	root, err := s.Reader.Read(ctx, owner, coordinate(k, sha, ""), "")
	if err != nil {
		return snap, e
	}
	name := strings.TrimPrefix(k.ChangeDir, "openspec/changes/")
	candidates := map[string]bool{}
	for _, entry := range root.Entries {
		prefix := "openspec/changes/archive/"
		if !strings.HasPrefix(entry.Path, prefix) {
			continue
		}
		tail := strings.TrimPrefix(entry.Path, prefix)
		part := strings.SplitN(tail, "/", 2)[0]
		if len(part) > 11 && part[10] == '-' && part[11:] == name {
			if _, err := time.Parse("2006-01-02", part[:10]); err == nil {
				candidates[prefix+part] = true
			}
		}
	}
	if len(candidates) != 1 {
		return snap, e
	}
	for dir := range candidates {
		return s.Reader.ReadDispatchSnapshot(ctx, owner, coordinate(k, sha, ""), dir)
	}
	return snap, e
}
func mutationForLock(l Lock, kind, id string) Mutation {
	return Mutation{OperationID: id, Kind: kind, Key: l.Key, ExpectedGeneration: l.Generation, ExpectedToken: l.LeaseToken, ActorKind: "system", HolderAgentID: l.HolderAgentID, HolderOwnerLC: l.HolderOwnerLC, AssignedByUserLC: l.AssignedByUserLC, AuthorizationGroup: l.AuthorizationGroup, CommitSHA: l.CommitSHA, TasksBlobSHA: l.TasksBlobSHA, TaskLine: l.TaskLine, MonitorRef: l.MonitorRef, PullRequestID: l.PullRequestID, PRDimension: l.PRDimension, PRHeadSHA: l.PRHeadSHA, PRRemoteState: l.PRRemoteState, PRBaseRef: l.PRBaseRef, PRHeadRef: l.PRHeadRef, ComparisonState: l.ComparisonState}
}
func operationFor(l Lock, kind string, now time.Time) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%s/%s", l.Key.Digest(), l.Generation, kind, now.UTC().Format(time.RFC3339Nano))))
	return "r10_" + hex.EncodeToString(h[:])
}

func ComparisonPolicyVersion() string { return (specconfirm.ConservativePolicyA{}).Version() }
