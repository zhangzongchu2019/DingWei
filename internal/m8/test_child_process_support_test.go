package m8

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	unixSocketPathMax = 107 // sun_path reserves one byte for the trailing NUL.
	childStderrLimit  = 8 << 10
)

// shortIPCSocketPath deliberately roots test IPC under /tmp instead of the
// caller's TMPDIR. The socket itself does not exist yet when the byte limit is
// checked.
func shortIPCSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "dwipc-")
	if err != nil {
		t.Fatalf("create short IPC directory under /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "m.sock")
	if n := len([]byte(socket)); n > unixSocketPathMax {
		t.Fatalf("short IPC socket path too long before helper startup: %d bytes exceeds Unix limit %d: %q", n, unixSocketPathMax, socket)
	}
	return socket
}

// boundedStderr retains only the most recent bytes. Its report method drops a
// leading partial line after truncation and replaces any whole line containing
// credential-bearing markers, so child diagnostics cannot echo secrets.
type boundedStderr struct {
	mu        sync.Mutex
	limit     int
	data      []byte
	truncated bool
}

func newBoundedStderr(limit int) *boundedStderr {
	return &boundedStderr{limit: limit}
}

func (b *boundedStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.truncated = true
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *boundedStderr) report() string {
	b.mu.Lock()
	raw := string(append([]byte(nil), b.data...))
	truncated := b.truncated
	b.mu.Unlock()
	if raw == "" {
		return "(empty)"
	}
	if truncated {
		if newline := strings.IndexByte(raw, '\n'); newline >= 0 {
			raw = raw[newline+1:]
		} else {
			return "[leading truncated stderr line omitted]"
		}
	}
	// Whole-line filters cover secret/token/credential/authorization/bearer/
	// password/pairing-code/API-key/cookie/private-key markers and helper tokens.
	markers := []string{"secret", "token", "credential", "authorization", "bearer", "password", "claim code", `"code"`, "code=", "code:", "api_key", "api key", "cookie", "private key", "dwa_"}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				lines[i] = "[redacted: credential-bearing stderr line]"
				break
			}
		}
	}
	report := strings.TrimSpace(strings.Join(lines, "\n"))
	if len([]byte(report)) > b.limit {
		report = report[len(report)-b.limit:]
		if newline := strings.IndexByte(report, '\n'); newline >= 0 {
			report = report[newline+1:]
		} else {
			return "[leading truncated stderr line omitted]"
		}
	}
	return report
}

func logChildStderrOnFailure(t *testing.T, label string, stderr *boundedStderr) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s stderr tail (last %d bytes; credential-bearing lines filtered):\n%s", label, childStderrLimit, stderr.report())
		}
	})
}

func TestBoundedStderrRedactsCredentialLinesAndKeepsTail(t *testing.T) {
	stderr := newBoundedStderr(48)
	_, _ = stderr.Write([]byte("discarded first line\nuseful diagnostic\nBearer secret-value\nfinal line\n"))
	report := stderr.report()
	if strings.Contains(report, "secret-value") || strings.Contains(report, "Bearer") {
		t.Fatalf("credential-bearing stderr leaked: %q", report)
	}
	if !strings.Contains(report, "final line") || len([]byte(report)) > 48 {
		t.Fatalf("bounded tail was not retained: %q", report)
	}
}

func TestShortIPCSocketPathIgnoresCallerTMPDIR(t *testing.T) {
	socket := shortIPCSocketPath(t)
	if !strings.HasPrefix(socket, "/tmp/") {
		t.Fatalf("IPC socket path is not rooted in fixed /tmp: %q", socket)
	}
	if n := len([]byte(socket)); n > unixSocketPathMax {
		t.Fatalf("IPC socket path length = %d, exceeds %d", n, unixSocketPathMax)
	}
}
