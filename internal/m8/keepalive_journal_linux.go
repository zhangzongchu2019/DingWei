//go:build linux

package m8

import (
	"strconv"
	"syscall"
)

// Opening a new pipe descriptor with NONBLOCK does not change the flags of the
// process logger's original descriptor. One <=PIPE_BUF write is atomic or fails
// immediately when the journal pipe is full. No blocked logger goroutine.
func writeKeepaliveJournal(data []byte) bool { return writeKeepaliveJournalFD(2, data) }
func writeKeepaliveJournalFD(originalFD int, data []byte) bool {
	if len(data) > 4096 {
		return false
	}
	// systemd journals commonly use AF_UNIX streams. MSG_DONTWAIT bounds
	// this call without changing the process-wide descriptor flags.
	n, sendErr := syscall.SendmsgN(originalFD, data, nil, nil, syscall.MSG_DONTWAIT|syscall.MSG_NOSIGNAL)
	if sendErr != syscall.ENOTSOCK {
		return sendErr == nil && n == len(data)
	}
	fd, err := syscall.Open("/proc/self/fd/"+strconv.Itoa(originalFD), syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	if syscall.Fstat(fd, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		return false
	}
	n, err = syscall.Write(fd, data)
	return err == nil && n == len(data)
}
