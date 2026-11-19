package m8

import (
	"context"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// dispatchAgentFeishuCopyCommand accepts only an anchored private-chat command
// from a currently verified Feishu identity. A command-shaped malformed line is
// consumed with usage guidance and is never forwarded as Agent content.
func (h *Hub) dispatchAgentFeishuCopyCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	trimmed := strings.TrimSpace(text)
	const command = "Agent抄送"
	if trimmed != command && !strings.HasPrefix(trimmed, command+" ") && !strings.HasPrefix(trimmed, command+"\t") {
		return model.PrefixDispatchResult{}, false, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	if model.NormalizeIngressProvenance(msg.IngressProvenance) != model.IngressFeishuWSAuthenticated {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法验证此飞书账号，设置未更改。"}, true, nil
	}
	rest := strings.TrimSpace(trimmed[len(command):])
	space := strings.IndexAny(rest, " \t")
	if rest == "" || space == 0 {
		return model.PrefixDispatchResult{Matched: true, Reply: "格式：Agent抄送 开 <Agent全名> 或 Agent抄送 关 <Agent全名>。仅有一个 Agent 时可省略全名。"}, true, nil
	}
	action, name := rest, ""
	if space > 0 {
		action, name = rest[:space], strings.TrimSpace(rest[space:])
	}
	if (action != "开" && action != "关") || strings.ContainsAny(name, "\r\n") {
		return model.PrefixDispatchResult{Matched: true, Reply: "格式：Agent抄送 开 <Agent全名> 或 Agent抄送 关 <Agent全名>。仅有一个 Agent 时可省略全名。"}, true, nil
	}
	if h.Identity == nil || h.AgentFeishuCopies == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "Agent 飞书抄送设置暂不可用。"}, true, nil
	}
	user, ok := h.newPlatformFeishuUser(ctx, msg)
	if !ok || user.State != identity.UserActive {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法验证此飞书账号，设置未更改。"}, true, nil
	}
	agents, err := h.Identity.Store.ListAgents(ctx, user.UserIDLC())
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	active := make([]identity.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent.State == identity.AgentActive {
			active = append(active, agent)
		}
	}
	if len(active) == 0 {
		return model.PrefixDispatchResult{Matched: true, Reply: "你名下没有可设置的活动 Agent。"}, true, nil
	}
	var selected *identity.Agent
	if name == "" {
		if len(active) != 1 {
			names := make([]string, 0, len(active))
			for i := range active {
				names = append(names, h.Identity.FullName(ctx, &active[i]))
			}
			return model.PrefixDispatchResult{Matched: true, Reply: "请指定 Agent 全名：" + strings.Join(names, "、")}, true, nil
		}
		selected = &active[0]
	} else {
		for i := range active {
			if strings.EqualFold(h.Identity.FullName(ctx, &active[i]), name) {
				if selected != nil {
					return model.PrefixDispatchResult{Matched: true, Reply: "Agent 全名不唯一，设置未更改。"}, true, nil
				}
				selected = &active[i]
			}
		}
		if selected == nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到该 Agent，设置未更改。"}, true, nil
		}
	}
	enabled := action == "开"
	if err := h.AgentFeishuCopies.SetPreference(ctx, user.UserIDLC(), selected.ID, enabled); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	state := "已关闭"
	if enabled {
		state = "已开启"
	}
	return model.PrefixDispatchResult{Matched: true, Reply: state + " Agent 抄送：" + h.Identity.FullName(ctx, selected)}, true, nil
}
