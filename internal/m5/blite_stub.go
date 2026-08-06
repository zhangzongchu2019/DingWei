//go:build !linux

package m5

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Stub: B-lite supervisor requires Linux 5.3+ for pidfd.
// This file provides type-compatible stubs for compilation on non-Linux hosts.

func unixOpen(path string, flags int, perm os.FileMode) (int, error) {
	return -1, errors.New("m5: linux only")
}
func unixClose(fd int)                        {}
func unixFlock(fd int, how int) error         { return errors.New("m5: linux only") }

var (
	unixSIGTERM = errorSignal{}
	unixSIGKILL = errorSignal{}
)

type errorSignal struct{}

func (errorSignal) Signal() {}

func unixKill(pidFd int, sig errorSignal) error { return errors.New("m5: linux only") }

func readProcTuple(pid int) (ProcTuple, error) {
	return ProcTuple{}, fmt.Errorf("m5: /proc not available on this platform")
}

func startProcess(binary string, args []string, env []string) (ProcTuple, int, error) {
	return ProcTuple{}, -1, errors.New("m5: linux only")
}

func probeProcess(tuple ProcTuple, attempts int, interval time.Duration) error {
	return errors.New("m5: linux only")
}

func killProcess(pidFd int, tuple ProcTuple) {}
