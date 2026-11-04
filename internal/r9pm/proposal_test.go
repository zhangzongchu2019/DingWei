package r9pm

import (
	"errors"
	"testing"
)

func TestParseDecisionPointsRequiresCompleteUnambiguousSection(t *testing.T) {
	valid := `# Change
## 待 PM 决策
- R9-PM02: 决策方式
  - 问题：超时后是否推进？
  - A. 保持等待
  - B. 自动同意
  - 建议：A
### 说明
这里不属于该节
`
	pts, err := ParseDecisionPoints(valid)
	if err != nil || len(pts) != 1 || pts[0].ID != "R9-PM02" || len(pts[0].Options) != 2 {
		t.Fatalf("valid point parse=%+v err=%v", pts, err)
	}
	if pts[0].ContentSHA256("pm-a") == pts[0].ContentSHA256("pm-b") {
		t.Fatal("PM identity missing from content fingerprint")
	}
	for _, bad := range []string{
		"# x\n## 待 PM 决策\n```md\n- R9-PM01: fake\n```\n",
		"## 待 PM 决策\n- R9-PM01: one\n- R9-PM01: two\n",
		"## 待 PM 决策\n- R9-PM01: incomplete\n  - A. only one\n",
		"## 待 PM 决策\nmalformed prose\n",
		"# proposal without section\n",
	} {
		if _, err := ParseDecisionPoints(bad); !errors.Is(err, ErrIncomplete) {
			t.Errorf("invalid section accepted: %q err=%v", bad, err)
		}
	}
	empty, err := ParseDecisionPoints("## 待 PM 决策\n无待决项\n")
	if err != nil || len(empty) != 0 {
		t.Fatalf("explicit empty set=%v err=%v", empty, err)
	}
}

func TestParseDecisionPointsRejectsMalformedClosedPointAndFence(t *testing.T) {
	for _, bad := range []string{"## 待 PM 决策\n- R9-PM01: 状态：已决\n", "## 待 PM 决策\n```\n- R9-PM01: hidden\n"} {
		if _, err := ParseDecisionPoints(bad); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("malformed complete-set evidence accepted: %v", err)
		}
	}
}
