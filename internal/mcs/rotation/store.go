package rotation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrRequestConflict  = errors.New("rotation request id conflict")
	ErrRotationConflict = errors.New("rotation id already belongs to another request")
)

// RotationOutcome is the durable, replayable first outcome. It intentionally
// has no plaintext fence-token field; callers may persist only an externally
// sealed token blob.
type RotationOutcome struct {
	RequestID        string `json:"request_id"`
	RotationID       string `json:"rotation_id"`
	Outcome          string `json:"outcome"`
	Phase            Phase  `json:"phase"`
	ReasonCode       string `json:"reason_code,omitempty"`
	SealedFenceToken string `json:"sealed_fence_token,omitempty"`
}

type durableRotationData struct {
	SchemaVersion string                     `json:"schema_version"`
	Usage         UsageState                 `json:"usage"`
	Outcomes      map[string]RotationOutcome `json:"outcomes"`
	Rotations     map[string]string          `json:"rotations"` // rotation_id -> first request_id
}

// FileStore is the single-coordinator durable boundary for M5 debounce and
// replay outcomes. Board CAS remains the multi-coordinator safety boundary.
type FileStore struct {
	path  string
	mu    sync.Mutex
	write func(string, []byte) error
}

func NewFileStore(path string) *FileStore {
	s := &FileStore{path: path}
	s.write = s.atomicWrite
	return s
}

func (s *FileStore) Load() (UsageState, map[string]RotationOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.loadLocked()
	if err != nil {
		return UsageState{}, nil, err
	}
	return d.Usage, cloneOutcomes(d.Outcomes), nil
}

func (s *FileStore) SaveUsage(state UsageState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.loadLocked()
	if err != nil {
		return err
	}
	d.Usage = state
	return s.saveLocked(d)
}

// RecordFirstOutcome persists before returning success. Exact request replay
// returns the first outcome. Reusing a request or rotation with different
// identity fails closed and never overwrites durable state.
func (s *FileStore) RecordFirstOutcome(in RotationOutcome) (RotationOutcome, bool, error) {
	if in.RequestID == "" || in.RotationID == "" || in.Outcome == "" {
		return RotationOutcome{}, false, errors.New("rotation outcome identity is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.loadLocked()
	if err != nil {
		return RotationOutcome{}, false, err
	}
	if first, ok := d.Outcomes[in.RequestID]; ok {
		if first.RotationID != in.RotationID {
			return RotationOutcome{}, false, ErrRequestConflict
		}
		return first, true, nil
	}
	if firstRequest, ok := d.Rotations[in.RotationID]; ok && firstRequest != in.RequestID {
		return RotationOutcome{}, false, ErrRotationConflict
	}
	d.Outcomes[in.RequestID] = in
	d.Rotations[in.RotationID] = in.RequestID
	if err := s.saveLocked(d); err != nil {
		return RotationOutcome{}, false, err
	}
	return in, false, nil
}

func (s *FileStore) loadLocked() (durableRotationData, error) {
	d := durableRotationData{
		SchemaVersion: "m5-rotation-state-v1",
		Outcomes:      make(map[string]RotationOutcome),
		Rotations:     make(map[string]string),
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return durableRotationData{}, err
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return durableRotationData{}, fmt.Errorf("decode durable rotation state: %w", err)
	}
	if d.SchemaVersion != "m5-rotation-state-v1" {
		return durableRotationData{}, fmt.Errorf("unsupported rotation state schema %q", d.SchemaVersion)
	}
	if d.Outcomes == nil || d.Rotations == nil {
		return durableRotationData{}, errors.New("durable rotation maps missing")
	}
	return d, nil
}

func (s *FileStore) saveLocked(d durableRotationData) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.write(s.path, raw)
}

func (s *FileStore) atomicWrite(path string, raw []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".m5-rotation-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	defer cleanup()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func cloneOutcomes(in map[string]RotationOutcome) map[string]RotationOutcome {
	out := make(map[string]RotationOutcome, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
