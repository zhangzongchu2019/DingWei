//go:build linux

package m5

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ── flock syscall wrappers (§1: flock 防第二 supervisor) ───────────────

const (
	unixLOCK_SH = 1
	unixLOCK_EX = 2
	unixLOCK_NB = 4
	unixLOCK_UN = 8
)

func unixOpen(path string, flags int, perm os.FileMode) (int, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC, uint32(perm))
	return fd, err
}

func unixClose(fd int) {
	unix.Close(fd)
}

func unixFlock(fd int, how int) error {
	for {
		err := unix.Flock(fd, how)
		if err != syscall.EINTR {
			return err
		}
	}
}

var (
	unixSIGTERM = unix.SIGTERM
	unixSIGKILL = unix.SIGKILL
)

// ── pidfd (§1: Linux 5.3+ pidfd_open) ─────────────────────────────────

func unixPidfdOpen(pid int) (int, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return -1, fmt.Errorf("pidfd_open(%d): %w", pid, err)
	}
	return fd, nil
}

func unixPidfdSendSignal(pidFd int, sig syscall.Signal) error {
	_, _, errno := unix.Syscall(unix.SYS_PIDFD_SEND_SIGNAL, uintptr(pidFd), uintptr(sig), 0)
	if errno != 0 {
		return fmt.Errorf("pidfd_send_signal(%d, %d): %v", pidFd, sig, errno)
	}
	return nil
}

// unixKill sends a signal via pidfd. Returns error if pidfd is invalid
// or the signal cannot be sent. Callers MUST NOT treat this as "process gone"
// — only tuple disappearance proves process exit (§1.1).
func unixKill(pidFd int, sig syscall.Signal) error {
	if pidFd < 0 {
		return fmt.Errorf("unixKill: pidfd=%d (MC-1: no pidfd; fail-closed)", pidFd)
	}
	return unixPidfdSendSignal(pidFd, sig)
}

// ── process tuple (§1: pid + starttime + exe + cgroup) ────────────────

func readProcTuple(pid int) (ProcTuple, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return ProcTuple{}, fmt.Errorf("read %s: %w", statPath, err)
	}
	// /proc/pid/stat: pid (name) state ... field22=starttime
	// Find the closing paren to locate field 22 (handles comm with spaces/parens)
	s := string(data)
	closeParen := strings.LastIndex(s, ")")
	if closeParen < 0 {
		return ProcTuple{}, fmt.Errorf("bad stat format: %s", statPath)
	}
	fields := strings.Fields(s[closeParen+2:])
	// fields[0]=state, fields[19]=starttime (field 22 = index 19 after state)
	if len(fields) < 20 {
		return ProcTuple{}, fmt.Errorf("stat too short: %d fields", len(fields))
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcTuple{}, fmt.Errorf("parse starttime: %w", err)
	}

	// exe
	exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))

	// cgroup (first line)
	cgroup := ""
	cgData, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err == nil {
		lines := strings.SplitN(string(cgData), "\n", 2)
		if len(lines) > 0 {
			cgroup = strings.TrimSpace(lines[0])
		}
	}

	return ProcTuple{
		PID:       int32(pid),
		StartTime: startTime,
		Exe:       exe,
		Cgroup:    cgroup,
	}, nil
}

// ── process lifecycle ──────────────────────────────────────────────────

// startProcess starts a new process and returns its identity tuple + pidfd.
// MC-1: pidfd acquisition failure is fail-closed (error, not -1/nil).
// N-4: tuple read failure is fail-closed (error, not degraded tuple).
func startProcess(binary string, args []string, env []string) (ProcTuple, int, error) {
	// Find executable (n-1: use os/exec.LookPath)
	binPath := binary
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		lp, err := exec.LookPath(binary)
		if err != nil {
			return ProcTuple{}, -1, fmt.Errorf("binary not found: %s", binary)
		}
		binPath = lp
	}

	argv := append([]string{binPath}, args...)
	envp := append(os.Environ(), env...)

	pid, pidFd, err := forkExec(binPath, argv, envp)
	if err != nil {
		return ProcTuple{}, -1, err
	}

	// Read tuple after start to verify identity
	time.Sleep(50 * time.Millisecond)
	tuple, err := readProcTuple(pid)
	if err != nil {
		// N-4: fail-closed — can't verify identity, must not proceed
		// Recycle the started process
		syscall.Kill(pid, syscall.SIGKILL)
		if pidFd >= 0 {
			unixClose(pidFd)
		}
		return ProcTuple{}, -1, fmt.Errorf("startProcess: cannot read tuple for pid %d: %w", pid, err)
	}

	// N-1: verify tuple matches within pidfd_open window (PID reuse guard)
	// If pid was reused between StartProcess and pidfd_open, tuple will differ.
	if pidFd >= 0 {
		// pidfd_open succeeded — verify it points to the same process
		if err := unixPidfdSendSignal(pidFd, 0); err != nil {
			// pidfd doesn't point to a live process → PID was reused
			syscall.Kill(pid, syscall.SIGKILL)
			unixClose(pidFd)
			return ProcTuple{}, -1, fmt.Errorf("startProcess: pidfd verification failed for pid %d: %w", pid, err)
		}
	}

	return tuple, pidFd, nil
}

// forkExec starts a process and opens a pidfd for it.
// N-1: post-pidfd_open tuple verification is done by caller (startProcess).
// MC-1: pidfd_open failure is FAIL-CLOSED — returns error, not (-1, nil).
func forkExec(binary string, argv []string, envp []string) (int, int, error) {
	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   envp,
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	proc, err := os.StartProcess(binary, argv, attr)
	if err != nil {
		return -1, -1, err
	}

	// MC-1: pidfd_open failure is fail-closed.
	// A missing pidfd means we cannot safely send signals per §1.1 —
	// we must not proceed with a process we cannot control.
	pidFd, err := unixPidfdOpen(proc.Pid)
	if err != nil {
		// Kill the process we just started — we cannot manage it without pidfd
		proc.Kill()
		proc.Wait()
		return -1, -1, fmt.Errorf("forkExec: pidfd_open(%d) failed (MC-1 fail-closed): %w", proc.Pid, err)
	}
	return proc.Pid, pidFd, nil
}

// ── probe (§N4: tuple existence + health endpoint 200, 3 probes 2s interval) ─

// probeProcess probes a started process: tuple identity (§1.2) + health endpoint (§N4).
// healthURL is the session's health check URL (e.g. http://127.0.0.1:PORT/health).
// N-2b: healthURL is mandatory (zzc裁定A) — if empty, probe passes on tuple alone.
func probeProcess(tuple ProcTuple, healthURL string, attempts int, interval time.Duration) error {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		// Tuple existence check (§1.2)
		current, err := readProcTuple(int(tuple.PID))
		if err != nil {
			continue // process gone, retry
		}
		if !current.Equal(tuple) {
			return fmt.Errorf("probe: pid %d tuple changed", tuple.PID)
		}
		// Health endpoint check (§N4, N-2b: enabled by default)
		if healthURL != "" {
			if !checkHealth200(healthURL) {
				continue // health not ready, retry
			}
		}
		return nil
	}
	return ErrProbeFail
}

// checkHealth200 performs an HTTP GET to url and returns true on 200 OK.
// Uses a short timeout (2s) and treats any non-200 or connection failure as unhealthy.
func checkHealth200(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func killProcess(pidFd int, tuple ProcTuple) {
	// Verify identity before killing
	current, err := readProcTuple(int(tuple.PID))
	if err != nil {
		return // already dead (tuple disappearance proves exit, §1.1)
	}
	if !current.Equal(tuple) {
		return // not our process anymore (§1.2)
	}
	unixKill(pidFd, unixSIGKILL)
}
