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

func (g *ProxyGateway) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	return "", ErrNotImplemented
}
