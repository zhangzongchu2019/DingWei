package m8

import (
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// groupNameRuleCases 编组名规则（R7：1～24 个码点；Unicode 字母／数字／ASCII 下划线；至少一个字母或数字）的共享用例表。
// 🔴 internal/store/group_name_rule_test.go 有同一张表的副本（store 包不可见 m8 包），改一处必须同步另一处。
var groupNameRuleCases = map[string]bool{
	"a": true, "Dev1": true, "开发组": true, "Ⅳ组": true, "八个字八个字八个": true, "九个字九个字九个字": true,
	"かな": true, "カタカナ": true, "한국어팀": true, "dev_team": true, "_a": true, "a_": true, "_1_": true,
	"abcdefghijklmnopqrstuvwx": true,
	"二十四个字二十四个字二十四个字二十四个字二十四个": true,
	"\U00020000":                     true,
	strings.Repeat("\U00020000", 24): true,
	"":                               false,
	"_":                              false,
	"___":                            false,
	"abcdefghijklmnopqrstuvwxy":      false,
	"二十五个字二十五个字二十五个字二十五个字二十五个字":      false,
	strings.Repeat("\U00020000", 25): false,
	" dev":                           false, "dev ": false, "a b": false, "a-b": false, "a.b": false, "a@b": false, "a/b": false,
	"a　b": false, "a\tb": false, "a!b": false, "组.一": false, "😀": false,
}

// 三处 Go 校验器（identity、m8 旧编组接口、store 旧平台落库）对同一张表结论一致，且等于期望。
func TestGroupNameValidatorsAgree(t *testing.T) {
	for name, want := range groupNameRuleCases {
		if got := identity.ValidGroupName(name); got != want {
			t.Errorf("identity.ValidGroupName(%q) = %v, want %v", name, got, want)
		}
		if got := validConsoleGroupName(name); got != want {
			t.Errorf("validConsoleGroupName(%q) = %v, want %v", name, got, want)
		}
	}
}

// 建议名截断到 24 个码点后接数字。
func TestSuggestedConsoleGroupNamesTruncatesTo24(t *testing.T) {
	name := strings.Repeat("组", 30)
	got := suggestedConsoleGroupNames(name, nil, 2)
	want := []string{strings.Repeat("组", 23) + "1", strings.Repeat("组", 23) + "2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("suggestions %v, want %v", got, want)
	}
	for _, s := range got {
		if !validConsoleGroupName(s) {
			t.Fatalf("suggestion %q invalid", s)
		}
	}
}
