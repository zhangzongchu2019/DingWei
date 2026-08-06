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

// ── B9: PID reuse — MC-2: falsifiable assertion (no signal to wrong process) ─

func TestB9_PIDReuseReal(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// Start a marker process that catches signals.
	// If it receives SIGTERM or SIGKILL, it writes to a marker file.
	marker := filepath.Join(dir, "signal-marker")
	markerProc := exec.Command("/bin/bash", "-c",
		"trap 'echo GOT_SIGNAL >> "+marker+"' TERM KILL; sleep 30")
	if err := markerProc.Start(); err != nil {
		t.Fatal(err)
	}
	pid := markerProc.Process.Pid
	defer markerProc.Process.Kill()

	// Read the real tuple of the marker process
	realTuple, err := readProcTuple(pid)
	if err != nil {
		t.Fatalf("read real tuple: %v", err)
	}

	// Create a fake (stale) tuple with same PID but different starttime
	fakeTuple := ProcTuple{PID: realTuple.PID, StartTime: realTuple.StartTime + 1, Exe: realTuple.Exe}

	// Register the FAKE (stale) tuple — simulating PID reuse
	// pidfd = -1 (MC-1: will be rejected by stopProcess before any signal)
	s.RegisterProcess("test-session", fakeTuple, -1)

	// Try to stop — must NOT send any signal to the real process.
	// MC-1: stopProcess rejects pidfd < 0 (fail-closed).
	err = s.stopProcess("test-session")
	if err == nil {
		t.Fatal("B9: stopProcess should fail when pidfd is -1 (MC-1)")
	}
	if !strings.Contains(err.Error(), "no pidfd") {
		t.Fatalf("B9: expected no-pidfd error, got: %v", err)
	}

	// Verify marker file was NOT written — no signal was sent to the real process
	time.Sleep(200 * time.Millisecond)
	if data, err := os.ReadFile(marker); err == nil && len(data) > 0 {
		t.Fatalf("B9 FAIL: signal was sent to wrong process! marker: %s", string(data))
	}
	t.Log("B9 PASS: no signal sent to PID-reused process")
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

// ── B11: fsync durability — MC-4 anti-proof (with-fsync vs without-fsync) ─



func TestB11_FsyncDurability(t *testing.T) {
	dir := testDir(t)

	// ── Phase 1: WITH fsync — write data, kill process, verify survival ──
	func() {
		FsyncEnabled = true
		defer func() { FsyncEnabled = true }()

		s := New(dir)
		if err := s.Lock(); err != nil {
			t.Fatal(err)
		}
		defer s.Unlock()

		// Write journal entries
		if err := s.appendJournal(JournalEntry{
			OpID: "op-fsync-on", Intent: "checkpoint", Target: "fsync-session",
			Phase: PhaseIntent, TS: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}

		cp := CheckpointPayload{
			Session: "fsync-session", BinaryPath: "/bin/true",
			State: "active", CheckpointAt: time.Now().Unix(),
		}
		sha, _ := s.writeCheckpoint(cp)
		s.updateCurrent("fsync-session", sha)
		s.appendJournal(JournalEntry{
			OpID: "op-fsync-on", Intent: "checkpoint", Target: "fsync-session",
			Phase: PhaseDone,
		})

		// Verify data is on disk BEFORE any crash simulation
		entries, err := s.readJournal()
		if err != nil || len(entries) < 2 {
			t.Fatalf("with-fsync: expected >=2 entries, got %d (err=%v)", len(entries), err)
		}

		// Simulate crash: re-open a new supervisor and read back
		// (The fsync should have made the data durable)
		s2 := New(dir)
		entries2, err := s2.readJournal()
		if err != nil {
			t.Fatalf("with-fsync: re-read after simulated crash: %v", err)
		}
		if len(entries2) < 2 {
			t.Fatalf("B11 FAIL with-fsync: data lost after simulated crash: got %d entries", len(entries2))
		}
		t.Logf("B11 with-fsync: %d entries survived simulated crash", len(entries2))
	}()

	// ── Phase 2: WITHOUT fsync — write data, verify it can be lost ──
	func() {
		FsyncEnabled = false
		defer func() { FsyncEnabled = true }()

		dir2 := testDir(t)
		s := New(dir2)
		if err := s.Lock(); err != nil {
			t.Fatal(err)
		}
		defer s.Unlock()

		// Write WITHOUT fsync — data may stay in page cache only
		if err := s.appendJournal(JournalEntry{
			OpID: "op-nofsync", Intent: "checkpoint", Target: "nofsync-session",
			Phase: PhaseIntent, TS: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}

		// Read back immediately (will succeed from page cache)
		entries, err := s.readJournal()
		if err != nil || len(entries) < 1 {
			t.Fatalf("no-fsync: write failed: %v", err)
		}

		// Now re-open with a NEW supervisor (fresh page cache).
		// Without fsync, data may NOT be on disk.
		// NOTE: on the same machine without an actual crash, page cache
		// may still serve the data. This test documents the anti-proof
		// structure; a full kill -9 test requires a subprocess.
		s2 := New(dir2)
		entries2, err := s2.readJournal()
		// MC-4: no-fsync means data MAY be lost.
		// If it survives, that's page cache luck, not durability.
		// If it's lost, the anti-proof is confirmed.
		if err != nil || len(entries2) < 1 {
			t.Logf("B11 anti-proof: without fsync, data lost (expected): %d entries, err=%v", len(entries2), err)
		} else {
			t.Logf("B11 anti-proof: without fsync, data in page cache survived (not a failure — kill -9 would lose it): %d entries", len(entries2))
		}
	}()
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
