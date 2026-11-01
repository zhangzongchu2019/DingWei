package m8

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type legacySpecFreezeRepository interface {
	BeginLegacySpecWrite(context.Context) (context.Context, func(), error)
	LegacySpecsFrozen(context.Context) (bool, error)
}

func (h *Hub) beginLegacySpecWrite(ctx context.Context) (context.Context, func(), error) {
	if repo, ok := h.Repo.(legacySpecFreezeRepository); ok {
		return repo.BeginLegacySpecWrite(ctx)
	}
	// Repositories without legacy-spec persistence cannot mutate those objects.
	return ctx, func() {}, nil
}

func (h *Hub) legacySpecMutationRequest(w http.ResponseWriter, r *http.Request) (*http.Request, func(), bool) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return r, func() {}, true
	}
	ctx, release, err := h.beginLegacySpecWrite(r.Context())
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		if errors.Is(err, store.ErrLegacySpecFrozen) {
			taskJSON(w, http.StatusConflict, map[string]any{"error": "legacy_spec_frozen", "message": "旧规格已冻结，请在仓库 OpenSpec 中提交新需求；本请求未写入。", "outcome": "rejected"})
		} else {
			http.Error(w, "旧规格冻结状态不可用，请稍后重试", http.StatusServiceUnavailable)
		}
		return r, nil, false
	}
	return r.WithContext(ctx), release, true
}

func legacySpecGenerationEnvelope(env model.Envelope) bool {
	typ := specReviewEnvelopeType(env)
	return specReviewType(typ) || typ == specPresplitRequestType || typ == specInferenceAssistRequestType
}
