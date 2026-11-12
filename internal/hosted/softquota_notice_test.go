package hosted

import (
	"strings"
	"testing"
	"time"
)

func TestQuotaNoticeDistinguishesMeasurementFailureFromUsageThreshold(t *testing.T) {
	parallelHostedTest(t)
	for _, tc := range []struct{ reason, want string }{{"usage_warning", "用量暂时无法测量"}, {"disk_warning", "磁盘用量已到或超过告警线"}, {"usage_unknown", "用量读数持续未知"}, {"disk_limit", "用量达到上限"}, {"node_inode_emergency", "inode 低于紧急线"}} {
		n := QuotaNotice{NodeID: "nd_fixture", SeatID: "ag_fixture", EventID: strings.Repeat("a", 32), Reason: tc.reason, MeasuredAt: time.Date(2026, 10, 17, 17, 0, 0, 0, time.UTC)}
		if text := n.Text(); !strings.Contains(text, tc.want) || !strings.Contains(text, "2026-10-17T17:00:00Z") {
			t.Fatalf("reason %s has wrong event semantics: %s", tc.reason, text)
		}
	}
}
