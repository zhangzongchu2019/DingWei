package fcs

import (
	"regexp"
	"testing"
)

// 飞书把裸点分四段当 IP 自动转链接。近似判据:纯数字点分四段。
var looksLikeIPv4 = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

func TestStampVersionNotLinkifiedAsIP(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.1.2.9", "v0.1.2.9"},
		{"v0.1.2.9", "v0.1.2.9"}, // 已带 v,不重复加
		{"0.1.3.0", "v0.1.3.0"},
		{"", ""},
		{"  0.1.2.9  ", "v0.1.2.9"},
	}
	for _, c := range cases {
		got := stampVersion(c.in)
		if got != c.want {
			t.Fatalf("stampVersion(%q) = %q, want %q", c.in, got, c.want)
		}
		if looksLikeIPv4.MatchString(got) {
			t.Fatalf("stampVersion(%q) = %q 仍会被飞书判为 IP 并转成链接", c.in, got)
		}
	}
}

// 阳性对照:未经处理的版本号确实会被判为 IP —— 否则上面的断言是空的。
func TestRawVersionWouldBeLinkified(t *testing.T) {
	if !looksLikeIPv4.MatchString("0.1.2.9") {
		t.Fatal("判据本身失效:裸版本号没被判为 IP,则本组测试不成立")
	}
}
