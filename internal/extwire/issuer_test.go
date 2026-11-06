package extwire

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

type fakeSeats struct {
	gotP                extshared.Principal
	gotShort, gotTarget string
	raw                 string
	seat                extseat.Seat
	group               string
	err                 error
}

func (f *fakeSeats) IssueToken(_ context.Context, p extshared.Principal, short, target string) (string, extseat.Seat, string, error) {
	f.gotP, f.gotShort, f.gotTarget = p, short, target
	return f.raw, f.seat, f.group, f.err
}

func TestIssuerMapsAllFields(t *testing.T) {
	exp := time.Date(2026, 11, 20, 1, 2, 3, 0, time.UTC)
	p := extshared.Principal{Provider: "feishu", TenantKey: "t1", Issuer: "app1", Subject: "ou_1", DisplayName: "zhangwei"}
	f := &fakeSeats{raw: "tok-plain", seat: extseat.Seat{ID: "seat-9", FullName: "mybot_AG3.zzc", ExpiresAt: exp}, group: "AG3"}
	got, err := Issuer{Seats: f, Resource: "https://dw123.cmjrt.com/mcp"}.IssueToken(context.Background(),
		feishulogin.IssueRequest{Principal: p, ShortName: "mybot", TargetAgent: "prodops.zzc", AttemptID: "att-1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotP != p || f.gotShort != "mybot" || f.gotTarget != "prodops.zzc" {
		t.Fatalf("request not passed through: %+v %q %q", f.gotP, f.gotShort, f.gotTarget)
	}
	want := feishulogin.IssuedToken{Token: "tok-plain", SeatID: "seat-9", ExternalAgentFullName: "mybot_AG3.zzc",
		Group: "AG3", ExpiresAt: exp, Audience: "https://dw123.cmjrt.com/mcp",
		Scopes: []string{"list_online", "send_message", "wait_for_messages", "ack"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping mismatch:\n got %+v\nwant %+v", got, want)
	}
	got.Scopes[0] = "mutated"
	if extshared.MVPScopes[0] != "list_online" {
		t.Fatal("returned scopes must not alias the shared constant")
	}
}

func TestIssuerPropagatesErrorsAndRejectsMisconfig(t *testing.T) {
	f := &fakeSeats{err: extshared.ErrTenantRejected}
	if _, err := (Issuer{Seats: f, Resource: "r"}).IssueToken(context.Background(), feishulogin.IssueRequest{}); !errors.Is(err, extshared.ErrTenantRejected) {
		t.Fatalf("error not propagated: %v", err)
	}
	if _, err := (Issuer{Seats: f}).IssueToken(context.Background(), feishulogin.IssueRequest{}); !errors.Is(err, extshared.ErrNotPermitted) {
		t.Fatalf("missing resource must be rejected: %v", err)
	}
	if _, err := (Issuer{Resource: "r"}).IssueToken(context.Background(), feishulogin.IssueRequest{}); !errors.Is(err, extshared.ErrNotPermitted) {
		t.Fatalf("missing seats must be rejected: %v", err)
	}
	f2 := &fakeSeats{raw: "", seat: extseat.Seat{ID: "s"}, group: "AG1"}
	if _, err := (Issuer{Seats: f2, Resource: "r"}).IssueToken(context.Background(), feishulogin.IssueRequest{}); err == nil {
		t.Fatal("empty token from extseat must be rejected")
	}
}
