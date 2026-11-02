package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebsocketAlreadyClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		closed bool
	}{
		{"EOF", fmt.Errorf("frame header: %w", io.EOF), true},
		{"closed network", fmt.Errorf("socket: %w", net.ErrClosed), true},
		{"peer close frame", fmt.Errorf("read: %w", websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "fixture"}), true},
		{"peer broken pipe", &net.OpError{Op: "write", Net: "tcp", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}}, true},
		{"peer reset", fmt.Errorf("socket: %w", syscall.ECONNRESET), true},
		{"write timeout", fmt.Errorf("write: %w", os.ErrDeadlineExceeded), false},
		{"deadline", context.DeadlineExceeded, false},
		{"canceled", context.Canceled, false},
		{"joined timeout closed", errors.Join(net.ErrClosed, os.ErrDeadlineExceeded), false},
		{"untyped text", errors.New("use of closed network connection EOF"), false},
		{"other failure", io.ErrShortWrite, false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := websocketAlreadyClosed(tc.err); got != tc.closed {
				t.Fatalf("closed=%v want=%v", got, tc.closed)
			}
		})
	}
}

type hlcFailWriteConn struct {
	net.Conn
	fail *atomic.Bool
}

func (c *hlcFailWriteConn) Write(p []byte) (int, error) {
	if c.fail.Load() {
		return 0, os.ErrDeadlineExceeded
	}
	return c.Conn.Write(p)
}

type hlcFailWriteListener struct {
	net.Listener
	fail *atomic.Bool
}

func (l hlcFailWriteListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &hlcFailWriteConn{c, l.fail}, nil
}

// S014 pins tracked sockets until shutdown to reproduce the real unregister /
// SIGTERM race deterministically. No cleanup-order swap or shutdown stub.
func TestHubWSCloseProcessChild(t *testing.T) {
	mode := os.Getenv("HLC_WS_CLOSE_CHILD")
	if mode == "" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	h := &Hub{}
	listener, err := net.Listen("tcp", os.Getenv("HLC_WS_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	accepted := make(chan *websocket.Conn, 3)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		h.trackSocket(c, "fixture_ws")
		accepted <- c
	})}
	go server.Serve(hlcFailWriteListener{listener, &fail})
	clients := make([]*websocket.Conn, 0, 3)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, _, e := websocket.Dial(ctx, "ws://"+listener.Addr().String(), nil)
		cancel()
		if e != nil {
			t.Fatal(e)
		}
		<-accepted
		clients = append(clients, c)
	}
	if mode == "peer-first" {
		for _, c := range clients {
			_ = c.CloseNow()
		}
	} else {
		for _, c := range clients {
			go func(c *websocket.Conn) { _, _, _ = c.Read(context.Background()) }(c)
		}
	}
	if mode == "live-write-failure" {
		fail.Store(true)
	}
	fmt.Printf("HLC_WS_READY mode=%s address=%s home=%s\n", mode, listener.Addr(), os.Getenv("HOME"))
	<-signals
	fmt.Printf("HLC_WS_SIGTERM %s\n", time.Now().UTC().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err = h.CloseContext(ctx)
	snapshot := h.WSCloseSnapshot()
	json.NewEncoder(os.Stdout).Encode(snapshot)
	for _, c := range clients {
		_ = c.CloseNow()
	}
	_ = server.Close()
	if err != nil {
		fmt.Printf("HLC_WS_ERROR %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestHubWSCloseProcesses(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		exit, already, failed int
	}{
		{"peer-first", 0, 3, 0}, {"hub-first", 0, 0, 0}, {"live-write-failure", 1, 0, 3},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			reserve, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			address := reserve.Addr().String()
			reserve.Close()
			t.Logf("RESOURCE address=%s home=%s database=none", address, dir)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHubWSCloseProcessChild$", "-test.v")
			cmd.Env = []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH"), "TZ=UTC", "HLC_WS_CLOSE_CHILD=" + tc.mode, "HLC_WS_ADDRESS=" + address}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = cmd.Stdout
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			// Read the ready marker before sending a real SIGTERM. Keep every raw line.
			var raw strings.Builder
			one := make([]byte, 1)
			ready := make(chan error, 1)
			go func() {
				for {
					n, e := stdout.Read(one)
					if n > 0 {
						raw.Write(one[:n])
						if one[0] == '\n' && strings.Contains(raw.String(), "HLC_WS_READY") {
							ready <- nil
							return
						}
					}
					if e != nil {
						ready <- e
						return
					}
				}
			}()
			select {
			case err = <-ready:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("ready timeout")
			}
			sent := time.Now().UTC()
			if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			tail, e := io.ReadAll(stdout)
			if e != nil {
				t.Fatal(e)
			}
			raw.Write(tail)
			err = cmd.Wait()
			got := 0
			if err != nil {
				var x *exec.ExitError
				if !errors.As(err, &x) {
					t.Fatal(err)
				}
				got = x.ExitCode()
			}
			t.Logf("signal_utc=%s exit=%d raw=%s", sent.Format(time.RFC3339Nano), got, raw.String())
			if out := os.Getenv("HLC_WS_EVIDENCE"); out != "" {
				os.WriteFile(fmt.Sprintf("%s/%s-%d.log", out, tc.mode, sent.UnixNano()), []byte(raw.String()), 0644)
			}
			if got != tc.exit {
				t.Fatalf("S014_EXIT: got %d want %d", got, tc.exit)
			}
			var snap WSCloseSnapshot
			for _, line := range strings.Split(raw.String(), "\n") {
				if strings.HasPrefix(line, "{\"already_closed\"") {
					if e := json.Unmarshal([]byte(line), &snap); e != nil {
						t.Fatal(e)
					}
				}
			}
			if snap.AlreadyClosed != tc.already || snap.Failed != tc.failed || len(snap.Records) != 3 {
				t.Fatalf("S014_COUNTS: %+v", snap)
			}
			for _, r := range snap.Records {
				if tc.mode != "hub-first" && r.Error == "" {
					t.Fatalf("S014_RAW_ERROR_MISSING: %+v", r)
				}
			}
		})
	}
}
