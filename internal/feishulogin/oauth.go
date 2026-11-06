package feishulogin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultAuthorizeURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	DefaultTokenURL     = "https://accounts.feishu.cn/oauth/v3/token"
	DefaultUserInfoURL  = "https://open.feishu.cn/open-apis/authen/v1/user_info"
	maxResponseBytes    = 64 << 10
)

// OAuthConfig configures the Feishu OAuth v3 adapter. Endpoint overrides exist
// for deterministic httptest servers; production uses the defaults.
type OAuthConfig struct {
	AppID        string
	AppSecret    string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
	HTTPClient   *http.Client
}

// FeishuClient implements the current Feishu web OAuth flow, not the legacy QR SDK.
type FeishuClient struct {
	appID       string
	appSecret   string
	redirectURI string
	authorize   string
	token       string
	userInfo    string
	http        *http.Client
}

func NewFeishuClient(cfg OAuthConfig) (*FeishuClient, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" || !validRedirectURI(cfg.RedirectURI) {
		return nil, ErrConfig
	}
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = DefaultAuthorizeURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultTokenURL
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = DefaultUserInfoURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &FeishuClient{
		appID: cfg.AppID, appSecret: cfg.AppSecret, redirectURI: cfg.RedirectURI,
		authorize: cfg.AuthorizeURL, token: cfg.TokenURL, userInfo: cfg.UserInfoURL, http: &clone,
	}, nil
}

func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && u.Host != "" && u.User == nil && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http")
}

func (c *FeishuClient) AuthorizationURL(state, redirectURI string) (string, error) {
	if state == "" || redirectURI != c.redirectURI {
		return "", ErrInvalidStart
	}
	u, err := url.Parse(c.authorize)
	if err != nil || !u.IsAbs() {
		return "", ErrConfig
	}
	q := u.Query()
	q.Set("client_id", c.appID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.redirectURI)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *FeishuClient) ExchangeCode(ctx context.Context, code, redirectURI string) (UserAccessToken, error) {
	if code == "" || redirectURI != c.redirectURI {
		return "", ErrInvalidCallback
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {c.appID},
		"client_secret": {c.appSecret},
		"code":          {code},
		"redirect_uri":  {c.redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.token, strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrUpstream
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var response struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Code        int    `json:"code"`
	}
	if err := c.doJSON(req, &response); err != nil || response.Code != 0 || response.AccessToken == "" ||
		(response.TokenType != "" && !strings.EqualFold(response.TokenType, "Bearer")) {
		return "", ErrUpstream
	}
	return UserAccessToken(response.AccessToken), nil
}

func (c *FeishuClient) UserInfo(ctx context.Context, token UserAccessToken) (UserInfo, error) {
	if token == "" {
		return UserInfo{}, ErrUpstream
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userInfo, nil)
	if err != nil {
		return UserInfo{}, ErrUpstream
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Accept", "application/json")
	var response struct {
		Code int      `json:"code"`
		Data UserInfo `json:"data"`
	}
	if err := c.doJSON(req, &response); err != nil || response.Code != 0 {
		return UserInfo{}, ErrUpstream
	}
	return response.Data, nil
}

func (c *FeishuClient) doJSON(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUpstream
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes || json.Unmarshal(body, out) != nil {
		return ErrUpstream
	}
	return nil
}
