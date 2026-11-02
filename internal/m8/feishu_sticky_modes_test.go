package m8

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"strings"
	"testing"
	"time"
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

// 嵌入的 nil capability 在任何 R14 存取时都会 panic，关闭态不得触碰它。
type stickyNoAccessRepository struct {
	store.Repository
	store.FeishuStickyRepository
}

func TestR14DisabledStartsNoWorkerAndAllowsAllDeliveries(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1 "} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("DW_FEISHU_STICKY_ENABLED", value)
			t.Setenv("FCS_RPC_URL", "")
			h, db, ctx := newTestHub(t)
			r14Assert(t, h.lifetime().background.Snapshot()["owned"] == 1, "FLAG_DISABLED_WORKER_STARTED", h.lifetime().background.Snapshot())
			guard := stickyNoAccessRepository{Repository: db}
			h.Repo = guard
			msg := feishuInboundMessage("disabled", "plain")
			_, handled, err := h.dispatchFeishuSticky(ctx, msg, "plain", false)
			r14Assert(t, !handled && err == nil, "FLAG_DISABLED_ROUTER_ENTERED")
			for _, env := range []model.Envelope{{}, {Meta: map[string]any{"r14_target_agent_id": "forged", "r14_principal_lc": "other"}}} {
				r14Assert(t, h.stickyDeliveryAllowed(ctx, nil, env), "FLAG_DISABLED_DELIVERY_FILTER")
			}
			h.cleanupFeishuSticky(ctx, time.Now())
			h.cleanupFeishuStickyWithRepository(ctx, time.Now(), guard)
			h.runFeishuStickyMaintenance(context.Background(), nil, guard)
		})
	}
	t.Run("enabled_in_process", func(t *testing.T) {
		t.Setenv("DW_FEISHU_STICKY_ENABLED", "1")
		t.Setenv("FCS_RPC_URL", "")
		h, _, _ := newTestHub(t)
		r14Assert(t, h.lifetime().background.Snapshot()["owned"] == 2, "FLAG_ENABLED_WORKER_MISSING")
	})
	t.Run("enabled_remote_fcs", func(t *testing.T) {
		t.Setenv("DW_FEISHU_STICKY_ENABLED", "1")
		t.Setenv("FCS_RPC_URL", "http://127.0.0.1:1/not-used")
		h, _, _ := newTestHub(t)
		r14Assert(t, h.lifetime().background.Snapshot()["owned"] == 1, "F01_REMOTE_WORKER_STARTED")
	})
}
