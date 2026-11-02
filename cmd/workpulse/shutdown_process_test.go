package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This child calls the actual production assembly. It has a temporary HOME/DB,
// two loopback listeners, fake gateway and an explicit environment allowlist.
func TestHLCProcessChild(t *testing.T) {
	if os.Getenv("HLC_PROCESS_CHILD") != "1" {
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	err := run(logger)
	os.Exit(exitCode(err))
}
func hlcFreeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
func TestHLCActualProcessIdle(t *testing.T) {
	root := t.TempDir()
	sock := filepath.Join(root, "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	public, internal := hlcFreeAddress(t), hlcFreeAddress(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHLCProcessChild$", "-test.count=1")
	cmd.Dir = root
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TZ=UTC", "HLC_PROCESS_CHILD=1", "NOTIFY_SOCKET=" + sock, "WP_DATA_DIR=" + root, "WP_DB_PATH=" + filepath.Join(root, "hub.db"), "WP_ADDR=" + public, "WP_INTERNAL_ADDR=" + internal, "FEISHU_TRANSPORT=fake", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(root, "no-team.md"), "WP_SCHEDULE_PERSONAL_DIR=" + root, "WP_CONSOLE_AUTH_ENABLED=false"}
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	_ = listener.SetReadDeadline(time.Now().Add(20 * time.Second))
	buffer := make([]byte, 1024)
	for {
		n, _, err := listener.ReadFromUnix(buffer)
		if err != nil {
			t.Fatalf("ready: %v logs=%s", err, log.String())
		}
		if strings.Contains(string(buffer[:n]), "READY=1") {
			break
		}
	}
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err = <-waited:
	case <-time.After(17 * time.Second):
		t.Fatal("child exceeded total budget")
	}
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("actual exit: %v logs=%s", err, log.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("S001 stop elapsed=%v", elapsed)
	}

	scanner := bufio.NewScanner(bytes.NewReader(log.Bytes()))
	var report shutdownReport
	found := false
	for scanner.Scan() {
		var row struct {
			Shutdown *shutdownReport `json:"shutdown"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		if row.Shutdown != nil {
			report = *row.Shutdown
			found = true
		}
	}

	if !found || report.ExitCode != 0 || len(report.Stages) != 5 {
		t.Fatalf("missing or failed report: %s", log.String())
	}
	snapshot, ok := report.Snapshot.(map[string]any)
	if !ok {
		t.Fatal("missing snapshot")
	}
	servers, ok := snapshot["servers"].(map[string]any)
	if !ok || len(servers) != 2 {
		t.Fatalf("both server results required: %+v", snapshot)
	}
	var times []time.Time
	for name, raw := range servers {
		entry := raw.(map[string]any)
		if entry["error"] != "nil" {
			t.Fatalf("%s shutdown=%+v", name, entry)
		}
		closed, err := time.Parse(time.RFC3339Nano, entry["listener_closed_utc"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if closed.Sub(start) > time.Second {
			t.Fatal("listener closure exceeded one second")
		}
		times = append(times, closed)
	}
	delta := times[0].Sub(times[1])
	if delta < 0 {
		delta = -delta
	}
	if delta > 100*time.Millisecond {
		t.Fatalf("listener close difference=%v", delta)
	}
	t.Logf("S001 raw process rc=0 stop_ms=%d listener_delta_us=%d stages=%d", elapsed.Milliseconds(), delta.Microseconds(), len(report.Stages))
	if evidence := os.Getenv("HLC_EVIDENCE_DIR"); evidence != "" {
		if err := os.MkdirAll(evidence, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidence, "S001-process.log"), log.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
