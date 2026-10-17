package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

type capacityRows func(context.Context, string, ...any) (*sql.Rows, error)
type seatCapacityFrame struct {
	Capacity *seatcapacity.View         `json:"seat_capacity"`
	Seats    map[string]json.RawMessage `json:"soft_seats"`
}

func capacityPlan(p Plan) seatcapacity.Resources {
	return seatcapacity.Resources{CPU: int64(p.VCPU) * 1000, Memory: p.MemMB << 20, Disk: p.DiskMB << 20}
}

func unknownSeatCapacity(version int) *seatcapacity.View {
	return &seatcapacity.View{Version: version, Known: false, Reasons: []string{"load_unknown"}}
}

// New B capability is mandatory for explicit soft admission. Legacy hard nodes
// retain their old behavior and omit this extension. Unknown is never zero load.
func effectiveSeatCapacity(ctx context.Context, n Node, query capacityRows) (*seatcapacity.View, error) {
	mode, _ := registeredQuota(n)
	if mode == "hard" {
		return nil, nil
	}
	var frame seatCapacityFrame
	if json.Unmarshal(n.Capacity, &frame) != nil || frame.Capacity == nil {
		return unknownSeatCapacity(0), nil
	}
	v := *frame.Capacity
	if !v.ValidateAt(time.Now(), 60*time.Second) {
		return unknownSeatCapacity(v.Version), nil
	}
	if len(frame.Seats) > 256 {
		return unknownSeatCapacity(v.Version), nil
	}
	ordinary := 0
	for _, raw := range frame.Seats {
		var writer struct {
			Kind string `json:"writer_kind"`
		}
		if json.Unmarshal(raw, &writer) != nil {
			return unknownSeatCapacity(v.Version), nil
		}
		if writer.Kind == "" {
			ordinary++
		} else if writer.Kind != "login" {
			return unknownSeatCapacity(v.Version), nil
		}
	}
	if ordinary != v.Occupied {
		return unknownSeatCapacity(v.Version), nil
	}
	rows, err := query(ctx, `SELECT agent_id,plan FROM g4_hosted_seat WHERE node_id=$1 AND state IN ('queued','pulling','starting','busy') LIMIT 357`, n.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []seatcapacity.Resources
	for rows.Next() {
		var id, planID string
		if err := rows.Scan(&id, &planID); err != nil {
			return nil, err
		}
		if _, known := frame.Seats[id]; known {
			continue
		}
		plan, ok := PlanByID(planID)
		if !ok {
			return unknownSeatCapacity(v.Version), nil
		}
		pending = append(pending, capacityPlan(plan))
		if len(pending) > 100 {
			return unknownSeatCapacity(v.Version), nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	v = v.WithPending(pending)
	return &v, nil
}

func (s *Store) nodeSeatCapacity(ctx context.Context, n Node) (*seatcapacity.View, error) {
	return effectiveSeatCapacity(ctx, n, s.query)
}

// Serialized with other creates and node heartbeats by the node row lock.
// Queued seats consume capacity before the node has reported their reservation.
func checkSeatCapacityTx(ctx context.Context, tx repo.Tx, seat Seat) error {
	var n Node
	n.ID = seat.NodeID
	if err := tx.QueryRow(ctx, `SELECT labels,capacity,state,last_seen_at FROM g4_node WHERE id=$1 FOR UPDATE`, n.ID).Scan(&n.Labels, &n.Capacity, &n.State, &n.LastSeenAt); err != nil {
		return err
	}
	mode, _ := registeredQuota(n)
	if mode == "hard" {
		return nil
	}
	if mode != "soft" || n.State != "online" || !nodeQuotaEligible(n) {
		return ErrNoNodeCapacity
	}
	v, err := effectiveSeatCapacity(ctx, n, tx.Query)
	if err != nil {
		return err
	}
	plan, ok := PlanByID(seat.Plan)
	if !ok || v == nil || len(v.CanAdd(v.Policy(), capacityPlan(plan))) != 0 {
		return ErrNoNodeCapacity
	}
	return nil
}
