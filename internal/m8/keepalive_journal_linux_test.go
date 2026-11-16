//go:build linux

package m8

import (
	"syscall"
	"testing"
	"time"
)

func TestKeepaliveJournalPipeAndSocketBackpressure(t *testing.T) {
	for _, socket := range []bool{false, true} {
		t.Run(map[bool]string{false: "pipe", true: "socket"}[socket], func(t *testing.T) {
			var pair [2]int
			var err error
			if socket {
				pair, err = syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
			} else {
				fds := make([]int, 2)
				err = syscall.Pipe(fds)
				pair = [2]int{fds[1], fds[0]}
			}
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(pair[0])
			defer syscall.Close(pair[1])
			if !writeKeepaliveJournalFD(pair[0], []byte("test-private-free\n")) {
				t.Fatal("healthy journal rejected")
			}
			buf := make([]byte, 64)
			n, err := syscall.Read(pair[1], buf)
			if err != nil || string(buf[:n]) != "test-private-free\n" {
				t.Fatal("journal not written", n, err)
			}
			for i := 0; i < 100000; i++ {
				if !writeKeepaliveJournalFD(pair[0], make([]byte, 1024)) {
					break
				}
				if i == 99999 {
					t.Fatal("journal did not reach bounded pipe capacity")
				}
			}
			start := time.Now()
			if writeKeepaliveJournalFD(pair[0], make([]byte, 4096)) {
				t.Fatal("full journal silently accepted")
			}
			if time.Since(start) > 100*time.Millisecond {
				t.Fatal("full journal blocked")
			}
		})
	}
}
