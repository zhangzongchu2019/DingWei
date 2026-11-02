package m8

import (
	"encoding/json"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Different provider messages must share the same lane, not merely deduplicate.
func TestR14S016ConcurrentExplicitAndPlainHaveDurableOrder(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("opus50ts")
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, tc := range []struct{ id, text string }{{"lane-explicit", "#Beta concurrent-explicit-body"}, {"lane-plain", "concurrent-plain-body"}} {
		tc := tc
		go func() {
			<-start
			_, err := r.hub.Dispatch(r.ctx, feishuInboundMessage(tc.id, tc.text), tc.text)
			done <- err
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	key := store.FeishuStickyKey{UserIDLC: "zzc", BotChannelID: "devplatform"}
	explicit, fresh, err := r.repo.BeginFeishuSticky(r.ctx, key, "lane-explicit", "explicit", r.now)
	r14Assert(t, err == nil && !fresh, "S016_CONCURRENT_ORDER", err, fresh)
	plain, fresh, err := r.repo.BeginFeishuSticky(r.ctx, key, "lane-plain", "no-prefix", r.now)
	r14Assert(t, err == nil && !fresh, "S016_CONCURRENT_ORDER", err, fresh)
	expectedPlain := r.agents["opus50ts"]
	first, second := plain, explicit
	if explicit.Seq < plain.Seq {
		expectedPlain = r.agents["beta"]
		first, second = explicit, plain
	}
	r14Assert(t, first.Status == "accepted" && second.Status == "accepted" && first.BaseGeneration == 1 && first.ResultGeneration == 2 && second.BaseGeneration == 2 && second.ResultGeneration == 3, "S016_CONCURRENT_ORDER", first, second)
	r14Assert(t, explicit.TargetAgentID == r.agents["beta"] && plain.TargetAgentID == expectedPlain && r.state().AgentID == r.agents["beta"] && r.state().Generation == 3 && r.repo.count() == 2, "S016_CONCURRENT_ORDER", explicit, plain, r.state(), r.repo.count())
	r.repo.mu.Lock()
	sent := append([]model.SessionDelivery(nil), r.repo.sent...)
	r.repo.mu.Unlock()
	for _, d := range sent {
		var env model.Envelope
		if err := json.Unmarshal([]byte(d.Envelope), &env); err != nil {
			t.Fatal(err)
		}
		expected := r.agents["beta"]
		if env.Body == "concurrent-plain-body" {
			expected = expectedPlain
		}
		r14Assert(t, metaString(env.Meta, "r14_target_agent_id") == expected, "S016_CONCURRENT_ORDER", env)
	}
	t.Logf("same-key order first=%s seq=%d generation=%d->%d target=%s; second=%s seq=%d generation=%d->%d target=%s", first.SourceMessageID, first.Seq, first.BaseGeneration, first.ResultGeneration, first.TargetAgentID, second.SourceMessageID, second.Seq, second.BaseGeneration, second.ResultGeneration, second.TargetAgentID)
}
