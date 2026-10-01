package hosted

// T-G4-40 · 配额服务：新建硬校验（C1/C7）· 已用统计（停用计入、destroyed 不计）·
// 新上限低于已用量允许保存、只限制之后的新建（G6）· 管理面 ListWithUsage/Get/Put（C6 仅管理用户）。
//
// 边界（G3 重审⑥）：凭据轮换（ReissueHostedToken——重建、升级）不算新建、不占新配额，
// 故本服务只暴露 CheckCreate；升级路径不经过它（满额席位升级成功是硬判据）。

import (
	"context"
	"encoding/json"
	"fmt"
)

type QuotaService struct {
	st  *Store
	idp IdentityIssuer
}

func NewQuotaService(st *Store, idp IdentityIssuer) *QuotaService {
	return &QuotaService{st: st, idp: idp}
}

// CheckCreate 新建硬校验：四项任一超额 ⇒ ErrQuotaExceeded（响应附带各维度剩余，供 C1 提示）。
func (q *QuotaService) CheckCreate(ctx context.Context, ownerLC string, plan Plan) error {
	quota, usage, err := q.quotaAndUsage(ctx, ownerLC)
	if err != nil {
		return err
	}
	// 契约分工（tosonnet50ts 联调第 6 条）：席位数超额 ⇒ quota_exceeded（带 remaining）；
	// 套餐本身超剩余额度（vcpu/mem/disk）⇒ plan_exceeds_quota（页面文案不同）。
	if usage.Agents+1 > quota.AgentsLimit {
		return &QuotaExceededError{Quota: quota, Usage: usage}
	}
	if usage.MemMB+plan.MemMB > quota.MemMBLimit ||
		usage.DiskMB+plan.DiskMB > quota.DiskMBLimit || usage.VCPU+plan.VCPU > quota.VCPULimit {
		return &PlanExceededError{Quota: quota, Usage: usage}
	}
	return nil
}

// QuotaExceededError 带各维度已用/上限的超额错误（契约：quota_exceeded 响应带剩余）。
type QuotaExceededError struct {
	Quota Quota
	Usage Usage
}

func (e *QuotaExceededError) Error() string { return ErrQuotaExceeded.Error() }
func (e *QuotaExceededError) Unwrap() error { return ErrQuotaExceeded }

// PlanExceededError 套餐超剩余（契约：plan_exceeds_quota 也带 remaining，同 quota_exceeded 形状）。
type PlanExceededError struct {
	Quota Quota
	Usage Usage
}

func (e *PlanExceededError) Error() string { return ErrPlanExceedsQuota.Error() }
func (e *PlanExceededError) Unwrap() error { return ErrPlanExceedsQuota }

// RemainingJSON 各维度剩余（契约响应附加字段）。
func (e *PlanExceededError) RemainingJSON() json.RawMessage {
	return (&QuotaExceededError{Quota: e.Quota, Usage: e.Usage}).RemainingJSON()
}

// RemainingJSON 各维度剩余（契约响应附加字段）。
func (e *QuotaExceededError) RemainingJSON() json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"agents":  max0(int64(e.Quota.AgentsLimit) - int64(e.Usage.Agents)),
		"vcpu":    max0(int64(e.Quota.VCPULimit) - int64(e.Usage.VCPU)),
		"mem_gb":  max0(e.Quota.MemMBLimit-e.Usage.MemMB) / 1024,
		"disk_gb": max0(e.Quota.DiskMBLimit-e.Usage.DiskMB) / 1024,
	})
	return raw
}

func max0(x int64) int64 {
	if x < 0 {
		return 0
	}
	return x
}

func (q *QuotaService) quotaAndUsage(ctx context.Context, ownerLC string) (Quota, Usage, error) {
	quota, err := q.st.GetQuota(ctx, ownerLC)
	if err != nil {
		return quota, Usage{}, err
	}
	agentIDs, err := q.idp.ListHostedAgentIDsByOwner(ctx, ownerLC)
	if err != nil {
		return quota, Usage{}, err
	}
	usage, err := q.st.UsageFor(ctx, agentIDs)
	return quota, usage, err
}

// QuotaWithUsage 管理视图一行：已用/上限并排（C 表第三节的形状）。
type QuotaWithUsage struct {
	UserLC string `json:"user_lc"`
	Quota  Quota  `json:"quota"`
	Usage  Usage  `json:"usage"`
}

// GetWithUsage 单用户已用/上限（配额页行；用户自助查自己也走这里——归属由 handler 判）。
func (q *QuotaService) GetWithUsage(ctx context.Context, ownerLC string) (QuotaWithUsage, error) {
	quota, usage, err := q.quotaAndUsage(ctx, ownerLC)
	return QuotaWithUsage{UserLC: ownerLC, Quota: quota, Usage: usage}, err
}

// ListWithUsage 管理面：按用户清单逐户已用/上限（用户清单由 identity 侧提供——接线时经
// httpapi.N3.Actor 的管理角色与 identity 的用户列表接口取，本层不复制用户表）。
func (q *QuotaService) ListWithUsage(ctx context.Context, userLCs []string) ([]QuotaWithUsage, error) {
	var out []QuotaWithUsage
	for _, u := range userLCs {
		row, err := q.GetWithUsage(ctx, u)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// Put 设上限：允许低于已用量（G6——保存成功、只限制之后的新建）；
// 调用方必须是管理用户（isAdmin=false ⇒ ErrForbiddenRole，C6 反向）。
func (q *QuotaService) Put(ctx context.Context, by string, isAdmin bool, quota Quota) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if quota.UserLC == "" {
		return fmt.Errorf("%w: user_lc 必填", ErrParams)
	}
	for _, v := range []int64{int64(quota.AgentsLimit), int64(quota.VCPULimit), quota.MemMBLimit, quota.DiskMBLimit} {
		if v < 0 {
			return fmt.Errorf("%w: 上限不得为负", ErrParams)
		}
	}
	return q.st.PutQuota(ctx, quota, by)
}
