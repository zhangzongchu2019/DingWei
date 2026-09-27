package m8

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecurityOpsBroadcastEstablishesSimultaneityBeforeDispatch(t *testing.T) {
	_, ownFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate sec-ops simultaneity contract")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(ownFile), "hub_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func TestSecurityOpsRoutesToAllSystemSessions")
	if start < 0 {
		t.Fatal("cannot isolate sec-ops broadcast test")
	}
	end := strings.Index(text[start:], "\nfunc TestParseTerminalInputCommand")
	if end < 0 {
		t.Fatal("cannot isolate sec-ops broadcast test")
	}
	body := text[start : start+end]
	joint := "waitSessionsOnlineTogether(t, hub, key.ID, claudeName, deepseekName)"
	if strings.Count(body, joint) != 1 {
		t.Fatalf("sec-ops simultaneity guard missing: joint_wait_count=%d want 1", strings.Count(body, joint))
	}
	if strings.Contains(body, "waitSessionOnline(t, hub, key.ID, claudeName)") || strings.Contains(body, "waitSessionOnline(t, hub, key.ID, deepseekName)") {
		t.Fatal("sec-ops test regressed to sequential waits; simultaneity is not established")
	}
}
