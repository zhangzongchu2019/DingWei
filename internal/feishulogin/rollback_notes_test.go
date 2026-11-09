package feishulogin

import (
	"os"
	"strings"
	"testing"
)

// The rollback acceptance criterion is an operator constraint, so protect its
// exact reviewable deliverable rather than inventing an automatic downgrade.
func TestConsentRollbackRunbook(t *testing.T) {
	raw, e := os.ReadFile("../../docs/reviews/mcp-zero-consent/RELEASE-NOTES.md")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"封口全部长名外部席位", "处理完这些席位确定未执行的投递", "保留墓碑与ACK", "存在长名行时不得恢复旧CHECK，只能前向修复", "关闭默认授权功能只停止产生新的长名，不改已有长名"} {
		if !strings.Contains(string(raw), s) {
			t.Fatalf("ZC_ROLLBACK_RUNBOOK_MISSING %s", s)
		}
	}
}
