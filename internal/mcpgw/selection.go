package mcpgw

import (
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	"html/template"
	"net/http"
)

var selectionPage = template.Must(template.New("selection").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>选择协作目标</title><h1>选择协作目标</h1><form method="post" action="/mcp/oauth/feishu/callback"><input type="hidden" name="selection" value="{{.Selection}}">{{range .Targets}}<label><input type="radio" name="target_agent" required value="{{.}}" {{if eq . $.Preferred}}checked{{end}}>{{.}}</label><br>{{end}}{{if .AllowUnpaired}}<label><input type="radio" name="target_agent" required value="__no_pair__">不创建临时配对（加入普通编组前不可通信）</label>{{end}}<button type="submit">同意并接入</button></form></html>`))

func (g *Gateway) renderSelection(w http.ResponseWriter, r *http.Request, issued feishulogin.CallbackResult, attempt string, a authorization) {
	if issued.AttemptID != attempt || (len(issued.Targets) == 0 && !issued.AllowUnpaired) {
		oauthError(w, 400, "access_denied")
		return
	}
	if err := g.cfg.Store.Put(r.Context(), "login", hash("selection:"+issued.Selection), []byte(attempt), a.Expires, g.cfg.Now()); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", feishulogin.FormRedirectPolicy("default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'", a.Redirect))
	w.Header().Set("X-Frame-Options", "DENY")
	_ = selectionPage.Execute(w, issued)
}
