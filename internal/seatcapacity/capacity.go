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
	v := View{Version: 1, Minimum: p.Minimum, At: s.At, Occupied: len(managed), Reasons: []string{}}
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
	if !valid(r) || p.Validate() != nil {
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
