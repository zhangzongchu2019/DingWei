package store

import (
	"strings"
	"testing"
)

// 编组名规则共享用例表的副本：🔴 必须与 internal/m8/group_name_rule_parity_test.go 的 groupNameRuleCases 保持一致
// （那里同时校验 identity.ValidGroupName 与 m8 validConsoleGroupName；store 包看不到 m8，故复制）。
var storeGroupNameRuleCases = map[string]bool{
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

func TestValidConsoleAgentGroupNameMatchesSharedTable(t *testing.T) {
	for name, want := range storeGroupNameRuleCases {
		if got := validConsoleAgentGroupName(name); got != want {
			t.Errorf("validConsoleAgentGroupName(%q) = %v, want %v", name, got, want)
		}
	}
}
