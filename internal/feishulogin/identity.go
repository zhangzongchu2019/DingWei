package feishulogin

import (
	"strings"

	"github.com/mozillazg/go-pinyin"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

// ResolvePrincipal trusts only server-fetched user_info and server configuration.
func ResolvePrincipal(info UserInfo, appID, allowedTenant string) (extshared.Principal, error) {
	if appID == "" || allowedTenant == "" {
		return extshared.Principal{}, ErrConfig
	}
	if info.TenantKey == "" || info.TenantKey != allowedTenant {
		return extshared.Principal{}, extshared.ErrTenantRejected
	}
	if info.OpenID == "" {
		return extshared.Principal{}, ErrIdentityIncomplete
	}
	displayName := strings.TrimSpace(info.EnName)
	if displayName == "" {
		displayName = PinyinDisplayName(info.Name)
	}
	if displayName == "" {
		return extshared.Principal{}, ErrDisplayName
	}
	return extshared.Principal{
		Provider:    "feishu",
		TenantKey:   info.TenantKey,
		Issuer:      appID,
		Subject:     info.OpenID,
		UnionID:     info.UnionID,
		DisplayName: displayName,
	}, nil
}

// PinyinDisplayName applies Normal-style pinyin, joins without separators, and
// retains ASCII lowercase letters only. An empty result must reject issuance.
func PinyinDisplayName(name string) string {
	args := pinyin.NewArgs()
	args.Style = pinyin.Normal
	args.Fallback = func(r rune, _ pinyin.Args) []string { return []string{string(r)} }
	joined := strings.ToLower(strings.Join(pinyin.LazyPinyin(name, args), ""))
	var b strings.Builder
	b.Grow(len(joined))
	for i := 0; i < len(joined); i++ {
		if joined[i] >= 'a' && joined[i] <= 'z' {
			b.WriteByte(joined[i])
		}
	}
	return b.String()
}
