package m8

import (
	"strings"
	"testing"
	"time"
)

// T-060：回合级失败通知的「→ 目标」位不得出现内部原因码。
func TestT060_TurnLevelFailureHasNoArrowTarget(t *testing.T) {
	c := &sessionClient{sessionName: "zzc-opus50ts-2642", keyID: "FB-k"}
	now := time.Now().UTC()

	// 无目标（回合级失败：TTL / 配额）⇒ 不得出现箭头
	env := deliveryFailureEnvelope(c, "dlv-1", "", "本轮未收到可靠终态，已按 TTL 关闭且不会派送", now)
	if strings.Contains(env.Body, "→") {
		t.Fatalf("回合级失败不应出现「→ 目标」位: %q", env.Body)
	}
	for _, code := range []string{"ttl", "capacity"} {
		if strings.Contains(env.Body, code) {
			t.Fatalf("内部原因码 %q 不得出现在通知正文: %q", code, env.Body)
		}
	}

	// 有真实目标（正常路径）⇒ 箭头必须保留，防止修过头
	env2 := deliveryFailureEnvelope(c, "dlv-2", "zzc-prodops-2642", "目标会话不在线", now)
	if !strings.Contains(env2.Body, "→ zzc-prodops-2642") {
		t.Fatalf("有目标时必须保留「→ 目标」: %q", env2.Body)
	}
}
