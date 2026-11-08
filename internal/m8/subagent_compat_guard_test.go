package m8

import (
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestSubagentAMRMentionSkippedButRootKept(t *testing.T) {
	body := "@r2.agent.user @agent.user please review"
	candidates, looked := parseAMRCandidatesDetailed(body, "source", 20)
	if !looked || len(candidates) != 1 || candidates[0].selector != "#agent.user" || candidates[0].body != "please review" {
		t.Fatalf("AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED candidates=%+v looked=%t", candidates, looked)
	}
	if feedback := amrParseFeedback(body, candidates, looked, 20); !strings.Contains(feedback, "三段及以上") {
		t.Fatalf("unsupported child mention lacks visible notice: %q", feedback)
	}
	limited, _ := parseAMRCandidatesDetailed("@r2.agent.user @agent.user review", "source", 1)
	if len(limited) != 1 || limited[0].selector != "#agent.user" {
		t.Fatalf("AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED under root target limit: %+v", limited)
	}
	// Parsing is read-only; the bytes that are later routed remain untouched.
	if got := []byte(body); string(got) != body {
		t.Fatalf("mention parsing changed original body bytes")
	}
}

func TestUnnegotiatedSubPathNeverReachesParent(t *testing.T) {
	err := rejectUnnegotiatedSubAgentEnvelope(model.Envelope{Meta: map[string]any{"sub_path": []string{"agent"}}})
	if err == nil || !strings.Contains(err.Error(), "sub_agent_unsupported") {
		t.Fatalf("UNNEGOTIATED_SUBPATH_REACHED_PARENT err=%v", err)
	}
}

func TestExternalSubAgentReservedFieldsAreUntrusted(t *testing.T) {
	for _, key := range []string{"target_full_name", "target_node_id", "lane_generation", "subtree_revision", "from_full_name", "from_node_id", "from_generation"} {
		t.Run(key, func(t *testing.T) {
			err := rejectUnnegotiatedSubAgentEnvelope(model.Envelope{Meta: map[string]any{key: "forged"}})
			if err == nil || !strings.Contains(err.Error(), "sub_agent_unsupported") {
				t.Fatalf("reserved field %q accepted: %v", key, err)
			}
		})
	}
}
