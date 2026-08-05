package datad

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是 datad VS API 的只读 HTTP 客户端(看板直连用,T18 切换)。
// 仅 GET:不承载任何写路径,单写入口铁律(coordinator 经 API 写)不经此处。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient 构造客户端;base 为 datad 根地址(如 https://dingwei.wegoab.com/datad)。
func NewClient(base, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	u, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return fmt.Errorf("datad url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("datad request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("datad get %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("datad get %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("datad decode %s: %w", path, err)
	}
	return nil
}

// ListTasks 拉取任务台账(GET /v1/tasks)。
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	if err := c.get(ctx, "/v1/tasks", &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

// ListSessions 拉取会话状态(GET /v1/sessions)。
func (c *Client) ListSessions(ctx context.Context) ([]SessionState, error) {
	var out struct {
		Sessions []SessionState `json:"sessions"`
	}
	if err := c.get(ctx, "/v1/sessions", &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}
