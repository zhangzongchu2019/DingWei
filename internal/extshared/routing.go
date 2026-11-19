package extshared

import "context"

// Routing 只是客户端选择，不能用作授权证明。零值保留旧协议的原 AG。
type Routing struct {
	Mode string `json:"mode"`
	Kind string `json:"kind,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

func (r Routing) Valid() bool {
	return (r.Mode == "" && r.Kind == "" && r.Ref == "") ||
		(r.Mode == "direct" && r.Kind == "" && r.Ref == "") ||
		(r.Mode == "group" && (r.Kind == "ordinary" || r.Kind == "temporary") && r.Ref != "" && len(r.Ref) <= 128)
}

type routingKey struct{}

func WithRouting(ctx context.Context, r Routing) context.Context {
	return context.WithValue(ctx, routingKey{}, r)
}

func RoutingFrom(ctx context.Context) Routing {
	r, _ := ctx.Value(routingKey{}).(Routing)
	return r
}

// TenantAdmission 是后续跨租户审批接线的边界。本片只装配 SameTenantAdmission，
// 客户端 tenant label 或编组成员关系均不是准入证据。
type TenantAdmission interface {
	Check(context.Context, Principal, string) error
}

type SameTenantAdmission struct{}

func (SameTenantAdmission) Check(ctx context.Context, p Principal, team string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if team == "" || p.TenantKey != team {
		return ErrTenantRejected
	}
	return nil
}

// SeatChoice 仅由已验证主体的服务端查询产生，不包含凭据。
type SeatChoice struct {
	ID       string
	FullName string
}
