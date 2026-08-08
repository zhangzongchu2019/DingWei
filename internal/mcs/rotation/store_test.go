package rotation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileStoreRestoresUsageAndFirstOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.json")
	s := NewFileStore(path)
	wantState := UsageState{SessionID: "s", Model: "m", MaxContext: 100, LastSampleSeq: 9, ConsecutiveHigh: 1, RotationID: "rot-1"}
	if err := s.SaveUsage(wantState); err != nil {
		t.Fatal(err)
	}
	want := RotationOutcome{RequestID: "req-1", RotationID: "rot-1", Outcome: "applied", Phase: PhaseActivating, SealedFenceToken: "sealed:v1:abc"}
	if got, replay, err := s.RecordFirstOutcome(want); err != nil || replay || got != want {
		t.Fatalf("first=(%+v,%v,%v)", got, replay, err)
	}

	restarted := NewFileStore(path)
	state, outcomes, err := restarted.Load()
	if err != nil || state != wantState || outcomes["req-1"] != want {
		t.Fatalf("restart state=%+v outcomes=%+v err=%v", state, outcomes, err)
	}
	changed := want
	changed.Outcome = "cas_miss"
	got, replay, err := restarted.RecordFirstOutcome(changed)
	if err != nil || !replay || got != want {
		t.Fatalf("replay=(%+v,%v,%v), want first", got, replay, err)
	}
}

func TestFileStoreRejectsIdentityConflicts(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "rotation.json"))
	first := RotationOutcome{RequestID: "req-1", RotationID: "rot-1", Outcome: "applied", Phase: PhaseActivating}
	if _, _, err := s.RecordFirstOutcome(first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordFirstOutcome(RotationOutcome{RequestID: "req-1", RotationID: "rot-2", Outcome: "applied"}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("request reuse err=%v", err)
	}
	if _, _, err := s.RecordFirstOutcome(RotationOutcome{RequestID: "req-2", RotationID: "rot-1", Outcome: "applied"}); !errors.Is(err, ErrRotationConflict) {
		t.Fatalf("rotation reuse err=%v", err)
	}
}

func TestFileStoreWriteFailureNeverReturnsSuccess(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "rotation.json"))
	s.write = func(string, []byte) error { return errors.New("fsync failed") }
	if err := s.SaveUsage(UsageState{SessionID: "s"}); err == nil {
		t.Fatal("SaveUsage returned success")
	}
	if _, replay, err := s.RecordFirstOutcome(RotationOutcome{RequestID: "r", RotationID: "rot", Outcome: "applied"}); err == nil || replay {
		t.Fatalf("RecordFirstOutcome=(replay=%v,err=%v)", replay, err)
	}
}

func TestFileStoreConcurrentRotationGuard(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "rotation.json"))
	var wg sync.WaitGroup
	var wins, conflicts int
	var mu sync.Mutex
	for _, request := range []string{"req-a", "req-b"} {
		wg.Add(1)
		go func(request string) {
			defer wg.Done()
			_, _, err := s.RecordFirstOutcome(RotationOutcome{RequestID: request, RotationID: "rot-1", Outcome: "applied"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrRotationConflict):
				conflicts++
			default:
				t.Errorf("unexpected err=%v", err)
			}
		}(request)
	}
	wg.Wait()
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestFileStoreRejectsCorruptionAndHasNoPlainTokenField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewFileStore(path).Load(); err == nil {
		t.Fatal("corrupt store accepted")
	}

	path = filepath.Join(t.TempDir(), "clean.json")
	s := NewFileStore(path)
	if _, _, err := s.RecordFirstOutcome(RotationOutcome{RequestID: "r", RotationID: "rot", Outcome: "applied", SealedFenceToken: "sealed"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"fence_token"`) {
		t.Fatalf("plaintext token field in %s", raw)
	}
}
