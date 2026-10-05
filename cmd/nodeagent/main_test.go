package main

// envTruthy 格（R-SEAT-HUB-VIA-PROXY 复验预审）：开关解析语义固定——
// 生产配置写 1；未知值不得静默开启（fail-closed 到旧行为）。

import "testing"

func TestEnvTruthy(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"on", true},
		{"", false},       // 未设
		{"0", false},      // 显式关
		{"2", false},      // 未知数字
		{"enable", false}, // 未知词——不得静默开启
		{"off", false},
		{" 1 ", true}, // 空白容忍
	} {
		t.Setenv("NODEAGENT_SEAT_HUB_VIA_PROXY", tc.val)
		if got := envTruthy("NODEAGENT_SEAT_HUB_VIA_PROXY"); got != tc.want {
			t.Fatalf("envTruthy(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

// 包 B2：NODEAGENT_SEAT_CODEX_MODELS CSV 解析（逐项 TrimSpace、去空、空＝nil）。
func TestCsvList(t *testing.T) {
	t.Setenv("NODEAGENT_SEAT_CODEX_MODELS", "gpt-6.1-sol, gpt-5.5 ,,")
	got := csvList("NODEAGENT_SEAT_CODEX_MODELS")
	if len(got) != 2 || got[0] != "gpt-6.1-sol" || got[1] != "gpt-5.5" {
		t.Fatalf("解析: %v", got)
	}
	t.Setenv("NODEAGENT_SEAT_CODEX_MODELS", "")
	if got := csvList("NODEAGENT_SEAT_CODEX_MODELS"); got != nil {
		t.Fatalf("空应 nil: %v", got)
	}
}
