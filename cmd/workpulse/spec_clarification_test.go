package main

import (
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func TestClarificationStartupFlagsIndependent(t *testing.T) {
	for _, outbound := range []string{"", "0", "1"} {
		for _, thread := range []string{"", "0", "1", " true ", " 1 "} {
			for _, reply := range []string{"", "0", "1", "true", " 1 "} {
				t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", outbound)
				t.Setenv("DW_SPEC_CLARIFICATION_ENABLED", thread)
				t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", reply)
				h := &m8.Hub{QuestionFeishuEnabled: outbound == "1"}
				configureSpecClarification(h)
				if h.SpecClarificationEnabled != (thread == "1" || thread == " 1 ") || h.SpecClarificationFeishuReplyEnabled != (reply == "1" || reply == " 1 ") || h.QuestionFeishuEnabled != (outbound == "1") {
					t.Fatal("CLARIFICATION_SWITCH_COUPLED", outbound, thread, reply)
				}
				t.Setenv("DW_SPEC_CLARIFICATION_ENABLED", "opposite")
				t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", "opposite")
				if h.SpecClarificationEnabled != (thread == "1" || thread == " 1 ") || h.SpecClarificationFeishuReplyEnabled != (reply == "1" || reply == " 1 ") {
					t.Fatal("CLARIFICATION_SWITCH_NOT_STARTUP_SNAPSHOT")
				}
			}
		}
	}
}
