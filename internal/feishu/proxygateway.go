package feishu

// ProxyGateway — T17 测试 Hub 出站模式(设计 §4 唯一 hub 小改):Send 不直连 Lark,
// 而是 POST OutMessage JSON 给 fan-out 代理 /outbound(bearer 鉴权),由代理统一加
// [测试环境]后缀并以代理自持凭据发飞书。测试 Hub 因此不持真飞书凭据(安全边界)。
// 仅 FEISHU_TRANSPORT=proxy 时装配;fake/ws 路径零触碰。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ProxyGateway struct {
	url    string // 代理 /outbound 完整 URL
	token  string
	client *http.Client
}

func NewProxyGateway(url, token string) *ProxyGateway {
	return &ProxyGateway{url: url, token: token, client: &http.Client{Timeout: 10 * time.Second}}
}

func (g *ProxyGateway) Send(ctx context.Context, out OutMessage) (string, error) {
	// T17 v2.3 §C/§K2:带 RequestID=v:1 托管契约(202=代理接管,返回 outbound_id
	// 作托管凭证记账);无 RequestID=legacy 立发(升级过渡兼容)。
	if out.RequestID != "" {
		return g.sendCustody(ctx, out)
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("fanout proxy status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out2 struct {
		MsgID string `json:"msg_id"`
	}
	if err := json.Unmarshal(body, &out2); err != nil {
		return "", err
	}
	return out2.MsgID, nil
}

// 名称解析在测试路不经代理(代理无名录);返回入参/未实现,与 Stub 语义一致。
func (g *ProxyGateway) ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error) {
	return chatID, nil
}

func (g *ProxyGateway) ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error) {
	return openID, nil
}

// ResolveNameToID T17 v2.3 §G/§K4:经代理 /resolve 透传(代理持凭据调 Lark 通讯录)。
// Hub 加性改动①:transport=proxy 下"按名发信"路径由此闭合。
func (g *ProxyGateway) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"v": 1, "bot_channel": botChannelID, "name": name, "chat_type": chatType,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(g.url, "/outbound")+"/resolve", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("feishu name %q not found via proxy", name)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxy resolve status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// sendCustody §K2 v:1 托管:202/200 均成功,返回 outbound_id(托管凭证,记账用)。
func (g *ProxyGateway) sendCustody(ctx context.Context, out OutMessage) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"v": 1, "request_id": out.RequestID, "bot_channel": out.BotChannelID,
		"to_id": out.ToID, "to_name": out.ToName, "to_type": out.ToType, "text": out.Text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fanout custody status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out2 struct {
		OutboundID string `json:"outbound_id"`
	}
	if err := json.Unmarshal(body, &out2); err != nil {
		return "", err
	}
	return out2.OutboundID, nil
}
