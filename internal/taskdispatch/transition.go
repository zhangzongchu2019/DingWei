package taskdispatch

import (
	"strings"
	"time"
)

// Transition contains only state rules. Authentication, repository reads and the
// authorization fence belong to the caller; persistence must be one transaction.
func Transition(old *Lock, m Mutation, now time.Time, token string) (Lock, error) {
	l := Lock{Key: m.Key}
	if old != nil {
		l = *old
	}
	if now.IsZero() || (old != nil && (now.Before(l.GrantedAt) || now.Before(l.LastRenewedAt) || now.Before(l.UpdatedAt))) {
		return l, ErrClock
	}
	if m.Kind == "grant" {
		if m.ActorKind != "user" || m.ActorUserLC == "" || m.HolderAgentID == "" || m.HolderOwnerLC == "" || m.AssignedByUserLC != m.ActorUserLC || m.AuthorizationGroup == "" {
			return l, ErrForbidden
		}
		if !ValidSHA(m.CommitSHA) || !ValidSHA(m.TasksBlobSHA) || m.TaskLine < 1 || strings.TrimSpace(m.MonitorRef) == "" || len(m.MonitorRef) > 256 || strings.ContainsAny(m.MonitorRef, "\x00\r\n") {
			return l, ErrInvalid
		}
		if m.ComparisonState != "clean" || !validPR(m) {
			return l, ErrComparison
		}
		if old == nil {
			if m.ExpectedGeneration != 0 {
				return l, ErrConflict
			}
		} else {
			if m.ExpectedGeneration != l.Generation || l.Effective(now) {
				return l, ErrConflict
			}
			if !m.Handoff.ValidFor(m.Key, l.CommitSHA) {
				return l, ErrInvalid
			}
		}
		generation := l.Generation + 1
		l = Lock{Key: m.Key, State: "active", Generation: generation, LeaseToken: token, HolderAgentID: m.HolderAgentID, HolderOwnerLC: m.HolderOwnerLC, AssignedByUserLC: m.ActorUserLC, AuthorizationGroup: m.AuthorizationGroup, CommitSHA: m.CommitSHA, TasksBlobSHA: m.TasksBlobSHA, TaskLine: m.TaskLine, GrantedAt: now, ExpiresAt: now.Add(LeaseDuration), LastRenewedAt: now, UpdatedAt: now, MonitorRef: m.MonitorRef, ComparisonState: "clean", Handoff: m.Handoff}
		applyObservation(&l, m, now)
		return l, nil
	}
	if old == nil {
		return l, ErrNotFound
	}
	if m.ExpectedGeneration != l.Generation || m.ExpectedToken != l.LeaseToken {
		return l, ErrConflict
	}
	if m.Kind == "expire" {
		if m.ActorKind != "system" {
			return l, ErrForbidden
		}
		if l.State != "active" || now.Before(l.ExpiresAt) {
			return l, ErrConflict
		}
		l.State = "expired"
		l.Generation++
		l.LeaseToken = token
		l.UpdatedAt = now
		return l, nil
	}
	if !l.Effective(now) {
		return l, ErrConflict
	}
	holder := m.ActorKind == "agent" && m.ActorAgentID == l.HolderAgentID && m.ActorOwnerLC == l.HolderOwnerLC
	responsible := m.ActorKind == "user" && m.ActorUserLC != "" && m.AuthorizationGroup == l.AuthorizationGroup
	if m.Kind == "observe" {
		if m.ActorKind != "system" {
			return l, ErrForbidden
		}
	} else if m.Kind == "handoff" {
		if !responsible {
			return l, ErrForbidden
		}
	} else if m.Kind == "register_pr" {
		if !holder && !responsible {
			return l, ErrForbidden
		}
	} else if !holder {
		return l, ErrForbidden
	}
	switch m.Kind {
	case "release", "handoff":
		if !m.Handoff.ValidFor(m.Key, l.CommitSHA) {
			return l, ErrInvalid
		}
		l.Handoff = m.Handoff
		l.Generation++
		l.LeaseToken = token
		if m.Kind == "release" {
			l.State = "released"
		} else {
			if m.HolderAgentID == "" || m.HolderOwnerLC == "" || !ValidSHA(m.CommitSHA) || !ValidSHA(m.TasksBlobSHA) || m.TaskLine < 1 || strings.TrimSpace(m.MonitorRef) == "" || m.ComparisonState != "clean" || !validPR(m) {
				return *old, ErrInvalid
			}
			l.HolderAgentID = m.HolderAgentID
			l.HolderOwnerLC = m.HolderOwnerLC
			l.AssignedByUserLC = m.ActorUserLC
			l.CommitSHA = m.CommitSHA
			l.TasksBlobSHA = m.TasksBlobSHA
			l.TaskLine = m.TaskLine
			l.MonitorRef = m.MonitorRef
			l.GrantedAt = now
			l.LastRenewedAt = now
			l.ExpiresAt = now.Add(LeaseDuration)
			l.ComparisonState = "clean"
			applyObservation(&l, m, now)
		}
	case "observe", "renew", "continue", "submit", "sync", "register_pr":
		if m.Kind == "register_pr" {
			if l.PullRequestID != 0 || m.PullRequestID <= 0 || !validPR(m) {
				return l, ErrInvalid
			}
		} else if m.PullRequestID != l.PullRequestID || (l.PullRequestID == 0 && m.PRDimension != "pr_not_opened") {
			return l, ErrInvalid
		}
		if m.ComparisonState != "clean" && m.ComparisonState != "needs_sync" && m.ComparisonState != "comparison_unknown" {
			return l, ErrComparison
		}
		if !validPR(m) {
			return l, ErrComparison
		}
		applyObservation(&l, m, now)
		if m.Kind == "observe" {
			break
		}
		if l.ComparisonState == "comparison_unknown" || l.PRDimension == "comparison_unknown" {
			l.UpdatedAt = now
			return l, ErrComparison
		}
		if m.Kind == "sync" {
			if !ValidSHA(m.CommitSHA) || m.CommitSHA != m.ObservedCommitSHA || !ValidSHA(m.TasksBlobSHA) || m.TaskLine < 1 || m.ComparisonState != "clean" {
				return *old, ErrInvalid
			}
			l.CommitSHA = m.CommitSHA
			l.TasksBlobSHA = m.TasksBlobSHA
			l.TaskLine = m.TaskLine
			l.ComparisonState = "clean"
			l.SyncRequired = false
			l.Generation++
			l.LeaseToken = token
		} else {
			if l.ComparisonState == "needs_sync" {
				l.UpdatedAt = now
				return l, ErrNeedsSync
			}
			if m.Kind == "submit" && (l.PullRequestID <= 0 || l.PRDimension != "readable") {
				l.UpdatedAt = now
				return l, ErrPRRequired
			}
			if m.Kind == "renew" {
				l.LastRenewedAt = now
				l.ExpiresAt = now.Add(LeaseDuration)
			}
		}
	default:
		return l, ErrInvalid
	}
	l.UpdatedAt = now
	return l, nil
}
func validPR(m Mutation) bool {
	if m.PullRequestID == 0 {
		return m.PRDimension == "pr_not_opened"
	}
	return m.PullRequestID > 0 && (m.PRDimension == "comparison_unknown" || (m.PRDimension == "readable" && ValidSHA(m.PRHeadSHA)))
}
func applyObservation(l *Lock, m Mutation, now time.Time) {
	l.PullRequestID = m.PullRequestID
	l.PRDimension = m.PRDimension
	l.PRHeadSHA = m.PRHeadSHA
	l.PRRemoteState = m.PRRemoteState
	l.PRBaseRef = m.PRBaseRef
	l.PRHeadRef = m.PRHeadRef
	l.PRUpdatedAt = m.PRUpdatedAt
	l.LastAttemptAt = now
	l.ComparisonError = m.ComparisonError
	// A material change remains pending even if the ref later moves back. Only
	// explicit holder sync can acknowledge it. Unknown may recover to clean.
	if m.ComparisonState == "comparison_unknown" || m.PRDimension == "comparison_unknown" {
		l.ComparisonState = "comparison_unknown"
	} else if l.ComparisonState != "needs_sync" {
		l.ComparisonState = m.ComparisonState
	}
	if m.ComparisonState == "needs_sync" {
		l.SyncRequired = true
	}
	if l.SyncRequired && l.ComparisonState != "comparison_unknown" {
		l.ComparisonState = "needs_sync"
	}
	if m.ComparisonState != "comparison_unknown" && m.PRDimension != "comparison_unknown" {
		l.LastCheckedAt = now
		l.ObservedCommitSHA = m.ObservedCommitSHA
	}
}
