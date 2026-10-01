package hosted

// T-G4-35 配套 · hosted 列表注入（契约 v2 第二节：统一 GET /api/console/agents 由 G3 实现，
// 托管子对象由本函数叠加——tosonnet50ts fb962e61 起页面只读该端点）。
//
// 给 opus50tc2k 的函数形状：G3 装配列表时对 origin=hosted 的行收集 agent_id，调
//   HostedOverlay(ctx, ids) (map[string]HostedListItem, error)
// 把返回 map 逐项挂到行的 hosted 字段；map 中缺席的 id（非托管/已销毁/未落席位）⇒ 不挂
// hosted 子对象——页面按契约降级约定只显示在线/离线，不编造状态。

import (
	"context"
	"errors"
)

// HostedListItem 列表行的托管子对象（契约第二节字段名即 JSON 键）。
type HostedListItem struct {
	NodeName   string `json:"node_name"`
	State      string `json:"state"` // running|disabled|abnormal|busy
	Upgradable bool   `json:"upgradable"`
}

// HostedOverlay 批量取托管子对象。状态映射（契约第二节）：
//   · busy＝席位操作进行中（busy/queued/pulling/starting）——期间该行操作全部置灰；
//   · disabled＝已停用（节点离线不改写它——停用是用户意图态）；
//   · abnormal＝节点离线或席位 error/failed；
//   · running＝席位 online 且节点在线。
// upgradable＝有更新镜像登记且当前 running（disabled 须先启用，暂停中不可升级——生命周期层
// invalid_state 口径一致）。
func (o *Orchestrator) HostedOverlay(ctx context.Context, agentIDs []string) (map[string]HostedListItem, error) {
	out := make(map[string]HostedListItem, len(agentIDs))
	for _, id := range agentIDs {
		seat, err := o.st.GetSeat(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue // 未落席位：不挂子对象（页面降级）
		}
		if err != nil {
			return nil, err
		}
		if seat.State == "destroyed" {
			continue // 已销毁：Agent 已 Retire，G3 列表本不应含；防御性缺席
		}
		item := HostedListItem{}
		node, nerr := o.st.GetNode(ctx, seat.NodeID)
		if nerr == nil {
			item.NodeName = node.Name
		}
		switch {
		case seat.State == "busy" || seat.State == "queued" || seat.State == "pulling" || seat.State == "starting":
			item.State = "busy"
		case seat.State == "disabled":
			item.State = "disabled"
		case nerr != nil || node.State != "online":
			item.State = "abnormal"
		case seat.State == "online":
			item.State = "running"
		default: // error/failed 等
			item.State = "abnormal"
		}
		if item.State == "running" {
			if ref, ok := o.images.Latest(seat.CLI); ok && ref != seat.ImageRef {
				item.Upgradable = true
			}
		}
		out[id] = item
	}
	return out, nil
}
