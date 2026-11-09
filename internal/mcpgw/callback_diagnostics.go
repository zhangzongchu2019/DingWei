package mcpgw

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

var callbackFailurePage = template.Must(template.New("callback-failure").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>接入未完成</title><style>body{font-family:system-ui,sans-serif;max-width:36rem;margin:3rem auto;padding:0 1rem;color:#17202a}code{overflow-wrap:anywhere}</style></head>
<body><main><h1>接入未完成</h1><p id="error-description" role="alert">{{.Description}}</p><p>OAuth error: <code id="oauth-error">{{.Error}}</code></p>
<details><summary>错误说明</summary><dl><dt>error_description</dt><dd>{{.Description}}</dd></dl></details></main></body></html>`))

type callbackFailureBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Description      string `json:"-"`
}

func callbackDescription(reason feishulogin.CallbackFailureReason) string {
	switch reason {
	case feishulogin.FailureTargetUnavailable:
		return "目标 Agent 不存在或不可用。请确认填写了系统中已存在且可用的另一个 Agent 全名。"
	case feishulogin.FailureTenantRejected:
		return "当前飞书账号不属于本组织，请使用本组织的飞书账号重新授权。"
	case feishulogin.FailureAuthorizationUsed:
		return "授权已过期或已使用，请重新发起接入。"
	case feishulogin.FailureFeishuUnavailable:
		return "飞书服务暂不可用，请稍后重试。"
	default:
		return "接入暂未完成，请重新发起；如问题持续，请联系运维。"
	}
}

func callbackAttemptPrefix(attempt string) string {
	if len(attempt) > 8 {
		return attempt[:8]
	}
	if attempt == "" {
		return "unknown"
	}
	return attempt
}

func callbackTargetCategory(category feishulogin.CallbackTargetCategory) string {
	switch category {
	case feishulogin.TargetCategoryExists:
		return "exists"
	case feishulogin.TargetCategoryMissing:
		return "missing"
	default:
		return "unverified"
	}
}

func (g *Gateway) failCallback(w http.ResponseWriter, r *http.Request, status int, oauthCode string,
	reason feishulogin.CallbackFailureReason, attempt string, target feishulogin.CallbackTargetCategory) {
	if reason == "" {
		reason = feishulogin.FailureOther
	}
	if target == "" {
		target = feishulogin.TargetCategoryUnverified
	}
	if g.cfg.Logger != nil {
		g.cfg.Logger.Warn("MCP Feishu authorization callback failed",
			"reason_code", string(reason),
			"attempt_prefix", callbackAttemptPrefix(attempt),
			"target_category", callbackTargetCategory(target),
			"utc", g.cfg.Now().UTC().Format(time.RFC3339Nano),
		)
	} else {
		slog.Default().Warn("MCP Feishu authorization callback failed",
			"reason_code", string(reason),
			"attempt_prefix", callbackAttemptPrefix(attempt),
			"target_category", callbackTargetCategory(target),
			"utc", g.cfg.Now().UTC().Format(time.RFC3339Nano),
		)
	}
	if oauthCode == "" {
		oauthCode = "access_denied"
	}
	description := callbackDescription(reason)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_ = callbackFailurePage.Execute(w, callbackFailureBody{Error: oauthCode, ErrorDescription: description, Description: description})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(callbackFailureBody{Error: oauthCode, ErrorDescription: description})
}
