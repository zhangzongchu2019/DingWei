package m8

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const hubFailureNoticeMetaKey = "_hub_failure_notice_v1"

type failureNoticeRouteKey struct{}

func trustedFailureNoticeDelivery(d *model.SessionDelivery, key amrTurnKey) bool {
	return d != nil && d.KeyID == key.KeyID && d.SessionName == key.Session && d.SourceType == "failure_notice" && d.Provenance == model.FailureNoticeProvenance
}

// Only persisted Hub-created provenance bound to the authenticated session may
// establish the route context. Neither body, ID shape nor client Meta is proof.
func (h *Hub) failureNoticeRouteContext(ctx context.Context, key amrTurnKey) (context.Context, error) {
	if h == nil || h.Repo == nil || key.DeliveryID == "" {
		return ctx, nil
	}
	d, err := h.Repo.GetSessionDelivery(ctx, key.DeliveryID)
	if err != nil {
		return ctx, err
	}
	if trustedFailureNoticeDelivery(d, key) {
		ctx = context.WithValue(ctx, failureNoticeRouteKey{}, true)
	}
	return ctx, nil
}
func failureNoticeRouteStopped(ctx context.Context) bool {
	v, _ := ctx.Value(failureNoticeRouteKey{}).(bool)
	return v
}
