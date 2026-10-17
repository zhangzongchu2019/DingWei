package hosted

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

var policyRevisionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var quotaEventIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var loginWriterID = regexp.MustCompile(`^login-[0-9a-f]{32}$`)
var nodeSeatID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

// Registration is administrator-owned. A capacity frame cannot enable soft or
// change its approved policy revision. Empty registrations remain legacy hard.
func normalizeQuotaRegistration(n *Node) error {
	if n.QuotaMode == "" {
		n.QuotaMode = "hard"
	}
	if n.QuotaMode != "hard" && n.QuotaMode != "soft" {
		return fmt.Errorf("%w: quota_mode", ErrParams)
	}
	labels := map[string]string{}
	if len(n.Labels) > 0 {
		if err := json.Unmarshal(n.Labels, &labels); err != nil || labels == nil {
			return fmt.Errorf("%w: node labels", ErrParams)
		}
	}
	if n.QuotaMode == "soft" {
		if !policyRevisionPattern.MatchString(n.QuotaPolicyRevision) {
			return fmt.Errorf("%w: soft quota policy revision required", ErrParams)
		}
		labels["quota_mode"] = "soft"
		labels["quota_policy_revision"] = n.QuotaPolicyRevision
	} else {
		if labels["quota_mode"] == "soft" {
			return fmt.Errorf("%w: soft requires explicit registration", ErrParams)
		}
		labels["quota_mode"] = "hard"
		delete(labels, "quota_policy_revision")
	}
	b, err := json.Marshal(labels)
	n.Labels = b
	return err
}
func registeredQuota(n Node) (mode, revision string) {
	var labels map[string]string
	if json.Unmarshal(n.Labels, &labels) != nil {
		return "unknown", ""
	}
	mode = labels["quota_mode"]
	if mode == "" {
		mode = "hard"
	}
	return mode, labels["quota_policy_revision"]
}
func nodeQuotaEligible(n Node) bool {
	mode, rev := registeredQuota(n)
	var c struct {
		Mode     string `json:"quota_mode"`
		Revision string `json:"quota_policy_revision"`
		Ready    bool   `json:"soft_ready"`
	}
	if json.Unmarshal(n.Capacity, &c) != nil {
		return false
	}
	if mode == "hard" {
		return c.Mode == "" || c.Mode == "hard"
	}
	return mode == "soft" && c.Mode == "soft" && policyRevisionPattern.MatchString(rev) && c.Revision == rev && c.Ready && n.LastSeenAt != nil && time.Since(*n.LastSeenAt) >= 0 && time.Since(*n.LastSeenAt) <= 60*time.Second
}

type quotaEvent struct {
	WriterKind string    `json:"writer_kind,omitempty"`
	ID         string    `json:"id"`
	SeatID     string    `json:"seat_id"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}

func quotaEvents(raw json.RawMessage) ([]quotaEvent, error) {
	var frame struct {
		Events []quotaEvent `json:"quota_events"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, ErrParams
	}
	if len(frame.Events) > 256 {
		return nil, ErrParams
	}
	for _, e := range frame.Events {
		if (e.WriterKind != "" && (e.WriterKind != "login" || !loginWriterID.MatchString(e.SeatID))) || !quotaEventIDPattern.MatchString(e.ID) || !nodeSeatID.MatchString(e.SeatID) || e.At.IsZero() {
			return nil, ErrParams
		}
		switch e.Reason {
		case "usage_warning", "disk_warning", "disk_limit", "usage_unknown", "private_state_unavailable", "unclean_restart", "node_space_emergency", "node_inode_emergency", "node_space_unknown":
		default:
			return nil, ErrParams
		}
	}
	return frame.Events, nil
}

// Capacity and idempotent audit receipts commit together. An authenticated node
// only affects seats belonging to that node; it never retires or deletes data.
func (s *Store) SetNodeQuotaCapacity(ctx context.Context, id string, capacity json.RawMessage) error {
	events, err := quotaEvents(capacity)
	if err != nil {
		return err
	}
	return s.pool.InTx(ctx, htx(), nil, func(tx repo.Tx) error {
		var registered json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT labels FROM g4_node WHERE id=$1 FOR UPDATE`, id).Scan(&registered); err != nil {
			return err
		}
		mode, revision := registeredQuota(Node{Labels: registered})
		var cap struct {
			Mode     string `json:"quota_mode"`
			Revision string `json:"quota_policy_revision"`
			Seats    map[string]struct {
				WriterKind    string `json:"writer_kind"`
				Hold          string `json:"hold"`
				LastReleaseID string `json:"last_release_id"`
				Stop          string `json:"stop_state"`
			} `json:"soft_seats"`
		}
		if json.Unmarshal(capacity, &cap) != nil || len(cap.Seats) > 102 {
			return ErrParams
		}
		if len(events) > 0 && (mode != "soft" || cap.Mode != "soft" || cap.Revision != revision) {
			return ErrParams
		}
		for _, e := range events {
			for _, recipient := range []string{"owner", "ops"} {
				if _, err := tx.Exec(ctx, `INSERT INTO g4_quota_notice(node_id,event_id,recipient,seat_id,reason,writer_kind,measured_at)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, id, e.ID, recipient, e.SeatID, e.Reason, e.WriterKind, e.At); err != nil {
					return err
				}
			}
			fields := map[string]any{"event_id": e.ID, "seat_id": e.SeatID, "reason": e.Reason, "measured_at": e.At, "quota_mode": "soft", "notice": "无硬磁盘配额"}
			if e.WriterKind != "" {
				fields["writer_kind"] = e.WriterKind
			}
			detail, _ := json.Marshal(fields)
			receipt, err := tx.Exec(ctx, `INSERT INTO g4_audit(actor,action,target,detail)
SELECT $1,'node.soft_quota',$2,$3 WHERE NOT EXISTS
(SELECT 1 FROM g4_audit WHERE action='node.soft_quota' AND target=$2 AND detail->>'event_id'=$4)`, "node:"+id, id, detail, e.ID)
			if err != nil {
				return err
			}
			// Pending events are history until ACK, not the current hold. Apply a
			// legacy event-only state transition once; a same-frame seat snapshot
			// is authoritative even when the event is first received after release.
			first, err := receipt.RowsAffected()
			if err != nil {
				return err
			}
			_, hasSnapshot := cap.Seats[e.SeatID]
			if first == 1 && !hasSnapshot && e.WriterKind != "login" && e.Reason != "disk_warning" && e.Reason != "usage_warning" {
				if _, err := tx.Exec(ctx, `UPDATE g4_hosted_seat SET state='error',state_detail='soft_quota_held',updated_at=now()
WHERE agent_id=$1 AND node_id=$2 AND state NOT IN ('destroyed','disabled')`, e.SeatID, id); err != nil {
					return err
				}
			}
		}
		if mode == "soft" && cap.Mode == "soft" && cap.Revision == revision {
			for seatID, v := range cap.Seats {
				if !nodeSeatID.MatchString(seatID) {
					return ErrParams
				}
				if v.WriterKind != "" {
					if v.WriterKind != "login" || !loginWriterID.MatchString(seatID) {
						return ErrParams
					}
					continue
				}
				if v.Hold == "" {
					if quotaEventIDPattern.MatchString(v.LastReleaseID) {
						// A lost command reply is resolved by an authenticated node
						// snapshot plus the exact, prior owner intent; never resend.
						// Only quota-held errors can converge to stopped. Other failures
						// and terminal states retain their original meaning.
						detail, _ := json.Marshal(map[string]string{"seat_id": seatID, "hold_id": v.LastReleaseID})
						if _, err := tx.Exec(ctx, `INSERT INTO g4_audit(actor,action,target,detail)
SELECT $4,'node.soft_quota.release_confirmed',$2,$5
WHERE EXISTS (SELECT 1 FROM g4_audit WHERE target=$2 AND action='node.soft_quota.release_requested' AND detail->>'seat_id'=$1 AND detail->>'hold_id'=$3)
AND NOT EXISTS (SELECT 1 FROM g4_audit WHERE target=$2 AND action='node.soft_quota.release_confirmed' AND detail->>'seat_id'=$1 AND detail->>'hold_id'=$3)`, seatID, id, v.LastReleaseID, "node:"+id, detail); err != nil {
							return err
						}
						if _, err := tx.Exec(ctx, `UPDATE g4_hosted_seat SET state='stopped',state_detail='soft_quota_released',updated_at=now()
WHERE agent_id=$1 AND node_id=$2 AND state IN ('stopped','error') AND state_detail='soft_quota_held'
AND EXISTS (SELECT 1 FROM g4_audit WHERE target=$2 AND action='node.soft_quota.release_requested' AND detail->>'seat_id'=$1 AND detail->>'hold_id'=$3)`, seatID, id, v.LastReleaseID); err != nil {
							return err
						}
					}
					continue
				}
				state := "error"
				if v.Stop == "stopped" {
					state = "stopped"
				}
				if _, err := tx.Exec(ctx, `UPDATE g4_hosted_seat SET state=$3,state_detail='soft_quota_held',updated_at=now()
WHERE agent_id=$1 AND node_id=$2 AND state NOT IN ('destroyed','disabled')`, seatID, id, state); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, `UPDATE g4_node SET state='online',capacity=$2,last_seen_at=now(),updated_at=now() WHERE id=$1`, id, capacity)
		return err
	})
}
func NodeQuotaEventIDs(capacity json.RawMessage) []string {
	events, err := quotaEvents(capacity)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

var ErrSoftQuotaHeld = fmt.Errorf("soft_quota_held")

func softSeatHeld(n Node, id string) bool {
	var c struct {
		Seats map[string]struct {
			Hold string `json:"hold"`
		} `json:"soft_seats"`
	}
	if json.Unmarshal(n.Capacity, &c) != nil {
		return false
	}
	return c.Seats[id].Hold != ""
}
func softSeatWarning(n Node, id string) bool {
	var c struct {
		Seats map[string]struct {
			Warn         bool `json:"warn"`
			UsageWarning bool `json:"usage_warning"`
			SpaceWarning bool `json:"space_warning"`
		} `json:"soft_seats"`
	}
	if json.Unmarshal(n.Capacity, &c) != nil {
		return false
	}
	return c.Seats[id].Warn || c.Seats[id].UsageWarning || c.Seats[id].SpaceWarning
}
func softSeatUnknown(n Node, id string) bool {
	var c struct {
		Seats map[string]struct {
			Usage bool `json:"usage_warning"`
			Space bool `json:"space_warning"`
		} `json:"soft_seats"`
	}
	if json.Unmarshal(n.Capacity, &c) != nil {
		return false
	}
	return c.Seats[id].Usage || c.Seats[id].Space
}
func (l *Lifecycle) quotaStartGuard(ctx context.Context, seat Seat) error {
	n, err := l.st.GetNode(ctx, seat.NodeID)
	if err != nil {
		return err
	}
	mode, _ := registeredQuota(n)
	if mode == "soft" && (!nodeQuotaEligible(n) || softSeatHeld(n, seat.AgentID) || seat.StateDetail == "soft_quota_held") {
		return ErrSoftQuotaHeld
	}
	if mode == "soft" {
		v, err := l.st.nodeSeatCapacity(ctx, n)
		if err != nil {
			return err
		}
		var frame seatCapacityFrame
		if json.Unmarshal(n.Capacity, &frame) != nil || v == nil {
			return capacityFailure(nil)
		}
		if reasons := v.CanStart(); len(reasons) != 0 {
			return capacityFailure(reasons)
		}
		if _, known := frame.Seats[seat.AgentID]; !known {
			return capacityFailure(nil)
		}
	}
	if mode != "hard" && mode != "soft" {
		return ErrSoftQuotaHeld
	}
	return nil
}
func (l *Lifecycle) quotaResult(ctx context.Context, seat Seat, result NodeResult) bool {
	if result.OK || !strings.Contains(result.Error, "soft_quota_blocked") {
		return false
	}
	_ = l.st.SetSeatState(ctx, seat.AgentID, "error", "soft_quota_held", "soft_quota", nil)
	return true
}

// Old hard nodes retain the exact v1 command shape; explicit CPU bandwidth is
// sent only after the node advertises the resource extension.
func (s *Store) resourceParams(ctx context.Context, nodeID string, plan Plan) map[string]any {
	r := map[string]any{"memory_mb": plan.MemMB, "cpu_shares": cpuShares(plan.VCPU), "disk_mb": plan.DiskMB}
	n, err := s.GetNode(ctx, nodeID)
	if err != nil {
		return r
	}
	var c struct {
		Version int `json:"resource_limits_version"`
	}
	if json.Unmarshal(n.Capacity, &c) == nil && c.Version == 1 {
		r["cpu_limit_millicores"] = plan.VCPU * 1000
	}
	return r
}
