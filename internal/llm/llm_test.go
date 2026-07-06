package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testProvider struct {
	name string
	out  string
	err  error
}

func (p testProvider) Name() string                                     { return p.name }
func (p testProvider) Complete(_ context.Context, _, _ string) (string, error) { return p.out, p.err }

func TestFailoverPrimarySucceeds(t *testing.T) {
	f := &Failover{Providers: []Provider{
		testProvider{name: "primary", out: `{"ok":true}`, err: nil},
		testProvider{name: "backup", out: "", err: errors.New("down")},
	}}
	out, err := f.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("out=%q", out)
	}
}

func TestFailoverFallsBackToBackup(t *testing.T) {
	f := &Failover{Providers: []Provider{
		testProvider{name: "primary", out: "", err: errors.New("primary down")},
		testProvider{name: "backup", out: `{"from":"backup"}`, err: nil},
	}}
	out, err := f.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"from":"backup"}` {
		t.Fatalf("out=%q", out)
	}
}

func TestFailoverAllDown(t *testing.T) {
	f := &Failover{Providers: []Provider{
		testProvider{name: "primary", err: errors.New("down1")},
		testProvider{name: "backup", err: errors.New("down2")},
	}}
	_, err := f.Complete(context.Background(), "sys", "user")
	if err == nil || !errors.Is(err, ErrAllDown) {
		t.Fatalf("want ErrAllDown, got %v", err)
	}
	// errors.Join(ErrAllDown, last) — last 是最后一个 provider 的错误
	if !strings.Contains(err.Error(), "down2") {
		t.Fatalf("error message should mention last failure: %v", err)
	}
}

func TestFailoverEmptyProviders(t *testing.T) {
	f := &Failover{Providers: nil}
	_, err := f.Complete(context.Background(), "sys", "user")
	if !errors.Is(err, ErrAllDown) {
		t.Fatalf("want ErrAllDown for empty providers, got %v", err)
	}
}

func TestFailoverHealthy(t *testing.T) {
	f := &Failover{Providers: []Provider{
		testProvider{name: "primary", err: errors.New("down")},
		testProvider{name: "backup", out: "ok", err: nil},
	}}
	if !f.Healthy(context.Background()) {
		t.Fatal("Healthy()=false, want true (backup is ok)")
	}
}

func TestFailoverNotHealthy(t *testing.T) {
	f := &Failover{Providers: []Provider{
		testProvider{name: "primary", err: errors.New("down1")},
		testProvider{name: "backup", err: errors.New("down2")},
	}}
	if f.Healthy(context.Background()) {
		t.Fatal("Healthy()=true, want false (all down)")
	}
}

func TestStubAlwaysFails(t *testing.T) {
	s := Stub{ID: "test"}
	if s.Name() != "stub:test" {
		t.Fatalf("Name=%q", s.Name())
	}
	_, err := s.Complete(context.Background(), "", "")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unexpected stub error: %v", err)
	}
}
