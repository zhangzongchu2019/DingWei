package m8

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeReadTimeoutHasNoHardcodedFamilyMembers(t *testing.T) {
	_, ownFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate timeout contract test")
	}
	// Scan only hub_test.go, deliberately excluding this guard file: the guard
	// must describe the forbidden literal without counting its own pattern as a
	// production-family hit.
	source, err := os.ReadFile(filepath.Join(filepath.Dir(ownFile), "hub_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The one declaration is the approved default, not a family member. Remove
	// exactly that declaration before scanning; any call-site literal still hits.
	scanned := regexp.MustCompile(`(?m)^const defaultEnvelopeReadTimeout = 2 \* time[.]Second\n`).ReplaceAll(source, nil)
	forbidden := regexp.MustCompile(`2\s*\*\s*time[.]Second|time[.]Second\s*\*\s*2`)
	if matches := forbidden.FindAll(scanned, -1); len(matches) != 0 {
		t.Fatalf("envelope timeout family regained %d hardcoded 2-second member(s)", len(matches))
	}
	if uses := strings.Count(string(source), "envelopeReadTimeout"); uses != 19 {
		t.Fatalf("envelope timeout consolidation drifted: uses=%d want 19 (definition + 17 original call sites + joint sec-ops wait)", uses)
	}
}

func TestEnvelopeReadTimeoutDerivesFromEnvironment(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestEnvelopeReadTimeoutEnvironmentChild$", "-test.count=1", "-test.v")
	command.Env = append(os.Environ(), "M8_TIMEOUT_CHILD=1", "M8_ENVELOPE_TEST_TIMEOUT=1ms")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("timeout environment is not connected to the package knob: %v\n%s", err, output)
	}
}

func TestEnvelopeReadTimeoutEnvironmentChild(t *testing.T) {
	if os.Getenv("M8_TIMEOUT_CHILD") != "1" {
		t.Skip("subprocess probe only")
	}
	if envelopeReadTimeout != time.Millisecond {
		t.Fatalf("M8_ENVELOPE_TEST_TIMEOUT ignored: got=%s want=1ms", envelopeReadTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), envelopeReadTimeout)
	defer cancel()
	<-ctx.Done()
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("tiny timeout did not reach a context deadline: %v", ctx.Err())
	}
}
