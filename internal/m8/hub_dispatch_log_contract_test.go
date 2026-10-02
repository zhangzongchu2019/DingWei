package m8

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFeishuDispatchRejectedLogHasStructuredReasons(t *testing.T) {
	previous := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	for _, reason := range []string{
		"sender_not_active_member",
		"cross_group_denied",
		"target_not_owned_or_offline",
		"short_name_not_resolvable",
		"route_error",
		"session_offline",
	} {
		logFeishuDispatchRejected(reason, "ou_visible", "target-visible", "bot-visible")
	}
	got := output.String()
	for _, want := range []string{
		"feishu dispatch rejected",
		"open_id=\"ou_visible\"",
		"target=\"target-visible\"",
		"bot_channel=\"bot-visible\"",
		"reason=sender_not_active_member",
		"reason=cross_group_denied",
		"reason=target_not_owned_or_offline",
		"reason=short_name_not_resolvable",
		"reason=route_error",
		"reason=session_offline",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("结构化拒绝日志缺少 %q: %s", want, got)
		}
	}
}

// Every user-visible rejection production point must emit the structured log
// immediately before it. This source contract makes removing any one of the
// eleven calls fail even when another path still logs the same reason.
func TestEveryFeishuDispatchRejectionProductionPointIsLogged(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate contract source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "hub.go"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(source), "\n")
	type expectation struct {
		outputNeedle string
		logNeedle    string
		count        int
	}
	expectations := []expectation{
		{"env.Body = strings.Replace(env.Body", "logFeishuDispatchRejected(groupingRejectionReason(routeErr)", 1},
		{"Reply: groupAgentNotFoundText", "logFeishuDispatchRejected(groupingRejectionReason(err)", 3},
		{"Reply: \"该消息投递失败：\" + text", "logFeishuDispatchRejected(\"route_error\"", 5},
		{"return errors.New(\"目标会话不在你的账号下或不在线：\"", "logFeishuDispatchRejected(\"target_not_owned_or_offline\"", 1},
		{"return true, env, errors.New(\"目标会话无法补全为在线 canonical 地址：\"", "logFeishuDispatchRejected(\"short_name_not_resolvable\"", 1},
		{"Reply: \"目标会话无法补全为在线 canonical 地址：\"", "logFeishuDispatchRejected(\"short_name_not_resolvable\"", 1},
		{"return fmt.Errorf(\"session %s offline\"", "logFeishuDispatchRejected(\"session_offline\"", 1},
	}
	for _, expected := range expectations {
		matched := 0
		for index, line := range lines {
			if !strings.Contains(line, expected.outputNeedle) {
				continue
			}
			matched++
			start := index - 8
			if start < 0 {
				start = 0
			}
			window := strings.Join(lines[start:index], "\n")
			if !strings.Contains(window, expected.logNeedle) {
				t.Errorf("hub.go:%d rejection %q has no structured log %q immediately before it", index+1, expected.outputNeedle, expected.logNeedle)
			}
		}
		if matched != expected.count {
			t.Errorf("rejection production points for %q = %d, want %d", expected.outputNeedle, matched, expected.count)
		}
	}
}
