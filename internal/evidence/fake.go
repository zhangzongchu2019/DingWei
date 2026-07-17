package evidence

import "context"

// FakeAdapter is a deterministic transcript adapter for M4 tests and local demos.
type FakeAdapter struct {
	Sessions []Session
	Chunks   map[string][]Chunk
}

func (f *FakeAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	return append([]Session(nil), f.Sessions...), nil
}

func (f *FakeAdapter) ReadIncremental(ctx context.Context, session Session) ([]Chunk, error) {
	if f.Chunks == nil {
		return nil, nil
	}
	return append([]Chunk(nil), f.Chunks[session.ID]...), nil
}
