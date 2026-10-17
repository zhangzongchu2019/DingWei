package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

type cpuCounters struct{ total, busy uint64 }

func parseCapacityMemory(raw []byte) (int64, int64, error) {
	var total, available int64
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
			continue
		}
		if seen[fields[0]] || len(fields) != 3 || fields[2] != "kB" {
			return 0, 0, fmt.Errorf("memory counters invalid")
		}
		seen[fields[0]] = true
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || n < 0 || n > 1<<50 {
			return 0, 0, fmt.Errorf("memory counters invalid")
		}
		if fields[0] == "MemTotal:" {
			total = n << 10
		} else {
			available = n << 10
		}
	}
	if !seen["MemTotal:"] || !seen["MemAvailable:"] || total <= 0 || available > total {
		return 0, 0, fmt.Errorf("memory counters unknown")
	}
	return total, available, nil
}

// Linux aggregate CPU fields exclude guest/guest_nice, already included in
// user/nice. IO wait is not running CPU. Missing/overflowing counters are unknown.
func parseCapacityCPU(raw []byte) (cpuCounters, error) {
	fields := strings.Fields(strings.SplitN(string(raw), "\n", 2)[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		return cpuCounters{}, fmt.Errorf("CPU counters unavailable")
	}
	var out cpuCounters
	for i := 1; i <= 8; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil || math.MaxUint64-out.total < n {
			return cpuCounters{}, fmt.Errorf("CPU counters invalid")
		}
		out.total += n
		if i != 4 && i != 5 {
			if math.MaxUint64-out.busy < n {
				return cpuCounters{}, fmt.Errorf("CPU counters invalid")
			}
			out.busy += n
		}
	}
	return out, nil
}

type managedCPUCounter struct {
	usage  uint64
	system uint64
}
type ownedCapacityUsage struct {
	cpu    int64
	memory int64
}

type capacityWriter struct {
	cids              []string
	root, destination string
	login             bool
}

var errCapacityWarming = errors.New("capacity CPU warming or reset")

type seatCapacityReader struct {
	d        *dockerOps
	previous cpuCounters
	warmed   bool
	owned    map[string]managedCPUCounter
	readCPU  func() (cpuCounters, error)
	usage    func(context.Context, string, string, map[[2]uint64]bool) (diskUsage, error)
}

func newSeatCapacityReader(d *dockerOps) *seatCapacityReader {
	return &seatCapacityReader{d: d, owned: map[string]managedCPUCounter{}, usage: measureUsageSeen, readCPU: func() (cpuCounters, error) {
		raw, err := os.ReadFile("/proc/stat")
		if err != nil {
			return cpuCounters{}, err
		}
		return parseCapacityCPU(raw)
	}}
}

func (r *seatCapacityReader) sample(ctx context.Context) (snapshot seatcapacity.Snapshot, err error) {
	defer func() {
		// Never compare a fresh host window with a managed counter retained
		// across failed/partial samples: that could subtract stale workload.
		if err != nil && !errors.Is(err, errCapacityWarming) {
			r.warmed = false
			r.owned = map[string]managedCPUCounter{}
		}
	}()
	return r.sampleOnce(ctx)
}

func (r *seatCapacityReader) sampleOnce(ctx context.Context) (seatcapacity.Snapshot, error) {
	q := r.d.soft
	s := seatcapacity.Snapshot{At: q.now()}
	cpu, total, available, err := q.resources()
	if err != nil || cpu <= 0 || total <= 0 || available < 0 || available > total {
		return s, fmt.Errorf("capacity resources unknown")
	}
	space, err := q.disk(q.root)
	if err != nil || !validNodeSpace(space) {
		return s, fmt.Errorf("capacity space unknown")
	}
	counters, err := r.readCPU()
	if err != nil {
		r.warmed = false
		return s, err
	}
	previous, warmed := r.previous, r.warmed
	r.previous, r.warmed = counters, true
	if !warmed || counters.total <= previous.total || counters.busy < previous.busy {
		r.owned = map[string]managedCPUCounter{}
		return s, errCapacityWarming
	}
	busy := int64(math.Ceil(float64(cpu) * float64(counters.busy-previous.busy) / float64(counters.total-previous.total)))
	if busy < 0 || busy > cpu {
		return s, fmt.Errorf("capacity CPU invalid")
	}
	q.mu.Lock()
	seats := make(map[string]capacityWriter, len(q.state.Seats))
	for id, seat := range q.state.Seats {
		w := capacityWriter{cids: append([]string(nil), seat.CIDs...), root: q.root, destination: "/work"}
		if seat.WriterKind == "login" {
			if !strings.HasPrefix(id, "login-") || !loginIDPattern.MatchString(strings.TrimPrefix(id, "login-")) || !filepath.IsAbs(seat.WriterRoot) || filepath.Clean(seat.WriterRoot) != seat.WriterRoot {
				q.mu.Unlock()
				return s, fmt.Errorf("capacity writer metadata invalid")
			}
			w.root, w.destination, w.login = seat.WriterRoot, "/h", true
		} else if seat.WriterKind != "" {
			q.mu.Unlock()
			return s, fmt.Errorf("capacity writer metadata invalid")
		}
		seats[id] = w
	}
	q.mu.Unlock()
	seen := map[[2]uint64]bool{}
	seenCID := map[string]bool{}
	var allocated, ownedCPU, ownedMemory int64
	for id, w := range seats {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		inodeSet := seen
		if w.login {
			inodeSet = map[[2]uint64]bool{}
		}
		u, err := r.usage(ctx, w.root, id, inodeSet)
		if err != nil {
			// A durable reservation precedes mkdir; it cannot have a writer yet.
			if !errors.Is(err, os.ErrNotExist) || len(w.cids) != 0 {
				return s, fmt.Errorf("capacity usage unknown")
			}
		} else if !w.login {
			if u.Allocated < 0 || allocated > math.MaxInt64-u.Allocated {
				return s, fmt.Errorf("capacity usage invalid")
			}
			allocated += u.Allocated
		}
		for _, cid := range w.cids {
			if seenCID[cid] || !safeContainerID.MatchString(cid) {
				return s, fmt.Errorf("capacity container invalid")
			}
			seenCID[cid] = true
			owned, err := r.containerUsage(ctx, id, cid, cpu, w.root, w.destination)
			if err != nil {
				return s, err
			}
			if ownedCPU > math.MaxInt64-owned.cpu || ownedMemory > math.MaxInt64-owned.memory {
				return s, fmt.Errorf("capacity usage invalid")
			}
			ownedCPU += owned.cpu
			ownedMemory += owned.memory
		}
	}
	for cid := range r.owned {
		if !seenCID[cid] {
			delete(r.owned, cid)
		}
	}
	if ownedMemory > total || allocated > space.Total {
		return s, fmt.Errorf("capacity usage exceeds filesystem or memory")
	}
	reserve, _ := q.admissionReserves(space)
	if reserve > math.MaxInt64-q.policy.ImageReserveBytes {
		return s, fmt.Errorf("capacity reserve invalid")
	}
	s.Total = seatcapacity.Resources{CPU: cpu, Memory: total, Disk: space.Total}
	s.Reserved = seatcapacity.Resources{CPU: q.policy.ReserveCPUMillicores, Memory: q.policy.ReserveMemoryBytes, Disk: reserve + q.policy.ImageReserveBytes}
	// Linux/Docker samples are bounded-window measurements, not instantaneous
	// guarantees. Untracked containers, old Engines and host processes stay in
	// host occupation. Subtract only identified managed CPU/anonymous RSS and
	// the metadata-only physical inode union; never subtract configured limits.
	s.External = seatcapacity.Resources{CPU: max(int64(0), busy-ownedCPU), Memory: max(int64(0), total-available-ownedMemory), Disk: max(int64(0), space.Total-space.Free-allocated)}
	s.Known = true
	return s, ctx.Err()
}

func (r *seatCapacityReader) containerUsage(ctx context.Context, id, cid string, cpus int64, root, destination string) (ownedCapacityUsage, error) {
	var info struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State  struct{ Running bool }                 `json:"State"`
		Mounts []struct{ Source, Destination string } `json:"Mounts"`
	}
	err := r.d.eng.call(ctx, "GET", "/containers/"+cid+"/json", nil, &info)
	if err != nil {
		var ee *engineError
		if errors.As(err, &ee) && ee.Status == 404 {
			delete(r.owned, cid)
			return ownedCapacityUsage{}, nil
		}
		return ownedCapacityUsage{}, fmt.Errorf("capacity inspect unavailable")
	}
	owned := info.Config.Labels[managedSeatIDLabel] == id
	if destination == "/h" {
		owned = info.Config.Labels[loginLabel] == strings.TrimPrefix(id, "login-")
	}
	if info.ID != cid || info.Config.Labels[managedSeatLabel] != r.d.soft.nodeID || !owned {
		return ownedCapacityUsage{}, fmt.Errorf("capacity ownership mismatch")
	}
	bound := false
	for _, m := range info.Mounts {
		if m.Destination == destination && m.Source == seatDir(root, id) {
			bound = true
		}
	}
	if !bound {
		return ownedCapacityUsage{}, fmt.Errorf("capacity mount mismatch")
	}
	if !info.State.Running {
		delete(r.owned, cid)
		return ownedCapacityUsage{}, nil
	}
	var stats struct {
		Read time.Time `json:"read"`
		CPU  struct {
			Usage struct {
				Total *uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System *uint64 `json:"system_cpu_usage"`
		} `json:"cpu_stats"`
		Memory struct {
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := r.d.eng.call(ctx, "GET", "/containers/"+cid+"/stats?stream=false&one-shot=true", nil, &stats); err != nil {
		return ownedCapacityUsage{}, fmt.Errorf("capacity stats unavailable")
	}
	if stats.CPU.Usage.Total == nil || stats.CPU.System == nil || *stats.CPU.System == 0 || stats.Read.IsZero() || r.d.soft.now().Before(stats.Read) || r.d.soft.now().Sub(stats.Read) > r.d.policyValue().ScanBudget {
		return ownedCapacityUsage{}, fmt.Errorf("capacity stats invalid")
	}
	memory, ok := stats.Memory.Stats["anon"]
	if !ok {
		memory, ok = stats.Memory.Stats["total_rss"]
	}
	if !ok || memory > 1<<60 {
		return ownedCapacityUsage{}, fmt.Errorf("capacity memory unknown")
	}
	current := managedCPUCounter{*stats.CPU.Usage.Total, *stats.CPU.System}
	previous, known := r.owned[cid]
	r.owned[cid] = current
	usage := int64(0)
	if known {
		if current.usage < previous.usage || current.system <= previous.system {
			return ownedCapacityUsage{}, fmt.Errorf("capacity stats reset")
		}
		usage = int64(math.Floor(float64(cpus) * float64(current.usage-previous.usage) / float64(current.system-previous.system)))
		if usage < 0 || usage > cpus {
			return ownedCapacityUsage{}, fmt.Errorf("capacity stats invalid")
		}
	}
	return ownedCapacityUsage{cpu: usage, memory: int64(memory)}, nil
}
