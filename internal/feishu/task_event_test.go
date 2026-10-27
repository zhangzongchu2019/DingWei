package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestTenantTaskUpdateIgnoredAtRealDispatcher(t *testing.T) {
	var logs bytes.Buffer
	g := &LarkGateway{botChannelID: "synthetic-task-bot", logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	var messages []model.Message
	disp := g.eventDispatcher(func(_ context.Context, m model.Message) error {
		messages = append(messages, m)
		return nil
	})
	for i := 0; i < 5; i++ {
		raw := []byte(fmt.Sprintf(`{"schema":"2.0","header":{"event_type":"task.task.update_tenant_v1","event_id":"synthetic-%d"},"event":{"open_id":"PRIVATE_TASK_USER","task_id":"PRIVATE_TASK_CONTENT"}}`, i))
		if _, err := disp.Do(context.Background(), raw); err != nil {
			t.Fatalf("tenant task event must be acknowledged without SDK missing-handler error: %v", err)
		}
	}
	if len(messages) != 0 || g.ingress.eventReceived.Load() != 0 || g.ingress.enqueued.Load() != 0 || g.ingress.taskUpdateIgnored.Load() != 5 {
		t.Fatalf("task events entered business path or lost ignored count: messages=%d received=%d enqueued=%d ignored=%d", len(messages), g.ingress.eventReceived.Load(), g.ingress.enqueued.Load(), g.ingress.taskUpdateIgnored.Load())
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("ignore records=%d", len(lines))
	}
	expectedCounts := []float64{1, 2, 4}
	for i, line := range lines {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatal(err)
		}
		if fields["msg"] != "feishu ingress event_ignored" || fields["event_type"] != "task.task.update_tenant_v1" || fields["reason"] != "unsupported_task_event" || fields["bot_channel"] != g.botChannelID || fields["count"] != expectedCounts[i] || fields["level"] != "INFO" {
			t.Fatalf("missing bounded ignore metadata: %s", line)
		}
	}
	if strings.Contains(logs.String(), "PRIVATE_") {
		t.Fatal("task data leaked into ignore log")
	}
	// The fix is narrow: unrelated unknown events must still fail dispatch.
	if _, err := disp.Do(context.Background(), []byte(`{"schema":"2.0","header":{"event_type":"synthetic.unknown_v1"},"event":{}}`)); err == nil {
		t.Fatal("unregistered event was silently swallowed")
	}
	if _, err := disp.Do(context.Background(), []byte(`{"schema":"2.0","header":{"event_type":"im.message.message_read_v1"},"event":{}}`)); err != nil {
		t.Fatal(err)
	}
	message := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"ou_synthetic"}},"message":{"message_id":"om_synthetic","chat_type":"p2p","content":"{\"text\":\"ordinary message\"}"}}}`)
	if _, err := disp.Do(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].FeishuMsgID != "om_synthetic" || g.ingress.enqueued.Load() != 1 || g.ingress.taskUpdateIgnored.Load() != 5 {
		t.Fatal("ordinary message path changed")
	}
	// Counters remain per bot, not a process-global reset on reconnect.
	other := &LarkGateway{botChannelID: "other", logger: g.logger}
	if other.ingress.taskUpdateIgnored.Load() != 0 {
		t.Fatal("cross-bot ignored count")
	}
}
