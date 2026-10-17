package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

var errHoldRelease = errors.New("hold_release_refused")
var holdIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Only an authenticated Hub command can reach this handler. The Hub separately
// requires the configured human owner session. A release never starts a writer.
func (d *dockerOps) releaseHold(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SeatID string `json:"seat_id"`
		HoldID string `json:"hold_id"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 2048 || dec.Decode(&p) != nil || dec.Decode(&struct{}{}) != io.EOF || !validSeatID(p.SeatID) || !holdIDPattern.MatchString(p.HoldID) || d.soft == nil || ctx.Err() != nil {
		return nil, errHoldRelease
	}
	if err := d.soft.releaseHold(p.SeatID, p.HoldID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "hold_id": p.HoldID, "state": "stopped"})
}

func (q *softQuota) releaseHold(id, generation string) error {
	// Serialize with enforcement. No Engine request or filesystem call holds mu.
	if !q.enforceMu.TryLock() {
		return errHoldRelease
	}
	defer q.enforceMu.Unlock()
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats[id]
	if q.lock == nil || s == nil || s.Retiring || s.Hold == "" || s.HoldID != generation || s.StopState != "stopped" || q.admitLocked(0) != nil || s.Usage.charged() > s.Limit/100*q.policy.RecoveryPercent {
		return errHoldRelease
	}
	oldHold, oldStop, oldRelease := s.Hold, s.StopState, s.LastReleaseID
	s.Hold, s.HoldID, s.StopState = "", "", ""
	s.LastReleaseID = generation
	if err := q.persistLocked(); err != nil {
		// Persistence failures already latch broken and block every writer. Keep
		// the hold in memory too; never report a successful release on uncertainty.
		if s.Hold == "" {
			s.Hold, s.HoldID, s.StopState = oldHold, generation, oldStop
			s.LastReleaseID = oldRelease
		}
		return errHoldRelease
	}
	if s.Hold != "" { // A concurrent monitor may have recorded a fresh hazard.
		return errHoldRelease
	}
	return nil
}

// Public monitoring fields only: never expose private roots, Engine sockets,
// CIDs, approval references or credentials through capacity/monitor responses.
type softQuotaSummary struct {
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

func (q *softQuota) summary() *softQuotaSummary {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, i := q.admissionReserves(q.space)
	return &softQuotaSummary{q.spaceAt, !q.rootUnknownLocked(), q.policy.WarnPercent, q.policy.RecoveryPercent, r, i, q.space.Free, q.space.FreeFiles, q.emergencyThreshold(q.space), q.inodeEmergencyThreshold(q.space), int64(q.policy.MaxAge.Seconds())}
}
