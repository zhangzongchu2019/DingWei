package feishulogin

import (
	"context"
	"net/url"
	"testing"
)

func TestUnpairedSelectionBindsVerifiedStateAndHonorsFlag(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f, "Helper.zzc")
	f.handler.allowUnpaired = true
	start, e := f.handler.Start(context.Background(), StartRequest{Automatic: true, AttemptID: "unpaired"})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(start.AuthorizationURL)
	selected, e := f.handler.Callback(context.Background(), CallbackRequest{State: u.Query().Get("state"), Code: "code"})
	if e != nil || selected.Selection == "" || !selected.AllowUnpaired || f.issuer.calls != 0 {
		t.Fatalf("identity must precede pair selection: %+v %v", selected, e)
	}
	result, e := f.handler.Callback(context.Background(), CallbackRequest{Selection: selected.Selection, Target: "__no_pair__", AttemptID: "unpaired"})
	if e != nil || result.Token == "" || !f.issuer.request.WithoutPair || f.issuer.request.TargetAgent != "" || f.issuer.request.Principal.Subject == "" {
		t.Fatalf("unpaired selection not bound to verified state: req=%+v %v", f.issuer.request, e)
	}
	if _, e = f.handler.Callback(context.Background(), CallbackRequest{Selection: selected.Selection, Target: "__no_pair__"}); e == nil {
		t.Fatal("unpaired selection replay issued another token")
	}
}
func TestUnpairedSelectionCannotBypassDisabledFlag(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f, "Helper.zzc", "Other.zzc")
	start, e := f.handler.Start(context.Background(), StartRequest{Automatic: true})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(start.AuthorizationURL)
	result, e := f.handler.Callback(context.Background(), CallbackRequest{State: u.Query().Get("state"), Code: "code"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.handler.Callback(context.Background(), CallbackRequest{Selection: result.Selection, Target: "__no_pair__"}); e == nil || f.issuer.calls != 0 {
		t.Fatal("disabled unpaired enrollment escaped policy")
	}
}
