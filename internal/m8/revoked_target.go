package m8

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// errTargetRevoked 发往已吊销 Agent 的消息：发送方收到明确的「目标已吊销」，不静默、也不只说「离线」（V2 A5）。
var errTargetRevoked = errors.New("目标已吊销")

// explainRevokedTarget 新平台模式下，路由失败的目标若落在某用户的虚拟 key 上、且该名下只剩已吊销的同名 Agent，
// 就把失败原因改写为「目标已吊销」。其余情况原样返回（不存在、在用但离线，都不改）。
func (h *Hub) explainRevokedTarget(ctx context.Context, target string, routeErr error) error {
	if h.Identity == nil || routeErr == nil {
		return routeErr
	}
	addr, err := parseAddress(target)
	if err != nil || addr.Kind != addressSession || !strings.HasPrefix(addr.KeyID, "u_") {
		return routeErr
	}
	ag, err := h.Identity.Store.FindAgentByName(ctx, strings.TrimPrefix(addr.KeyID, "u_"), addr.SessionName)
	if err != nil || ag.State != identity.AgentRevoked {
		return routeErr
	}
	return fmt.Errorf("%w：%s（%s 吊销）", errTargetRevoked, ag.Name, ag.RevokedAt.UTC().Format("2006-01-02T15:04Z"))
}
