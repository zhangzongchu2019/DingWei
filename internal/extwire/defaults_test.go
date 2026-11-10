package extwire

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func TestConsentConfigIsolationAndValidation(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"t":[]}`, `{"t":["Helper.zzc","Other.zzc"]}`} {
		d, e := ParseConsentTargets(raw)
		if e != nil {
			t.Fatalf("ZC_CONFIG_VALID %v", e)
		}
		if len(d.Allowed("foreign")) != 0 {
			t.Fatal("ZC_CONFIG_TENANT_LEAK")
		}
		a := d.Allowed("t")
		if len(a) > 0 {
			a[0] = "evil.zzc"
			if d.Allowed("t")[0] != "Helper.zzc" {
				t.Fatal("ZC_CONFIG_MUTABLE")
			}
		}
	}
	for _, raw := range []string{`[]`, `{"t":["foo"]}`, `{"t":["bot_AG1.zzc"]}`, `{"t":["Helper.zzc","Helper.zzc"]}`, `{"t":["Helper.zzc","HELPER.ZZC"]}`, `{" t ":["Helper.zzc"]}`, `{"t":[` + strings.Repeat(`"Helper.zzc",`, 64) + `"Other.zzc"]}`} {
		if _, e := ParseConsentTargets(raw); e == nil {
			t.Fatalf("ZC_CONFIG_INVALID_ACCEPTED %s", raw)
		}
	}
}
func TestConsentAuthoritativeValidity(t *testing.T) {
	ctx := context.Background()
	st := identity.NewMemStore()
	if e := st.CreateUser(ctx, identity.User{UserID: "owner", ShortName: "zzc", Type: identity.UserMember, State: identity.UserActive}); e != nil {
		t.Fatal(e)
	}
	// Use a store wrapper to model each authoritative agent state without touching
	// the ordinary registration path, which must still reject external names.
	for _, mode := range []string{"active", "missing", "revoked", "external", "owner-inactive"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := ParseConsentTargets(`{"t":["Helper.zzc"]}`)
			a := identity.Agent{ID: "a", Name: "Helper", OwnerLC: "owner", Origin: identity.OriginSelfhost, State: identity.AgentActive}
			if mode == "revoked" {
				a.State = identity.AgentRevoked
			}
			if mode == "external" {
				a.Origin = identity.OriginExternal
			}
			d.Identity = identity.NewService(consentIdentityStore{Store: st, a: a, mode: mode}, nil, nil)
			if d.Valid(ctx, "Helper.zzc") != (mode == "active") {
				t.Fatalf("ZC_VALIDITY_%s", mode)
			}
		})
	}
}

type consentIdentityStore struct {
	identity.Store
	a    identity.Agent
	mode string
}

func (s consentIdentityStore) FindAgentByName(ctx context.Context, owner, name string) (*identity.Agent, error) {
	if s.mode == "missing" {
		return nil, identity.ErrNotFound
	}
	a := s.a
	return &a, nil
}
func (s consentIdentityStore) GetUserByShortName(ctx context.Context, name string) (*identity.User, error) {
	u, e := s.Store.GetUserByShortName(ctx, name)
	if e == nil && s.mode == "owner-inactive" {
		u.State = identity.UserDisabled
	}
	return u, e
}
