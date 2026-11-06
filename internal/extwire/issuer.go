// Package extwire 把 r53 各路的包装配到一起；这里只放跨路适配，不放业务逻辑。
package extwire

import (
	"context"
	"errors"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

// SeatIssuer 是 A 路 extseat.Service.IssueToken 的签名（只取这一个方法，便于测试替身）。
type SeatIssuer interface {
	IssueToken(ctx context.Context, p extshared.Principal, short, target string) (string, extseat.Seat, string, error)
}

// Issuer 把 C 路的 feishulogin.TokenIssuer 接到 A 路。
// 幂等：A 路 IssueToken 不接幂等键；重复签发由 C 路一次性 state 防住（回调重放不会再次进入这里），
// 因此 req.AttemptID 在这里不使用。
type Issuer struct {
	Seats    SeatIssuer
	Resource string // MCP 受保护资源标识，与 A 路 Service.Resource 同一配置
}

var _ feishulogin.TokenIssuer = Issuer{}

// IssueToken 实现 feishulogin.TokenIssuer。
func (a Issuer) IssueToken(ctx context.Context, req feishulogin.IssueRequest) (feishulogin.IssuedToken, error) {
	if a.Seats == nil || a.Resource == "" {
		return feishulogin.IssuedToken{}, extshared.ErrNotPermitted
	}
	raw, seat, group, err := a.Seats.IssueToken(ctx, req.Principal, req.ShortName, req.TargetAgent)
	if err != nil {
		return feishulogin.IssuedToken{}, err
	}
	if raw == "" || seat.ID == "" || group == "" {
		return feishulogin.IssuedToken{}, errors.New("extwire: incomplete issue result from extseat")
	}
	return feishulogin.IssuedToken{
		Token:                 raw,
		SeatID:                seat.ID,
		ExternalAgentFullName: seat.FullName,
		Group:                 extshared.AGCode(group),
		ExpiresAt:             seat.ExpiresAt,
		Audience:              a.Resource,
		Scopes:                append([]string(nil), extshared.MVPScopes[:]...),
	}, nil
}
