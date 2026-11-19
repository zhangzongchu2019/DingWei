package feishulogin

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestConnectTargetHasNoPresetAgentExample(t *testing.T) {
	f := newConnectFixture(t)
	response, _ := f.getForm(t, "")
	if response.Code != http.StatusOK {
		t.Fatalf("connect page status=%d", response.Code)
	}
	body := response.Body.String()
	input := regexp.MustCompile(`<input\b[^>]*\bid="target_agent"[^>]*>`).FindString(body)
	if input == "" || !strings.Contains(input, `value=""`) {
		t.Fatalf("CONNECT_TARGET_NOT_EMPTY: target input=%q", input)
	}
	if strings.Contains(input, "placeholder=") {
		t.Fatalf("CONNECT_TARGET_PRESET_EXAMPLE: target input=%q", input)
	}
	hint := regexp.MustCompile(`<small id="target_hint"[^>]*>([^<]*)</small>`).FindStringSubmatch(body)
	const want = "填写你要协作的 Agent 全名（向你的组长或管理员索取），系统中必须已存在"
	if len(hint) != 2 || hint[1] != want {
		t.Fatalf("CONNECT_TARGET_GUIDANCE: hint=%q", hint)
	}
	if regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`).MatchString(hint[1]) {
		t.Fatalf("CONNECT_TARGET_NAMED_EXAMPLE: hint=%q", hint[1])
	}
}
