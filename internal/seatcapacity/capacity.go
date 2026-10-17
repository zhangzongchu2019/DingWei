// Package seatcapacity computes admission from explicit, independently measured
// node resources. It performs no IO and never changes running workloads.
package seatcapacity

import (
	"fmt"
	"math"
	"time"
)

type Resources struct {
	CPU    int64 `json:"cpu_millicores"`
	Memory int64 `json:"memory_bytes"`
	Disk   int64 `json:"disk_bytes"`
}
type Policy struct {
	Minimum  Resources `json:"minimum"`
	MaxSeats int       `json:"max_seats"`
}

func DefaultPolicy() Policy {
	return Policy{Minimum: Resources{1000, 1536 << 20, 40 << 30}, MaxSeats: 100}
}
func (p Policy) Validate() error {
	if p.Minimum.CPU <= 0 || p.Minimum.CPU > 1000000 || p.Minimum.Memory < 4<<20 || p.Minimum.Memory > 1<<50 || p.Minimum.Disk < 1<<20 || p.Minimum.Disk > 1<<50 || p.MaxSeats <= 0 || p.MaxSeats > 100 {
		return fmt.Errorf("invalid seat capacity policy")
	}
	return nil
}

// External is non-managed occupation, never an implicit zero for unknown IO.
// A conservative upper bound is allowed only if the sampler labels that basis.
type Snapshot struct {
	At                        time.Time
	Known                     bool
	Total, Reserved, External Resources
}
type Dimension struct {
	Total           int64 `json:"total"`
	NodeReserve     int64 `json:"node_reserve"`
	ExternalUsed    int64 `json:"external_used"`
	ManagedReserved int64 `json:"managed_reserved"`
	Allocatable     int64 `json:"allocatable"`
	Available       int64 `json:"available"`
	Slots           int64 `json:"slots"`
	RemainingSlots  int64 `json:"remaining_slots"`
}
type View struct {
	Version        int       `json:"version"`
	MaxSeats       int       `json:"max_seats"`
	Known          bool      `json:"known"`
	At             time.Time `json:"at"`
	Minimum        Resources `json:"minimum"`
	CPU            Dimension `json:"cpu"`
	Memory         Dimension `json:"memory"`
	Disk           Dimension `json:"disk"`
	TotalSlots     int64     `json:"total_slots"`
	Occupied       int       `json:"occupied"`
	AvailableSlots int64     `json:"available_slots"`
	Reasons        []string  `json:"reasons"`
}

func values(r Resources) [3]int64 { return [3]int64{r.CPU, r.Memory, r.Disk} }
func valid(r Resources) bool {
	return r.CPU >= 0 && r.CPU <= 1<<60 && r.Memory >= 0 && r.Memory <= 1<<60 && r.Disk >= 0 && r.Disk <= 1<<60
}
func Charge(r Resources, p Policy) Resources {
	return Resources{max(r.CPU, p.Minimum.CPU), max(r.Memory, p.Minimum.Memory), max(r.Disk, p.Minimum.Disk)}
}

func Evaluate(p Policy, s Snapshot, managed []Resources, now time.Time, maxAge time.Duration) View {
	v := View{Version: 1, MaxSeats: p.MaxSeats, Minimum: p.Minimum, At: s.At, Occupied: len(managed), Reasons: []string{}}
	if p.Validate() != nil {
		v.Reasons = []string{"policy_invalid"}
		return v
	}
	if !s.Known || s.At.IsZero() || maxAge <= 0 || now.Before(s.At) || now.Sub(s.At) > maxAge || !valid(s.Total) || !valid(s.Reserved) || !valid(s.External) {
		v.Reasons = []string{"load_unknown"}
		return v
	}
	used := [3]int64{}
	for _, r := range managed {
		if !valid(r) {
			v.Reasons = []string{"load_unknown"}
			return v
		}
		for i, x := range values(Charge(r, p)) {
			if used[i] > math.MaxInt64-x {
				v.Reasons = []string{"load_unknown"}
				return v
			}
			used[i] += x
		}
	}
	totals, reserved, external, minimum := values(s.Total), values(s.Reserved), values(s.External), values(p.Minimum)
	dimensions := []*Dimension{&v.CPU, &v.Memory, &v.Disk}
	names := []string{"cpu", "memory", "disk"}
	v.TotalSlots = int64(p.MaxSeats)
	v.AvailableSlots = int64(max(0, p.MaxSeats-len(managed)))
	for i, d := range dimensions {
		net := max(int64(0), totals[i]-reserved[i]-external[i])
		free := max(int64(0), net-used[i])
		*d = Dimension{Total: totals[i], NodeReserve: reserved[i], ExternalUsed: external[i], ManagedReserved: used[i], Allocatable: net, Available: free, Slots: net / minimum[i], RemainingSlots: free / minimum[i]}
		v.TotalSlots = min(v.TotalSlots, d.Slots)
		v.AvailableSlots = min(v.AvailableSlots, d.RemainingSlots)
		if d.RemainingSlots == 0 {
			v.Reasons = append(v.Reasons, names[i])
		}
	}
	if len(managed) >= p.MaxSeats {
		v.Reasons = append(v.Reasons, "seat_limit")
	}
	v.Known = true
	return v
}

// CanStart reuses an already reserved seat. Equality is valid; it never charges
// that reservation twice. Capacity loss denies this start but does not stop it.
func (v View) CanStart() []string {
	if !v.Known {
		if len(v.Reasons) == 0 {
			return []string{"load_unknown"}
		}
		return append([]string(nil), v.Reasons...)
	}
	reasons := []string{}
	for i, d := range []Dimension{v.CPU, v.Memory, v.Disk} {
		if d.ManagedReserved > d.Allocatable {
			reasons = append(reasons, []string{"cpu", "memory", "disk"}[i])
		}
	}
	return reasons
}
func (v View) CanAdd(p Policy, r Resources) []string {
	if !v.Known {
		if len(v.Reasons) == 0 {
			return []string{"load_unknown"}
		}
		return append([]string(nil), v.Reasons...)
	}
	if !valid(r) || p.Validate() != nil || p.Minimum != v.Minimum || p.MaxSeats != v.MaxSeats {
		return []string{"policy_invalid"}
	}
	reasons := []string{}
	for i, x := range values(Charge(r, p)) {
		if x > []Dimension{v.CPU, v.Memory, v.Disk}[i].Available {
			reasons = append(reasons, []string{"cpu", "memory", "disk"}[i])
		}
	}
	if v.Occupied >= p.MaxSeats {
		reasons = append(reasons, "seat_limit")
	}
	return reasons
}

func (v View) Policy() Policy { return Policy{Minimum: v.Minimum, MaxSeats: v.MaxSeats} }

// ValidateAt is used on the authenticated node's wire frame, not only on locally
// constructed values. Known=true with omitted or inconsistent fields is unknown.
func (v View) ValidateAt(now time.Time, maxAge time.Duration) bool {
	p := v.Policy()
	if v.Version != 1 || !v.Known || p.Validate() != nil || v.At.IsZero() || maxAge <= 0 || now.Before(v.At) || now.Sub(v.At) > maxAge || v.Occupied < 0 || v.Occupied > 100 {
		return false
	}
	total, remaining := int64(p.MaxSeats), int64(max(0, p.MaxSeats-v.Occupied))
	for i, d := range []Dimension{v.CPU, v.Memory, v.Disk} {
		if d.Total < 0 || d.Total > 1<<60 || d.NodeReserve < 0 || d.NodeReserve > 1<<60 || d.ExternalUsed < 0 || d.ExternalUsed > 1<<60 || d.ManagedReserved < 0 || d.ManagedReserved > 1<<60 {
			return false
		}
		net := max(int64(0), d.Total-d.NodeReserve-d.ExternalUsed)
		free := max(int64(0), net-d.ManagedReserved)
		minimum := values(p.Minimum)[i]
		if d.Allocatable != net || d.Available != free || d.Slots != net/minimum || d.RemainingSlots != free/minimum || d.ManagedReserved < int64(v.Occupied)*minimum {
			return false
		}
		total = min(total, d.Slots)
		remaining = min(remaining, d.RemainingSlots)
	}
	return v.TotalSlots == total && v.AvailableSlots == remaining
}

// Pending rows not yet in the node's durable seat list consume capacity too.
// Operates only on a copy for scheduling/options; node reservations stay final.
func (v View) WithPending(pending []Resources) View {
	p := v.Policy()
	if !v.Known || p.Validate() != nil {
		v.Known = false
		v.Reasons = []string{"load_unknown"}
		return v
	}
	for _, r := range pending {
		if !valid(r) {
			v.Known = false
			v.Reasons = []string{"load_unknown"}
			return v
		}
		for i, d := range []*Dimension{&v.CPU, &v.Memory, &v.Disk} {
			x := values(Charge(r, p))[i]
			if d.ManagedReserved > 1<<60-x {
				v.Known = false
				v.Reasons = []string{"load_unknown"}
				return v
			}
			d.ManagedReserved += x
		}
		v.Occupied++
	}
	v.TotalSlots = int64(p.MaxSeats)
	v.AvailableSlots = int64(max(0, p.MaxSeats-v.Occupied))
	v.Reasons = []string{}
	for i, d := range []*Dimension{&v.CPU, &v.Memory, &v.Disk} {
		d.Available = max(int64(0), d.Allocatable-d.ManagedReserved)
		d.RemainingSlots = d.Available / values(p.Minimum)[i]
		v.TotalSlots = min(v.TotalSlots, d.Slots)
		v.AvailableSlots = min(v.AvailableSlots, d.RemainingSlots)
		if d.RemainingSlots == 0 {
			v.Reasons = append(v.Reasons, []string{"cpu", "memory", "disk"}[i])
		}
	}
	if v.Occupied >= p.MaxSeats {
		v.Reasons = append(v.Reasons, "seat_limit")
	}
	return v
}
