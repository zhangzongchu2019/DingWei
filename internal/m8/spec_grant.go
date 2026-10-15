package m8

// Compatibility response only. No approval state, request creation, capability
// issuance or poll collection survives the direct-write cutover.
import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
)

func (h *Hub) handleSpecGrantRequest(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	typ := metaString(env.Meta, "type")
	if typ != "spec_grant_request" && typ != "spec_grant_poll" {
		return false
	}
	if c != nil && c.conn != nil {
		requestID := strings.TrimSpace(metaString(env.Meta, "request_id"))
		if len(requestID) > 128 {
			requestID = ""
		}
		response := model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, firstNonEmpty(cKeyID(c), "unknown")), To: sessionAddress(c.sessionName, c.keyID), TS: h.now().Unix(), Meta: map[string]any{"type": "spec_grant_response", "request_id": requestID, "status": "upgrade_required", "message": "请升级 helper 后直接创建或更新 spec", "system": true, "no_mirror": true}}
		_ = c.write(ctx, response)
	}
	return true
}
