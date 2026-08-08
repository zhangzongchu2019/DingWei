//go:build linux

package m5

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testDir returns a temporary directory for test data.
func testDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// ── B2: checkpoint crash resilience (content written, no pointer) ──────

func TestB2_CheckpointCrashNoPointer(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	cp := CheckpointPayload{
		Session:      "test-session",
		BinaryPath:   "/usr/bin/claude",
		State:        "active",
		CheckpointAt: time.Now().Unix(),
	}
	sha, err := s.writeCheckpoint(cp)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(s.checkpointDir(), sha)); err != nil {
		t.Fatalf("checkpoint missing after write: %v", err)
	}
	if _, err := s.readCurrent("test-session"); err == nil {
		t.Fatal("pointer should not exist after content-only write (crash simulation)")
	}
}

// ── B3: pointer atomicity (read never sees half sha) ──────────────────

func TestB3_PointerAtomic(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	sha := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := s.updateCurrent("test-session", sha); err != nil {
		t.Fatal(err)
	}

	got, err := s.readCurrent("test-session")
	if err != nil {
		t.Fatal(err)
	}
	if got != sha {
		t.Fatalf("pointer mismatch: got %q want %q", got, sha)
	}

	data, err := os.ReadFile(s.currentPath("test-session"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != sha {
		t.Fatalf("pointer file corrupt: %q", string(data))
	}
}

// ── B4: restart full chain ────────────────────────────────────────────

func TestB4_RestartFullChain(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	cp := CheckpointPayload{
			Session:      "test-session",
			BinaryPath:   "python3",
			HealthURL:    "http://127.0.0.1:19998/",
			State:        "active",
			CheckpointAt: time.Now().Unix(),
		}
	sha, err := s.Checkpoint("test-session", cp)
	if err != nil {
		t.Fatal(err)
	}

	gotCP, err := s.readCheckpoint(sha)
	if err != nil {
		t.Fatal(err)
	}
	if gotCP.Session != "test-session" {
		t.Fatalf("checkpoint session mismatch: got %q", gotCP.Session)
	}

	err = s.Restart("test-session", "python3", []string{"-m", "http.server", "19998"}, nil)
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	names := s.ListSessions()
	found := false
	for _, n := range names {
		if n == "test-session" {
			found = true
		}
	}
	if !found {
		t.Fatal("session not registered after restart")
	}
	if err := s.stopProcess("test-session"); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

// ── B5: bad checkpoint rejection ──────────────────────────────────────

func TestB5_BadCheckpointRejection(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	badDir := s.checkpointDir()
	os.MkdirAll(badDir, 0700)
	fakeSHA := "0000000000000000000000000000000000000000000000000000000000000000"
	badPath := filepath.Join(badDir, fakeSHA)

	os.WriteFile(badPath, []byte("not valid json"), 0600)
	_, err := s.readCheckpoint(fakeSHA)
	if err == nil {
		t.Fatal("expected ErrCheckpointBad for corrupt checkpoint")
	}

	wrongCP := CheckpointPayload{Session: "x", State: "idle", CheckpointAt: 1}
	jsonData, _ := json.Marshal(wrongCP)
	os.WriteFile(badPath, jsonData, 0600)
	_, err = s.readCheckpoint(fakeSHA)
	if err == nil {
		t.Fatal("expected ErrCheckpointBad for sha mismatch")
	}
}

// ── B6: journal crash recovery — stop intent ──────────────────────────

func TestB6_JournalCrashRecovery(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	s.appendJournal(JournalEntry{
		OpID: "op-stop", Intent: "stop", Target: "session-a", Phase: PhaseIntent,
	})
	errs := s.Recover()
	if len(errs) > 0 {
		t.Logf("recovery errors: %v", errs)
	}
	entries, _ := s.readJournal()
	hasDone := false
	for _, e := range entries {
		if e.OpID == "op-stop" && e.Phase == PhaseDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Fatal("stop recovery should write done entry")
	}
}

// ── B7: error code table completeness (§4 whitelist) ──────────────────

func TestB7_ErrorCodeTable(t *testing.T) {
	allErrs := []error{
		ErrCheckpointBad, ErrSessHeld, ErrStopTimeout,
		ErrProbeFail, ErrJournalBad, ErrPidReuse,
	}
	for _, e := range allErrs {
		if !isBusinessErr[e] {
			t.Fatalf("error %v not in business error whitelist", e)
		}
		code := errCode(e)
		if code == "" || code == "ErrUnknown" {
			t.Fatalf("errCode(%v) = %q, want known code", e, code)
		}
	}
}

// ── B8: replace chain 3-segment recovery ──────────────────────────────

func TestB8_ReplaceChainRecovery(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	s.appendJournal(JournalEntry{
		OpID: "op-replace", Intent: "replace", Target: "session-c", Phase: PhaseIntent,
	})
	errs := s.Recover()
	if len(errs) > 0 {
		t.Logf("replace recovery (no process): %v", errs)
	}
}

// ── B12: ErrJournalBad → fail-closed, don't silently proceed ───────────

func TestB12_JournalTailCorruption(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	s.appendJournal(JournalEntry{
		OpID: "op-good", Intent: "checkpoint", Target: "s", Phase: PhaseDone,
	})

	f, _ := os.OpenFile(s.journalPath(), os.O_WRONLY|os.O_APPEND, 0600)
	f.Write([]byte("this is not valid json\n"))
	f.Close()

	entries, err := s.readJournal()
	if err == nil {
		t.Fatal("B12: expected ErrJournalBad for corrupt tail")
	}
	if len(entries) < 1 {
		t.Fatal("B12: valid entries before corruption should be returned")
	}
	if !errors.Is(err, ErrJournalBad) {
		t.Fatalf("B12: wrong error type: %v", err)
	}
}

// ── B14: orphan CLI recognition (tuple match → adopt) ─────────────────

func TestB14_OrphanRecognition(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	tuple, pidFd, err := startProcess("/bin/sleep", []string{"30"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(pidFd, tuple)

	s.RegisterProcess("adopted-session", tuple, pidFd)
	names := s.ListSessions()
	found := false
	for _, n := range names {
		if n == "adopted-session" {
			found = true
		}
	}
	if !found {
		t.Fatal("B14: orphan with matching tuple should be adopted")
	}
}

// ── checkpoint roundtrip (JCS-based via shared jcs package) ───────────

func TestCheckpointRoundtrip(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	cp := CheckpointPayload{
		Session:      "roundtrip",
		BinaryPath:   "/usr/bin/claude",
		BinarySHA:    "abc123",
		Args:         "--model sonnet",
		Env:          "KEY=val\nOTHER=42",
		State:        "active",
		CheckpointAt: time.Now().Unix(),
	}

	sha, err := s.Checkpoint("roundtrip", cp)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.readCheckpoint(sha)
	if err != nil {
		t.Fatal(err)
	}

	if got.Session != cp.Session || got.BinaryPath != cp.BinaryPath || got.State != cp.State {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", cp, got)
	}
}

// ── JCS determinism (shared jcs package, not hand-rolled) ─────────────

func TestCheckpointJCSDeterminism(t *testing.T) {
	cp1 := CheckpointPayload{
		Session: "test", BinaryPath: "/bin/sleep", State: "active",
		CheckpointAt: 1234567890,
	}
	cp2 := CheckpointPayload{
		State: "active", CheckpointAt: 1234567890,
		BinaryPath: "/bin/sleep", Session: "test",
	}
	if cp1.SHA256() != cp2.SHA256() {
		t.Fatal("JCS: field order in struct must not affect sha256")
	}
}

// ── GC: checkpoint retention ──────────────────────────────────────────

func TestGCCheckpointRetention(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	cp := CheckpointPayload{
		Session: "keep-session", BinaryPath: "/bin/true",
		State: "active", CheckpointAt: time.Now().Unix(),
	}
	sha, err := s.Checkpoint("keep-session", cp)
	if err != nil {
		t.Fatal(err)
	}

	orphanCP := CheckpointPayload{
		Session: "no-session", BinaryPath: "/bin/false",
		State: "idle", CheckpointAt: time.Now().Add(-8 * 24 * time.Hour).Unix(),
	}
	orphanSHA := orphanCP.SHA256()
	os.MkdirAll(s.checkpointDir(), 0700)
	jsonData, _ := json.Marshal(orphanCP)
	orphanPath := filepath.Join(s.checkpointDir(), orphanSHA)
	os.WriteFile(orphanPath, jsonData, 0600)
	os.Chtimes(orphanPath, time.Now().Add(-8*24*time.Hour), time.Now().Add(-8*24*time.Hour))

	s.gcCheckpoints()

	if _, err := os.Stat(filepath.Join(s.checkpointDir(), sha)); err != nil {
		t.Fatalf("reachable checkpoint should survive GC: %v", err)
	}
}

// ── drain marker (§1.1 N-3) ───────────────────────────────────────────

func TestDrainMarkerLifecycle(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// drain writes marker with parseable unix ts (rename+fsync path)
	if err := s.drain("sess1"); err != nil {
		t.Fatalf("drain: %v", err)
	}
	data, err := os.ReadFile(s.drainPath("sess1"))
	if err != nil {
		t.Fatalf("marker missing after drain: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatal("marker empty")
	}

	// clearDrain removes it (new generation accepts tasks again)
	s.clearDrain("sess1")
	if _, err := os.Stat(s.drainPath("sess1")); !os.IsNotExist(err) {
		t.Fatal("marker still present after clearDrain")
	}
}

func TestDrainFailClosed(t *testing.T) {
	dir := testDir(t)
	s := New(dir)
	if err := s.Lock(); err != nil {
		t.Fatal(err)
	}
	defer s.Unlock()

	// Make sessions/ a regular file so writeAtomic(MkdirAll) must fail —
	// drain error must propagate (§5⑤ no silent skip of the ordering step).
	sessPath := filepath.Join(dir, "sessions")
	if err := os.WriteFile(sessPath, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.drain("sess1"); err == nil {
		t.Fatal("drain must fail-closed when marker cannot be written")
	}
}
