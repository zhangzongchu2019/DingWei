package feishulogin

import (
	"errors"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

func TestResolvePrincipalPrefersUnicodeNameAndStableKey(t *testing.T) {
	got, err := ResolvePrincipal(UserInfo{
		Name: "  张伟  ", EnName: "Zhang Wei", OpenID: "open-1", UnionID: "union-1", TenantKey: "tenant-1",
	}, "app-1", "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "张伟" || got.Provider != "feishu" || got.Issuer != "app-1" ||
		got.Subject != "open-1" || got.UnionID != "union-1" {
		t.Fatalf("unexpected principal: %+v", got)
	}
	if want := "feishu|tenant-1|app-1|open-1"; got.Key() != want {
		t.Fatalf("key=%q want %q", got.Key(), want)
	}
}

func TestResolvePrincipalRejectsMissingIdentityFields(t *testing.T) {
	tests := []struct {
		name        string
		info        UserInfo
		want        error
		wantDisplay string
	}{
		{name: "missing tenant", info: UserInfo{OpenID: "open", EnName: "Name"}, want: extshared.ErrTenantRejected},
		{name: "missing open id", info: UserInfo{TenantKey: "tenant", EnName: "Name"}, want: ErrIdentityIncomplete},
		{name: "fallback en name", info: UserInfo{TenantKey: "tenant", OpenID: "open", EnName: " Zhang Wei "}, wantDisplay: "Zhang Wei"},
		{name: "invalid unicode name falls back to en name", info: UserInfo{TenantKey: "tenant", OpenID: "open", Name: "bad\x00name", EnName: "Fallback Name"}, wantDisplay: "Fallback Name"},
		{name: "empty display name", info: UserInfo{TenantKey: "tenant", OpenID: "open"}, want: ErrDisplayName},
		{name: "control character", info: UserInfo{TenantKey: "tenant", OpenID: "open", Name: "bad\x00name"}, want: ErrDisplayName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePrincipal(tt.info, "app", "tenant")
			if tt.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				if tt.wantDisplay != "" && got.DisplayName != tt.wantDisplay {
					t.Fatalf("display name=%q want %q", got.DisplayName, tt.wantDisplay)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want %v", err, tt.want)
			}
		})
	}
}

func TestPinyinDisplayNameNormalStyle(t *testing.T) {
	for name, want := range map[string]string{"张伟": "zhangwei", "欧阳娜": "ouyangna"} {
		if got := PinyinDisplayName(name); got != want {
			t.Fatalf("PinyinDisplayName(%q)=%q want %q", name, got, want)
		}
	}
}

func TestPinyinDisplayNameFiltersNonAZ(t *testing.T) {
	if got := PinyinDisplayName("张-伟123"); got != "zhangwei" {
		t.Fatalf("filtered display name=%q want zhangwei", got)
	}
}

func TestPinyinDisplayNameEmptyAfterFilter(t *testing.T) {
	if got := PinyinDisplayName("123 ---"); got != "" {
		t.Fatalf("display name=%q want empty", got)
	}
}

func TestPrincipalKeyDoesNotMergeAcrossApplications(t *testing.T) {
	profile := UserInfo{OpenID: "same-open-id", UnionID: "same-union", TenantKey: "tenant", EnName: "Same Name"}
	oldApp, err := ResolvePrincipal(profile, "old-app", "tenant")
	if err != nil {
		t.Fatal(err)
	}
	newApp, err := ResolvePrincipal(profile, "new-app", "tenant")
	if err != nil {
		t.Fatal(err)
	}
	if oldApp.Key() == newApp.Key() {
		t.Fatal("union_id or display name merged identities across issuers")
	}
	profile.OpenID = "new-open-id"
	migrated, err := ResolvePrincipal(profile, "new-app", "tenant")
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Key() == newApp.Key() || migrated.Subject != "new-open-id" {
		t.Fatal("new application open_id was treated as an old binding")
	}
}
