package hosted

// T-G4-51 · hosted 列表注入（契约 v2 第二节；G3 注入点＝internal/identity/httpapi 的
// API.Hosted，800f24d8 起在，签名 func(ctx, agentID) (map[string]any, bool)）。
//
// 接线形状（给 opus50tc2k）：newplat.go 里
//   api.Hosted = orch.HostedItem
// 一行即可——签名逐字匹配。G3 只对 origin=hosted 的行调用；返回的 map 原样进列表项的
// hosted 子对象（G3 不解析不改名）；false＝暂无数据 ⇒ 不出子对象，页面降级只显示在线/离线。
// 顶层 state（active/suspended）由 G3 给，是「是否停用」的权威来源；本函数只管托管语义。
//
// 契约字段：node_name、state（running|disabled|abnormal|busy）、upgradable、image_version、
// plan、account_last4；token_file 类账号无后四位 ⇒ 改出 account_kind＋account_registered_at
// （登记时间；组长裁定：token_file 显示「账号类别＋登记时间」）。

import (
	"context"
	"errors"
	"log"
	"time"
)

// HostedItem G3 注入点形状：单席位托管子对象。错误一律收敛为 false——列表读路径
// 不放大为整页 500；无数据（非托管/已销毁/未落席位）与出错同回 false，页面降级兜底。
// 🔵 但出错必须留痕（opus50tc2k 复核观察：静默降级运维看不出来）——收敛前打日志一行；
// 无数据（ok=false, err=nil）不打，那是正常缺席。需要区分错误语义的内部调用方用
// HostedOverlay（批量、错误上抛）。
func (o *Orchestrator) HostedItem(ctx context.Context, agentID string) (map[string]any, bool) {
	m, ok, err := o.hostedItem(ctx, agentID)
	if err != nil {
		log.Printf("hosted item agent=%s 查询失败，该行降级不出 hosted 子对象: %v", agentID, err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	return m, true
}

// HostedOverlay 批量版（内部/测试用；错误上抛，不做 false 收敛）。缺席语义同 HostedItem。
func (o *Orchestrator) HostedOverlay(ctx context.Context, agentIDs []string) (map[string]map[string]any, error) {
	out := make(map[string]map[string]any, len(agentIDs))
	for _, id := range agentIDs {
		m, ok, err := o.hostedItem(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out[id] = m
		}
	}
	return out, nil
}

// hostedItem 单席位映射核心。ok=false：未落席位（非托管）或已销毁（防御——G3 列表本不应含）。
func (o *Orchestrator) hostedItem(ctx context.Context, agentID string) (map[string]any, bool, error) {
	seat, err := o.st.GetSeat(ctx, agentID)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if seat.State == "destroyed" {
		return nil, false, nil
	}
	m := map[string]any{"plan": seat.Plan, "image_version": seat.ImageRef}

	node, nerr := o.st.GetNode(ctx, seat.NodeID)
	if nerr == nil {
		m["node_name"] = node.Name
	}
	// 状态映射（契约第二节）：busy＝操作中（busy/queued/pulling/starting，行操作全置灰）；
	// disabled＝已停用（节点离线不改写——停用是用户意图态）；节点离线/error/failed＝abnormal；
	// online＝running。
	var state string
	switch {
	case seat.State == "busy" || seat.State == "queued" || seat.State == "pulling" || seat.State == "starting":
		state = "busy"
	case seat.State == "disabled":
		state = "disabled"
	case nerr != nil || node.State != "online":
		state = "abnormal"
	case seat.State == "online":
		state = "running"
	default:
		state = "abnormal"
	}
	m["state"] = state
	// upgradable＝running 且 G1 镜像登记有更新（disabled 须先启用——生命周期 invalid_state 同口径）
	m["upgradable"] = state == "running" && func() bool {
		ref, ok := o.images.Latest(seat.CLI)
		return ok && ref != seat.ImageRef
	}()

	// 账号显示：api_key ⇒ account_last4；token_file ⇒ 无后四位，出类别＋登记时间（组长裁定）
	if seat.AccountRef.Valid {
		if acct, aerr := o.st.GetAccount(ctx, seat.AccountRef.String); aerr == nil {
			if acct.Kind == "token_file" {
				m["account_kind"] = acct.Kind
				m["account_registered_at"] = acct.CreatedAt.UTC().Format(time.RFC3339)
			} else {
				m["account_last4"] = acct.Last4
			}
		}
		// GetAccount 失败：账号字段缺席（不编造），其余字段照常——读路径降级
	}
	return m, true, nil
}
