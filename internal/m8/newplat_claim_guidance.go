package m8

import (
	"context"
	"net/url"
	"regexp"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

var publicFeishuAppID = regexp.MustCompile(`^cli_[A-Za-z0-9_]{6,64}$`)

// ClaimGuidance deliberately projects only public bot metadata, never a channel's secrets.
func (h *Hub) ClaimGuidance(ctx context.Context) ([]httpapi.ClaimBot, error) {
	channels, err := h.Repo.ListBotChannels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.ClaimBot, 0)
	for _, channel := range channels {
		if !channel.Active || !channel.CanReceive || !publicFeishuAppID.MatchString(channel.AppID) {
			continue
		}
		name := channel.Name
		if name == "" {
			name = channel.ID
		}
		result = append(result, httpapi.ClaimBot{Name: name, AppID: channel.AppID,
			URL: "https://applink.feishu.cn/client/bot/open?appId=" + url.QueryEscape(channel.AppID)})
	}
	return result, nil
}
