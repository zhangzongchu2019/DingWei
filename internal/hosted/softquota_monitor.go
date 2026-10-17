package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrHoldReleaseRefused = errors.New("hold_release_refused")

// These projections deliberately omit container IDs, private paths, labels,
// credentials and all unrecognized node-supplied fields.
type QuotaUsage struct {
	Allocated int64 `json:"allocated_bytes"`
	Apparent  int64 `json:"apparent_bytes"`
}
type QuotaSeat struct {
	WriterKind   string     `json:"writer_kind,omitempty"`
	Limit        int64      `json:"limit_bytes"`
	Usage        QuotaUsage `json:"usage"`
	MeasuredAt   time.Time  `json:"measured_at"`
	UsageUnknown bool       `json:"usage_unknown"`
	Warn         bool       `json:"warn"`
	UsageWarning bool       `json:"usage_warning"`
	SpaceWarning bool       `json:"space_warning"`
	Hold         string     `json:"hold,omitempty"`
	HoldID       string     `json:"hold_id,omitempty"`
	StopState    string     `json:"stop_state,omitempty"`
}
type QuotaThresholds struct {
	SnapshotAt      time.Time `json:"snapshot_at"`
	Known           bool      `json:"known"`
	WarnPercent     int64     `json:"warn_percent"`
	RecoveryPercent int64     `json:"recovery_percent"`
	ReserveBytes    int64     `json:"reserve_bytes"`
	ReserveInodes   uint64    `json:"reserve_inodes"`
	FreeBytes       int64     `json:"free_bytes"`
	FreeInodes      uint64    `json:"free_inodes"`
	EmergencyBytes  int64     `json:"emergency_bytes"`
	EmergencyInodes uint64    `json:"emergency_inodes"`
	MaxAgeSeconds   int64     `json:"max_age_seconds"`
}
type QuotaMonitorNode struct {
	NoticesPending int64                `json:"notices_pending"`
	NoticesQueued  int64                `json:"notices_queued"`
	ID             string               `json:"node_id"`
	Mode           string               `json:"quota_mode"`
	Fresh          bool                 `json:"fresh"`
	LastSeenAt     *time.Time           `json:"last_seen_at"`
	ReleaseVersion int                  `json:"hold_release_version"`
	Thresholds     *QuotaThresholds     `json:"thresholds,omitempty"`
	Seats          map[string]QuotaSeat `json:"seats"`
}

func projectQuotaNode(n Node, now time.Time) QuotaMonitorNode {
	mode, revision := registeredQuota(n)
	v := QuotaMonitorNode{ID: n.ID, Mode: mode, LastSeenAt: n.LastSeenAt, Seats: map[string]QuotaSeat{}}
	var c struct {
		Mode           string               `json:"quota_mode"`
		Revision       string               `json:"quota_policy_revision"`
		ReleaseVersion int                  `json:"hold_release_version"`
		Thresholds     *QuotaThresholds     `json:"soft_quota_policy"`
		Seats          map[string]QuotaSeat `json:"soft_seats"`
	}
	if mode != "soft" || json.Unmarshal(n.Capacity, &c) != nil || c.Mode != mode || c.Revision != revision || len(c.Seats) > 102 {
		return v
	}
	v.Fresh = n.State == "online" && n.LastSeenAt != nil && !now.Before(*n.LastSeenAt) && now.Sub(*n.LastSeenAt) <= 60*time.Second
	v.ReleaseVersion, v.Thresholds = c.ReleaseVersion, c.Thresholds
	for id, s := range c.Seats {
		if !nodeSeatID.MatchString(id) || (s.WriterKind != "" && (s.WriterKind != "login" || !loginWriterID.MatchString(id))) {
			continue
		}
		if s.Hold != "" && !knownQuotaReason(s.Hold) {
			s.Hold = "unknown"
		}
		if !quotaEventIDPattern.MatchString(s.HoldID) {
			s.HoldID = ""
		}
		switch s.StopState {
		case "", "pending", "stopped", "failed":
		default:
			s.StopState = "unknown"
		}
		v.Seats[id] = s
	}
	return v
}

func knownQuotaReason(reason string) bool {
	switch reason {
	case "usage_warning", "disk_warning", "disk_limit", "usage_unknown", "private_state_unavailable", "unclean_restart", "node_space_emergency", "node_inode_emergency", "node_space_unknown":
		return true
	}
	return false
}

func (s *Store) QuotaMonitor(ctx context.Context) ([]QuotaMonitorNode, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string][2]int64{}
	rows, err := s.query(ctx, `SELECT node_id,count(*) FILTER(WHERE delivered_at IS NULL),count(*) FILTER(WHERE delivered_at IS NOT NULL) FROM g4_quota_notice GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var pending, queued int64
		if err := rows.Scan(&id, &pending, &queued); err != nil {
			rows.Close()
			return nil, err
		}
		counts[id] = [2]int64{pending, queued}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]QuotaMonitorNode, 0, len(nodes))
	for _, n := range nodes {
		v := projectQuotaNode(n, time.Now())
		v.NoticesPending, v.NoticesQueued = counts[n.ID][0], counts[n.ID][1]
		out = append(out, v)
	}
	return out, nil
}

// Authorization is a separate HTTP identity boundary; actor is a verified human
// owner. Audit precedes dispatch. A timeout is uncertain, never retried here.
func (s *Store) ReleaseSoftHold(ctx context.Context, gw NodeGateway, actor, nodeID, seatID, holdID string) error {
	if gw == nil || actor == "" || !nodeSeatID.MatchString(seatID) || !quotaEventIDPattern.MatchString(holdID) {
		return ErrHoldReleaseRefused
	}
	n, err := s.GetNode(ctx, nodeID)
	if err != nil {
		return ErrHoldReleaseRefused
	}
	v := projectQuotaNode(n, time.Now())
	seat, ok := v.Seats[seatID]
	if !v.Fresh || v.ReleaseVersion != 1 || !ok || seat.Hold == "" || seat.HoldID != holdID || seat.StopState != "stopped" {
		return ErrHoldReleaseRefused
	}
	detail, _ := json.Marshal(map[string]string{"seat_id": seatID, "hold_id": holdID})
	if err = s.AddAudit(ctx, actor, "node.soft_quota.release_requested", nodeID, detail); err != nil {
		return err
	}
	params, _ := json.Marshal(map[string]string{"seat_id": seatID, "hold_id": holdID})
	res, sendErr := gw.Send(ctx, nodeID, NodeCommand{Type: "release_soft_hold", Params: params})
	var receipt struct {
		SeatID string `json:"seat_id"`
		HoldID string `json:"hold_id"`
		State  string `json:"state"`
	}
	valid := sendErr == nil && res.OK && json.Unmarshal(res.Payload, &receipt) == nil && receipt.SeatID == seatID && receipt.HoldID == holdID && receipt.State == "stopped"
	action := "node.soft_quota.release_refused"
	if valid {
		action = "node.soft_quota.released"
	}
	if err = s.AddAudit(ctx, actor, action, nodeID, detail); err != nil {
		return err
	}
	if !valid {
		return ErrHoldReleaseRefused
	}
	// Do not resume credentials, mark online, clear other holds, or start anything.
	return nil
}
