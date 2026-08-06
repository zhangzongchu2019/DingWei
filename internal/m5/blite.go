// Package m5 implements the B-lite supervisor: per-machine single-instance
// process manager for CLI sessions, with pidfd-based process identity,
// content-addressed checkpoints, and an append-only operation journal.
//
// Contract: docs/m5/blite-contract-v1.md v1.5 (86dd8098).
//
// Build: go build -tags linux (pidfd requires Linux 5.3+, Go 1.20+).
package m5

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/jcs"
)

// ── configuration ──────────────────────────────────────────────────────

// Dir returns the supervisor data directory (DATAD_M5_DIR or default).
func Dir() string {
	if d := os.Getenv("DATAD_M5_DIR"); d != "" {
		return d
	}
	return "/data/m5-supervisor"
}

// ── error codes (§4: 全量枚举, 每错恰一机器码) ───────────────────────

var (
	ErrCheckpointBad = errors.New("m5: checkpoint corrupt or missing")        // sha mismatch / file missing
	ErrSessHeld      = errors.New("m5: flock held or lock fd leaked")          // second supervisor or fd leak
	ErrStopTimeout   = errors.New("m5: stop timeout: process did not exit after SIGKILL")
	ErrProbeFail     = errors.New("m5: new process probe failed after restart")
	ErrJournalBad    = errors.New("m5: journal line corrupt, fail-closed")
	ErrPidReuse      = errors.New("m5: pid reuse detected, refusing signal")
)

// isBusinessErr is the §4 whitelist — every business error must appear here.
var isBusinessErr = map[error]bool{
	ErrCheckpointBad: true,
	ErrSessHeld:      true,
	ErrStopTimeout:   true,
	ErrProbeFail:     true,
	ErrJournalBad:    true,
	ErrPidReuse:      true,
}

// ── process identity (§1: pidfd + tuple, not bare pid) ─────────────────

// ProcTuple is the immutable process identity tuple.
// pid alone is never sufficient for identity decisions (§1.1).
type ProcTuple struct {
	PID       int32  `json:"pid"`
	StartTime uint64 `json:"starttime"`        // /proc/pid/stat field 22, clock ticks since boot
	Exe       string `json:"exe"`              // readlink /proc/pid/exe
	Cgroup    string `json:"cgroup,omitempty"` // /proc/pid/cgroup (first line, optional on non-Linux)
}

// Equal reports whether two tuples match.
func (t ProcTuple) Equal(o ProcTuple) bool {
	return t.PID == o.PID && t.StartTime == o.StartTime && t.Exe == o.Exe && t.Cgroup == o.Cgroup
}

// IsZero reports whether the tuple is the zero value (no process).
func (t ProcTuple) IsZero() bool {
	return t == (ProcTuple{})
}

// ── journal types (§3: append-only JSON lines) ────────────────────────

// JournalPhase is the stage of a journaled operation.
type JournalPhase string

const (
	PhaseIntent    JournalPhase = "intent"
	PhaseDone      JournalPhase = "done"
	PhaseFailed    JournalPhase = "failed"
	PhaseReleased  JournalPhase = "released"
)

// JournalEntry is one line in the operation journal.
type JournalEntry struct {
	OpID    string       `json:"op_id"`
	Intent  string       `json:"intent"`  // checkpoint / restart / replace / stop
	Target  string       `json:"target"`  // session name
	Phase   JournalPhase `json:"phase"`
	TS      int64        `json:"ts"`      // unix seconds
	ErrCode string       `json:"err,omitempty"`
}

// ── checkpoint payload (§2: JCS RFC8785, content-addressed) ──────────

// CheckpointPayload is the canonical state of one session.
// Serialized with JCS (RFC 8785) — Go's encoding/json produces
// RFC 8785-compliant output when we use struct tags (sorted by field order)
// and never use map[string]any for the canonical payload.
type CheckpointPayload struct {
	Session     string `json:"session"`
	BinaryPath  string `json:"binary_path"`
	BinarySHA   string `json:"binary_sha,omitempty"`
	Args        string `json:"args,omitempty"`
	Env         string `json:"env,omitempty"` // key=value\n lines, sorted
	HealthURL   string `json:"health_url,omitempty"` // §N4: health endpoint for probe (e.g. http://127.0.0.1:PORT/health)
	State       string `json:"state"`         // active / idle / restarting
	CheckpointAt int64 `json:"checkpoint_at"` // unix seconds
}

// SHA256 returns the content-addressed id of the payload (JCS RFC 8785 via shared internal/jcs).
func (p CheckpointPayload) SHA256() string {
	b, err := jcs.Canonicalize(p)
	if err != nil {
		panic(fmt.Sprintf("m5: JCS canonicalize: %v", err))
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}

// Validate checks that the payload's sha matches the expected id.
func (p CheckpointPayload) Validate(id string) bool {
	return p.SHA256() == id
}

// ── file-system paths (§2) ────────────────────────────────────────────

func (s *Supervisor) lockPath() string      { return filepath.Join(s.dir, "supervisor.lock") }
func (s *Supervisor) journalPath() string   { return filepath.Join(s.dir, "journal.log") }
func (s *Supervisor) checkpointDir() string { return filepath.Join(s.dir, "checkpoints") }
func (s *Supervisor) currentPath(session string) string {
	return filepath.Join(s.dir, "sessions", session+".current")
}

// ── atomic file helpers ────────────────────────────────────────────────

// writeAtomic writes data to a temp file, fsyncs it, then renames atomically.
func (s *Supervisor) writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	// fsync parent directory so the rename is durable
	return syncDir(dir)
}

// FsyncCount is called (if non-nil) each time appendJournal calls f.Sync().
// MC-4(a): tests set this to count fsync calls, making the anti-proof deterministic.
// Production: nil (no overhead).
var FsyncCount func()

// readFile reads a file's full content.
func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// syncDir opens and fsyncs a directory to make renames durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ── Supervisor ─────────────────────────────────────────────────────────

// Supervisor is the per-machine singleton process manager.
type Supervisor struct {
	dir string
	mu  sync.Mutex // single event loop (§3: sequential, no concurrency)

	lockFd   int              // flock fd, -1 if not acquired
	sessions map[string]*sessionState // session name → state
}

type sessionState struct {
	Name       string
	BinaryPath string
	Tuple      ProcTuple // current process identity
	PidFd      int       // pidfd, -1 if not open
}

// New creates a Supervisor for the given data directory.
func New(dir string) *Supervisor {
	if dir == "" {
		dir = Dir()
	}
	return &Supervisor{
		dir:      dir,
		lockFd:   -1,
		sessions: map[string]*sessionState{},
	}
}

// ── lock/unlock (§1: flock 防第二 supervisor) ────────────────────────

// Lock acquires the supervisor flock. Returns ErrSessHeld if another
// supervisor is already running.
func (s *Supervisor) Lock() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	fd, err := unixOpen(s.lockPath(), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err := unixFlock(fd, unixLOCK_EX|unixLOCK_NB); err != nil {
		unixClose(fd)
		return fmt.Errorf("%w: flock %s: %v", ErrSessHeld, s.lockPath(), err)
	}
	s.lockFd = fd
	return nil
}

// Unlock releases the supervisor lock.
func (s *Supervisor) Unlock() {
	if s.lockFd >= 0 {
		unixFlock(s.lockFd, unixLOCK_UN)
		unixClose(s.lockFd)
		s.lockFd = -1
	}
}

// ── journal (§3: append-only, fsync after each line) ─────────────────

// appendJournal writes one journal entry with fsync.
func (s *Supervisor) appendJournal(e JournalEntry) error {
	if e.TS == 0 {
		e.TS = time.Now().Unix()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(s.journalPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	// short flock for write atomicity (§2: very short critical section, not held)
	if err := unixFlock(int(f.Fd()), unixLOCK_EX); err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		unixFlock(int(f.Fd()), unixLOCK_UN)
		return err
	}
	if err := f.Sync(); err != nil {
		unixFlock(int(f.Fd()), unixLOCK_UN)
		return err
	}
	if FsyncCount != nil {
		FsyncCount()
	}
	unixFlock(int(f.Fd()), unixLOCK_UN)
	// fsync parent directory so journal append is durable
	return syncDir(s.dir)
}

// readJournal reads all journal entries in order.
func (s *Supervisor) readJournal() ([]JournalEntry, error) {
	data, err := readFile(s.journalPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entries []JournalEntry
	for i, line := range lines {
		if line == "" {
			continue
		}
		var e JournalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// §3b/§4: tail corruption → ErrJournalBad, fail-closed
			if i == len(lines)-1 {
				return entries, fmt.Errorf("%w: line %d: %v", ErrJournalBad, i+1, err)
			}
			// MC-3: any corruption -> ErrJournalBad fail-closed (§3b/§4/§5⑤).
			// Disk corruption/fs error/human edit can corrupt any line;
			// losing an open intent from recovery is silent data loss.
			return entries, fmt.Errorf("%w: line %d: %v", ErrJournalBad, i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// lastIntent returns the last intent entry for a session that has no matching done/failed.
func lastOpenIntent(entries []JournalEntry, session string) *JournalEntry {
	for i := len(entries) - 1; i >= 0; i-- {
		e := &entries[i]
		if e.Target == session && e.Phase == PhaseIntent {
			// check if there's a done/failed after it
			closed := false
			for j := i + 1; j < len(entries); j++ {
				if entries[j].Target == session && entries[j].OpID == e.OpID &&
					(entries[j].Phase == PhaseDone || entries[j].Phase == PhaseFailed) {
					closed = true
					break
				}
			}
			if !closed {
				return e
			}
		}
	}
	return nil
}

// ── checkpoint (§2/§3: content-addressed, atomic pointer update) ──────

// writeCheckpoint serializes payload, writes to checkpoints/<sha>,
// and returns the sha256 id.
func (s *Supervisor) writeCheckpoint(p CheckpointPayload) (string, error) {
	b, err := jcs.Canonicalize(p)
	if err != nil {
		return "", err
	}
	sha := p.SHA256()
	path := filepath.Join(s.checkpointDir(), sha)
	// Checkpoint is immutable — write only if not exists
	if _, err := os.Stat(path); err == nil {
		return sha, nil // already exists
	}
	if err := os.MkdirAll(s.checkpointDir(), 0700); err != nil {
		return "", err
	}
	if err := s.writeAtomic(path, b); err != nil {
		return "", err
	}
	// GC: keep track for retention (best-effort)
	return sha, nil
}

// readCheckpoint reads and validates a checkpoint by sha256 id.
func (s *Supervisor) readCheckpoint(sha string) (CheckpointPayload, error) {
	path := filepath.Join(s.checkpointDir(), sha)
	data, err := readFile(path)
	if err != nil {
		return CheckpointPayload{}, fmt.Errorf("%w: %s: %v", ErrCheckpointBad, sha, err)
	}
	var p CheckpointPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return CheckpointPayload{}, fmt.Errorf("%w: %s: bad json: %v", ErrCheckpointBad, sha, err)
	}
	if !p.Validate(sha) {
		return CheckpointPayload{}, fmt.Errorf("%w: %s: sha mismatch", ErrCheckpointBad, sha)
	}
	return p, nil
}

// updateCurrent atomically updates sessions/<name>.current to point to sha.
func (s *Supervisor) updateCurrent(session, sha string) error {
	dir := filepath.Join(s.dir, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, session+".current")
	return s.writeAtomic(path, []byte(sha+"\n"))
}

// readCurrent reads the current checkpoint sha for a session.
func (s *Supervisor) readCurrent(session string) (string, error) {
	data, err := readFile(s.currentPath(session))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ── GC: checkpoint/journal retention (§2 N2/S5) ───────────────────────

// gcCheckpoints removes unreachable checkpoints older than 7 days.
func (s *Supervisor) gcCheckpoints() error {
	// Collect all reachable shas from current pointers
	reachable := map[string]bool{}
	sessDir := filepath.Join(s.dir, "sessions")
	entries, _ := os.ReadDir(sessDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".current") {
			continue
		}
		data, err := readFile(filepath.Join(sessDir, e.Name()))
		if err != nil {
			continue
		}
		sha := strings.TrimSpace(string(data))
		if sha != "" {
			reachable[sha] = true
		}
	}

	cpDir := s.checkpointDir()
	dirs, err := os.ReadDir(cpDir)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, d := range dirs {
		name := d.Name()
		if reachable[name] {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(cpDir, name))
		}
	}
	return nil
}

// ── operations (§3) ───────────────────────────────────────────────────

// Checkpoint captures a session's current state.
func (s *Supervisor) Checkpoint(session string, payload CheckpointPayload) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	opID := newOpID()
	e := JournalEntry{OpID: opID, Intent: "checkpoint", Target: session, Phase: PhaseIntent}
	if err := s.appendJournal(e); err != nil {
		return "", err
	}

	// Write checkpoint content first (§3 N1: content before pointer)
	sha, err := s.writeCheckpoint(payload)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "checkpoint", Target: session, Phase: PhaseFailed, ErrCode: "ErrCheckpointBad"})
		return "", err
	}

	// Atomic pointer update
	if err := s.updateCurrent(session, sha); err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "checkpoint", Target: session, Phase: PhaseFailed, ErrCode: "ErrSessHeld"})
		return "", err
	}

	// done AFTER rename (§3 N1: done记在rename之后)
	s.appendJournal(JournalEntry{OpID: opID, Intent: "checkpoint", Target: session, Phase: PhaseDone})
	return sha, nil
}

// Restart stops the old process and starts a new one from the latest checkpoint.
func (s *Supervisor) Restart(session string, newBinary string, args []string, env []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	opID := newOpID()
	e := JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseIntent}
	if err := s.appendJournal(e); err != nil {
		return err
	}

	// 1. Stop old process (§1.1: drain→TERM→bounded→KILL+verify)
	if err := s.stopProcess(session); err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseFailed, ErrCode: errCode(err)})
		return err
	}

	// 2. Load checkpoint
	sha, err := s.readCurrent(session)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseFailed, ErrCode: "ErrCheckpointBad"})
		return fmt.Errorf("%w: read current: %v", ErrCheckpointBad, err)
	}
	cp, err := s.readCheckpoint(sha)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseFailed, ErrCode: "ErrCheckpointBad"})
		return err
	}

	// 3. Start new process (§1: record pidfd/tuple)
	tuple, pidFd, err := startProcess(newBinary, args, env)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseFailed, ErrCode: "ErrProbeFail"})
		return fmt.Errorf("%w: start: %v", ErrProbeFail, err)
	}

	// 4. Probe health (§N4/N-2b: tuple + health endpoint 200, 3 probes 2s interval)
	if err := probeProcess(tuple, cp.HealthURL, 3, 2*time.Second); err != nil {
		// Try to kill the failed process
		killProcess(pidFd, tuple)
		s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseFailed, ErrCode: "ErrProbeFail"})
		return fmt.Errorf("%w: probe: %v", ErrProbeFail, err)
	}

	// 5. Record state
	ss := &sessionState{
		Name:       session,
		BinaryPath: newBinary,
		Tuple:      tuple,
		PidFd:      pidFd,
	}
	s.sessions[session] = ss

	s.appendJournal(JournalEntry{OpID: opID, Intent: "restart", Target: session, Phase: PhaseDone})
	_ = cp
	return nil
}

// Replace does checkpoint → stop → swap binary → restart as one atomic chain.
func (s *Supervisor) Replace(session string, newBinary string, args []string, env []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	opID := newOpID()
	e := JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseIntent}
	if err := s.appendJournal(e); err != nil {
		return err
	}

	// Phase 1: checkpoint
	ss := s.sessions[session]
	cp := CheckpointPayload{
		Session:      session,
		BinaryPath:   ss.BinaryPath,
		State:        "restarting",
		CheckpointAt: time.Now().Unix(),
	}
	sha, err := s.writeCheckpoint(cp)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseFailed, ErrCode: "ErrCheckpointBad"})
		return err
	}

	// Phase 2: stop
	if err := s.stopProcess(session); err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseFailed, ErrCode: errCode(err)})
		return err
	}

	// Phase 3: start new binary
	tuple, pidFd, err := startProcess(newBinary, args, env)
	if err != nil {
		s.appendJournal(JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseFailed, ErrCode: "ErrProbeFail"})
		return err
	}
	if err := probeProcess(tuple, cp.HealthURL, 3, 2*time.Second); err != nil {
		killProcess(pidFd, tuple)
		s.appendJournal(JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseFailed, ErrCode: "ErrProbeFail"})
		return err
	}

	newSS := &sessionState{
		Name:       session,
		BinaryPath: newBinary,
		Tuple:      tuple,
		PidFd:      pidFd,
	}
	s.sessions[session] = newSS

	// Update current pointer
	s.updateCurrent(session, sha)
	s.appendJournal(JournalEntry{OpID: opID, Intent: "replace", Target: session, Phase: PhaseDone})
	return nil
}

// ── stop process (§1.1: drain→SIGTERM→bounded→SIGKILL+verify) ────────

func (s *Supervisor) stopProcess(session string) error {
	ss, ok := s.sessions[session]
	if !ok || ss.Tuple.IsZero() {
		return nil // nothing to stop
	}

	tuple := ss.Tuple
	pidFd := ss.PidFd

	// MC-1: pidfd is mandatory for safe process control.
	// Without pidfd we cannot safely send signals â fail-closed.
	if pidFd < 0 {
		return fmt.Errorf("stopProcess: session=%s pid=%d: no pidfd (MC-1 fail-closed)", session, tuple.PID)
	}

	// Verify identity: tuple must match (§1.1)
	current, err := readProcTuple(int(tuple.PID))
	if err != nil {
		// Process already gone → stop done
		s.removeSession(session)
		return nil
	}
	if !current.Equal(tuple) {
		// PID reuse detected (§1.2 row 3)
		return fmt.Errorf("%w: session=%s pid=%d current=%+v registered=%+v",
			ErrPidReuse, session, tuple.PID, current, tuple)
	}

	// N-3: drain â stop accepting new tasks before SIGTERM (Â§1.1)
	s.drain(session)

	// SIGTERM
	if err := unixKill(pidFd, unixSIGTERM); err != nil {
		// MC-1: signal-send failure does NOT prove process exit.
		// Only tuple disappearance proves exit. Verify.
		if _, err2 := readProcTuple(int(tuple.PID)); err2 != nil {
			s.removeSession(session)
			return nil // tuple gone = process truly exited
		}
		return fmt.Errorf("stopProcess: session=%s pid=%d: SIGTERM failed and process still alive: %w", session, tuple.PID, err)
	}

	// Bounded wait (TTL 10s). N-5: compare tuple, not just /proc existence.
	const stopTTL = 10 * time.Second
	deadline := time.Now().Add(stopTTL)
	for time.Now().Before(deadline) {
		cur, err := readProcTuple(int(tuple.PID))
		if err != nil {
			s.removeSession(session)
			return nil // process exited
		}
		if !cur.Equal(tuple) {
			// PID was reused during wait â the original process is gone
			s.removeSession(session)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// SIGKILL
	if err := unixKill(pidFd, unixSIGKILL); err != nil {
		if _, err2 := readProcTuple(int(tuple.PID)); err2 != nil {
			s.removeSession(session)
			return nil // tuple gone
		}
		return fmt.Errorf("stopProcess: session=%s pid=%d: SIGKILL failed and process still alive: %w", session, tuple.PID, err)
	}

	// Verify exit after SIGKILL (§1.1: must verify)
	time.Sleep(500 * time.Millisecond)
	if _, err := readProcTuple(int(tuple.PID)); err != nil {
		s.removeSession(session)
		return nil
	}

	// Still running → ErrStopTimeout
	return fmt.Errorf("%w: session=%s pid=%d", ErrStopTimeout, session, tuple.PID)
}

func (s *Supervisor) removeSession(session string) {
	ss := s.sessions[session]
	if ss != nil && ss.PidFd >= 0 {
		unixClose(ss.PidFd)
	}
	delete(s.sessions, session)
}

// ── crash recovery (§3b: replay journal, probe reality) ───────────────

// Recover replays the journal after a crash and completes or restarts
// operations that were interrupted.
func (s *Supervisor) Recover() []error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readJournal()
	if err != nil {
		// ErrJournalBad: §4 fail-closed → report, don't auto-proceed
		return []error{err}
	}

	var errs []error

	// Find open intents (N-6: removed dead seen map; linear scan is correct for journal size)
	for _, e := range entries {
		if e.Phase != PhaseIntent {
			continue
		}
		// Check if there is a closing entry (done/failed) for this intent
		closed := false
		for _, e2 := range entries {
			if e2.Target == e.Target && e2.OpID == e.OpID &&
				(e2.Phase == PhaseDone || e2.Phase == PhaseFailed) {
				closed = true
				break
			}
		}
		if closed {
			continue
		}

		// This intent is open — recover based on type and reality
		switch e.Intent {
		case "checkpoint":
			if err := s.recoverCheckpoint(e); err != nil {
				errs = append(errs, err)
			}
		case "restart":
			if err := s.recoverRestart(e); err != nil {
				errs = append(errs, err)
			}
		case "replace":
			if err := s.recoverReplace(e); err != nil {
				errs = append(errs, err)
			}
		case "stop":
			if err := s.recoverStop(e); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func (s *Supervisor) recoverCheckpoint(e JournalEntry) error {
	// §3b row "checkpoint中": probe reality
	sha, err := s.readCurrent(e.Target)
	if os.IsNotExist(err) {
		// No pointer yet — check if checkpoint file exists
		// Best-effort: re-do checkpoint from scratch
		return fmt.Errorf("checkpoint %s: no current pointer, re-checkpoint needed", e.Target)
	}
	if err != nil {
		return err
	}
	// Pointer exists — verify sha
	cpDir := s.checkpointDir()
	if _, err := os.Stat(filepath.Join(cpDir, sha)); err != nil {
		// Pointer points to missing checkpoint → corrupt, re-do
		return fmt.Errorf("%w: checkpoint %s: .current points to missing %s", ErrCheckpointBad, e.Target, sha)
	}
	// All good — complete the journal
	s.appendJournal(JournalEntry{OpID: e.OpID, Intent: "checkpoint", Target: e.Target, Phase: PhaseDone})
	return nil
}

func (s *Supervisor) recoverStop(e JournalEntry) error {
	// §3b row "stop中": tuple-verify
	ss, ok := s.sessions[e.Target]
	if !ok || ss.Tuple.IsZero() {
		// No process registered → stop done
		s.appendJournal(JournalEntry{OpID: e.OpID, Intent: "stop", Target: e.Target, Phase: PhaseDone})
		return nil
	}
	// Verify tuple, re-execute stop if needed
	if err := s.stopProcess(e.Target); err != nil {
		if errors.Is(err, ErrPidReuse) {
			return err // don't auto-handle
		}
		// Stop timeout or other — re-send kill
		killProcess(ss.PidFd, ss.Tuple)
		s.removeSession(e.Target)
	}
	s.appendJournal(JournalEntry{OpID: e.OpID, Intent: "stop", Target: e.Target, Phase: PhaseDone})
	return nil
}

func (s *Supervisor) recoverRestart(e JournalEntry) error {
	// §3b rows "stop中"/"start中": probe reality
	ss, ok := s.sessions[e.Target]
	if ok && !ss.Tuple.IsZero() {
		// Might be running — probe
		if err := probeProcess(ss.Tuple, "", 1, 1*time.Second); err == nil {
			// Process alive, restart done
			s.appendJournal(JournalEntry{OpID: e.OpID, Intent: "restart", Target: e.Target, Phase: PhaseDone})
			return nil
		}
		// Dead — re-restart from checkpoint
	}
	// Re-execute full restart if we have a current pointer
	return fmt.Errorf("restart %s: incomplete, re-restart required", e.Target)
}

func (s *Supervisor) recoverReplace(e JournalEntry) error {
	// §3b: replace is checkpoint→stop→start chain, recover each segment
	return s.recoverRestart(e) // same as restart for the start segment
}

// ── GC public entry ───────────────────────────────────────────────────

// GC runs checkpoint and journal garbage collection.
func (s *Supervisor) GC() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gcCheckpoints()
}

// ── helpers ────────────────────────────────────────────────────────────

func newOpID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func errCode(err error) string {
	if err == nil {
		return ""
	}
	for e := range isBusinessErr {
		if errors.Is(err, e) {
			return strings.TrimPrefix(e.Error(), "m5: ")
		}
	}
	return "ErrUnknown"
}

// RegisterProcess records a pre-existing process under the supervisor's management
// (for orphan recognition, §1.2 row 2).
func (s *Supervisor) RegisterProcess(session string, tuple ProcTuple, pidFd int) {
	s.sessions[session] = &sessionState{
		Name:  session,
		Tuple: tuple,
		PidFd: pidFd,
	}
}

// ListSessions returns all registered session names.
func (s *Supervisor) ListSessions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.sessions))
	for n := range s.sessions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
