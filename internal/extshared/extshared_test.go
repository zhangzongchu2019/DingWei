package extshared

import "testing"

func TestShortNameRules(t *testing.T) {
	for _, s := range []string{"a", "mybot", "abc12345", "abcdefghijklmnopqrst", "0"} {
		if !ValidShortName(s) {
			t.Fatalf("want valid: %q", s)
		}
	}
	for _, s := range []string{"", "abcdefghijklmnopqrstu", "MyBot", "my_bot", "my-bot", "机器人", "ab c"} {
		if ValidShortName(s) {
			t.Fatalf("want invalid: %q", s)
		}
	}
}

func TestAGCodeRoundTrip(t *testing.T) {
	for _, n := range []int64{1, 3, 42, 1000000} {
		ag, err := FormatAGCode(n)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := ParseAGCode(string(ag))
		if !ok || got != n {
			t.Fatalf("round trip %d -> %q -> %d %v", n, ag, got, ok)
		}
	}
	if _, err := FormatAGCode(0); err == nil {
		t.Fatal("seq 0 must be rejected")
	}
	for _, s := range []string{"AG0", "AG01", "ag1", "AG", "AG-1", "XG1", "AG1x"} {
		if _, ok := ParseAGCode(s); ok {
			t.Fatalf("want invalid AG code: %q", s)
		}
	}
}

func TestAgentNameCompose(t *testing.T) {
	name, err := AgentName("mybot", "AG3")
	if err != nil || name != "mybot_AG3" {
		t.Fatalf("got %q %v", name, err)
	}
	short, ag, ok := SplitAgentName(name)
	if !ok || short != "mybot" || ag != "AG3" {
		t.Fatalf("split %q -> %q %q %v", name, short, ag, ok)
	}
	if _, err := AgentName("MyBot", "AG3"); err == nil {
		t.Fatal("uppercase short name must be rejected")
	}
	if _, err := AgentName("mybot", "AG0"); err == nil {
		t.Fatal("invalid AG must be rejected")
	}
	for _, s := range []string{"prodops", "my_bot_AG3x", "_AG3", "mybot_", "MyBot_AG3"} {
		if _, _, ok := SplitAgentName(s); ok {
			t.Fatalf("want not external: %q", s)
		}
	}
}

func TestPrincipalKeyIgnoresDisplayName(t *testing.T) {
	a := Principal{Provider: "feishu", TenantKey: "t", Issuer: "app", Subject: "ou_1", DisplayName: "zhangwei"}
	b := a
	b.Subject = "ou_2"
	if a.Key() == b.Key() {
		t.Fatal("different open_id with same display name must not share a key")
	}
	c := a
	c.DisplayName = "other"
	if a.Key() != c.Key() {
		t.Fatal("display name must not affect key")
	}
}

func TestTTLConstants(t *testing.T) {
	if TokenTTL.Hours() != 336 || GroupTTL.Hours() != 72 || MessageTTL.Hours() != 24 ||
		PresenceTimeout.Seconds() != 90 || WaitMax.Seconds() != 25 {
		t.Fatal("TTL constants drifted from rulings ㊹-补4／提案 GW-005／GW-006")
	}
}

func TestScopeStringMatchesPersistedText(t *testing.T) {
	// A 路 0105 令牌行写入的 scopes 文本；两处必须逐字一致。
	if ScopeString() != "list_online send_message wait_for_messages ack" {
		t.Fatalf("scope text drifted: %q", ScopeString())
	}
}
