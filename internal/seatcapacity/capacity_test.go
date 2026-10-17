package seatcapacity

import (
	"reflect"
	"testing"
	"time"
)

func TestOwnerEightPassNineRefusedNetResources(t *testing.T) {
	p := DefaultPolicy()
	now := time.Unix(100, 0)
	s := Snapshot{Known: true, At: now, Total: Resources{8000, 16 << 30, 320 << 30}}
	managed := []Resources{}
	for n := 1; n <= 8; n++ {
		v := Evaluate(p, s, managed, now, time.Minute)
		if v.TotalSlots != 8 || len(v.CanAdd(p, p.Minimum)) != 0 {
			t.Fatalf("seat%d: %+v", n, v)
		}
		managed = append(managed, p.Minimum)
	}
	v := Evaluate(p, s, managed, now, time.Minute)
	if v.AvailableSlots != 0 || !reflect.DeepEqual(v.CanAdd(p, p.Minimum), []string{"cpu", "disk"}) {
		t.Fatalf("ninth not refused: %+v", v)
	}
	if len(v.CanStart()) != 0 {
		t.Fatal("existing reservation charged twice")
	}
	if len(managed) != 8 {
		t.Fatal("evaluation modified running state")
	}
}
func TestDimensionsReservesAndActualLargerRequests(t *testing.T) {
	now := time.Unix(100, 0)
	p := DefaultPolicy()
	s := Snapshot{Known: true, At: now, Total: Resources{12000, 24 << 30, 480 << 30}, Reserved: Resources{2000, 4 << 30, 80 << 30}, External: Resources{2000, 4 << 30, 80 << 30}}
	v := Evaluate(p, s, nil, now, time.Minute)
	if v.TotalSlots != 8 || v.CPU.Allocatable != 8000 || v.Memory.Allocatable != 16<<30 || v.Disk.Allocatable != 320<<30 {
		t.Fatalf("deduction %+v", v)
	}
	for i, resource := range []Resources{{9000, 0, 0}, {0, 17 << 30, 0}, {0, 0, 321 << 30}} {
		reasons := v.CanAdd(p, resource)
		if len(reasons) != 1 || reasons[0] != []string{"cpu", "memory", "disk"}[i] {
			t.Fatalf("dimension%d: %v", i, reasons)
		}
	}
	p.Minimum = Resources{2000, 4 << 30, 80 << 30}
	if Evaluate(p, s, nil, now, time.Minute).TotalSlots != 4 {
		t.Fatal("configured minimum ignored")
	}
}
func TestUnknownIsNeverZeroAndAgeBoundaries(t *testing.T) {
	now := time.Unix(100, 0)
	p := DefaultPolicy()
	s := Snapshot{Known: true, At: now.Add(-time.Minute), Total: Resources{8000, 16 << 30, 320 << 30}}
	if !Evaluate(p, s, nil, now, time.Minute).Known {
		t.Fatal("equality snapshot expired")
	}
	for _, mutate := range []func(*Snapshot){func(s *Snapshot) { s.Known = false }, func(s *Snapshot) { s.At = s.At.Add(-time.Nanosecond) }, func(s *Snapshot) { s.At = now.Add(time.Second) }, func(s *Snapshot) { s.External.CPU = -1 }} {
		x := s
		mutate(&x)
		v := Evaluate(p, x, nil, now, time.Minute)
		if v.Known || len(v.CanAdd(p, p.Minimum)) == 0 || len(v.CanStart()) == 0 {
			t.Fatalf("unknown admitted: %+v", v)
		}
	}
}
func TestExistingStartRefusedAfterCapacityLossWithoutChangingReservations(t *testing.T) {
	now := time.Unix(100, 0)
	p := DefaultPolicy()
	s := Snapshot{Known: true, At: now, Total: Resources{1000, 2 << 30, 40 << 30}}
	managed := []Resources{p.Minimum}
	before := append([]Resources(nil), managed...)
	s.External.CPU = 1
	v := Evaluate(p, s, managed, now, time.Minute)
	if !reflect.DeepEqual(v.CanStart(), []string{"cpu"}) || !reflect.DeepEqual(managed, before) {
		t.Fatal("start loss or mutation", v)
	}
}
