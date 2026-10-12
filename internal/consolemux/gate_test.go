package consolemux

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type deadlineWriter struct {
	mu       sync.Mutex
	deadline time.Time
	wrote    int
	header   http.Header
}

func (w *deadlineWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (*deadlineWriter) WriteHeader(int)                      {}
func (w *deadlineWriter) SetWriteDeadline(t time.Time) error { w.deadline = t; return nil }
func (w *deadlineWriter) Write(p []byte) (int, error)        { w.wrote++; return len(p), nil }
func (*deadlineWriter) FlushError() error                    { return nil }

func TestGateDoesNotExtendSlowCheckOrResurrectRevokedScope(t *testing.T) {
	now := time.Now()
	o := Owner{Nonce: "synthetic", Generation: 1, Until: now.Add(3 * time.Second)}
	g := NewGate(o)
	v := g.Version()
	if g.CommitCheck(v, now.Add(-time.Second-time.Millisecond), now.Add(time.Hour), o, map[string]string{"a": "1"}) {
		t.Fatal("expired slow check admitted")
	}
	if !g.CommitCheck(v, now, now.Add(time.Hour), o, map[string]string{"a": "1", "b": "1"}) {
		t.Fatal("fresh check rejected")
	}
	w := &deadlineWriter{}
	if err := g.Write(w, "a", "1", []byte("data: test\n\n")); err != nil {
		t.Fatal(err)
	}
	if w.deadline.After(time.Now().Add(WriteLimit)) {
		t.Fatal("write exceeded deadline")
	}
	g.Revoke("a")
	if g.CommitCheck(v, now, now.Add(time.Hour), o, map[string]string{"a": "1"}) {
		t.Fatal("late check resurrected")
	}
	if err := g.Write(w, "a", "1", nil); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked queued frame written")
	}
	if err := g.Write(w, "b", "1", []byte("data: other\n\n")); err != nil {
		t.Fatal("other agent blocked", err)
	}
}
func TestGateRejectsExpiredSlotAndUnsupportedWriter(t *testing.T) {
	now := time.Now()
	o := Owner{Nonce: "synthetic", Generation: 1, Until: now.Add(-time.Millisecond)}
	g := NewGate(o)
	g.CommitCheck(0, now, now.Add(time.Hour), o, map[string]string{"a": "1"})
	if err := g.Write(&deadlineWriter{}, "a", "1", nil); !errors.Is(err, ErrDenied) {
		t.Fatal("expired slot wrote")
	}
	o.Until = now.Add(time.Second)
	g = NewGate(o)
	g.CommitCheck(0, now, now.Add(time.Hour), o, map[string]string{"a": "1"})
	if err := g.Write(httptest.NewRecorder(), "a", "1", []byte("data: body\n\n")); err == nil {
		t.Fatal("unsupported cutoff admitted")
	}
}
