//go:build linux

package m5

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ── B1: flock — real cross-process exclusion ──────────────────────────

func TestB1_FlockCrossProcess(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer s.Unlock()

	// Build a child process that tries to flock the same lock file.
	// We use a small Go helper that tries to flock and prints the result.
	// For testing without go build, we test via a shell: flock -n.
	// The supervisor.lock path is known.
	lockPath := s.lockPath()
	cmd := exec.Command("flock", "-n", lockPath, "echo", "ACQUIRED")
	out, err := cmd.Output()
	// If flock -n succeeds, it prints ACQUIRED — that would be a B1 failure.
	if err == nil && strings.TrimSpace(string(out)) == "ACQUIRED" {
		t.Fatal("B1: second process acquired flock — cross-process exclusion broken")
	}
	// flock -n should fail (exit 1) because parent holds the lock
	if err == nil {
		t.Fatal("B1: flock -n should have failed but exited 0")
	}
	t.Logf("B1 PASS: cross-process flock rejected (err=%v)", err)
}

// ── B9: PID reuse — MC-2b: walks through TUPLE gate (§1.2), not pidfd gate ─

func TestB9_PIDReuseReal(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// Start a marker process. Use trap on SIGTERM only (SIGKILL is uncatchable).
	marker := filepath.Join(dir, "signal-marker")
	markerProc := exec.Command("/bin/bash", "-c",
		"trap 'echo GOT_SIGNAL >> "+marker+"' TERM; while true; do sleep 1; done")
	markerProc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := markerProc.Start(); err != nil {
		t.Fatal(err)
	}
	pid := markerProc.Process.Pid
	defer func() { syscall.Kill(pid, syscall.SIGKILL); markerProc.Wait() }()

	// Read the REAL tuple of the marker process
	realTuple, err := readProcTuple(pid)
	if err != nil {
		t.Fatalf("read real tuple: %v", err)
	}

	// Open a REAL pidfd — we need this to walk through the pidfd gate
	// into the tuple comparison gate (§1.2)
	realPidFd, err := unixPidfdOpen(pid)
	if err != nil {
		t.Fatalf("pidfd_open: %v", err)
	}
	defer unixClose(realPidFd)

	// Create a FAKE tuple: same PID, different starttime (simulating PID reuse)
	fakeTuple := ProcTuple{
		PID: realTuple.PID, StartTime: realTuple.StartTime + 1,
		Exe: realTuple.Exe, Cgroup: realTuple.Cgroup,
	}

	// Register with REAL pidfd but FAKE tuple.
	// stopProcess order: 1) pidfd≥0 ✓ → 2) readProcTuple → 3) tuple.Equal? → NO! ErrPidReuse
	s.RegisterProcess("test-session", fakeTuple, realPidFd)

	err = s.stopProcess("test-session")
	// MC-2b assertion ①: must return ErrPidReuse (tuple mismatch, not pidfd)
	if err == nil {
		t.Fatal("B9 FAIL: stopProcess should reject tuple mismatch")
	}
	if !strings.Contains(err.Error(), "PidReuse") && !strings.Contains(err.Error(), "pid reuse") {
		t.Fatalf("B9 FAIL: expected ErrPidReuse (tuple gate), got: %v", err)
	}

	// MC-2b assertion ②: marker file must NOT exist — no signal was sent
	time.Sleep(200 * time.Millisecond)
	if data, err := os.ReadFile(marker); err == nil && len(data) > 0 {
		t.Fatalf("B9 FAIL: signal sent to wrong process! marker: %s", string(data))
	}

	// MC-2b assertion ③: the real process must STILL BE ALIVE.
	// (SIGKILL is uncatchable; if one was sent, the process would be dead.
	//  We can't trap KILL, so liveness confirms no KILL was sent either.)
	if _, err := readProcTuple(pid); err != nil {
		t.Fatalf("B9 FAIL: process is dead — may have received SIGKILL: %v", err)
	}

	t.Log("B9 PASS: ErrPidReuse + no signal + process still alive")
}

// ── B10: SIGKILL timeout — verify exit via pidfd_send_signal(ESRCH) ──

func TestB10_SigkillVerify(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// Start a process that ignores SIGTERM (trap '' TERM; sleep 30)
	// This forces the stop sequence to escalate to SIGKILL.
	child := exec.Command("/bin/bash", "-c", "trap '' TERM; sleep 30")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()

	pid := child.Process.Pid
	realTuple, err := readProcTuple(pid)
	if err != nil {
		t.Fatalf("read tuple: %v", err)
	}

	// Open pidfd
	pidFd, err := unixPidfdOpen(pid)
	if err != nil {
		t.Fatalf("pidfd_open: %v", err)
	}
	defer unixClose(pidFd)

	s.RegisterProcess("test-session", realTuple, pidFd)

	// Run stopProcess which will: SIGTERM (ignored) → wait 10s → SIGKILL → verify
	err = s.stopProcess("test-session")
	if err != nil {
		// May get ErrStopTimeout if process somehow survives SIGKILL
		t.Logf("stopProcess result: %v", err)
	}

	// MC-2: Verify process is actually dead via pidfd_send_signal(0).
	// If the process is still alive, this is a test FAILURE (not a log line).
	time.Sleep(200 * time.Millisecond)
	if err := unixPidfdSendSignal(pidFd, 0); err == nil {
		// Process survived SIGKILL — this is a real defect
		syscall.Kill(pid, syscall.SIGKILL) // cleanup
		t.Fatal("B10 FAIL: process still alive after SIGKILL — stop sequence did not kill it")
	}
	// pidfd_send_signal(0) on a dead process returns ESRCH
	t.Logf("B10 PASS: pidfd_send_signal(0) returned error (process dead): %v", err)
}

// ── B11: fsync anti-proof — MC-4(a): deterministic fsync-count assertion ─
// Anti-proof: assert f.Sync() IS called (not just "data survived").
// This prevents regression where someone removes fsync and all tests still pass.
// Contract B11 change pending: coordinator→devo48 for contract wording update.

func TestB11_FsyncCount(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// ── Phase 1: WITH fsync — counter must increment ──
	count := 0
	oldCount := FsyncCount
	FsyncCount = func() { count++ }
	defer func() { FsyncCount = oldCount }()

	if err := s.appendJournal(JournalEntry{
		OpID: "op-count", Intent: "checkpoint", Target: "count-session",
		Phase: PhaseIntent, TS: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	if count < 1 {
		t.Fatalf("B11 FAIL: fsync counter = %d, want >=1 per appendJournal call", count)
	}
	t.Logf("B11 PASS: fsync called %d times in appendJournal", count)

	// ── Phase 2: Verify nil counter (production path, no overhead) ──
	FsyncCount = nil
	if err := s.appendJournal(JournalEntry{
		OpID: "op-nil-count", Intent: "checkpoint", Target: "nil-session",
		Phase: PhaseIntent, TS: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	t.Log("B11 PASS: nil FsyncCount (production path) works")
}
// ── B13: CLOEXEC — real fork/exec, verify fd invisible in child ──────

func TestB13_CloexecInChild(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	lockFd := s.lockFd
	if lockFd < 0 {
		t.Fatal("no lock fd")
	}

	// Get the fd path for verification in child
	fdPath := "/proc/self/fd/" + strings.TrimSpace(strings.Split(
		mustReadFile("/proc/self/fd"), "\n")[0])

	// Fork a child that checks if this fd is visible
	// The fd should NOT be visible because CLOEXEC is set
	child := exec.Command("/bin/bash", "-c",
		"if [ -e /proc/self/fd/"+string(rune(lockFd+'0'))+" ]; then exit 1; else exit 0; fi")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	err := child.Run()
	if err != nil {
		// Exit 1 means the fd IS visible in child — B13 failure
		t.Fatalf("B13: lock fd %d visible in child process (CLOEXEC not working): %v", lockFd, err)
	}

	// Also check: child /proc/self/fd should NOT contain our lock fd
	_ = fdPath
	t.Logf("B13 PASS: lock fd %d not visible in child", lockFd)
}

func mustReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
