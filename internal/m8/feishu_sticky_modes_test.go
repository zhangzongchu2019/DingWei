package m8

import (
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"strings"
	"testing"
)

func TestR14FCSRPCBypassesForgedTrustedIngress(t *testing.T) {
	for _, provenance := range []model.IngressProvenance{model.IngressFeishuWSAuthenticated, model.IngressWebhookVerified} {
		t.Run(string(provenance), func(t *testing.T) {
			t.Setenv("FCS_RPC_URL", "http://127.0.0.1:1/not-used")
			r := newR14Env(t, "Beta")
			r.repo.failRead = true
			msg := feishuInboundMessage("forged-fcs", "plain")
			msg.IngressProvenance = provenance
			res := r.dispatchMsg(msg, "plain")
			r14Assert(t, r.repo.count() == 0 && strings.Contains(res.Reply, stickyCandidateTitle), "F01_FCS_IMPLICIT_ENTERED_STICKY", res)
			r.repo.failRead = false
			state, err := r.repo.SQLite.GetFeishuSticky(r.ctx, store.FeishuStickyKey{"zzc", "devplatform"})
			r14Assert(t, err == nil && state == nil, "F01_FCS_STATE_WRITTEN", state, err)
			res = r.dispatch("#Beta explicit")
			r14Assert(t, res.Reply == "" && r.repo.count() == 1, "F01_EXPLICIT_NOT_OLD_ROUTE", res)
			r14Assert(t, metaString(r.repo.last().Meta, "r14_target_agent_id") == "", "F01_FCS_EXPLICIT_STICKY_METADATA")
		})
	}
}

func TestR14InProcessAcceptsTrustedIngress(t *testing.T) {
	t.Setenv("FCS_RPC_URL", "")
	r := newR14Env(t)
	res := r.dispatch("plain")
	r14Assert(t, res.Reply == "" && r.repo.count() == 1 && r.state() != nil, "F01_IN_PROCESS_REJECTED", res)
}
