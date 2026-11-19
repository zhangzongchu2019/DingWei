package mcpgw

import (
	"io"
	"net/http"
)

const connectAttemptFailureDescription = "这个授权链接必须在发起授权的同一个浏览器里、5 分钟内打开。请回到你的 AI 工具（如 Antigravity 桌面端 Customizations → Installed → MCP Servers → dw123 → Authenticate）重新发起授权，不要复制链接到别的浏览器。"

// Static content deliberately reveals neither the submitted attempt identifier
// nor whether it exists, expired, or has a different browser binding.
const connectAttemptFailureHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>请重新发起授权</title></head><body><main><h1>请重新发起授权</h1><p role="alert">` + connectAttemptFailureDescription + `</p></main></body></html>`

func failConnectAttempt(w http.ResponseWriter, r *http.Request) {
	if !accepts(r.Header.Get("Accept"), "text/html") {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, connectAttemptFailureHTML)
}
