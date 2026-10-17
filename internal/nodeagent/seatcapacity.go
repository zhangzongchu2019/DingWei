package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

// Installed only by the production soft-mode wiring. Isolated legacy component
// tests may omit it; actual soft registration cannot opt out. The single reader
// owns CPU counter history, while cached publication uses the quota mutex only.
type seatCapacityState struct {
	snapshot seatcapacity.Snapshot
	sampling bool
	started  time.Time
	read     func(context.Context) (seatcapacity.Snapshot, error)
}

var ErrSeatCapacity = errors.New("seat_capacity_blocked")

func capacityResources(r seatResources) seatcapacity.Resources {
	if r.validate() != nil || r.DiskMB <= 0 || r.DiskMB > 1<<40 {
		return seatcapacity.Resources{CPU: -1, Memory: -1, Disk: -1}
	}
	return seatcapacity.Resources{CPU: r.cpuMillicores(), Memory: r.MemoryMB << 20, Disk: r.DiskMB << 20}
}

func (q *softQuota) capacityViewLocked() seatcapacity.View {
	var snapshot seatcapacity.Snapshot
	if q.capacity != nil {
		snapshot = q.capacity.snapshot
		if q.capacity.sampling && (q.now().Before(q.capacity.started) || q.now().Sub(q.capacity.started) > q.policy.ScanBudget) {
			snapshot.Known = false
		}
	}
	managed := make([]seatcapacity.Resources, 0, len(q.state.Seats))
	for _, seat := range q.state.Seats {
		r := capacityResources(seat.Resources)
		if seat.WriterKind == "login" {
			// Temporary login writers consume their actual budget, not a 40-GiB
			// business-seat slot. The node service itself stays in fixed reserve.
			if r.CPU < 0 || r.Memory < 0 || r.Disk < 0 || snapshot.Reserved.CPU > 1<<60-r.CPU || snapshot.Reserved.Memory > 1<<60-r.Memory || snapshot.Reserved.Disk > 1<<60-r.Disk {
				snapshot.Known = false
				continue
			}
			snapshot.Reserved.CPU += r.CPU
			snapshot.Reserved.Memory += r.Memory
			snapshot.Reserved.Disk += r.Disk
			continue
		}
		if seat.WriterKind != "" {
			snapshot.Known = false
		}
		managed = append(managed, r)
	}
	return seatcapacity.Evaluate(q.policy.capacityPolicy(), snapshot, managed, q.now(), min(q.policy.MaxAge, time.Minute))
}

func (q *softQuota) capacityAdmissionLocked(add *seatResources) error {
	if q.capacity == nil {
		return nil
	}
	v := q.capacityViewLocked()
	reasons := v.CanStart()
	if add != nil {
		reasons = v.CanAdd(q.policy.capacityPolicy(), capacityResources(*add))
	}
	if len(reasons) != 0 {
		return fmt.Errorf("%w: seat_capacity_%s", ErrSeatCapacity, strings.Join(reasons, "+"))
	}
	return nil
}

func (q *softQuota) capacityReport() *seatcapacity.View {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.capacity == nil {
		return nil
	}
	v := q.capacityViewLocked()
	return &v
}

// No request or tick can spawn a replacement for a blocked reader. IO never
// holds q.mu and never updates old root-health/hold state. Its age/5s budget
// independently closes new admission; running seats are unaffected by B.
func (d *dockerOps) runSeatCapacity(ctx context.Context) {
	q := d.soft
	if q == nil || q.capacity == nil {
		<-ctx.Done()
		return
	}
	reader := newSeatCapacityReader(d)
	read := reader.sample
	if q.capacity.read != nil {
		read = q.capacity.read
	}
	ticker := time.NewTicker(q.policy.ReserveInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		q.mu.Lock()
		q.capacity.sampling = true
		q.capacity.started = q.now()
		q.mu.Unlock()
		c, cancel := context.WithTimeout(ctx, q.policy.ScanBudget)
		snapshot, err := read(c)
		if c.Err() != nil {
			err = c.Err()
		}
		cancel()
		q.mu.Lock()
		if err != nil {
			snapshot.Known = false
		}
		q.capacity.snapshot = snapshot
		q.capacity.sampling = false
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
