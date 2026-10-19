package nodeagent

import (
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pre-R31 feature tests that wire handlers directly must now supply the node
// identity/private ledger and a measured capacity frame. Keep that test-only
// assumption explicit; production wiring and unknown/admission negatives are
// exercised separately by r31_hard_capacity_test.go.
func (a *Agent) registerR31LegacyFixture(t *testing.T, eng *engine, quota quotaOps, seatRoot, registry, cgroupParent, seatProxy, hubWSS, localImagesCSV string, seatHubViaProxy bool, seatCodexModel, seatMode string, seatModels []string, policies ...NodePolicy) error {
	t.Helper()
	// All callers provide positive seat roots; invalid policy/model inputs remain untouched.
	if err := os.Chmod(seatRoot, 0700); err != nil {
		return err
	}
	if a.cfg.NodeID == "" {
		a.cfg.NodeID = "nd_legacy_fixture"
	}
	if a.cfg.LedgerPath == "" {
		private := t.TempDir()
		if err := os.Chmod(private, 0700); err != nil {
			return err
		}
		a.cfg.LedgerPath = filepath.Join(private, "ledger")
	}
	err := a.RegisterDockerHandlers(eng, quota, seatRoot, registry, cgroupParent, seatProxy, hubWSS, localImagesCSV, seatHubViaProxy, seatCodexModel, seatMode, seatModels, policies...)
	if err == nil && a.docker.capacityQuota != nil {
		q := a.docker.capacityQuota
		q.capacity.snapshot = seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 64000, Memory: 64 << 30, Disk: 1 << 40}}
		t.Cleanup(a.policyClose)
	}
	return err
}
