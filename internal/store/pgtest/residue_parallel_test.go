package pgtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResidueSamplesRequireSameNewZombie(t *testing.T) {
	set := func(ids ...int) map[int]struct{} {
		out := map[int]struct{}{}
		for _, pid := range ids {
			out[pid] = struct{}{}
		}
		return out
	}
	for _, tc := range []struct {
		name                  string
		before, first, second map[int]struct{}
		want                  string
	}{
		{"two concurrent owners finish in turn", set(), set(101), set(202), "[]"},
		{"persistent new orphan remains red", set(), set(101), set(101), "[101]"},
		{"history excluded but persistent new retained", set(7), set(7, 101, 202), set(7, 202, 303), "[202]"},
		{"first sample clean", set(), set(), set(101), "[]"},
		{"reaped before second", set(), set(101), set(), "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprint(persistentAddedPIDs(tc.before, tc.first, tc.second)); got != tc.want {
				t.Fatalf("ROTATING_ZOMBIES_FALSE_RESIDUE: got %s want %s", got, tc.want)
			}
		})
	}
}

// Real /proc control: retain one child zombie across both reads (must be found),
// then reap it and create another (must not be labelled the same residue).
// Both are our children and always Wait'ed; no new PID-1 orphan is created.
func TestResidueRotatingRealChildZombies(t *testing.T) {
	if _, ok := listZombiePostgres(); !ok {
		t.Skip("/proc unavailable")
	}
	bin, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true unavailable")
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "postgres")
	if err := os.WriteFile(executable, data, 0700); err != nil {
		t.Fatal(err)
	}
	var children []*exec.Cmd
	defer func() {
		for _, cmd := range children {
			if cmd.ProcessState == nil {
				_ = cmd.Wait()
			}
		}
	}()
	spawn := func() *exec.Cmd {
		cmd := exec.Command(executable)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			if _, ok := listZombiePostgresOrEmpty()[cmd.Process.Pid]; ok {
				return cmd
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("child did not reach zombie state")
		return nil
	}
	before := listZombiePostgresOrEmpty()
	a := spawn()
	first := map[int]struct{}{a.Process.Pid: {}}
	same := listZombiePostgresOrEmpty()
	if got := persistentAddedPIDs(before, first, same); len(got) != 1 {
		t.Fatalf("persistent child must remain detected: %v", got)
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	b := spawn()
	second := map[int]struct{}{b.Process.Pid: {}}
	t.Logf("real sampling counterexample: first PID=%d reaped; second PID=%d PPID=%d; both individual deltas nonempty", a.Process.Pid, b.Process.Pid, os.Getpid())
	if got := persistentAddedPIDs(before, first, second); len(got) != 0 {
		t.Fatalf("ROTATING_REAL_ZOMBIES_FALSE_RESIDUE: %v", got)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
}

// Run the actual t.Errorf guard in child test processes, so expected failures
// are observable without making the parent suite fail or replacing its logger.
func TestResidueGuardRecheckEndToEnd(t *testing.T) {
	for _, mode := range []string{"rotating", "persistent"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestResidueGuardSampleProbe$", "-test.v")
		cmd.Env = append(os.Environ(), "PGTEST_RESIDUE_SAMPLE_PROBE="+mode)
		out, err := cmd.CombinedOutput()
		if mode == "rotating" {
			if err != nil {
				t.Fatalf("ROTATING_GUARD_FALSE_POSITIVE: %v: %s", err, out)
			}
		} else if err == nil || !strings.Contains(string(out), "postgres 僵尸(pid: 101)") {
			t.Fatalf("PERSISTENT_GUARD_DISABLED: err=%v output=%s", err, out)
		}
	}
}

func TestResidueGuardSampleProbe(t *testing.T) {
	mode := os.Getenv("PGTEST_RESIDUE_SAMPLE_PROBE")
	if mode == "" {
		return
	}
	calls := 0
	w := &residueWatch{zombies: map[int]struct{}{}, zombiesOK: true, readZombies: func() map[int]struct{} {
		calls++
		if calls == 1 || mode == "persistent" {
			return map[int]struct{}{101: {}}
		}
		return map[int]struct{}{202: {}}
	}}
	w.check(t)
	if calls != 2 {
		t.Fatalf("expected two guard samples, got %d", calls)
	}
}
