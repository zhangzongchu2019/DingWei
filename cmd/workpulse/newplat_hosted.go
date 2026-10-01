package main

import (
	"context"
	"encoding/json"
	"log"
)

// hostedListHook 把 G4 的批量叠加函数（hosted.Orchestrator.HostedOverlay，T-G4-51）包成 httpapi.API.Hosted 的逐行形状。
// G4 装配编排器的地方只需一行：cfg.hosted = hostedListHook(orch.HostedOverlay)。
//
//   - T 是 G4 的子对象类型；经 JSON 往返转成 map，键就是 G4 结构体的 json 标签 —— 字段名与取值由 G4 定，本侧不解析、不改名；
//   - 叠加结果里没有这个 Agent（非托管、未落席位、已销毁）⇒ 不出 hosted 子对象；
//   - 叠加出错 ⇒ 记一行日志、不出子对象：列表照常返回，页面按契约降级为只显示在线／离线，不因 G4 一时取不到而整页失败。
//
// 逐行调用：G4 的实现本就逐个 Agent 查席位，单个调用与批量调用代价相同，且一行出错不连累其他行。
func hostedListHook[T any](overlay func(context.Context, []string) (map[string]T, error)) func(context.Context, string) (map[string]any, bool) {
	return func(ctx context.Context, agentID string) (map[string]any, bool) {
		items, err := overlay(ctx, []string{agentID})
		if err != nil {
			log.Printf("hosted overlay agent=%s: %v", agentID, err)
			return nil, false
		}
		item, ok := items[agentID]
		if !ok {
			return nil, false
		}
		raw, err := json.Marshal(item)
		if err != nil {
			log.Printf("hosted overlay agent=%s: encode: %v", agentID, err)
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			log.Printf("hosted overlay agent=%s: not an object: %v", agentID, err)
			return nil, false
		}
		return out, true
	}
}
