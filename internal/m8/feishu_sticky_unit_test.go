package m8

import (
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func TestR14UnitOwnerStateAndExactPrefix(t *testing.T) {
	agents := []identity.Agent{{ID: "exact", OwnerLC: "owner", Name: "ts3dev55", State: identity.AgentActive}, {ID: "longer", OwnerLC: "owner", Name: "ts3dev5520", State: identity.AgentActive}}
	full := map[string]string{"exact": "ts3dev55.owner", "longer": "ts3dev5520.owner"}
	for _, tc := range []struct{ input, id string }{{"ts3dev55", "exact"}, {"ts3dev", ""}, {"ts3dev552", "longer"}, {"TS3dev55.owner", "exact"}, {"ts3dev55.other", ""}} {
		got := stickyMatch(agents, full, tc.input)
		if tc.id == "" {
			r14Assert(t, got == nil, "UNIT_S021_AMBIGUOUS_OR_FOREIGN", tc.input, got)
		} else {
			r14Assert(t, got != nil && got.ID == tc.id, "UNIT_S021_EXACT_PREFIX", tc.input, got)
		}
	}
	r14Assert(t, stickyOwnedActive(agents[0], "owner") && !stickyOwnedActive(agents[0], "other"), "UNIT_S003_OWNER_SCOPE")
	agents[0].State = identity.AgentSuspended
	r14Assert(t, !stickyOwnedActive(agents[0], "owner"), "UNIT_S004_SUSPENDED")
}
func TestR14UnitCandidateTextParserAndOrdering(t *testing.T) {
	agents := []identity.Agent{{ID: "A"}, {ID: "B"}, {ID: "C"}}
	full := map[string]string{"A": "Alpha.owner", "B": "Beta.owner", "C": "Charlie.owner"}
	text := stickyCandidates(agents, full, "B")
	r14Assert(t, text == stickyCandidateTitle+"\n#Beta.owner\n#Alpha.owner\n#Charlie.owner", "UNIT_S011_ORDER_AND_HASH", text)
	for _, tc := range []struct {
		text  string
		block bool
	}{{text, true}, {strings.ReplaceAll(text, "\n", "\r\n"), true}, {"#Beta.owner\n#Alpha.owner", true}, {"#Beta.owner business\n#Alpha.owner", false}, {"business #Beta.owner\n#Alpha.owner", false}, {"#Beta.owner", false}, {"# heading\n#Beta.owner", false}} {
		r14Assert(t, stickyCandidateBlock(tc.text) == tc.block, "UNIT_S012_CANDIDATE_BLOCK", tc)
	}
	name, body, ok := parseSelector("#Beta.owner")
	r14Assert(t, ok && name == "Beta.owner" && body == "", "UNIT_S022_SELECT_ONLY")
	_, _, ok = parseSelector("body #Beta.owner")
	r14Assert(t, !ok, "UNIT_S022_MIDDLE_SELECTOR")
}
