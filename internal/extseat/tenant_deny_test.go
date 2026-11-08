package extseat

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

type tenantDenyStoreSpy struct {
	identity.Store
	resolves int
	audit    []identity.AuditEvent
	fail     bool
}

func (s *tenantDenyStoreSpy) GetUserByShortName(ctx context.Context, n string) (*identity.User, error) {
	s.resolves++
	return s.Store.GetUserByShortName(ctx, n)
}
func (s *tenantDenyStoreSpy) FindAgentByName(ctx context.Context, o, n string) (*identity.Agent, error) {
	s.resolves++
	return s.Store.FindAgentByName(ctx, o, n)
}
func (s *tenantDenyStoreSpy) AppendAudit(ctx context.Context, e identity.AuditEvent) error {
	s.audit = append(s.audit, e)
	if s.fail {
		return errors.New("synthetic audit unavailable")
	}
	return s.Store.AppendAudit(ctx, e)
}
func tenantDenyService() (*Service, *tenantDenyStoreSpy) {
	spy := &tenantDenyStoreSpy{Store: identity.NewMemStore()}
	at := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	return &Service{Identity: identity.NewService(spy, func() time.Time { return at }, nil), Groups: testGroups{}, Resource: "https://resource.example.invalid/mcp", TenantKey: "configured-tenant", Now: func() time.Time { return at }}, spy
}
func deniedPrincipal() extshared.Principal {
	return extshared.Principal{Provider: "feishu", TenantKey: "foreign-tenant-secret", Issuer: "app", Subject: "source-principal", DisplayName: "alice"}
}
func assertTenantDeny(t *testing.T, s *Service, target string) {
	t.Helper()
	raw, seat, group, e := s.IssueToken(context.Background(), deniedPrincipal(), "bot", target)
	if e != extshared.ErrTenantRejected || e.Error() != "extshared: tenant not allowed" || raw != "" || group != "" || !reflect.DeepEqual(seat, Seat{}) {
		t.Fatalf("deny response changed: %q %#v %q %v", raw, seat, group, e)
	}
}

// YIZU_PEP_BEFORE_RESOLVE(extseat)
func TestTenantDenyPEPBeforeResolve(t *testing.T) {
	s, spy := tenantDenyService()
	assertTenantDeny(t, s, "helper.ho")
	if spy.resolves != 0 {
		t.Fatalf("resource query spy=%d want0", spy.resolves)
	}
	if len(spy.audit) != 1 {
		t.Fatal("audit operation missing")
	}
}

// YIZU_DENY_NOT_EMPTY_SET(extseat)
func TestTenantDenyIsErrorNotSuccess(t *testing.T) {
	s, _ := tenantDenyService()
	assertTenantDeny(t, s, "helper.ho")
}

// YIZU_EXISTENCE_INDISTINGUISHABLE(extseat)
func TestTenantDenyExistingAndMissingSameShape(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t)) // helper.ho exists; missing.ho does not.
	spy := &tenantDenyStoreSpy{Store: f.s.Identity.Store}
	f.s.Identity.Store = spy
	for _, target := range []string{"helper.ho", "missing.ho"} {
		assertTenantDeny(t, f.s, target)
	}
	if spy.resolves != 0 || len(spy.audit) != 2 || !reflect.DeepEqual(spy.audit[0], spy.audit[1]) {
		t.Fatalf("target existence affected lookup/audit: %d %#v", spy.resolves, spy.audit)
	}
	var n int
	row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM audit_log WHERE action='extseat.issue_denied'`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&n); e != nil || n != 2 {
		t.Fatalf("persisted audit=%d err=%v", n, e)
	}
}

// YIZU_MINIMAL_DENY_AUDIT(extseat)
func TestTenantDenyMinimalAudit(t *testing.T) {
	s, spy := tenantDenyService()
	assertTenantDeny(t, s, "sensitive-resource.ho")
	if len(spy.audit) != 1 {
		t.Fatal("minimal deny audit missing")
	}
	a := spy.audit[0]
	var d map[string]string
	if e := json.Unmarshal([]byte(a.Detail), &d); e != nil {
		t.Fatal(e)
	}
	want := map[string]string{"tenant": "configured-tenant", "op": "extseat.issue_token", "decision": "deny"}
	if !reflect.DeepEqual(d, want) || a.Actor != digest([]string{"feishu", "app", "source-principal"}) || a.Target != "" || a.Action != "extseat.issue_denied" || !a.At.Equal(s.now()) {
		t.Fatalf("audit shape=%#v", a)
	}
	b, _ := json.Marshal(a)
	for _, secret := range []string{"foreign-tenant-secret", "sensitive-resource.ho", "source-principal"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("audit reflected predicate input")
		}
	}
}
func TestTenantDenyAuditFailureStillDenies(t *testing.T) {
	s, spy := tenantDenyService()
	spy.fail = true
	assertTenantDeny(t, s, "helper.ho")
	if len(spy.audit) != 1 || spy.resolves != 0 {
		t.Fatalf("fail-closed audit=%d reads=%d", len(spy.audit), spy.resolves)
	}
}

func TestTenantDenyUnconfiguredAuditStillDenies(t *testing.T) {
	s, _ := tenantDenyService()
	s.Identity.Store = nil
	assertTenantDeny(t, s, "helper.ho")
}
