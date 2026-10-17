package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Independent inode emergency is leader-added scope, ruling eeb21f5b A1.
func TestSoftInodeEmergencyIndependentConfiguration(t *testing.T) {
	p := DefaultNodePolicy()
	if p.InodeEmergencyReservePercent != 50 {
		t.Fatal("inode emergency default must be50")
	}
	original := p.Revision()
	p.InodeEmergencyReservePercent = 75
	if p.Validate() != nil || p.Revision() == original || p.EmergencyReservePercent != 50 {
		t.Fatal("inode configuration not independent/revision bound")
	}
	for _, value := range []string{"0", "100", "-1", "not-a-number", "18446744073709551616"} {
		if _, err := ParseNodePolicy(func(k string) string {
			if k == "NODEAGENT_INODE_EMERGENCY_RESERVE_PERCENT" {
				return value
			}
			return ""
		}); err == nil {
			t.Fatal("invalid inode emergency accepted", value)
		}
	}
	for _, value := range []uint64{1, 37, 99} {
		got, err := ParseNodePolicy(func(k string) string {
			if k == "NODEAGENT_INODE_EMERGENCY_RESERVE_PERCENT" {
				return fmt.Sprint(value)
			}
			return ""
		})
		if err != nil || got.InodeEmergencyReservePercent != value || got.EmergencyReservePercent != 50 {
			t.Fatal("independent inode override ignored", value, err)
		}
	}
	q, _ := softFixture(t)
	q.policy.ReserveInodes = 1
	q.policy.ReserveInodePercent = 10
	q.policy.InodeEmergencyReservePercent = 50
	space := fsSpace{1 << 40, 1 << 40, 199, 199}
	if _, i := q.admissionReserves(space); i != 20 || q.inodeEmergencyThreshold(space) != 10 {
		t.Fatal("actual inode total truncated before percentage")
	}
	q.policy.ReserveInodes = 21
	if q.inodeEmergencyThreshold(space) != 11 {
		t.Fatal("fixed/odd inode reserve not rounded safely")
	}
}

func TestSoftInodeEmergencyBoundariesPersistStopAndRetainData(t *testing.T) {
	for _, offset := range []int64{-1, 0, 1} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			q, p := softFixture(t)
			// Deliberately diverge the percentages so sharing byte configuration fails.
			q.policy.EmergencyReservePercent = 1
			q.policy.InodeEmergencyReservePercent = 75
			p = q.policy
			reserveFixture(t, q, "ag_one")
			if err := q.track("ag_one", "cid"); err != nil {
				t.Fatal(err)
			}
			business := filepath.Join(seatDir(q.root, "ag_one"), "business")
			if err := os.WriteFile(business, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			space := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
			// I=max(65536, 1% of10million)=100000; independently configured75%=75000.
			space.FreeFiles = uint64(int64(75000) + offset)
			q.disk = func(string) (fsSpace, error) { return space, nil }
			q.sampleNode()
			q.checkReserve()
			stops := 0
			q.enforce(context.Background(), func(context.Context, string, string) error {
				raw, err := os.ReadFile(q.path)
				if err != nil {
					t.Error(err)
					return err
				}
				var state softState
				if json.Unmarshal(raw, &state) != nil || state.Seats["ag_one"].Hold != "node_inode_emergency" {
					t.Error("inode stop before correct durable intent")
					return fmt.Errorf("fixture rejected incorrect durable intent")
				}
				stops++
				return nil
			})
			held := offset < 0
			if (q.state.Seats["ag_one"].Hold != "") != held || (stops == 1) != held {
				t.Fatalf("inode boundary hold=%s stops=%d", q.state.Seats["ag_one"].Hold, stops)
			}
			if held && q.state.Seats["ag_one"].Hold != "node_inode_emergency" {
				t.Fatal("wrong independent inode reason")
			}
			if q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}) == nil {
				t.Fatal("original I admission gate weakened")
			}
			if raw, err := os.ReadFile(business); err != nil || string(raw) != "keep" {
				t.Fatal("inode hold deleted/changed data")
			}
			if !held {
				return
			}
			q.close()
			restored, err := newSoftQuota(p, q.root, q.path, q.nodeID)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close()
			restored.resources = q.resources
			restored.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 10000000}, nil }
			restored.sampleNode()
			restored.checkReserve()
			if restored.state.Seats["ag_one"].Hold != "node_inode_emergency" {
				t.Fatal("recovery/restart released inode hold")
			}
		})
	}
}
