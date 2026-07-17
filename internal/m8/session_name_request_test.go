package m8

import "testing"

const testKeyID = "FB-example-e0d10000" // key 末4位 0000

func TestDeriveRegisteredSessionNameCorrectsImpersonation(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		want      string
	}{
		{"裸短名按 owner 派生", "developer", "owner1-developer-0000"},
		{"自身完整名原样保留", "owner1-developer-0000", "owner1-developer-0000"},
		{"冒名 owner 被纠正", "alice-developer-0000", "owner1-developer-0000"},
		{"错末4位被纠正", "owner1-developer-3dd6", "owner1-developer-0000"},
		{"冒名 owner 且错末4位被纠正", "alice-developer-3dd6", "owner1-developer-0000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveRegisteredSessionName(tc.requested, testKeyID, "owner1"); got != tc.want {
				t.Fatalf("deriveRegisteredSessionName(%q) = %q, want %q", tc.requested, got, tc.want)
			}
		})
	}
}

func TestDeriveRegisteredSessionNameWithoutOwnerKeepsRequest(t *testing.T) {
	if got := deriveRegisteredSessionName("developer", testKeyID, ""); got != "developer" {
		t.Fatalf("owner 为空时应原样返回, got %q", got)
	}
}

// enforce 必须仍能拦下不合规上报名:B 方案下派生名恒合规,
// 故判定必须基于客户端上报名而非派生名。
func TestSessionNameRequestWarningJudgesClientRequest(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		ownerKey  string
		wantWarn  bool
	}{
		{"裸短名合规", "developer", "owner1", false},
		{"自身完整名合规", "owner1-developer-0000", "owner1", false},
		{"冒名 owner 触发告警", "alice-developer-0000", "owner1", true},
		{"错末4位触发告警", "owner1-developer-3dd6", "owner1", true},
		{"非法字符触发告警", "Dev-1", "owner1", true},
		{"key 未绑定成员触发告警", "developer", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warn := sessionNameRequestWarning(tc.requested, testKeyID, tc.ownerKey)
			if (warn != "") != tc.wantWarn {
				t.Fatalf("sessionNameRequestWarning(%q, owner=%q) = %q, wantWarn=%v", tc.requested, tc.ownerKey, warn, tc.wantWarn)
			}
		})
	}
}

// warn 模式(灰度默认)下冒名上报不得注册成被冒名者:注册名取派生名。
func TestWarnModeRegistersDerivedNameNotSpoofed(t *testing.T) {
	spoofed := "alice-boss-0000"
	if warn := sessionNameRequestWarning(spoofed, testKeyID, "owner1"); warn == "" {
		t.Fatal("冒名上报应产生告警")
	}
	if got := deriveRegisteredSessionName(spoofed, testKeyID, "owner1"); got != "owner1-boss-0000" {
		t.Fatalf("warn 模式注册名应为派生名 owner1-boss-0000, got %q", got)
	}
}
