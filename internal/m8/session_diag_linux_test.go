//go:build linux

package m8

import (
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSessionDiagnosticsFullPipeNeverBlocksReader(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	fd := int(write.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	fill := make([]byte, 4096)
	for {
		_, err = syscall.Write(fd, fill)
		if err == syscall.EAGAIN {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	h, _, _ := newTestHub(t)
	h.sessionJournal = func(raw []byte) bool { return writeKeepaliveJournalFD(fd, raw) }
	reg := obs.New(nil)
	d := newSessionDiagnostics(h, "pipe-fixture")
	d.client = &sessionClient{obsConn: reg.OpenWS("session")}
	d.reason("reader_eof")
	done := make(chan struct{})
	go func() { d.end(); close(done) }()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("full journal pipe blocked session end")
	}
	for _, v := range reg.Snapshot().WS {
		if v.Kind == "session" && v.DiagnosticDrop != 1 {
			t.Fatal("full-pipe drop missing", v.DiagnosticDrop)
		}
	}
}
