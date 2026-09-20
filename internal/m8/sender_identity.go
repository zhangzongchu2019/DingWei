package m8

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	agentReplyHint    = "回复方式：发送 #%s <内容>。"
	agentOneWayNotice = "回复说明：此消息为单向 Agent 输出，不可直接回复。"
)

// renderAgentFeishuBody is the final Agent-originated presentation boundary.
// Identity comes from the authenticated envelope address, never from body text.
func (h *Hub) renderAgentFeishuBody(ctx context.Context, env model.Envelope) string {
	from, err := parseAddress(env.From)
	if err != nil || from.Kind != addressSession || from.SessionName == "" || from.SessionName == systemAddressName {
		return protectRoutingLineStarts(env.Body)
	}

	shortName := from.SessionName
	displayName := shortName
	selectorName := shortName
	noDirectory := false
	if h.Repo != nil {
		if endpoints, listErr := h.Repo.ListSessionEndpoints(ctx); listErr == nil {
			for _, ep := range endpoints {
				if ep.KeyID != from.KeyID || ep.SessionName != from.SessionName {
					continue
				}
				if strings.TrimSpace(ep.FullSessionName) != "" {
					displayName = strings.TrimSpace(ep.FullSessionName)
				}
				noDirectory = ep.NoDirectory
				break
			}
		}
	}

	body := strings.TrimSpace(env.Body)
	if !hasTrustedLegacySenderPrefix(body, shortName, displayName) {
		body = fmt.Sprintf("【Agent：%s】\n%s", displayName, body)
	}
	replyLine := fmt.Sprintf(agentReplyHint, selectorName)
	if isProducerEnvelope(env) || noDirectory {
		replyLine = agentOneWayNotice
	}
	return protectRoutingLineStarts(strings.TrimSpace(body) + "\n\n" + replyLine)
}

// hasTrustedLegacySenderPrefix only accepts a prefix that contains the
// authenticated sender session. An arbitrary leading 【...】 cannot suppress
// the real sender label.
func hasTrustedLegacySenderPrefix(body string, sessionNames ...string) bool {
	if !strings.HasPrefix(body, "【") {
		return false
	}
	end := strings.Index(body, "】")
	if end < 0 || strings.ContainsAny(body[:end], "\r\n") {
		return false
	}
	prefix := body[:end+len("】")]
	for _, name := range sessionNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if prefix == "【"+name+"】" || strings.HasSuffix(prefix, "·"+name+"】") {
			return true
		}
	}
	return false
}

// protectRoutingLineStarts keeps explanatory/body lines from being mistaken
// for Hub selector syntax if the rendered text is later copied back verbatim.
func protectRoutingLineStarts(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			lines[i] = " " + line
		}
	}
	return strings.Join(lines, "\n")
}
