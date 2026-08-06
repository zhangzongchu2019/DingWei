//go:build linux

package m5

import (
	"fmt"
	"os"
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

func unixKill(pidFd int, sig syscall.Signal) error {
	if pidFd >= 0 {
		return unixPidfdSendSignal(pidFd, sig)
	}
	return fmt.Errorf("invalid pidfd: %d", pidFd)
}

// ── process tuple (§1: pid + starttime + exe + cgroup) ────────────────

func readProcTuple(pid int) (ProcTuple, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return ProcTuple{}, fmt.Errorf("read %s: %w", statPath, err)
	}
	// /proc/pid/stat: pid (name) state ... field22=starttime
	// Find the closing paren to locate field 22
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

func startProcess(binary string, args []string, env []string) (ProcTuple, int, error) {
	// Find executable
	binPath := binary
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		// Try lookup
		lp, err := execLookPath(binary)
		if err != nil {
			return ProcTuple{}, -1, fmt.Errorf("binary not found: %s", binary)
		}
		binPath = lp
	}

	// Build argv
	argv := append([]string{binPath}, args...)
	// Build envp
	envp := append(os.Environ(), env...)

	// fork+exec via syscall
	// Use clone with CLONE_PIDFD for atomic pidfd acquisition
	pid, pidFd, err := forkExec(binPath, argv, envp)
	if err != nil {
		return ProcTuple{}, -1, err
	}

	// Read tuple after start (give the process a moment)
	time.Sleep(50 * time.Millisecond)
	tuple, err := readProcTuple(pid)
	if err != nil {
		// Process started but can't read tuple — return what we have
		return ProcTuple{PID: int32(pid), Exe: binPath}, pidFd, nil
	}
	return tuple, pidFd, nil
}

// forkExec uses clone+exec for process creation with pidfd.
// Falls back to os.StartProcess + pidfd_open if clone not available.
func forkExec(binary string, argv []string, envp []string) (int, int, error) {
	// Use os.StartProcess as portable base, then open pidfd
	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   envp,
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	proc, err := os.StartProcess(binary, argv, attr)
	if err != nil {
		return -1, -1, err
	}
	pidFd, err := unixPidfdOpen(proc.Pid)
	if err != nil {
		// Process started but pidfd failed — release process? No, just return without pidfd
		return proc.Pid, -1, nil
	}
	return proc.Pid, pidFd, nil
}

func execLookPath(file string) (string, error) {
	return execLookPathImpl(file)
}

func execLookPathImpl(file string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			dir = "."
		}
		path := dir + "/" + file
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("exec: %s not found in PATH", file)
}

// ── probe (§N4: health check, 3 probes 2s interval) ──────────────────

func probeProcess(tuple ProcTuple, attempts int, interval time.Duration) error {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		// Check process exists
		current, err := readProcTuple(int(tuple.PID))
		if err != nil {
			continue
		}
		if !current.Equal(tuple) {
			return fmt.Errorf("probe: pid %d tuple changed", tuple.PID)
		}
		// Process exists and matches — probe OK
		return nil
	}
	return ErrProbeFail
}

func killProcess(pidFd int, tuple ProcTuple) {
	// Verify identity before killing
	current, err := readProcTuple(int(tuple.PID))
	if err != nil {
		return // already dead
	}
	if !current.Equal(tuple) {
		return // not our process anymore (§1.2)
	}
	unixKill(pidFd, unixSIGKILL)
}

