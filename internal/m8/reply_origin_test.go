package m8

import (
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func env(body string, meta map[string]any) model.Envelope {
	return model.Envelope{ID: "e1", To: "a#k", From: "b#k", Body: body, Meta: meta}
}

func TestClassifyProtocolClass(t *testing.T) {
	cases := []struct {
		name      string
		env       model.Envelope
		wantClass string
		wantSpoof bool
	}{
		{"plain conversation", env("hello", nil), protocolClassConversation, false},
		{"explicit conversation", env("hi", map[string]any{"protocol_class": "conversation"}), protocolClassConversation, false},
		{"explicit control", env("hi", map[string]any{"protocol_class": "control"}), protocolClassControl, false},
		{"mcs dispatch payload", env(`{"kind":"mcs_dispatch_v1","node_id":"n1"}`, nil), protocolClassControl, false},
		{"mcs receipt payload", env(`{"kind":"mcs_receipt_v1","phase":"done"}`, nil), protocolClassControl, false},
		{"board_control payload", env(`{"kind":"board_control"}`, nil), protocolClassControl, false},
		{"ack kind = control", env("x", map[string]any{"kind": kindAck}), protocolClassControl, false},
		{"delivery meta type", env("x", map[string]any{"type": "delivery_ack"}), protocolClassControl, false},
		// spoof: 显式声明 conversation 却带 reserved-control payload → control + spoof
		{"spoof conversation+mcs payload", env(`{"kind":"mcs_dispatch_v1"}`, map[string]any{"protocol_class": "conversation"}), protocolClassControl, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyProtocolClass(tc.env)
			if got.Class != tc.wantClass {
				t.Fatalf("class=%q want %q", got.Class, tc.wantClass)
			}
			if got.Spoof != tc.wantSpoof {
				t.Fatalf("spoof=%v want %v", got.Spoof, tc.wantSpoof)
			}
		})
	}
}

func TestKindDefaults(t *testing.T) {
	if k := kindOf(env("x", nil)); k != kindContent {
		t.Fatalf("default kind=%q want content", k)
	}
	if k := kindOf(env("x", map[string]any{"kind": "reply"})); k != kindReply {
		t.Fatalf("kind=%q want reply", k)
	}
	if k := kindOf(env("x", map[string]any{"kind": "bogus"})); k != kindContent {
		t.Fatalf("bogus kind should default to content, got %q", k)
	}
}

func TestHopBackstop(t *testing.T) {
	// 缺 hop_count/max_hops → hop 0 < defaultMaxHops → 不越界
	if hopBackstopExceeded(env("x", nil)) {
		t.Fatal("empty hop should not exceed")
	}
	// hop_count 达 defaultMaxHops → 越界
	if !hopBackstopExceeded(env("x", map[string]any{"hop_count": defaultMaxHops})) {
		t.Fatal("hop==defaultMaxHops should exceed")
	}
	// 自定义 max_hops=3:hop 2 不越界、hop 3 越界
	if hopBackstopExceeded(env("x", map[string]any{"hop_count": 2, "max_hops": 3})) {
		t.Fatal("hop 2 < max 3 should not exceed")
	}
	if !hopBackstopExceeded(env("x", map[string]any{"hop_count": 3, "max_hops": 3})) {
		t.Fatal("hop 3 >= max 3 should exceed")
	}
}

func TestReplyDedup(t *testing.T) {
	fake := time.Unix(1000, 0)
	d := newReplyDedup(5 * time.Minute)
	d.now = func() time.Time { return fake }

	// 空 key 永不去重
	if d.SeenOrMark("") {
		t.Fatal("empty key should never be seen")
	}
	// 首见 false,再见 true
	if d.SeenOrMark("k1") {
		t.Fatal("first sight should be false")
	}
	if !d.SeenOrMark("k1") {
		t.Fatal("second sight should be true (dup)")
	}
	// TTL 过期后当作首见
	fake = fake.Add(6 * time.Minute)
	if d.SeenOrMark("k1") {
		t.Fatal("after TTL expiry should be first sight again")
	}
	// Sweep 清过期
	fake = fake.Add(6 * time.Minute)
	if n := d.Sweep(); n != 1 {
		t.Fatalf("sweep removed %d want 1", n)
	}
	if d.size() != 0 {
		t.Fatalf("size after sweep=%d want 0", d.size())
	}
}
